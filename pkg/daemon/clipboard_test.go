package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/phinze/bankshot/pkg/clipboard"
	"github.com/phinze/bankshot/pkg/protocol"
)

// fakeClipboard holds png, or nothing when it's nil.
type fakeClipboard struct{ png []byte }

func (f fakeClipboard) HasImage(context.Context) (bool, error) { return f.png != nil, nil }

func (f fakeClipboard) PNG(context.Context) ([]byte, error) {
	if f.png == nil {
		return nil, clipboard.ErrNoImage
	}
	return f.png, nil
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
