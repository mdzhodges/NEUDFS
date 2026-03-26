package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"grpc-server/commands"
	"grpc-server/internal/storage"
)

// testS3Connection is a helper function to make sure we can talk to AWS
func testS3Connection() {
    fmt.Println("--- STARTING S3 CHECK ---")
    
    // Create a "Context," this as a timer for our request
    // If the upload takes too long, the context can cancel it
    myContext := context.Background()

    // Tells which bucket to look for
	// should match terraform
    myBucketName := "neudfs-storage-dev" 

    // Initialize S3 storage tool
    // This looks for your credentials in ~/.aws/credentials automatically
    myStore, err := storage.NewS3Store(myContext, myBucketName)
    if err != nil {
        log.Fatalf("Oops! Could not start S3: %v", err)
    }

    // Create fake data to test the upload
    testWords := []byte("This is a test file for the NEUDFS project.")
    myClass := "CS6650"
    myID := "glass.j"
    myFileName := "connection_test.txt"

    // Try to upload the file using the path
	// Example filepath: s3://neudfs-bucket/users/jordanglass/assignments/hw1.txt
    // Or local source: /Users/jordanglass/School/CS6650-HW/test.txt
    fmt.Println("Sending file to Amazon S3...")
    err = myStore.UploadFile(myContext, myClass, myID, myFileName, testWords)
    if err != nil {
        // If it fails, log.Fatalf will print the error and STOP the program.
        log.Fatalf("Upload failed! Error: %v", err)
    }

    fmt.Println("--- S3 CHECK PASSED! ---")
}

func main() {
	// Test S3 Connections
	// testS3Connection()

	// Register Commands
	cmds := commands.RegisterCommands()

	fmt.Println("Available commands:")
	for cmd := range cmds.Commands {
		fmt.Println(cmd)
	}

	// Init Scanner
	scanner := bufio.NewScanner(os.Stdin)
	for fmt.Print("Enter command: "); scanner.Scan(); fmt.Print("Enter command: ") {
		// Grab arguments
		args := strings.Fields(scanner.Text())
		if len(args) == 0 {
			continue
		}
		// Find Command
		cmd, ok := cmds.Commands[args[0]]
		if !ok {
			fmt.Printf("Invalid command entry %s\n", args[0])
			continue
		}
		// Execute Command
		cmd(args[1:])
	}
}