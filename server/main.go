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
	currentDirectory string
	mu               sync.Mutex
}

func (s *server) ChangeDirectory(ctx context.Context, in *proto.ChangeDirectoryRequest) (*proto.ChangeDirectoryResponse, error) {
	//we implement logic here
	return nil, nil
}

func main() {

}
