package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"grpc-server/proto"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type state struct {
	win fyne.Window

	serverAddr string

	emailEntry *widget.Entry
	status     *widget.Label

	statusData     binding.String
	outputData     binding.String
	currentDirData binding.String

	loginView fyne.CanvasObject
	mainView  fyne.CanvasObject

	serverLabel *widget.Label
	emailLabel  *widget.Label

	output       *widget.Entry
	outputScroll *container.Scroll

	entries       []string
	currentDir    string
	entryRows     []*entryRow
	entriesBox    *fyne.Container
	entriesScroll *container.Scroll

	selectedIndex int

	cdEntry         *widget.Entry
	mkdirEntry      *widget.Entry
	renameEntry     *widget.Entry
	renameNameEntry *widget.Entry
	deleteEntry     *widget.Entry
	downloadEntry   *widget.Entry
	uploadNameEntry *widget.Entry

	conn   *grpc.ClientConn
	client proto.ServerClient
}

func newState(w fyne.Window) *state {
	return &state{win: w, selectedIndex: -1}
}

func (s *state) queueUI(fn func()) {
	// Many Fyne window implementations support QueueEvent() to safely run code on the UI thread.
	// Use it when available; otherwise fall back to direct execution.
	if w, ok := s.win.(interface{ QueueEvent(func()) }); ok {
		w.QueueEvent(fn)
		return
	}
	fn()
}

func (s *state) runAction(action string, fn func() error) {
	s.setStatus(action + "…")
	if err := fn(); err != nil {
		if st, ok := status.FromError(err); ok {
			s.logf("%s error: %s", action, st.Message())
		} else {
			s.logf("%s error: %v", action, err)
		}
		s.setStatus("Error")
		return
	}
	s.setStatus("OK")
}

func (s *state) setStatus(text string) {
	_ = s.statusData.Set(text)
}

func (s *state) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prev, _ := s.outputData.Get()
	if prev == "" {
		_ = s.outputData.Set(msg)
	} else {
		_ = s.outputData.Set(prev + "\n" + msg)
	}
	s.outputScroll.ScrollToBottom()
}

func (s *state) clientOrErr() (proto.ServerClient, error) {
	email := strings.TrimSpace(s.emailEntry.Text)
	if email == "" {
		return nil, errors.New("email required")
	}
	if s.client == nil || s.conn == nil {
		return nil, errors.New("not connected")
	}
	return s.client, nil
}

func (s *state) rpcCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	email := strings.TrimSpace(s.emailEntry.Text)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("email", email))
	return ctx, cancel
}

func (s *state) login() error {
	_ = s.disconnect()
	addr := strings.TrimSpace(s.serverAddr)
	email := strings.TrimSpace(s.emailEntry.Text)
	if email == "" {
		return errors.New("email required (sent as gRPC metadata key \"email\")")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return err
	}
	s.conn = conn
	s.client = proto.NewServerClient(conn)

	// Mirror the CLI flow: prompt for email, then attempt a real RPC using that email.
	rpcCtx, rpcCancel := s.rpcCtx(8 * time.Second)
	defer rpcCancel()
	_, err = s.client.CurrentDirectory(rpcCtx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		_ = s.disconnect()
		return err
	}

	s.logf("Logged in: %s @ %s", email, addr)
	if s.serverLabel != nil {
		s.serverLabel.SetText(addr)
	}
	if s.emailLabel != nil {
		s.emailLabel.SetText(email)
	}
	s.win.SetContent(s.mainView)
	_ = s.refreshAll()
	return nil
}

func (s *state) disconnect() error {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn = nil
	s.client = nil
	s.entries = nil
	s.clearSelectionAndFields()
	s.refreshEntriesUI()
	_ = s.statusData.Set("Disconnected")
	return nil
}

func (s *state) refreshAll() error {
	if err := s.pwd(); err != nil {
		s.logf("pwd error: %v", err)
	}
	if err := s.ls(); err != nil {
		s.logf("ls error: %v", err)
	}
	return nil
}

func (s *state) pwd() error {
	_, err := s.clientOrErr()
	if err != nil {
		return err
	}
	ctx, cancel := s.rpcCtx(10 * time.Second)
	defer cancel()
	res, err := s.client.CurrentDirectory(ctx, &proto.CurrentDirectoryRequest{})
	if err != nil {
		return err
	}
	dir := res.GetDirectory()
	_ = s.currentDirData.Set(dir)
	if dir != s.currentDir {
		s.currentDir = dir
		s.clearSelectionAndFields()
	}
	s.logf("pwd: %s", dir)
	return nil
}

func (s *state) ls() error {
	_, err := s.clientOrErr()
	if err != nil {
		return err
	}
	ctx, cancel := s.rpcCtx(15 * time.Second)
	defer cancel()
	res, err := s.client.ListDirectory(ctx, &proto.ListDirectoryRequest{})
	if err != nil {
		return err
	}
	s.entries = append([]string(nil), res.GetEntries()...)
	s.refreshEntriesUI()
	s.logf("ls: %d entries", len(res.GetEntries()))
	return nil
}

func (s *state) tree() error {
	_, err := s.clientOrErr()
	if err != nil {
		return err
	}
	ctx, cancel := s.rpcCtx(30 * time.Second)
	defer cancel()
	res, err := s.client.TreeDirectory(ctx, &proto.TreeDirectoryRequest{})
	if err != nil {
		return err
	}
	s.entries = append([]string(nil), res.GetEntries()...)
	s.refreshEntriesUI()
	s.logf("tree: %d entries", len(res.GetEntries()))
	return nil
}
