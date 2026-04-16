package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"grpc-server/proto"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"strings"
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
	"google.golang.org/grpc/keepalive"
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
	errFileCannotBeStreamed = status.Errorf(codes.InvalidArgument, "File cannot be streamed")
	userTable               = envOrDefault("DYNAMODB_USER_TABLE", "user")
	metadataTable           = envOrDefault("DYNAMODB_METADATA_TABLE", "classroom_metadata")
	s3Bucket                = envOrDefault("S3_BUCKET", "neudfs-storage-dev")
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type server struct {
	proto.UnimplementedServerServer
	DB       *dynamodb.Client
	S3Client *s3.Client
}

// Initializes gRPC server
func NewServer(db *dynamodb.Client, s3 *s3.Client) *server {
	return &server{DB: db, S3Client: s3}
}

// Changes Current Directory of a user
// User can cd .., cd directoryName or cd fullPath if they have access
func (s *server) ChangeDirectory(ctx context.Context, in *proto.ChangeDirectoryRequest) (*proto.ChangeDirectoryResponse, error) {
	user := ctx.Value("User").(User)
	email := user.Email
	cd := user.CurrentDirectory
	if in.Folder == "" {
		if err := s.SetCurrentDirectory(ctx, email, cd, ""); err != nil {
			return nil, err
		}
		return &proto.ChangeDirectoryResponse{Message: fmt.Sprintf("Changed directory to root")}, nil
	}
	// Depth 0: Entering a College
	if cd == "" {
		if _, ok := user.Colleges[in.Folder]; ok {
			if err := s.SetCurrentDirectory(ctx, email, cd, in.Folder+"/"); err != nil {
				return nil, err
			}
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
		if err := s.SetCurrentDirectory(ctx, email, cd, parentDir); err != nil {
			return nil, err
		}
		msg = fmt.Sprintf("Changed directory to %q\n", parentDir)
		return &proto.ChangeDirectoryResponse{Message: msg}, nil
	}
	depth := GetDepth(cd)
	// Depth 1: Entering a Class (e.g. Khoury -> CS101)
	if depth == 1 {
		if _, ok := user.Colleges[collegeName].Classes[in.Folder]; ok {
			newCD := cd + in.Folder + "/"
			if err := s.SetCurrentDirectory(ctx, email, cd, newCD); err != nil {
				return nil, err
			}
			return &proto.ChangeDirectoryResponse{Message: fmt.Sprintf("Changed directory to %q\n", newCD)}, nil
		}
		return nil, errInvalidPath
	}

	// Depth >= 2: Entering a Subfolder (e.g. CS101 -> bob)
	className := parts[1]
	newCD := cd + in.Folder + "/"
	classInfo, err := s.getClassInfo(ctx, className)
	if err != nil {
		logger("Cannot query db for shared folders: %v", err)
		return nil, errDB
	}
	// Calculate the relative path expected by the DB (e.g., "Khoury/CS101/bob/" -> "bob")
	relPath := strings.TrimPrefix(newCD, collegeName+"/"+className+"/")
	relPath = strings.TrimSuffix(relPath, "/")

	if slices.Contains(user.Colleges[collegeName].Classes[className].Folders, relPath) ||
		slices.Contains(classInfo.SharedFolders, relPath) {
		if err := s.SetCurrentDirectory(ctx, email, cd, newCD); err != nil {
			return nil, err
		}
		return &proto.ChangeDirectoryResponse{
			Message: fmt.Sprintf("Changed directory to %q\n", newCD),
		}, nil
	}

	// Professors and TAs can navigate to any folder that exists in the class metadata.
	if user.Role == "professor" || user.Role == "TA" {
		folderResult, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(metadataTable),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: className},
				"sk": &types.AttributeValueMemberS{Value: relPath + "/"},
			},
		})
		if err == nil && folderResult.Item != nil {
			if err := s.SetCurrentDirectory(ctx, email, cd, newCD); err != nil {
				return nil, err
			}
			return &proto.ChangeDirectoryResponse{
				Message: fmt.Sprintf("Changed directory to %q\n", newCD),
			}, nil
		}
	}

	return nil, errInvalidPath
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
	user := ctx.Value("User").(User)
	var res []string
	cd := user.CurrentDirectory
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
	classInfo, err := s.getClassInfo(ctx, className)
	if err != nil {
		logger("Cannot query db for shared folders: %v", err)
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
	input := &dynamodb.QueryInput{
		TableName:                 aws.String(metadataTable),
		KeyConditionExpression:    aws.String(keyCondition),
		ExpressionAttributeValues: exprValues,
	}
	results, err := s.queryAllPages(ctx, input)

	if err != nil {
		logger("Unable to query DB: %v", err)
		return nil, errDB
	}

	var entries []Metadata
	if len(results) != 0 {
		attributevalue.UnmarshalListOfMaps(results, &entries)
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
		result, err := db.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(userTable),
			Key: map[string]types.AttributeValue{
				"email": &types.AttributeValueMemberS{Value: emails[0]},
			},
		})
		if err != nil {
			logger("Database error: %v", err)
			return err
		}
		if result.Item == nil {
			return status.Error(codes.Unauthenticated, "user not found")
		}
		var foundUser User
		err = attributevalue.UnmarshalMap(result.Item, &foundUser)
		if foundUser.DirectoryTTL != 0 && time.Now().Unix() > foundUser.DirectoryTTL {
			if foundUser.CurrentDirectory != "" {
				db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
					TableName: aws.String(userTable),
					Key: map[string]types.AttributeValue{
						"email": &types.AttributeValueMemberS{Value: emails[0]},
					},
					UpdateExpression: aws.String("REMOVE currentDirectory, directoryTTL"),
				})
				foundUser.CurrentDirectory = ""
				foundUser.DirectoryTTL = 0
			}
		}
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
		result, err := db.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(userTable),
			Key: map[string]types.AttributeValue{
				"email": &types.AttributeValueMemberS{Value: email},
			},
		})
		if err != nil {
			logger("Database error: %v", err)
			return nil, err
		}
		if result.Item == nil {
			return nil, status.Error(codes.Unauthenticated, "user not found: "+email)
		}
		var foundUser User
		err = attributevalue.UnmarshalMap(result.Item, &foundUser)
		if foundUser.DirectoryTTL != 0 && time.Now().Unix() > foundUser.DirectoryTTL {
			if foundUser.CurrentDirectory != "" {
				db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
					TableName: aws.String(userTable),
					Key: map[string]types.AttributeValue{
						"email": &types.AttributeValueMemberS{Value: email},
					},
					UpdateExpression: aws.String("REMOVE currentDirectory, directoryTTL"),
				})
			}
			foundUser.CurrentDirectory = ""
			foundUser.DirectoryTTL = 0
		}
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
	cd := user.CurrentDirectory
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
	cd := user.CurrentDirectory
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
		classInfo, err := s.getClassInfo(ctx, className)
		if err != nil {
			logger("Unable to get class info: %v", err)
			return nil, errDB
		}

		if slices.Contains(classInfo.SharedFolders, newFolderPath) {
			return nil, errAlreadyExists
		}
		if err := s.updateSharedFolders(className, newFolderPath); err != nil {
			logger("Unable to update shared folders: %v", err)
			return nil, errDB
		}
		if err := s.createFolderMetadata(className, newFolderPath+"/", newFolder, email, cd+newFolder+"/"); err != nil {
			logger("Unable to create folder metadata: %v", err)
			return nil, errDB
		}
		return &proto.MakeDirectoryResponse{Message: "Added " + newFolder + " to directory"}, nil

	}
	// If student is trying to build a folder path outside of their assigned directories, reject it
	if user.Role == "student" {
		isAllowed := false
		userFolders := user.Colleges[collegeName].Classes[className].Folders

		// Look through the folders the student already owns in this class
		for _, ownedFolder := range userFolders {
			// Extract their base root directory ("victor" from "victor/notes")
			baseFolder := strings.Split(ownedFolder, "/")[0]

			// check if their current path falls under their base folder
			if strings.HasPrefix(pathWithinClass, baseFolder+"/") || pathWithinClass == baseFolder {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			return nil, errMkdir
		}
		if slices.Contains(user.Colleges[collegeName].Classes[className].Folders, newFolderPath) {
			return nil, errAlreadyExists
		}
		if err := s.updateUserFolders(email, collegeName, className, newFolderPath); err != nil {
			logger("Unable to update user folders: %v", err)
			return nil, errDB
		}
		if err := s.createFolderMetadata(className, newFolderPath+"/", newFolder, email, cd+newFolder+"/"); err != nil {
			logger("Unable to create folder metadata: %v", err)
			return nil, errDB
		}
		return &proto.MakeDirectoryResponse{Message: "Added " + newFolder + " to directory"}, nil
	}
	classInfo, err := s.getClassInfo(ctx, className)
	if err != nil {
		logger("Cannot query db for shared folders: %v", err)
		return nil, errDB
	}

	studentName := strings.Split(pathWithinClass, "/")[0]
	isStudentFolder := false
	for _, studentEmail := range classInfo.Students {
		if strings.ReplaceAll(strings.Split(studentEmail, "@")[0], ".", "_") == studentName {
			isStudentFolder = true
			foundUser, err := s.getUser(ctx, studentEmail)
			if err != nil {
				logger("Unable to get student user: %v", err)
				return nil, errDB
			}
			if slices.Contains(foundUser.Colleges[collegeName].Classes[className].Folders, newFolderPath) {
				return nil, errAlreadyExists
			}
			if err := s.updateUserFolders(studentEmail, collegeName, className, newFolderPath); err != nil {
				logger("Unable to update user folders: %v", err)
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
			logger("Unable to update shared folders: %v", err)
			return nil, errDB
		}
	}

	if err := s.createFolderMetadata(className, newFolderPath+"/", newFolder, email, cd+newFolder+"/"); err != nil {
		logger("Unable to create folder metadata: %v", err)
		return nil, errDB
	}

	return &proto.MakeDirectoryResponse{Message: "Added " + newFolder + " to directory"}, nil
}

func (s *server) Rename(ctx context.Context, in *proto.RenameRequest) (*proto.RenameResponse, error) {
	// Grab user from your interceptor context
	user := ctx.Value("User").(User)
	// Get current directory context
	cd := user.CurrentDirectory

	depth := GetDepth(cd)
	if depth < 2 {
		return nil, status.Errorf(codes.PermissionDenied, "must be inside a class to rename files")
	}
	if depth == 2 && user.Role == "student" {
		return nil, status.Errorf(codes.PermissionDenied, "must be a teacher to rename a file in class directory")
	}
	parts := strings.Split(cd, "/")
	className := parts[1]
	classInfo, err := s.getClassInfo(ctx, className)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to retrieve classroom info")
	}
	sharedFolders := classInfo.SharedFolders
	// Calculate the file's SK based on current directory
	pathWithinClass := strings.TrimPrefix(cd, parts[0]+"/"+className+"/")
	if user.Role == "student" {
		for _, folder := range sharedFolders {
			if strings.HasPrefix(pathWithinClass, folder+"/") || pathWithinClass == folder+"/" {
				return nil, status.Errorf(codes.PermissionDenied, "must be a teacher to rename a folder in shared directory")
			}
		}
	}
	oldSK := pathWithinClass + in.Entry
	newSK := pathWithinClass + in.Name

	// Update DynamoDB using the helpe
	err = s.renameFileMetadata(ctx, className, oldSK, newSK, in.Name, cd+in.Name)
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
func (s *server) queryClassEntries(ctx context.Context, className, pathWithinClass, entryPrefix string, allowedFolders []string) ([]string, error) {
	keyCondition := "pk = :pk"
	exprValues := map[string]types.AttributeValue{
		":pk": &types.AttributeValueMemberS{Value: className},
	}
	if pathWithinClass != "" {
		keyCondition = "pk = :pk AND begins_with(sk, :prefix)"
		exprValues[":prefix"] = &types.AttributeValueMemberS{Value: pathWithinClass}
	}
	input := &dynamodb.QueryInput{
		TableName:                 aws.String(metadataTable),
		KeyConditionExpression:    aws.String(keyCondition),
		ExpressionAttributeValues: exprValues,
	}
	results, err := s.queryAllPages(ctx, input)
	if err != nil {
		return nil, err
	}

	var dbEntries []Metadata
	attributevalue.UnmarshalListOfMaps(results, &dbEntries)

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
func (s *server) allowedFoldersForClass(ctx context.Context, user User, collegeName, className string) []string {
	if user.Role == "teacher" {
		return nil
	}
	classInfo, err := s.getClassInfo(ctx, className)
	allowed := append([]string{}, user.Colleges[collegeName].Classes[className].Folders...)
	if err == nil {
		allowed = append(allowed, classInfo.SharedFolders...)
	}
	return allowed
}

func (s *server) TreeDirectory(ctx context.Context, in *proto.TreeDirectoryRequest) (*proto.TreeDirectoryResponse, error) {
	user := ctx.Value("User").(User)
	cd := user.CurrentDirectory
	depth := GetDepth(cd)

	var entries []string

	// Depth 0: show everything under each college → class → DynamoDB contents
	if depth == 0 || cd == "" {
		for collegeName, college := range user.Colleges {
			entries = append(entries, collegeName+"/")
			for className := range college.Classes {
				classPrefix := collegeName + "/" + className + "/"
				entries = append(entries, classPrefix)
				classEntries, err := s.queryClassEntries(ctx, className, "", classPrefix, s.allowedFoldersForClass(ctx, user, collegeName, className))
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
			classEntries, err := s.queryClassEntries(ctx, className, "", classPrefix, s.allowedFoldersForClass(ctx, user, collegeName, className))
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
	classEntries, err := s.queryClassEntries(ctx, className, pathWithinClass, "", s.allowedFoldersForClass(ctx, user, collegeName, className))
	if err != nil {
		logger("Unable to query DB: %v", err)
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
	cd := user.CurrentDirectory
	depth := GetDepth(cd)
	if depth < 2 {
		return nil, status.Errorf(codes.PermissionDenied, "must be inside a class to rename directories")
	}
	if depth == 2 && user.Role == "student" {
		return nil, status.Errorf(codes.PermissionDenied, "must be a teacher to rename a folder in class directory")
	}

	parts := strings.Split(cd, "/")
	className := parts[1]
	classInfo, err := s.getClassInfo(ctx, className)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to retrieve classroom info")
	}
	sharedFolders := classInfo.SharedFolders
	// Calculate the exact string prefixes we need to search for in the database
	pathWithinClass := strings.TrimPrefix(cd, parts[0]+"/"+className+"/")
	//ensures a student cannot rename a folder inside shared folder
	if user.Role == "student" {
		for _, folder := range sharedFolders {
			if strings.HasPrefix(pathWithinClass, folder+"/") || pathWithinClass == folder+"/" {
				return nil, status.Errorf(codes.PermissionDenied, "must be a teacher to rename a folder in shared directory")
			}
		}
	}

	oldPrefix := pathWithinClass + in.Entry
	newPrefix := pathWithinClass + in.Name

	// Call our new DB helper
	err = s.renameDirectoryMetadata(ctx, className, oldPrefix, newPrefix)
	if err != nil {
		if err.Error() == "directory not found" {
			return nil, status.Errorf(codes.NotFound, "directory '%s' does not exist", in.Entry)
		}
		logger("Rename directory failed: %v", err)
		return nil, status.Errorf(codes.Internal, "database error")
	}

	// sync with permissions array
	s.updateFolderLists(ctx, email, parts[0], className, oldPrefix, newPrefix)

	return &proto.RenameResponse{
		Message: fmt.Sprintf("Successfully renamed directory %s to %s", in.Entry, in.Name),
	}, nil
}
func (s *server) Download(req *proto.DownloadRequest, stream proto.Server_DownloadServer) error {
	// Grab user from your interceptor context
	ctx := stream.Context()
	user := ctx.Value("User").(User)
	cd := user.CurrentDirectory
	cd = strings.TrimSuffix(cd, "/")
	depth := GetDepth(cd)
	if depth < 2 {
		return status.Errorf(codes.PermissionDenied, "must be inside a class to download files")
	}
	collegeName, className, pathWithinClass := parsePath(cd + "/")
	fileSK := pathWithinClass + req.Name
	result, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(metadataTable),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: fileSK},
		},
	})
	if err != nil {
		logger("Failed to look up file metadata: %v", err)
		return status.Errorf(codes.Internal, "failed to look up file")
	}
	if result.Item == nil {
		return status.Errorf(codes.NotFound, "file %q not found", req.Name)
	}
	var meta Metadata
	if err := attributevalue.UnmarshalMap(result.Item, &meta); err != nil {
		logger("Failed to unmarshal file metadata: %v", err)
		return status.Errorf(codes.Internal, "failed to read file metadata")
	}
	if meta.Type != "file" {
		return status.Errorf(codes.InvalidArgument, "%q is not a file", req.Name)
	}

	s3Key := collegeName + "/" + className + "/" + fileSK
	s3Result, err := s.DownloadS3File(ctx, s3Key)
	if err != nil {
		logger("Failed to download file from S3: %v", err)
		return status.Errorf(codes.Internal, "failed to download file")
	}
	if s3Result == nil {
		logger("S3 download returned nil result", fmt.Errorf("nil result"))
		return status.Errorf(codes.Internal, "failed to download file")
	}
	defer s3Result.Body.Close()
	buf := make([]byte, 64*1024)
	for {
		// Check if client disconnected before each read
		select {
		case <-ctx.Done():
			return status.Errorf(codes.Canceled, "client disconnected")
		default:
		}
		n, err := s3Result.Body.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&proto.DownloadResponse{Data: buf[:n]}); sendErr != nil {
				return status.Errorf(codes.Internal, "failed to send chunk")
			}
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
	ctx := stream.Context()
	user := ctx.Value("User").(User)
	email := user.Email
	cd := strings.TrimSuffix(user.CurrentDirectory, "/")
	depth := GetDepth(cd)
	role := user.Role

	if depth < 2 {
		return status.Errorf(codes.PermissionDenied, "must be inside a class to upload files")
	}
	if role == "student" && depth == 2 {
		return status.Errorf(codes.PermissionDenied, "students must be inside their personal folder to upload files")
	}

	collegeName, className, pathWithinClass := parsePath(cd + "/")

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

	// First message must contain metadata
	req, err := stream.Recv()
	if err != nil {
		logger("Failed to receive upload metadata: %v", err)
		return errFileCannotBeStreamed
	}
	meta := req.GetMetadata()
	if meta == nil || meta.Name == "" || meta.ContentType == "" {
		return errFileCannotBeStreamed
	}

	s3Key := cd + "/" + meta.Name

	mp, err := s.NewMultipartUpload(ctx, s3Key, meta.ContentType)
	if err != nil {
		logger("Failed to initiate multipart upload: %v", err)
		return status.Errorf(codes.Internal, "failed to start upload")
	}

	// Stream chunks directly to S3
	for {
		req, err = stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			mp.Abort(ctx)
			return status.Errorf(codes.Internal, "error receiving chunk")
		}
		if chunk := req.GetChunk(); len(chunk) > 0 {
			if err := mp.Write(ctx, chunk); err != nil {
				mp.Abort(ctx)
				logger("Failed to upload part: %v", err)
				return status.Errorf(codes.Internal, "failed to upload file part")
			}
		}
	}

	url, err := mp.Complete(ctx)
	if err != nil {
		mp.Abort(ctx)
		logger("Failed to complete multipart upload: %v", err)
		return status.Errorf(codes.Internal, "failed to complete upload")
	}

	// Save metadata to DynamoDB
	err = s.uploadFileMetadata(ctx, className, pathWithinClass+meta.Name, meta.Name, email, cd+"/"+meta.Name, url)
	if err != nil {
		logger("Failed to save file metadata to DynamoDB: %v", err)
		return status.Errorf(codes.Internal, "failed to save file metadata")
	}

	return stream.SendAndClose(&proto.UploadResponse{Message: "uploaded"})
}
func (s *server) Delete(ctx context.Context, in *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	user := ctx.Value("User").(User)
	cd := user.CurrentDirectory
	depth := GetDepth(cd)

	if depth < 2 {
		return nil, status.Errorf(codes.PermissionDenied, "must be inside a class to delete")
	}

	targetName := strings.TrimSuffix(in.Path, "/")
	if targetName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "invalid path")
	}

	collegeName, className, pathWithinClass := parsePath(cd)
	targetSK := pathWithinClass + targetName

	// Nobody can delete a student's root folder.
	// A student's root folder is any top-level class folder (no "/" in name) that
	// appears in a student's personal folders list.
	if depth == 2 && !strings.Contains(targetName, "/") {
		classInfo, err := s.getClassInfo(ctx, className)
		if err != nil {
			logger("Cannot query db for class info: %v", err)
			return nil, errDB
		}
		for _, studentEmail := range classInfo.Students {
			studentUser, err := s.getUser(ctx, studentEmail)
			if err != nil {
				continue
			}
			if college, ok := studentUser.Colleges[collegeName]; ok {
				if classData, ok := college.Classes[className]; ok {
					for _, folder := range classData.Folders {
						if !strings.Contains(folder, "/") && folder == targetName {
							return nil, status.Errorf(codes.PermissionDenied, "cannot delete a student's root folder")
						}
					}
				}
			}
		}
	}

	// Students can only delete within their own personal folders (not shared folders, not other students').
	// Use the actual folders list — not an email-derived name — since folder names may differ from email prefix.
	if user.Role == "student" {
		personalFolders := user.Colleges[collegeName].Classes[className].Folders
		isAllowed := false
		for _, folder := range personalFolders {
			// HasPrefix with "folder/" ensures target is *inside* the folder, not the folder itself
			if strings.HasPrefix(targetSK, folder+"/") {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			return nil, status.Errorf(codes.PermissionDenied, "you can only delete files and folders within your own directory")
		}
	}

	// Check if target is a file (exact SK, no trailing slash)
	fileResult, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(metadataTable),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: targetSK},
		},
	})
	if err != nil {
		logger("DB error checking for file: %v", err)
		return nil, errDB
	}

	// Check if target is a folder (SK with trailing slash)
	folderSK := targetSK + "/"
	folderResult, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(metadataTable),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: folderSK},
		},
	})
	if err != nil {
		logger("DB error checking for folder: %v", err)
		return nil, errDB
	}

	if fileResult.Item == nil && folderResult.Item == nil {
		return nil, status.Errorf(codes.NotFound, "%s not found", targetName)
	}

	// Delete a file
	if fileResult.Item != nil {
		s3Key := collegeName + "/" + className + "/" + targetSK
		if err := s.DeleteS3File(ctx, s3Key); err != nil {
			logger("Failed to delete S3 file %s: %v", s3Key, err)
			return nil, errDB
		}
		_, err = s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(metadataTable),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: className},
				"sk": &types.AttributeValueMemberS{Value: targetSK},
			},
			ConditionExpression: aws.String("attribute_exists(pk)"),
		})
		if err != nil {
			var condErr *types.ConditionalCheckFailedException
			if errors.As(err, &condErr) {
				return nil, status.Errorf(codes.NotFound, "%s not found", targetName)
			}
			logger("Failed to delete file metadata: %v", err)
			return nil, errDB
		}
		return &proto.DeleteResponse{Message: fmt.Sprintf("Deleted %s", targetName)}, nil
	}

	// Delete a folder: query all items with the folder prefix then delete them
	input := &dynamodb.QueryInput{
		TableName:              aws.String(metadataTable),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: className},
			":prefix": &types.AttributeValueMemberS{Value: folderSK},
		},
	}
	queryResult, err := s.queryAllPages(ctx, input)
	if err != nil {
		logger("Failed to query folder contents: %v", err)
		return nil, errDB
	}

	for _, item := range queryResult {
		var meta Metadata
		if err := attributevalue.UnmarshalMap(item, &meta); err != nil {
			logger("Failed to unmarshal item: %v", err)
			return nil, errDB
		}
		if meta.Type == "file" {
			s3Key := collegeName + "/" + className + "/" + meta.SK
			if err := s.DeleteS3File(ctx, s3Key); err != nil {
				logger("Failed to delete S3 file %s: %v", s3Key, err)
				return nil, errDB
			}
		}
		_, err = s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(metadataTable),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: className},
				"sk": &types.AttributeValueMemberS{Value: meta.SK},
			},
		})
		if err != nil {
			logger("Failed to delete item %s: %v", meta.SK, err)
			return nil, errDB
		}
	}

	// Remove deleted folder and its subfolders from permission arrays
	if err := s.removeFolderFromLists(ctx, collegeName, className, targetSK); err != nil {
		logger("Failed to update folder permissions after delete: %v", err)
		return nil, errDB
	}

	return &proto.DeleteResponse{Message: fmt.Sprintf("Deleted folder %s", targetName)}, nil
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
	env := os.Getenv("ENVIRONMENT")
	var cfg aws.Config
	var cwm *CloudWatchMetrics
	if isDev {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("fake", "fake", "fake")),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
		cwm = NewCloudWatchMetrics(cfg, env)
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
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		}
	})
	//Init Server Object and gRPC server
	s := NewServer(dbClient, s3Client)
	//add interceptor ie middleware to validate user

	var unaryInt grpc.UnaryServerInterceptor
	var streamInt grpc.StreamServerInterceptor

	if cwm != nil {
		unaryInt = cloudwatchUnaryInterceptor(cwm, unaryInterceptor(dbClient))
		streamInt = cloudwatchStreamInterceptor(cwm, streamInterceptor(dbClient))
	} else {
		unaryInt = unaryInterceptor(dbClient)
		streamInt = streamInterceptor(dbClient)
	}

	g := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionAge:      5 * time.Minute,
			MaxConnectionAgeGrace: 30 * time.Second,
		}),
		grpc.UnaryInterceptor(unaryInt),
		grpc.StreamInterceptor(streamInt),
	)
	//Register server object into gRPC server
	proto.RegisterServerServer(g, s)
	//Listen on TCP Port 5001
	if err := g.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
