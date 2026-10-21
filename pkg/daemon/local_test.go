package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/phinze/bankshot/pkg/config"
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
