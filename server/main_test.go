package main

import (
	"context"
	"fmt"
	"grpc-server/proto"
	"io"
	"log"
	"net"
	"os"
	"strings"
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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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
	// Setup DB and S3 clients pointing to local
	cfg, err := config.LoadDefaultConfig(context.TODO(),
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

func resetToRoot(t *testing.T, email string) {
	t.Helper()
	_, err := dbClient.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName: aws.String("user"),
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
		UpdateExpression: aws.String("SET currentDirectory = :empty"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":empty": &types.AttributeValueMemberS{Value: ""},
		},
	})
	if err != nil {
		t.Fatalf("failed to reset directory for %s: %v", email, err)
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
func uploadFile(t *testing.T, email, filename, content string) {
	t.Helper()
	ctx := ctxForUser(email)
	stream, err := testClient.Upload(ctx)
	if err != nil {
		t.Fatalf("failed to create upload stream: %v", err)
	}
	stream.Send(&proto.UploadRequest{
		Request: &proto.UploadRequest_Metadata{
			Metadata: &proto.UploadMetadata{
				Name:        filename,
				ContentType: "text/plain",
			},
		},
	})
	stream.Send(&proto.UploadRequest{
		Request: &proto.UploadRequest_Chunk{
			Chunk: []byte(content),
		},
	})
	_, err = stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
}

func downloadFile(t *testing.T, email, filename string) string {
	t.Helper()
	ctx := ctxForUser(email)
	stream, err := testClient.Download(ctx, &proto.DownloadRequest{Name: filename})
	if err != nil {
		t.Fatalf("failed to start download: %v", err)
	}
	var data []byte
	for {
		res, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("download failed: %v", err)
		}
		data = append(data, res.Data...)
	}
	return string(data)
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

// Test: concurrent uploads from all students
func TestConcurrentUploads(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	students := getStudentEmails(t, "CS5010")
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
			ctx := ctxForUser(email)
			stream, err := testClient.Upload(ctx)
			if err != nil {
				errs <- fmt.Errorf("student %d: %w", i, err)
				return
			}
			stream.Send(&proto.UploadRequest{
				Request: &proto.UploadRequest_Metadata{
					Metadata: &proto.UploadMetadata{
						Name:        fmt.Sprintf("hw_%d.txt", i),
						ContentType: "text/plain",
					},
				},
			})
			stream.Send(&proto.UploadRequest{
				Request: &proto.UploadRequest_Chunk{
					Chunk: []byte(fmt.Sprintf("homework from student %d", i)),
				},
			})
			_, err = stream.CloseAndRecv()
			if err != nil {
				errs <- fmt.Errorf("student %d: %w", i, err)
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
		content := downloadFile(t, email, fmt.Sprintf("hw_%d.txt", i))
		expected := fmt.Sprintf("homework from student %d", i)
		if content != expected {
			t.Errorf("student %d: got %q, want %q", i, content, expected)
		}
	}
}

// Test: teacher updates file while students download
func TestReadWhileWriting(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	teacherEmail := getProfessorEmail(t, "CS5010")
	navigateTo(t, teacherEmail, "Khoury")
	navigateTo(t, teacherEmail, "CS5010")
	navigateTo(t, teacherEmail, "announcements")

	uploadFile(t, teacherEmail, "lecture.txt", "version 1")

	validVersions := map[string]bool{
		"version 1": true, "version 2": true,
		"version 3": true, "version 4": true,
		"version 5": true,
	}

	var wg sync.WaitGroup
	results := make(chan string, 200)

	// Teacher keeps updating
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 2; i <= 5; i++ {
			uploadFile(t, teacherEmail, "lecture.txt", fmt.Sprintf("version %d", i))
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Students download concurrently
	students := getStudentEmails(t, "CS5010")
	for _, email := range students {
		navigateTo(t, email, "Khoury")
		navigateTo(t, email, "CS5010")
		navigateTo(t, email, "announcements")

		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				content := downloadFile(t, email, "lecture.txt")
				results <- content
				time.Sleep(30 * time.Millisecond)
			}
		}(email)
	}

	wg.Wait()
	close(results)

	for content := range results {
		if !validVersions[content] {
			t.Errorf("got invalid content: %q", content)
		}
	}
}

// Test: teacher can ls a class and see all student folders
func TestTeacherCanSeeStudentFolders(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	profEmail := getProfessorEmail(t, "CS5010")
	resetToRoot(t, profEmail)
	navigateTo(t, profEmail, "Khoury")
	navigateTo(t, profEmail, "CS5010")

	ctx := ctxForUser(profEmail)
	res, err := testClient.ListDirectory(ctx, &proto.ListDirectoryRequest{})
	if err != nil {
		t.Fatalf("teacher ls failed: %v", err)
	}

	students := getStudentEmails(t, "CS5010")
	// Build set of expected student folder names (first name of email)
	seen := make(map[string]bool)
	for _, e := range res.Entries {
		seen[e] = true
	}
	for _, email := range students {
		firstName := strings.Split(email, ".")[0]
		if !seen[firstName+"/"] {
			t.Errorf("teacher should see student folder %s/ but didn't", firstName)
		}
	}
}

// Test: teacher can cd into a student folder
func TestTeacherCanCDIntoStudentFolder(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	profEmail := getProfessorEmail(t, "CS5010")
	resetToRoot(t, profEmail)
	navigateTo(t, profEmail, "Khoury")
	navigateTo(t, profEmail, "CS5010")

	students := getStudentEmails(t, "CS5010")
	user := getUser(t, students[0])
	studentFolder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]

	ctx := ctxForUser(profEmail)
	_, err := testClient.ChangeDirectory(ctx, &proto.ChangeDirectoryRequest{Folder: studentFolder})
	if err != nil {
		t.Errorf("teacher should be able to cd into student folder %s: %v", studentFolder, err)
	}
}

