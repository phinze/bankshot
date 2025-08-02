package forwarder

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/go-homedir"
)

// Forward represents an active port forward
type Forward struct {
	RemotePort int
	LocalPort  int
	Host       string
	SocketPath string
	CreatedAt  time.Time
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
func (f *Forwarder) AddForward(socketPath string, remotePort, localPort int, host string) error {
	if host == "" {
		host = "localhost"
	}
	if localPort == 0 {
		localPort = remotePort
	}

	key := fmt.Sprintf("%s:%d", host, remotePort)

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
		socketPath,
	)

	f.logger.Info("Executing port forward",
		"command", strings.Join(cmd.Args, " "),
		"remote", fmt.Sprintf("%s:%d", host, remotePort),
		"local", localPort,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to forward port: %w (output: %s)", err, string(output))
	}

	// Store forward info
	forward := &Forward{
		RemotePort: remotePort,
		LocalPort:  localPort,
		Host:       host,
		SocketPath: socketPath,
		CreatedAt:  time.Now(),
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

// RemoveForward removes a port forward
func (f *Forwarder) RemoveForward(socketPath string, remotePort int, host string) error {
	if host == "" {
		host = "localhost"
	}

	key := fmt.Sprintf("%s:%d", host, remotePort)

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
	cmd := exec.Command(f.sshCmd,
		"-O", "cancel",
		"-L", fmt.Sprintf("%d:%s:%d", localPort, host, remotePort),
		socketPath,
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
	// Parse connection info (could be hostname, user@host, etc.)
	// For now, we'll look for common socket patterns

	home, err := homedir.Dir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Common ControlPath patterns
	patterns := []string{
		// Default OpenSSH pattern
		filepath.Join(home, ".ssh", "master-*"),
		// Common custom patterns
		filepath.Join(home, ".ssh", "sockets", "*"),
		filepath.Join(home, ".ssh", "control", "*"),
		// Look for specific connection
		filepath.Join(home, ".ssh", fmt.Sprintf("*%s*", connectionInfo)),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		for _, match := range matches {
			// Check if it's a socket
			info, err := os.Stat(match)
			if err != nil {
				continue
			}

			// Check if it's a socket file
			if info.Mode()&os.ModeSocket != 0 {
				// Verify it's an active SSH control socket
				cmd := exec.Command("ssh", "-O", "check", match)
				if err := cmd.Run(); err == nil {
					return match, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no SSH ControlMaster socket found for connection: %s", connectionInfo)
}

// CleanupForSocket removes all forwards for a specific socket
func (f *Forwarder) CleanupForSocket(socketPath string) {
	f.mu.RLock()
	var toRemove []string
	for key, forward := range f.forwards {
		if forward.SocketPath == socketPath {
			toRemove = append(toRemove, key)
		}
	}
	f.mu.RUnlock()

	for _, key := range toRemove {
		parts := strings.Split(key, ":")
		if len(parts) == 2 {
			host := parts[0]
			var port int
			fmt.Sscanf(parts[1], "%d", &port)
			f.RemoveForward(socketPath, port, host)
		}
	}
}