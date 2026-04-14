package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"grpc-server/proto"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (s *state) downloadWithDialog(remote string) error {
	save := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil {
			s.logf("save dialog error: %v", err)
			return
		}
		if wc == nil {
			s.logf("download canceled")
			return
		}
		localPath := wc.URI().Path()
		go func() {
			defer wc.Close()
			ctx, cancel := s.rpcCtx(10 * time.Minute)
			defer cancel()
			stream, err := s.client.Download(ctx, &proto.DownloadRequest{Name: remote})
			if err != nil {
				s.logf("download error: %v", err)
				return
			}
			var total int64
			for {
				msg, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					s.logf("download recv error: %v", err)
					return
				}
				n, werr := wc.Write(msg.GetData())
				total += int64(n)
				if werr != nil {
					s.logf("download write error: %v", werr)
					return
				}
			}
			s.logf("downloaded %q (%d bytes) -> %s", remote, total, localPath)
		}()
	}, s.win)
	save.SetFileName(filepath.Base(remote))
	save.Show()
	return nil
}

func (s *state) uploadWithDialog(remoteOverride string) error {
	open := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil {
			s.logf("open dialog error: %v", err)
			return
		}
		if rc == nil {
			s.logf("upload canceled")
			return
		}

		go func() {
			defer rc.Close()

			localPath := rc.URI().Path()
			remote := remoteOverride
			if remote == "" {
				remote = filepath.Base(localPath)
			}
			ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(remote)))
			if ct == "" {
				ct = "application/octet-stream"
			}

			ctx, cancel := s.rpcCtx(10 * time.Minute)
			defer cancel()
			stream, err := s.client.Upload(ctx)
			if err != nil {
				s.logf("upload error: %v", err)
				return
			}
			if err := stream.Send(&proto.UploadRequest{
				Request: &proto.UploadRequest_Metadata{
					Metadata: &proto.UploadMetadata{Name: remote, ContentType: ct},
				},
			}); err != nil {
				s.logf("upload send metadata error: %v", err)
				return
			}

			buf := make([]byte, 64*1024)
			var total int64
			for {
				n, rerr := rc.Read(buf)
				if n > 0 {
					total += int64(n)
					if err := stream.Send(&proto.UploadRequest{
						Request: &proto.UploadRequest_Chunk{Chunk: buf[:n]},
					}); err != nil {
						s.logf("upload send chunk error: %v", err)
						return
					}
				}
				if errors.Is(rerr, io.EOF) {
					break
				}
				if rerr != nil {
					s.logf("upload read error: %v", rerr)
					return
				}
			}
			res, err := stream.CloseAndRecv()
			if err != nil {
				s.logf("upload close error: %v", err)
				return
			}
			s.logf("uploaded %q (%d bytes): %s", remote, total, strings.TrimSpace(res.GetMessage()))

			// Auto-refresh listing so the upload shows up immediately.
			if s.client == nil {
				return
			}
			lsCtx, lsCancel := s.rpcCtx(15 * time.Second)
			defer lsCancel()
			lsRes, err := s.client.ListDirectory(lsCtx, &proto.ListDirectoryRequest{})
			if err != nil {
				s.logf("ls after upload error: %v", err)
				return
			}
			entries := append([]string(nil), lsRes.GetEntries()...)
			s.queueUI(func() {
				s.entries = entries
				s.refreshEntriesUI()
				s.logf("ls: %d entries", len(entries))
			})
		}()
	}, s.win)
	open.Show()
	return nil
}