// Test: TA can ls a class and see all student folders
func TestTACanSeeStudentFolders(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	taEmails := getTAsEmails(t, "CS5010")
	taEmail := taEmails[0]
	resetToRoot(t, taEmail)
	navigateTo(t, taEmail, "Khoury")
	navigateTo(t, taEmail, "CS5010")

	ctx := ctxForUser(taEmail)
	res, err := testClient.ListDirectory(ctx, &proto.ListDirectoryRequest{})
	if err != nil {
		t.Fatalf("TA ls failed: %v", err)
	}

	students := getStudentEmails(t, "CS5010")
	seen := make(map[string]bool)
	for _, e := range res.Entries {
		seen[e] = true
	}
	for _, email := range students {
		firstName := strings.Split(email, ".")[0]
		if !seen[firstName+"/"] {
			t.Errorf("TA should see student folder %s/ but didn't", firstName)
		}
	}
}

// Test: TA can cd into a student folder
func TestTACanCDIntoStudentFolder(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	taEmails := getTAsEmails(t, "CS5010")
	taEmail := taEmails[0]
	resetToRoot(t, taEmail)
	navigateTo(t, taEmail, "Khoury")
	navigateTo(t, taEmail, "CS5010")

	students := getStudentEmails(t, "CS5010")
	user := getUser(t, students[0])
	studentFolder := user.Colleges["Khoury"].Classes["CS5010"].Folders[0]

	ctx := ctxForUser(taEmail)
	_, err := testClient.ChangeDirectory(ctx, &proto.ChangeDirectoryRequest{Folder: studentFolder})
	if err != nil {
		t.Errorf("TA should be able to cd into student folder %s: %v", studentFolder, err)
	}
}

// Test: student can't upload to another student's folder
func TestStudentPermissions(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	students := getStudentEmails(t, "CS5010")
	student1 := students[0]
	student2 := students[1]

	// Get student2's folder
	user2 := getUser(t, student2)
	folder2 := user2.Colleges["Khoury"].Classes["CS5010"].Folders[0]

	// Navigate student1 into student2's folder (should fail at cd or upload)
	navigateTo(t, student1, "Khoury")
	navigateTo(t, student1, "CS5010")

	// Try to cd into student2's folder
	ctx := ctxForUser(student1)
	_, err := testClient.ChangeDirectory(ctx, &proto.ChangeDirectoryRequest{Folder: folder2})
	if err == nil {
		t.Error("student1 should not be able to cd into student2's folder")
	}
}
