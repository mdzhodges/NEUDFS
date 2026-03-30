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
	errName            = status.Errorf(codes.InvalidArgument, "invalid folder name for mkdir")
	errMkdir           = status.Errorf(codes.Internal, "Unable to create a folder here")
	errAlreadyExists   = status.Errorf(codes.Internal, "Folder already exists")
)

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
	if in.Folder == "" {
		s.currentDirectory[email] = ""
		return &proto.ChangeDirectoryResponse{Message: fmt.Sprintf("Changed directory to root")}, nil
	}
	cd := s.currentDirectory[email]

	// Depth 0: Entering a College
	if cd == "" {
		if _, ok := user.Colleges[in.Folder]; ok {
			s.currentDirectory[email] = in.Folder + "/"
			return &proto.ChangeDirectoryResponse{Message: fmt.Sprintf("Changed directory to %q\n", in.Folder+"/")}, nil
		}
		return nil, errInvalidPath
	}

	parts := strings.Split(cd, "/")
	collegeName := parts[0]
	var msg string

	// Handle going up a directory
	if in.Folder == ".." {
		trimmed := strings.TrimSuffix(cd, "/")
		parentDir := trimmed[:strings.LastIndex(trimmed, "/")+1]
		s.currentDirectory[email] = parentDir
		msg = fmt.Sprintf("Changed directory to %q\n", parentDir)
		return &proto.ChangeDirectoryResponse{Message: msg}, nil
	}

	depth := GetDepth(cd)

	// Depth 1: Entering a Class (e.g. Khoury -> CS101)
	if depth == 1 {
		if _, ok := user.Colleges[collegeName].Classes[in.Folder]; ok {
			newCD := cd + in.Folder + "/"
			s.currentDirectory[email] = newCD
			return &proto.ChangeDirectoryResponse{Message: fmt.Sprintf("Changed directory to %q\n", newCD)}, nil
		}
		return nil, errInvalidPath
	}

	// Depth >= 2: Entering a Subfolder (e.g. CS101 -> bob)
	className := parts[1]
	newCD := cd + in.Folder + "/"
	classInfo, err := s.getClassInfo(className)
	if err != nil {
		logger("Cannot query db for shared folders", err)
		return nil, errDB
	}
	// Calculate the relative path expected by the DB (e.g., "Khoury/CS101/bob/" -> "bob")
	relPath := strings.TrimPrefix(newCD, collegeName+"/"+className+"/")
	relPath = strings.TrimSuffix(relPath, "/")

	if slices.Contains(user.Colleges[collegeName].Classes[className].Folders, relPath) {
		s.currentDirectory[email] = newCD
		msg = fmt.Sprintf("Changed directory to %q\n", newCD)
	} else if slices.Contains(classInfo.SharedFolders, relPath) {
		s.currentDirectory[email] = newCD
		msg = fmt.Sprintf("Changed directory to %q\n", newCD)
		return &proto.ChangeDirectoryResponse{Message: msg}, nil
	} else {
		return nil, errInvalidPath
	}

	return &proto.ChangeDirectoryResponse{Message: msg}, nil
}

func GetDepth(cd string) int {
	if cd == "" {
		return 0
	}
	trimmed := strings.TrimSuffix(cd, "/")
	return len(strings.Split(trimmed, "/"))
}

