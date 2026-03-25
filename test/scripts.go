package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Class struct {
	Role    string   `dynamodbav:"role"`
	Folders []string `dynamodbav:"folders"`
}

type User struct {
	Email      string           `dynamodbav:"email"`
	Role       string           `dynamodbav:"role"`
	Classrooms map[string]Class `dynamodbav:"classrooms"`
}

type Metadata struct {
	PK           string `dynamodbav:"pk"`
	SK           string `dynamodbav:"sk"`
	Name         string `dynamodbav:"name"`
	Owner        string `dynamodbav:"owner"`
	LastModified string `dynamodbav:"last_modified"`
	Type         string `dynamodbav:"type"`
	FullPath     string `dynamodbav:"full_path"`
	S3Url        string `dynamodbav:"s3_url"`
}

func main() {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("fake", "fake", "fake")),
	)
	if err != nil {
		log.Fatal(err)
	}
	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String("http://localhost:8000")
	})

	createTables(client)
	seedData(client)
	fmt.Println("Setup complete!")
}

func createTables(client *dynamodb.Client) {
	// Create user table
	_, err := client.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		TableName: aws.String("user"),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("email"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("email"), KeyType: types.KeyTypeHash},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		log.Printf("User table: %v", err)
	} else {
		fmt.Println("User table created")
	}

	// Create classroom_metadata table
	_, err = client.CreateTable(context.TODO(), &dynamodb.CreateTableInput{
		TableName: aws.String("classroom_metadata"),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("owner"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("last_modified"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("owner-index"),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("owner"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("last_modified"), KeyType: types.KeyTypeRange},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		log.Printf("Metadata table: %v", err)
	} else {
		fmt.Println("Metadata table created")
	}

	// Wait for tables to be ready
	time.Sleep(2 * time.Second)
}

func seedData(client *dynamodb.Client) {
	now := time.Now().Format(time.RFC3339)

	// Seed users
	users := []User{
		{
			Email: "alice@school.edu",
			Role:  "student",
			Classrooms: map[string]Class{
				"CS101": {
					Role:    "student",
					Folders: []string{"alice", "alice/homework", "alice/notes"},
				},
			},
		},
		{
			Email: "bob@school.edu",
			Role:  "student",
			Classrooms: map[string]Class{
				"CS101": {
					Role:    "student",
					Folders: []string{"bob", "bob/homework"},
				},
			},
		},
		{
			Email: "professor@school.edu",
			Role:  "teacher",
			Classrooms: map[string]Class{
				"CS101": {
					Role:    "teacher",
					Folders: []string{},
				},
			},
		},
	}

	for _, user := range users {
		item, err := attributevalue.MarshalMap(user)
		if err != nil {
			log.Printf("Error marshalling user %s: %v", user.Email, err)
			continue
		}
		_, err = client.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String("user"),
			Item:      item,
		})
		if err != nil {
			log.Printf("Error seeding user %s: %v", user.Email, err)
		} else {
			fmt.Printf("Seeded user: %s\n", user.Email)
		}
	}

	// Seed classroom metadata (folders and files)
	entries := []Metadata{
		// Alice's folders
		{PK: "CS101", SK: "alice/", Name: "alice", Owner: "alice@school.edu", LastModified: now, Type: "folder", FullPath: "CS101/alice/"},
		{PK: "CS101", SK: "alice/homework/", Name: "homework", Owner: "alice@school.edu", LastModified: now, Type: "folder", FullPath: "CS101/alice/homework/"},
		{PK: "CS101", SK: "alice/notes/", Name: "notes", Owner: "alice@school.edu", LastModified: now, Type: "folder", FullPath: "CS101/alice/notes/"},
		// Alice's files
		{PK: "CS101", SK: "alice/homework/assignment1.pdf", Name: "assignment1.pdf", Owner: "alice@school.edu", LastModified: now, Type: "file", FullPath: "CS101/alice/homework/assignment1.pdf", S3Url: "s3://neudfs/CS101/alice/homework/assignment1.pdf"},
		{PK: "CS101", SK: "alice/notes/chapter1.txt", Name: "chapter1.txt", Owner: "alice@school.edu", LastModified: now, Type: "file", FullPath: "CS101/alice/notes/chapter1.txt", S3Url: "s3://neudfs/CS101/alice/notes/chapter1.txt"},
		// Bob's folders
		{PK: "CS101", SK: "bob/", Name: "bob", Owner: "bob@school.edu", LastModified: now, Type: "folder", FullPath: "CS101/bob/"},
		{PK: "CS101", SK: "bob/homework/", Name: "homework", Owner: "bob@school.edu", LastModified: now, Type: "folder", FullPath: "CS101/bob/homework/"},
		// Bob's file
		{PK: "CS101", SK: "bob/homework/assignment1.pdf", Name: "assignment1.pdf", Owner: "bob@school.edu", LastModified: now, Type: "file", FullPath: "CS101/bob/homework/assignment1.pdf", S3Url: "s3://neudfs/CS101/bob/homework/assignment1.pdf"},
	}

	for _, entry := range entries {
		item, err := attributevalue.MarshalMap(entry)
		if err != nil {
			log.Printf("Error marshalling entry %s/%s: %v", entry.PK, entry.SK, err)
			continue
		}
		_, err = client.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String("classroom_metadata"),
			Item:      item,
		})
		if err != nil {
			log.Printf("Error seeding entry %s/%s: %v", entry.PK, entry.SK, err)
		} else {
			fmt.Printf("Seeded metadata: %s/%s\n", entry.PK, entry.SK)
		}
	}
}
