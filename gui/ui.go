package main

import (
	"errors"
	"strings"
	"time"

	"grpc-server/proto"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func (s *state) buildUI() {
	s.serverAddr = detectDefaultServerAddr()

	s.emailEntry = widget.NewEntry()
	s.emailEntry.SetPlaceHolder("user email (required)")

	s.statusData = binding.NewString()
	_ = s.statusData.Set("Disconnected")
	s.status = widget.NewLabelWithData(s.statusData)

	s.currentDirData = binding.NewString()
	_ = s.currentDirData.Set("/")

	s.output = widget.NewMultiLineEntry()
	s.output.Wrapping = fyne.TextWrapWord
	s.output.SetPlaceHolder("Output…")
	s.outputData = binding.NewString()
	s.output.Bind(s.outputData)
	s.output.Disable()
	s.outputScroll = container.NewVScroll(s.output)

	s.cdEntry = widget.NewEntry()
	s.cdEntry.SetPlaceHolder("folder (blank = root, .. = parent)")
	s.mkdirEntry = widget.NewEntry()
	s.mkdirEntry.SetPlaceHolder("folder name")
	s.renameEntry = widget.NewEntry()
	s.renameEntry.SetPlaceHolder("entry (file or folder name)")
	s.renameNameEntry = widget.NewEntry()
	s.renameNameEntry.SetPlaceHolder("new name")
	s.deleteEntry = widget.NewEntry()
	s.deleteEntry.SetPlaceHolder("path (file or folder)")
	s.downloadEntry = widget.NewEntry()
	s.downloadEntry.SetPlaceHolder("remote filename")
	s.uploadNameEntry = widget.NewEntry()
	s.uploadNameEntry.SetPlaceHolder("remote name (optional)")

	s.entriesBox = container.NewVBox()
	s.entriesScroll = container.NewVScroll(s.entriesBox)

	s.loginView = s.buildLoginView()
	s.mainView = s.buildMainView()

	s.win.SetContent(s.loginView)
}

func (s *state) buildLoginView() fyne.CanvasObject {
	loginBtn := widget.NewButton("Login", func() {
		s.runAction("login", func() error {
			if err := s.login(); err != nil {
				dialog.ShowError(err, s.win)
				return err
			}
			return nil
		})
	})

	card := widget.NewCard(
		"NEUDFS Login",
		"",
		container.NewVBox(
			container.NewGridWithColumns(2,
				widget.NewLabel("Server"),
				widget.NewLabel(strings.TrimSpace(s.serverAddr)),
				widget.NewLabel("Email"),
				s.emailEntry,
			),
			container.NewHBox(loginBtn, layout.NewSpacer(), s.status),
		),
	)

	return container.NewCenter(container.NewVBox(card))
}

func (s *state) buildMainView() fyne.CanvasObject {
	s.serverLabel = widget.NewLabel(strings.TrimSpace(s.serverAddr))
	s.emailLabel = widget.NewLabel("")
	dirLabel := widget.NewLabelWithData(s.currentDirData)
	dirLabel.Truncation = fyne.TextTruncateEllipsis
	dirLabel.Wrapping = fyne.TextWrapOff
	logoutBtn := widget.NewButton("Logout", func() {
		s.runAction("logout", func() error {
			_ = s.disconnect()
			s.win.SetContent(s.loginView)
			s.logf("Logged out")
			return nil
		})
	})

	topRow := container.NewHBox(
		widget.NewLabelWithStyle("Connected", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		s.serverLabel,
		widget.NewLabel("|"),
		s.emailLabel,
		logoutBtn,
	)
	dirPrefix := widget.NewLabelWithStyle("Dir:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	dirRow := container.NewBorder(nil, nil, dirPrefix, nil, dirLabel)
	header := container.NewVBox(topRow, dirRow)

	pwdBtn := widget.NewButton("pwd", func() { s.runAction("pwd", s.pwd) })
	lsBtn := widget.NewButton("ls", func() { s.runAction("ls", s.ls) })
	treeBtn := widget.NewButton("tree", func() { s.runAction("tree", s.tree) })

	cdBtn := widget.NewButton("cd", func() {
		folder := strings.TrimSpace(s.cdEntry.Text)
		s.runAction("cd", func() error { return s.cd(folder) })
	})
	backBtn := widget.NewButton("←", func() {
		s.runAction("back", func() error { return s.cd("..") })
	})

	mkdirBtn := widget.NewButton("mkdir", func() {
		name := strings.TrimSpace(s.mkdirEntry.Text)
		s.runAction("mkdir", func() error {
			_, err := s.clientOrErr()
			if err != nil {
				return err
			}
			ctx, cancel := s.rpcCtx(15 * time.Second)
			defer cancel()
			res, err := s.client.MakeDirectory(ctx, &proto.MakeDirectoryRequest{Name: name})
			if err != nil {
				return err
			}
			s.logf("%s", strings.TrimSpace(res.GetMessage()))
			_ = s.refreshAll()
			return nil
		})
	})

	renameFileBtn := widget.NewButton("rename file", func() {
		entry := strings.TrimSpace(s.renameEntry.Text)
		newName := strings.TrimSpace(s.renameNameEntry.Text)
		s.runAction("rename", func() error { return s.rename(false, entry, newName) })
	})
	renameDirBtn := widget.NewButton("rename dir", func() {
		entry := strings.TrimSpace(s.renameEntry.Text)
		newName := strings.TrimSpace(s.renameNameEntry.Text)
		s.runAction("renamedir", func() error { return s.rename(true, entry, newName) })
	})
	deleteBtn := widget.NewButton("delete", func() {
		path := strings.TrimSpace(s.deleteEntry.Text)
		s.runAction("delete", func() error {
			_, err := s.clientOrErr()
			if err != nil {
				return err
			}
			ctx, cancel := s.rpcCtx(25 * time.Second)
			defer cancel()
			res, err := s.client.Delete(ctx, &proto.DeleteRequest{Path: path})
			if err != nil {
				return err
			}
			s.logf("%s", strings.TrimSpace(res.GetMessage()))
			_ = s.refreshAll()
			return nil
		})
	})

	downloadBtn := widget.NewButton("download…", func() {
		remote := strings.TrimSpace(s.downloadEntry.Text)
		s.runAction("download", func() error {
			_, err := s.clientOrErr()
			if err != nil {
				return err
			}
			if remote == "" {
				return errors.New("download: remote filename required")
			}
			return s.downloadWithDialog(remote)
		})
	})

	openBtn := widget.NewButton("open…", func() {
		remote := strings.TrimSpace(s.downloadEntry.Text)
		s.runAction("open", func() error {
			_, err := s.clientOrErr()
			if err != nil {
				return err
			}
			if remote == "" {
				return errors.New("open: remote filename required")
			}
			return s.openWithDialog(remote)
		})
	})

	uploadBtn := widget.NewButton("upload…", func() {
		s.runAction("upload", func() error {
			_, err := s.clientOrErr()
			if err != nil {
				return err
			}
			return s.uploadWithDialog(strings.TrimSpace(s.uploadNameEntry.Text))
		})
	})

	left := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Directory", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(pwdBtn, lsBtn, treeBtn),
		),
		container.NewVBox(
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Navigation", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, backBtn, cdBtn, s.cdEntry),
			container.NewBorder(nil, nil, nil, mkdirBtn, s.mkdirEntry),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Rename", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewGridWithColumns(2, s.renameEntry, s.renameNameEntry),
			container.NewHBox(renameFileBtn, renameDirBtn),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Delete", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, nil, deleteBtn, s.deleteEntry),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Transfer", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, nil, container.NewHBox(downloadBtn, openBtn), s.downloadEntry),
			container.NewBorder(nil, nil, nil, uploadBtn, s.uploadNameEntry),
		),
		nil,
		nil,
		container.NewBorder(nil, nil, nil, nil, s.entriesScroll),
	)

	split := container.NewHSplit(left, s.outputScroll)
	split.Offset = 0.62

	return container.NewBorder(header, nil, nil, nil, split)
}

func (s *state) cd(folder string) error {
	_, err := s.clientOrErr()
	if err != nil {
		return err
	}
	ctx, cancel := s.rpcCtx(10 * time.Second)
	defer cancel()
	_, err = s.client.ChangeDirectory(ctx, &proto.ChangeDirectoryRequest{Folder: folder})
	if err != nil {
		return err
	}
	s.logf("cd %q OK", folder)
	_ = s.refreshAll()
	return nil
}
