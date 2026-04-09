package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const SessionTTL = 2 * time.Hour

const minPartSize = 5 * 1024 * 1024 // 5MB - S3 multipart minimum

type MultipartUpload struct {
	s3Client       *s3.Client
	bucket         string
	key            string
	uploadID       *string
	completedParts []s3types.CompletedPart
	partNumber     int32
	buf            []byte
}

func (m *MultipartUpload) Write(ctx context.Context, chunk []byte) error {
	m.buf = append(m.buf, chunk...)
	if len(m.buf) >= minPartSize {
		return m.flushPart(ctx)
	}
	return nil
}
func (m *MultipartUpload) flushPart(ctx context.Context) error {
	resp, err := m.s3Client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(m.bucket),
		Key:        aws.String(m.key),
		UploadId:   m.uploadID,
		PartNumber: aws.Int32(m.partNumber),
		Body:       bytes.NewReader(m.buf),
	})
	if err != nil {
		return err
	}
	m.completedParts = append(m.completedParts, s3types.CompletedPart{
		ETag:       resp.ETag,
		PartNumber: aws.Int32(m.partNumber),
	})
	m.partNumber++
	m.buf = m.buf[:0]
	return nil
}

func (m *MultipartUpload) Complete(ctx context.Context) (string, error) {
	if len(m.buf) > 0 {
		if err := m.flushPart(ctx); err != nil {
			return "", err
		}
	}
	_, err := m.s3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(m.bucket),
		Key:      aws.String(m.key),
		UploadId: m.uploadID,
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: m.completedParts,
		},
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", m.bucket, m.key), nil
}

// Abort cleans up an incomplete multipart upload. Safe to call multiple times.
func (m *MultipartUpload) Abort(ctx context.Context) {
	m.s3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(m.bucket),
		Key:      aws.String(m.key),
		UploadId: m.uploadID,
	})
}

func (s *server) NewMultipartUpload(ctx context.Context, key, contentType string) (*MultipartUpload, error) {
	resp, err := s.S3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s3Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return nil, err
	}
	return &MultipartUpload{
		s3Client:   s.S3Client,
		bucket:     s3Bucket,
		key:        key,
		uploadID:   resp.UploadId,
		partNumber: 1,
		buf:        make([]byte, 0, minPartSize+1024*1024),
	}, nil
}

func logger(format string, a ...any) {
	fmt.Printf("LOG:\t"+format+"\n", a...)
}

// converts a []string into a DynamoDB List of String AttributeValues
func stringSliceToAVList(vals []string) []types.AttributeValue {
	out := make([]types.AttributeValue, 0, len(vals))
	for _, v := range vals {
		out = append(out, &types.AttributeValueMemberS{Value: v})
	}
	return out
}

func parsePath(cd string) (collegeName, className, pathWithinClass string) {
	parts := strings.Split(cd, "/")
	collegeName = parts[0]
	className = parts[1]
	pathWithinClass = strings.TrimPrefix(cd, collegeName+"/"+className+"/")
	return
}

func (s *server) getClassInfo(ctx context.Context, className string) (ClassInfo, error) {
	result, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(metadataTable),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: "class_info"},
		},
	})
	if err != nil {
		logger("Cannot query db for shared folders", err)
		return ClassInfo{}, err
	}
	var classInfo ClassInfo
	if result.Item != nil {
		err = attributevalue.UnmarshalMap(result.Item, &classInfo)
		if err != nil {
			return ClassInfo{}, err
		}
	}
	return classInfo, nil
}

func (s *server) getUser(ctx context.Context, email string) (User, error) {
	result, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(userTable),
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
	})
	if err != nil {
		return User{}, err
	}
	if result.Item == nil {
		return User{}, fmt.Errorf("user not found: %s", email)
	}
	var user User
	err = attributevalue.UnmarshalMap(result.Item, &user)
	if err != nil {
		return User{}, err
	}
	return user, nil
}
func (s *server) updateSharedFolders(className, newFolderPath string) error {
	_, err := s.DB.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName: aws.String(metadataTable),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: "class_info"},
		},
		UpdateExpression: aws.String("SET shared_folders = list_append(if_not_exists(shared_folders, :empty), :folder)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":folder": &types.AttributeValueMemberL{Value: []types.AttributeValue{
				&types.AttributeValueMemberS{Value: newFolderPath},
			}},
			":empty": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
		},
	})
	return err
}
func (s *server) updateUserFolders(email, collegeName, className, newFolderPath string) error {
	_, err := s.DB.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName: aws.String(userTable),
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
		UpdateExpression: aws.String("SET colleges.#col.classes.#cls.folders = list_append(colleges.#col.classes.#cls.folders, :folder)"),
		ExpressionAttributeNames: map[string]string{
			"#col": collegeName,
			"#cls": className,
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":folder": &types.AttributeValueMemberL{Value: []types.AttributeValue{
				&types.AttributeValueMemberS{Value: newFolderPath},
			}},
		},
	})
	return err
}
func (s *server) createFolderMetadata(className, sk, name, owner, fullPath string) error {
	item, err := attributevalue.MarshalMap(Metadata{
		PK:       className,
		SK:       sk,
		Name:     name,
		Owner:    owner,
		Type:     "folder",
		FullPath: fullPath,
	})
	if err != nil {
		return err
	}
	_, err = s.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(metadataTable),
		Item:      item,
	})
	return err
}

