//go:build integration && linux

package monitor

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"
)

func TestEBPFProbe(t *testing.T) {
	if err := probeEBPF(); err != nil {
		t.Fatalf("probeEBPF failed (run with sudo or CAP_BPF+CAP_PERFMON): %v", err)
	}
}

func TestEBPFMonitorEvents(t *testing.T) {
	if err := probeEBPF(); err != nil {
		t.Skipf("eBPF not available: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	mon := newEBPFMonitor(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Drain initial-state events from Start()
	drainEvents(t, mon.Events(), 500*time.Millisecond)

	// Open a TCP listener on a random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	t.Logf("listening on port %d", port)

	// Expect a PortOpened event for our port
	evt := waitForEvent(t, mon.Events(), port, PortOpened, 2*time.Second)
	if evt == nil {
		t.Fatalf("timed out waiting for PortOpened on port %d", port)
	}
	t.Logf("got PortOpened: port=%d pid=%d", evt.Port, evt.PID)

	// Close the listener
	ln.Close()

	// Expect a PortClosed event for our port
	evt = waitForEvent(t, mon.Events(), port, PortClosed, 2*time.Second)
	if evt == nil {
		t.Fatalf("timed out waiting for PortClosed on port %d", port)
	}
	t.Logf("got PortClosed: port=%d pid=%d", evt.Port, evt.PID)
}

func TestEBPFMonitorCompileTimeCheck(t *testing.T) {
	var _ PortEventSource = (*ebpfMonitor)(nil)
}

// drainEvents reads and discards events for the given duration.
func drainEvents(t *testing.T, ch <-chan PortEvent, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			t.Logf("drained initial event: type=%s port=%d", evt.Type, evt.Port)
		case <-timeout:
			return
		}
	}
}

// waitForEvent waits up to timeout for a PortEvent matching the given port and type.
// Other events are logged and skipped.
func waitForEvent(t *testing.T, ch <-chan PortEvent, port int, evtType EventType, timeout time.Duration) *PortEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				t.Log("event channel closed")
				return nil
			}
			if evt.Port == port && evt.Type == evtType {
				return &evt
			}
			t.Logf("skipping event: type=%s port=%d (waiting for type=%s port=%d)", evt.Type, evt.Port, evtType, port)
		case <-deadline:
			return nil
		}
	}
}
