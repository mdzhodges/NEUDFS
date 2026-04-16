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
	testEnv := os.Getenv("TEST_ENV")
	if testEnv == "aws" || testEnv == "aws-remote" {
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

// downloadFileWithCode is like downloadFile, but also captures the gRPC status code.
func downloadFileWithCode(t *testing.T, email, filename string) (string, codes.Code, error) {
	t.Helper()
	ctx := ctxForUser(email)
	stream, err := testClient.Download(ctx, &proto.DownloadRequest{Name: filename})
	if err != nil {
		if st, ok := status.FromError(err); ok {
			return "", st.Code(), err
		}
		return "", codes.Unknown, err
	}

	var data []byte
	for {
		res, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			if st, ok := status.FromError(err); ok {
				return string(data), st.Code(), err
			}
			return string(data), codes.Unknown, err
		}
		data = append(data, res.Data...)
	}
	return string(data), codes.OK, nil
}

func setupTest(t *testing.T) func() {
	if os.Getenv("TEST_ENV") == "aws-remote" {
		// Connect to real Fargate server through NLB
		addr := os.Getenv("NLB_ADDR")
		if addr == "" {
			t.Fatal("NLB_ADDR env var required for aws-remote tests")
		}
		conn, err := grpc.DialContext(context.Background(), addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatal(err)
		}
		testClient = proto.NewServerClient(conn)
		testConn = conn

		return func() {
			conn.Close()
		}
	}

	// Existing local bufconn setup
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
// Test: concurrent uploads followed by concurrent deletes
// This verifies the file lifecycle for many students, not just access control.
// ─────────────────────────────────────────────────────
func TestConcurrentUploadsAndDeletes(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}

	type uploadResult struct {
		email    string
		filename string
		content  string
		err      error
	}

	uploadResults := make(chan uploadResult, len(students))
	var wg sync.WaitGroup

	for i, email := range students {
		user := getUser(t, email)
		folder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]
		navigateTo(t, email, "Khoury")
		navigateTo(t, email, "CS5010")
		navigateTo(t, email, folder)

		wg.Add(1)
		go func(i int, email, folder string) {
			defer wg.Done()
			filename := fmt.Sprintf("hw_delete_%d.txt", i)
			content := fmt.Sprintf("homework to delete from student %d", i)
			err := uploadFile(t, email, filename, content)
			uploadResults <- uploadResult{
				email:    email,
				filename: filename,
				content:  content,
				err:      err,
			}
		}(i, email, folder)
	}

	wg.Wait()
	close(uploadResults)

	var uploaded []uploadResult
	for r := range uploadResults {
		if r.err != nil {
			t.Errorf("%s upload failed: %v", r.email, r.err)
			continue
		}
		uploaded = append(uploaded, r)
	}

	for _, r := range uploaded {
		content, err := downloadFile(t, r.email, r.filename)
		if err != nil {
			t.Errorf("%s download after upload failed: %v", r.email, err)
			continue
		}
		if content != r.content {
			t.Errorf("%s download mismatch: got %q want %q", r.email, content, r.content)
		}
	}

	deleteResults := make(chan error, len(uploaded))
	wg = sync.WaitGroup{}
	for _, r := range uploaded {
		resetUserDirectory(t, r.email)
		user := getUser(t, r.email)
		folder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]
		navigateTo(t, r.email, "Khoury")
		navigateTo(t, r.email, "CS5010")
		navigateTo(t, r.email, folder)

		wg.Add(1)
		go func(email, filename string) {
			defer wg.Done()
			ctx := ctxForUser(email)
			_, err := testClient.Delete(ctx, &proto.DeleteRequest{Path: filename})
			deleteResults <- err
		}(r.email, r.filename)
	}

	wg.Wait()
	close(deleteResults)

	for err := range deleteResults {
		if err != nil {
			t.Errorf("delete failed: %v", err)
		}
	}

	for _, r := range uploaded {
		_, code, err := downloadFileWithCode(t, r.email, r.filename)
		if err == nil {
			t.Errorf("%s: expected file %s to be gone after delete", r.email, r.filename)
			continue
		}
		if code != codes.NotFound {
			t.Errorf("%s: expected NotFound after delete, got %s: %v", r.email, code, err)
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
		code    codes.Code
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

	// Students download concurrently.
	for _, email := range students {
		navigateTo(t, email, "Khoury")
		navigateTo(t, email, "CS5010")
		navigateTo(t, email, "announcements")

		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				content, code, err := downloadFileWithCode(t, email, "lecture.txt")
				results <- downloadResult{student: email, content: content, err: err, code: code}
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

	// Phase 1: start downloads from all students concurrently.
	var wg sync.WaitGroup
	phase1 := make(chan downloadResult, len(students))
	started := make(chan struct{}, len(students))

	for _, email := range students {
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			ctx := ctxForUser(email)
			stream, err := testClient.Download(ctx, &proto.DownloadRequest{Name: "handout.txt"})
			if err != nil {
				r := downloadResult{student: email, err: err}
				if st, ok := status.FromError(err); ok {
					r.code = st.Code()
				}
				started <- struct{}{}
				phase1 <- r
				return
			}
			started <- struct{}{}

			var data []byte
			for {
				res, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					r := downloadResult{student: email, size: len(data), err: err}
					if st, ok := status.FromError(err); ok {
						r.code = st.Code()
					}
					phase1 <- r
					return
				}
				data = append(data, res.Data...)
			}

			r := downloadResult{student: email, size: len(data)}
			phase1 <- r
		}(email)
	}

	for i := 0; i < len(students); i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for downloads to start")
		}
	}

	ctx := ctxForUser(teacherEmail)
	if _, deleteErr := testClient.Delete(ctx, &proto.DeleteRequest{Path: "handout.txt"}); deleteErr != nil {
		t.Fatalf("teacher delete failed: %v", deleteErr)
	}

	wg.Wait()
	close(phase1)

	for r := range phase1 {
		if r.err != nil {
			// If the delete won the race, the request should fail with NotFound.
			// Anything else points to a transport or consistency bug.
			if r.code == codes.NotFound {
				t.Logf("student %s: got NotFound during race (acceptable)", r.student)
				continue
			}
			t.Errorf("student %s: unexpected race error: %v", r.student, r.err)
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

	// Create a temporary folder for this test so we don't destroy the shared "announcements" folder
	// that other tests depend on.
	ctx := ctxForUser(teacherEmail)
	if _, err := testClient.MakeDirectory(ctx, &proto.MakeDirectoryRequest{Name: "temp_delete_test"}); err != nil {
		t.Fatalf("failed to create temp_delete_test folder: %v", err)
	}
	navigateTo(t, teacherEmail, "temp_delete_test")

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
		navigateTo(t, email, "temp_delete_test")
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
	started := make(chan struct{}, len(students)*3)

	// Each student downloads all 3 files concurrently
	for _, email := range students {
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(email string, fileIdx int) {
				defer wg.Done()
				filename := fmt.Sprintf("notes_%d.txt", fileIdx)
				ctx := ctxForUser(email)
				stream, err := testClient.Download(ctx, &proto.DownloadRequest{Name: filename})
				if err != nil {
					r := downloadResult{student: email, filename: filename, err: err}
					if st, ok := status.FromError(err); ok {
						r.code = st.Code()
					}
					started <- struct{}{}
					results <- r
					return
				}
				started <- struct{}{}

				var data []byte
				for {
					res, err := stream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						r := downloadResult{student: email, filename: filename, size: len(data), err: err}
						if st, ok := status.FromError(err); ok {
							r.code = st.Code()
						}
						results <- r
						return
					}
					data = append(data, res.Data...)
				}
				results <- downloadResult{student: email, filename: filename, size: len(data)}
			}(email, i)
		}
	}

	for i := 0; i < len(students)*3; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for folder downloads to start")
		}
	}

	// Teacher deletes the entire folder while downloads are in flight.
	// Navigate teacher back to class root to delete the folder.
	navigateTo(t, teacherEmail, "..")
	if _, err := testClient.Delete(ctx, &proto.DeleteRequest{Path: "temp_delete_test/"}); err != nil {
		t.Fatalf("teacher folder delete failed: %v", err)
	}

	wg.Wait()
	close(results)

	successCount := 0
	failCount := 0
	for r := range results {
		if r.err != nil {
			if r.code == codes.NotFound {
				failCount++
				t.Logf("student %s file %s: NotFound during race (acceptable)", r.student, r.filename)
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
func TestStudentCdPermissions(t *testing.T) {
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

func TestStudentPermissions(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}
	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)

	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	if err := uploadFile(t, teacherEmail, "class_lecture.txt", "version 1"); err != nil {
		t.Fatalf("seed upload failed: %v", err)
	}
	navigateTo(t, teacherEmail, "announcements")

	if err := uploadFile(t, teacherEmail, "shared_lecture.txt", "version 1"); err != nil {
		t.Fatalf("seed upload failed: %v", err)
	}
	student1 := students[0]
	user := getUser(t, student1)
	folder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]
	navigateTo(t, student1, "Khoury")
	navigateTo(t, student1, "CS5010")
	ctx := ctxForUser(student1)
	_, err := testClient.RenameDirectory(ctx, &proto.RenameRequest{Entry: "announcements", Name: "unauth_announcements"})
	if err == nil {
		t.Error("student should not be able to rename a directory at the class level")
	}
	_, err = testClient.MakeDirectory(ctx, &proto.MakeDirectoryRequest{Name: "unauth_class_folder"})
	if err == nil {
		t.Error("student should not be able to create a directory at class level")
	}
	_, err = testClient.Rename(ctx, &proto.RenameRequest{Entry: "class_lecture.txt", Name: "unauth_lecture.txt"})
	if err == nil {
		t.Error("student should not be able to rename file in class directory")
	}
	navigateTo(t, student1, "announcements")
	_, err = testClient.Rename(ctx, &proto.RenameRequest{Entry: "shared_lecture.txt", Name: "unauth_lecture.txt"})
	if err == nil {
		t.Error("student should not be able to rename a file in the shared folder")
	}
	_, err = testClient.MakeDirectory(ctx, &proto.MakeDirectoryRequest{Name: "unauth_shared_class_folder"})
	if err == nil {
		t.Error("student should not be able to create a directory at class shared folder level")
	}
	// student cannot delete files in shared folder
	_, err = testClient.Delete(ctx, &proto.DeleteRequest{Path: "shared_lecture.txt"})
	if err == nil {
		t.Error("student should not be able to delete a file in the shared folder")
	}

	// student cannot upload to shared folder
	if err = uploadFile(t, student1, "unauth_shared_upload.txt", "bad"); err == nil {
		t.Error("student should not be able to upload to the shared folder")
	}

	navigateTo(t, student1, "..")

	// student cannot delete files at class level
	_, err = testClient.Delete(ctx, &proto.DeleteRequest{Path: "class_lecture.txt"})
	if err == nil {
		t.Error("student should not be able to delete a file at the class level")
	}

	// student cannot upload to class root
	if err = uploadFile(t, student1, "unauth_class_upload.txt", "bad"); err == nil {
		t.Error("student should not be able to upload to the class root")
	}

	// student cannot rename another student's personal folder
	if len(students) > 1 {
		user2 := getUser(t, students[1])
		folder2 := user2.Colleges["Khoury"].Classes["CS5010"].Folders[0]
		_, err = testClient.RenameDirectory(ctx, &proto.RenameRequest{Entry: folder2, Name: "unauth_steal_folder"})
		if err == nil {
			t.Error("student should not be able to rename another student's personal folder")
		}
	}

	navigateTo(t, student1, folder)

	// student can upload, rename, and delete their own file
	if err = uploadFile(t, student1, "my_file.txt", "my content"); err != nil {
		t.Errorf("student should be able to upload to their own folder: %v", err)
	}
	_, err = testClient.Rename(ctx, &proto.RenameRequest{Entry: "my_file.txt", Name: "my_renamed_file.txt"})
	if err != nil {
		t.Errorf("student should be able to rename their own file: %v", err)
	}
	_, err = testClient.Delete(ctx, &proto.DeleteRequest{Path: "my_renamed_file.txt"})
	if err != nil {
		t.Errorf("student should be able to delete their own file: %v", err)
	}

	_, err = testClient.MakeDirectory(ctx, &proto.MakeDirectoryRequest{Name: "personal_folder"})
	if err != nil {
		t.Errorf("student should be able to create a folder in their personal directory: %v", err)
	}
	_, err = testClient.RenameDirectory(ctx, &proto.RenameRequest{Entry: "personal_folder", Name: "rename_personal_folder"})
	if err != nil {
		t.Errorf("student should be able to rename a folder in their personal directory: %v", err)
	}
}

// ─────────────────────────────────────────────────────
// Test: CurrentDirectory reflects session state correctly
// after navigating through the hierarchy
// ─────────────────────────────────────────────────────
func TestCurrentDirectory(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	student := students[0]
	resetUserDirectory(t, student)

	ctx := ctxForUser(student)

	// at root, current directory should be empty or "/"
	resp, err := testClient.CurrentDirectory(ctx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		t.Fatalf("CurrentDirectory at root failed: %v", err)
	}
	t.Logf("root cwd: %q", resp.Directory)

	navigateTo(t, student, "Khoury")
	resp, err = testClient.CurrentDirectory(ctx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		t.Fatalf("CurrentDirectory after cd Khoury failed: %v", err)
	}
	if resp.Directory == "" {
		t.Error("expected non-empty path after navigating to Khoury")
	}
	t.Logf("after cd Khoury: %q", resp.Directory)

	navigateTo(t, student, "CS5010")
	resp, err = testClient.CurrentDirectory(ctx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		t.Fatalf("CurrentDirectory after cd CS5010 failed: %v", err)
	}
	t.Logf("after cd CS5010: %q", resp.Directory)

	user := getUser(t, student)
	folder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]
	navigateTo(t, student, folder)
	resp, err = testClient.CurrentDirectory(ctx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		t.Fatalf("CurrentDirectory after cd personal folder failed: %v", err)
	}
	t.Logf("after cd personal folder: %q", resp.Directory)

	// navigate back to root and verify state resets
	resetUserDirectory(t, student)
	resp, err = testClient.CurrentDirectory(ctx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		t.Fatalf("CurrentDirectory after reset failed: %v", err)
	}
	t.Logf("after reset: %q", resp.Directory)

	// two users navigating independently should not affect each other's cwd
	teacherCtx := ctxForUser(teacherEmail)
	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	navigateTo(t, teacherEmail, "announcements")

	studentResp, err := testClient.CurrentDirectory(ctx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		t.Fatalf("student CurrentDirectory failed after teacher navigation: %v", err)
	}
	teacherResp, err := testClient.CurrentDirectory(teacherCtx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		t.Fatalf("teacher CurrentDirectory failed: %v", err)
	}
	if studentResp.Directory == teacherResp.Directory {
		t.Errorf("student and teacher cwd should differ: both got %q", studentResp.Directory)
	}
}

