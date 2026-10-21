package forwarder

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/phinze/bankshot/pkg/tailnet"
)

// ErrPortBusy means a forward's local port is already taken on this machine.
var ErrPortBusy = errors.New("port already in use")

// TailnetForwarder forwards ports from tailnet peers. Each forward is a set of
// loopback listeners (IPv4 and IPv6, matching what ssh -L localhost binds) that
// relay every accepted connection to the peer's bankshot monitor, which dials
// the port on its own loopback.
//
// Nothing like an ssh session tells it when a peer goes away, so each peer
// holds a lease that any request from it renews. A peer that stays silent
// past the lease loses its forwards. Leases age by Go's monotonic clock, which
// stops while a Mac sleeps, so waking up doesn't expire everything at once.
type TailnetForwarder struct {
	logger      *slog.Logger
	relayPort   int
	lease       time.Duration
	dialTimeout time.Duration
	bindAddrs   []string
	dial        func(ctx context.Context, network, addr string) (net.Conn, error)

	mu       sync.Mutex
	forwards map[int]*tailnetForward // key: local port, which only one forward can hold
	leases   map[string]time.Time    // key: node; last time the peer was heard from
}

type tailnetForward struct {
	Forward
	relayAddr string
	listeners []net.Listener
}

var _ Forwarder = (*TailnetForwarder)(nil)

// NewTailnet creates a TailnetForwarder that relays to monitors listening on
// relayPort and drops a peer's forwards after lease without hearing from it.
func NewTailnet(logger *slog.Logger, relayPort int, lease time.Duration) *TailnetForwarder {
	var d net.Dialer
	return &TailnetForwarder{
		logger:      logger,
		relayPort:   relayPort,
		lease:       lease,
		dialTimeout: 5 * time.Second,
		bindAddrs:   []string{"127.0.0.1", "::1"},
		dial:        d.DialContext,
		forwards:    make(map[int]*tailnetForward),
		leases:      make(map[string]time.Time),
	}
}

// Lease is how long a peer keeps its forwards without being heard from.
func (f *TailnetForwarder) Lease() time.Duration { return f.lease }

// AddForwards forwards each port from target, whose ConnectionInfo is the
// peer's node name and whose Address is its tailnet IP. It renews the peer's
// lease.
func (f *TailnetForwarder) AddForwards(target Target, ports []Port) ([]Result, error) {
	if target.ConnectionInfo == "" || target.Address == "" {
		return nil, fmt.Errorf("tailnet target needs a node name and address, got %+v", target)
	}
	relayAddr := net.JoinHostPort(target.Address, strconv.Itoa(f.relayPort))

	f.mu.Lock()
	defer f.mu.Unlock()
	f.leases[target.ConnectionInfo] = time.Now()

	results := make([]Result, 0, len(ports))
	for _, p := range ports {
		created, err := f.addLocked(target.ConnectionInfo, relayAddr, &p)
		results = append(results, Result{Port: p, Created: created, Err: err})
	}
	return results, nil
}

// Sync makes target's forwards exactly ports: it adds the missing ones,
// removes the ones no longer listed, and renews the peer's lease.
func (f *TailnetForwarder) Sync(target Target, ports []Port) ([]Result, error) {
	want := make(map[int]bool, len(ports))
	for _, p := range ports {
		want[p.RemotePort] = true
	}
	f.mu.Lock()
	for _, fwd := range f.forwards {
		if fwd.ConnectionInfo == target.ConnectionInfo && !want[fwd.RemotePort] {
			f.logger.Info("Removing tailnet forward no longer synced",
				"node", fwd.ConnectionInfo, "port", fwd.RemotePort)
			f.closeLocked(fwd)
		}
	}
	f.mu.Unlock()
	return f.AddForwards(target, ports)
}

// addLocked establishes one forward, filling in p's defaults. Must be called
// with f.mu held.
func (f *TailnetForwarder) addLocked(node, relayAddr string, p *Port) (bool, error) {
	if p.Host == "" {
		p.Host = "localhost"
	}
	if p.LocalPort == 0 {
		p.LocalPort = p.RemotePort
	}
	if p.Host != "localhost" && p.Host != "127.0.0.1" {
		return false, fmt.Errorf("tailnet forwards reach only the remote's loopback, not %s", p.Host)
	}

	if existing, ok := f.forwards[p.LocalPort]; ok {
		if existing.ConnectionInfo == node && existing.RemotePort == p.RemotePort {
			existing.relayAddr = relayAddr
			return false, nil
		}
		return false, fmt.Errorf("local port %d: %w (forwarded from %s:%d)",
			p.LocalPort, ErrPortBusy, existing.ConnectionInfo, existing.RemotePort)
	}

	listeners, err := f.listen(p.LocalPort)
	if err != nil {
		return false, err
	}
	fwd := &tailnetForward{
		Forward: Forward{
			RemotePort:     p.RemotePort,
			LocalPort:      p.LocalPort,
			Host:           p.Host,
			ConnectionInfo: node,
			CreatedAt:      time.Now(),
		},
		relayAddr: relayAddr,
		listeners: listeners,
	}
	f.forwards[p.LocalPort] = fwd
	for _, ln := range listeners {
		go f.serve(fwd, ln)
	}
	f.logger.Info("Tailnet forward established",
		"node", node, "remote", p.RemotePort, "local", p.LocalPort)
	return true, nil
}

