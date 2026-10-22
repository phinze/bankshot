package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CommandType represents the type of command
type CommandType string

const (
	// CommandOpen opens a URL in the browser
	CommandOpen CommandType = "open"
	// CommandForward requests a port forward
	CommandForward CommandType = "forward"
	// CommandForwardBatch requests multiple port forwards in a single call.
	// The monitor coalesces a burst of newly-opened ports (e.g. everything
	// already listening when an SSH connection comes up) into one batch so the
	// daemon can set them all up and post a single rolled-up notification.
	CommandForwardBatch CommandType = "forward-batch"
	// CommandUnforward removes a port forward
	CommandUnforward CommandType = "unforward"
	// CommandStatus gets daemon status
	CommandStatus CommandType = "status"
	// CommandList lists active forwards
	CommandList CommandType = "list"
	// CommandReconcile triggers immediate forward reconciliation
	CommandReconcile CommandType = "reconcile"
	// CommandOpProxy proxies 1Password CLI requests to the local machine
	CommandOpProxy CommandType = "op-proxy"
	// CommandAgent turns the connection into an ssh-agent stream to the
	// daemon's local agent, after a response. Tailnet peers only.
	CommandAgent CommandType = "agent"
	// CommandSync declares the full set of ports a monitor wants forwarded
	// over the tailnet. The daemon converges to it, and the request renews
	// the monitor's lease: a host that stops syncing loses its forwards.
	CommandSync CommandType = "sync"
)

// Request represents a command request from client to daemon
type Request struct {
	ID      string          `json:"id"`      // Unique request ID
	Type    CommandType     `json:"type"`    // Command type
	Payload json.RawMessage `json:"payload"` // Command-specific payload
	// Origin describes who on the remote host made the request. A monitor
	// fills it in when it passes a local caller's request over the tailnet.
	Origin *Origin `json:"origin,omitempty"`
}

// Origin attributes a request to a process on the remote host, so the user
// can tell what asked when, say, a 1Password prompt appears.
type Origin struct {
	// Node is the remote host. The daemon sets it from tailscale whois and
	// ignores whatever arrived on the wire.
	Node    string   `json:"node,omitempty"`
	Chain   []string `json:"chain,omitempty"`   // process names, outermost first, caller last
	Cwd     string   `json:"cwd,omitempty"`     // caller's working directory
	Rig     string   `json:"rig,omitempty"`     // RIG_ID from the caller's environment
	Session string   `json:"session,omitempty"` // Claude Code session id, shortened
	Command string   `json:"command,omitempty"` // caller's command line, shortened
}

// String renders the origin as one caption line, e.g.
// "foxtrotbase · rig hopwatch · claude › bash › op".
func (o *Origin) String() string {
	if o == nil {
		return "unknown caller"
	}
	var parts []string
	if o.Node != "" {
		parts = append(parts, o.Node)
	}
	if o.Rig != "" {
		parts = append(parts, "rig "+o.Rig)
	}
	if len(o.Chain) > 0 {
		parts = append(parts, strings.Join(o.Chain, " › "))
	}
	if o.Session != "" {
		parts = append(parts, "session "+o.Session)
	}
	if len(parts) == 0 {
		return "unknown caller"
	}
	return strings.Join(parts, " · ")
}

// Response represents a response from daemon to client
type Response struct {
	ID      string          `json:"id"`              // Request ID this responds to
	Success bool            `json:"success"`         // Whether command succeeded
	Error   string          `json:"error,omitempty"` // Error message if failed
	Data    json.RawMessage `json:"data,omitempty"`  // Response data if succeeded
}

// OpenRequest represents a request to open a URL
type OpenRequest struct {
	URL string `json:"url"`
}

// ForwardRequest represents a request to forward a port
type ForwardRequest struct {
	RemotePort     int    `json:"remote_port"`            // Port on remote machine
	LocalPort      int    `json:"local_port,omitempty"`   // Port on local machine (0 = same as remote)
	Host           string `json:"host,omitempty"`         // Remote host (default: localhost)
	ConnectionInfo string `json:"connection_info"`        // SSH connection identifier (hostname, user@host, etc.)
	SocketPath     string `json:"socket_path,omitempty"`  // Optional: specific socket path
	ProcessName    string `json:"process_name,omitempty"` // Name of the process that opened the port
	ProcessCwd     string `json:"process_cwd,omitempty"`  // Working directory of the process
	UntilExit      bool   `json:"until_exit,omitempty"`   // Over the tailnet: drop the forward when the requesting process exits
}

// BatchForwardRequest represents a request to forward several ports at once
// over a single SSH connection. ConnectionInfo and SocketPath are shared by
// every port in the batch.
type BatchForwardRequest struct {
	ConnectionInfo string             `json:"connection_info"`       // SSH connection identifier (hostname, user@host, etc.)
	SocketPath     string             `json:"socket_path,omitempty"` // Optional: specific control socket path
	Forwards       []BatchForwardPort `json:"forwards"`              // Ports to forward
}

