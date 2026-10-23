//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// System reads the macOS clipboard through osascript, the way Claude Code
// does on the Mac, so it needs no cgo and no extra tools.
type System struct{}

// HasImage asks for the clipboard's types and looks for PNG among them.
func (System) HasImage(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", "clipboard info").Output()
	if err != nil {
		return false, fmt.Errorf("osascript clipboard info: %w", err)
	}
	return bytes.Contains(out, []byte("«class PNGf»")), nil
}

// PNG coerces the clipboard to PNG. osascript fails when it can't, which is
// the clipboard holding no image.
func (System) PNG(ctx context.Context) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", "the clipboard as «class PNGf»").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, ErrNoImage
		}
		return nil, fmt.Errorf("osascript: %w", err)
	}
	return parseAppleScriptData(out)
}

// File asks for the clipboard as a file reference. osascript fails when it
// can't make one, which is the clipboard holding no file.
func (System) File(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", "POSIX path of (the clipboard as «class furl»)").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", ErrNoFile
		}
		return "", fmt.Errorf("osascript: %w", err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}
