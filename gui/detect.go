package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func detectDefaultServerAddr() string {
	const fallback = "127.0.0.1:50051"

	if v := strings.TrimSpace(os.Getenv("NEUDFS_SERVER_ADDR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GRPC_SERVER_ADDR")); v != "" {
		return v
	}

	tfDir := findTerraformDir()
	if tfDir == "" {
		return fallback
	}
	if _, err := os.Stat(filepath.Join(tfDir, "terraform.tfstate")); err != nil {
		return fallback
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "terraform", "-chdir="+tfDir, "output", "-no-color", "-raw", "server_address")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fallback
	}
	addr := strings.TrimSpace(stdout.String())
	if !isLikelyServerAddr(addr) {
		return fallback
	}
	return addr
}

func isLikelyServerAddr(addr string) bool {
	if addr == "" {
		return false
	}
	if strings.Contains(addr, "://") {
		return false
	}
	if strings.Contains(addr, "Warning:") || strings.Contains(addr, "No outputs found") {
		return false
	}
	if strings.ContainsAny(addr, " \t\r\n") {
		return false
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host != "" && port != ""
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
