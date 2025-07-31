package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// PortEvent represents a port state change
type PortEvent struct {
	Port      Port
	EventType EventType
	Timestamp time.Time
}

// EventType represents the type of port event
type EventType string

const (
	PortOpened EventType = "opened"
	PortClosed EventType = "closed"
)

// Monitor watches for port changes
type Monitor struct {
	pid          int
	pollInterval time.Duration
	debounceTime time.Duration
	events       chan PortEvent
	logger       *slog.Logger
	
	mu          sync.RWMutex
	knownPorts  map[int]Port
	pendingPorts map[int]time.Time // For debouncing
}

// New creates a new port monitor
func New(pid int, logger *slog.Logger) *Monitor {
	return &Monitor{
		pid:          pid,
		pollInterval: 500 * time.Millisecond,
		debounceTime: 100 * time.Millisecond,
		events:       make(chan PortEvent, 10),
		logger:       logger,
		knownPorts:   make(map[int]Port),
		pendingPorts: make(map[int]time.Time),
	}
}

// Start begins monitoring for port changes
func (m *Monitor) Start(ctx context.Context) error {
	// Get initial port state
	initialPorts, err := GetProcessListeningPorts(m.pid)
	if err != nil {
		m.logger.Warn("failed to get initial ports", slog.String("error", err.Error()))
	}
	
	m.mu.Lock()
	for _, port := range initialPorts {
		m.knownPorts[port.Port] = port
		m.logger.Debug("initial port detected", 
			slog.Int("port", port.Port),
			slog.String("protocol", port.Protocol),
		)
	}
	m.mu.Unlock()

	// Start monitoring loop
	go m.monitorLoop(ctx)
	
	// Start debounce processor
	go m.processDebounced(ctx)
	
	return nil
}

// Events returns the channel of port events
func (m *Monitor) Events() <-chan PortEvent {
	return m.events
}

// monitorLoop polls for port changes
func (m *Monitor) monitorLoop(ctx context.Context) {
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
func (m *Monitor) checkPorts() {
	currentPorts, err := GetProcessListeningPorts(m.pid)
	if err != nil {
		m.logger.Debug("failed to get ports", slog.String("error", err.Error()))
		return
	}

	// Create map of current ports for easy lookup
	currentMap := make(map[int]Port)
	for _, port := range currentPorts {
		currentMap[port.Port] = port
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for new ports
	for portNum := range currentMap {
		if _, exists := m.knownPorts[portNum]; !exists {
			// New port detected - add to pending for debouncing
			if _, pending := m.pendingPorts[portNum]; !pending {
				m.pendingPorts[portNum] = time.Now()
				m.logger.Debug("new port detected (pending)", slog.Int("port", portNum))
			}
		}
	}

	// Check for closed ports
	for portNum, knownPort := range m.knownPorts {
		if _, exists := currentMap[portNum]; !exists {
			// Port closed
			delete(m.knownPorts, portNum)
			delete(m.pendingPorts, portNum)
			
			event := PortEvent{
				Port:      knownPort,
				EventType: PortClosed,
				Timestamp: time.Now(),
			}
			
			select {
			case m.events <- event:
				m.logger.Info("port closed", 
					slog.Int("port", portNum),
					slog.String("protocol", knownPort.Protocol),
				)
			default:
				m.logger.Warn("event channel full, dropping closed event")
			}
		}
	}
}

// processDebounced handles debouncing of new port events
func (m *Monitor) processDebounced(ctx context.Context) {
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
func (m *Monitor) processPendingPorts() {
	now := time.Now()
	
	m.mu.Lock()
	defer m.mu.Unlock()

	for portNum, pendingSince := range m.pendingPorts {
		if now.Sub(pendingSince) >= m.debounceTime {
			// Port has been stable - check if it still exists
			currentPorts, err := GetProcessListeningPorts(m.pid)
			if err != nil {
				continue
			}

			for _, port := range currentPorts {
				if port.Port == portNum {
					// Port is confirmed open
					m.knownPorts[portNum] = port
					delete(m.pendingPorts, portNum)
					
					event := PortEvent{
						Port:      port,
						EventType: PortOpened,
						Timestamp: time.Now(),
					}
					
					select {
					case m.events <- event:
						m.logger.Info("port opened", 
							slog.Int("port", portNum),
							slog.String("protocol", port.Protocol),
						)
					default:
						m.logger.Warn("event channel full, dropping opened event")
					}
					break
				}
			}
		}
	}
}