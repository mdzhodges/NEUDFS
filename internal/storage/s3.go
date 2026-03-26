package storage

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Store holds our S3 connection tools
type S3Store struct {
	S3Client   *s3.Client
	BucketName string
}

// helper function to set up our S3Store
func NewS3Store(ctx context.Context, nameOfBucket string) (*S3Store, error) {

	// Load the AWS settings like credentials and region
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		// explain exactly what happened if something goes wrong
		return nil, fmt.Errorf("could not load AWS settings: %v", err)
	}

	// Create the actual S3 client tool using those settings
	clientTool := s3.NewFromConfig(awsConfig)

	// Put the tool and the bucket name into our S3Store box and return it
	newStore := &S3Store{
		S3Client:   clientTool,
		BucketName: nameOfBucket,
	}

	return newStore, nil
}

// takes data and puts it into the specific folder structure we planned
func (s *S3Store) UploadFile(ctx context.Context, classID string, studentID string, fileName string, fileData []byte) error {

	// Create the path string: Khoury/class/CS6650/jordan.g/project.zip
	s3Path := fmt.Sprintf("Khoury/class/%s/%s/%s", classID, studentID, fileName)

	// Prepare the "PutObject" request for AWS
	uploadRequest := &s3.PutObjectInput{
		Bucket: aws.String(s.BucketName), // AWS requires pointers to strings
		Key:    aws.String(s3Path),
		Body:   bytes.NewReader(fileData), // Turns our byte slice into a readable stream
	}

	// Send the request to S3
	_, err := s.S3Client.PutObject(ctx, uploadRequest)
	if err != nil {
		return fmt.Errorf("error: could not upload file to path %s. Details: %v", s3Path, err)
	}

	fmt.Printf("Success! File uploaded to %s\n", s3Path)
	return nil
}
