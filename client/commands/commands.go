package commands

import (
	"context"
	"fmt"
	"grpc-server/proto"

	"google.golang.org/grpc/metadata"
)

// All commands share the same signature: they receive a slice of args
type CommandMap struct {
	Commands  map[string]func(args []string)
	Client    proto.ServerClient
	UserEmail string
}

func (c *CommandMap) change_dir(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: cd <directory>")
		return
	}
	//append user email to metadata context
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	//create gRPC request
	in := proto.ChangeDirectoryRequest{Folder: args[0]}
	//request gRPC server for Changing Directory
	message, err := c.Client.ChangeDirectory(ctx, &in)
	if err != nil {
		fmt.Errorf(err.Error())
		fmt.Println("Please try again")
		return
	}
	fmt.Println(message.Message)
}

func (c *CommandMap) list_dir(args []string) {
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	in := proto.ListDirectoryRequest{}
	message, err := c.Client.ListDirectory(ctx, &in)
	if err != nil {
		fmt.Println(err.Error())
		fmt.Println("Please try again")
		return
	}
	for i := range message.Entries {
		fmt.Println(message.Entries[i])
	}
}

func (c *CommandMap) rename_file(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: rename <old_name> <new_name>")
		return
	}

	// Set up the context with the user's email
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	// Create the gRPC request using the correct protobuf fields
	in := proto.RenameRequest{
		Entry: args[0], 
		Name:  args[1],
	}

	// Send the request to the gRPC server
	message, err := c.Client.Rename(ctx, &in)
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}

	// Print the server's response
	fmt.Println(message.Message)
}

func (c *CommandMap) rename_dir(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: renamedir <old_name> <new_name>")
		return
	}
	fmt.Printf("rename dir %s to %s\n", args[0], args[1])
}

func (c *CommandMap) upload(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: upload <file>")
		return
	}
	fmt.Printf("upload %s\n", args[0])
}

func (c *CommandMap) download(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: download <file>")
		return
	}
	fmt.Printf("download %s\n", args[0])
}

func (c *CommandMap) move(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: move <source> <destination>")
		return
	}
	fmt.Printf("move %s to %s\n", args[0], args[1])
}

func (c *CommandMap) delete_file_folder(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: delete <file_or_folder>")
		return
	}
	fmt.Printf("delete %s\n", args[0])
}

func (c *CommandMap) create(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: mkdir <directory>")
		return
	}
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	in := proto.MakeDirectoryRequest{Name: args[0]}
	message, err := c.Client.MakeDirectory(ctx, &in)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println(message.Message)
}

func (c *CommandMap) pwd(args []string) {
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	in := proto.CurrentDirectoryRequest{}
	message, err := c.Client.CurrentDirectory(ctx, &in)
	if err != nil {
		fmt.Println(err.Error())
		fmt.Println("Please try again")
		return
	}

	fmt.Println(message.Directory)
}

func RegisterCommands(client proto.ServerClient, email string) *CommandMap {
	cm := &CommandMap{
		Client:    client,
		UserEmail: email,
	}
	cm.Commands = map[string]func(args []string){
		"cd":       cm.change_dir,
		"ls":       cm.list_dir,
		"rename":   cm.rename_file,
		"mkdir":    cm.create,
		"upload":   cm.upload,
		"download": cm.download,
		"move":     cm.move,
		"delete":   cm.delete_file_folder,
		"--help":   cm.help,
		"pwd":      cm.pwd,
	}
	return cm
}

func (c *CommandMap) help(args []string) {
	fmt.Println("Available commands:")
	for cmd := range c.Commands {
		fmt.Println("  " + cmd)
	}
}
