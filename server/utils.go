package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
