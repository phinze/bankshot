package inbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSave(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir, _ := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "clipboard-old.png")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-Keep - time.Hour)
	_ = os.Chtimes(stale, old, old)

	path, err := Save("2026-10-23 at 2.06.15 PM.MP4", strings.NewReader("video and more"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), "2026-10-23-at-2.06.15-PM-") || !strings.HasSuffix(path, ".mp4") {
		t.Errorf("saved to %s, want a 2026-10-23-at-2.06.15-PM-*.mp4 in %s", path, dir)
	}
	if got, _ := os.ReadFile(path); string(got) != "video" {
		t.Errorf("saved %q, want the first 5 bytes", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale file survived the paste")
	}

	if _, err := Save("short.png", strings.NewReader("abc"), 10); err == nil {
		t.Error("a short stream saved, want an error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("inbox holds %d files, want the short one cleaned up", len(entries))
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-23 at 2.06.15 PM":  "2026-10-23-at-2.06.15-PM",
		"clipboard-20261023-140106": "clipboard-20261023-140106",
		"$(rm -rf ~)":               "rm--rf",
		"../..":                     "clipboard",
		"":                          "clipboard",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
