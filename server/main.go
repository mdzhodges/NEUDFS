package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"grpc-server/proto"
	"io"
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
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	port                    = flag.Int("port", 50051, "the port to serve on")
	errMissingMetadata      = status.Errorf(codes.InvalidArgument, "missing metadata")
	errInvalidPath          = status.Errorf(codes.InvalidArgument, "invalid folder path")
	errDB                   = status.Errorf(codes.Internal, "internal db server error")
	errName                 = status.Errorf(codes.InvalidArgument, "invalid folder name for mkdir")
	errMkdir                = status.Errorf(codes.Internal, "Unable to create a folder here")
	errAlreadyExists        = status.Errorf(codes.Internal, "Folder already exists")
	errFileCannotBeStreamed  = status.Errorf(codes.InvalidArgument, "File cannot be streamed")
	port                     = flag.Int("port", 50051, "the port to serve on")
	errMissingMetadata       = status.Errorf(codes.InvalidArgument, "missing metadata")
	errInvalidPath           = status.Errorf(codes.InvalidArgument, "invalid folder path")
	errDB                    = status.Errorf(codes.Internal, "internal db server error")
	errName                  = status.Errorf(codes.InvalidArgument, "invalid folder name for mkdir")
	errMkdir                 = status.Errorf(codes.Internal, "Unable to create a folder here")
	errAlreadyExists         = status.Errorf(codes.Internal, "Folder already exists")
	errFileCannnotBeStreamed = status.Errorf(codes.InvalidArgument, "File cannot be streamed")
)

type server struct {
	proto.UnimplementedServerServer
	DB               *dynamodb.Client
	S3Client         *s3.Client
	currentDirectory map[string]string
	mu               sync.RWMutex
	S3Client         *s3.Client
}

// Initializes gRPC server
func NewServer(db *dynamodb.Client, s3Client *s3.Client) *server {
	return &server{DB: db, S3Client: s3Client, currentDirectory: make(map[string]string)}
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
	} else if user.Role == "teacher" {
		// Teachers can cd into any folder that exists in DynamoDB
		result, err := s.DB.GetItem(context.TODO(), &dynamodb.GetItemInput{
			TableName: aws.String("classroom_metadata"),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: className},
				"sk": &types.AttributeValueMemberS{Value: relPath + "/"},
			},
		})
		if err != nil || result.Item == nil {
			return nil, errInvalidPath
		}
		s.currentDirectory[email] = newCD
		msg = fmt.Sprintf("Changed directory to %q\n", newCD)
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
func streamInterceptor(db *dynamodb.Client) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return errMissingMetadata
		}
		emails := md["email"]
		if len(emails) == 0 {
			return status.Error(codes.Unauthenticated, "no email provided in metadata")
		}
		result, err := db.GetItem(context.TODO(), &dynamodb.GetItemInput{
			TableName: aws.String("user"),
			Key: map[string]types.AttributeValue{
				"email": &types.AttributeValueMemberS{Value: emails[0]},
			},
		})
		if err != nil {
			logger("Database error", err)
			return err
		}
		if result.Item == nil {
			return status.Error(codes.Unauthenticated, "user not found")
		}
		var foundUser User
		err = attributevalue.UnmarshalMap(result.Item, &foundUser)
		if err != nil {
			return err
		}
		wrapped := &wrappedStream{
			ServerStream: ss,
			ctx:          context.WithValue(ctx, "User", foundUser),
		}
		return handler(srv, wrapped)
	}
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
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