// renameFileMetadata replaces a file's metadata in DynamoDB to simulate a rename.
// Because the file path is the Sort Key (SK), we cannot simply use UpdateItem.
// We must Get the old item, Put a new item with the new SK, and Delete the old item.
func (s *server) renameFileMetadata(className, oldSK, newSK, newName, newFullPath string) error {

	// GET the existing file metadata using the old SK
	getResult, err := s.DB.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(metadataTable),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: oldSK},
		},
	})
	if err != nil {
		logger("Database error retrieving old file: %v", err)
		return err
	}
	if getResult.Item == nil {
		return fmt.Errorf("file not found")
	}

	// UNMARSHAL the old data into our Go struct
	var item Metadata // Ensure Metadata struct is available in this file's scope
	if err := attributevalue.UnmarshalMap(getResult.Item, &item); err != nil {
		logger("Error unmarshaling metadata: %v", err)
		return err
	}

	// UPDATE the struct with the new values
	item.SK = newSK
	item.Name = newName
	item.FullPath = newFullPath

	// MARSHAL the updated struct back into DynamoDB format
	newItem, err := attributevalue.MarshalMap(item)
	if err != nil {
		logger("Error marshaling new metadata: %v", err)
		return err
	}

	// PUT the new item into the database (this creates the "renamed" file)
	_, err = s.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(metadataTable),
		Item:      newItem,
	})
	if err != nil {
		logger("Cannot save new file metadata: %v", err)
		return err
	}

	// DELETE the old item to clean up the original filename
	_, err = s.DB.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
		TableName: aws.String(metadataTable),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: oldSK},
		},
	})
	if err != nil {
		logger("Cannot delete old file metadata: %v", err)
		return err
	}

	return nil
}

// renameDirectoryMetadata finds all files/folders inside a directory and updates their paths.
// In DynamoDB, we do this by querying for all items whose SK starts with the old directory prefix,
// then creating new copies with the new prefix, and finally deleting the old copies.
func (s *server) renameDirectoryMetadata(ctx context.Context, className, oldPrefix, newPrefix string) error {
	// QUERY all items that belong to this class AND start with the old folder path
	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(metadataTable),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :oldPrefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":        &types.AttributeValueMemberS{Value: className},
			":oldPrefix": &types.AttributeValueMemberS{Value: oldPrefix},
		},
	}

	result, err := s.queryAllPages(ctx, queryInput)
	if err != nil {
		logger("Failed to query directory items: %v", err)
		return err
	}

	if len(result) == 0 {
		return fmt.Errorf("directory not found")
	}

	// LOOP through every single item we found inside the directory
	for _, dynamodbItem := range result {
		var item Metadata
		if err := attributevalue.UnmarshalMap(dynamodbItem, &item); err != nil {
			logger("Error unmarshaling item in directory: %v", err)
			continue // Skip this item if there's an error, but keep trying the rest
		}

		oldSK := item.SK

		// CALCULATE the new paths
		// We replace the first instance of the old folder path with the new folder path
		newSK := strings.Replace(oldSK, oldPrefix, newPrefix, 1)

		item.SK = newSK
		item.FullPath = strings.Replace(item.FullPath, oldSK, newSK, 1)

		// If this specific item IS the directory itself (not a file inside it), update its Name attribute
		if oldSK == oldPrefix {
			// Strip the trailing slash and get the actual new folder name
			cleanName := strings.TrimSuffix(newPrefix, "/")
			parts := strings.Split(cleanName, "/")
			item.Name = parts[len(parts)-1]
		}

		// PUT the newly updated item into the database
		newItem, err := attributevalue.MarshalMap(item)
		if err != nil {
			logger("Error marshaling new item: %v", err)
			continue
		}

		_, err = s.DB.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(metadataTable),
			Item:      newItem,
		})
		if err != nil {
			logger("Cannot save new item metadata: %v", err)
			continue
		}

		// DELETE the old item to clean up the database
		_, err = s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(metadataTable),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: className},
				"sk": &types.AttributeValueMemberS{Value: oldSK},
			},
		})
		if err != nil {
			logger("Cannot delete old item metadata: %v", err)
		}
	}

	return nil
}

