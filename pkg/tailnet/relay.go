package tailnet

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/phinze/bankshot/pkg/protocol"
)

// Relay is the monitor's end of a tailnet forward. The daemon dials it, names
// a port in a one-line protocol.RelayRequest, and once the relay answers OK the
// connection carries bytes to and from that port on this machine's loopback.
type Relay struct {
	Gate   *Gate
	Logger *slog.Logger
	// Forwarding reports whether the monitor is forwarding port. The relay
	// reaches no other port, so it can't be used to probe this machine.
	Forwarding func(port int) bool
}

const relayTimeout = 5 * time.Second

// Serve accepts relay connections on ln until it's closed.
func (r *Relay) Serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go r.handle(conn)
	}
}

func (r *Relay) handle(conn net.Conn) {
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), relayTimeout)
	defer cancel()
	id, err := r.Gate.Check(ctx, conn.RemoteAddr().String())
	if err != nil {
		r.Logger.Warn("Rejected relay connection", "remote", conn.RemoteAddr(), "error", err)
		return
	}

	_ = conn.SetDeadline(time.Now().Add(relayTimeout))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}
	reply := func(resp protocol.RelayResponse) error {
		b, _ := json.Marshal(resp)
		_, err := conn.Write(append(b, '\n'))
		return err
	}
	var req protocol.RelayRequest
	if err := json.Unmarshal(line, &req); err != nil {
		_ = reply(protocol.RelayResponse{Error: "invalid relay request"})
		return
	}
	if !r.Forwarding(req.Port) {
		r.Logger.Info("Relay refused port it isn't forwarding", "node", id.Node, "port", req.Port)
		_ = reply(protocol.RelayResponse{Error: "port is not forwarded", NotForwarded: true})
		return
	}

	backend, err := dialLoopback(ctx, req.Port)
	if err != nil {
		_ = reply(protocol.RelayResponse{Error: err.Error()})
		return
	}
	defer backend.Close()
	if err := reply(protocol.RelayResponse{OK: true}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	Splice(conn, reader, backend, backend)
}

// dialLoopback reaches port on IPv4 loopback, or IPv6 for servers that bound
// only ::1.
func dialLoopback(ctx context.Context, port int) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil {
		return conn, nil
	}
	if conn6, err6 := d.DialContext(ctx, "tcp", net.JoinHostPort("::1", strconv.Itoa(port))); err6 == nil {
		return conn6, nil
	}
	return nil, err
}
