package ssh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSocket(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()
	
	// Create a regular file
	regularFile := filepath.Join(tempDir, "regular")
	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create regular file: %v", err)
	}
	
	// Test regular file
	if isSocket(regularFile) {
		t.Errorf("isSocket returned true for regular file")
	}
	
	// Test non-existent file
	if isSocket(filepath.Join(tempDir, "nonexistent")) {
		t.Errorf("isSocket returned true for non-existent file")
	}
}

func TestExpandPath(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	
	// Set test HOME
	os.Setenv("HOME", "/test/home")
	
	tests := []struct {
		input    string
		expected string
	}{
		{"~/test", "/test/home/test"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~", "~"}, // Doesn't expand bare ~
	}
	
	for _, tt := range tests {
		result := expandPath(tt.input)
		if result != tt.expected {
			t.Errorf("expandPath(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestExtractHostFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/tmp/ssh-user@example.com:22", "example.com"},
		{"/tmp/ssh-user@example.com", "example.com"},
		{"/tmp/ssh-user@example.com.sock", "example.com"},
		{"/tmp/ssh-control-master", ""},
		{"ssh_mux_user@192.168.1.1:22", "192.168.1.1"},
	}
	
	for _, tt := range tests {
		result := extractHostFromPath(tt.path)
		if result != tt.expected {
			t.Errorf("extractHostFromPath(%s) = %s, want %s", tt.path, result, tt.expected)
		}
	}
}

func TestDetectSSHConnection(t *testing.T) {
	// Save original env
	origSSHConn := os.Getenv("SSH_CONNECTION")
	origSSHClient := os.Getenv("SSH_CLIENT")
	defer func() {
		os.Setenv("SSH_CONNECTION", origSSHConn)
		os.Setenv("SSH_CLIENT", origSSHClient)
	}()
	
	// Test without SSH connection
	os.Unsetenv("SSH_CONNECTION")
	os.Unsetenv("SSH_CLIENT")
	
	_, err := DetectSSHConnection()
	if err == nil {
		t.Errorf("Expected error when not in SSH session")
	}
	
	// Test with SSH connection
	os.Setenv("SSH_CONNECTION", "192.168.1.100 50000 192.168.1.200 22")
	os.Setenv("SSH_CLIENT", "192.168.1.100 50000 22")
	
	conn, err := DetectSSHConnection()
	if err == nil {
		t.Errorf("Expected error when no ControlMaster found, got nil")
	} else if conn == nil {
		t.Errorf("Expected connection info even with error")
	} else {
		if !conn.IsSSHSession {
			t.Errorf("Expected IsSSHSession to be true")
		}
		if conn.Port != "22" {
			t.Errorf("Expected port 22, got %s", conn.Port)
		}
	}
}