// updateFolderLists syncs renamed folder paths inside the User and ClassInfo permission arrays.
func (s *server) updateFolderLists(ctx context.Context, callerEmail, collegeName, className, oldPrefix, newPrefix string) {
	oldTarget := strings.TrimSuffix(oldPrefix, "/")
	newTarget := strings.TrimSuffix(newPrefix, "/")

	// Update ClassInfo (Shared Folders)
	classInfo, err := s.getClassInfo(ctx, className)
	if err == nil {
		for i, f := range classInfo.SharedFolders {
			if f == oldTarget || strings.HasPrefix(f, oldPrefix) {
				newVal := strings.Replace(f, oldTarget, newTarget, 1)
				_, err := s.DB.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
					TableName: aws.String(metadataTable),
					Key: map[string]types.AttributeValue{
						"pk": &types.AttributeValueMemberS{Value: className},
						"sk": &types.AttributeValueMemberS{Value: "class_info"},
					},
					UpdateExpression: aws.String(fmt.Sprintf("SET shared_folders[%d] = :val", i)),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":val": &types.AttributeValueMemberS{Value: newVal},
					},
				})
				if err != nil {
					logger("Failed to update shared folder at index %d: %v", i, err)
				}
			}
		}
	}

	// Gather emails to check (the caller + all known students)
	emailsToCheck := map[string]bool{callerEmail: true}
	if err == nil {
		for _, studentEmail := range classInfo.Students {
			emailsToCheck[studentEmail] = true
		}
	}

	// Update User profiles
	for emailToCheck := range emailsToCheck {
		user, err := s.getUser(ctx, emailToCheck)
		if err != nil {
			continue
		}

		college, ok := user.Colleges[collegeName]
		if !ok {
			continue
		}
		classData, ok := college.Classes[className]
		if !ok {
			continue
		}

		for i, f := range classData.Folders {
			if f == oldTarget || strings.HasPrefix(f, oldPrefix) {
				newVal := strings.Replace(f, oldTarget, newTarget, 1)
				_, err := s.DB.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
					TableName: aws.String(userTable),
					Key: map[string]types.AttributeValue{
						"email": &types.AttributeValueMemberS{Value: emailToCheck},
					},
					UpdateExpression: aws.String(fmt.Sprintf("SET colleges.#col.classes.#cls.folders[%d] = :val", i)),
					ExpressionAttributeNames: map[string]string{
						"#col": collegeName,
						"#cls": className,
					},
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":val": &types.AttributeValueMemberS{Value: newVal},
					},
				})
				if err != nil {
					logger("Failed to update folder at index %d for user %s: %v", i, emailToCheck, err)
				}
			}
		}
	}
}
func (s *server) DeleteS3File(ctx context.Context, s3Key string) error {
	_, err := s.S3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(s3Key),
	})
	return err
}

// removeFolderFromLists removes a folder and all its subfolders from user and class permission arrays.
func (s *server) removeFolderFromLists(ctx context.Context, collegeName, className, folderPath string) {
	targetPath := strings.TrimSuffix(folderPath, "/")
	prefix := targetPath + "/"

	// I used compare and swap to avoid race conditions
	// the trick is to write it back only if the list is unchanged
	var classInfo ClassInfo
	//  I looped 8 times so this will still work if the list is being constantly modified
	for attempt := 0; attempt < 8; attempt++ {
		var err error
		classInfo, err = s.getClassInfo(ctx, className)
		if err != nil {
			logger("removeFolderFromLists: failed to get class info: %v", err)
			return
		}

		newSharedStr := make([]string, 0, len(classInfo.SharedFolders))
		changed := false
		for _, f := range classInfo.SharedFolders {
			if f == targetPath || strings.HasPrefix(f, prefix) {
				changed = true
				continue
			}
			newSharedStr = append(newSharedStr, f)
		}
		if !changed {
			break
		}

		_, err = s.DB.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
			TableName: aws.String(metadataTable),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: className},
				"sk": &types.AttributeValueMemberS{Value: "class_info"},
			},
			UpdateExpression: aws.String("SET shared_folders = :val"),
			ConditionExpression: aws.String(
				"attribute_not_exists(shared_folders) OR shared_folders = :expected",
			),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":val": &types.AttributeValueMemberL{Value: stringSliceToAVList(newSharedStr)},
				// compare against the list read, if another request updated it then it will fail
				":expected": &types.AttributeValueMemberL{Value: stringSliceToAVList(classInfo.SharedFolders)},
			},
		})
		if err == nil {
			break
		}
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			continue
		}
		logger("removeFolderFromLists: failed to update shared folders: %v", err)
		break
	}

	// Rebuild every class member's (students, TAs, professor) folders list.
	// Each user profile stores their own copy of the folder list, so all must be updated.
	allEmails := append([]string{}, classInfo.Students...)
	allEmails = append(allEmails, classInfo.TAs...)
	if classInfo.Professor != "" {
		allEmails = append(allEmails, classInfo.Professor)
	}
	for _, memberEmail := range allEmails {
		for attempt := 0; attempt < 8; attempt++ {
			u, err := s.getUser(ctx, memberEmail)
			if err != nil {
				break
			}
			college, ok := u.Colleges[collegeName]
			if !ok {
				break
			}
			classData, ok := college.Classes[className]
			if !ok {
				break
			}
			oldFolders := classData.Folders

			newFoldersStr := make([]string, 0, len(oldFolders))
			changed := false
			for _, f := range oldFolders {
				if f == targetPath || strings.HasPrefix(f, prefix) {
					changed = true
					continue
				}
				newFoldersStr = append(newFoldersStr, f)
			}
			if !changed {
				break
			}

			_, err = s.DB.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
				TableName: aws.String(userTable),
				Key: map[string]types.AttributeValue{
					"email": &types.AttributeValueMemberS{Value: memberEmail},
				},
				UpdateExpression: aws.String("SET colleges.#col.classes.#cls.folders = :val"),
				ConditionExpression: aws.String(
					"attribute_not_exists(colleges.#col.classes.#cls.folders) OR colleges.#col.classes.#cls.folders = :expected",
				),
				ExpressionAttributeNames: map[string]string{
					"#col": collegeName,
					"#cls": className,
				},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":val": &types.AttributeValueMemberL{Value: stringSliceToAVList(newFoldersStr)},
					// Same CAS idea as above; prevents one delete from clobbering another.
					":expected": &types.AttributeValueMemberL{Value: stringSliceToAVList(oldFolders)},
				},
			})
			if err == nil {
				break
			}
			var condErr *types.ConditionalCheckFailedException
			if errors.As(err, &condErr) {
				continue
			}
			logger("removeFolderFromLists: failed to update folders for %s: %v", memberEmail, err)
			break
		}
	}
}

