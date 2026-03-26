package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"grpc-server/client/commands"
	"grpc-server/internal/storage"
	"grpc-server/proto"
	"log"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
	//CLI client will grab address of gRPC server during compile time, or default to localhost
	serverAddr := flag.String("addr", "localhost:50051", "server address")
	flag.Parse()
	//Setting up connection to gRPC server w/out TLS
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	//Set up grpc session
	conn, err := grpc.NewClient(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	// Init Scanner
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Enter your student email: ")
	scanner.Scan()
	email := strings.Fields(scanner.Text())[0]
	client := proto.NewServerClient(conn)
	// Register Commands
	cmds := commands.RegisterCommands(client, email)

	fmt.Println("Available commands:")
	for cmd := range cmds.Commands {
		fmt.Println(cmd)
	}

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
