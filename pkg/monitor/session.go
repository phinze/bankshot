package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
)

// SessionMonitor manages port forwarding for an SSH session
type SessionMonitor struct {
	sessionID       string
	multiMonitor    *MultiProcessMonitor
	daemonClient    DaemonClient
	logger          *slog.Logger
	portRanges      []PortRange
	ignoreProcesses []string
	gracePeriod     time.Duration
	activeForwards  map[string]ForwardInfo // key: "pid:port"
	pendingRemovals map[string]time.Time   // forwards pending removal
	mutex           sync.RWMutex
}

// PortRange defines a range of ports to auto-forward
type PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ForwardInfo tracks an active forward
type ForwardInfo struct {
	PID         int
	Port        int
	ProcessName string
	RequestID   string
	CreatedAt   time.Time
}

// DaemonClient interface for communicating with the daemon
type DaemonClient interface {
	SendRequest(req *protocol.Request) (*protocol.Response, error)
}

// SessionConfig holds configuration for the session monitor
type SessionConfig struct {
	SessionID       string
	DaemonClient    DaemonClient
	PortRanges      []PortRange
	IgnoreProcesses []string
	PollInterval    time.Duration
	GracePeriod     time.Duration
	Logger          *slog.Logger
}

// NewSessionMonitor creates a new session monitor
func NewSessionMonitor(cfg SessionConfig) (*SessionMonitor, error) {
	multiMonitor, err := NewMultiProcessMonitor(cfg.Logger, cfg.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to create multi-process monitor: %w", err)
	}

	return &SessionMonitor{
		sessionID:       cfg.SessionID,
		multiMonitor:    multiMonitor,
		daemonClient:    cfg.DaemonClient,
		logger:          cfg.Logger,
		portRanges:      cfg.PortRanges,
		ignoreProcesses: cfg.IgnoreProcesses,
		gracePeriod:     cfg.GracePeriod,
		activeForwards:  make(map[string]ForwardInfo),
		pendingRemovals: make(map[string]time.Time),
	}, nil
}

// Start begins monitoring and auto-forwarding
func (m *SessionMonitor) Start(ctx context.Context) error {
	m.logger.Info("Starting session monitor",
		"session", m.sessionID,
		"portRanges", m.portRanges,
		"ignoreProcesses", m.ignoreProcesses)

	// Start multi-process monitoring
	go m.multiMonitor.Start(ctx)

	// Handle events
	go m.handleEvents(ctx)

	// Periodic cleanup
	go m.cleanupLoop(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	return m.cleanup()
}

// handleEvents processes port events and manages forwards
func (m *SessionMonitor) handleEvents(ctx context.Context) {
	events := m.multiMonitor.GetEvents()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			m.handlePortEvent(event)
		}
	}
}

// handlePortEvent processes a single port event
func (m *SessionMonitor) handlePortEvent(event PortEvent) {
	// Check if process should be ignored
	if m.shouldIgnoreProcess(event.ProcessName) {
		m.logger.Debug("Ignoring port event from ignored process",
			"process", event.ProcessName,
			"port", event.Port)
		return
	}

	// Check if port is in auto-forward range
	if !m.isPortInRange(event.Port) {
		m.logger.Debug("Port outside auto-forward range",
			"port", event.Port,
			"ranges", m.portRanges)
		return
	}

	key := fmt.Sprintf("%d:%d", event.PID, event.Port)

	switch event.Type {
	case PortOpened:
		m.handlePortOpened(key, event)
	case PortClosed:
		m.handlePortClosed(key, event)
	}
}

