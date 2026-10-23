package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phinze/bankshot/pkg/clipboard"
	"github.com/phinze/bankshot/pkg/protocol"
)

// fakeClipboard holds png, or a copied file at path, or nothing.
type fakeClipboard struct {
	png  []byte
	path string
}

func (f fakeClipboard) HasImage(context.Context) (bool, error) { return f.png != nil, nil }

func (f fakeClipboard) PNG(context.Context) ([]byte, error) {
	if f.png == nil {
		return nil, clipboard.ErrNoImage
	}
	return f.png, nil
}

func (f fakeClipboard) File(context.Context) (string, error) {
	if f.path == "" {
		return "", clipboard.ErrNoFile
	}
	return f.path, nil
}

func TestClipboardCommand(t *testing.T) {
	d := newTailnetDaemon(t)
	ask := func(peek bool) protocol.ClipboardResponse {
		t.Helper()
		resp := d.handleCommand(request(t, protocol.CommandClipboard, protocol.ClipboardRequest{Peek: peek}), nil)
		if !resp.Success {
			t.Fatalf("clipboard request failed: %s", resp.Error)
		}
		var clipResp protocol.ClipboardResponse
		if err := json.Unmarshal(resp.Data, &clipResp); err != nil {
			t.Fatal(err)
		}
		return clipResp
	}

	d.clipboard = fakeClipboard{}
	if got := ask(false); got.Image || got.Data != nil {
		t.Errorf("empty clipboard answered %+v, want no image", got)
	}
	if got := ask(true); got.Image {
		t.Error("peek at an empty clipboard found an image")
	}

	d.clipboard = fakeClipboard{png: []byte("png bytes")}
	if got := ask(true); !got.Image || got.Data != nil {
		t.Errorf("peek answered %+v, want an image and no bytes", got)
	}
	if got := ask(false); !got.Image || string(got.Data) != "png bytes" {
		t.Errorf("read answered %+v, want the image", got)
	}
}

func TestClipboardFileToInbox(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	d := newTailnetDaemon(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.handleConnection(conn, nil)
		}
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &Monitor{logger: logger}
	client := &localDaemonClient{network: "tcp", address: ln.Addr().String(), logger: logger}
	paste := func() (*protocol.ClipboardFileResponse, error) {
		t.Helper()
		return m.receiveClipboardFile(&protocol.Request{ID: "t", Type: protocol.CommandClipboardFile}, client)
	}

	d.clipboard = fakeClipboard{}
	if file, err := paste(); err != nil || file.Path != "" {
		t.Errorf("empty clipboard pasted %+v, %v; want nothing", file, err)
	}

	d.clipboard = fakeClipboard{png: []byte("png bytes")}
	file, err := paste()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file.Path); string(got) != "png bytes" || !strings.HasPrefix(filepath.Base(file.Path), "clipboard-") {
		t.Errorf("image pasted to %s holding %q", file.Path, got)
	}

	dir := t.TempDir()
	video := filepath.Join(dir, "2026-10-23 at 2.06.15 PM.mp4")
	if err := os.WriteFile(video, []byte("video bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.clipboard = fakeClipboard{path: video}
	if file, err = paste(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file.Path); string(got) != "video bytes" || !strings.HasSuffix(file.Path, ".mp4") || strings.Contains(file.Path, " ") {
		t.Errorf("video pasted to %s holding %q", file.Path, got)
	}

	doc := filepath.Join(dir, "taxes.pdf")
	if err := os.WriteFile(doc, []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.clipboard = fakeClipboard{path: doc}
	if _, err := paste(); err == nil {
		t.Error("a copied PDF was sent, want refused")
	}
	d.clipboard = fakeClipboard{path: dir}
	if _, err := paste(); err == nil {
		t.Error("a copied directory was sent, want refused")
	}
}
