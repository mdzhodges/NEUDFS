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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
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
	errDB              = status.Errorf(codes.Internal, "internal db server error")
)

// This will log to our metrics in Cloudwatch
func logger(format string, a ...any) {
	fmt.Printf("LOG:\t"+format+"\n", a...)
}

type Metadata struct {
	PK           string    `dynamodbav:"pk"`
	SK           string    `dynamodbav:"sk"`
	Name         string    `dynamodbav:"name"`
	Owner        string    `dynamodbav:"owner"`
	LastModified time.Time `dynamodbav:"last_modified"`
	Type         string    `dynamodbav:"type"`
	FullPath     string    `dynamodbav:"full_path"`
	S3Url        string    `dynamodbav:"s3_url"`
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
	if cd == "" {
		if _, ok := user.Classrooms[in.Folder]; ok {
			s.currentDirectory[email] = in.Folder + "/"
			msg := fmt.Sprintf("Changing Current Directory to %q\n", in.Folder+"/")
			return &proto.ChangeDirectoryResponse{Message: msg}, nil
		}
		return nil, errInvalidPath
	}
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
	var res []string
	cd := s.currentDirectory[email]
	if cd == "" {
		for className := range user.Classrooms {
			res = append(res, className+"/")
		}
		return &proto.ListDirectoryResponse{Entries: res}, nil
	}
	parts := strings.Split(cd, "/")
	className := parts[0]
	pathWithinClass := strings.TrimPrefix(cd, className+"/")
	var results *dynamodb.QueryOutput
	var err error
	if pathWithinClass == "" {
		results, err = s.DB.Query(context.TODO(), &dynamodb.QueryInput{
			TableName:              aws.String("classroom_metadata"),
			KeyConditionExpression: aws.String("pk = :pk"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": &types.AttributeValueMemberS{Value: className},
			},
		})
	} else {
		results, err = s.DB.Query(context.TODO(), &dynamodb.QueryInput{
			TableName:              aws.String("classroom_metadata"),
			KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
			FilterExpression:       aws.String("owner = :owner"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk":     &types.AttributeValueMemberS{Value: className},
				":prefix": &types.AttributeValueMemberS{Value: pathWithinClass},
				":owner":  &types.AttributeValueMemberS{Value: user.Email},
			},
		})
	}
	var entries []Metadata
	if err != nil {
		logger("Unable to query DB", err)
		return nil, errDB
	}
	if len(results.Items) != 0 {
		err = attributevalue.UnmarshalListOfMaps(results.Items, &entries)
		if err != nil {
			logger("Unable to marshal user data", err)
			return nil, err
		}
		for _, entry := range entries {
			remaining := strings.TrimPrefix(entry.SK, pathWithinClass)
			remaining = strings.TrimSuffix(remaining, "/")
			if remaining == "" {
				continue
			}
			if !strings.Contains(remaining, "/") {
				if entry.Type == "folder" {
					res = append(res, entry.Name+"/")
				} else {
					res = append(res, entry.Name)
				}
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
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatalf("Critical error: Could not connect to AWS: %v", err)
	}
	//sets up dynamodb
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")
	dbClient := dynamodb.NewFromConfig(cfg)
	if endpoint != "" {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("fake", "fake", "fake")),
		)
		dbClient = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	} else {
		dbClient = dynamodb.NewFromConfig(cfg)
	}
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
