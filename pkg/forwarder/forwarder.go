package forwarder

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Forward represents an active port forward
type Forward struct {
	RemotePort     int
	LocalPort      int
	Host           string
	SocketPath     string
	ConnectionInfo string // SSH connection target (e.g., hostname)
	CreatedAt      time.Time
}

// Forwarder manages SSH port forwards
type Forwarder struct {
	logger   *slog.Logger
	sshCmd   string
	forwards map[string]*Forward // key: "host:remotePort"
	mu       sync.RWMutex
}

// New creates a new Forwarder
func New(logger *slog.Logger, sshCmd string) *Forwarder {
	return &Forwarder{
		logger:   logger,
		sshCmd:   sshCmd,
		forwards: make(map[string]*Forward),
	}
}

// AddForward creates a new port forward
func (f *Forwarder) AddForward(socketPath string, connectionInfo string, remotePort, localPort int, host string) error {
	if host == "" {
		host = "localhost"
	}
	if localPort == 0 {
		localPort = remotePort
	}

	// Include connection info in key to support multiple SSH sessions
	key := fmt.Sprintf("%s:%s:%d", connectionInfo, host, remotePort)

	// Check if already forwarded
	f.mu.RLock()
	if existing, ok := f.forwards[key]; ok {
		f.mu.RUnlock()
		f.logger.Info("Port already forwarded",
			"remote", fmt.Sprintf("%s:%d", host, remotePort),
			"local", existing.LocalPort,
		)
		return nil
	}
	f.mu.RUnlock()

	// Execute SSH forward command
	cmd := exec.Command(f.sshCmd,
		"-O", "forward",
		"-L", fmt.Sprintf("%d:%s:%d", localPort, host, remotePort),
		connectionInfo,
	)

	f.logger.Info("Executing port forward",
		"command", strings.Join(cmd.Args, " "),
		"remote", fmt.Sprintf("%s:%d", host, remotePort),
		"local", localPort,
		"socketPath", socketPath,
		"connectionInfo", connectionInfo,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to forward port: %w (output: %s)", err, string(output))
	}

	// Store forward info
	forward := &Forward{
		RemotePort:     remotePort,
		LocalPort:      localPort,
		Host:           host,
		SocketPath:     socketPath,
		ConnectionInfo: connectionInfo,
		CreatedAt:      time.Now(),
	}

	f.mu.Lock()
	f.forwards[key] = forward
	f.mu.Unlock()

	f.logger.Info("Port forward established",
		"remote", fmt.Sprintf("%s:%d", host, remotePort),
		"local", localPort,
	)

	return nil
}

// RegisterExistingForward registers a forward that already exists (e.g., discovered on startup)
func (f *Forwarder) RegisterExistingForward(socketPath string, connectionInfo string, remotePort, localPort int, host string) error {
	if host == "" {
		host = "localhost"
	}
	if localPort == 0 {
		localPort = remotePort
	}

	// Include connection info in key to support multiple SSH sessions
	key := fmt.Sprintf("%s:%s:%d", connectionInfo, host, remotePort)

	// Check if already registered
	f.mu.RLock()
	if existing, ok := f.forwards[key]; ok {
		f.mu.RUnlock()
		f.logger.Debug("Forward already registered",
			"remote", fmt.Sprintf("%s:%d", host, remotePort),
			"local", existing.LocalPort,
		)
		return nil
	}
	f.mu.RUnlock()

	// Store forward info without executing SSH command
	forward := &Forward{
		RemotePort:     remotePort,
		LocalPort:      localPort,
		Host:           host,
		SocketPath:     socketPath,
		ConnectionInfo: connectionInfo,
		CreatedAt:      time.Now(),
	}

	f.mu.Lock()
	f.forwards[key] = forward
	f.mu.Unlock()

	f.logger.Info("Registered existing forward",
		"remote", fmt.Sprintf("%s:%d", host, remotePort),
		"local", localPort,
		"connectionInfo", connectionInfo,
	)

	return nil
}

