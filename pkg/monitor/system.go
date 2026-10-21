package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SystemMonitor monitors all listening ports on the system
type SystemMonitor struct {
	pollInterval time.Duration
	debounceTime time.Duration
	logger       *slog.Logger
	events       chan PortEvent

	mu           sync.RWMutex
	knownPorts   map[string]Port // key: "port:protocol"
	pendingPorts map[string]time.Time
}

// NewSystemMonitor creates a new system-wide port monitor
func NewSystemMonitor(logger *slog.Logger, pollInterval time.Duration) *SystemMonitor {
	return &SystemMonitor{
		pollInterval: pollInterval,
		debounceTime: 100 * time.Millisecond,
		logger:       logger,
		events:       make(chan PortEvent, 50),
		knownPorts:   make(map[string]Port),
		pendingPorts: make(map[string]time.Time),
	}
}

// Start begins monitoring system-wide ports
func (m *SystemMonitor) Start(ctx context.Context) error {
	// Get initial port state
	initialPorts, err := GetListeningPorts()
	if err != nil {
		m.logger.Warn("failed to get initial ports", "error", err)
	}

	m.mu.Lock()
	for _, port := range initialPorts {
		key := fmt.Sprintf("%d:%s", port.Port, port.Protocol)
		m.knownPorts[key] = port
		m.logger.Debug("initial port detected",
			"port", port.Port,
			"protocol", port.Protocol)
	}
	m.mu.Unlock()

	// Start monitoring loop
	go m.monitorLoop(ctx)

	// Start debounce processor
	go m.processDebounced(ctx)

	return nil
}

// Events returns the channel of port events
func (m *SystemMonitor) Events() <-chan PortEvent {
	return m.events
}

// monitorLoop polls for port changes
func (m *SystemMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(m.events)
			return
		case <-ticker.C:
			m.checkPorts()
		}
	}
}

// checkPorts scans for port changes
func (m *SystemMonitor) checkPorts() {
	currentPorts, err := GetListeningPorts()
	if err != nil {
		m.logger.Debug("failed to get ports", "error", err)
		return
	}

	// Create map of current ports for easy lookup
	currentMap := make(map[string]Port)
	for _, port := range currentPorts {
		key := fmt.Sprintf("%d:%s", port.Port, port.Protocol)
		currentMap[key] = port
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for new ports
	for key, port := range currentMap {
		if _, exists := m.knownPorts[key]; !exists {
			// New port detected - add to pending for debouncing
			if _, pending := m.pendingPorts[key]; !pending {
				m.pendingPorts[key] = time.Now()
				m.logger.Debug("new port detected (pending)", "port", port.Port, "protocol", port.Protocol)
			}
		}
	}

	// Check for closed ports
	for key, knownPort := range m.knownPorts {
		if _, exists := currentMap[key]; !exists {
			// Port closed
			delete(m.knownPorts, key)
			delete(m.pendingPorts, key)

			// Try to find which PID owns this port (best effort)
			pid := m.findPortOwner(knownPort)

			event := PortEvent{
				Type:      PortClosed,
				PID:       pid,
				Port:      knownPort.Port,
				Protocol:  knownPort.Protocol,
				BindAddr:  knownPort.BindAddr,
				Timestamp: time.Now(),
			}

			select {
			case m.events <- event:
				m.logger.Info("port closed",
					"port", knownPort.Port,
					"protocol", knownPort.Protocol)
			default:
				m.logger.Warn("event channel full, dropping closed event")
			}
		}
	}
}

// processDebounced handles debouncing of new port events
func (m *SystemMonitor) processDebounced(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.processPendingPorts()
		}
	}
}

// processPendingPorts checks if pending ports have been stable long enough
func (m *SystemMonitor) processPendingPorts() {
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	for key, pendingSince := range m.pendingPorts {
		if now.Sub(pendingSince) >= m.debounceTime {
			// Port has been stable - check if it still exists
			currentPorts, err := GetListeningPorts()
			if err != nil {
				continue
			}

			// Find this port in current state
			for _, port := range currentPorts {
				portKey := fmt.Sprintf("%d:%s", port.Port, port.Protocol)
				if portKey == key {
					// Port is confirmed open
					m.knownPorts[key] = port
					delete(m.pendingPorts, key)

					// Try to find which PID owns this port (best effort)
					pid := m.findPortOwner(port)

					event := PortEvent{
						Type:      PortOpened,
						PID:       pid,
						Port:      port.Port,
						Protocol:  port.Protocol,
						BindAddr:  port.BindAddr,
						Timestamp: time.Now(),
					}

					select {
					case m.events <- event:
						m.logger.Info("port opened",
							"port", port.Port,
							"protocol", port.Protocol,
							"pid", pid)
					default:
						m.logger.Warn("event channel full, dropping opened event")
					}
					break
				}
			}
		}
	}
}

// findPortOwner returns the process that owns a port, or 0 if it couldn't be
// resolved. GetListeningPorts resolves owners on Linux.
func (m *SystemMonitor) findPortOwner(port Port) int {
	return port.PID
}
