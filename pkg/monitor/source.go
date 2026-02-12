package monitor

import "context"

// PortEventSource is implemented by any monitor that can emit port events.
// Both polling-based monitors and eBPF monitors satisfy this interface.
type PortEventSource interface {
	Start(ctx context.Context) error
	Events() <-chan PortEvent
}
