package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type ClassInfo struct {
	PK            string   `dynamodbav:"pk"`
	SK            string   `dynamodbav:"sk"`
	SharedFolders []string `dynamodbav:"shared_folders"`
	Students      []string `dynamodbav:"students"`
	Professor     string   `dynamodbav:"professor"`
	TAs           []string `dynamodbav:"tas"`
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
	Role          string   `dynamodbav:"role"`
	Folders       []string `dynamodbav:"folders"`
	SharedFolders []string `dynamodbav:"shared_folders"`
}
type Classroom struct {
	Classes map[string]Class `dynamodbav:"classes"`
}
type User struct {
	Email            string               `dynamodbav:"email"`
	Role             string               `dynamodbav:"role"`
	Colleges         map[string]Classroom `dynamodbav:"colleges"`
	CurrentDirectory string               `dynamodbav:"currentDirectory"`
	DirectoryTTL     int64                `dynamodbav:"directoryTTL"`
}

var firstNames = []string{
	"Alice", "Bob", "Charlie", "Diana", "Eve", "Frank", "Grace", "Henry", "Iris", "Jack",
	"Karen", "Leo", "Mia", "Noah", "Olivia", "Paul", "Quinn", "Ruby", "Sam", "Tina",
	"Uma", "Victor", "Wendy", "Xander", "Yara", "Zach", "Bella", "Caleb", "Daisy", "Ethan",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Martinez", "Wilson",
	"Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin", "Lee", "Harris", "Clark", "Lewis",
}

func randomName() string {
	first := firstNames[rand.Intn(len(firstNames))]
	last := lastNames[rand.Intn(len(lastNames))]
	return first + " " + last
}

func randomEmail(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", ".")) + "@northeastern.edu"
}
func createTables(client *dynamodb.Client) {
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

	time.Sleep(2 * time.Second)
}

func main() {
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")

	var cfg aws.Config
	var err error
	if endpoint != "" {
		// Local — fake credentials, custom endpoint
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("fake", "fake", "fake")),
		)
	} else {
		// Deployed — use real AWS credentials from environment/profile
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
		)
	}
	if err != nil {
		log.Fatal(err)
	}

	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	if endpoint != "" {
		createTables(client)
	}
	seedLargeData(client)
	fmt.Println("Setup complete!")
}

// 100 users, 1 college, 10 classes, 10 students per class, 10 professors one for each class, 2 TAs per class, shared folders for each class is announcements and assignments