// LS for student works, next step is encapuslate logic in another func and add logic if professor does ls
// list
func (s *server) ListDirectory(ctx context.Context, in *proto.ListDirectoryRequest) (*proto.ListDirectoryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user := ctx.Value("User").(User)
	email := user.Email
	var res []string
	cd := s.currentDirectory[email]
	depth := GetDepth(cd)

	// Depth 0: Show Colleges
	if depth == 0 || cd == "" {
		for collegeName := range user.Colleges {
			res = append(res, collegeName+"/")
		}
		return &proto.ListDirectoryResponse{Entries: res}, nil
	}

	parts := strings.Split(cd, "/")
	collegeName := parts[0]

	// Depth 1: Show Classes in College
	if depth == 1 {
		for cn := range user.Colleges[collegeName].Classes {
			res = append(res, cn+"/")
		}
		return &proto.ListDirectoryResponse{Entries: res}, nil
	}

	// Depth >= 2: Show Files/Folders in Class
	className := parts[1]
	classInfo, err := s.getClassInfo(className)
	if err != nil {
		logger("Cannot query db for shared folders", err)
		return nil, errDB
	}
	set := make(map[string]bool)
	pathWithinClass := strings.TrimPrefix(cd, collegeName+"/"+className+"/")
	allowedFolders := user.Colleges[collegeName].Classes[className].Folders
	sharedFolders := classInfo.SharedFolders
	allowedFolders = append(allowedFolders, sharedFolders...)
	// 1. Gather Folders from the User Profile
	for _, folder := range allowedFolders {
		folderPath := folder + "/"
		if !strings.HasPrefix(folderPath, pathWithinClass) {
			continue
		}
		remaining := strings.TrimPrefix(folderPath, pathWithinClass)
		if remaining == "" {
			continue
		}

		childParts := strings.Split(strings.TrimSuffix(remaining, "/"), "/")
		if len(childParts) > 0 && childParts[0] != "" {
			set[childParts[0]+"/"] = true
		}
	}

	// 2. Gather Files/Folders from DynamoDB
	keyCondition := "pk = :pk"
	exprValues := map[string]types.AttributeValue{
		":pk": &types.AttributeValueMemberS{Value: className},
	}

	if pathWithinClass != "" {
		keyCondition = "pk = :pk AND begins_with(sk, :prefix)"
		exprValues[":prefix"] = &types.AttributeValueMemberS{Value: pathWithinClass}
	}

	results, err := s.DB.Query(context.TODO(), &dynamodb.QueryInput{
		TableName:                 aws.String("classroom_metadata"),
		KeyConditionExpression:    aws.String(keyCondition),
		ExpressionAttributeValues: exprValues,
	})

	if err != nil {
		logger("Unable to query DB", err)
		return nil, errDB
	}

	var entries []Metadata
	if len(results.Items) != 0 {
		attributevalue.UnmarshalListOfMaps(results.Items, &entries)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.SK, pathWithinClass) {
				continue
			}

			// --- ACCESS CONTROL CHECK ---
			isAllowed := false
			for _, allowed := range allowedFolders {
				allowedPath := allowed
				if !strings.HasSuffix(allowedPath, "/") {
					allowedPath += "/"
				}
				// Allow if entry is inside allowed folder OR allowed folder is inside entry
				if strings.HasPrefix(entry.SK, allowedPath) || strings.HasPrefix(allowedPath, entry.SK) {
					isAllowed = true
					break
				}
			}

			// Skip this entry if the user doesn't have permission and isn't a teacher
			if !isAllowed && user.Role != "teacher" {
				continue
			}
			// --- END ACCESS CONTROL CHECK ---

			remaining := strings.TrimPrefix(entry.SK, pathWithinClass)
			if remaining == "" {
				continue
			}

			childParts := strings.Split(remaining, "/")
			if len(childParts) == 1 {
				// It's a file
				set[childParts[0]] = true
			} else if len(childParts) > 1 {
				// It's a directory
				set[childParts[0]+"/"] = true
			}
		}
	}

	// Convert set back to slice
	for k := range set {
		res = append(res, k)
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
		if len(emails) == 0 {
			return nil, status.Error(codes.Unauthenticated, "no email provided in metadata")
		}
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

		ctx = context.WithValue(ctx, "User", foundUser)
		m, err := handler(ctx, req)
		if err != nil {
			logger("RPC failed with error: %v", err)
		}
		return m, err
	}
}

func (s *server) CurrentDirectory(ctx context.Context, in *proto.CurrentDirectoryRequest) (*proto.CurrentDirectoryResponse, error) {
	user := ctx.Value("User").(User)
	email := user.Email
	cd := s.currentDirectory[email]
	cd = "/" + cd

	return &proto.CurrentDirectoryResponse{Directory: cd}, nil
}

