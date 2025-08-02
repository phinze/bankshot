package protocol

import (
	"encoding/json"
	"fmt"
)

// CommandType represents the type of command
type CommandType string

const (
	// CommandOpen opens a URL in the browser
	CommandOpen CommandType = "open"
	// CommandForward requests a port forward
	CommandForward CommandType = "forward"
	// CommandStatus gets daemon status
	CommandStatus CommandType = "status"
	// CommandList lists active forwards
	CommandList CommandType = "list"
)

// Request represents a command request from client to daemon
type Request struct {
	ID      string          `json:"id"`      // Unique request ID
	Type    CommandType     `json:"type"`    // Command type
	Payload json.RawMessage `json:"payload"` // Command-specific payload
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
	RemotePort     int    `json:"remote_port"`           // Port on remote machine
	LocalPort      int    `json:"local_port,omitempty"`  // Port on local machine (0 = same as remote)
	Host           string `json:"host,omitempty"`        // Remote host (default: localhost)
	ConnectionInfo string `json:"connection_info"`       // SSH connection identifier (hostname, user@host, etc.)
	SocketPath     string `json:"socket_path,omitempty"` // Optional: specific socket path
}

// ForwardInfo represents information about an active forward
type ForwardInfo struct {
	RemotePort int    `json:"remote_port"`
	LocalPort  int    `json:"local_port"`
	Host       string `json:"host"`
	CreatedAt  string `json:"created_at"`
}

// StatusResponse represents daemon status
type StatusResponse struct {
	Version        string `json:"version"`
	Uptime         string `json:"uptime"`
	ActiveForwards int    `json:"active_forwards"`
}

// ListResponse represents list of active forwards
type ListResponse struct {
	Forwards []ForwardInfo `json:"forwards"`
}

// ParseRequest parses a JSON request
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}
	return &req, nil
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
