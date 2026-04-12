package main

import (
	"context"
	"fmt"
	"grpc-server/proto"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

var (
	testClient proto.ServerClient
	testConn   *grpc.ClientConn
	dbClient   *dynamodb.Client
	s3Client   *s3.Client
)

func NewS3Client(cfg aws.Config, endpoint string) *s3.Client {
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})
	return client
}

func TestMain(m *testing.M) {
	var cfg aws.Config
	var err error

	if os.Getenv("TEST_ENV") == "aws" {
		// Use real AWS credentials and endpoints
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
		)
		if err != nil {
			log.Fatal(err)
		}
		dbClient = dynamodb.NewFromConfig(cfg)
		s3Client = s3.NewFromConfig(cfg)
	} else {
		// Local setup
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("fake", "fake", "fake")),
		)
		if err != nil {
			log.Fatal(err)
		}
		dbClient = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String("http://localhost:8000")
		})
		s3Client = NewS3Client(cfg, "http://localhost:4566")
	}

	os.Exit(m.Run())
}

func ctxForUser(email string) context.Context {
	md := metadata.New(map[string]string{"email": email})
	return metadata.NewOutgoingContext(context.Background(), md)
}

func getUser(t *testing.T, email string) User {
	t.Helper()
	result, err := dbClient.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String("user"),
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var user User
	attributevalue.UnmarshalMap(result.Item, &user)
	return user
}

func navigateTo(t *testing.T, email, folder string) {
	t.Helper()
	ctx := ctxForUser(email)
	_, err := testClient.ChangeDirectory(ctx, &proto.ChangeDirectoryRequest{Folder: folder})
	if err != nil {
		t.Fatalf("failed to cd to %s: %v", folder, err)
	}
}

func getTAsEmails(t *testing.T, className string) []string {
	t.Helper()
	result, err := dbClient.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String("classroom_metadata"),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: "class_info"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var classInfo ClassInfo
	attributevalue.UnmarshalMap(result.Item, &classInfo)
	return classInfo.TAs
}

func getProfessorEmail(t *testing.T, className string) string {
	t.Helper()
	result, err := dbClient.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String("classroom_metadata"),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: "class_info"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var classInfo ClassInfo
	attributevalue.UnmarshalMap(result.Item, &classInfo)
	return classInfo.Professor
}

func getStudentEmails(t *testing.T, className string) []string {
	t.Helper()
	result, err := dbClient.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String("classroom_metadata"),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: "class_info"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var classInfo ClassInfo
	attributevalue.UnmarshalMap(result.Item, &classInfo)
	return classInfo.Students
}

func resetUserDirectory(t *testing.T, email string) {
	t.Helper()
	ctx := ctxForUser(email)
	testClient.ChangeDirectory(ctx, &proto.ChangeDirectoryRequest{Folder: ""})
}

// uploadFile sends a file via the gRPC upload stream.
// Returns an error instead of calling t.Fatal so it's safe to call from goroutines.
func uploadFile(t *testing.T, email, filename, content string) error {
	t.Helper()
	ctx := ctxForUser(email)
	stream, err := testClient.Upload(ctx)
	if err != nil {
		return fmt.Errorf("failed to create upload stream: %w", err)
	}
	if err := stream.Send(&proto.UploadRequest{
		Request: &proto.UploadRequest_Metadata{
			Metadata: &proto.UploadMetadata{
				Name:        filename,
				ContentType: "text/plain",
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to send metadata: %w", err)
	}
	if err := stream.Send(&proto.UploadRequest{
		Request: &proto.UploadRequest_Chunk{
			Chunk: []byte(content),
		},
	}); err != nil {
		return fmt.Errorf("failed to send chunk: %w", err)
	}
	_, err = stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("upload close failed: %w", err)
	}
	return nil
}

// downloadFile retrieves a file via the gRPC download stream.
// Returns content and error instead of calling t.Fatal so it's safe from goroutines.
func downloadFile(t *testing.T, email, filename string) (string, error) {
	t.Helper()
	ctx := ctxForUser(email)
	stream, err := testClient.Download(ctx, &proto.DownloadRequest{Name: filename})
	if err != nil {
		return "", fmt.Errorf("failed to start download: %w", err)
	}
	var data []byte
	for {
		res, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("download recv failed: %w", err)
		}
		data = append(data, res.Data...)
	}
	return string(data), nil
}

func setupTest(t *testing.T) func() {
	lis := bufconn.Listen(1024 * 1024)

	srv := grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor(dbClient)),
		grpc.StreamInterceptor(streamInterceptor(dbClient)),
	)
	proto.RegisterServerServer(srv, NewServer(dbClient, s3Client))

	go srv.Serve(lis)

	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}

	testClient = proto.NewServerClient(conn)
	testConn = conn

	return func() {
		conn.Close()
		srv.Stop()
	}
}