/*
For depth >= 2
Build the new folder path relative to the class (pathWithinClass + newFolder)
Check it doesn't already exist in the user's folders
Add it to the user's Folders list in the user table
Add a metadata entry to classroom_metadata
*/
//DISCLAIMER, THIS IS SLOP I HAVE BEEN WORKING ON PIECING DB CALLS TOGETHER, WILL ADD
//parsePath, getClassInfo, updateStudentFolders, updateSharedFolders and such to parse out logic cause this sucks
func (s *server) MakeDirectory(ctx context.Context, in *proto.MakeDirectoryRequest) (*proto.MakeDirectoryResponse, error) {
	//Grab context data
	user := ctx.Value("User").(User)
	email := user.Email
	cd := s.currentDirectory[email]
	newFolder := in.Name
	if newFolder == "" {
		return nil, errName
	}
	depth := GetDepth(cd)

	if depth == 0 || depth == 1 {
		return nil, errMkdir
	}
	collegeName, className, pathWithinClass := parsePath(cd)
	newFolderPath := pathWithinClass + newFolder

	if depth == 2 {
		if user.Role == "student" {
			return nil, errMkdir
		}
		var classInfo ClassInfo
		classInfo, err := s.getClassInfo(className)
		if err != nil {
			logger("Unable to get class info", err)
			return nil, errDB
		}

		if slices.Contains(classInfo.SharedFolders, newFolderPath) {
			return nil, errAlreadyExists
		}
		if err := s.updateSharedFolders(className, newFolderPath); err != nil {
			logger("Unable to update shared folders", err)
			return nil, errDB
		}
		if err := s.createFolderMetadata(className, newFolderPath+"/", newFolder, email, cd+newFolder+"/"); err != nil {
			logger("Unable to create folder metadata", err)
			return nil, errDB
		}
		return &proto.MakeDirectoryResponse{Message: "Added " + newFolder + " to directory"}, nil

	}
	//if student is trying to build a folder path that doesn't start with their name, reject the request
	if user.Role == "student" {
		studentFolder := email[:strings.Index(email, "@")]
		if !strings.HasPrefix(pathWithinClass, studentFolder+"/") && pathWithinClass != studentFolder {
			return nil, errMkdir
		}
		if slices.Contains(user.Colleges[collegeName].Classes[className].Folders, newFolderPath) {
			return nil, errAlreadyExists
		}
		if err := s.updateUserFolders(email, collegeName, className, newFolderPath); err != nil {
			logger("Unable to update user folders", err)
			return nil, errDB
		}
		if err := s.createFolderMetadata(className, newFolderPath+"/", newFolder, email, cd+newFolder+"/"); err != nil {
			logger("Unable to create folder metadata", err)
			return nil, errDB
		}
		return &proto.MakeDirectoryResponse{Message: "Added " + newFolder + " to directory"}, nil
	}
	classInfo, err := s.getClassInfo(className)
	if err != nil {
		logger("Cannot query db for shared folders", err)
		return nil, errDB
	}

	studentName := strings.Split(pathWithinClass, "/")[0]
	isStudentFolder := false
	for _, studentEmail := range classInfo.Students {
		if studentEmail[:strings.Index(studentEmail, "@")] == studentName {
			isStudentFolder = true
			foundUser, err := s.getUser(studentEmail)
			if err != nil {
				logger("Unable to get student user", err)
				return nil, errDB
			}
			if slices.Contains(foundUser.Colleges[collegeName].Classes[className].Folders, newFolderPath) {
				return nil, errAlreadyExists
			}
			if err := s.updateUserFolders(studentEmail, collegeName, className, newFolderPath); err != nil {
				logger("Unable to update user folders", err)
				return nil, errDB
			}
			break
		}
	}
	if !isStudentFolder {
		if slices.Contains(classInfo.SharedFolders, newFolderPath) {
			return nil, errAlreadyExists
		}
		if err := s.updateSharedFolders(className, newFolderPath); err != nil {
			logger("Unable to update shared folders", err)
			return nil, errDB
		}
	}

	if err := s.createFolderMetadata(className, newFolderPath+"/", newFolder, email, cd+newFolder+"/"); err != nil {
		logger("Unable to create folder metadata", err)
		return nil, errDB
	}

	return &proto.MakeDirectoryResponse{Message: "Added " + newFolder + " to directory"}, nil
}

func (s *server) Rename(ctx context.Context, in *proto.RenameRequest) (*proto.RenameResponse, error) {
	// Grab user from your interceptor context
	user := ctx.Value("User").(User)
	email := user.Email

	// Get current directory context
	s.mu.RLock()
	cd := s.currentDirectory[email]
	s.mu.RUnlock()

	depth := GetDepth(cd)
	if depth < 2 {
		return nil, status.Errorf(codes.PermissionDenied, "must be inside a class to rename files")
	}

	parts := strings.Split(cd, "/")
	className := parts[1]

	// Calculate the file's SK based on current directory
	pathWithinClass := strings.TrimPrefix(cd, parts[0]+"/"+className+"/")
	oldSK := pathWithinClass + in.OldName

	// Update DynamoDB using the helper 
	err := s.renameFileMetadata(className, oldSK, in.NewName, cd+in.NewName)
	if err != nil {
		logger("Rename failed: %v", err)
		return nil, errDB
	}

	return &proto.RenameResponse{
		// ensure your proto fields match (e.g., OldName / NewName)
		Message: fmt.Sprintf("Successfully renamed %s to %s", in.OldName, in.NewName),
	}, nil
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
