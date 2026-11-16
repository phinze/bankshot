package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
)

// SessionMonitor holds this host's forwards: the ports asked for with
// bankshot forward or bankshot wrap. It declares them to the daemon on every
// sync and drops a forward when the process holding it exits.
type SessionMonitor struct {
	sessionID        string
	daemonClient     DaemonClient
	logger           *slog.Logger
	resolveStartTime func(pid int) string // defaults to ResolveStartTime
	pinned           map[int]pin          // key: remote port
	mutex            sync.RWMutex
}

// pin is a forward someone asked for. It holds whether or not anything
// listens on the port yet.
type pin struct {
	localPort   int
	holder      int    // pid whose exit drops the pin; 0 holds it until unpinned
	holderStart string // holder's start time, so a reused pid doesn't keep it
}

// DaemonClient interface for communicating with the daemon
type DaemonClient interface {
	SendRequest(req *protocol.Request) (*protocol.Response, error)
}

// SessionConfig holds configuration for the session monitor
type SessionConfig struct {
	SessionID    string
	DaemonClient DaemonClient
	Logger       *slog.Logger
}

// NewSessionMonitor creates a new session monitor
func NewSessionMonitor(cfg SessionConfig) *SessionMonitor {
	return &SessionMonitor{
		sessionID:        cfg.SessionID,
		daemonClient:     cfg.DaemonClient,
		logger:           cfg.Logger,
		resolveStartTime: ResolveStartTime,
		pinned:           make(map[int]pin),
	}
}

// Start reaps forwards whose holders have exited until ctx is done, then
// drops every forward.
func (m *SessionMonitor) Start(ctx context.Context) error {
	m.logger.Info("Starting session monitor", "session", m.sessionID)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return m.cleanup()
		case <-ticker.C:
			m.reapPins()
		}
	}
}

// unforward asks the daemon to drop its forward of port.
func (m *SessionMonitor) unforward(port int) error {
	payload, _ := json.Marshal(protocol.UnforwardRequest{
		RemotePort:     port,
		Host:           "localhost",
		ConnectionInfo: m.sessionID,
	})
	return m.send(&protocol.Request{ID: uuid.New().String(), Type: protocol.CommandUnforward, Payload: payload})
}

// send passes req to the daemon and turns a refusal into an error.
func (m *SessionMonitor) send(req *protocol.Request) error {
	resp, err := m.daemonClient.SendRequest(req)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

// Pin forwards port to localPort, whether or not anything listens on it yet.
// A nonzero holder ties the forward to that process, and it's dropped when
// the process exits.
func (m *SessionMonitor) Pin(port, localPort, holder int) error {
	if localPort == 0 {
		localPort = port
	}
	p := pin{localPort: localPort}
	if holder != 0 {
		p.holder = holder
		p.holderStart = m.resolveStartTime(holder)
		if p.holderStart == "" {
			return fmt.Errorf("process %d isn't running", holder)
		}
	}

	m.mutex.Lock()
	prev, had := m.pinned[port]
	if had && prev.holder == 0 && prev.localPort == localPort {
		// Already held until unpinned; a process asking too doesn't
		// shorten that.
		p = prev
	}
	m.pinned[port] = p
	m.mutex.Unlock()

	payload, _ := json.Marshal(protocol.ForwardRequest{
		RemotePort:     port,
		LocalPort:      localPort,
		Host:           "localhost",
		ConnectionInfo: m.sessionID,
	})
	err := m.send(&protocol.Request{ID: uuid.New().String(), Type: protocol.CommandForward, Payload: payload})
	if err != nil {
		m.mutex.Lock()
		if m.pinned[port] == p {
			if had {
				m.pinned[port] = prev
			} else {
				delete(m.pinned, port)
			}
		}
		m.mutex.Unlock()
		return err
	}
	m.logger.Info("Pinned manual forward", "port", port, "local", localPort, "holder", holder)
	return nil
}

// Unpin drops the forward of port.
func (m *SessionMonitor) Unpin(port int) error {
	return m.unpin(port, func(pin) bool { return true })
}

// unpin drops the forward of port if match accepts it.
func (m *SessionMonitor) unpin(port int, match func(pin) bool) error {
	m.mutex.Lock()
	p, pinned := m.pinned[port]
	if pinned && !match(p) {
		m.mutex.Unlock()
		return nil
	}
	delete(m.pinned, port)
	m.mutex.Unlock()

	err := m.unforward(port)
	if err == nil {
		m.logger.Info("Unpinned manual forward", "port", port)
	}
	return err
}

// reapPins drops forwards whose holding process has exited.
func (m *SessionMonitor) reapPins() {
	m.mutex.RLock()
	stale := make(map[int]pin)
	for port, p := range m.pinned {
		if p.holder != 0 && m.resolveStartTime(p.holder) != p.holderStart {
			stale[port] = p
		}
	}
	m.mutex.RUnlock()

	for port, p := range stale {
		m.logger.Info("Dropping manual forward; its process exited", "port", port, "holder", p.holder)
		if err := m.unpin(port, func(cur pin) bool { return cur == p }); err != nil {
			m.logger.Warn("Failed to drop manual forward", "port", port, "error", err)
		}
	}
}

// cleanup drops every forward on shutdown.
func (m *SessionMonitor) cleanup() error {
	m.logger.Info("Cleaning up session monitor", "session", m.sessionID)
	m.mutex.Lock()
	ports := make([]int, 0, len(m.pinned))
	for port := range m.pinned {
		ports = append(ports, port)
	}
	m.pinned = make(map[int]pin)
	m.mutex.Unlock()
	for _, port := range ports {
		_ = m.unforward(port)
	}
	return nil
}

// Forwarding reports whether port is meant to be forwarded. The tailnet relay
// reaches only these ports.
func (m *SessionMonitor) Forwarding(port int) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	_, pinned := m.pinned[port]
	return pinned
}

// Sync declares every forward to the daemon in one sync request, which
// converges the daemon to that set and renews this host's tailnet lease.
// Forwards that failed earlier, say while the daemon was asleep, get another
// try each time. It returns the lease the daemon granted.
func (m *SessionMonitor) Sync() (time.Duration, error) {
	m.mutex.RLock()
	ports := make([]protocol.BatchForwardPort, 0, len(m.pinned))
	for port, p := range m.pinned {
		ports = append(ports, protocol.BatchForwardPort{RemotePort: port, LocalPort: p.localPort, Host: "localhost"})
	}
	m.mutex.RUnlock()
	sort.Slice(ports, func(i, j int) bool { return ports[i].RemotePort < ports[j].RemotePort })
	payload, _ := json.Marshal(protocol.SyncRequest{Forwards: ports})
	req := &protocol.Request{ID: uuid.New().String(), Type: protocol.CommandSync, Payload: payload}

	resp, err := m.daemonClient.SendRequest(req)
	if err != nil {
		return 0, fmt.Errorf("send sync: %w", err)
	}
	if !resp.Success {
		return 0, fmt.Errorf("sync failed: %s", resp.Error)
	}
	var syncResp protocol.SyncResponse
	if err := json.Unmarshal(resp.Data, &syncResp); err != nil {
		return 0, fmt.Errorf("parse sync response: %w", err)
	}
	for _, r := range syncResp.Results {
		if r.Error != "" {
			m.logger.Debug("Port did not forward on sync", "port", r.RemotePort, "error", r.Error)
		}
	}
	m.logger.Debug("Synced forwards with daemon", "ports", len(ports))
	return time.Duration(syncResp.LeaseSeconds) * time.Second, nil
}
