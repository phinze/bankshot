package monitor

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/phinze/bankshot/pkg/protocol"
)

// newPinTestMonitor returns a monitor whose processes are all named "" and
// have the start times in starts; a pid missing from starts isn't running.
func newPinTestMonitor(client DaemonClient, starts map[int]string) *SessionMonitor {
	sm, _ := NewSessionMonitor(SessionConfig{
		SessionID:       "test",
		DaemonClient:    client,
		IgnorePorts:     []int{9000},
		GracePeriod:     0,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		PortEventSource: &mockPortEventSource{},
	})
	sm.resolveProcessName = func(int) string { return "" }
	sm.resolveProcessCwd = func(int) string { return "" }
	sm.resolveParentPID = func(int) int { return 0 }
	sm.resolveStartTime = func(pid int) string { return starts[pid] }
	return sm
}

// syncedPorts returns the remote -> local ports in the last sync request.
func syncedPorts(t *testing.T, client *mockDaemonClient) map[int]int {
	t.Helper()
	syncs := client.requestsOfType(protocol.CommandSync)
	if len(syncs) == 0 {
		t.Fatal("no sync request sent")
	}
	var sr protocol.SyncRequest
	if err := json.Unmarshal(syncs[len(syncs)-1].Payload, &sr); err != nil {
		t.Fatal(err)
	}
	ports := make(map[int]int)
	for _, p := range sr.Forwards {
		ports[p.RemotePort] = p.LocalPort
	}
	return ports
}

