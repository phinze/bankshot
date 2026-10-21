package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/forwarder"
	"github.com/phinze/bankshot/pkg/notify"
	"github.com/phinze/bankshot/pkg/opener"
	"github.com/phinze/bankshot/pkg/opproxy"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/phinze/bankshot/version"
)

// Daemon represents the bankshot daemon
type Daemon struct {
	config      *config.Config
	listener    net.Listener
	logger      *slog.Logger
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	opener      *opener.Opener
	forwarder   forwarder.Forwarder
	ssh         *forwarder.SSHForwarder // also d.forwarder; kept concrete for startup discovery
	notifier    *notify.Notifier
	opProxy     *opproxy.OpProxy
	startTime   time.Time
	systemdMode bool   // Running under systemd
	pidFile     string // PID file path
}

// New creates a new daemon instance
func New(cfg *config.Config, logger *slog.Logger) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	ssh := forwarder.NewSSH(logger, cfg.SSHCommand)
	return &Daemon{
		config:    cfg,
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
		opener:    opener.New(logger),
		forwarder: ssh,
		ssh:       ssh,
		notifier:  notify.New(logger, cfg.NotifyCommand),
		opProxy:   opproxy.New(&cfg.OpProxy, logger),
		startTime: time.Now(),
	}
}

// Run starts the daemon
func (d *Daemon) Run() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Clean up existing socket if unix
	if d.config.Network == "unix" {
		// Expand tilde in address if present
		if len(d.config.Address) > 0 && d.config.Address[0] == '~' {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			d.config.Address = filepath.Join(home, d.config.Address[1:])
		}

		// Check if another daemon is already running
		if err := d.checkExistingDaemon(); err != nil {
			return err
		}

		// Set umask for socket permissions (user-only access)
		oldUmask := syscall.Umask(0077)
		defer syscall.Umask(oldUmask)

		// Remove existing socket
		if err := os.RemoveAll(d.config.Address); err != nil {
			return fmt.Errorf("failed to remove existing socket: %w", err)
		}

		// Ensure directory exists with secure permissions
		socketDir := filepath.Dir(d.config.Address)
		if err := os.MkdirAll(socketDir, 0700); err != nil {
			return fmt.Errorf("failed to create socket directory: %w", err)
		}

		// Verify directory permissions
		if info, err := os.Stat(socketDir); err == nil {
			mode := info.Mode()
			if mode.Perm()&0077 != 0 {
				d.logger.Warn("Socket directory has weak permissions",
					"path", socketDir,
					"mode", mode.Perm())
			}
		}
	}

	// Start listener (with systemd socket activation if available)
	listener, err := d.getListenerWithActivation()
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	d.listener = listener

	d.logger.Info("Daemon started",
		"network", d.config.Network,
		"address", d.config.Address,
	)

	// Auto-discover existing SSH port forwards
	if err := d.autoDiscoverForwards(); err != nil {
		d.logger.Warn("Failed to auto-discover forwards", "error", err)
	}

	// Start periodic reconciliation to detect stale forwards
	d.wg.Add(1)
	go d.reconcileLoop()

	// Start accepting connections
	d.wg.Add(1)
	go d.acceptConnections()

	// Notify systemd we're ready
	if d.systemdMode {
		d.notifySystemd("READY=1")
		d.notifySystemd("STATUS=Bankshot daemon running")

		// Start watchdog if configured
		go d.watchdogLoop()
	}

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		d.logger.Info("Received signal", "signal", sig)
	case <-d.ctx.Done():
		d.logger.Info("Context cancelled")
	}

	// Shutdown
	return d.shutdown()
}

// acceptConnections accepts incoming connections
func (d *Daemon) acceptConnections() {
	defer d.wg.Done()

	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.ctx.Done():
				// Shutting down
				return
			default:
				d.logger.Error("Failed to accept connection", "error", err)
				continue
			}
		}

		// Handle connection in goroutine
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.handleConnection(conn)
		}()
	}
}

