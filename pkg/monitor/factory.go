package monitor

import "log/slog"

// NewPortEventSource returns a PortEventSource for the ports one process and
// its children listen on, found by polling.
func NewPortEventSource(pid int, logger *slog.Logger) PortEventSource {
	return New(pid, logger)
}
