package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/phinze/bankshot/pkg/protocol"
)

// processMatcher matches a process name by either substring or regexp.
// Entries wrapped in /slashes/ are compiled as regexps; others use
// case-insensitive substring matching.
type processMatcher struct {
	pattern string         // original pattern string (for logging)
	re      *regexp.Regexp // non-nil for /regex/ patterns
	substr  string         // lowercased substring for plain patterns
}

func (pm processMatcher) matches(name string) bool {
	if pm.re != nil {
		return pm.re.MatchString(name)
	}
	return strings.Contains(strings.ToLower(name), pm.substr)
}

// SessionMonitor manages port forwarding for an SSH session
type SessionMonitor struct {
	sessionID          string
	systemMonitor      PortEventSource
	daemonClient       DaemonClient
	logger             *slog.Logger
	portRanges         []PortRange
	ignorePorts        map[int]bool
	ignoreProcesses    []string             // raw config (for logging)
	processMatchers    []processMatcher     // compiled matchers
	resolveProcessName func(pid int) string // defaults to ResolveProcessName
	resolveProcessCwd  func(pid int) string // defaults to ResolveProcessCwd
	resolveParentPID   func(pid int) int    // defaults to ResolveParentPID
	resolveStartTime   func(pid int) string // defaults to ResolveStartTime
	gracePeriod        time.Duration
	activeForwards     map[string]ForwardInfo // key: "port" (PID not needed)
	wanted             map[string]PortEvent   // key: "port"; every open port that passed the filters, forwarded or not
	pinned             map[int]pin            // key: remote port; manual forwards
	pendingRemovals    map[string]time.Time   // forwards pending removal
	batchWindow        time.Duration          // coalesce newly-opened ports for this long before forwarding
	pendingBatch       map[int]PortEvent      // ports awaiting the next batch flush, keyed by port
	batchTimer         *time.Timer            // fires batchWindow after the first port in the current batch
	mutex              sync.RWMutex
}

// pin is a manual forward, asked for with bankshot forward or wrap. It skips
// the port filters and holds whether or not anything listens on the port yet.
type pin struct {
	localPort   int
	holder      int    // pid whose exit drops the pin; 0 holds it until unpinned
	holderStart string // holder's start time, so a reused pid doesn't keep it
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
	IgnorePorts     []int
	IgnoreProcesses []string
	GracePeriod     time.Duration
	BatchWindow     time.Duration // coalesce a burst of newly-opened ports into one forward request
	Logger          *slog.Logger
	PortEventSource PortEventSource
}

// NewSessionMonitor creates a new session monitor
func NewSessionMonitor(cfg SessionConfig) (*SessionMonitor, error) {
	ignoreMap := make(map[int]bool, len(cfg.IgnorePorts))
	for _, p := range cfg.IgnorePorts {
		ignoreMap[p] = true
	}

	// Compile process matchers: /pattern/ entries become regexps,
	// plain strings use case-insensitive substring matching.
	matchers := make([]processMatcher, 0, len(cfg.IgnoreProcesses))
	for _, p := range cfg.IgnoreProcesses {
		if strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/") && len(p) > 2 {
			expr := p[1 : len(p)-1]
			re, err := regexp.Compile("(?i)" + expr)
			if err != nil {
				cfg.Logger.Warn("Invalid ignore process regexp, falling back to substring",
					"pattern", p, "error", err)
				matchers = append(matchers, processMatcher{pattern: p, substr: strings.ToLower(expr)})
			} else {
				matchers = append(matchers, processMatcher{pattern: p, re: re})
			}
		} else {
			matchers = append(matchers, processMatcher{pattern: p, substr: strings.ToLower(p)})
		}
	}

	return &SessionMonitor{
		sessionID:          cfg.SessionID,
		systemMonitor:      cfg.PortEventSource,
		daemonClient:       cfg.DaemonClient,
		logger:             cfg.Logger,
		portRanges:         cfg.PortRanges,
		ignorePorts:        ignoreMap,
		ignoreProcesses:    cfg.IgnoreProcesses,
		processMatchers:    matchers,
		resolveProcessName: ResolveProcessName,
		resolveProcessCwd:  ResolveProcessCwd,
		resolveParentPID:   ResolveParentPID,
		resolveStartTime:   ResolveStartTime,
		gracePeriod:        cfg.GracePeriod,
		batchWindow:        cfg.BatchWindow,
		activeForwards:     make(map[string]ForwardInfo),
		wanted:             make(map[string]PortEvent),
		pinned:             make(map[int]pin),
		pendingRemovals:    make(map[string]time.Time),
		pendingBatch:       make(map[int]PortEvent),
	}, nil
}