// handleConnection handles a single connection
func (d *Daemon) handleConnection(conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()

	remoteAddr := conn.RemoteAddr().String()
	d.logger.Debug("New connection", "remote", remoteAddr)

	// For Unix sockets, verify connection is from same user
	if d.config.Network == "unix" {
		if unixConn, ok := conn.(*net.UnixConn); ok {
			// Get connection credentials if supported by platform
			rawConn, err := unixConn.SyscallConn()
			if err == nil {
				err = rawConn.Control(func(fd uintptr) {
					// This is platform-specific and may not work on all systems
					// On Linux, we could use SO_PEERCRED
					// For now, we rely on socket file permissions
				})
				if err != nil {
					d.logger.Debug("Could not verify peer credentials", "error", err)
				}
			}
		}
	}

	// Read request from connection
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		if err != io.EOF {
			d.logger.Error("Failed to read from connection", "error", err, "remote", remoteAddr)
		}
		return
	}

	// Parse request
	req, err := protocol.ParseRequest([]byte(line))
	if err != nil {
		d.logger.Error("Failed to parse request", "error", err, "remote", remoteAddr)
		// Send error response
		resp := protocol.NewErrorResponse("", fmt.Errorf("invalid request format"))
		d.sendResponse(conn, resp)
		return
	}

	if isConnectivityProbe(req) {
		d.logger.Debug("Received command", "type", req.Type, "id", req.ID, "remote", remoteAddr)
	} else {
		d.logger.Info("Received command", "type", req.Type, "id", req.ID, "remote", remoteAddr)
	}

	// Handle command
	resp := d.handleCommand(req)

	// Send response
	d.sendResponse(conn, resp)

	d.logger.Debug("Connection closed", "remote", remoteAddr)
}

// handleCommand processes a command and returns a response
func (d *Daemon) handleCommand(req *protocol.Request) *protocol.Response {
	switch req.Type {
	case protocol.CommandOpen:
		return d.handleOpenCommand(req)
	case protocol.CommandStatus:
		return d.handleStatusCommand(req)
	case protocol.CommandList:
		return d.handleListCommand(req)
	case protocol.CommandForward:
		return d.handleForwardCommand(req)
	case protocol.CommandForwardBatch:
		return d.handleForwardBatchCommand(req)
	case protocol.CommandUnforward:
		return d.handleUnforwardCommand(req)
	case protocol.CommandReconcile:
		return d.handleReconcileCommand(req)
	case protocol.CommandOpProxy:
		return d.handleOpProxyCommand(req)
	default:
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("unknown command type: %s", req.Type))
	}
}

func isConnectivityProbe(req *protocol.Request) bool {
	return req.Type == protocol.CommandStatus && strings.HasPrefix(req.ID, "connectivity-check-")
}

// handleOpenCommand handles the open URL command
func (d *Daemon) handleOpenCommand(req *protocol.Request) *protocol.Response {
	// Parse payload
	var openReq protocol.OpenRequest
	if err := json.Unmarshal(req.Payload, &openReq); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid payload: %w", err))
	}

	// Open URL
	if err := d.opener.OpenURL(openReq.URL); err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	// Return success
	resp, _ := protocol.NewSuccessResponse(req.ID, map[string]string{
		"message": fmt.Sprintf("Opened URL: %s", openReq.URL),
	})
	return resp
}

