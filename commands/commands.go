package commands

import (
	"fmt"
)

// All commands share the same signature: they receive a slice of args
type CommandMap struct {
	Commands map[string]func(args []string)
}

func change_dir(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: cd <directory>")
		return
	}
	fmt.Printf("change_dir to %s\n", args[0])
}

func list_dir(args []string) {
	fmt.Println("list_dir")
}

func rename_file(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: rename <old_name> <new_name>")
		return
	}
	fmt.Printf("rename %s to %s\n", args[0], args[1])
}

func rename_dir(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: renamedir <old_name> <new_name>")
		return
	}
	fmt.Printf("rename dir %s to %s\n", args[0], args[1])
}

func upload(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: upload <file>")
		return
	}
	fmt.Printf("upload %s\n", args[0])
}

func download(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: download <file>")
		return
	}
	fmt.Printf("download %s\n", args[0])
}

func move(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: move <source> <destination>")
		return
	}
	fmt.Printf("move %s to %s\n", args[0], args[1])
}

func delete_file_folder(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: delete <file_or_folder>")
		return
	}
	fmt.Printf("delete %s\n", args[0])
}

func create(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: mkdir <directory>")
		return
	}
	fmt.Printf("create %s\n", args[0])
}

func RegisterCommands() CommandMap {
	return CommandMap{
		Commands: map[string]func(args []string){
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

func help(args []string) {
	fmt.Println("Available commands:")
	for cmd := range RegisterCommands().Commands {
		fmt.Println("  " + cmd)
	}
}