package main

import (
	"errors"
	"strings"
	"time"

	"grpc-server/proto"
)

func (s *state) rename(isDir bool, entry, newName string) error {
	_, err := s.clientOrErr()
	if err != nil {
		return err
	}
	if entry == "" || newName == "" {
		return errors.New("rename: entry and new name required")
	}

	ctx, cancel := s.rpcCtx(20 * time.Second)
	defer cancel()
	req := &proto.RenameRequest{Entry: entry, Name: newName}
	var res *proto.RenameResponse
	if isDir {
		res, err = s.client.RenameDirectory(ctx, req)
	} else {
		res, err = s.client.Rename(ctx, req)
	}
	if err != nil {
		return err
	}
	s.logf("%s", strings.TrimSpace(res.GetMessage()))
	_ = s.refreshAll()
	return nil
}
