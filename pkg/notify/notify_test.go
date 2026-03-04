package notify

import (
	"log/slog"
	"os"
	"testing"
)

func TestEmptyHelperPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	n := New(logger, "")

	// Should be a graceful no-op (no panic, no error)
	n.NotifyForward(3000, 3000, "localhost", "python3", "/home/user/projects/myapp")
}

func TestShortPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/home/user/projects/myapp", "projects/myapp"},
		{"/var/app", "var/app"},
		{"/root", "/root"},
		{"relative", "relative"},
		{"/a/b/c/d/e", "d/e"},
	}
	for _, tt := range tests {
		got := shortPath(tt.input)
		if got != tt.want {
			t.Errorf("shortPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNonexistentBinary(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	n := New(logger, "/nonexistent/bankshot-notify")

	// Should not panic; the goroutine logs a warning but doesn't block.
	n.NotifyForward(8080, 8080, "localhost", "", "")
}
