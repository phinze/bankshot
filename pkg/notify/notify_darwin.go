//go:build darwin

package notify

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// New creates a Notifier. If helperPath is empty, notifications are disabled.
func New(logger *slog.Logger, helperPath string) *Notifier {
	return &Notifier{
		logger:     logger,
		helperPath: helperPath,
	}
}

// NotifyOpProxy posts a macOS notification for a proxied 1Password CLI request.
func (n *Notifier) NotifyOpProxy(args []string) {
	if n.helperPath == "" {
		return
	}

	title := "1Password"
	body := "op"
	if len(args) > 0 {
		body = "op " + strings.Join(args, " ")
	}
	// Truncate long command lines
	if len(body) > 80 {
		body = body[:77] + "..."
	}

	go func() {
		cmd := exec.Command(n.helperPath,
			"--title", title,
			"--body", body,
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

// NotifyForward posts a macOS notification for a newly-forwarded port.
// It shells out to the helper app in a goroutine so it never blocks the caller.
func (n *Notifier) NotifyForward(remotePort, localPort int, host, processName, processCwd string) {
	if n.helperPath == "" {
		n.logger.Debug("Skipping notification, no helper configured")
		return
	}

	title := fmt.Sprintf("Port %d forwarded", remotePort)
	body := fmt.Sprintf("%s:%d → localhost:%d", host, remotePort, localPort)

	// Add process context if available
	if processName != "" {
		context := processName
		if processCwd != "" {
			context += " in " + shortPath(processCwd)
		}
		body += "\n" + context
	}

	url := fmt.Sprintf("http://localhost:%d", localPort)

	n.logger.Info("Sending notification",
		"title", title,
		"remotePort", remotePort,
		"localPort", localPort,
		"helper", n.helperPath,
	)

	go func() {
		cmd := exec.Command(n.helperPath,
			"--title", title,
			"--body", body,
			"--url", url,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			n.logger.Warn("notification helper failed",
				"error", err,
				"output", string(out),
				"helper", n.helperPath,
			)
		} else {
			n.logger.Debug("Notification helper succeeded",
				"title", title,
				"output", string(out),
			)
		}
	}()
}