// ─────────────────────────────────────────────────────
// Test: rename and delete race on the same file
// One must win cleanly; no corruption or deadlock
// ─────────────────────────────────────────────────────
func TestConcurrentRenameDelete(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)

	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	navigateTo(t, teacherEmail, "announcements")

	if err := uploadFile(t, teacherEmail, "race_target.txt", "content"); err != nil {
		t.Fatalf("seed upload failed: %v", err)
	}

	ctx := ctxForUser(teacherEmail)

	var wg sync.WaitGroup
	renameErr := make(chan error, 1)
	deleteErr := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := testClient.Rename(ctx, &proto.RenameRequest{Entry: "race_target.txt", Name: "race_renamed.txt"})
		renameErr <- err
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := testClient.Delete(ctx, &proto.DeleteRequest{Path: "race_target.txt"})
		deleteErr <- err
	}()

	wg.Wait()
	close(renameErr)
	close(deleteErr)

	rErr := <-renameErr
	dErr := <-deleteErr

	// exactly one should succeed; the other gets a not-found or conflict error
	if rErr == nil && dErr == nil {
		t.Error("rename and delete both succeeded on the same file — one should have lost the race")
	}
	if rErr != nil && dErr != nil {
		t.Errorf("both rename and delete failed: rename=%v delete=%v", rErr, dErr)
	}

	t.Logf("rename err: %v | delete err: %v", rErr, dErr)

	// cleanup whichever name survived
	testClient.Delete(ctx, &proto.DeleteRequest{Path: "race_target.txt"})
	testClient.Delete(ctx, &proto.DeleteRequest{Path: "race_renamed.txt"})
}