func seedLargeData(client *dynamodb.Client) {
	classes := []struct {
		college string
		name    string
	}{
		{"Khoury", "CS5010"},
		{"Khoury", "CS5100"},
		{"Khoury", "CS5400"},
		{"Khoury", "CS5600"},
		{"Khoury", "CS5800"},
		{"Khoury", "CS6650"},
		{"Khoury", "CS6750"},
		{"Khoury", "CS6600"},
		{"Khoury", "CS7200"},
		{"Khoury", "CS7500"},
	}
	//two shared folders for each class, for each class info, then folder entries for each student
	mdFolders := make([]Metadata, 0)
	entries := make([]ClassInfo, 0)
	students := make([]User, 100)
	professors := make([]User, 10)
	tas := make([]User, 0, 20)
	usedNames := make(map[string]bool)
	for i, class := range classes {
		usedFirstNames := make(map[string]bool)
		// Create professor
		var profName string
		for {
			profName = randomName()
			if !usedNames[profName] {
				usedNames[profName] = true
				break
			}
		}
		profEmail := randomEmail(profName)
		usedNames[profName] = true
		fmt.Printf("Professor: %s (%s) -> %s/%s\n", profName, profEmail, class.college, class.name)
		professors[i] = User{
			Email:            profEmail,
			Role:             "professor",
			CurrentDirectory: "",
			DirectoryTTL:     0,
			Colleges: map[string]Classroom{
				class.college: {
					Classes: map[string]Class{
						class.name: {
							Role:          "professor",
							Folders:       []string{"announcements", "assignments"},
							SharedFolders: []string{"announcements", "assignments"},
						},
					},
				},
			},
		}
		mdFolders = append(mdFolders, Metadata{
			PK:           class.name,
			SK:           "announcements/",
			Name:         "announcements",
			Owner:        profEmail,
			LastModified: time.Now(),
			Type:         "folder",
			FullPath:     fmt.Sprintf("%s/%s/announcements/", class.college, class.name),
		})
		mdFolders = append(mdFolders, Metadata{
			PK:           class.name,
			SK:           "assignments/",
			Name:         "assignments",
			Owner:        profEmail,
			LastModified: time.Now(),
			Type:         "folder",
			FullPath:     fmt.Sprintf("%s/%s/assignments/", class.college, class.name),
		})
		var taName string
		for {
			taName = randomName() // = not :=
			if !usedNames[taName] {
				usedNames[taName] = true
				break
			}
		}
		taEmail := randomEmail(taName)
		usedNames[taName] = true
		fmt.Printf("  TA: %s (%s) -> %s/%s\n", taName, taEmail, class.college, class.name)
		tas = append(tas, User{
			Email:            taEmail,
			Role:             "TA",
			CurrentDirectory: "",
			DirectoryTTL:     0,
			Colleges: map[string]Classroom{
				class.college: {
					Classes: map[string]Class{
						class.name: {
							Role:          "TA",
							Folders:       []string{"announcements", "assignments"},
							SharedFolders: []string{"announcements", "assignments"},
						},
					},
				},
			}})
		var taName2 string
		for {
			taName2 = randomName()
			if !usedNames[taName2] {
				usedNames[taName2] = true
				break
			}
		}
		taEmail2 := randomEmail(taName2)
		tas = append(tas, User{
			Email:            taEmail2,
			Role:             "TA",
			CurrentDirectory: "",
			DirectoryTTL:     0,
			Colleges: map[string]Classroom{
				class.college: {
					Classes: map[string]Class{
						class.name: {
							Role:          "TA",
							Folders:       []string{"announcements", "assignments"},
							SharedFolders: []string{"announcements", "assignments"},
						},
					},
				},
			}})
		fmt.Printf("  TA: %s (%s) -> %s/%s\n", taName2, taEmail2, class.college, class.name)

		// Create 10 students
		studentEmails := make([]string, 0, 10)
		for j := 0; j < 10; j++ {
			var studentName string
			var firstName string
			for {
				studentName = randomName()
				firstName = strings.ToLower(strings.Split(studentName, " ")[0])
				if !usedNames[studentName] && !usedFirstNames[firstName] {
					usedNames[studentName] = true
					usedFirstNames[firstName] = true
					break
				}
			}
			studentEmail := randomEmail(studentName)
			fmt.Printf("  Student: %s (%s) folder: %s\n", studentName, studentEmail, firstName)
			students[i*10+j] = User{
				Email:            studentEmail,
				Role:             "student",
				CurrentDirectory: "",
				DirectoryTTL:     0,
				Colleges: map[string]Classroom{
					class.college: {
						Classes: map[string]Class{
							class.name: {
								Role:          "student",
								Folders:       []string{firstName, firstName + "/homework", firstName + "/notes"},
								SharedFolders: []string{"announcements", "assignments"},
							},
						},
					},
				},
			}
			mdFolders = append(mdFolders, Metadata{
				PK:           class.name,
				SK:           firstName + "/",
				Name:         firstName,
				Owner:        studentEmail,
				LastModified: time.Now(),
				Type:         "folder",
				FullPath:     fmt.Sprintf("%s/%s/%s/", class.college, class.name, firstName),
			})
			mdFolders = append(mdFolders, Metadata{
				PK:           class.name,
				SK:           firstName + "/homework/",
				Name:         "homework",
				Owner:        studentEmail,
				LastModified: time.Now(),
				Type:         "folder",
				FullPath:     fmt.Sprintf("%s/%s/%s/homework/", class.college, class.name, firstName),
			})
			mdFolders = append(mdFolders, Metadata{
				PK:           class.name,
				SK:           firstName + "/notes/",
				Name:         "notes",
				Owner:        studentEmail,
				LastModified: time.Now(),
				Type:         "folder",
				FullPath:     fmt.Sprintf("%s/%s/%s/notes/", class.college, class.name, firstName),
			})
			studentEmails = append(studentEmails, studentEmail)
		}
		entries = append(entries, ClassInfo{
			PK:            class.name,
			SK:            "class_info",
			SharedFolders: []string{"announcements", "assignments"},
			Professor:     profEmail,
			TAs:           []string{taEmail, taEmail2},
			Students:      studentEmails,
		})
	}
	allUsers := append(students, professors...)
	allUsers = append(allUsers, tas...)
	//Add everything to DynamoDB
	for _, user := range allUsers {
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
	for _, entry := range entries {
		item, err := attributevalue.MarshalMap(entry)
		if err != nil {
			log.Printf("Error marshalling class info for %s: %v", entry.PK, err)
			continue
		}
		_, err = client.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String("classroom_metadata"),
			Item:      item,
		})
		if err != nil {
			log.Printf("Error seeding class info for %s: %v", entry.PK, err)
		} else {
			fmt.Printf("Seeded class info for: %s\n", entry.PK)
		}
	}
	for _, md := range mdFolders {
		item, err := attributevalue.MarshalMap(md)
		if err != nil {
			log.Printf("Error marshalling metadata for %s/%s: %v", md.PK, md.SK, err)
			continue
		}
		_, err = client.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String("classroom_metadata"),
			Item:      item,
		})
		if err != nil {
			log.Printf("Error seeding metadata for %s/%s: %v", md.PK, md.SK, err)
		} else {
			fmt.Printf("Seeded metadata for: %s/%s\n", md.PK, md.SK)
		}
	}
}
