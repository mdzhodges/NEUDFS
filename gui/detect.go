package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectDefaultServerAddr() string {
	if v := strings.TrimSpace(os.Getenv("NEUDFS_SERVER_ADDR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GRPC_SERVER_ADDR")); v != "" {
		return v
	}

	tfDir := findTerraformDir()
	if tfDir == "" {
		return "127.0.0.1:50051"
	}
	if _, err := os.Stat(filepath.Join(tfDir, "terraform.tfstate")); err != nil {
		return "127.0.0.1:50051"
	}

	out, err := exec.Command("terraform", "-chdir="+tfDir, "output", "-raw", "server_address").Output()
	if err != nil {
		return "127.0.0.1:50051"
	}
	addr := strings.TrimSpace(string(out))
	if addr == "" {
		return "127.0.0.1:50051"
	}
	return addr
}

func findTerraformDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}

	dir := wd
	for range 6 {
		candidate := filepath.Join(dir, "terraform")
		if _, err := os.Stat(filepath.Join(candidate, "main.tf")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
