package monitor

import (
	"log/slog"
	"time"
)

// NewPortEventSource returns a PortEventSource for a specific process.
// On Linux, it tries eBPF first and falls back to polling.
func NewPortEventSource(pid int, logger *slog.Logger) PortEventSource {
	if err := probeEBPF(); err != nil {
		logger.Info("eBPF not available, falling back to polling", "error", err)
		return New(pid, logger)
	}
	logger.Info("using eBPF port monitoring")
	return newEBPFMonitor(logger)
}

// NewSystemPortEventSource returns a PortEventSource for system-wide monitoring.
// On Linux, it tries eBPF first and falls back to polling.
func NewSystemPortEventSource(logger *slog.Logger, pollInterval time.Duration) PortEventSource {
	if err := probeEBPF(); err != nil {
		logger.Info("eBPF not available, falling back to polling", "error", err)
		return NewSystemMonitor(logger, pollInterval)
	}
	logger.Info("using eBPF port monitoring")
	return newEBPFMonitor(logger)
}