// handlePortOpened creates a forward for a newly opened port
func (m *SessionMonitor) handlePortOpened(key string, event PortEvent) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if already forwarded
	if _, exists := m.activeForwards[key]; exists {
		return
	}

	// Remove from pending removals if present
	delete(m.pendingRemovals, key)

	// Request forward from daemon
	req := &protocol.Request{
		ID:   uuid.New().String(),
		Type: protocol.CommandForward,
	}

	payload := protocol.ForwardRequest{
		RemotePort:     event.Port,
		LocalPort:      event.Port,
		Host:           "localhost",
		ConnectionInfo: fmt.Sprintf("session-%s", m.sessionID),
	}

	payloadBytes, _ := json.Marshal(payload)
	req.Payload = payloadBytes

	m.logger.Info("Requesting auto-forward",
		"pid", event.PID,
		"port", event.Port,
		"process", event.ProcessName)

	resp, err := m.daemonClient.SendRequest(req)
	if err != nil {
		m.logger.Error("Failed to request forward",
			"error", err,
			"port", event.Port)
		return
	}

	if !resp.Success {
		m.logger.Error("Forward request failed",
			"error", resp.Error,
			"port", event.Port)
		return
	}

	// Track the forward
	m.activeForwards[key] = ForwardInfo{
		PID:         event.PID,
		Port:        event.Port,
		ProcessName: event.ProcessName,
		RequestID:   req.ID,
		CreatedAt:   time.Now(),
	}

	m.logger.Info("Auto-forward created",
		"pid", event.PID,
		"port", event.Port,
		"process", event.ProcessName)
}

// handlePortClosed marks a forward for removal after grace period
func (m *SessionMonitor) handlePortClosed(key string, event PortEvent) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if we have this forward
	if _, exists := m.activeForwards[key]; !exists {
		return
	}

	// Mark for pending removal
	m.pendingRemovals[key] = time.Now()

	m.logger.Info("Port closed, scheduling forward removal",
		"pid", event.PID,
		"port", event.Port,
		"process", event.ProcessName,
		"gracePeriod", m.gracePeriod)
}

// cleanupLoop periodically removes forwards after grace period
func (m *SessionMonitor) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanupPendingRemovals()
		}
	}
}

// cleanupPendingRemovals removes forwards that have been pending removal
func (m *SessionMonitor) cleanupPendingRemovals() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := time.Now()
	for key, pendingSince := range m.pendingRemovals {
		if now.Sub(pendingSince) >= m.gracePeriod {
			// Time to remove the forward
			if fwd, exists := m.activeForwards[key]; exists {
				m.removeForward(fwd)
				delete(m.activeForwards, key)
			}
			delete(m.pendingRemovals, key)
		}
	}
}

// removeForward removes a port forward
func (m *SessionMonitor) removeForward(fwd ForwardInfo) {
	req := &protocol.Request{
		ID:   uuid.New().String(),
		Type: protocol.CommandUnforward,
	}

	payload := protocol.UnforwardRequest{
		RemotePort:     fwd.Port,
		Host:           "localhost",
		ConnectionInfo: fmt.Sprintf("session-%s", m.sessionID),
	}

	payloadBytes, _ := json.Marshal(payload)
	req.Payload = payloadBytes

	m.logger.Info("Removing auto-forward",
		"pid", fwd.PID,
		"port", fwd.Port,
		"process", fwd.ProcessName)

	resp, err := m.daemonClient.SendRequest(req)
	if err != nil {
		m.logger.Error("Failed to remove forward",
			"error", err,
			"port", fwd.Port)
		return
	}

	if !resp.Success {
		m.logger.Error("Unforward request failed",
			"error", resp.Error,
			"port", fwd.Port)
	}
}

// shouldIgnoreProcess checks if a process should be ignored
func (m *SessionMonitor) shouldIgnoreProcess(name string) bool {
	for _, ignore := range m.ignoreProcesses {
		if strings.Contains(strings.ToLower(name), strings.ToLower(ignore)) {
			return true
		}
	}
	return false
}

// isPortInRange checks if a port is within auto-forward ranges
func (m *SessionMonitor) isPortInRange(port int) bool {
	for _, r := range m.portRanges {
		if port >= r.Start && port <= r.End {
			return true
		}
	}
	return false
}

// cleanup removes all forwards on shutdown
func (m *SessionMonitor) cleanup() error {
	m.logger.Info("Cleaning up session monitor", "session", m.sessionID)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Remove all active forwards
	for _, fwd := range m.activeForwards {
		m.removeForward(fwd)
	}

	m.activeForwards = make(map[string]ForwardInfo)
	m.pendingRemovals = make(map[string]time.Time)

	return nil
}

// GetStatus returns the current status of the monitor
func (m *SessionMonitor) GetStatus() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return map[string]interface{}{
		"sessionID":       m.sessionID,
		"activeForwards":  len(m.activeForwards),
		"pendingRemovals": len(m.pendingRemovals),
		"processes":       len(m.multiMonitor.GetMonitoredProcesses()),
	}
}