// Start begins monitoring and auto-forwarding
func (m *SessionMonitor) Start(ctx context.Context) error {
	m.logger.Info("Starting session monitor",
		"session", m.sessionID,
		"portRanges", m.portRanges,
		"ignoreProcesses", m.ignoreProcesses)

	// Start system-wide port monitoring
	if err := m.systemMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start system monitor: %w", err)
	}

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
	events := m.systemMonitor.Events()

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
	// Check if port should be auto-forwarded
	if !m.shouldForwardPort(event.Port, event.BindAddr) {
		m.logger.Debug("Port excluded from auto-forwarding",
			"port", event.Port,
			"bindAddr", event.BindAddr)
		return
	}

	// Resolve process info when we have a PID
	if event.PID != 0 {
		if event.ProcessName == "" {
			event.ProcessName = m.resolveProcessName(event.PID)
		}
		if event.ProcessCwd == "" {
			event.ProcessCwd = m.resolveProcessCwd(event.PID)
		}

		// Check if the process or any ancestor should be ignored
		if len(m.processMatchers) > 0 {
			if ignored, matchedName := m.shouldIgnoreProcess(event.PID, event.ProcessName); ignored {
				m.logger.Info("Ignoring port event from excluded process",
					"port", event.Port,
					"pid", event.PID,
					"process", event.ProcessName,
					"matchedAncestor", matchedName)
				return
			}
		}
	}

	// Use port as key (we don't track by PID anymore since we monitor system-wide)
	key := fmt.Sprintf("%d", event.Port)

	switch event.Type {
	case PortOpened:
		m.handlePortOpened(key, event)
	case PortClosed:
		m.handlePortClosed(key, event)
	}
}

// handlePortOpened queues a newly opened port for the next batch forward.
func (m *SessionMonitor) handlePortOpened(key string, event PortEvent) {
	m.mutex.Lock()
	m.wanted[key] = event

	// Check if this port had a pending removal (server restart case)
	if _, wasPending := m.pendingRemovals[key]; wasPending {
		delete(m.pendingRemovals, key)
		m.logger.Info("Canceled pending removal for reopened port",
			"port", event.Port,
			"protocol", event.Protocol)
		// Fall through to re-enqueue: forwarding is idempotent, so this
		// re-establishes the forward if the daemon lost its state.
	} else if _, exists := m.activeForwards[key]; exists {
		m.mutex.Unlock()
		return
	}
	if _, pinned := m.pinned[event.Port]; pinned {
		// The manual forward already carries this port.
		m.mutex.Unlock()
		return
	}

	m.enqueueForwardLocked(event)
	immediate := m.batchWindow <= 0
	m.mutex.Unlock()

	// With no batch window configured, forward immediately (preserves the
	// original one-request-per-port behavior; used by tests).
	if immediate {
		m.flushBatch()
	}
}

// enqueueForwardLocked adds a port to the pending batch and arms the flush
// timer if it isn't already running. Must be called with m.mutex held.
func (m *SessionMonitor) enqueueForwardLocked(event PortEvent) {
	m.pendingBatch[event.Port] = event
	if m.batchWindow > 0 && m.batchTimer == nil {
		m.batchTimer = time.AfterFunc(m.batchWindow, m.flushBatch)
	}
}

// flushBatch sends every pending port to the daemon as one batched forward
// request. A burst of ports opened on connection thus becomes a single request
// and a single (rolled-up) notification.
func (m *SessionMonitor) flushBatch() {
	m.mutex.Lock()
	if m.batchTimer != nil {
		m.batchTimer.Stop()
		m.batchTimer = nil
	}
	if len(m.pendingBatch) == 0 {
		m.mutex.Unlock()
		return
	}
	events := make([]PortEvent, 0, len(m.pendingBatch))
	for _, e := range m.pendingBatch {
		events = append(events, e)
	}
	m.pendingBatch = make(map[int]PortEvent)
	m.mutex.Unlock()

	sort.Slice(events, func(i, j int) bool { return events[i].Port < events[j].Port })
	m.sendBatchForward(events)
}

