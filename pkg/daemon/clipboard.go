package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phinze/bankshot/pkg/clipboard"
	"github.com/phinze/bankshot/pkg/protocol"
)

// clipboardFileTypes are the copied files serveClipboardFile will send:
// screenshots and screen recordings, not whatever else was copied in the
// Finder.
var clipboardFileTypes = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".heic": true, ".webp": true,
	".mp4": true, ".mov": true, ".m4v": true, ".webm": true,
}

// clipboardFileMax caps what serveClipboardFile sends, well above a long
// screen recording.
const clipboardFileMax = 200 << 20

// serveClipboardFile answers a request for the clipboard's image, or the
// image or video file copied there, and then sends its bytes on conn.
func (d *Daemon) serveClipboardFile(conn net.Conn, req *protocol.Request, p *peer) {
	fail := func(err error) {
		d.logger.Warn("Refused clipboard file request", "error", err)
		d.sendResponse(conn, protocol.NewErrorResponse(req.ID, err))
	}
	if p != nil {
		if err := d.verifyPeerRequest(req, p); err != nil {
			fail(fmt.Errorf("%s refused: %w", req.Type, err))
			return
		}
	}

	ctx, cancel := context.WithTimeout(d.ctx, clipboardTimeout)
	defer cancel()
	name, body, size, image, err := d.clipboardFile(ctx)
	if err != nil {
		fail(err)
		return
	}
	resp, _ := protocol.NewSuccessResponse(req.ID, protocol.ClipboardFileResponse{Name: name, Size: size})
	d.sendResponse(conn, resp)
	if body == nil {
		return
	}
	defer body.Close()

	origin := ""
	if req.Origin != nil {
		origin = req.Origin.String()
	}
	d.logger.Info("Sending clipboard file", "name", name, "bytes", size, "origin", origin)
	if image {
		d.notifier.NotifyClipboard(origin, int(size))
	} else {
		d.notifier.NotifyClipboardFile(origin, name, size)
	}
	if _, err := io.CopyN(conn, body, size); err != nil {
		d.logger.Warn("Clipboard file send failed", "name", name, "error", err)
	}
}

// clipboardFile opens what's on the clipboard to send: its image as a PNG,
// or else the image or video file copied there. A nil body means nothing;
// image means the PNG, not a file.
func (d *Daemon) clipboardFile(ctx context.Context) (name string, body io.ReadCloser, size int64, image bool, err error) {
	data, err := d.clipboard.PNG(ctx)
	switch {
	case err == nil:
		name = "clipboard-" + time.Now().Format("20060102-150405") + ".png"
		return name, io.NopCloser(bytes.NewReader(data)), int64(len(data)), true, nil
	case !errors.Is(err, clipboard.ErrNoImage):
		return "", nil, 0, false, err
	}

	path, err := d.clipboard.File(ctx)
	switch {
	case errors.Is(err, clipboard.ErrNoFile):
		return "", nil, 0, false, nil
	case err != nil:
		return "", nil, 0, false, err
	}
	name = filepath.Base(path)
	if !clipboardFileTypes[strings.ToLower(filepath.Ext(name))] {
		return "", nil, 0, false, fmt.Errorf("won't send %s: only images and videos leave the clipboard", name)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", nil, 0, false, err
	}
	info, err := f.Stat()
	switch {
	case err != nil:
	case !info.Mode().IsRegular():
		err = fmt.Errorf("won't send %s: not a regular file", name)
	case info.Size() > clipboardFileMax:
		err = fmt.Errorf("won't send %s: %d MB is over the %d MB limit", name, info.Size()>>20, clipboardFileMax>>20)
	}
	if err != nil {
		f.Close()
		return "", nil, 0, false, err
	}
	return name, f, info.Size(), false, nil
}
