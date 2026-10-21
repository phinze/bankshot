package notify

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Notifier sends native desktop notifications for port forwarding events.
type Notifier struct {
	logger     *slog.Logger
	helperPath string
}

// ForwardEvent describes a single newly-established port forward.
type ForwardEvent struct {
	RemotePort  int
	LocalPort   int
	Host        string
	ProcessName string
	ProcessCwd  string
}

// rollupPortLimit caps how many ports we spell out in a rollup body before
// collapsing the rest into a "+N more" suffix.
const rollupPortLimit = 10

// NotifyForward posts a notification for a single newly-forwarded port. It's a
// thin wrapper over NotifyForwards for callers (like the manual `bankshot
// forward` command) that only ever deal with one port at a time.
func (n *Notifier) NotifyForward(remotePort, localPort int, host, processName, processCwd string) {
	n.NotifyForwards("", []ForwardEvent{{
		RemotePort:  remotePort,
		LocalPort:   localPort,
		Host:        host,
		ProcessName: processName,
		ProcessCwd:  processCwd,
	}})
}

// NotifyForwards posts a single notification covering one or more forwards.
// A lone forward gets the detailed treatment (process context + a clickable
// URL); multiple forwards are rolled up into one summary so an initial SSH
// connection that lights up a dozen ports doesn't bury the user in toasts.
func (n *Notifier) NotifyForwards(connectionInfo string, events []ForwardEvent) {
	if n.helperPath == "" {
		n.logger.Debug("Skipping notification, no helper configured")
		return
	}
	if len(events) == 0 {
		return
	}

	if len(events) == 1 {
		title, body, url := formatSingleForward(events[0])
		n.logger.Info("Sending notification",
			"title", title,
			"helper", n.helperPath,
		)
		n.send("--title", title, "--body", body, "--url", url)
		return
	}

	title, body := formatForwardRollup(connectionInfo, events)
	n.logger.Info("Sending rollup notification",
		"title", title,
		"forwards", len(events),
		"helper", n.helperPath,
	)
	n.send("--title", title, "--body", body)
}

// formatSingleForward renders the detailed, single-port notification.
func formatSingleForward(e ForwardEvent) (title, body, url string) {
	host := e.Host
	if host == "" {
		host = "localhost"
	}

	title = fmt.Sprintf("Port %d forwarded", e.RemotePort)
	body = fmt.Sprintf("%s:%d → localhost:%d", host, e.RemotePort, e.LocalPort)

	if e.ProcessName != "" {
		context := e.ProcessName
		if e.ProcessCwd != "" {
			context += " in " + shortPath(e.ProcessCwd)
		}
		body += "\n" + context
	}

	url = fmt.Sprintf("http://localhost:%d", e.LocalPort)
	return title, body, url
}

// formatForwardRollup renders the summary notification for a batch of forwards.
// We can't pick a single URL to open, so the rollup is informational only.
func formatForwardRollup(connectionInfo string, events []ForwardEvent) (title, body string) {
	sorted := make([]ForwardEvent, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].RemotePort < sorted[j].RemotePort
	})

	if connectionInfo != "" {
		title = fmt.Sprintf("%d ports forwarded from %s", len(sorted), connectionInfo)
	} else {
		title = fmt.Sprintf("%d ports forwarded", len(sorted))
	}

	ports := make([]string, 0, len(sorted))
	for _, e := range sorted {
		ports = append(ports, strconv.Itoa(e.RemotePort))
	}
	if len(ports) > rollupPortLimit {
		extra := len(ports) - rollupPortLimit
		body = strings.Join(ports[:rollupPortLimit], ", ") + fmt.Sprintf(", +%d more", extra)
	} else {
		body = strings.Join(ports, ", ")
	}
	return title, body
}

// NotifyOpProxy posts a notification for a proxied 1Password CLI request.
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

	n.send("--title", title, "--body", body)
}

// NotifyForwardBusy posts a notification that a forward couldn't take its
// local port, so the user knows why localhost:PORT isn't the remote service.
func (n *Notifier) NotifyForwardBusy(connectionInfo string, remotePort int, err error) {
	if n.helperPath == "" {
		return
	}
	n.send("--title", fmt.Sprintf("Can't forward %s:%d", connectionInfo, remotePort), "--body", err.Error())
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