// listen binds port on every loopback address. IPv4 must succeed; IPv6 may be
// missing entirely, but if something else holds it, a browser resolving
// localhost to ::1 would reach that instead of us, so that counts as busy too.
func (f *TailnetForwarder) listen(port int) ([]net.Listener, error) {
	var listeners []net.Listener
	closeAll := func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
	}
	for i, addr := range f.bindAddrs {
		ln, err := net.Listen("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err == nil {
			listeners = append(listeners, ln)
			continue
		}
		if errors.Is(err, syscall.EADDRINUSE) {
			closeAll()
			if holder := portHolder(port); holder != "" {
				return nil, fmt.Errorf("local port %d: %w (held by %s)", port, ErrPortBusy, holder)
			}
			return nil, fmt.Errorf("local port %d: %w", port, ErrPortBusy)
		}
		if i == 0 {
			closeAll()
			return nil, fmt.Errorf("listen on %s port %d: %w", addr, port, err)
		}
		f.logger.Debug("Skipping loopback address", "addr", addr, "port", port, "error", err)
	}
	return listeners, nil
}

// portHolder names the process listening on port, best effort.
func portHolder(port int) string {
	out, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-Fc").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if name, ok := strings.CutPrefix(line, "c"); ok {
			return name
		}
	}
	return ""
}

func (f *TailnetForwarder) serve(fwd *tailnetForward, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // closed by closeLocked
		}
		go f.relay(fwd, conn)
	}
}

// relay carries one local connection to the peer's monitor relay.
func (f *TailnetForwarder) relay(fwd *tailnetForward, local net.Conn) {
	defer local.Close()

	f.mu.Lock()
	relayAddr := fwd.relayAddr
	f.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), f.dialTimeout)
	defer cancel()
	remote, err := f.dial(ctx, "tcp", relayAddr)
	if err != nil {
		f.logger.Warn("Tailnet relay unreachable",
			"node", fwd.ConnectionInfo, "relay", relayAddr, "port", fwd.RemotePort, "error", err)
		return
	}
	defer remote.Close()

	reader, err := relayHandshake(remote, fwd.RemotePort, f.dialTimeout)
	if err != nil {
		var refused *relayRefusedError
		if errors.As(err, &refused) && refused.notForwarded {
			f.logger.Info("Relay no longer forwards port, dropping forward",
				"node", fwd.ConnectionInfo, "port", fwd.RemotePort)
			f.drop(fwd)
			return
		}
		f.logger.Warn("Tailnet relay failed",
			"node", fwd.ConnectionInfo, "port", fwd.RemotePort, "error", err)
		return
	}

	tailnet.Splice(local, remote, reader)
}

type relayRefusedError struct {
	msg          string
	notForwarded bool
}

func (e *relayRefusedError) Error() string { return "relay refused: " + e.msg }

// relayHandshake asks the relay for port and waits for its answer. It returns
// the reader to keep using, since it may have buffered forwarded bytes.
func relayHandshake(conn net.Conn, port int, timeout time.Duration) (*bufio.Reader, error) {
	_ = conn.SetDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	req, _ := json.Marshal(protocol.RelayRequest{Port: port})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return nil, fmt.Errorf("send relay request: %w", err)
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read relay response: %w", err)
	}
	var resp protocol.RelayResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("parse relay response: %w", err)
	}
	if !resp.OK {
		return nil, &relayRefusedError{msg: resp.Error, notForwarded: resp.NotForwarded}
	}
	return reader, nil
}

// drop removes fwd if it's still the forward on its port.
func (f *TailnetForwarder) drop(fwd *tailnetForward) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forwards[fwd.LocalPort] == fwd {
		f.closeLocked(fwd)
	}
}

// closeLocked closes fwd's listeners and forgets it. Connections already
// relaying finish on their own. Must be called with f.mu held.
func (f *TailnetForwarder) closeLocked(fwd *tailnetForward) {
	for _, ln := range fwd.listeners {
		_ = ln.Close()
	}
	delete(f.forwards, fwd.LocalPort)
}

// RemoveForward tears down the forward of remotePort from node.
func (f *TailnetForwarder) RemoveForward(node string, remotePort int, host string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leases[node] = time.Now()
	for _, fwd := range f.forwards {
		if fwd.ConnectionInfo == node && fwd.RemotePort == remotePort {
			f.closeLocked(fwd)
			f.logger.Info("Tailnet forward removed", "node", node, "port", remotePort)
			return nil
		}
	}
	return fmt.Errorf("forward not found: %s:%d", node, remotePort)
}

// ListForwards returns every tailnet forward.
func (f *TailnetForwarder) ListForwards() []*Forward {
	f.mu.Lock()
	defer f.mu.Unlock()
	forwards := make([]*Forward, 0, len(f.forwards))
	for _, fwd := range f.forwards {
		c := fwd.Forward
		forwards = append(forwards, &c)
	}
	return forwards
}

// Reconcile expires peers whose lease has run out. The listeners live in this
// process, so there's nothing else to repair.
func (f *TailnetForwarder) Reconcile() error {
	f.expire()
	return nil
}

// Run expires lapsed leases until ctx is done, then closes every forward.
func (f *TailnetForwarder) Run(ctx context.Context) {
	ticker := time.NewTicker(min(f.lease/4, 30*time.Second))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			f.mu.Lock()
			for _, fwd := range f.forwards {
				f.closeLocked(fwd)
			}
			f.mu.Unlock()
			return
		case <-ticker.C:
			f.expire()
		}
	}
}

func (f *TailnetForwarder) expire() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for node, at := range f.leases {
		if time.Since(at) < f.lease {
			continue
		}
		var dropped int
		for _, fwd := range f.forwards {
			if fwd.ConnectionInfo == node {
				f.closeLocked(fwd)
				dropped++
			}
		}
		delete(f.leases, node)
		f.logger.Info("Tailnet lease expired", "node", node, "silentFor", time.Since(at).Round(time.Second), "dropped", dropped)
	}
}
