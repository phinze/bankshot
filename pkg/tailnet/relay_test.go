package tailnet_test

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/phinze/bankshot/pkg/forwarder"
	"github.com/phinze/bankshot/pkg/tailnet"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func port(ln net.Listener) int { return ln.Addr().(*net.TCPAddr).Port }

// TestRelayEndToEnd runs a real daemon-side forwarder against a real monitor
// relay, both on loopback: bytes written to the forwarded local port reach the
// server behind the relay, and its answer comes back.
func TestRelayEndToEnd(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	backend := listen(t)
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				in, _ := io.ReadAll(c)
				_, _ = c.Write(append([]byte("got:"), in...))
			}()
		}
	}()

	relayLn := listen(t)
	relay := &tailnet.Relay{
		Gate:       tailnet.NewGate(nil, nil, true),
		Logger:     logger,
		Forwarding: func(p int) bool { return p == port(backend) },
	}
	go relay.Serve(relayLn)

	f := forwarder.NewTailnet(logger, port(relayLn), time.Minute)
	target := forwarder.Target{ConnectionInfo: "foxtrotbase", Address: "127.0.0.1"}

	freeLn := listen(t)
	local := port(freeLn)
	freeLn.Close()
	unforwardedLn := listen(t)
	unforwarded := port(unforwardedLn)
	unforwardedLn.Close()

	results, err := f.AddForwards(target, []forwarder.Port{
		{RemotePort: port(backend), LocalPort: local},
		{RemotePort: unforwarded},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("forward %d: %v", r.RemotePort, r.Err)
		}
	}
	t.Cleanup(func() {
		_ = f.RemoveForward("foxtrotbase", port(backend), "")
		_ = f.RemoveForward("foxtrotbase", unforwarded, "")
	})

	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(local)))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte("ping"))
	_ = conn.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(conn)
	conn.Close()
	if err != nil || string(got) != "got:ping" {
		t.Fatalf("echo through relay = %q, %v", got, err)
	}

	// The relay won't reach a port the monitor isn't forwarding, and the
	// forwarder drops its forward when told so.
	if c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(unforwarded))); err == nil {
		_, _ = io.ReadAll(c)
		c.Close()
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var still bool
		for _, fwd := range f.ListForwards() {
			still = still || fwd.RemotePort == unforwarded
		}
		if !still {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("forward of a port the relay refuses was not dropped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
