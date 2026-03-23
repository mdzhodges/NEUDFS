package commands

import (
	"fmt")

type Commands func(args []string)

func change_dir() {
	fmt.Println("change_dir")
}

func list_dir() {
	fmt.Println("list_dir")
}

func rename_file() {
	fmt.Println("rename file")
}

func rename_dir() {
	// cant be students home directory
	fmt.Println("Rename directory")
}

func upload() {
	// upload file or function
	fmt.Println("upload")
}

func download() {
	// download from server --> host
	fmt.Println("download")
}

func move() {
	// move file or directory
	fmt.Println("move")
}


func delete_file_folder() {
	// cannot delete student root or anything above that
	fmt.Println("delete")
}


func create(){
	fmt.Println("create")
}


func Main(){
	fmt.Println("Main")
}