func (s *state) openWithDialog(remote string) error {
	save := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil {
			s.logf("save dialog error: %v", err)
			return
		}
		if wc == nil {
			s.logf("open canceled")
			return
		}
		localPath := wc.URI().Path()

		go func() {
			ctx, cancel := s.rpcCtx(10 * time.Minute)
			defer cancel()
			stream, err := s.client.Download(ctx, &proto.DownloadRequest{Name: remote})
			if err != nil {
				s.logf("open download error: %v", err)
				_ = wc.Close()
				return
			}
			var total int64
			for {
				msg, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					s.logf("open download recv error: %v", err)
					_ = wc.Close()
					return
				}
				n, werr := wc.Write(msg.GetData())
				total += int64(n)
				if werr != nil {
					s.logf("open write error: %v", werr)
					_ = wc.Close()
					return
				}
			}

			if cerr := wc.Close(); cerr != nil {
				s.logf("open close error: %v", cerr)
				return
			}
			if _, statErr := os.Stat(localPath); statErr != nil {
				s.logf("open stat error: %v", statErr)
				return
			}

			s.logf("opened %q (%d bytes) -> %s", remote, total, localPath)
			method, details, err := openLocalFile(localPath)
			if err != nil {
				s.logf("open error: %v", err)
				s.viewFileInApp(localPath, remote)
				return
			}
			s.logf("open launched via %s", method)
			if strings.TrimSpace(details) != "" {
				s.logf("open details: %s", strings.TrimSpace(details))
			}

			// If the system opener claims success but nothing appears, show a viewer fallback.
			// This makes "open" reliable for texty files and in minimal desktop environments.
			s.viewFileInApp(localPath, remote)
		}()
	}, s.win)
	save.SetFileName(filepath.Base(remote))
	save.Show()
	return nil
}

func openLocalFile(path string) (method string, details string, err error) {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}

	// Prefer OS-native openers with a filesystem path.
	switch runtime.GOOS {
	case "darwin":
		out, err := runOpenCommand("open", []string{path})
		if err == nil {
			return "open", out, nil
		}
	case "linux":
		if _, err := exec.LookPath("xdg-open"); err == nil {
			out, err := runOpenCommand("xdg-open", []string{path})
			if err == nil && !looksLikeNoHandler(out) {
				return "xdg-open", out, nil
			}
			if err != nil {
				details = out
			}
		}
		if _, err := exec.LookPath("gio"); err == nil {
			out, err := runOpenCommand("gio", []string{"open", path})
			if err == nil && !looksLikeNoHandler(out) {
				return "gio open", out, nil
			}
			if details == "" && err != nil {
				details = out
			}
		}
		// keep going to file:// fallback
	case "windows":
		out, err := runOpenCommand("cmd", []string{"/c", "start", "", path})
		if err == nil && !looksLikeNoHandler(out) {
			return "cmd start", out, nil
		}
	default:
		return "", "", fmt.Errorf("open not supported on %s", runtime.GOOS)
	}

	// Fallback: use Fyne's OpenURL with a file:// URL.
	if app := fyne.CurrentApp(); app != nil {
		u := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
		if runtime.GOOS == "windows" && !strings.HasPrefix(u.Path, "/") {
			u.Path = "/" + u.Path
		}
		if err := app.OpenURL(u); err == nil {
			return "fyne OpenURL(file://)", "", nil
		}
	}

	if strings.TrimSpace(details) != "" {
		return "", details, errors.New("unable to launch an opener for this file")
	}
	return "", "", errors.New("unable to launch an opener for this file")
}

func runOpenCommand(bin string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("%s timed out", bin)
	}
	return string(out), err
}

func looksLikeNoHandler(output string) bool {
	s := strings.ToLower(output)
	return strings.Contains(s, "no method available") ||
		strings.Contains(s, "no application is registered") ||
		strings.Contains(s, "no application") ||
		strings.Contains(s, "not found") && strings.Contains(s, "xdg-open")
}

func (s *state) viewFileInApp(path string, title string) {
	// Best-effort text viewer.
	data, err := os.ReadFile(path)
	if err != nil {
		s.logf("view error: %v", err)
		return
	}
	if len(data) > 2*1024*1024 {
		s.logf("view skipped: file too large (%d bytes)", len(data))
		return
	}
	// If it contains NUL bytes, it's likely binary.
	if bytesContainsZero(data) {
		s.logf("view skipped: binary file")
		return
	}

	w := fyne.CurrentApp().NewWindow("View: " + title)
	w.Resize(fyne.NewSize(900, 600))

	// Allow closing even in environments without window decorations.
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev != nil && ev.Name == fyne.KeyEscape {
			w.Close()
		}
	})
	closeBtn := widget.NewButton("Close", func() { w.Close() })

	// Use TextGrid (not a disabled Entry) so text uses normal foreground color
	// and remains readable with our theme.
	grid := widget.NewTextGridFromString(string(data))
	w.SetContent(container.NewBorder(container.NewHBox(closeBtn), nil, nil, nil, container.NewScroll(grid)))
	w.Show()
}

func bytesContainsZero(b []byte) bool {
	for _, v := range b {
		if v == 0 {
			return true
		}
	}
	return false
}