// ─────────────────────────────────────────────────────
// Test: concurrent uploads from all students
// Each student uploads to their own folder — no path conflicts
// ─────────────────────────────────────────────────────
func TestConcurrentUploads(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	// Reset all users to root first
	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(students))

	for i, email := range students {
		user := getUser(t, email)
		folder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]
		navigateTo(t, email, "Khoury")
		navigateTo(t, email, "CS5010")
		navigateTo(t, email, folder)

		wg.Add(1)
		go func(i int, email string) {
			defer wg.Done()
			err := uploadFile(t, email, fmt.Sprintf("hw_%d.txt", i), fmt.Sprintf("homework from student %d", i))
			if err != nil {
				errs <- fmt.Errorf("student %d upload: %w", i, err)
			}
		}(i, email)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// Verify each student can download their file
	for i, email := range students {
		content, err := downloadFile(t, email, fmt.Sprintf("hw_%d.txt", i))
		if err != nil {
			t.Errorf("student %d download: %v", i, err)
			continue
		}
		expected := fmt.Sprintf("homework from student %d", i)
		if content != expected {
			t.Errorf("student %d: got %q, want %q", i, content, expected)
		}
	}
}

// ─────────────────────────────────────────────────────
// Test: teacher overwrites file while students download concurrently
// Every student must get a complete valid version, never partial/corrupt
// ─────────────────────────────────────────────────────
func TestReadWhileWriting(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	// Reset all users to root first
	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}
	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	navigateTo(t, teacherEmail, "announcements")

	if err := uploadFile(t, teacherEmail, "lecture.txt", "version 1"); err != nil {
		t.Fatalf("seed upload failed: %v", err)
	}

	validVersions := map[string]bool{
		"version 1": true, "version 2": true,
		"version 3": true, "version 4": true,
		"version 5": true,
	}

	type downloadResult struct {
		student string
		content string
		err     error
	}

	var wg sync.WaitGroup
	results := make(chan downloadResult, 200)
	uploadErrs := make(chan error, 10)

	// Teacher keeps overwriting the file
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 2; i <= 5; i++ {
			if err := uploadFile(t, teacherEmail, "lecture.txt", fmt.Sprintf("version %d", i)); err != nil {
				uploadErrs <- fmt.Errorf("teacher upload version %d: %w", i, err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Students download concurrently
	for _, email := range students {
		navigateTo(t, email, "Khoury")
		navigateTo(t, email, "CS5010")
		navigateTo(t, email, "announcements")

		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				content, err := downloadFile(t, email, "lecture.txt")
				results <- downloadResult{student: email, content: content, err: err}
				time.Sleep(30 * time.Millisecond)
			}
		}(email)
	}

	wg.Wait()
	close(results)
	close(uploadErrs)

	for err := range uploadErrs {
		t.Error(err)
	}

	successCount := 0
	for r := range results {
		if r.err != nil {
			t.Errorf("student %s download failed: %v", r.student, r.err)
			continue
		}
		if !validVersions[r.content] {
			t.Errorf("student %s got invalid content: %q", r.student, r.content)
			continue
		}
		successCount++
	}

	expectedMin := len(students) // at least one successful download per student
	if successCount < expectedMin {
		t.Errorf("only %d successful downloads, expected at least %d", successCount, expectedMin)
	}

}

// ─────────────────────────────────────────────────────
// Test: teacher deletes file while students are downloading
// In-progress S3 reads should complete; subsequent downloads get NotFound
// ─────────────────────────────────────────────────────
func TestDeleteDuringDownload(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	// Reset all users to root first
	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}

	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	navigateTo(t, teacherEmail, "announcements")

	// Seed a file large enough that downloads take measurable time
	bigContent := make([]byte, 512*1024) // 512KB
	for i := range bigContent {
		bigContent[i] = byte('A' + (i % 26))
	}
	if err := uploadFile(t, teacherEmail, "handout.txt", string(bigContent)); err != nil {
		t.Fatalf("seed upload failed: %v", err)
	}

	for _, email := range students {
		navigateTo(t, email, "Khoury")
		navigateTo(t, email, "CS5010")
		navigateTo(t, email, "announcements")
	}

	type downloadResult struct {
		student string
		size    int
		err     error
		code    codes.Code
	}

	// Phase 1: start downloads from all students concurrently
	var wg sync.WaitGroup
	phase1 := make(chan downloadResult, len(students))

	for _, email := range students {
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			content, err := downloadFile(t, email, "handout.txt")
			r := downloadResult{student: email, size: len(content), err: err}
			if err != nil {
				if st, ok := status.FromError(err); ok {
					r.code = st.Code()
				}
			}
			phase1 <- r
		}(email)
	}

	// Small delay then teacher deletes the file
	time.Sleep(20 * time.Millisecond)
	ctx := ctxForUser(teacherEmail)
	_, err := testClient.Delete(ctx, &proto.DeleteRequest{Path: "handout.txt"})
	if err != nil {
		t.Fatalf("teacher delete failed: %v", err)
	}

	wg.Wait()
	close(phase1)

	for r := range phase1 {
		if r.err != nil {
			// NotFound or Internal are acceptable — depends on whether the delete
			// landed before the student's metadata lookup or S3 GetObject
			if r.code == codes.NotFound || r.code == codes.Internal {
				t.Logf("student %s: got %s during race (acceptable)", r.student, r.code)
			} else {
				t.Errorf("student %s: unexpected error: %v", r.student, r.err)
			}
		} else {
			// If download succeeded, must have gotten the complete file
			if r.size != len(bigContent) {
				t.Errorf("student %s: got %d bytes, want %d (incomplete download)",
					r.student, r.size, len(bigContent))
			} else {
				t.Logf("student %s: completed full download (%d bytes)", r.student, r.size)
			}
		}
	}

	// Phase 2: all downloads after delete should fail with NotFound
	phase2Errs := make(chan downloadResult, len(students))
	for _, email := range students {
		content, err := downloadFile(t, email, "handout.txt")
		r := downloadResult{student: email, size: len(content), err: err}
		if err != nil {
			if st, ok := status.FromError(err); ok {
				r.code = st.Code()
			}
		}
		phase2Errs <- r
	}
	close(phase2Errs)

	for r := range phase2Errs {
		if r.err == nil {
			t.Errorf("student %s: expected error after delete, got %d bytes", r.student, r.size)
		} else if r.code != codes.NotFound {
			t.Errorf("student %s: expected NotFound after delete, got %s: %v", r.student, r.code, r.err)
		}
	}
}

