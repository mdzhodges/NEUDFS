package main

import (
	"bufio"
	"fmt"
	"os"
	"NEUDFS/commands"
	"strings"
)

func main() {
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