// handleStatusCommand handles the status command
func (d *Daemon) handleStatusCommand(req *protocol.Request) *protocol.Response {
	uptime := time.Since(d.startTime).Round(time.Second).String()

	// Get all forwards and group by connection
	forwards := d.forwarder.ListForwards()
	connectionMap := make(map[string]*protocol.ConnectionStatus)

	for _, fwd := range forwards {
		if _, exists := connectionMap[fwd.ConnectionInfo]; !exists {
			connectionMap[fwd.ConnectionInfo] = &protocol.ConnectionStatus{
				ConnectionInfo: fwd.ConnectionInfo,
				ForwardCount:   0,
				LastActivity:   fwd.CreatedAt.Format(time.RFC3339),
			}
		}
		connectionMap[fwd.ConnectionInfo].ForwardCount++
		// Update last activity if this forward is newer
		if fwd.CreatedAt.After(time.Time{}) {
			lastActivity, _ := time.Parse(time.RFC3339, connectionMap[fwd.ConnectionInfo].LastActivity)
			if fwd.CreatedAt.After(lastActivity) {
				connectionMap[fwd.ConnectionInfo].LastActivity = fwd.CreatedAt.Format(time.RFC3339)
			}
		}
	}

	// Convert map to slice
	connections := make([]protocol.ConnectionStatus, 0, len(connectionMap))
	for _, conn := range connectionMap {
		connections = append(connections, *conn)
	}

	status := protocol.StatusResponse{
		Version:        version.GetVersion(),
		Uptime:         uptime,
		ActiveForwards: len(forwards),
		Connections:    connections,
	}

	resp, err := protocol.NewSuccessResponse(req.ID, status)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

// handleListCommand handles the list forwards command
func (d *Daemon) handleListCommand(req *protocol.Request) *protocol.Response {
	forwards := d.forwarder.ListForwards()

	forwardInfos := make([]protocol.ForwardInfo, 0, len(forwards))
	for _, fwd := range forwards {
		forwardInfos = append(forwardInfos, protocol.ForwardInfo{
			RemotePort:     fwd.RemotePort,
			LocalPort:      fwd.LocalPort,
			Host:           fwd.Host,
			ConnectionInfo: fwd.ConnectionInfo,
			CreatedAt:      fwd.CreatedAt.Format(time.RFC3339),
		})
	}

	list := protocol.ListResponse{
		Forwards: forwardInfos,
	}

	resp, err := protocol.NewSuccessResponse(req.ID, list)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

// handleForwardCommand handles the port forward command
func (d *Daemon) handleForwardCommand(req *protocol.Request) *protocol.Response {
	// Parse payload
	var forwardReq protocol.ForwardRequest
	if err := json.Unmarshal(req.Payload, &forwardReq); err != nil {
		d.logger.Error("Failed to parse forward request",
			"error", err,
			"payload", string(req.Payload))
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid forward request format: %w", err))
	}

	target := forwarder.Target{ConnectionInfo: forwardReq.ConnectionInfo, SocketPath: forwardReq.SocketPath}
	results, err := d.forwarder.AddForwards(target, []forwarder.Port{{
		RemotePort: forwardReq.RemotePort,
		LocalPort:  forwardReq.LocalPort,
		Host:       forwardReq.Host,
	}})
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	res := results[0]
	if res.Err != nil {
		return protocol.NewErrorResponse(req.ID, res.Err)
	}

	// Notify on new forwards (not duplicates from reconciliation)
	if res.Created {
		d.notifier.NotifyForward(res.RemotePort, res.LocalPort, res.Host, forwardReq.ProcessName, forwardReq.ProcessCwd)
	}

	// Return success
	resp, _ := protocol.NewSuccessResponse(req.ID, map[string]interface{}{
		"message": fmt.Sprintf("Forwarded %s:%d to localhost:%d",
			res.Host, res.RemotePort, res.LocalPort),
	})
	return resp
}

// handleForwardBatchCommand handles a batch of port forwards from the monitor.
// It establishes every forward, then posts a single rolled-up notification for
// the ones that were newly created so an initial SSH connection doesn't spam
// the user with one toast per port.
func (d *Daemon) handleForwardBatchCommand(req *protocol.Request) *protocol.Response {
	var batchReq protocol.BatchForwardRequest
	if err := json.Unmarshal(req.Payload, &batchReq); err != nil {
		d.logger.Error("Failed to parse batch forward request",
			"error", err,
			"payload", string(req.Payload))
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid batch forward request format: %w", err))
	}

	ports := make([]forwarder.Port, 0, len(batchReq.Forwards))
	for _, fwd := range batchReq.Forwards {
		ports = append(ports, forwarder.Port{RemotePort: fwd.RemotePort, LocalPort: fwd.LocalPort, Host: fwd.Host})
	}
	target := forwarder.Target{ConnectionInfo: batchReq.ConnectionInfo, SocketPath: batchReq.SocketPath}
	fwdResults, err := d.forwarder.AddForwards(target, ports)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	results := make([]protocol.BatchForwardResult, 0, len(fwdResults))
	var created []notify.ForwardEvent

	for i, r := range fwdResults {
		fwd := batchReq.Forwards[i]
		res := protocol.BatchForwardResult{
			RemotePort: r.RemotePort,
			LocalPort:  r.LocalPort,
			Created:    r.Created,
		}
		if r.Err != nil {
			res.Error = r.Err.Error()
			d.logger.Warn("Batch forward failed for port",
				"port", r.RemotePort,
				"connectionInfo", batchReq.ConnectionInfo,
				"error", r.Err)
		} else if r.Created {
			created = append(created, notify.ForwardEvent{
				RemotePort:  r.RemotePort,
				LocalPort:   r.LocalPort,
				Host:        r.Host,
				ProcessName: fwd.ProcessName,
				ProcessCwd:  fwd.ProcessCwd,
			})
		}
		results = append(results, res)
	}

	// One notification for the whole batch (single or rollup, decided downstream).
	if len(created) > 0 {
		d.notifier.NotifyForwards(batchReq.ConnectionInfo, created)
	}

	resp, err := protocol.NewSuccessResponse(req.ID, protocol.BatchForwardResponse{Results: results})
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

// handleUnforwardCommand handles the port unforward command
func (d *Daemon) handleUnforwardCommand(req *protocol.Request) *protocol.Response {
	// Parse payload
	var unforwardReq protocol.UnforwardRequest
	if err := json.Unmarshal(req.Payload, &unforwardReq); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid unforward request format: %w", err))
	}

	// Default values
	host := unforwardReq.Host
	if host == "" {
		host = "localhost"
	}

	// Remove forward
	if err := d.forwarder.RemoveForward(unforwardReq.ConnectionInfo, unforwardReq.RemotePort, host); err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	// Return success
	resp, _ := protocol.NewSuccessResponse(req.ID, map[string]interface{}{
		"message": fmt.Sprintf("Removed forward for %s:%d",
			host, unforwardReq.RemotePort),
	})
	return resp
}

// handleOpProxyCommand handles the op-proxy command
func (d *Daemon) handleOpProxyCommand(req *protocol.Request) *protocol.Response {
	var opReq protocol.OpProxyRequest
	if err := json.Unmarshal(req.Payload, &opReq); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid op-proxy request format: %w", err))
	}

	opResp, err := d.opProxy.Execute(opReq)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	d.notifier.NotifyOpProxy(opReq.Args)

	resp, err := protocol.NewSuccessResponse(req.ID, opResp)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

// handleReconcileCommand handles the reconcile command
func (d *Daemon) handleReconcileCommand(req *protocol.Request) *protocol.Response {
	d.logger.Info("Reconciliation requested via API")

	// Trigger reconciliation
	if err := d.forwarder.Reconcile(); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("reconciliation failed: %w", err))
	}

	// Return success
	resp, _ := protocol.NewSuccessResponse(req.ID, map[string]interface{}{
		"message": "Reconciliation completed successfully",
	})
	return resp
}

