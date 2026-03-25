package commands

import (
	"fmt"
	"grpc-server/proto"
)

// All commands share the same signature: they receive a slice of args
type CommandMap struct {
	Commands map[string]func(args []string)
	Client   proto.ServerClient
}

func (c *CommandMap) change_dir(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: cd <directory>")
		return
	}
	fmt.Printf("change_dir to %s\n", args[0])
}

func (c *CommandMap) list_dir(args []string) {
	fmt.Println("list_dir")
}

func (c *CommandMap) rename_file(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: rename <old_name> <new_name>")
		return
	}
	fmt.Printf("rename %s to %s\n", args[0], args[1])
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
	fmt.Printf("create %s\n", args[0])
}

func RegisterCommands(client proto.ServerClient) CommandMap {
	var cm CommandMap
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
	}
	cm.Client = client
	return cm
}

func (c *CommandMap) help(args []string) {
	fmt.Println("Available commands:")
	for cmd := range c.Commands {
		fmt.Println("  " + cmd)
	}
}
