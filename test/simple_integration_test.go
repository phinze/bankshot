package test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/phinze/bankshot/pkg/protocol"
)

// TestSocketCommunication tests basic socket communication without daemon
func TestSocketCommunication(t *testing.T) {
	// Create temp directory for socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Create a simple socket server
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	// Handle connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleTestConnection(conn)
		}
	}()

	// Test client connection
	t.Run("BasicRequest", func(t *testing.T) {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Send request
		req := &protocol.Request{
			ID:   "test-1",
			Type: protocol.CommandStatus,
		}

		data, _ := json.Marshal(req)
		conn.Write(append(data, '\n'))

		// Read response
		resp := make([]byte, 1024)
		n, _ := conn.Read(resp)

		var response protocol.Response
		if err := json.Unmarshal(resp[:n], &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if response.ID != "test-1" {
			t.Errorf("Response ID = %v, want %v", response.ID, "test-1")
		}
		if !response.Success {
			t.Errorf("Response Success = %v, want %v", response.Success, true)
		}
	})
}

func handleTestConnection(conn net.Conn) {
	defer conn.Close()

	// Read request
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}

	// Parse request
	var req protocol.Request
	if err := json.Unmarshal(buf[:n-1], &req); err != nil { // -1 to remove newline
		return
	}

	// Create response
	resp := &protocol.Response{
		ID:      req.ID,
		Success: true,
		Data:    json.RawMessage(`{"status": "ok"}`),
	}

	// Send response
	data, _ := json.Marshal(resp)
	conn.Write(data)
}

// TestProtocolMarshaling tests protocol marshaling/unmarshaling
func TestProtocolMarshaling(t *testing.T) {
	tests := []struct {
		name string
		req  protocol.Request
	}{
		{
			name: "status request",
			req: protocol.Request{
				ID:   "test-status",
				Type: protocol.CommandStatus,
			},
		},
		{
			name: "open request",
			req: protocol.Request{
				ID:      "test-open",
				Type:    protocol.CommandOpen,
				Payload: json.RawMessage(`{"url":"https://example.com"}`),
			},
		},
		{
			name: "forward request",
			req: protocol.Request{
				ID:      "test-forward",
				Type:    protocol.CommandForward,
				Payload: json.RawMessage(`{"remote_port":8080,"local_port":8081,"connection_info":"host"}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			// Unmarshal
			var decoded protocol.Request
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Failed to unmarshal request: %v", err)
			}

			// Verify
			if decoded.ID != tt.req.ID {
				t.Errorf("ID = %v, want %v", decoded.ID, tt.req.ID)
			}
			if decoded.Type != tt.req.Type {
				t.Errorf("Type = %v, want %v", decoded.Type, tt.req.Type)
			}
		})
	}
}

// TestConcurrentConnections tests handling multiple connections
func TestConcurrentConnections(t *testing.T) {
	// Create temp directory for socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Create listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	// Handle connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleTestConnection(conn)
		}
	}()

	// Create multiple concurrent connections
	numConnections := 10
	done := make(chan bool, numConnections)

	for i := 0; i < numConnections; i++ {
		go func(id int) {
			conn, err := net.Dial("unix", socketPath)
			if err != nil {
				t.Errorf("Connection %d failed: %v", id, err)
				done <- false
				return
			}
			defer conn.Close()

			// Send request
			req := &protocol.Request{
				ID:   fmt.Sprintf("test-%d", id),
				Type: protocol.CommandStatus,
			}

			data, _ := json.Marshal(req)
			conn.Write(append(data, '\n'))

			// Read response
			resp := make([]byte, 1024)
			n, _ := conn.Read(resp)

			var response protocol.Response
			if err := json.Unmarshal(resp[:n], &response); err != nil {
				t.Errorf("Connection %d: Failed to unmarshal: %v", id, err)
				done <- false
				return
			}

			if response.ID != req.ID {
				t.Errorf("Connection %d: ID mismatch", id)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all connections
	successCount := 0
	for i := 0; i < numConnections; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != numConnections {
		t.Errorf("Not all connections succeeded: %d/%d", successCount, numConnections)
	}
}

// TestMockSSHIntegration tests integration with mock SSH
func TestMockSSHIntegration(t *testing.T) {
	mock := NewMockSSH(t)

	// Test that mock SSH script works
	t.Run("MockSSHCommands", func(t *testing.T) {
		// Test forward command
		cmd := exec.Command(mock.Path(), "-O", "forward", "-L", "8080:localhost:8080", "testhost")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("Mock SSH forward failed: %v, output: %s", err, output)
		}

		// Test cancel command
		cmd = exec.Command(mock.Path(), "-O", "cancel", "-L", "8080:localhost:8080", "testhost")
		output, err = cmd.CombinedOutput()
		if err != nil {
			t.Errorf("Mock SSH cancel failed: %v, output: %s", err, output)
		}
	})
}
