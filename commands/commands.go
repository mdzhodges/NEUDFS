package commands

import (
	"context"
	"fmt"
	// "grpc-server/proto" // add once the proto is set up
)

// All commands share the same signature: they receive a slice of args
type CommandMap struct {
	Commands map[string]CommandFunc
}

// CommandContext holds the dependencies for a command
type CommandContext struct {
	Client      interface{} // change to proto.ServerClient
	Ctx         context.Context
	CurrentPath *string // Pointer so all commands share and update the same string
}

// Every command now knows about the server and the path
type CommandFunc func(cmdCtx CommandContext, args []string)

func change_dir(cmdCtx CommandContext, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: cd <directory>")
		return
	}
	fmt.Printf("change_dir to %s\n", args[0])
}

func list_dir(cmdCtx CommandContext, args []string) {
	fmt.Println("list_dir")
}

func rename_file(cmdCtx CommandContext, args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: rename <old_name> <new_name>")
		return
	}
	fmt.Printf("rename %s to %s\n", args[0], args[1])
}

func rename_dir(cmdCtx CommandContext, args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: renamedir <old_name> <new_name>")
		return
	}
	fmt.Printf("rename dir %s to %s\n", args[0], args[1])
}

func upload(cmdCtx CommandContext, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: upload <file>")
		return
	}
	fmt.Printf("upload %s\n", args[0])
}

func download(cmdCtx CommandContext, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: download <file>")
		return
	}
	fmt.Printf("download %s\n", args[0])
}

func move(cmdCtx CommandContext, args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: move <source> <destination>")
		return
	}
	fmt.Printf("move %s to %s\n", args[0], args[1])
}

func delete_file_folder(cmdCtx CommandContext, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: delete <file_or_folder>")
		return
	}
	fmt.Printf("delete %s\n", args[0])
}

func create(cmdCtx CommandContext, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: mkdir <directory>")
		return
	}
	fmt.Printf("create %s\n", args[0])
}

func RegisterCommands() CommandMap {
	return CommandMap{
		Commands: map[string]CommandFunc{
			"cd":       change_dir,
			"ls":       list_dir,
			"rename":   rename_file,
			"mkdir":    create,
			"upload":   upload,
			"download": download,
			"move":     move,
			"delete":   delete_file_folder,
			"--help":   help,
		},
	}
}

func help(cmdCtx CommandContext, args []string) {
	fmt.Println("Available commands:")
	for cmd := range RegisterCommands().Commands {
		fmt.Println("  " + cmd)
	}
}
