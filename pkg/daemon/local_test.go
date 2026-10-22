package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/protocol"
)

// TestLocalProxy runs the monitor's local socket against a fake daemon and
// checks that an open goes through with a fresh ID the monitor holds in
// flight, and an origin naming the caller.
func TestLocalProxy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tailnet.Enabled = true
	cfg.Tailnet.LocalSocket = filepath.Join(t.TempDir(), "bankshot.sock")
	m := &Monitor{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), config: cfg}

	daemonLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer daemonLn.Close()
	seen := make(chan *protocol.Request, 1)
	go func() {
		conn, err := daemonLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadBytes('\n')
		req, _ := protocol.ParseRequest(line)
		if !m.inFlight.has(req.ID) {
			req.Origin = nil // flag it: the monitor wouldn't vouch for this
		}
		seen <- req
		resp, _ := protocol.NewSuccessResponse(req.ID, map[string]string{"message": "ok"})
		b, _ := json.Marshal(resp)
		_, _ = conn.Write(append(b, '\n'))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.serveLocal(ctx, &localDaemonClient{network: "tcp", address: daemonLn.Addr().String(), logger: m.logger})

	var conn net.Conn
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if conn, err = net.Dial("unix", cfg.Tailnet.LocalSocket); err == nil || time.Now().After(deadline) {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload, _ := json.Marshal(protocol.OpenRequest{URL: "https://example.com"})
	b, _ := json.Marshal(protocol.Request{ID: "caller-id", Type: protocol.CommandOpen, Payload: payload})
	_, _ = conn.Write(append(b, '\n'))

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := protocol.ParseResponse(line)
	if !resp.Success || resp.ID != "caller-id" {
		t.Errorf("response = %+v, want success under the caller's ID", resp)
	}

	req := <-seen
	if req.ID == "caller-id" {
		t.Error("monitor passed the caller's ID through; want a fresh one")
	}
	if req.Origin == nil || len(req.Origin.Chain) == 0 {
		t.Errorf("origin = %+v, want the caller's process chain (and the ID in flight)", req.Origin)
	}
	if m.inFlight.has(req.ID) {
		t.Error("request still in flight after it finished")
	}
}

// TestLocalForwardUntilExit runs a manual forward through the local socket
// from a process that then exits, and checks the monitor pins it for that
// process and drops it once the process is gone.
func TestLocalForwardUntilExit(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("needs python3 for a short-lived socket client")
	}
	cfg := config.DefaultConfig()
	cfg.Tailnet.Enabled = true
	cfg.Tailnet.LocalSocket = filepath.Join(t.TempDir(), "bankshot.sock")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	daemonLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer daemonLn.Close()
	seen := make(chan protocol.CommandType, 10)
	go func() {
		for {
			conn, err := daemonLn.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			req, _ := protocol.ParseRequest(line)
			seen <- req.Type
			resp, _ := protocol.NewSuccessResponse(req.ID, nil)
			b, _ := json.Marshal(resp)
			_, _ = conn.Write(append(b, '\n'))
			conn.Close()
		}
	}()
	client := &localDaemonClient{network: "tcp", address: daemonLn.Addr().String(), logger: logger}
	sm, err := monitor.NewSessionMonitor(monitor.SessionConfig{SessionID: "test", DaemonClient: client, Logger: logger, PortEventSource: quietSource{}})
	if err != nil {
		t.Fatal(err)
	}
	m := &Monitor{logger: logger, config: cfg, sessionMonitor: sm}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.serveLocal(ctx, client)
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(cfg.Tailnet.LocalSocket); err == nil || time.Now().After(deadline) {
			break
		}
	}

	payload, _ := json.Marshal(protocol.ForwardRequest{RemotePort: 47123, UntilExit: true})
	req, _ := json.Marshal(protocol.Request{ID: "caller-id", Type: protocol.CommandForward, Payload: payload})
	script := `import socket, sys
s = socket.socket(socket.AF_UNIX)
s.connect(sys.argv[1])
s.sendall(sys.argv[2].encode() + b"\n")
sys.stdout.write(s.makefile().readline())`
	out, err := exec.Command(python, "-c", script, cfg.Tailnet.LocalSocket, string(req)).Output()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := protocol.ParseResponse(out)
	if err != nil || !resp.Success {
		t.Fatalf("forward = %s, %v", out, err)
	}
	if got := <-seen; got != protocol.CommandForward {
		t.Errorf("daemon got %s, want forward", got)
	}
	if !sm.Forwarding(47123) {
		t.Fatal("port not pinned after a manual forward")
	}

	// The client has exited, so the monitor's next cleanup drops its forward.
	go func() { _ = sm.Start(ctx) }()
	for deadline := time.Now().Add(10 * time.Second); sm.Forwarding(47123); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("forward still pinned after its process exited")
		}
	}
	if got := <-seen; got != protocol.CommandUnforward {
		t.Errorf("daemon got %s, want unforward", got)
	}
}

// quietSource is a port event source that never reports anything.
type quietSource struct{}

func (quietSource) Start(context.Context) error      { return nil }
func (quietSource) Events() <-chan monitor.PortEvent { return nil }