// sendBatchForward forwards a set of ports in a single request and records the
// ones that succeeded as active forwards. This is idempotent - the daemon
// returns success for forwards that already exist.
func (m *SessionMonitor) sendBatchForward(events []PortEvent) {
	ports := make([]protocol.BatchForwardPort, 0, len(events))
	for _, e := range events {
		ports = append(ports, protocol.BatchForwardPort{
			RemotePort:  e.Port,
			LocalPort:   e.Port,
			Host:        "localhost",
			ProcessName: e.ProcessName,
			ProcessCwd:  e.ProcessCwd,
		})
	}

	req := &protocol.Request{
		ID:   uuid.New().String(),
		Type: protocol.CommandForwardBatch,
	}
	payload := protocol.BatchForwardRequest{
		ConnectionInfo: m.sessionID, // sessionID is the hostname for SSH connection matching
		Forwards:       ports,
	}
	payloadBytes, _ := json.Marshal(payload)
	req.Payload = payloadBytes

	m.logger.Info("Requesting batched auto-forward", "ports", len(ports))

	resp, err := m.daemonClient.SendRequest(req)
	if err != nil {
		m.logger.Error("Failed to request batch forward", "error", err, "ports", len(ports))
		return
	}
	if !resp.Success {
		m.logger.Error("Batch forward request failed", "error", resp.Error, "ports", len(ports))
		return
	}

	// Parse per-port results so we only track forwards that actually succeeded.
	var batchResp protocol.BatchForwardResponse
	if len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &batchResp); err != nil {
			m.logger.Warn("Failed to parse batch forward response, assuming all succeeded", "error", err)
		}
	}

	byPort := make(map[int]PortEvent, len(events))
	for _, e := range events {
		byPort[e.Port] = e
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	record := func(e PortEvent) {
		m.activeForwards[fmt.Sprintf("%d", e.Port)] = ForwardInfo{
			PID:         e.PID,
			Port:        e.Port,
			ProcessName: e.ProcessName,
			RequestID:   req.ID,
			CreatedAt:   time.Now(),
		}
	}

	if len(batchResp.Results) > 0 {
		for _, r := range batchResp.Results {
			if r.Error != "" {
				m.logger.Warn("Port failed to forward in batch", "port", r.RemotePort, "error", r.Error)
				continue
			}
			if e, ok := byPort[r.RemotePort]; ok {
				record(e)
			}
		}
	} else {
		// No structured results - track everything we sent.
		for _, e := range events {
			record(e)
		}
	}

	m.logger.Info("Batch auto-forward complete", "ports", len(ports))
}

// handlePortClosed marks a forward for removal after grace period
func (m *SessionMonitor) handlePortClosed(key string, event PortEvent) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if we have this forward, or wanted it and failed
	_, active := m.activeForwards[key]
	_, wanted := m.wanted[key]
	if !active && !wanted {
		return
	}

	// Verify the port is actually closed — another listener may have already
	// replaced it (hot-reload race: PortOpened(new) then PortClosed(old))
	ports, err := GetListeningPorts()
	if err == nil {
		for _, p := range ports {
			if p.Port == event.Port {
				m.logger.Debug("Ignoring stale PortClosed, port still listening",
					"port", event.Port,
					"protocol", event.Protocol)
				return
			}
		}
	}

	// Mark for pending removal
	m.pendingRemovals[key] = time.Now()

	m.logger.Info("Port closed, scheduling forward removal",
		"port", event.Port,
		"protocol", event.Protocol,
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
			m.reapPins()
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
				if _, pinned := m.pinned[fwd.Port]; !pinned {
					m.removeForward(fwd)
				}
				delete(m.activeForwards, key)
			}
			delete(m.wanted, key)
			delete(m.pendingRemovals, key)
		}
	}
}

// removeForward removes a port forward
func (m *SessionMonitor) removeForward(fwd ForwardInfo) {
	m.logger.Info("Removing auto-forward",
		"port", fwd.Port)

	if err := m.unforward(fwd.Port); err != nil {
		m.logger.Error("Failed to remove forward",
			"error", err,
			"port", fwd.Port)
	}
}

