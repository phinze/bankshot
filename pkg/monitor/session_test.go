package monitor

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/phinze/bankshot/pkg/protocol"
)

// mockDaemonClient records forward/unforward requests for test assertions
type mockDaemonClient struct {
	mu       sync.Mutex
	requests []*protocol.Request
}

func (m *mockDaemonClient) SendRequest(req *protocol.Request) (*protocol.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return &protocol.Response{Success: true}, nil
}

func (m *mockDaemonClient) forwardCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, r := range m.requests {
		if r.Type == protocol.CommandForward {
			count++
		}
	}
	return count
}

func TestShouldForwardPort(t *testing.T) {
	tests := []struct {
		name        string
		port        int
		bindAddr    string
		portRanges  []PortRange
		ignorePorts map[int]bool
		want        bool
	}{
		{
			name: "default non-privileged port",
			port: 8080, bindAddr: "0.0.0.0", portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "privileged port rejected",
			port: 22, bindAddr: "0.0.0.0", portRanges: nil, ignorePorts: nil,
			want: false,
		},
		{
			name: "boundary 1024 accepted",
			port: 1024, bindAddr: "0.0.0.0", portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "boundary 1023 rejected",
			port: 1023, bindAddr: "0.0.0.0", portRanges: nil, ignorePorts: nil,
			want: false,
		},
		{
			name: "high ephemeral port (OAuth)",
			port: 37593, bindAddr: "0.0.0.0", portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "ignored port rejected",
			port: 8080, bindAddr: "0.0.0.0", portRanges: nil, ignorePorts: map[int]bool{8080: true},
			want: false,
		},
		{
			name: "explicit range includes port",
			port: 5000, bindAddr: "0.0.0.0", portRanges: []PortRange{{3000, 9999}}, ignorePorts: nil,
			want: true,
		},
		{
			name: "explicit range excludes port",
			port: 37593, bindAddr: "0.0.0.0", portRanges: []PortRange{{3000, 9999}}, ignorePorts: nil,
			want: false,
		},
		{
			name: "ignore beats explicit range",
			port: 5000, bindAddr: "0.0.0.0", portRanges: []PortRange{{3000, 9999}}, ignorePorts: map[int]bool{5000: true},
			want: false,
		},
		// Bind address filtering
		{
			name: "wildcard IPv4 allowed",
			port: 8080, bindAddr: "0.0.0.0", portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "loopback IPv4 allowed",
			port: 8080, bindAddr: "127.0.0.1", portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "wildcard IPv6 allowed",
			port: 8080, bindAddr: "::", portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "loopback IPv6 allowed",
			port: 8080, bindAddr: "::1", portRanges: nil, ignorePorts: nil,
			want: true,
		},
		{
			name: "Tailscale IPv4 rejected",
			port: 8080, bindAddr: "100.99.110.72", portRanges: nil, ignorePorts: nil,
			want: false,
		},
		{
			name: "Tailscale IPv6 rejected",
			port: 8080, bindAddr: "fd7a:115c:a1e0::c501:6e48", portRanges: nil, ignorePorts: nil,
			want: false,
		},
		{
			name: "LAN IP rejected",
			port: 8080, bindAddr: "192.168.1.100", portRanges: nil, ignorePorts: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldForwardPort(tt.port, tt.bindAddr, tt.portRanges, tt.ignorePorts)
			if got != tt.want {
				t.Errorf("ShouldForwardPort(%d, %q, %v, %v) = %v, want %v",
					tt.port, tt.bindAddr, tt.portRanges, tt.ignorePorts, got, tt.want)
			}
		})
	}
}

func TestIsLocalAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"0.0.0.0", true},
		{"127.0.0.1", true},
		{"::", true},
		{"::1", true},
		{"100.99.110.72", false},
		{"192.168.1.100", false},
		{"10.0.0.1", false},
		{"fd7a:115c:a1e0::c501:6e48", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := IsLocalAddr(tt.addr)
			if got != tt.want {
				t.Errorf("IsLocalAddr(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestParseHexAddr(t *testing.T) {
	tests := []struct {
		name     string
		hexStr   string
		protocol string
		want     string
	}{
		{"IPv4 wildcard", "00000000", "tcp", "0.0.0.0"},
		{"IPv4 loopback", "0100007F", "tcp", "127.0.0.1"},
		{"IPv4 Tailscale", "48006E64", "tcp", "100.110.0.72"},
		{"IPv6 wildcard", "00000000000000000000000000000000", "tcp6", "::"},
		{"IPv6 loopback", "00000000000000000000000001000000", "tcp6", "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHexAddr(tt.hexStr, tt.protocol)
			if got != tt.want {
				t.Errorf("parseHexAddr(%q, %q) = %q, want %q", tt.hexStr, tt.protocol, got, tt.want)
			}
		})
	}
}

func TestHandlePortEvent_IgnoreProcesses(t *testing.T) {
	// Stub process name resolver so tests don't touch /proc
	stubResolver := func(pid int) string {
		switch pid {
		case 100:
			return "registry"
		case 200:
			return "node"
		default:
			return ""
		}
	}

	tests := []struct {
		name            string
		ignoreProcesses []string
		event           PortEvent
		wantForward     bool
	}{
		{
			name:            "ignored process is filtered",
			ignoreProcesses: []string{"registry"},
			event: PortEvent{
				Type: PortOpened, PID: 100, Port: 5000,
				BindAddr: "0.0.0.0", Timestamp: time.Now(),
			},
			wantForward: false,
		},
		{
			name:            "non-ignored process is forwarded",
			ignoreProcesses: []string{"registry"},
			event: PortEvent{
				Type: PortOpened, PID: 200, Port: 3000,
				BindAddr: "0.0.0.0", Timestamp: time.Now(),
			},
			wantForward: true,
		},
		{
			name:            "PID 0 bypasses process filter",
			ignoreProcesses: []string{"registry"},
			event: PortEvent{
				Type: PortOpened, PID: 0, Port: 5000,
				BindAddr: "0.0.0.0", Timestamp: time.Now(),
			},
			wantForward: true,
		},
		{
			name:            "case-insensitive match",
			ignoreProcesses: []string{"Registry"},
			event: PortEvent{
				Type: PortOpened, PID: 100, Port: 5000,
				BindAddr: "0.0.0.0", Timestamp: time.Now(),
			},
			wantForward: false,
		},
		{
			name:            "substring match",
			ignoreProcesses: []string{"regist"},
			event: PortEvent{
				Type: PortOpened, PID: 100, Port: 5000,
				BindAddr: "0.0.0.0", Timestamp: time.Now(),
			},
			wantForward: false,
		},
		{
			name:            "empty ignore list forwards everything",
			ignoreProcesses: nil,
			event: PortEvent{
				Type: PortOpened, PID: 100, Port: 5000,
				BindAddr: "0.0.0.0", Timestamp: time.Now(),
			},
			wantForward: true,
		},
		{
			name:            "event with ProcessName already set skips resolve",
			ignoreProcesses: []string{"registry"},
			event: PortEvent{
				Type: PortOpened, PID: 999, Port: 5000,
				ProcessName: "registry",
				BindAddr:    "0.0.0.0", Timestamp: time.Now(),
			},
			wantForward: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockDaemonClient{}
			sm := &SessionMonitor{
				sessionID:          "test",
				daemonClient:       client,
				logger:             slog.Default(),
				ignoreProcesses:    tt.ignoreProcesses,
				resolveProcessName: stubResolver,
				activeForwards:     make(map[string]ForwardInfo),
				pendingRemovals:    make(map[string]time.Time),
			}

			sm.handlePortEvent(tt.event)

			got := client.forwardCount() > 0
			if got != tt.wantForward {
				t.Errorf("forward created = %v, want %v", got, tt.wantForward)
			}
		})
	}
}
