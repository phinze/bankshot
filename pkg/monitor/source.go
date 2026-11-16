package monitor

import "context"

// PortEventSource is implemented by any monitor that can emit port events.
// The polling Monitor satisfies it.
type PortEventSource interface {
	Start(ctx context.Context) error
	Events() <-chan PortEvent
}