func (s *server) DownloadS3File(ctx context.Context, s3Url string) (*s3.GetObjectOutput, error) {
	result, err := s.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(s3Url),
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Need to implement S3
func (s *server) uploadToS3(content []byte, filePath string) (string, error) {
	s3Url := "https://s3.amazonaws.com/neudfs-storage-dev/" + filePath
	_, err := s.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(filePath),
		Body:   bytes.NewReader(content),
	})
	if err != nil {
		return "", err
	}
	return s3Url, nil
}

func (s *server) uploadFileMetadata(className, sk, name, owner, fullPath, s3Url string) error {
	item, err := attributevalue.MarshalMap(Metadata{
		PK:       className,
		SK:       sk,
		Name:     name,
		Owner:    owner,
		Type:     "file",
		FullPath: fullPath,
		S3Url:    s3Url,
	})
	if err != nil {
		return err
	}
	_, err = s.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(metadataTable),
		Item:      item,
	})
	return err
}

func (s *server) SetCurrentDirectory(ctx context.Context, email string, expectedPrev string, dir string) error {
	ttl := time.Now().Add(SessionTTL).Unix()
	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(userTable),
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
		UpdateExpression: aws.String("SET currentDirectory = :cd, directoryTTL = :ttl"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":cd":   &types.AttributeValueMemberS{Value: dir},
			":ttl":  &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", ttl)},
			":prev": &types.AttributeValueMemberS{Value: expectedPrev},
		},
	}
	//Ensures that that the directory being read has not changed
	if expectedPrev == "" {
		input.ConditionExpression = aws.String(
			"attribute_not_exists(currentDirectory) OR currentDirectory = :prev",
		)
	} else {
		input.ConditionExpression = aws.String(
			"attribute_not_exists(currentDirectory) OR currentDirectory = :prev",
		)
	}
	_, err := s.DB.UpdateItem(ctx, input)
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return status.Errorf(codes.Aborted, "directory was modified by another session, please retry")
		}
		return err
	}
	return nil
}

// Clears current directory of the user
func (s *server) ClearCurrentDirectory(ctx context.Context, email string) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(userTable),
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
		UpdateExpression: aws.String("REMOVE currentDirectory, directoryTTL"),
	})
	return err
}

func (u *User) GetCurrentDirectory() string {
	if u.DirectoryTTL == 0 {
		return ""
	}
	if time.Now().Unix() > u.DirectoryTTL {
		// Session expired — treat as root
		return ""
	}
	return u.CurrentDirectory
}

func (s *server) queryAllPages(ctx context.Context, input *dynamodb.QueryInput) ([]map[string]types.AttributeValue, error) {
	var allItems []map[string]types.AttributeValue
	for {
		result, err := s.DB.Query(ctx, input)
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, result.Items...)
		if result.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = result.LastEvaluatedKey
	}
	return allItems, nil
}
