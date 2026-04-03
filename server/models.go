package main

import "time"

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
	Email    string               `dynamodbav:"email"`
	Role     string               `dynamodbav:"role"`
	Colleges map[string]Classroom `dynamodbav:"colleges"`
}
