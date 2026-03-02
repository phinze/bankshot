package notify

import "log/slog"

// Notifier sends native desktop notifications for port forwarding events.
type Notifier struct {
	logger     *slog.Logger
	helperPath string
}
