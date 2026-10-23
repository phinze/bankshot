// Package inbox is where files pasted from the local machine's clipboard land
// on a remote host: $XDG_CACHE_HOME/bankshot/inbox, or ~/.cache/bankshot/inbox.
package inbox

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"
)

// Keep is how long pasted files stay in the inbox before a later paste
// clears them out.
const Keep = 7 * 24 * time.Hour

// Dir is the inbox's path.
func Dir() (string, error) {
	if cache := os.Getenv("XDG_CACHE_HOME"); cache != "" {
		return filepath.Join(cache, "bankshot", "inbox"), nil
	}
	return homedir.Expand("~/.cache/bankshot/inbox")
}

// Save writes size bytes from r to the inbox under a file named after name,
// readable only by this user, and returns its path. It clears out files
// older than Keep while it's there.
func Save(name string, r io.Reader, size int64) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create inbox: %w", err)
	}
	prune(dir, time.Now().Add(-Keep))

	ext := strings.ToLower(filepath.Ext(name))
	f, err := os.CreateTemp(dir, safeName(strings.TrimSuffix(name, filepath.Ext(name)))+"-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	if _, err := io.CopyN(f, r, size); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("write file: %w", err)
	}
	return f.Name(), nil
}

// safeName keeps a file name's letters, digits, dots, dashes and
// underscores and turns runs of anything else into one dash, so the path
// pastes into a shell as one word. CleanShot names recordings like
// "2026-10-23 at 2.06.15 PM".
func safeName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range name {
		ok := r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		switch {
		case ok:
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	if s := strings.Trim(b.String(), "-."); s != "" {
		return s
	}
	return "clipboard"
}

func prune(dir string, before time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.Mode().IsRegular() && info.ModTime().Before(before) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