// sendResponse sends a response to the client
func (d *Daemon) sendResponse(conn net.Conn, resp *protocol.Response) {
	data, err := protocol.MarshalResponse(resp)
	if err != nil {
		d.logger.Error("Failed to marshal response", "error", err)
		return
	}

	// Add newline for easier parsing
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		d.logger.Error("Failed to send response", "error", err)
	}
}

// shutdown gracefully shuts down the daemon
func (d *Daemon) shutdown() error {
	d.logger.Info("Shutting down daemon")

	// Notify systemd we're stopping
	if d.systemdMode {
		d.notifySystemd("STOPPING=1")
		d.notifySystemd("STATUS=Shutting down")
	}

	// Cancel context to stop accepting new connections
	d.cancel()

	// Close listener
	if d.listener != nil {
		if err := d.listener.Close(); err != nil {
			d.logger.Error("Failed to close listener", "error", err)
		}
	}

	// Wait for all connections to finish
	d.wg.Wait()

	// Clean up socket file if unix
	if d.config.Network == "unix" {
		if err := os.RemoveAll(d.config.Address); err != nil {
			d.logger.Error("Failed to remove socket file", "error", err)
		}
	}

	d.logger.Info("Daemon stopped")
	return nil
}

// checkExistingDaemon checks if another daemon instance is already running
func (d *Daemon) checkExistingDaemon() error {
	// First check if the socket file exists
	if _, err := os.Stat(d.config.Address); err != nil {
		if os.IsNotExist(err) {
			// Socket doesn't exist, we're good to go
			return nil
		}
		return fmt.Errorf("failed to check socket: %w", err)
	}

	// Socket exists, try to connect to it
	conn, err := net.Dial(d.config.Network, d.config.Address)
	if err != nil {
		// Can't connect, socket is stale
		d.logger.Debug("Found stale socket, will clean up", "address", d.config.Address)
		return nil
	}
	defer func() {
		_ = conn.Close()
	}()

	// Try to send a status request to verify it's actually a bankshot daemon
	req := &protocol.Request{
		ID:   "check-" + fmt.Sprintf("%d", time.Now().Unix()),
		Type: protocol.CommandStatus,
	}

	data, err := protocol.MarshalRequest(req)
	if err != nil {
		// Can't marshal request, assume it's not our daemon
		return nil
	}

	// Send request
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		// Can't set deadline, assume connection is not valid
		return nil
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		// Can't write, socket might be stale
		return nil
	}

	// Try to read response
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		// Can't set deadline, assume connection is not valid
		return nil
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		// Can't read response, might not be our daemon
		return nil
	}

	// Parse response
	resp, err := protocol.ParseResponse([]byte(line))
	if err != nil {
		// Invalid response, not our daemon
		return nil
	}

	// Check if it's a valid response (success or error)
	if resp.Success || resp.Error != "" {
		return fmt.Errorf("another bankshot daemon is already running at %s", d.config.Address)
	}

	// Some other response, but it's still a bankshot daemon
	return fmt.Errorf("another bankshot daemon is already running at %s", d.config.Address)
}