// RemoveForward removes a port forward
func (f *Forwarder) RemoveForward(connectionInfo string, remotePort int, host string) error {
	if host == "" {
		host = "localhost"
	}

	// Include connection info in key to support multiple SSH sessions
	key := fmt.Sprintf("%s:%s:%d", connectionInfo, host, remotePort)

	// Get forward info
	f.mu.RLock()
	forward, ok := f.forwards[key]
	if !ok {
		f.mu.RUnlock()
		return fmt.Errorf("forward not found: %s", key)
	}
	localPort := forward.LocalPort
	f.mu.RUnlock()

	// Execute SSH cancel command
	// WARNING: OpenSSH has a limitation where -O cancel will cancel ALL remote
	// socket forwards on the control socket, not just the specified one. This
	// includes any Unix socket forwards (like .bankshot.sock). See below for our
	// workaround to address this.
	cmd := exec.Command(f.sshCmd,
		"-O", "cancel",
		"-L", fmt.Sprintf("%d:%s:%d", localPort, host, remotePort),
		connectionInfo,
	)

	f.logger.Info("Canceling port forward",
		"command", strings.Join(cmd.Args, " "),
		"remote", fmt.Sprintf("%s:%d", host, remotePort),
		"local", localPort,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Log but don't fail - forward might already be gone
		f.logger.Warn("Failed to cancel port forward",
			"error", err,
			"output", string(output),
		)
	}

	// Remove from map
	f.mu.Lock()
	delete(f.forwards, key)
	f.mu.Unlock()

	// Re-establish all configured forwards (including Unix socket forwards)
	// This is necessary because SSH -O cancel removes ALL socket remote forwards
	reestablishCmd := exec.Command(f.sshCmd, "-O", "forward", connectionInfo)

	f.logger.Info("Re-establishing configured forwards after cancel",
		"command", strings.Join(reestablishCmd.Args, " "),
	)

	reestablishOutput, reestablishErr := reestablishCmd.CombinedOutput()
	if reestablishErr != nil {
		f.logger.Error("Failed to re-establish forwards",
			"error", reestablishErr,
			"output", string(reestablishOutput),
		)
		// Don't fail the operation - the forward was still removed
	} else {
		f.logger.Info("Successfully re-established configured forwards",
			"output", string(reestablishOutput),
		)
	}

	return nil
}

// ListForwards returns all active forwards
func (f *Forwarder) ListForwards() []*Forward {
	f.mu.RLock()
	defer f.mu.RUnlock()

	forwards := make([]*Forward, 0, len(f.forwards))
	for _, fwd := range f.forwards {
		forwards = append(forwards, fwd)
	}
	return forwards
}

// FindControlSocket finds the SSH ControlMaster socket for a given connection
func FindControlSocket(connectionInfo string) (string, error) {
	// First, verify the connection is active
	checkCmd := exec.Command("ssh", "-O", "check", connectionInfo)
	if err := checkCmd.Run(); err != nil {
		return "", fmt.Errorf("no active SSH connection to %s", connectionInfo)
	}

	// Use ssh -G to get the actual configuration
	cmd := exec.Command("ssh", "-G", connectionInfo)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get SSH config for %s: %w", connectionInfo, err)
	}

	// Parse the output to find ControlPath
	var controlPath string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "controlpath" {
			controlPath = strings.Join(parts[1:], " ")
			break
		}
	}

	if controlPath == "" {
		return "", fmt.Errorf("no ControlPath configured for %s", connectionInfo)
	}

	// The control path might contain % tokens that need to be expanded
	// ssh -G should have already expanded them, but let's verify the socket exists
	if _, err := os.Stat(controlPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("control socket does not exist at %s", controlPath)
		}
		return "", fmt.Errorf("failed to stat control socket: %w", err)
	}

	// Verify it's actually a socket
	info, err := os.Stat(controlPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat control socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return "", fmt.Errorf("path %s exists but is not a socket", controlPath)
	}

	return controlPath, nil
}

// CleanupForSocket removes all forwards for a specific socket
func (f *Forwarder) CleanupForSocket(socketPath string) {
	f.mu.RLock()
	var toRemove []struct {
		key            string
		connectionInfo string
		host           string
		port           int
	}
	for key, forward := range f.forwards {
		if forward.SocketPath == socketPath {
			toRemove = append(toRemove, struct {
				key            string
				connectionInfo string
				host           string
				port           int
			}{
				key:            key,
				connectionInfo: forward.ConnectionInfo,
				host:           forward.Host,
				port:           forward.RemotePort,
			})
		}
	}
	f.mu.RUnlock()

	for _, item := range toRemove {
		f.RemoveForward(item.connectionInfo, item.port, item.host)
	}
}

// CleanupForConnection removes all forwards for a specific connection
func (f *Forwarder) CleanupForConnection(connectionInfo string) {
	f.mu.RLock()
	var toRemove []struct {
		host string
		port int
	}
	for _, forward := range f.forwards {
		if forward.ConnectionInfo == connectionInfo {
			toRemove = append(toRemove, struct {
				host string
				port int
			}{
				host: forward.Host,
				port: forward.RemotePort,
			})
		}
	}
	f.mu.RUnlock()

	for _, item := range toRemove {
		f.RemoveForward(connectionInfo, item.port, item.host)
	}
}

// ListConnectionForwards returns forwards for a specific connection
func (f *Forwarder) ListConnectionForwards(connectionInfo string) []*Forward {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var forwards []*Forward
	for _, fwd := range f.forwards {
		if fwd.ConnectionInfo == connectionInfo {
			forwards = append(forwards, fwd)
		}
	}
	return forwards
}
