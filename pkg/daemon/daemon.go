package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/phinze/bankshot/pkg/clipboard"
	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/forwarder"
	"github.com/phinze/bankshot/pkg/notify"
	"github.com/phinze/bankshot/pkg/opener"
	"github.com/phinze/bankshot/pkg/opproxy"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/phinze/bankshot/pkg/tailnet"
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
	ssh         *forwarder.SSHForwarder     // also d.forwarder; kept concrete for startup discovery
	tailnet     *forwarder.TailnetForwarder // nil unless tailnet forwarding is enabled
	gate        *tailnet.Gate
	verify      func(ctx context.Context, relayAddr, id string) error // tailnet.Verify; swapped in tests
	agentToasts agentToasts
	busyMu      sync.Mutex
	busy        map[string]bool // "node:port" forwards already reported busy
	notifier    *notify.Notifier
	opProxy     *opproxy.OpProxy
	clipboard   clipboard.Reader
	startTime   time.Time
	systemdMode bool   // Running under systemd
	pidFile     string // PID file path
}

// New creates a new daemon instance
func New(cfg *config.Config, logger *slog.Logger) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	ssh := forwarder.NewSSH(logger, cfg.SSHCommand)
	d := &Daemon{
		config:    cfg,
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
		opener:    opener.New(logger),
		forwarder: ssh,
		ssh:       ssh,
		notifier:  notify.New(logger, cfg.NotifyCommand),
		opProxy:   opproxy.New(&cfg.OpProxy, logger),
		clipboard: clipboard.System{},
		startTime: time.Now(),
		busy:      make(map[string]bool),
		verify:    tailnet.Verify,
	}
	if cfg.Tailnet.Enabled {
		d.tailnet = forwarder.NewTailnet(logger, cfg.Tailnet.RelayPort, cfg.Tailnet.LeaseDuration())
		d.gate = tailnet.NewGate(cfg.Tailnet.Allow, tailnet.Whois, false)
	}
	return d
}

// peer is a caller that reached the daemon over the tailnet, identified by
// tailscale whois. A nil *peer means the local socket, i.e. the ssh path.
type peer struct {
	node string // tailnet node name; keys its forwards
	ip   string // tailnet IP; where its relay listens
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

	if d.tailnet != nil {
		d.wg.Add(2)
		go func() {
			defer d.wg.Done()
			d.tailnet.Run(d.ctx)
		}()
		go d.serveTailnet()
	}

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
			d.handleConnection(conn, nil)
		}()
	}
}