func TestPinSkipsFiltersAndRidesSync(t *testing.T) {
	client := &mockDaemonClient{}
	sm := newPinTestMonitor(client, nil)

	// 9000 is ignored and 80 is privileged; a pin forwards both anyway,
	// and nothing has to be listening yet.
	if err := sm.Pin(9000, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := sm.Pin(80, 8080, 0); err != nil {
		t.Fatal(err)
	}
	if len(client.requestsOfType(protocol.CommandForward)) != 2 {
		t.Errorf("want a forward request per pin")
	}
	if !sm.Forwarding(9000) || !sm.Forwarding(80) {
		t.Error("relay wouldn't reach a pinned port")
	}

	_, _ = sm.Sync()
	if got := syncedPorts(t, client); got[9000] != 9000 || got[80] != 8080 {
		t.Errorf("synced %v, want 9000->9000 and 80->8080", got)
	}
}

func TestPinOutlivesItsPortClosing(t *testing.T) {
	client := &mockDaemonClient{}
	sm := newPinTestMonitor(client, nil)

	sm.handlePortEvent(PortEvent{Type: PortOpened, Port: 5000, BindAddr: "127.0.0.1", Timestamp: time.Now()})
	if err := sm.Pin(5000, 0, 0); err != nil {
		t.Fatal(err)
	}
	// GetListeningPorts won't list 5000, so the close counts.
	sm.handlePortEvent(PortEvent{Type: PortClosed, Port: 5000, BindAddr: "127.0.0.1", Timestamp: time.Now()})
	sm.cleanupPendingRemovals()

	if n := len(client.requestsOfType(protocol.CommandUnforward)); n != 0 {
		t.Errorf("sent %d unforwards for a pinned port", n)
	}
	if !sm.Forwarding(5000) {
		t.Error("pinned port stopped forwarding when its server closed")
	}
}

func TestUnpinKeepsAnAutoForward(t *testing.T) {
	client := &mockDaemonClient{}
	sm := newPinTestMonitor(client, nil)

	sm.handlePortEvent(PortEvent{Type: PortOpened, Port: 5000, BindAddr: "127.0.0.1", Timestamp: time.Now()})
	if err := sm.Pin(5000, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := sm.Unpin(5000); err != nil {
		t.Fatal(err)
	}
	if n := len(client.requestsOfType(protocol.CommandUnforward)); n != 0 {
		t.Errorf("sent %d unforwards for a port still forwarded automatically", n)
	}
	if !sm.Forwarding(5000) {
		t.Error("auto-forwarded port stopped forwarding after unpin")
	}
	if err := sm.Unpin(5000); err == nil {
		t.Error("unpinning an auto-forward that was never pinned should say why it stays")
	}
}

func TestUnpinMovesAnAutoForwardBack(t *testing.T) {
	client := &mockDaemonClient{}
	sm := newPinTestMonitor(client, nil)

	sm.handlePortEvent(PortEvent{Type: PortOpened, Port: 5000, BindAddr: "127.0.0.1", Timestamp: time.Now()})
	if err := sm.Pin(5000, 5432, 0); err != nil {
		t.Fatal(err)
	}
	if n := len(client.requestsOfType(protocol.CommandUnforward)); n != 1 {
		t.Errorf("sent %d unforwards, want 1 to move the auto-forward off 5000", n)
	}
	_, _ = sm.Sync()
	if got := syncedPorts(t, client); got[5000] != 5432 {
		t.Errorf("synced %v, want the pin's 5000->5432", got)
	}

	if err := sm.Unpin(5000); err != nil {
		t.Fatal(err)
	}
	if n := len(client.requestsOfType(protocol.CommandUnforward)); n != 2 {
		t.Errorf("sent %d unforwards, want a second for the pin's forward", n)
	}
	_, _ = sm.Sync()
	if got := syncedPorts(t, client); got[5000] != 5000 {
		t.Errorf("synced %v, want the auto-forward's 5000->5000 back", got)
	}
}

func TestPinsReapedWhenHolderExits(t *testing.T) {
	client := &mockDaemonClient{}
	starts := map[int]string{100: "111", 200: "222"}
	sm := newPinTestMonitor(client, starts)

	if err := sm.Pin(5000, 0, 100); err != nil {
		t.Fatal(err)
	}
	if err := sm.Pin(5001, 0, 200); err != nil {
		t.Fatal(err)
	}
	if err := sm.Pin(5002, 0, 0); err != nil {
		t.Fatal(err)
	}

	delete(starts, 100) // exited
	starts[200] = "999" // exited, and its pid went to something new
	sm.reapPins()

	for _, port := range []int{5000, 5001} {
		if sm.Forwarding(port) {
			t.Errorf("port %d still pinned after its holder exited", port)
		}
	}
	if !sm.Forwarding(5002) {
		t.Error("unheld pin reaped")
	}
	if n := len(client.requestsOfType(protocol.CommandUnforward)); n != 2 {
		t.Errorf("sent %d unforwards, want 2", n)
	}
}

func TestPinHeldByProcessDoesNotShortenUnheldPin(t *testing.T) {
	client := &mockDaemonClient{}
	starts := map[int]string{100: "111"}
	sm := newPinTestMonitor(client, starts)

	if err := sm.Pin(5000, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := sm.Pin(5000, 0, 100); err != nil {
		t.Fatal(err)
	}
	delete(starts, 100)
	sm.reapPins()
	if !sm.Forwarding(5000) {
		t.Error("a process's exit dropped a pin that was held until unpinned")
	}
}

func TestPinRefusesExitedHolder(t *testing.T) {
	sm := newPinTestMonitor(&mockDaemonClient{}, nil)
	if err := sm.Pin(5000, 0, 100); err == nil {
		t.Error("pinned for a process that isn't running")
	}
	if sm.Forwarding(5000) {
		t.Error("refused pin left the port forwarding")
	}
}

type refusingDaemonClient struct{}

func (refusingDaemonClient) SendRequest(req *protocol.Request) (*protocol.Response, error) {
	return nil, fmt.Errorf("daemon asleep")
}

func TestPinRolledBackWhenDaemonFails(t *testing.T) {
	sm := newPinTestMonitor(refusingDaemonClient{}, nil)
	if err := sm.Pin(5000, 0, 0); err == nil {
		t.Fatal("pin succeeded with the daemon down")
	}
	if sm.Forwarding(5000) {
		t.Error("failed pin left the port forwarding")
	}
}

func TestUnpinHandsOverAPortThatOpenedWhilePinned(t *testing.T) {
	client := &mockDaemonClient{}
	sm := newPinTestMonitor(client, nil)

	if err := sm.Pin(5000, 0, 0); err != nil {
		t.Fatal(err)
	}
	sm.handlePortEvent(PortEvent{Type: PortOpened, Port: 5000, BindAddr: "127.0.0.1", Timestamp: time.Now()})
	if err := sm.Unpin(5000); err != nil {
		t.Fatal(err)
	}
	sm.handlePortEvent(PortEvent{Type: PortClosed, Port: 5000, BindAddr: "127.0.0.1", Timestamp: time.Now()})
	sm.cleanupPendingRemovals()

	if n := len(client.requestsOfType(protocol.CommandUnforward)); n != 1 {
		t.Errorf("sent %d unforwards once the auto-forward's port closed, want 1", n)
	}
}
