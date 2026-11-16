package daemon

import (
	"bufio"
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
	"github.com/phinze/bankshot/pkg/tailnet"
)

// Monitor is the remote-side service that carries this host's requests to the
// daemon over the tailnet and holds the forwards asked for here
type Monitor struct {
	logger          *slog.Logger
	systemdMode     bool
	pidFile         string
	ctx             context.Context
	sessionMonitor  *monitor.SessionMonitor
	config          *config.Config
	socketReachable bool
	inFlight        inFlight // local requests passed to the daemon, for the relay to vouch for
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

// Start runs the monitor until ctx is done
func (d *Monitor) Start(ctx context.Context) error {
	d.ctx = ctx
	if !d.config.Tailnet.Enabled {
		return fmt.Errorf("bankshot monitor talks to the daemon over the tailnet; set tailnet.enabled")
	}
	d.logger.Info("Starting monitor")

	// Write PID file if requested
	if d.pidFile != "" {
		if err := d.writePIDFile(); err != nil {
			return fmt.Errorf("failed to write PID file: %w", err)
		}
		defer d.removePIDFile()
	}

	// Create daemon client for sending forward requests
	daemonClient := &localDaemonClient{
		network: d.config.Network,
		address: d.config.Address,
		logger:  d.logger,
	}

	// Generate session ID based on hostname
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}
	d.sessionMonitor = monitor.NewSessionMonitor(monitor.SessionConfig{
		SessionID:    hostname,
		DaemonClient: daemonClient,
		Logger:       d.logger,
	})

	// Notify systemd we're ready
	if d.systemdMode {
		d.notifySystemd("READY=1")
		d.notifySystemd("STATUS=Monitor running")

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

	// The sync heartbeat both keeps the daemon's lease alive and repairs
	// forwards after the daemon comes back.
	go d.serveRelay(monitorCtx)
	go d.serveLocal(monitorCtx, daemonClient)
	if d.config.Tailnet.AgentSocket != "" {
		go d.serveAgent(monitorCtx, daemonClient)
	}
	go d.heartbeatLoop(monitorCtx)

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

const (
	daemonDialTimeout    = 5 * time.Second
	daemonRequestTimeout = time.Minute
)

// serveRelay listens on this machine's tailnet address for the daemon's
// relayed connections. At boot the tailnet may not be up yet, so it keeps
// trying until it is.
func (d *Monitor) serveRelay(ctx context.Context) {
	var ln net.Listener
	for delay := time.Second; ; delay = min(delay*2, time.Minute) {
		ip, err := tailnet.IPv4(ctx)
		if err == nil {
			ln, err = net.Listen("tcp", net.JoinHostPort(ip, fmt.Sprint(d.config.Tailnet.RelayPort)))
		}
		if err == nil {
			break
		}
		d.logger.Warn("Tailnet relay not up yet, retrying", "error", err, "in", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	d.logger.Info("Relaying forwards over the tailnet", "address", ln.Addr())
	relay := &tailnet.Relay{
		Gate:       tailnet.NewGate(d.config.Tailnet.Allow, tailnet.Whois, false),
		Logger:     d.logger,
		Forwarding: d.sessionMonitor.Forwarding,
		Vouches:    d.inFlight.has,
	}
	relay.Serve(ln)
}

// heartbeatLoop syncs this host's forwards to the daemon: once shortly after
// start, then every heartbeat. Each sync renews the lease that keeps the
// daemon's forwards alive. While the daemon is unreachable it retries more
// often, so forwards come back soon after the laptop wakes.
func (d *Monitor) heartbeatLoop(ctx context.Context) {
	interval := d.config.Tailnet.HeartbeatDuration()
	retry := min(30*time.Second, interval)
	next := 10 * time.Second // let the initial port scan settle first
	first := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(next):
		}
		lease, err := d.sessionMonitor.Sync()
		if err != nil {
			if d.socketReachable || first {
				d.logger.Warn("Daemon unreachable over the tailnet", "error", err)
			}
			first = false
			d.socketReachable = false
			next = retry
			continue
		}
		if !d.socketReachable {
			d.logger.Info("Synced forwards with daemon over the tailnet", "lease", lease)
		}
		if lease > 0 && lease <= interval {
			d.logger.Warn("Daemon lease is shorter than the heartbeat; forwards will lapse between syncs",
				"lease", lease, "heartbeat", interval)
		}
		first = false
		d.socketReachable = true
		next = interval
	}
}

// localDaemonClient implements DaemonClient for sending requests to local daemon
type localDaemonClient struct {
	network string
	address string
	logger  *slog.Logger
}

// Open sends req and, if the daemon accepts it, returns its response and the
// connection for the stream that follows, with the reader to keep using on it.
func (c *localDaemonClient) Open(req *protocol.Request) (*protocol.Response, net.Conn, *bufio.Reader, error) {
	conn, err := net.DialTimeout(c.network, c.address, daemonDialTimeout)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(daemonRequestTimeout))
	reqData, _ := json.Marshal(req)
	if _, err := conn.Write(append(reqData, '\n')); err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("failed to send request: %w", err)
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("failed to read response: %w", err)
	}
	resp, err := protocol.ParseResponse(line)
	if err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	if !resp.Success {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("daemon refused: %s", resp.Error)
	}
	_ = conn.SetDeadline(time.Time{})
	return resp, conn, reader, nil
}

func (c *localDaemonClient) SendRequest(req *protocol.Request) (*protocol.Response, error) {
	// Connect to the daemon through the configured transport: an
	// SSH-forwarded socket, or the daemon's tailnet address. Bound both, so a
	// laptop that's asleep fails the request rather than hanging it.
	conn, err := net.DialTimeout(c.network, c.address, daemonDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(daemonRequestTimeout))

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
