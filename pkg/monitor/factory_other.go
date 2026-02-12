//go:build !linux

package monitor

import (
	"log/slog"
	"time"
)

// NewPortEventSource returns a polling PortEventSource for a specific process.
func NewPortEventSource(pid int, logger *slog.Logger) PortEventSource {
	return New(pid, logger)
}

// NewSystemPortEventSource returns a polling PortEventSource for system-wide monitoring.
func NewSystemPortEventSource(logger *slog.Logger, pollInterval time.Duration) PortEventSource {
	return NewSystemMonitor(logger, pollInterval)
}
