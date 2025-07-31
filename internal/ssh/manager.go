package ssh

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
)

// Manager handles SSH ControlMaster operations
type Manager struct {
	conn          *Connection
	logger        *slog.Logger
	mu            sync.Mutex
	activeFwds    map[int]bool
	sshCommand    string
}

// NewManager creates a new SSH manager
func NewManager(logger *slog.Logger) (*Manager, error) {
	conn, err := DetectSSHConnection()
	if err != nil {
		return nil, err
	}
	
	m := &Manager{
		conn:       conn,
		logger:     logger,
		activeFwds: make(map[int]bool),
		sshCommand: "ssh",
	}
	
	// Validate the control socket
	if err := m.validateControlSocket(); err != nil {
		return nil, fmt.Errorf("invalid control socket: %w", err)
	}
	
	logger.Info("SSH ControlMaster detected",
		slog.String("socket", conn.ControlPath),
		slog.String("host", conn.Host),
	)
	
	return m, nil
}

// validateControlSocket checks if the control socket is valid
func (m *Manager) validateControlSocket() error {
	// Use ssh -O check to validate the socket
	cmd := exec.Command(m.sshCommand, "-O", "check", "-S", m.conn.ControlPath, "dummy")
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		// Check if the error is because the socket is valid
		// SSH returns exit code 255 with "Master running" message for valid sockets
		outputStr := string(output)
		if strings.Contains(outputStr, "Master running") {
			return nil
		}
		return fmt.Errorf("control socket check failed: %s", outputStr)
	}
	
	return nil
}

// AddPortForward adds a port forward via ControlMaster
func (m *Manager) AddPortForward(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Check if already forwarded
	if m.activeFwds[port] {
		m.logger.Debug("port already forwarded", slog.Int("port", port))
		return nil
	}
	
	// Build the forward spec: local:localhost:remote
	forwardSpec := fmt.Sprintf("%d:localhost:%d", port, port)
	
	// Execute ssh -O forward command
	cmd := exec.Command(m.sshCommand, 
		"-O", "forward",
		"-L", forwardSpec,
		"-S", m.conn.ControlPath,
		"dummy", // dummy argument required by SSH
	)
	
	m.logger.Debug("executing SSH forward command", 
		slog.String("command", cmd.String()),
		slog.String("forward", forwardSpec),
	)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if the error is benign (e.g., already forwarded)
		outputStr := string(output)
		if strings.Contains(outputStr, "already forwarded") || 
		   strings.Contains(outputStr, "Address already in use") {
			m.logger.Debug("port forward already exists", slog.Int("port", port))
			m.activeFwds[port] = true
			return nil
		}
		return fmt.Errorf("failed to add port forward: %s", outputStr)
	}
	
	m.activeFwds[port] = true
	m.logger.Info("port forward added", 
		slog.Int("port", port),
		slog.String("forward", forwardSpec),
	)
	
	return nil
}

// RemovePortForward removes a port forward via ControlMaster
func (m *Manager) RemovePortForward(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Check if actually forwarded
	if !m.activeFwds[port] {
		m.logger.Debug("port not forwarded", slog.Int("port", port))
		return nil
	}
	
	// Build the forward spec
	forwardSpec := fmt.Sprintf("%d:localhost:%d", port, port)
	
	// Execute ssh -O cancel command
	cmd := exec.Command(m.sshCommand,
		"-O", "cancel",
		"-L", forwardSpec,
		"-S", m.conn.ControlPath,
		"dummy",
	)
	
	m.logger.Debug("executing SSH cancel command", 
		slog.String("command", cmd.String()),
		slog.String("forward", forwardSpec),
	)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		// Some SSH versions don't support -O cancel, log but don't fail
		m.logger.Warn("failed to remove port forward (may not be supported)", 
			slog.String("error", outputStr),
			slog.Int("port", port),
		)
	}
	
	delete(m.activeFwds, port)
	m.logger.Info("port forward removed", 
		slog.Int("port", port),
		slog.String("forward", forwardSpec),
	)
	
	return nil
}

// Cleanup removes all active port forwards
func (m *Manager) Cleanup() {
	m.mu.Lock()
	ports := make([]int, 0, len(m.activeFwds))
	for port := range m.activeFwds {
		ports = append(ports, port)
	}
	m.mu.Unlock()
	
	for _, port := range ports {
		if err := m.RemovePortForward(port); err != nil {
			m.logger.Error("failed to remove port forward during cleanup",
				slog.Int("port", port),
				slog.String("error", err.Error()),
			)
		}
	}
}