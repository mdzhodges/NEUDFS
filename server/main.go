package main

import (
	"context"
	"fmt"
	"grpc-server/proto"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// This will log to our metrics in Cloudwatch
func logger(format string, a ...any) {
	fmt.Printf("LOG:\t"+format+"\n", a...)
}

type Class struct {
	Role    string   `dynamodbav:"role"`
	Folders []string `dynamodbav:"folders"`
}

type User struct {
	Email      string           `dynamodbav:"email"`
	Role       string           `dynamodbav:"role"`
	Classrooms map[string]Class `dynamodbav:"classrooms"`
}
type server struct {
	proto.UnimplementedServerServer
	DB               *dynamodb.Client
	currentDirectory map[string]string
	mu               sync.RWMutex
}

// Initializes gRPC server
func NewServer(db *dynamodb.Client) *server {
	return &server{DB: db, currentDirectory: make(map[string]string)}
}

// Changes Current Directory of a user
func (s *server) ChangeDirectory(ctx context.Context, in *proto.ChangeDirectoryRequest) (*proto.ChangeDirectoryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email := ctx.Value("email").(string)
	var msg string
	if in.Folder == ".." {
		trimmed := strings.TrimSuffix(in.Folder, "/")
		parentDir := trimmed[:strings.LastIndex(trimmed, "/")+1]
		s.currentDirectory[email] = parentDir
		msg = fmt.Sprintf("Changing Current Directory to %q\n", parentDir)
	} else {
		s.currentDirectory[email] = in.Folder
		msg = fmt.Sprintf("Changing Current Directory to %q\n", in.Folder)
	}
	return &proto.ChangeDirectoryResponse{Message: msg}, nil

}

// List entries in the user's current directory
func (s *server) ListDirectory(ctx context.Context, in *proto.ListDirectoryRequest) (*proto.ListDirectoryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	email := ctx.Value("email").(string)
	cd := s.currentDirectory[email]
	result, err := s.DB.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String("user"),
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
	})
	if err != nil {
		logger("Database error", err)
		return nil, err
	}
	if result.Item == nil {
		logger("User not found", err)
		return nil, err
	}
	var foundUser User
	err = attributevalue.UnmarshalMap(result.Item, &foundUser)
	if err != nil {
		logger("Unable to marshal user data", err)
		return nil, err
	}
}

func main() {
	region := os.Getenv("AWS_REGION")
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("Critical error: Could not connect to AWS: %v", err)
	}
	dbClient := dynamodb.NewFromConfig(cfg)
	s := NewServer(dbClient)
}