// autoDiscoverForwards discovers and registers existing SSH port forwards
func (d *Daemon) autoDiscoverForwards() error {
	d.logger.Info("Auto-discovering existing SSH port forwards")

	// Discover active forwards
	forwards, err := forwarder.DiscoverActiveForwards(d.logger)
	if err != nil {
		return fmt.Errorf("failed to discover forwards: %w", err)
	}

	d.logger.Info("Discovered forwards", "count", len(forwards))

	// Register each discovered forward
	registeredCount := 0
	for _, fwd := range forwards {
		// Skip if no connection info
		if fwd.ConnectionInfo == "" {
			d.logger.Debug("Skipping forward without connection info",
				"localPort", fwd.LocalPort)
			continue
		}

		// Register the forward in our forwarder (without executing SSH command)
		// Note: We're assuming the remote port is the same as local port
		// This might not always be accurate, but it's a reasonable default
		err := d.ssh.RegisterExistingForward(
			fwd.SocketPath,
			fwd.ConnectionInfo,
			fwd.RemotePort,
			fwd.LocalPort,
			fwd.RemoteHost,
		)
		if err != nil {
			d.logger.Warn("Failed to register discovered forward",
				"localPort", fwd.LocalPort,
				"connectionInfo", fwd.ConnectionInfo,
				"error", err)
			continue
		}

		registeredCount++
	}

	d.logger.Info("Auto-discovery complete",
		"discovered", len(forwards),
		"registered", registeredCount)

	return nil
}

// reconcileLoop periodically validates tracked forwards against actual listening ports
func (d *Daemon) reconcileLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.logger.Debug("Running periodic forward reconciliation")
			if err := d.forwarder.Reconcile(); err != nil {
				d.logger.Warn("Reconciliation failed", "error", err)
			}
		}
	}
}
