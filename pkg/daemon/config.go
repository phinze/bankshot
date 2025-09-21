package daemon

import (
	"context"
	"log/slog"

	"github.com/phinze/bankshot/pkg/config"
)

// Config holds daemon configuration
type Config struct {
	SystemdMode bool   // Run in systemd mode with sd_notify support
	LogLevel    string // Log level (debug, info, warn, error)
	PIDFile     string // Path to PID file (optional)
}

// NewWithConfig creates a new daemon with custom configuration
func NewWithConfig(daemonConfig Config) (*Daemon, error) {
	// Load bankshot config
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}

	// Set up logger with requested level
	logLevel := slog.LevelInfo
	switch daemonConfig.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	logger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Create daemon with existing New function
	d := New(cfg, logger)
	
	// Add systemd-specific configuration
	d.systemdMode = daemonConfig.SystemdMode
	d.pidFile = daemonConfig.PIDFile

	return d, nil
}

// Start starts the daemon with context support
func (d *Daemon) Start(ctx context.Context) error {
	// Update daemon context
	d.ctx = ctx
	d.cancel = func() {} // Will be set by Run

	// If systemd mode, notify readiness
	if d.systemdMode {
		d.notifySystemd("STATUS=Starting bankshot daemon")
	}

	// Write PID file if requested
	if d.pidFile != "" {
		if err := d.writePIDFile(); err != nil {
			return err
		}
		defer d.removePIDFile()
	}

	// Run the daemon
	return d.Run()
}