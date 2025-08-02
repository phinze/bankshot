package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// MockSSH creates a mock SSH command for testing
type MockSSH struct {
	t          *testing.T
	scriptPath string
	tmpDir     string
}

// NewMockSSH creates a new mock SSH command
func NewMockSSH(t *testing.T) *MockSSH {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "ssh")

	// Create mock SSH script
	script := `#!/usr/bin/env bash
# Mock SSH script for testing

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -O)
            shift
            OPERATION=$1
            shift
            ;;
        -L)
            shift
            FORWARD=$1
            shift
            ;;
        -G)
            # Mock ssh -G output
            echo "hostname testhost"
            echo "user testuser"
            echo "port 22"
            echo "controlpath /tmp/bankshot-test-%h-%p-%r"
            exit 0
            ;;
        *)
            CONNECTION=$1
            shift
            ;;
    esac
done

# Handle operations
case $OPERATION in
    check)
        # Always report connection is active
        exit 0
        ;;
    forward)
        # Mock successful forward
        echo "Forward established: $FORWARD"
        exit 0
        ;;
    cancel)
        # Mock successful cancel
        echo "Forward cancelled: $FORWARD"
        exit 0
        ;;
    *)
        echo "Unknown operation: $OPERATION"
        exit 1
        ;;
esac
`

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to create mock SSH script: %v", err)
	}

	return &MockSSH{
		t:          t,
		scriptPath: scriptPath,
		tmpDir:     tmpDir,
	}
}

// Path returns the path to the mock SSH command
func (m *MockSSH) Path() string {
	return m.scriptPath
}

// CreateControlSocket creates a mock control socket
func (m *MockSSH) CreateControlSocket(connectionInfo string) string {
	// Create a mock socket file
	socketPath := filepath.Join(m.tmpDir, fmt.Sprintf("bankshot-test-%s", strings.ReplaceAll(connectionInfo, "@", "-")))
	
	// Create empty file to simulate socket
	if err := os.WriteFile(socketPath, []byte{}, 0600); err != nil {
		m.t.Fatalf("Failed to create mock socket: %v", err)
	}

	return socketPath
}

// TestMockSSH verifies the mock SSH works correctly
func TestMockSSH(t *testing.T) {
	mock := NewMockSSH(t)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{
			name: "check connection",
			args: []string{"-O", "check", "testhost"},
			want: 0,
		},
		{
			name: "forward port",
			args: []string{"-O", "forward", "-L", "8080:localhost:8080", "testhost"},
			want: 0,
		},
		{
			name: "cancel forward",
			args: []string{"-O", "cancel", "-L", "8080:localhost:8080", "testhost"},
			want: 0,
		},
		{
			name: "get config",
			args: []string{"-G", "testhost"},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(mock.Path(), tt.args...)
			err := cmd.Run()

			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("Command failed: %v", err)
				}
			}

			if exitCode != tt.want {
				t.Errorf("Exit code = %d, want %d", exitCode, tt.want)
			}
		})
	}
}