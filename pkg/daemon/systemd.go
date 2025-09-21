package daemon

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// systemdMode indicates if we're running under systemd
var systemdMode bool

// pidFile path for PID file
var pidFile string

// notifySystemd sends notification to systemd if running in systemd mode
func (d *Daemon) notifySystemd(state string) {
	if !d.systemdMode {
		return
	}

	// Check for NOTIFY_SOCKET environment variable
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return
	}

	// Handle abstract socket notation
	if socketPath[0] == '@' {
		socketPath = "\x00" + socketPath[1:]
	}

	// Connect to systemd notify socket
	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		d.logger.Debug("Failed to connect to systemd notify socket", "error", err)
		return
	}
	defer conn.Close()

	// Send notification
	_, err = conn.Write([]byte(state))
	if err != nil {
		d.logger.Debug("Failed to send systemd notification", "error", err)
	}
}

// watchdogLoop sends periodic watchdog notifications to systemd
func (d *Daemon) watchdogLoop() {
	if !d.systemdMode {
		return
	}

	// Get watchdog interval from environment
	watchdogUsec := os.Getenv("WATCHDOG_USEC")
	if watchdogUsec == "" {
		return
	}

	// Parse interval
	usec, err := strconv.ParseInt(watchdogUsec, 10, 64)
	if err != nil {
		d.logger.Debug("Invalid WATCHDOG_USEC value", "value", watchdogUsec, "error", err)
		return
	}

	// Convert to duration and use half the interval for safety
	interval := time.Duration(usec) * time.Microsecond / 2

	d.logger.Debug("Starting watchdog loop", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.notifySystemd("WATCHDOG=1")
		case <-d.ctx.Done():
			return
		}
	}
}

// writePIDFile writes the current process ID to a file
func (d *Daemon) writePIDFile() error {
	if d.pidFile == "" {
		return nil
	}

	pid := os.Getpid()
	pidStr := strconv.Itoa(pid)

	err := os.WriteFile(d.pidFile, []byte(pidStr), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	d.logger.Debug("Wrote PID file", "path", d.pidFile, "pid", pid)
	return nil
}

// removePIDFile removes the PID file
func (d *Daemon) removePIDFile() {
	if d.pidFile == "" {
		return
	}

	if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
		d.logger.Warn("Failed to remove PID file", "path", d.pidFile, "error", err)
	}
}

// getListenerWithActivation tries to get listener from systemd socket activation
func (d *Daemon) getListenerWithActivation() (net.Listener, error) {
	if !d.systemdMode {
		// Not in systemd mode, create our own listener
		return net.Listen(d.config.Network, d.config.Address)
	}

	// Check for systemd socket activation
	// This is indicated by the LISTEN_FDS environment variable
	listenFDs := os.Getenv("LISTEN_FDS")
	if listenFDs == "" {
		// No socket activation, create our own listener
		return net.Listen(d.config.Network, d.config.Address)
	}

	// Parse number of file descriptors
	numFDs, err := strconv.Atoi(listenFDs)
	if err != nil || numFDs < 1 {
		return net.Listen(d.config.Network, d.config.Address)
	}

	// File descriptors start at 3 (0=stdin, 1=stdout, 2=stderr)
	// We'll use the first one
	fd := 3

	// Create listener from file descriptor
	file := os.NewFile(uintptr(fd), "systemd-socket")
	if file == nil {
		return net.Listen(d.config.Network, d.config.Address)
	}
	defer file.Close()

	listener, err := net.FileListener(file)
	if err != nil {
		d.logger.Warn("Failed to create listener from systemd socket", "error", err)
		return net.Listen(d.config.Network, d.config.Address)
	}

	d.logger.Info("Using systemd socket activation")
	return listener, nil
}