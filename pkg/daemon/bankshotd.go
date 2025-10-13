package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"time"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/protocol"
)

// BankshotD is the server-side daemon that monitors ports and requests forwards
type BankshotD struct {
	logger         *slog.Logger
	systemdMode    bool
	pidFile        string
	ctx            context.Context
	sessionMonitor *monitor.SessionMonitor
	config         *config.Config
}

// NewBankshotD creates a new bankshotd instance
func NewBankshotD(cfg Config) (*BankshotD, error) {
	// Set up logger
	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Load bankshot config for monitor settings
	bankshotConfig, err := config.Load("")
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return &BankshotD{
		logger:      logger,
		systemdMode: cfg.SystemdMode,
		pidFile:     cfg.PIDFile,
		config:      bankshotConfig,
	}, nil
}

// Start runs bankshotd with monitoring
func (d *BankshotD) Start(ctx context.Context) error {
	d.ctx = ctx
	d.logger.Info("Starting bankshotd with port monitoring")

	// Write PID file if requested
	if d.pidFile != "" {
		if err := d.writePIDFile(); err != nil {
			return fmt.Errorf("failed to write PID file: %w", err)
		}
		defer d.removePIDFile()
	}

	// Create daemon client for sending forward requests
	daemonClient := &localDaemonClient{
		socketPath: d.config.Address,
		logger:     d.logger,
	}

	// Generate session ID based on user
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}
	sessionID := fmt.Sprintf("%s-%d", currentUser.Username, os.Getpid())

	// Parse monitor config from main config
	portRanges := []monitor.PortRange{{Start: 3000, End: 9999}} // Default
	ignoreProcesses := []string{"sshd", "systemd", "ssh-agent"}
	pollInterval := 1 * time.Second
	gracePeriod := 30 * time.Second

	// Override with config if present
	if d.config.Monitor.PortRanges != nil {
		portRanges = make([]monitor.PortRange, len(d.config.Monitor.PortRanges))
		for i, pr := range d.config.Monitor.PortRanges {
			portRanges[i] = monitor.PortRange{Start: pr.Start, End: pr.End}
		}
	}
	if len(d.config.Monitor.IgnoreProcesses) > 0 {
		ignoreProcesses = d.config.Monitor.IgnoreProcesses
	}
	if d.config.Monitor.PollInterval != "" {
		if duration, err := time.ParseDuration(d.config.Monitor.PollInterval); err == nil {
			pollInterval = duration
		}
	}
	if d.config.Monitor.GracePeriod != "" {
		if duration, err := time.ParseDuration(d.config.Monitor.GracePeriod); err == nil {
			gracePeriod = duration
		}
	}

	// Create and start session monitor
	sessionMonitor, err := monitor.NewSessionMonitor(monitor.SessionConfig{
		SessionID:       sessionID,
		DaemonClient:    daemonClient,
		PortRanges:      portRanges,
		IgnoreProcesses: ignoreProcesses,
		PollInterval:    pollInterval,
		GracePeriod:     gracePeriod,
		Logger:          d.logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create session monitor: %w", err)
	}
	d.sessionMonitor = sessionMonitor

	// Notify systemd we're ready
	if d.systemdMode {
		d.notifySystemd("READY=1")
		d.notifySystemd("STATUS=Bankshotd monitoring ports")

		// Start watchdog if configured
		go d.watchdogLoop()
	}

	// Start monitoring in background
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()

	go func() {
		if err := d.sessionMonitor.Start(monitorCtx); err != nil {
			d.logger.Error("Session monitor error", "error", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	// Notify systemd we're stopping
	if d.systemdMode {
		d.notifySystemd("STOPPING=1")
		d.notifySystemd("STATUS=Bankshotd is shutting down")
	}

	d.logger.Info("Bankshotd stopped")
	return nil
}

// localDaemonClient implements DaemonClient for sending requests to local daemon
type localDaemonClient struct {
	socketPath string
	logger     *slog.Logger
}

func (c *localDaemonClient) SendRequest(req *protocol.Request) (*protocol.Response, error) {
	// Connect to daemon socket
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	// Marshal request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	reqData = append(reqData, '\n')
	if _, err := conn.Write(reqData); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(conn)
	var resp protocol.Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}

// notifySystemd sends a notification to systemd
func (d *BankshotD) notifySystemd(state string) {
	if !d.systemdMode {
		return
	}

	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return
	}

	// Connect to systemd socket
	conn, err := net.Dial("unixgram", socketPath)
	if err != nil {
		d.logger.Debug("Failed to connect to systemd socket", "error", err)
		return
	}
	defer conn.Close()

	// Send notification
	if _, err := conn.Write([]byte(state)); err != nil {
		d.logger.Debug("Failed to notify systemd", "state", state, "error", err)
	}
}

// watchdogLoop sends periodic watchdog notifications to systemd
func (d *BankshotD) watchdogLoop() {
	watchdogUsec := os.Getenv("WATCHDOG_USEC")
	if watchdogUsec == "" {
		return
	}

	// Parse interval (simplified - just use 30 seconds)
	interval := 30 * time.Second
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
func (d *BankshotD) writePIDFile() error {
	if d.pidFile == "" {
		return nil
	}

	pid := os.Getpid()
	pidStr := fmt.Sprintf("%d\n", pid)

	if err := os.WriteFile(d.pidFile, []byte(pidStr), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	d.logger.Debug("Wrote PID file", "path", d.pidFile, "pid", pid)
	return nil
}

// removePIDFile removes the PID file
func (d *BankshotD) removePIDFile() {
	if d.pidFile == "" {
		return
	}

	if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
		d.logger.Error("Failed to remove PID file", "error", err)
	}
}
