package main

import (
	"bufio"
	"flag"
	"fmt"
	"grpc-server/client/commands"
	"grpc-server/proto"
	"log"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
