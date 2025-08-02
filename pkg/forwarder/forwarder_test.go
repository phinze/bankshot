package forwarder

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	f := New(logger, "ssh")

	if f == nil {
		t.Fatal("New() returned nil")
	}
	if f.logger == nil {
		t.Error("New() created Forwarder with nil logger")
	}
	if f.sshCmd != "ssh" {
		t.Errorf("New() sshCmd = %v, want %v", f.sshCmd, "ssh")
	}
	if f.forwards == nil {
		t.Error("New() created Forwarder with nil forwards map")
	}
}

func TestAddForward(t *testing.T) {
	// Skip if ssh command is not available
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh command not found")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := New(logger, "ssh")

	tests := []struct {
		name           string
		socketPath     string
		connectionInfo string
		remotePort     int
		localPort      int
		host           string
		wantErr        bool
	}{
		{
			name:           "basic forward",
			socketPath:     "/tmp/test.sock",
			connectionInfo: "test-host",
			remotePort:     8080,
			localPort:      8081,
			host:           "localhost",
			wantErr:        true, // Will fail without actual SSH connection
		},
		{
			name:           "default local port",
			socketPath:     "/tmp/test.sock",
			connectionInfo: "test-host",
			remotePort:     8080,
			localPort:      0,
			host:           "localhost",
			wantErr:        true, // Will fail without actual SSH connection
		},
		{
			name:           "default host",
			socketPath:     "/tmp/test.sock",
			connectionInfo: "test-host",
			remotePort:     8080,
			localPort:      8081,
			host:           "",
			wantErr:        true, // Will fail without actual SSH connection
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.AddForward(tt.socketPath, tt.connectionInfo, tt.remotePort, tt.localPort, tt.host)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddForward() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListForwards(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := New(logger, "ssh")

	// Initially empty
	forwards := f.ListForwards()
	if len(forwards) != 0 {
		t.Errorf("ListForwards() initial length = %v, want %v", len(forwards), 0)
	}

	// Add some forwards manually (bypassing SSH command)
	f.mu.Lock()
	f.forwards["test-host:localhost:8080"] = &Forward{
		RemotePort:     8080,
		LocalPort:      8081,
		Host:           "localhost",
		SocketPath:     "/tmp/test.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.forwards["test-host2:localhost:9090"] = &Forward{
		RemotePort:     9090,
		LocalPort:      9091,
		Host:           "localhost",
		SocketPath:     "/tmp/test2.sock",
		ConnectionInfo: "test-host2",
		CreatedAt:      time.Now(),
	}
	f.mu.Unlock()

	// Should return both forwards
	forwards = f.ListForwards()
	if len(forwards) != 2 {
		t.Errorf("ListForwards() length = %v, want %v", len(forwards), 2)
	}
}

func TestRemoveForward(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := New(logger, "ssh")

	// Add a forward manually
	f.mu.Lock()
	f.forwards["test-host:localhost:8080"] = &Forward{
		RemotePort:     8080,
		LocalPort:      8081,
		Host:           "localhost",
		SocketPath:     "/tmp/test.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.mu.Unlock()

	// Try to remove non-existent forward
	err := f.RemoveForward("test-host", 9999, "localhost")
	if err == nil {
		t.Error("RemoveForward() for non-existent forward should error")
	}

	// Remove existing forward (will fail SSH command but should still remove from map)
	err = f.RemoveForward("test-host", 8080, "localhost")
	if err != nil {
		t.Errorf("RemoveForward() unexpected error: %v", err)
	}

	// Verify it's removed
	forwards := f.ListForwards()
	if len(forwards) != 0 {
		t.Errorf("ListForwards() after remove length = %v, want %v", len(forwards), 0)
	}
}

func TestCleanupForSocket(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := New(logger, "ssh")

	// Add multiple forwards
	f.mu.Lock()
	f.forwards["test-host:localhost:8080"] = &Forward{
		RemotePort:     8080,
		LocalPort:      8081,
		Host:           "localhost",
		SocketPath:     "/tmp/test.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.forwards["test-host:localhost:8090"] = &Forward{
		RemotePort:     8090,
		LocalPort:      8091,
		Host:           "localhost",
		SocketPath:     "/tmp/test.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.forwards["test-host2:localhost:9090"] = &Forward{
		RemotePort:     9090,
		LocalPort:      9091,
		Host:           "localhost",
		SocketPath:     "/tmp/test2.sock",
		ConnectionInfo: "test-host2",
		CreatedAt:      time.Now(),
	}
	f.mu.Unlock()

	// Cleanup for specific socket
	f.CleanupForSocket("/tmp/test.sock")

	// Should only have one forward left
	forwards := f.ListForwards()
	if len(forwards) != 1 {
		t.Errorf("ListForwards() after cleanup length = %v, want %v", len(forwards), 1)
	}
	if len(forwards) > 0 && forwards[0].SocketPath != "/tmp/test2.sock" {
		t.Errorf("Wrong forward remaining after cleanup")
	}
}

func TestCleanupForConnection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := New(logger, "ssh")

	// Add multiple forwards
	f.mu.Lock()
	f.forwards["test-host:localhost:8080"] = &Forward{
		RemotePort:     8080,
		LocalPort:      8081,
		Host:           "localhost",
		SocketPath:     "/tmp/test.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.forwards["test-host:localhost:8090"] = &Forward{
		RemotePort:     8090,
		LocalPort:      8091,
		Host:           "localhost",
		SocketPath:     "/tmp/test2.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.forwards["test-host2:localhost:9090"] = &Forward{
		RemotePort:     9090,
		LocalPort:      9091,
		Host:           "localhost",
		SocketPath:     "/tmp/test3.sock",
		ConnectionInfo: "test-host2",
		CreatedAt:      time.Now(),
	}
	f.mu.Unlock()

	// Cleanup for specific connection
	f.CleanupForConnection("test-host")

	// Should only have one forward left
	forwards := f.ListForwards()
	if len(forwards) != 1 {
		t.Errorf("ListForwards() after cleanup length = %v, want %v", len(forwards), 1)
	}
	if len(forwards) > 0 && forwards[0].ConnectionInfo != "test-host2" {
		t.Errorf("Wrong forward remaining after cleanup")
	}
}

func TestListConnectionForwards(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := New(logger, "ssh")

	// Add multiple forwards
	f.mu.Lock()
	f.forwards["test-host:localhost:8080"] = &Forward{
		RemotePort:     8080,
		LocalPort:      8081,
		Host:           "localhost",
		SocketPath:     "/tmp/test.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.forwards["test-host:localhost:8090"] = &Forward{
		RemotePort:     8090,
		LocalPort:      8091,
		Host:           "localhost",
		SocketPath:     "/tmp/test2.sock",
		ConnectionInfo: "test-host",
		CreatedAt:      time.Now(),
	}
	f.forwards["test-host2:localhost:9090"] = &Forward{
		RemotePort:     9090,
		LocalPort:      9091,
		Host:           "localhost",
		SocketPath:     "/tmp/test3.sock",
		ConnectionInfo: "test-host2",
		CreatedAt:      time.Now(),
	}
	f.mu.Unlock()

	// List forwards for test-host
	forwards := f.ListConnectionForwards("test-host")
	if len(forwards) != 2 {
		t.Errorf("ListConnectionForwards('test-host') length = %v, want %v", len(forwards), 2)
	}

	// List forwards for test-host2
	forwards = f.ListConnectionForwards("test-host2")
	if len(forwards) != 1 {
		t.Errorf("ListConnectionForwards('test-host2') length = %v, want %v", len(forwards), 1)
	}

	// List forwards for non-existent connection
	forwards = f.ListConnectionForwards("non-existent")
	if len(forwards) != 0 {
		t.Errorf("ListConnectionForwards('non-existent') length = %v, want %v", len(forwards), 0)
	}
}

func TestFindControlSocket(t *testing.T) {
	// Skip if ssh command is not available
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh command not found")
	}

	// This test will fail without an actual SSH connection
	// Just test that the function doesn't panic
	_, err := FindControlSocket("test-host")
	if err == nil {
		t.Error("FindControlSocket() should fail for non-existent connection")
	}
}

func TestKeyGeneration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := New(logger, "ssh")

	// Test that duplicate forwards are handled correctly
	f.mu.Lock()
	key1 := fmt.Sprintf("%s:%s:%d", "host1", "localhost", 8080)
	key2 := fmt.Sprintf("%s:%s:%d", "host2", "localhost", 8080)

	f.forwards[key1] = &Forward{
		RemotePort:     8080,
		LocalPort:      8081,
		Host:           "localhost",
		SocketPath:     "/tmp/test.sock",
		ConnectionInfo: "host1",
		CreatedAt:      time.Now(),
	}
	f.forwards[key2] = &Forward{
		RemotePort:     8080,
		LocalPort:      8082,
		Host:           "localhost",
		SocketPath:     "/tmp/test2.sock",
		ConnectionInfo: "host2",
		CreatedAt:      time.Now(),
	}
	f.mu.Unlock()

	// Both forwards should exist
	forwards := f.ListForwards()
	if len(forwards) != 2 {
		t.Errorf("Should support multiple connections to same port, got %v forwards", len(forwards))
	}
}
