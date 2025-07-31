package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProcNet(t *testing.T) {
	// Create a temporary test file with sample /proc/net/tcp content
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "tcp")
	
	// Sample data from /proc/net/tcp
	// Format: sl local_address rem_address st ...
	// Port 8080 = 0x1F90, Port 22 = 0x0016
	testData := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12346 1 0000000000000000 100 0 0 10 0
   2: 0100007F:2328 0100007F:1F90 01 00000000:00000000 00:00000000 00000000  1000        0 12347 1 0000000000000000 100 0 0 10 0`
	
	if err := os.WriteFile(testFile, []byte(testData), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	ports, err := parseProcNet(testFile, "tcp")
	if err != nil {
		t.Fatalf("parseProcNet failed: %v", err)
	}
	
	// Should find 2 LISTEN ports
	if len(ports) != 2 {
		t.Errorf("Expected 2 ports, got %d", len(ports))
	}
	
	// Check port numbers
	expectedPorts := map[int]bool{22: false, 8080: false}
	for _, port := range ports {
		if port.State != "LISTEN" {
			t.Errorf("Expected LISTEN state, got %s", port.State)
		}
		if port.Protocol != "tcp" {
			t.Errorf("Expected tcp protocol, got %s", port.Protocol)
		}
		if _, ok := expectedPorts[port.Port]; ok {
			expectedPorts[port.Port] = true
		} else {
			t.Errorf("Unexpected port %d", port.Port)
		}
	}
	
	// Verify all expected ports were found
	for port, found := range expectedPorts {
		if !found {
			t.Errorf("Expected port %d was not found", port)
		}
	}
}

func TestParseState(t *testing.T) {
	tests := []struct {
		hexState string
		expected string
	}{
		{"01", "ESTABLISHED"},
		{"0A", "LISTEN"},
		{"06", "TIME_WAIT"},
		{"FF", "UNKNOWN"},
	}
	
	for _, tt := range tests {
		result := parseState(tt.hexState)
		if result != tt.expected {
			t.Errorf("parseState(%s) = %s, want %s", tt.hexState, result, tt.expected)
		}
	}
}