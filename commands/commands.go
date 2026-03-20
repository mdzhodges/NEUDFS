package commands

import (
	"fmt"
	"os")

type Commands func(args []string)

func change_dir() {
	return fmt.println("change_dir")
}

func list_dir() {
	return fmt.println("list_dir")
}

func rename_file() {
	return fmt.println("rename file")
}

func rename_dir() {
	// cant be students home directory
	return fmt.println("Rename directory")
}

func upload() {
	// upload file or function
	return fmt.println("upload")
}

func download() {
	// download from server --> host
	return fmt.println("download")
}

func move() {
	// move file or directory
	return fmt.println("move")
}


func delete_file_folder() {
	// cannot delete student root or anything above that
	return fmt.println("delete")
}


func create(){
	return fmt.println("create")
}




func Main(){
	fmt.println("Main")
}



