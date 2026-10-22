package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseWlPasteArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		list bool
		mime string
	}{
		// What Claude Code runs on Linux.
		{[]string{"-l"}, true, ""},
		{[]string{"--type", "image/png"}, false, "image/png"},
		{[]string{"--type", "image/bmp"}, false, "image/bmp"},
		{[]string{"--no-newline"}, false, ""},
		// Other spellings.
		{[]string{"--list-types"}, true, ""},
		{[]string{"-t", "image/png"}, false, "image/png"},
		{[]string{"--type=image/png"}, false, "image/png"},
		{[]string{"-t"}, false, ""},
	} {
		list, mime := parseWlPasteArgs(tc.args)
		if list != tc.list || mime != tc.mime {
			t.Errorf("parseWlPasteArgs(%q) = %v, %q; want %v, %q", tc.args, list, mime, tc.list, tc.mime)
		}
	}
}

func TestSaveToInbox(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir, _ := inboxDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "clipboard-old.png")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-inboxKeep - time.Hour)
	_ = os.Chtimes(stale, old, old)

	path, err := saveToInbox([]byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasSuffix(path, ".png") {
		t.Errorf("saved to %s, want a .png in %s", path, dir)
	}
	if got, _ := os.ReadFile(path); string(got) != "image" {
		t.Errorf("saved %q, want the image", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("image mode %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale image survived the paste")
	}
}
