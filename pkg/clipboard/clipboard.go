// Package clipboard reads images and copied files off this machine's
// clipboard, for remote callers that can't reach it themselves. It never
// reads text.
package clipboard

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrNoImage means the clipboard holds no image.
var ErrNoImage = errors.New("no image on the clipboard")

// ErrNoFile means the clipboard holds no file.
var ErrNoFile = errors.New("no file on the clipboard")

// Reader reads the clipboard's image. System is the real one.
type Reader interface {
	// HasImage reports whether the clipboard holds an image.
	HasImage(ctx context.Context) (bool, error)
	// PNG returns the clipboard's image as PNG, or ErrNoImage.
	PNG(ctx context.Context) ([]byte, error)
	// File returns the path of the file copied to the clipboard, as the
	// Finder or a screen recorder puts it there, or ErrNoFile.
	File(ctx context.Context) (string, error)
}

// pngMagic starts every PNG file.
var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// parseAppleScriptData decodes what osascript prints for clipboard data
// coerced to PNG, e.g. «data PNGf89504E47...».
func parseAppleScriptData(out []byte) ([]byte, error) {
	out = bytes.TrimSpace(out)
	prefix, suffix := []byte("«data PNGf"), []byte("»")
	if !bytes.HasPrefix(out, prefix) || !bytes.HasSuffix(out, suffix) {
		return nil, fmt.Errorf("unexpected osascript output %.40q", out)
	}
	data, err := hex.DecodeString(string(out[len(prefix) : len(out)-len(suffix)]))
	if err != nil {
		return nil, fmt.Errorf("decode clipboard data: %w", err)
	}
	if !bytes.HasPrefix(data, pngMagic) {
		return nil, errors.New("clipboard data isn't a PNG")
	}
	return data, nil
}
