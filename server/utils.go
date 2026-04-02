package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func logger(format string, a ...any) {
	fmt.Printf("LOG:\t"+format+"\n", a...)
}

func parsePath(cd string) (collegeName, className, pathWithinClass string) {
	parts := strings.Split(cd, "/")
	collegeName = parts[0]
	className = parts[1]
	pathWithinClass = strings.TrimPrefix(cd, collegeName+"/"+className+"/")
	return
}

func (s *server) getClassInfo(className string) (ClassInfo, error) {
	result, err := s.DB.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String("classroom_metadata"),
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

func (s *server) getUser(email string) (User, error) {
	result, err := s.DB.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String("user"),
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
		TableName: aws.String("classroom_metadata"),
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
		TableName: aws.String("user"),
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
		TableName: aws.String("classroom_metadata"),
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
		TableName: aws.String("classroom_metadata"),
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
		TableName: aws.String("classroom_metadata"),
		Item:      newItem,
	})
	if err != nil {
		logger("Cannot save new file metadata: %v", err)
		return err
	}

	// DELETE the old item to clean up the original filename
	_, err = s.DB.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
		TableName: aws.String("classroom_metadata"),
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
func (s *server) renameDirectoryMetadata(className, oldPrefix, newPrefix string) error {
	// QUERY all items that belong to this class AND start with the old folder path
	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String("classroom_metadata"),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :oldPrefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":        &types.AttributeValueMemberS{Value: className},
			":oldPrefix": &types.AttributeValueMemberS{Value: oldPrefix},
		},
	}

	result, err := s.DB.Query(context.TODO(), queryInput)
	if err != nil {
		logger("Failed to query directory items: %v", err)
		return err
	}

	if len(result.Items) == 0 {
		return fmt.Errorf("directory not found")
	}

	// LOOP through every single item we found inside the directory
	for _, dynamodbItem := range result.Items {
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

		_, err = s.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String("classroom_metadata"),
			Item:      newItem,
		})
		if err != nil {
			logger("Cannot save new item metadata: %v", err)
			continue
		}

		// DELETE the old item to clean up the database
		_, err = s.DB.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
			TableName: aws.String("classroom_metadata"),
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
func (s *server) updateFolderLists(callerEmail, collegeName, className, oldPrefix, newPrefix string) {
	oldTarget := strings.TrimSuffix(oldPrefix, "/")
	newTarget := strings.TrimSuffix(newPrefix, "/")

	// Update ClassInfo (Shared Folders)
	classInfo, err := s.getClassInfo(className)
	if err == nil {
		updated := false
		for i, f := range classInfo.SharedFolders {
			if f == oldTarget || strings.HasPrefix(f, oldPrefix) {
				classInfo.SharedFolders[i] = strings.Replace(f, oldTarget, newTarget, 1)
				updated = true
			}
		}
		if updated {
			item, _ := attributevalue.MarshalMap(classInfo)
			s.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String("classroom_metadata"),
				Item:      item,
			})
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
		user, err := s.getUser(emailToCheck)
		if err != nil {
			continue
		}

		updated := false
		if college, ok := user.Colleges[collegeName]; ok {
			if classData, ok := college.Classes[className]; ok {
				for i, f := range classData.Folders {
					if f == oldTarget || strings.HasPrefix(f, oldPrefix) {
						classData.Folders[i] = strings.Replace(f, oldTarget, newTarget, 1)
						updated = true
					}
				}
			}
		}

		if updated {
			item, _ := attributevalue.MarshalMap(user)
			s.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String("user"),
				Item:      item,
			})
		}
	}
}

// Need to implement S3
func (s *server) uploadToS3(content []byte, filePath string) (string, error) {
	// In a real implementation, this function would use the AWS SDK to upload the file content to S3
	// and return the public URL of the uploaded file. For this example, we'll just return a placeholder URL.
	s3Url := "https://s3.amazonaws.com/your-bucket/" + filePath
	_, err := s.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("neudfs-storage-dev"),
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
		TableName: aws.String("classroom_metadata"),
		Item:      item,
	})
	return err
}