// BatchForwardPort is a single port entry within a BatchForwardRequest.
type BatchForwardPort struct {
	RemotePort  int    `json:"remote_port"`            // Port on remote machine
	LocalPort   int    `json:"local_port,omitempty"`   // Port on local machine (0 = same as remote)
	Host        string `json:"host,omitempty"`         // Remote host (default: localhost)
	ProcessName string `json:"process_name,omitempty"` // Name of the process that opened the port
	ProcessCwd  string `json:"process_cwd,omitempty"`  // Working directory of the process
}

// BatchForwardResponse reports the per-port outcome of a BatchForwardRequest.
type BatchForwardResponse struct {
	Results []BatchForwardResult `json:"results"`
}

// BatchForwardResult is the outcome of forwarding a single port in a batch.
type BatchForwardResult struct {
	RemotePort int    `json:"remote_port"`
	LocalPort  int    `json:"local_port"`
	Created    bool   `json:"created"`         // true if newly established (false = already forwarded)
	Error      string `json:"error,omitempty"` // non-empty if this port failed to forward
}

// UnforwardRequest represents a request to remove a port forward
type UnforwardRequest struct {
	RemotePort     int    `json:"remote_port"`     // Port on remote machine
	Host           string `json:"host,omitempty"`  // Remote host (default: localhost)
	ConnectionInfo string `json:"connection_info"` // SSH connection identifier
}

// ForwardInfo represents information about an active forward
type ForwardInfo struct {
	RemotePort     int    `json:"remote_port"`
	LocalPort      int    `json:"local_port"`
	Host           string `json:"host"`
	ConnectionInfo string `json:"connection_info"`
	CreatedAt      string `json:"created_at"`
}

// StatusResponse represents daemon status
type StatusResponse struct {
	Version        string             `json:"version"`
	Uptime         string             `json:"uptime"`
	ActiveForwards int                `json:"active_forwards"`
	Connections    []ConnectionStatus `json:"connections,omitempty"`
}

// ConnectionStatus represents status of a single SSH connection
type ConnectionStatus struct {
	ConnectionInfo string `json:"connection_info"`
	ForwardCount   int    `json:"forward_count"`
	LastActivity   string `json:"last_activity"`
}

// ListResponse represents list of active forwards
type ListResponse struct {
	Forwards []ForwardInfo `json:"forwards"`
}

// OpProxyRequest represents a request to proxy an op CLI invocation
type OpProxyRequest struct {
	Args []string `json:"args"`
}

// OpProxyResponse represents the result of a proxied op CLI invocation
type OpProxyResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// SyncRequest is the payload of CommandSync. The caller's tailnet identity,
// not anything in the payload, decides which host the forwards belong to.
type SyncRequest struct {
	Forwards []BatchForwardPort `json:"forwards"`
}

// SyncResponse reports the outcome of every port in a SyncRequest and how
// long the lease it renewed lasts.
type SyncResponse struct {
	Results      []BatchForwardResult `json:"results"`
	LeaseSeconds int                  `json:"lease_seconds"`
}

// RelayRequest is the first line the daemon sends on a tailnet relay
// connection, naming the port on the monitor's host to reach.
type RelayRequest struct {
	Port int `json:"port,omitempty"`
	// Verify, instead of a port, asks whether the monitor itself sent the
	// request with this ID. The daemon checks before acting on open or
	// op-proxy, since anything on the remote host can reach its tailnet
	// listener but only the monitor answers on the relay.
	Verify string `json:"verify,omitempty"`
}

// RelayResponse is the monitor relay's one-line answer to a RelayRequest.
// After an OK response the connection carries the forwarded bytes.
type RelayResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// NotForwarded means the relay isn't forwarding the port at all, as
	// opposed to failing to reach it, so the daemon should drop the forward.
	NotForwarded bool `json:"not_forwarded,omitempty"`
}

// ParseRequest parses a JSON request
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}
	return &req, nil
}

// MarshalRequest marshals a request to JSON
func MarshalRequest(req *Request) ([]byte, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	return data, nil
}

// ParseResponse parses a JSON response
func ParseResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp, nil
}

// MarshalResponse marshals a response to JSON
func MarshalResponse(resp *Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return data, nil
}

// NewErrorResponse creates an error response
func NewErrorResponse(id string, err error) *Response {
	return &Response{
		ID:      id,
		Success: false,
		Error:   err.Error(),
	}
}

// NewSuccessResponse creates a success response with data
func NewSuccessResponse(id string, data interface{}) (*Response, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	return &Response{
		ID:      id,
		Success: true,
		Data:    jsonData,
	}, nil
}
