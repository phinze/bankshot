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
	n.NotifyForward(3000, 3000, "localhost")
}

func TestNonexistentBinary(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	n := New(logger, "/nonexistent/bankshot-notify")

	// Should not panic; the goroutine logs a warning but doesn't block.
	n.NotifyForward(8080, 8080, "localhost")
}