func streamInterceptor(db *dynamodb.Client) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return errMissingMetadata
		}
		emails := md["email"]
		if len(emails) == 0 {
			return status.Error(codes.Unauthenticated, "no email provided in metadata")
		}
		result, err := db.GetItem(context.TODO(), &dynamodb.GetItemInput{
			TableName: aws.String("user"),
			Key: map[string]types.AttributeValue{
				"email": &types.AttributeValueMemberS{Value: emails[0]},
			},
		})
		if err != nil {
			logger("Database error", err)
			return err
		}
		if result.Item == nil {
			return status.Error(codes.Unauthenticated, "user not found")
		}
		var foundUser User
		err = attributevalue.UnmarshalMap(result.Item, &foundUser)
		if err != nil {
			return err
		}
		wrapped := &wrappedStream{
			ServerStream: ss,
			ctx:          context.WithValue(ctx, "User", foundUser),
		}
		return handler(srv, wrapped)
	}
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
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

	oldSK := pathWithinClass + in.Entry
	newSK := pathWithinClass + in.Name

	// Update DynamoDB using the helpe
	err := s.renameFileMetadata(className, oldSK, newSK, in.Name, cd+in.Name)
	if err != nil {
		if err.Error() == "file not found" {
			return nil, status.Errorf(codes.NotFound, "file '%s' does not exist or is a directory", in.Entry)
		}
		logger("Rename failed: %v", err)
		return nil, errDB
	}

	return &proto.RenameResponse{
		// Use in.Entry and in.Name
		Message: fmt.Sprintf("Successfully renamed %s to %s", in.Entry, in.Name),
	}, nil
}