// ─────────────────────────────────────────────────────
// Test: professor can perform all privileged operations
// ─────────────────────────────────────────────────────
func TestProfessorPermissions(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	teacherEmail := getProfessorEmail(t, "CS5010")
	resetUserDirectory(t, teacherEmail)
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		resetUserDirectory(t, email)
	}

	ctx := ctxForUser(teacherEmail)

	// professor can upload to class root
	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	if err := uploadFile(t, teacherEmail, "prof_class_file.txt", "class content"); err != nil {
		t.Errorf("professor should be able to upload to class root: %v", err)
	}

	// professor can rename a file at class root
	_, err := testClient.Rename(ctx, &proto.RenameRequest{Entry: "prof_class_file.txt", Name: "prof_class_file_renamed.txt"})
	if err != nil {
		t.Errorf("professor should be able to rename a file at class root: %v", err)
	}

	// professor can delete a file at class root
	_, err = testClient.Delete(ctx, &proto.DeleteRequest{Path: "prof_class_file_renamed.txt"})
	if err != nil {
		t.Errorf("professor should be able to delete a file at class root: %v", err)
	}

	// professor can create and rename a directory at class root
	_, err = testClient.MakeDirectory(ctx, &proto.MakeDirectoryRequest{Name: "prof_shared_dir"})
	if err != nil {
		t.Errorf("professor should be able to mkdir at class root: %v", err)
	}
	_, err = testClient.RenameDirectory(ctx, &proto.RenameRequest{Entry: "prof_shared_dir", Name: "prof_shared_dir_renamed"})
	if err != nil {
		t.Errorf("professor should be able to rename a directory at class root: %v", err)
	}
	_, err = testClient.Delete(ctx, &proto.DeleteRequest{Path: "prof_shared_dir_renamed"})
	if err != nil {
		t.Errorf("professor should be able to delete a directory at class root: %v", err)
	}

	// professor can upload and delete in shared folder
	navigateTo(t, teacherEmail, "announcements")
	if err = uploadFile(t, teacherEmail, "prof_shared_file.txt", "shared content"); err != nil {
		t.Errorf("professor should be able to upload to shared folder: %v", err)
	}
	_, err = testClient.Delete(ctx, &proto.DeleteRequest{Path: "prof_shared_file.txt"})
	if err != nil {
		t.Errorf("professor should be able to delete from shared folder: %v", err)
	}

	// professor can upload, rename, and delete files in a student's personal folder
	if len(students) > 0 {
		student := students[0]
		user := getUser(t, student)
		studentFolder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]

		navigateTo(t, teacherEmail, "..")
		navigateTo(t, teacherEmail, studentFolder)

		if err = uploadFile(t, teacherEmail, "prof_in_student_folder.txt", "content"); err != nil {
			t.Errorf("professor should be able to upload into a student folder: %v", err)
		}
		_, err = testClient.Rename(ctx, &proto.RenameRequest{Entry: "prof_in_student_folder.txt", Name: "prof_in_student_folder_renamed.txt"})
		if err != nil {
			t.Errorf("professor should be able to rename a file in a student folder: %v", err)
		}
		_, err = testClient.Delete(ctx, &proto.DeleteRequest{Path: "prof_in_student_folder_renamed.txt"})
		if err != nil {
			t.Errorf("professor should be able to delete a file in a student folder: %v", err)
		}
	}
}
