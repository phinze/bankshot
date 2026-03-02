//go:build darwin

package notify

import (
	"fmt"
	"log/slog"
	"os/exec"
)

// New creates a Notifier. If helperPath is empty, notifications are disabled.
func New(logger *slog.Logger, helperPath string) *Notifier {
	return &Notifier{
		logger:     logger,
		helperPath: helperPath,
	}
}

// NotifyForward posts a macOS notification for a newly-forwarded port.
// It shells out to the helper app in a goroutine so it never blocks the caller.
func (n *Notifier) NotifyForward(remotePort, localPort int, host string) {
	if n.helperPath == "" {
		return
	}

	title := fmt.Sprintf("Port %d forwarded", remotePort)
	body := fmt.Sprintf("%s:%d → localhost:%d", host, remotePort, localPort)
	url := fmt.Sprintf("http://localhost:%d", localPort)

	go func() {
		cmd := exec.Command(n.helperPath,
			"--title", title,
			"--body", body,
			"--url", url,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			n.logger.Warn("notification helper failed",
				"error", err,
				"output", string(out),
				"helper", n.helperPath,
			)
		}
	}()
}
