// Package forwarder makes ports on a remote machine reachable on this one.
// Each implementation is a transport: SSHForwarder rides an existing SSH
// ControlMaster, and the daemon picks one by how a request reached it.
package forwarder

import "time"

// Forwarder establishes and tracks port forwards from remote targets to local
// ports.
type Forwarder interface {
	// AddForwards forwards each port from target. A non-nil error means the
	// target itself was unusable and nothing was attempted; otherwise each
	// Result reports how its port fared.
	AddForwards(target Target, ports []Port) ([]Result, error)
	// RemoveForward tears down one forward from the target named by
	// connectionInfo.
	RemoveForward(connectionInfo string, remotePort int, host string) error
	// ListForwards returns every forward this transport is tracking.
	ListForwards() []*Forward
	// Reconcile repairs or drops forwards whose local side has gone away.
	Reconcile() error
}

// Target names the remote end a set of forwards comes from.
type Target struct {
	// ConnectionInfo identifies the remote machine. For SSH it's the host
	// alias the ControlMaster was opened with.
	ConnectionInfo string
	// SocketPath is the SSH control socket, resolved from ConnectionInfo
	// when empty. Other transports ignore it.
	SocketPath string
}

// Port is one requested forward.
type Port struct {
	RemotePort int
	LocalPort  int    // 0 means the same as RemotePort
	Host       string // remote host to reach; "" means localhost
}

// Result is the outcome of forwarding one Port.
type Result struct {
	Port
	Created bool  // false when the port was already forwarded
	Err     error // non-nil if this port failed
}

// Forward represents an active port forward
type Forward struct {
	RemotePort     int
	LocalPort      int
	Host           string
	SocketPath     string
	ConnectionInfo string // SSH connection target (e.g., hostname)
	CreatedAt      time.Time
}
