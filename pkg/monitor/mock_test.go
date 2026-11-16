package monitor

import (
	"sync"

	"github.com/phinze/bankshot/pkg/protocol"
)

// mockDaemonClient records requests for test assertions.
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

// requestsOfType returns all recorded requests of the given command type.
func (m *mockDaemonClient) requestsOfType(t protocol.CommandType) []*protocol.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*protocol.Request
	for _, r := range m.requests {
		if r.Type == t {
			out = append(out, r)
		}
	}
	return out
}