// ─────────────────────────────────────────────────────
// Test: teacher deletes folder while students download files inside it
// ─────────────────────────────────────────────────────
func TestDeleteFolderDuringDownload(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	// Reset all users to root first
	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}
	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	navigateTo(t, teacherEmail, "announcements")

	// Seed multiple files in the folder
	for i := 0; i < 3; i++ {
		filename := fmt.Sprintf("notes_%d.txt", i)
		content := fmt.Sprintf("content for notes %d", i)
		if err := uploadFile(t, teacherEmail, filename, content); err != nil {
			t.Fatalf("seed upload %s failed: %v", filename, err)
		}
	}

	for _, email := range students {
		navigateTo(t, email, "Khoury")
		navigateTo(t, email, "CS5010")
		navigateTo(t, email, "announcements")
	}

	type downloadResult struct {
		student  string
		filename string
		size     int
		err      error
		code     codes.Code
	}

	var wg sync.WaitGroup
	results := make(chan downloadResult, len(students)*3)

	// Each student downloads all 3 files concurrently
	for _, email := range students {
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(email string, fileIdx int) {
				defer wg.Done()
				filename := fmt.Sprintf("notes_%d.txt", fileIdx)
				content, err := downloadFile(t, email, filename)
				r := downloadResult{student: email, filename: filename, size: len(content), err: err}
				if err != nil {
					if st, ok := status.FromError(err); ok {
						r.code = st.Code()
					}
				}
				results <- r
			}(email, i)
		}
	}

	// Teacher deletes the entire folder while downloads are in flight
	time.Sleep(10 * time.Millisecond)
	// Navigate teacher back to class root to delete the folder
	navigateTo(t, teacherEmail, "..")
	ctx := ctxForUser(teacherEmail)
	_, err := testClient.Delete(ctx, &proto.DeleteRequest{Path: "announcements/"})
	if err != nil {
		t.Fatalf("teacher folder delete failed: %v", err)
	}

	wg.Wait()
	close(results)

	successCount := 0
	failCount := 0
	for r := range results {
		if r.err != nil {
			if r.code == codes.NotFound || r.code == codes.Internal {
				failCount++
				t.Logf("student %s file %s: %s during race (acceptable)", r.student, r.filename, r.code)
			} else {
				t.Errorf("student %s file %s: unexpected error: %v", r.student, r.filename, r.err)
			}
		} else {
			if r.size == 0 {
				t.Errorf("student %s file %s: success but 0 bytes", r.student, r.filename)
			} else {
				successCount++
			}
		}
	}
	t.Logf("results: %d successful, %d expected failures", successCount, failCount)

	// After delete, all downloads from this folder should fail
	for _, email := range students {
		for i := 0; i < 3; i++ {
			filename := fmt.Sprintf("notes_%d.txt", i)
			_, err := downloadFile(t, email, filename)
			if err == nil {
				t.Errorf("student %s: expected error downloading %s after folder delete", email, filename)
			}
		}
	}
}

// ─────────────────────────────────────────────────────
// Test: student can't upload to another student's folder
// ─────────────────────────────────────────────────────
func TestStudentPermissions(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}
	student1 := students[0]
	student2 := students[1]

	user2 := getUser(t, student2)
	folder2 := user2.Colleges["Khoury"].Classes["CS5010"].Folders[0]

	navigateTo(t, student1, "Khoury")
	navigateTo(t, student1, "CS5010")

	ctx := ctxForUser(student1)
	_, err := testClient.ChangeDirectory(ctx, &proto.ChangeDirectoryRequest{Folder: folder2})
	if err == nil {
		t.Error("student1 should not be able to cd into student2's folder")
	}
}
