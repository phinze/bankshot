package forwarder

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/phinze/bankshot/pkg/tailnet"
)

// fakeRelay stands in for a monitor's relay: it answers RelayRequests for
// ports it was told about by dialing them on loopback.
func fakeRelay(t *testing.T, forwarded map[int]bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				line, err := reader.ReadBytes('\n')
				if err != nil {
					return
				}
				var req protocol.RelayRequest
				_ = json.Unmarshal(line, &req)
				reply := func(r protocol.RelayResponse) {
					b, _ := json.Marshal(r)
					_, _ = conn.Write(append(b, '\n'))
				}
				if !forwarded[req.Port] {
					reply(protocol.RelayResponse{Error: "not forwarded", NotForwarded: true})
					return
				}
				backend, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(req.Port)))
				if err != nil {
					reply(protocol.RelayResponse{Error: err.Error()})
					return
				}
				defer backend.Close()
				reply(protocol.RelayResponse{OK: true})
				tailnet.Splice(conn, reader, backend, backend)
			}()
		}
	}()
	return ln.Addr().String()
}

// echoServer listens on a free loopback port and answers "got:" plus what it
// read. It doesn't echo verbatim, so a relay that loops a client's bytes back
// to it can't pass for a working one.
func echoServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				in, _ := io.ReadAll(conn)
				_, _ = conn.Write(append([]byte("got:"), in...))
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// freePort returns a loopback port nothing is listening on right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newTestTailnet(t *testing.T, relayAddr string, lease time.Duration) (*TailnetForwarder, Target) {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(relayAddr)
	port, _ := strconv.Atoi(portStr)
	f := NewTailnet(slog.New(slog.NewTextHandler(io.Discard, nil)), port, lease)
	f.bindAddrs = []string{"127.0.0.1"} // keep tests off ::1, which CI may lack
	t.Cleanup(func() {
		f.mu.Lock()
		for _, fwd := range f.forwards {
			f.closeLocked(fwd)
		}
		f.mu.Unlock()
	})
	return f, Target{ConnectionInfo: "foxtrotbase", Address: host}
}

func roundTrip(t *testing.T, port int, msg string) (string, error) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte(msg)); err != nil {
		return "", err
	}
	conn.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(conn)
	return string(got), err
}

func TestTailnetForwardRelays(t *testing.T) {
	backend := echoServer(t)
	f, target := newTestTailnet(t, fakeRelay(t, map[int]bool{backend: true}), time.Minute)

	// The backend already holds its port on this loopback, so forward it to a
	// different local port, the way it would arrive from another machine.
	local := freePort(t)
	results, err := f.AddForwards(target, []Port{{RemotePort: backend, LocalPort: local}})
	if err != nil || results[0].Err != nil || !results[0].Created {
		t.Fatalf("AddForwards = %+v, %v", results, err)
	}

	got, err := roundTrip(t, local, "hello over the tailnet")
	if err != nil || got != "got:hello over the tailnet" {
		t.Fatalf("round trip = %q, %v", got, err)
	}

	again, _ := f.AddForwards(target, []Port{{RemotePort: backend, LocalPort: local}})
	if again[0].Created || again[0].Err != nil {
		t.Errorf("re-adding an existing forward = %+v, want not created and no error", again[0])
	}
}

func TestTailnetBusyPort(t *testing.T) {
	f, target := newTestTailnet(t, fakeRelay(t, nil), time.Minute)
	taken := echoServer(t) // something local already listens here

	results, _ := f.AddForwards(target, []Port{{RemotePort: taken}})
	if !errors.Is(results[0].Err, ErrPortBusy) {
		t.Errorf("forwarding a taken port: err = %v, want ErrPortBusy", results[0].Err)
	}

	// A second peer asking for a port the first already forwards is busy too.
	port := freePort(t)
	if r, _ := f.AddForwards(target, []Port{{RemotePort: port}}); r[0].Err != nil {
		t.Fatal(r[0].Err)
	}
	other := Target{ConnectionInfo: "hodag", Address: target.Address}
	results, _ = f.AddForwards(other, []Port{{RemotePort: port}})
	if !errors.Is(results[0].Err, ErrPortBusy) {
		t.Errorf("forwarding a port another peer holds: err = %v, want ErrPortBusy", results[0].Err)
	}
}

func TestTailnetSyncRemovesUnlisted(t *testing.T) {
	f, target := newTestTailnet(t, fakeRelay(t, nil), time.Minute)
	a, b := freePort(t), freePort(t)
	if _, err := f.Sync(target, []Port{{RemotePort: a}, {RemotePort: b}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Sync(target, []Port{{RemotePort: b}}); err != nil {
		t.Fatal(err)
	}
	forwards := f.ListForwards()
	if len(forwards) != 1 || forwards[0].RemotePort != b {
		t.Errorf("after syncing only %d, forwards = %+v", b, forwards)
	}
	if _, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(a))); err == nil {
		t.Errorf("port %d still listening after it was synced away", a)
	}
}

func TestTailnetLeaseExpires(t *testing.T) {
	f, target := newTestTailnet(t, fakeRelay(t, nil), 20*time.Millisecond)
	if _, err := f.AddForwards(target, []Port{{RemotePort: freePort(t)}}); err != nil {
		t.Fatal(err)
	}
	f.expire()
	if n := len(f.ListForwards()); n != 1 {
		t.Fatalf("forwards before the lease ran out = %d, want 1", n)
	}
	time.Sleep(30 * time.Millisecond)
	f.expire()
	if n := len(f.ListForwards()); n != 0 {
		t.Errorf("forwards after the lease ran out = %d, want 0", n)
	}
}

func TestTailnetDropsWhenRelayNotForwarding(t *testing.T) {
	f, target := newTestTailnet(t, fakeRelay(t, nil), time.Minute)
	port := freePort(t)
	if _, err := f.AddForwards(target, []Port{{RemotePort: port}}); err != nil {
		t.Fatal(err)
	}
	_, _ = roundTrip(t, port, "anyone home?")

	deadline := time.Now().Add(2 * time.Second)
	for len(f.ListForwards()) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("forward still present after the relay said it isn't forwarding the port")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
