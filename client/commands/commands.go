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
	"runtime"
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
		fmt.Println(err.Error())
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
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	stream, err := c.Client.Download(ctx, &proto.DownloadRequest{Name: args[0]})
	if err != nil {
		fmt.Printf("Error initiating download: %v\n", err)
		return
	}
	file, err := os.Create(args[0])
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer file.Close()
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("Error receiving file chunk: %v\n", err)
			return
		}
		file.Write(resp.Data)
	}
	fmt.Printf("downloaded %s\n", args[0])
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
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	in := proto.DeleteRequest{Path: args[0]}
	message, err := c.Client.Delete(ctx, &in)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			fmt.Println("Error:", st.Message())
		} else {
			fmt.Println("Error:", err.Error())
		}
		return
	}
	fmt.Println(message.Message)
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
func (c *CommandMap) open(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: open <file>")
		return
	}
	c.download(args)
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", args[0]).Start()
	case "linux":
		exec.Command("xdg-open", args[0]).Start()
	case "windows":
		exec.Command("cmd", "/c", "start", args[0]).Start()
	}
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
		"mkdir":    cm.create,
		"upload":   cm.upload,
		"download": cm.download,
		"move":     cm.move,
		"delete":   cm.delete_file_folder,
		"--help":   cm.help,
		"clear":    cm.clear,
		"pwd":      cm.pwd,
		"tree":     cm.tree,
		"open":      cm.open,
	}
	return cm
}

func (c *CommandMap) tree(args []string) {
	md := metadata.New(map[string]string{"email": c.UserEmail})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	message, err := c.Client.TreeDirectory(ctx, &proto.TreeDirectoryRequest{})
	if err != nil {
		if st, ok := status.FromError(err); ok {
			fmt.Println("Error:", st.Message())
		} else {
			fmt.Println("Error:", err.Error())
		}
		return
	}
	fmt.Println(".")
	renderTree(message.Entries, "", "")
}

// renderTree prints entries with tree-style branch characters.
// entries are relative paths like "bob/", "bob/hw1/", "bob/hw1/main.go"
// pathPrefix is the current directory being expanded, visualPrefix is the indentation string.
func renderTree(entries []string, pathPrefix, visualPrefix string) {
	seen := make(map[string]bool)
	var children []string
	for _, e := range entries {
		if !strings.HasPrefix(e, pathPrefix) {
			continue
		}
		trimmed := strings.TrimPrefix(e, pathPrefix)
		if trimmed == "" {
			continue
		}
		// Take everything up to and including the first slash (directory),
		// or the full string if there is no slash (file).
		idx := strings.Index(trimmed, "/")
		var name string
		if idx == -1 {
			name = trimmed
		} else {
			name = trimmed[:idx+1]
		}
		if !seen[name] {
			seen[name] = true
			children = append(children, name)
		}
	}

	for i, child := range children {
		isLast := i == len(children)-1
		connector := "├── "
		childVisualPrefix := visualPrefix + "│   "
		if isLast {
			connector = "└── "
			childVisualPrefix = visualPrefix + "    "
		}
		fmt.Println(visualPrefix + connector + child)

		if strings.HasSuffix(child, "/") {
			renderTree(entries, pathPrefix+child, childVisualPrefix)
		}
	}
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
