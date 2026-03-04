package notify

import (
	"log/slog"
	"path/filepath"
	"strings"
)

// Notifier sends native desktop notifications for port forwarding events.
type Notifier struct {
	logger     *slog.Logger
	helperPath string
}

// shortPath returns the last two segments of a path for compact display.
// e.g. "/home/user/projects/myapp" → "projects/myapp"
func shortPath(p string) string {
	p = filepath.Clean(p)
	parts := strings.Split(p, string(filepath.Separator))
	if len(parts) <= 2 {
		return p
	}
	return filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
}