// serveTailnet listens for monitors on this machine's tailnet address. At
// login the tailnet may not be up yet, so it keeps trying until it is.
func (d *Daemon) serveTailnet() {
	defer d.wg.Done()

	var ln net.Listener
	for delay := time.Second; ; delay = min(delay*2, time.Minute) {
		ip, err := tailnet.IPv4(d.ctx)
		if err == nil {
			ln, err = net.Listen("tcp", net.JoinHostPort(ip, fmt.Sprint(d.config.Tailnet.ControlPort)))
		}
		if err == nil {
			break
		}
		d.logger.Warn("Tailnet listener not up yet, retrying", "error", err, "in", delay)
		select {
		case <-d.ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	go func() {
		<-d.ctx.Done()
		_ = ln.Close()
	}()
	d.logger.Info("Listening for monitors on the tailnet", "address", ln.Addr())

	for {
		conn, err := ln.Accept()
		if err != nil {
			if d.ctx.Err() != nil {
				return
			}
			d.logger.Error("Failed to accept tailnet connection", "error", err)
			continue
		}
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			id, err := d.gate.Check(d.ctx, conn.RemoteAddr().String())
			if err != nil {
				d.logger.Warn("Rejected tailnet connection", "remote", conn.RemoteAddr(), "error", err)
				_ = conn.Close()
				return
			}
			ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
			d.handleConnection(conn, &peer{node: id.Node, ip: ip})
		}()
	}
}

// handleConnection handles a single connection
func (d *Daemon) handleConnection(conn net.Conn, p *peer) {
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

	// An agent request turns this connection into an agent stream.
	if req.Type == protocol.CommandAgent {
		if p == nil {
			d.sendResponse(conn, protocol.NewErrorResponse(req.ID, fmt.Errorf("agent is only available over the tailnet")))
			return
		}
		d.serveAgent(conn, reader, req, p)
		return
	}

	if isConnectivityProbe(req) {
		d.logger.Debug("Received command", "type", req.Type, "id", req.ID, "remote", remoteAddr)
	} else {
		d.logger.Info("Received command", "type", req.Type, "id", req.ID, "remote", remoteAddr)
	}

	// Handle command
	resp := d.handleCommand(req, p)

	// Send response
	d.sendResponse(conn, resp)

	d.logger.Debug("Connection closed", "remote", remoteAddr)
}

// handleCommand processes a command and returns a response. Open,
// op-proxy and clipboard act on this machine as the user, so a tailnet peer's request must
// come through its monitor, which the peer's relay vouches for; anything else
// on that host can reach this listener but can't get the relay to vouch.
func (d *Daemon) handleCommand(req *protocol.Request, p *peer) *protocol.Response {
	if p != nil {
		switch req.Type {
		case protocol.CommandForward, protocol.CommandForwardBatch, protocol.CommandUnforward,
			protocol.CommandSync, protocol.CommandStatus, protocol.CommandList:
		case protocol.CommandOpen, protocol.CommandOpProxy, protocol.CommandClipboard:
			if err := d.verifyPeerRequest(req, p); err != nil {
				d.logger.Warn("Refused unvouched tailnet request", "type", req.Type, "node", p.node, "error", err)
				return protocol.NewErrorResponse(req.ID, fmt.Errorf("%s refused: %w", req.Type, err))
			}
		default:
			return protocol.NewErrorResponse(req.ID, fmt.Errorf("%s is not available over the tailnet", req.Type))
		}
	}
	switch req.Type {
	case protocol.CommandOpen:
		return d.handleOpenCommand(req)
	case protocol.CommandStatus:
		return d.handleStatusCommand(req)
	case protocol.CommandList:
		return d.handleListCommand(req)
	case protocol.CommandForward:
		return d.handleForwardCommand(req, p)
	case protocol.CommandForwardBatch:
		return d.handleForwardBatchCommand(req, p)
	case protocol.CommandUnforward:
		return d.handleUnforwardCommand(req, p)
	case protocol.CommandSync:
		return d.handleSyncCommand(req, p)
	case protocol.CommandReconcile:
		return d.handleReconcileCommand(req)
	case protocol.CommandOpProxy:
		return d.handleOpProxyCommand(req)
	case protocol.CommandClipboard:
		return d.handleClipboardCommand(req)
	default:
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("unknown command type: %s", req.Type))
	}
}

func isConnectivityProbe(req *protocol.Request) bool {
	return req.Type == protocol.CommandStatus && strings.HasPrefix(req.ID, "connectivity-check-")
}

// verifyPeerRequest checks that a tailnet peer's request came from its
// monitor, by asking the peer's relay to vouch for the request ID, and stamps
// the origin with the node whois named.
func (d *Daemon) verifyPeerRequest(req *protocol.Request, p *peer) error {
	if req.Origin == nil {
		return fmt.Errorf("no origin; send it through the monitor")
	}
	ctx, cancel := context.WithTimeout(d.ctx, verifyTimeout)
	defer cancel()
	relay := net.JoinHostPort(p.ip, fmt.Sprint(d.config.Tailnet.RelayPort))
	if err := d.verify(ctx, relay, req.ID); err != nil {
		return err
	}
	req.Origin.Node = p.node
	return nil
}

const verifyTimeout = 5 * time.Second

