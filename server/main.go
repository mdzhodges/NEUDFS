package main

import (
	"context"
	"fmt"
	"grpc-server/proto"
	"sync"
)

// This will log to our metrics in Cloudwatch
func logger(format string, a ...any) {
	fmt.Printf("LOG:\t"+format+"\n", a...)
}

type server struct {
	proto.UnimplementedServerServer
	currentDirectory map[string]string
	mu               sync.RWMutex
}

func (s *server) ChangeDirectory(ctx context.Context, in *proto.ChangeDirectoryRequest) (*proto.ChangeDirectoryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email := ctx.Value("email").(string)
	s.currentDirectory[email] = in.Folder
	msg := fmt.Sprintf("Changing Current Directory to %q\n", in.Folder)
	return &proto.ChangeDirectoryResponse{Message: msg}, nil
}

func main() {

}