// queryClassEntries fetches all metadata items for a class from DynamoDB,
// starting at pathWithinClass, and returns their SKs relative to pathWithinClass
// with entryPrefix prepended. If allowedFolders is non-nil, only entries whose
// SK falls within one of those folders are returned (student access control).
func (s *server) queryClassEntries(className, pathWithinClass, entryPrefix string, allowedFolders []string) ([]string, error) {
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
		return nil, err
	}

	var dbEntries []Metadata
	attributevalue.UnmarshalListOfMaps(results.Items, &dbEntries)

	set := make(map[string]bool)
	var entries []string
	for _, entry := range dbEntries {
		if entry.SK == "class_info" {
			continue
		}
		if allowedFolders != nil {
			allowed := false
			for _, f := range allowedFolders {
				fp := f
				if !strings.HasSuffix(fp, "/") {
					fp += "/"
				}
				if strings.HasPrefix(entry.SK, fp) || strings.HasPrefix(fp, entry.SK) {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		relPath := entryPrefix + strings.TrimPrefix(entry.SK, pathWithinClass)
		if relPath != "" && !set[relPath] {
			set[relPath] = true
			entries = append(entries, relPath)
		}
	}
	return entries, nil
}

// allowedFoldersForClass returns the folders a student may see in a class
// (their own folders + shared folders). Returns nil for teachers (no filter).
func (s *server) allowedFoldersForClass(user User, collegeName, className string) []string {
	if user.Role == "teacher" {
		return nil
	}
	classInfo, err := s.getClassInfo(className)
	allowed := append([]string{}, user.Colleges[collegeName].Classes[className].Folders...)
	if err == nil {
		allowed = append(allowed, classInfo.SharedFolders...)
	}
	return allowed
}

func (s *server) TreeDirectory(ctx context.Context, in *proto.TreeDirectoryRequest) (*proto.TreeDirectoryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user := ctx.Value("User").(User)
	email := user.Email
	cd := s.currentDirectory[email]
	depth := GetDepth(cd)

	var entries []string

	// Depth 0: show everything under each college → class → DynamoDB contents
	if depth == 0 || cd == "" {
		for collegeName, college := range user.Colleges {
			entries = append(entries, collegeName+"/")
			for className := range college.Classes {
				classPrefix := collegeName + "/" + className + "/"
				entries = append(entries, classPrefix)
				classEntries, err := s.queryClassEntries(className, "", classPrefix, s.allowedFoldersForClass(user, collegeName, className))
				if err != nil {
					logger("Unable to query DB for class %s", className)
					return nil, errDB
				}
				entries = append(entries, classEntries...)
			}
		}
		return &proto.TreeDirectoryResponse{Entries: entries}, nil
	}

	parts := strings.Split(cd, "/")
	collegeName := parts[0]

	// Depth 1: show each class and its DynamoDB contents
	if depth == 1 {
		for className := range user.Colleges[collegeName].Classes {
			classPrefix := className + "/"
			entries = append(entries, classPrefix)
			classEntries, err := s.queryClassEntries(className, "", classPrefix, s.allowedFoldersForClass(user, collegeName, className))
			if err != nil {
				logger("Unable to query DB for class %s", className)
				return nil, errDB
			}
			entries = append(entries, classEntries...)
		}
		return &proto.TreeDirectoryResponse{Entries: entries}, nil
	}

	// Depth >= 2: query DynamoDB for all items under the current path
	className := parts[1]
	pathWithinClass := strings.TrimPrefix(cd, collegeName+"/"+className+"/")
	classEntries, err := s.queryClassEntries(className, pathWithinClass, "", s.allowedFoldersForClass(user, collegeName, className))
	if err != nil {
		logger("Unable to query DB", err)
		return nil, errDB
	}
	entries = append(entries, classEntries...)

	return &proto.TreeDirectoryResponse{Entries: entries}, nil
}

func (s *server) RenameDirectory(ctx context.Context, in *proto.RenameRequest) (*proto.RenameResponse, error) {
	// Grab user from your interceptor context
	user := ctx.Value("User").(User)
	email := user.Email

	// Get current directory context
	s.mu.RLock()
	cd := s.currentDirectory[email]
	s.mu.RUnlock()

	depth := GetDepth(cd)
	if depth < 2 {
		return nil, status.Errorf(codes.PermissionDenied, "must be inside a class to rename directories")
	}

	parts := strings.Split(cd, "/")
	className := parts[1]

	// Calculate the exact string prefixes we need to search for in the database
	pathWithinClass := strings.TrimPrefix(cd, parts[0]+"/"+className+"/")

	oldPrefix := pathWithinClass + in.Entry
	newPrefix := pathWithinClass + in.Name

	// Call our new DB helper
	err := s.renameDirectoryMetadata(className, oldPrefix, newPrefix)
	if err != nil {
		if err.Error() == "directory not found" {
			return nil, status.Errorf(codes.NotFound, "directory '%s' does not exist", in.Entry)
		}
		logger("Rename directory failed: %v", err)
		return nil, status.Errorf(codes.Internal, "database error")
	}

	// sync with permissions array
	s.updateFolderLists(email, parts[0], className, oldPrefix, newPrefix)

	return &proto.RenameResponse{
		Message: fmt.Sprintf("Successfully renamed directory %s to %s", in.Entry, in.Name),
	}, nil
}
func (s *server) Download(req *proto.DownloadRequest, stream proto.Server_DownloadServer) error {
	// Grab user from your interceptor context
	user := stream.Context().Value("User").(User)
	email := user.Email
	cd := s.currentDirectory[email]
	cd = strings.TrimSuffix(cd, "/")
	depth := GetDepth(cd)
	if depth < 2 {
		return status.Errorf(codes.PermissionDenied, "must be inside a class to download files")
	}
	s3Key := cd + "/" + req.Name
	result, err := s.DownloadS3File(s3Key)
	if err != nil {
		logger("Failed to download file from S3", err)
		return status.Errorf(codes.Internal, "failed to download file")
	}
	if result == nil {
		logger("S3 download returned nil result", fmt.Errorf("nil result"))
		return status.Errorf(codes.Internal, "failed to download file")
	}
	defer result.Body.Close()
	buf := make([]byte, 64*1024)
	for {
		n, err := result.Body.Read(buf)
		if n > 0 {
			stream.Send(&proto.DownloadResponse{
				Data: buf[:n],
			})
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "error reading file")
		}
	}
	return nil
}

// bidirectional upload func, returns unary response
//Idea HERE:
/*
Read the first message to grab the metadata — filename, content type, and the cd (current directory) the user is in.
Accumulate the chunks as they come in via stream.Recv() in a loop. You can either buffer them all in memory (fine for small files) or pipe them directly to S3 using a multipart upload (better for large files).
Upload to S3 once you have the complete file (or as you stream it). You'll construct the S3 key however you want — maybe something like userID/college/class/folder/filename.
Save the metadata to DynamoDB — the file's path relative to the user's current directory, the S3 key, owner, timestamp, etc. This is where cd comes in — you'd store the file's parent folder as cd so your earlier query logic works.
Return the response via stream.SendAndClose() with whatever info the client needs (success/failure, the file path, etc.).
*/
func (s *server) Upload(stream proto.Server_UploadServer) error {
	// Grab user from your interceptor context
	user := stream.Context().Value("User").(User)
	email := user.Email
	cd := s.currentDirectory[email]
	cd = strings.TrimSuffix(cd, "/")
	depth := GetDepth(cd)
	role := user.Role
	if depth < 2 {
		return status.Errorf(codes.PermissionDenied, "must be inside a class to upload files")
	}
	if role == "student" && depth == 2 {
		return status.Errorf(codes.PermissionDenied, "students must be inside their personal folder to upload files")
	}

	parts := strings.Split(cd, "/")
	collegeName := parts[0]
	className := parts[1]
	pathWithinClass := strings.TrimPrefix(cd, collegeName+"/"+className+"/")
	//Checks for a student if they are trying to upload outside of their personal folder
	if role == "student" {
		personalFolders := user.Colleges[collegeName].Classes[className].Folders
		isAllowed := false
		for _, folder := range personalFolders {
			if strings.HasPrefix(pathWithinClass, folder+"/") || pathWithinClass == folder {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			return status.Errorf(codes.PermissionDenied, "you do not have permission to upload files to this directory")
		}
	}
	req, err := stream.Recv()
	if err != nil {
		logger("Failed to receive upload metadata", err)
		return errFileCannnotBeStreamed
	}
	meta := req.GetMetadata()
	if meta == nil {
		return errFileCannnotBeStreamed
	}
	filename := meta.Name
	contentType := meta.ContentType
	if filename == "" || contentType == "" {
		return errFileCannnotBeStreamed
	}
	var buf bytes.Buffer
	for {
		req, err = stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		buf.Write(req.GetChunk())
	}
	s3Key := cd + "/" + filename
	url, err := s.uploadToS3(buf.Bytes(), s3Key)
	if err != nil {
		logger("Failed to upload file to S3", err)
		return status.Errorf(codes.Internal, "failed to upload file")
	}
	if url == "" {
		logger("S3 upload returned empty URL", fmt.Errorf("empty URL"))
		return status.Errorf(codes.Internal, "failed to upload file")
	}
	err = s.uploadFileMetadata(className, pathWithinClass+"/"+filename, filename, email, cd+"/"+filename, url)
	if err != nil {
		logger("Failed to save file metadata to DynamoDB", err)
		return status.Errorf(codes.Internal, "failed to save file metadata")
	}
	fmt.Println("cd:", cd)
	fmt.Println("pathWithinClass:", pathWithinClass)
	fmt.Println("s3Key:", s3Key)
	return stream.SendAndClose(&proto.UploadResponse{Message: "uploaded"})
}
func main() {
	//Grab Port Number
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	//sets up dynamodb
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")
	endpointS3 := os.Getenv("S3_ENDPOINT")
	isDev := endpoint != "" || endpointS3 != ""
	var cfg aws.Config
	if isDev {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("fake", "fake", "fake")),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	}
	if err != nil {
		log.Fatal(err)
	}

	// DynamoDB client
	dbClient := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	// S3 client
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpointS3 != "" {
			o.BaseEndpoint = aws.String(endpointS3)
			o.UsePathStyle = true
		}
	})
	//Init Server Object and gRPC server
	s := NewServer(dbClient, s3Client)
	//add interceptor ie middleware to validate user
	g := grpc.NewServer(grpc.UnaryInterceptor(unaryInterceptor(dbClient)), grpc.StreamInterceptor(streamInterceptor(dbClient)))

	//Register server object into gRPC server
	proto.RegisterServerServer(g, s)
	//Listen on TCP Port 5001
	if err := g.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
