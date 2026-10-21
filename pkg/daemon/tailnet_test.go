package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/protocol"
)

func newTailnetDaemon(t *testing.T) *Daemon {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Tailnet.Enabled = true
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(d.cancel)
	go d.tailnet.Run(d.ctx)
	return d
}

func request(t *testing.T, typ protocol.CommandType, payload any) *protocol.Request {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return &protocol.Request{ID: "t", Type: typ, Payload: b}
}

func TestTailnetPeersOnlyForward(t *testing.T) {
	d := newTailnetDaemon(t)
	p := &peer{node: "foxtrotbase", ip: "100.99.110.72"}
	for _, typ := range []protocol.CommandType{protocol.CommandOpen, protocol.CommandOpProxy, protocol.CommandReconcile} {
		if resp := d.handleCommand(request(t, typ, map[string]any{}), p); resp.Success {
			t.Errorf("%s over the tailnet succeeded, want refused", typ)
		}
	}
	if resp := d.handleCommand(request(t, protocol.CommandSync, protocol.SyncRequest{}), nil); resp.Success {
		t.Error("sync over the local socket succeeded, want refused")
	}
}

func TestTailnetForwardsKeyedByWhois(t *testing.T) {
	d := newTailnetDaemon(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	p := &peer{node: "foxtrotbase", ip: "100.99.110.72"}
	resp := d.handleCommand(request(t, protocol.CommandForwardBatch, protocol.BatchForwardRequest{
		ConnectionInfo: "someone-else", // a peer can't claim another host's identity
		Forwards:       []protocol.BatchForwardPort{{RemotePort: port}},
	}), p)
	if !resp.Success {
		t.Fatalf("forward-batch failed: %s", resp.Error)
	}

	forwards := d.listForwards()
	if len(forwards) != 1 || forwards[0].ConnectionInfo != "foxtrotbase" {
		t.Fatalf("forwards = %+v, want one keyed by foxtrotbase", forwards)
	}

	// Syncing an empty set removes it.
	resp = d.handleCommand(request(t, protocol.CommandSync, protocol.SyncRequest{}), p)
	var sr protocol.SyncResponse
	if !resp.Success || json.Unmarshal(resp.Data, &sr) != nil || sr.LeaseSeconds != 360 {
		t.Fatalf("sync = %+v", resp)
	}
	if n := len(d.listForwards()); n != 0 {
		t.Errorf("forwards after empty sync = %d, want 0", n)
	}
}
