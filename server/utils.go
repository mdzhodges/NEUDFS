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

// renameFileMetadata updates the 'name' and 'full_path' attributes of a specific file in DynamoDB.
// It uses the className (PK) and file ID/path (SK) to locate the exact item to modify.
func (s *server) renameFileMetadata(className, sk, newName, newFullPath string) error {
	_, err := s.DB.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName: aws.String("classroom_metadata"),
		
		// Identify the exact item to update using its Primary Key (PK + SK)
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: className},
			"sk": &types.AttributeValueMemberS{Value: sk},
		},
		
		// Define the update expression (similar to a SQL SET clause)
		// We use '#n' as a placeholder for the column 'name', and ':newName' / ':newPath' as placeholders for the values.
		UpdateExpression: aws.String("SET #n = :newName, full_path = :newPath"),
		
		// Map the attribute name placeholders to the actual DynamoDB column names.
		// We MUST do this for 'name' because "name" is a reserved keyword in DynamoDB. 
		// If we put "SET name = :newName" directly in the expression above, AWS will throw an error.
		ExpressionAttributeNames: map[string]string{
			"#n": "name", 
		},
		
		// Map the value placeholders to the actual strings passed into this function.
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":newName": &types.AttributeValueMemberS{Value: newName},
			":newPath":  &types.AttributeValueMemberS{Value: newFullPath},
		},
	})

	// Handle any errors returned by the AWS SDK
	if err != nil {
		logger("Cannot update file metadata for rename: %v", err)
		return err
	}

	return nil
}