// unforward asks the daemon to drop its forward of port.
func (m *SessionMonitor) unforward(port int) error {
	payload, _ := json.Marshal(protocol.UnforwardRequest{
		RemotePort:     port,
		Host:           "localhost",
		ConnectionInfo: m.sessionID, // sessionID is now the hostname for SSH connection matching
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

// Pin forwards port to localPort on the user's say-so, past the port filters
// and whether or not anything listens on it yet. A nonzero holder ties the
// forward to that process, and it's dropped when the process exits.
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
	key := fmt.Sprintf("%d", port)
	auto, moved := m.activeForwards[key]
	moved = moved && localPort != port
	if moved {
		delete(m.activeForwards, key)
	}
	m.mutex.Unlock()

	if moved {
		// The pin puts an auto-forwarded port on another local port;
		// sync wouldn't drop the old one, since the port is still declared.
		m.removeForward(auto)
	}
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

// Unpin drops the manual forward of port. A port the monitor forwards on its
// own stays forwarded; ignorePorts is the way to stop one of those.
func (m *SessionMonitor) Unpin(port int) error {
	return m.unpin(port, func(pin) bool { return true })
}

// unpin drops the manual forward of port if match accepts it.
func (m *SessionMonitor) unpin(port int, match func(pin) bool) error {
	key := fmt.Sprintf("%d", port)
	m.mutex.Lock()
	p, pinned := m.pinned[port]
	event, wanted := m.wanted[key]
	if pinned && !match(p) {
		m.mutex.Unlock()
		return nil
	}
	if !pinned && wanted {
		m.mutex.Unlock()
		return fmt.Errorf("port %d is forwarded automatically; add it to monitor.ignorePorts to stop that", port)
	}
	delete(m.pinned, port)
	if wanted && p.localPort == port {
		// The auto-forward is the same forward, so the daemon keeps it,
		// now owned as an auto-forward and removed when the port closes.
		if _, active := m.activeForwards[key]; !active {
			m.activeForwards[key] = ForwardInfo{
				PID:         event.PID,
				Port:        port,
				ProcessName: event.ProcessName,
				CreatedAt:   time.Now(),
			}
		}
		m.mutex.Unlock()
		m.logger.Info("Unpinned manual forward; still forwarded automatically", "port", port)
		return nil
	}
	m.mutex.Unlock()

	err := m.unforward(port)
	if wanted {
		// The pin had moved this port to another local port; put the
		// auto-forward back on its own.
		m.mutex.Lock()
		delete(m.activeForwards, key)
		m.enqueueForwardLocked(event)
		immediate := m.batchWindow <= 0
		m.mutex.Unlock()
		if immediate {
			m.flushBatch()
		}
	}
	if err == nil {
		m.logger.Info("Unpinned manual forward", "port", port)
	}
	return err
}

// reapPins drops manual forwards whose holding process has exited.
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

// ShouldForwardPort determines whether a port should be auto-forwarded.
// Ports bound to non-local addresses (e.g. Tailscale, LAN IPs) are skipped.
// When portRanges is non-empty, the port must fall within one of the ranges.
// When portRanges is empty/nil, all non-privileged ports (>= 1024) are forwarded.
// Ports in ignorePorts are never forwarded regardless of other settings.
func ShouldForwardPort(port int, bindAddr string, portRanges []PortRange, ignorePorts map[int]bool) bool {
	if !IsLocalAddr(bindAddr) {
		return false
	}
	if ignorePorts[port] {
		return false
	}
	if len(portRanges) > 0 {
		for _, r := range portRanges {
			if port >= r.Start && port <= r.End {
				return true
			}
		}
		return false
	}
	return port >= 1024
}

// shouldForwardPort checks if a port should be auto-forwarded using this monitor's config
func (m *SessionMonitor) shouldForwardPort(port int, bindAddr string) bool {
	return ShouldForwardPort(port, bindAddr, m.portRanges, m.ignorePorts)
}

// IgnoresProcess reports whether pid or one of its ancestors matches
// ignoreProcesses, and the name that matched. An unresolved owner (pid 0)
// is never ignored.
func (m *SessionMonitor) IgnoresProcess(pid int) (bool, string) {
	if pid == 0 || len(m.processMatchers) == 0 {
		return false, ""
	}
	return m.shouldIgnoreProcess(pid, m.resolveProcessName(pid))
}

// shouldIgnoreProcess checks if the process or any of its ancestors match an
// ignoreProcesses entry. It first checks the given name, then walks the process
// tree upward via resolveParentPID, resolving each ancestor's name and checking
// against the matchers. Stops at PID <= 1 or after 16 levels.
func (m *SessionMonitor) shouldIgnoreProcess(pid int, name string) (bool, string) {
	// Check the process itself first
	for _, pm := range m.processMatchers {
		if pm.matches(name) {
			return true, name
		}
	}

	// Walk up the process tree
	currentPID := pid
	for depth := 0; depth < 16; depth++ {
		parentPID := m.resolveParentPID(currentPID)
		if parentPID <= 1 {
			break
		}
		parentName := m.resolveProcessName(parentPID)
		if parentName == "" {
			break
		}
		for _, pm := range m.processMatchers {
			if pm.matches(parentName) {
				return true, parentName
			}
		}
		currentPID = parentPID
	}

	return false, ""
}

// cleanup removes all forwards on shutdown
func (m *SessionMonitor) cleanup() error {
	m.logger.Info("Cleaning up session monitor", "session", m.sessionID)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Cancel any pending batch so it can't fire after shutdown.
	if m.batchTimer != nil {
		m.batchTimer.Stop()
		m.batchTimer = nil
	}
	m.pendingBatch = make(map[int]PortEvent)

	// Remove all active forwards
	for _, fwd := range m.activeForwards {
		m.removeForward(fwd)
	}
	for port := range m.pinned {
		if _, active := m.activeForwards[fmt.Sprintf("%d", port)]; !active {
			_ = m.unforward(port)
		}
	}

	m.activeForwards = make(map[string]ForwardInfo)
	m.wanted = make(map[string]PortEvent)
	m.pinned = make(map[int]pin)
	m.pendingRemovals = make(map[string]time.Time)

	return nil
}

// Seed treats every port already listening as just opened. Port sources
// take the listeners present at start as a silent baseline, so without this
// a monitor started after its servers would never forward them. Each goes
// through the same filters as a live event, ignoreProcesses included, where
// the owning process can be resolved.
func (m *SessionMonitor) Seed() error {
	ports, err := GetListeningPorts()
	if err != nil {
		return fmt.Errorf("list listening ports: %w", err)
	}
	for _, p := range ports {
		m.handlePortEvent(PortEvent{
			Type:      PortOpened,
			PID:       p.PID,
			Port:      p.Port,
			Protocol:  p.Protocol,
			BindAddr:  p.BindAddr,
			Timestamp: time.Now(),
		})
	}
	return nil
}

// Forwarding reports whether port is meant to be forwarded: open and
// passing the filters, or pinned by hand. The tailnet relay reaches only
// these ports.
func (m *SessionMonitor) Forwarding(port int) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	_, wanted := m.wanted[fmt.Sprintf("%d", port)]
	_, pinned := m.pinned[port]
	return wanted || pinned
}

// Sync declares every wanted and pinned port to the daemon in one sync request, which
// converges the daemon to that set and renews this host's tailnet lease.
// Ports that failed to forward earlier, say while the daemon was asleep, get
// another try each time. It returns the lease the daemon granted.
func (m *SessionMonitor) Sync() (time.Duration, error) {
	m.mutex.RLock()
	ports := make([]protocol.BatchForwardPort, 0, len(m.wanted)+len(m.pinned))
	for _, e := range m.wanted {
		if _, pinned := m.pinned[e.Port]; pinned {
			continue
		}
		ports = append(ports, protocol.BatchForwardPort{
			RemotePort:  e.Port,
			LocalPort:   e.Port,
			Host:        "localhost",
			ProcessName: e.ProcessName,
			ProcessCwd:  e.ProcessCwd,
		})
	}
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

	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, r := range syncResp.Results {
		key := fmt.Sprintf("%d", r.RemotePort)
		e, stillWanted := m.wanted[key]
		if r.Error != "" || !stillWanted {
			if r.Error != "" {
				m.logger.Debug("Port did not forward on sync", "port", r.RemotePort, "error", r.Error)
			}
			continue
		}
		if _, ok := m.activeForwards[key]; !ok {
			m.activeForwards[key] = ForwardInfo{
				PID:         e.PID,
				Port:        e.Port,
				ProcessName: e.ProcessName,
				RequestID:   req.ID,
				CreatedAt:   time.Now(),
			}
		}
	}
	m.logger.Debug("Synced forwards with daemon", "ports", len(ports))
	return time.Duration(syncResp.LeaseSeconds) * time.Second, nil
}

// GetStatus returns the current status of the monitor
func (m *SessionMonitor) GetStatus() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return map[string]interface{}{
		"sessionID":       m.sessionID,
		"activeForwards":  len(m.activeForwards),
		"pinnedForwards":  len(m.pinned),
		"pendingRemovals": len(m.pendingRemovals),
	}
}
