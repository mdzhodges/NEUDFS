package commands

import (
	"context"
	"fmt"
	"grpc-server/proto"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
		// Cleanly extract just the description from the gRPC error
		if st, ok := status.FromError(err); ok {
			fmt.Println("Error:", st.Message())
		} else {
			fmt.Println("Error:", err.Error())
		}
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

	// format as directory paths by ensuring they end with "/"
	oldDir := args[0]
	newDir := args[1]
	if !strings.HasSuffix(oldDir, "/") {
		oldDir += "/"
	}
	if !strings.HasSuffix(newDir, "/") {
		newDir += "/"
	}

	// set up the context with the user's email
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	// Create the request
	in := proto.RenameRequest{
		Entry: oldDir,
		Name:  newDir,
	}

	// Send the request to gRPC endpoint
	message, err := c.Client.RenameDirectory(ctx, &in)
	if err != nil {
		// Cleanly extract just the description from the gRPC error
		if st, ok := status.FromError(err); ok {
			fmt.Println("Error:", st.Message())
		} else {
			// Fallback just in case it's a non-gRPC error (like a network drop)
			fmt.Println("Error:", err.Error())
		}
		return
	}

	fmt.Println(message.Message)
}

func (c *CommandMap) upload(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: upload <file>")
		return
	}
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	contentType := mime.TypeByExtension(filepath.Ext(args[0]))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	file, err := os.Open(args[0])
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()
	stream, err := c.Client.Upload(ctx)
	err = stream.Send(&proto.UploadRequest{
		Request: &proto.UploadRequest_Metadata{
			Metadata: &proto.UploadMetadata{
				Name:        args[0],
				ContentType: contentType,
			},
		},
	})
	if err != nil {
		fmt.Printf("Error sending metadata: %v\n", err)
		return
	}

	buf := make([]byte, 64*1024) // 64KB chunks
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			break
		}

		stream.Send(&proto.UploadRequest{
			Request: &proto.UploadRequest_Chunk{
				Chunk: buf[:n],
			},
		})
	}

	// Close and get response
	_, err = stream.CloseAndRecv()
	if err != nil {
		fmt.Printf("Error uploading file: %v\n", err)
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
		"cd":        cm.change_dir,
		"ls":        cm.list_dir,
		"rename":    cm.rename_file,
		"renamedir": cm.rename_dir,
		"mkdir":     cm.create,
		"upload":    cm.upload,
		"download":  cm.download,
		"move":      cm.move,
		"delete":    cm.delete_file_folder,
		"--help":    cm.help,
		"clear":     cm.clear,
		"pwd":       cm.pwd,
	}
	return cm
}

func (c *CommandMap) clear(args []string) {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func (c *CommandMap) help(args []string) {
	fmt.Println("Available commands:")
	for cmd := range c.Commands {
		fmt.Println("  " + cmd)
	}
}
