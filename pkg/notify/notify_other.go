//go:build !darwin

package notify

import "log/slog"

// New creates a Notifier. On non-darwin platforms this is a no-op.
func New(logger *slog.Logger, helperPath string) *Notifier {
	return &Notifier{
		logger:     logger,
		helperPath: helperPath,
	}
}

// NotifyForward is a no-op on non-darwin platforms.
func (n *Notifier) NotifyForward(remotePort, localPort int, host string) {}
