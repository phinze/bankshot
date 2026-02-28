package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/protocol"
)

// Monitor is the remote-side service that monitors ports and requests forwards
type Monitor struct {
	logger          *slog.Logger
	systemdMode     bool
	pidFile         string
	ctx             context.Context
	sessionMonitor  *monitor.SessionMonitor
	config          *config.Config
	socketReachable bool
}

// NewMonitor creates a new monitor instance
func NewMonitor(cfg Config) (*Monitor, error) {
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

	// Validate and expand paths in config (e.g., ~/.bankshot.sock)
	if err := bankshotConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Monitor{
		logger:      logger,
		systemdMode: cfg.SystemdMode,
		pidFile:     cfg.PIDFile,
		config:      bankshotConfig,
	}, nil
}

// Start runs the monitor with port monitoring
func (d *Monitor) Start(ctx context.Context) error {
	d.ctx = ctx
	d.logger.Info("Starting monitor with port monitoring")

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

	// Generate session ID based on hostname (for SSH connection matching)
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}
	sessionID := hostname

	// Parse monitor config from main config
	var portRanges []monitor.PortRange // nil = forward all non-privileged ports (>= 1024)
	ignorePorts := d.config.Monitor.IgnorePorts
	ignoreProcesses := []string{"sshd", "systemd", "ssh-agent"}
	pollInterval := 5 * time.Second // Default to 5s for reasonable CPU usage
	gracePeriod := 30 * time.Second

	// Override with config if present
	if len(d.config.Monitor.PortRanges) > 0 {
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

	// Create port event source (eBPF on Linux if available, else polling)
	portSource := monitor.NewSystemPortEventSource(d.logger, pollInterval)

	// Create and start session monitor
	sessionMonitor, err := monitor.NewSessionMonitor(monitor.SessionConfig{
		SessionID:       sessionID,
		DaemonClient:    daemonClient,
		PortRanges:      portRanges,
		IgnorePorts:     ignorePorts,
		IgnoreProcesses: ignoreProcesses,
		GracePeriod:     gracePeriod,
		Logger:          d.logger,
		PortEventSource: portSource,
	})
	if err != nil {
		return fmt.Errorf("failed to create session monitor: %w", err)
	}
	d.sessionMonitor = sessionMonitor

	// Notify systemd we're ready
	if d.systemdMode {
		d.notifySystemd("READY=1")
		d.notifySystemd("STATUS=Monitor monitoring ports")

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

	// Start socket connectivity monitor for sleep/wake recovery
	go d.socketConnectivityLoop(monitorCtx, daemonClient)

	// Wait for shutdown signal
	<-ctx.Done()

	// Notify systemd we're stopping
	if d.systemdMode {
		d.notifySystemd("STOPPING=1")
		d.notifySystemd("STATUS=Monitor shutting down")
	}

	d.logger.Info("Monitor stopped")
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
func (d *Monitor) notifySystemd(state string) {
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
func (d *Monitor) watchdogLoop() {
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

// socketConnectivityLoop periodically probes the daemon socket to detect
// SSH reconnection after sleep/wake. On unreachable → reachable transition,
// it triggers reconciliation to re-establish port forwards.
func (d *Monitor) socketConnectivityLoop(ctx context.Context, client *localDaemonClient) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			req := &protocol.Request{
				ID:   "connectivity-check-" + fmt.Sprintf("%d", time.Now().Unix()),
				Type: protocol.CommandStatus,
			}

			_, err := client.SendRequest(req)
			reachable := err == nil

			if reachable && !d.socketReachable {
				d.logger.Info("Daemon socket became reachable, triggering reconciliation")
				if err := d.Reconcile(); err != nil {
					d.logger.Error("Reconciliation after reconnect failed", "error", err)
				}
			} else if !reachable && d.socketReachable {
				d.logger.Warn("Daemon socket became unreachable")
			}

			d.socketReachable = reachable
		}
	}
}

// writePIDFile writes the current process ID to a file
func (d *Monitor) writePIDFile() error {
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
func (d *Monitor) removePIDFile() {
	if d.pidFile == "" {
		return
	}

	if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
		d.logger.Error("Failed to remove PID file", "error", err)
	}
}

// Reconcile performs VM-side reconciliation of port forwards
// It queries the laptop daemon for existing forwards and compares with actual
// listening ports on the VM, then sends forward/unforward requests to converge.
func (d *Monitor) Reconcile() error {
	d.logger.Info("Starting VM-side reconciliation")

	// Create daemon client
	daemonClient := &localDaemonClient{
		socketPath: d.config.Address,
		logger:     d.logger,
	}

	// Get hostname for connection matching
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}
	sessionID := hostname

	// Query daemon for existing forwards
	listReq := &protocol.Request{
		ID:   "reconcile-" + fmt.Sprintf("%d", time.Now().Unix()),
		Type: protocol.CommandList,
	}

	listResp, err := daemonClient.SendRequest(listReq)
	if err != nil {
		return fmt.Errorf("failed to query daemon forwards: %w", err)
	}

	if !listResp.Success {
		return fmt.Errorf("daemon returned error: %s", listResp.Error)
	}

	// Parse forwards list
	var listData protocol.ListResponse
	if err := json.Unmarshal(listResp.Data, &listData); err != nil {
		return fmt.Errorf("failed to parse forwards list: %w", err)
	}

	d.logger.Debug("Retrieved forwards from daemon", "count", len(listData.Forwards))

	// Filter to forwards matching our session/hostname
	daemonForwards := make(map[int]bool) // port -> exists
	for _, fwd := range listData.Forwards {
		if fwd.ConnectionInfo == sessionID {
			daemonForwards[fwd.RemotePort] = true
		}
	}

	d.logger.Debug("Forwards for this session", "count", len(daemonForwards))

	// Get listening ports on VM
	vmPorts, err := monitor.GetListeningPorts()
	if err != nil {
		return fmt.Errorf("failed to get VM listening ports: %w", err)
	}

	// Parse port ranges and ignore ports from config
	var portRanges []monitor.PortRange
	if len(d.config.Monitor.PortRanges) > 0 {
		portRanges = make([]monitor.PortRange, len(d.config.Monitor.PortRanges))
		for i, pr := range d.config.Monitor.PortRanges {
			portRanges[i] = monitor.PortRange{Start: pr.Start, End: pr.End}
		}
	}
	ignorePortsMap := make(map[int]bool, len(d.config.Monitor.IgnorePorts))
	for _, p := range d.config.Monitor.IgnorePorts {
		ignorePortsMap[p] = true
	}

	// Build set of ALL VM listening ports (for detecting stale forwards)
	allVMListening := make(map[int]bool)
	for _, port := range vmPorts {
		allVMListening[port.Port] = true
	}

	// Build set of VM ports that should be auto-forwarded
	vmListeningInRange := make(map[int]bool)
	for _, port := range vmPorts {
		if monitor.ShouldForwardPort(port.Port, portRanges, ignorePortsMap) {
			vmListeningInRange[port.Port] = true
		}
	}

	d.logger.Debug("VM ports listening",
		"total", len(allVMListening),
		"shouldForward", len(vmListeningInRange))

	// Reconcile: determine what actions to take
	var toForward, toUnforward []int

	// Ports listening on VM in our auto-forward range but not forwarded -> need forward
	for port := range vmListeningInRange {
		if !daemonForwards[port] {
			toForward = append(toForward, port)
		}
	}

	// Ports forwarded but not listening on VM at all -> need unforward
	// This only removes truly stale forwards, preserving forwards created
	// by "bankshot wrap" for ports outside the auto-forward range.
	for port := range daemonForwards {
		if !allVMListening[port] {
			toUnforward = append(toUnforward, port)
		}
	}

	d.logger.Info("Reconciliation plan",
		"toForward", len(toForward),
		"toUnforward", len(toUnforward),
		"unchanged", len(vmListeningInRange)-len(toForward))

	// Execute forwards
	for _, port := range toForward {
		d.logger.Info("Requesting forward for VM port", "port", port)
		fwdReq := &protocol.Request{
			ID:   "reconcile-fwd-" + fmt.Sprintf("%d-%d", port, time.Now().Unix()),
			Type: protocol.CommandForward,
		}

		payload, err := json.Marshal(protocol.ForwardRequest{
			RemotePort:     port,
			LocalPort:      port,
			Host:           "localhost",
			ConnectionInfo: sessionID,
		})
		if err != nil {
			d.logger.Warn("Failed to marshal forward request", "port", port, "error", err)
			continue
		}
		fwdReq.Payload = payload

		fwdResp, err := daemonClient.SendRequest(fwdReq)
		if err != nil {
			d.logger.Warn("Failed to request forward", "port", port, "error", err)
			continue
		}

		if !fwdResp.Success {
			d.logger.Warn("Forward request failed", "port", port, "error", fwdResp.Error)
		} else {
			d.logger.Info("Successfully requested forward", "port", port)
		}
	}

	// Execute unforwards
	for _, port := range toUnforward {
		d.logger.Info("Requesting unforward for port", "port", port)
		unfwdReq := &protocol.Request{
			ID:   "reconcile-unfwd-" + fmt.Sprintf("%d-%d", port, time.Now().Unix()),
			Type: protocol.CommandUnforward,
		}

		payload, err := json.Marshal(protocol.UnforwardRequest{
			RemotePort:     port,
			Host:           "localhost",
			ConnectionInfo: sessionID,
		})
		if err != nil {
			d.logger.Warn("Failed to marshal unforward request", "port", port, "error", err)
			continue
		}
		unfwdReq.Payload = payload

		unfwdResp, err := daemonClient.SendRequest(unfwdReq)
		if err != nil {
			d.logger.Warn("Failed to request unforward", "port", port, "error", err)
			continue
		}

		if !unfwdResp.Success {
			d.logger.Warn("Unforward request failed", "port", port, "error", unfwdResp.Error)
		} else {
			d.logger.Info("Successfully requested unforward", "port", port)
		}
	}

	d.logger.Info("VM-side reconciliation complete",
		"forwarded", len(toForward),
		"unforwarded", len(toUnforward))

	return nil
}