// handleOpenCommand handles the open URL command
func (d *Daemon) handleOpenCommand(req *protocol.Request) *protocol.Response {
	// Parse payload
	var openReq protocol.OpenRequest
	if err := json.Unmarshal(req.Payload, &openReq); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid payload: %w", err))
	}

	if req.Origin != nil {
		d.logger.Info("Opening URL for remote caller", "url", openReq.URL, "origin", req.Origin.String())
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
	forwards := d.listForwards()
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
	forwards := d.listForwards()

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
func (d *Daemon) handleForwardCommand(req *protocol.Request, p *peer) *protocol.Response {
	// Parse payload
	var forwardReq protocol.ForwardRequest
	if err := json.Unmarshal(req.Payload, &forwardReq); err != nil {
		d.logger.Error("Failed to parse forward request",
			"error", err,
			"payload", string(req.Payload))
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid forward request format: %w", err))
	}

	fwdr, target := d.route(p, forwardReq.ConnectionInfo, forwardReq.SocketPath)
	results, err := fwdr.AddForwards(target, []forwarder.Port{{
		RemotePort: forwardReq.RemotePort,
		LocalPort:  forwardReq.LocalPort,
		Host:       forwardReq.Host,
	}})
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	res := results[0]
	d.noteBusy(target.ConnectionInfo, res)
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
func (d *Daemon) handleForwardBatchCommand(req *protocol.Request, p *peer) *protocol.Response {
	var batchReq protocol.BatchForwardRequest
	if err := json.Unmarshal(req.Payload, &batchReq); err != nil {
		d.logger.Error("Failed to parse batch forward request",
			"error", err,
			"payload", string(req.Payload))
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid batch forward request format: %w", err))
	}

	fwdr, target := d.route(p, batchReq.ConnectionInfo, batchReq.SocketPath)
	fwdResults, err := fwdr.AddForwards(target, batchPorts(batchReq.Forwards))
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	results := d.reportBatch(target.ConnectionInfo, batchReq.Forwards, fwdResults)

	resp, err := protocol.NewSuccessResponse(req.ID, protocol.BatchForwardResponse{Results: results})
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

// handleUnforwardCommand handles the port unforward command
func (d *Daemon) handleUnforwardCommand(req *protocol.Request, p *peer) *protocol.Response {
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
	fwdr, target := d.route(p, unforwardReq.ConnectionInfo, "")
	d.clearBusy(target.ConnectionInfo, unforwardReq.RemotePort)
	if err := fwdr.RemoveForward(target.ConnectionInfo, unforwardReq.RemotePort, host); err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	// Return success
	resp, _ := protocol.NewSuccessResponse(req.ID, map[string]interface{}{
		"message": fmt.Sprintf("Removed forward for %s:%d",
			host, unforwardReq.RemotePort),
	})
	return resp
}

// handleSyncCommand converges a tailnet peer's forwards to the full set it
// declares and renews its lease. Monitors send it on start and as their
// heartbeat.
func (d *Daemon) handleSyncCommand(req *protocol.Request, p *peer) *protocol.Response {
	if p == nil || d.tailnet == nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("sync is only available over the tailnet"))
	}
	var syncReq protocol.SyncRequest
	if err := json.Unmarshal(req.Payload, &syncReq); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid sync request format: %w", err))
	}

	_, target := d.route(p, "", "")
	fwdResults, err := d.tailnet.Sync(target, batchPorts(syncReq.Forwards))
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	results := d.reportBatch(target.ConnectionInfo, syncReq.Forwards, fwdResults)

	resp, err := protocol.NewSuccessResponse(req.ID, protocol.SyncResponse{
		Results:      results,
		LeaseSeconds: int(d.tailnet.Lease().Seconds()),
	})
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

// route picks the forwarder for a caller and the target its forwards come
// from. A tailnet peer's forwards are keyed by the node whois named and relay
// to the address it called from, whatever its request claims.
func (d *Daemon) route(p *peer, connectionInfo, socketPath string) (forwarder.Forwarder, forwarder.Target) {
	if p == nil {
		return d.forwarder, forwarder.Target{ConnectionInfo: connectionInfo, SocketPath: socketPath}
	}
	return d.tailnet, forwarder.Target{ConnectionInfo: p.node, Address: p.ip}
}

func batchPorts(forwards []protocol.BatchForwardPort) []forwarder.Port {
	ports := make([]forwarder.Port, 0, len(forwards))
	for _, fwd := range forwards {
		ports = append(ports, forwarder.Port{RemotePort: fwd.RemotePort, LocalPort: fwd.LocalPort, Host: fwd.Host})
	}
	return ports
}

// reportBatch turns per-port forward results into protocol results, logging
// failures and posting one notification for the ports newly forwarded.
func (d *Daemon) reportBatch(connectionInfo string, requested []protocol.BatchForwardPort, fwdResults []forwarder.Result) []protocol.BatchForwardResult {
	results := make([]protocol.BatchForwardResult, 0, len(fwdResults))
	var created []notify.ForwardEvent

	for i, r := range fwdResults {
		fwd := requested[i]
		res := protocol.BatchForwardResult{
			RemotePort: r.RemotePort,
			LocalPort:  r.LocalPort,
			Created:    r.Created,
		}
		news := d.noteBusy(connectionInfo, r)
		if r.Err != nil {
			res.Error = r.Err.Error()
			log := d.logger.Warn
			if !news {
				log = d.logger.Debug // a busy port already reported, retried by a sync
			}
			log("Batch forward failed for port",
				"port", r.RemotePort,
				"connectionInfo", connectionInfo,
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
		d.notifier.NotifyForwards(connectionInfo, created)
	}
	return results
}

// noteBusy tells the user the first time a forward can't take its local port.
// Monitors retry on every sync, so later failures stay quiet until the port
// forwards or the monitor stops asking for it. It reports whether r is news:
// anything but a busy port already reported.
func (d *Daemon) noteBusy(connectionInfo string, r forwarder.Result) bool {
	key := fmt.Sprintf("%s:%d", connectionInfo, r.RemotePort)
	d.busyMu.Lock()
	defer d.busyMu.Unlock()
	if !errors.Is(r.Err, forwarder.ErrPortBusy) {
		delete(d.busy, key)
		return true
	}
	if d.busy[key] {
		return false
	}
	d.busy[key] = true
	d.notifier.NotifyForwardBusy(connectionInfo, r.RemotePort, r.Err)
	return true
}

func (d *Daemon) clearBusy(connectionInfo string, remotePort int) {
	d.busyMu.Lock()
	defer d.busyMu.Unlock()
	delete(d.busy, fmt.Sprintf("%s:%d", connectionInfo, remotePort))
}

// listForwards returns the forwards of every transport.
func (d *Daemon) listForwards() []*forwarder.Forward {
	forwards := d.forwarder.ListForwards()
	if d.tailnet != nil {
		forwards = append(forwards, d.tailnet.ListForwards()...)
	}
	return forwards
}

func (d *Daemon) reconcileAll() error {
	err := d.forwarder.Reconcile()
	if d.tailnet != nil {
		err = errors.Join(err, d.tailnet.Reconcile())
	}
	return err
}

// handleOpProxyCommand handles the op-proxy command
func (d *Daemon) handleOpProxyCommand(req *protocol.Request) *protocol.Response {
	var opReq protocol.OpProxyRequest
	if err := json.Unmarshal(req.Payload, &opReq); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid op-proxy request format: %w", err))
	}

	if err := d.opProxy.Check(opReq); err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	// Say who's asking before op runs, so the toast arrives alongside any
	// 1Password prompt rather than after it.
	var origin string
	if req.Origin != nil {
		origin = req.Origin.String()
		d.logger.Info("Running op for remote caller", "args", opReq.Args, "origin", origin)
	}
	d.notifier.NotifyOpProxy(opReq.Args, origin)

	opResp, err := d.opProxy.Execute(opReq)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	resp, err := protocol.NewSuccessResponse(req.ID, opResp)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

// handleClipboardCommand sends the clipboard's image, if it holds one. A
// sent image raises a toast naming the caller: any of the user's processes
// on the remote host can ask, so the toast is what keeps it visible. A peek
// or an empty clipboard stays quiet.
func (d *Daemon) handleClipboardCommand(req *protocol.Request) *protocol.Response {
	var clipReq protocol.ClipboardRequest
	if err := json.Unmarshal(req.Payload, &clipReq); err != nil {
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid clipboard request format: %w", err))
	}

	ctx, cancel := context.WithTimeout(d.ctx, clipboardTimeout)
	defer cancel()
	var clipResp protocol.ClipboardResponse
	if clipReq.Peek {
		ok, err := d.clipboard.HasImage(ctx)
		if err != nil {
			return protocol.NewErrorResponse(req.ID, err)
		}
		clipResp.Image = ok
	} else {
		data, err := d.clipboard.PNG(ctx)
		switch {
		case errors.Is(err, clipboard.ErrNoImage):
		case err != nil:
			return protocol.NewErrorResponse(req.ID, err)
		default:
			origin := ""
			if req.Origin != nil {
				origin = req.Origin.String()
			}
			d.logger.Info("Sending clipboard image", "bytes", len(data), "origin", origin)
			d.notifier.NotifyClipboard(origin, len(data))
			clipResp = protocol.ClipboardResponse{Image: true, Data: data}
		}
	}

	resp, err := protocol.NewSuccessResponse(req.ID, clipResp)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}
	return resp
}

const clipboardTimeout = 10 * time.Second

// handleReconcileCommand handles the reconcile command
func (d *Daemon) handleReconcileCommand(req *protocol.Request) *protocol.Response {
	d.logger.Info("Reconciliation requested via API")

	// Trigger reconciliation
	if err := d.reconcileAll(); err != nil {
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
			if err := d.reconcileAll(); err != nil {
				d.logger.Warn("Reconciliation failed", "error", err)
			}
		}
	}
}
