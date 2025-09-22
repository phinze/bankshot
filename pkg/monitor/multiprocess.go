package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/phinze/bankshot/pkg/discovery"
)

// MultiProcessMonitor monitors ports for multiple processes
type MultiProcessMonitor struct {
	monitors    map[int]*Monitor  // PID -> Monitor
	discovery   *discovery.ProcessDiscovery
	logger      *slog.Logger
	mutex       sync.RWMutex
	events      chan PortEvent
	debounceMap map[string]time.Time // For deduplicating events
}

// NewMultiProcessMonitor creates a new multi-process monitor
func NewMultiProcessMonitor(logger *slog.Logger, pollInterval time.Duration) (*MultiProcessMonitor, error) {
	disc, err := discovery.New(logger, pollInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to create process discovery: %w", err)
	}

	return &MultiProcessMonitor{
		monitors:    make(map[int]*Monitor),
		discovery:   disc,
		logger:      logger,
		events:      make(chan PortEvent, 100),
		debounceMap: make(map[string]time.Time),
	}, nil
}

// Start begins monitoring all user processes
func (m *MultiProcessMonitor) Start(ctx context.Context) error {
	// Start process discovery
	go m.discovery.Start(ctx)

	// Poll for changes
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return m.cleanup()
		case <-ticker.C:
			if err := m.updateMonitors(ctx); err != nil {
				m.logger.Error("Failed to update monitors", "error", err)
			}
		}
	}
}

// updateMonitors updates the set of monitored processes
func (m *MultiProcessMonitor) updateMonitors(ctx context.Context) error {
	processes, err := m.discovery.GetUserProcesses()
	if err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Track current PIDs
	currentPIDs := make(map[int]bool)
	for _, proc := range processes {
		currentPIDs[proc.PID] = true

		// Add monitor for new processes
		if _, exists := m.monitors[proc.PID]; !exists {
			m.logger.Debug("Starting monitor for process",
				"pid", proc.PID,
				"name", proc.Name)
			
			monitor := New(proc.PID, m.logger)
			m.monitors[proc.PID] = monitor
			
			// Start monitoring in background
			go m.monitorProcess(ctx, monitor, proc)
		}
	}

	// Remove monitors for terminated processes
	for pid := range m.monitors {
		if !currentPIDs[pid] {
			m.logger.Debug("Stopping monitor for terminated process", "pid", pid)
			// Monitor will stop when its context is cancelled
			delete(m.monitors, pid)
		}
	}

	return nil
}

// monitorProcess monitors a single process for port events
func (m *MultiProcessMonitor) monitorProcess(ctx context.Context, monitor *Monitor, proc *discovery.ProcessInfo) {
	// Start the monitor with a cancellable context
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	
	if err := monitor.Start(monitorCtx); err != nil {
		m.logger.Error("Failed to start monitor", "pid", proc.PID, "error", err)
		return
	}

	eventChan := monitor.Events()

	for event := range eventChan {
		// Add process info to event
		event.ProcessName = proc.Name
		event.ProcessCmd = proc.CommandLine

		// Deduplicate events
		eventKey := fmt.Sprintf("%d:%d:%s", event.PID, event.Port, event.Type)
		
		m.mutex.Lock()
		lastTime, exists := m.debounceMap[eventKey]
		now := time.Now()
		
		// Only emit if this is a new event or enough time has passed
		if !exists || now.Sub(lastTime) > 100*time.Millisecond {
			m.debounceMap[eventKey] = now
			m.mutex.Unlock()
			
			// Forward event
			select {
			case m.events <- event:
				m.logger.Debug("Port event",
					"type", event.Type,
					"pid", event.PID,
					"port", event.Port,
					"process", proc.Name)
			default:
				m.logger.Warn("Event channel full, dropping event")
			}
		} else {
			m.mutex.Unlock()
		}
	}
}

// GetEvents returns the event channel for receiving port events
func (m *MultiProcessMonitor) GetEvents() <-chan PortEvent {
	return m.events
}

// GetMonitoredProcesses returns info about currently monitored processes
func (m *MultiProcessMonitor) GetMonitoredProcesses() []discovery.ProcessInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var processes []discovery.ProcessInfo
	for pid := range m.monitors {
		// Get process info from discovery
		procs, _ := m.discovery.GetUserProcesses()
		for _, proc := range procs {
			if proc.PID == pid {
				processes = append(processes, *proc)
				break
			}
		}
	}

	return processes
}

// cleanup stops all monitors
func (m *MultiProcessMonitor) cleanup() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for pid := range m.monitors {
		m.logger.Debug("Stopping monitor", "pid", pid)
		// Monitors will stop when context is cancelled
	}

	m.monitors = make(map[int]*Monitor)
	close(m.events)

	return nil
}