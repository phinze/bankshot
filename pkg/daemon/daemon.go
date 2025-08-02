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
	"sync"
	"syscall"
	"time"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/forwarder"
	"github.com/phinze/bankshot/pkg/opener"
	"github.com/phinze/bankshot/pkg/protocol"
)

// Daemon represents the bankshot daemon
type Daemon struct {
	config    *config.Config
	listener  net.Listener
	logger    *slog.Logger
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	opener    *opener.Opener
	forwarder *forwarder.Forwarder
	startTime time.Time
}

// New creates a new daemon instance
func New(cfg *config.Config, logger *slog.Logger) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	return &Daemon{
		config:    cfg,
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
		opener:    opener.New(logger),
		forwarder: forwarder.New(logger, cfg.SSHCommand),
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
		// Set umask for socket permissions
		oldUmask := syscall.Umask(0077)
		defer syscall.Umask(oldUmask)

		// Remove existing socket
		if err := os.RemoveAll(d.config.Address); err != nil {
			return fmt.Errorf("failed to remove existing socket: %w", err)
		}

		// Ensure directory exists
		socketDir := filepath.Dir(d.config.Address)
		if err := os.MkdirAll(socketDir, 0700); err != nil {
			return fmt.Errorf("failed to create socket directory: %w", err)
		}
	}

	// Start listener
	listener, err := net.Listen(d.config.Network, d.config.Address)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	d.listener = listener

	d.logger.Info("Daemon started",
		"network", d.config.Network,
		"address", d.config.Address,
	)

	// Start accepting connections
	d.wg.Add(1)
	go d.acceptConnections()

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
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	d.logger.Debug("New connection", "remote", remoteAddr)

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

	d.logger.Info("Received command", "type", req.Type, "id", req.ID, "remote", remoteAddr)

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
	default:
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("unknown command type: %s", req.Type))
	}
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
	
	status := protocol.StatusResponse{
		Version:        "0.1.0", // TODO: Use version from build
		Uptime:         uptime,
		ActiveForwards: len(d.forwarder.ListForwards()),
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
			RemotePort: fwd.RemotePort,
			LocalPort:  fwd.LocalPort,
			Host:       fwd.Host,
			CreatedAt:  fwd.CreatedAt.Format(time.RFC3339),
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
		return protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid payload: %w", err))
	}

	// Find socket path if not provided
	socketPath := forwardReq.SocketPath
	if socketPath == "" {
		var err error
		socketPath, err = forwarder.FindControlSocket(forwardReq.ConnectionInfo)
		if err != nil {
			return protocol.NewErrorResponse(req.ID, fmt.Errorf("failed to find SSH socket: %w", err))
		}
	}

	// Add forward
	if err := d.forwarder.AddForward(socketPath, forwardReq.RemotePort, forwardReq.LocalPort, forwardReq.Host); err != nil {
		return protocol.NewErrorResponse(req.ID, err)
	}

	// Default values
	host := forwardReq.Host
	if host == "" {
		host = "localhost"
	}
	localPort := forwardReq.LocalPort
	if localPort == 0 {
		localPort = forwardReq.RemotePort
	}

	// Return success
	resp, _ := protocol.NewSuccessResponse(req.ID, map[string]interface{}{
		"message": fmt.Sprintf("Forwarded %s:%d to localhost:%d", 
			host, forwardReq.RemotePort, localPort),
		"socket_path": socketPath,
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