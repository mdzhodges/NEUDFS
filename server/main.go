package main

import (
	"context"
	"flag"
	"fmt"
	"grpc-server/proto"
	"log"
	"net"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	port               = flag.Int("port", 50051, "the port to serve on")
	errMissingMetadata = status.Errorf(codes.InvalidArgument, "missing metadata")
	errInvalidPath     = status.Errorf(codes.InvalidArgument, "invalid folder path")
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
// User can cd .., cd directoryName or cd fullPath if they have access
func (s *server) ChangeDirectory(ctx context.Context, in *proto.ChangeDirectoryRequest) (*proto.ChangeDirectoryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := ctx.Value("User").(User)
	email := user.Email
	cd := s.currentDirectory[email]
	parts := strings.Split(cd, "/")
	className := parts[0]
	var msg string
	if in.Folder == ".." {
		trimmed := strings.TrimSuffix(cd, "/")
		parentDir := trimmed[:strings.LastIndex(trimmed, "/")+1]
		s.currentDirectory[email] = parentDir
		msg = fmt.Sprintf("Changing Current Directory to %q\n", parentDir)
	} else {
		newCD := cd + in.Folder
		if slices.Contains(user.Classrooms[className].Folders, newCD) {
			msg = fmt.Sprintf("Changing Current Directory to %q\n", newCD)
			s.currentDirectory[email] = newCD
		} else {
			return nil, errInvalidPath
		}
	}
	return &proto.ChangeDirectoryResponse{Message: msg}, nil
}

// List entries in the user's current directory
func (s *server) ListDirectory(ctx context.Context, in *proto.ListDirectoryRequest) (*proto.ListDirectoryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user := ctx.Value("User").(User)
	email := user.Email
	cd := s.currentDirectory[email]
	parts := strings.Split(cd, "/")
	className := parts[0]
	var res []string
	for folder := range user.Classrooms[className].Folders {
		if strings.HasPrefix(user.Classrooms[className].Folders[folder], cd) {
			match := strings.TrimPrefix(user.Classrooms[className].Folders[folder], cd)
			if !strings.Contains(match, "/") && match != "" {
				res = append(res, match)
			}
		}
	}
	return &proto.ListDirectoryResponse{Entries: res}, nil
}

// shorten db calls for grabbing user information
// add user data to context if exists
func unaryInterceptor(db *dynamodb.Client) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, errMissingMetadata
		}
		emails := md["email"]
		email := emails[0]
		result, err := db.GetItem(context.TODO(), &dynamodb.GetItemInput{
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
		ctx = context.WithValue(ctx, "User", foundUser)
		m, err := handler(ctx, req)
		if err != nil {
			logger("RPC failed with error: %v", err)
		}
		return m, err
	}
}

func main() {
	//Grab Port Number
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	region := os.Getenv("AWS_REGION")
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("Critical error: Could not connect to AWS: %v", err)
	}
	//sets up dynamodb
	dbClient := dynamodb.NewFromConfig(cfg)

	//Init Server Object and gRPC server
	s := NewServer(dbClient)
	//add interceptor ie middleware to validate user
	g := grpc.NewServer(grpc.UnaryInterceptor(unaryInterceptor(dbClient)))

	//Register server object into gRPC server
	proto.RegisterServerServer(g, s)
	//Listen on TCP Port 5001
	if err := g.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
