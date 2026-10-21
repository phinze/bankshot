package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/protocol"
)

// inFlight is the set of requests the monitor is passing to the daemon right
// now. The relay vouches for exactly these.
type inFlight struct {
	mu  sync.Mutex
	ids map[string]bool
}

func (f *inFlight) add(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ids == nil {
		f.ids = make(map[string]bool)
	}
	f.ids[id] = true
}

func (f *inFlight) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.ids, id)
}

func (f *inFlight) has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ids[id]
}

// serveLocal listens on the local socket for this host's bankshot CLI and
// passes its requests to the daemon over the tailnet. Only the monitor's own
// user gets in: the socket is 0600 and each peer's uid is checked.
func (d *Monitor) serveLocal(ctx context.Context, client *localDaemonClient) {
	path, err := homedir.Expand(d.config.Tailnet.LocalSocket)
	if err != nil {
		d.logger.Error("Bad local socket path", "path", d.config.Tailnet.LocalSocket, "error", err)
		return
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			d.logger.Error("Local socket path exists and isn't a socket; not replacing it", "path", path)
			return
		}
		_ = os.Remove(path)
	}
	oldUmask := syscall.Umask(0077)
	ln, err := net.Listen("unix", path)
	syscall.Umask(oldUmask)
	if err != nil {
		d.logger.Error("Failed to listen on local socket", "path", path, "error", err)
		return
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	d.logger.Info("Listening for local bankshot commands", "path", path)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go d.handleLocal(conn, client)
	}
}

func (d *Monitor) handleLocal(conn net.Conn, client *localDaemonClient) {
	defer conn.Close()

	pid, uid, err := peerCred(conn.(*net.UnixConn))
	if err != nil || uid != os.Getuid() {
		d.logger.Warn("Rejected local connection", "pid", pid, "uid", uid, "error", err)
		return
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	reply := func(resp *protocol.Response) {
		b, _ := json.Marshal(resp)
		_, _ = conn.Write(append(b, '\n'))
	}
	req, err := protocol.ParseRequest(line)
	if err != nil {
		reply(protocol.NewErrorResponse("", fmt.Errorf("invalid request format")))
		return
	}

	switch req.Type {
	case protocol.CommandOpen, protocol.CommandOpProxy:
		// A fresh ID only this monitor knows, held in flight while the
		// daemon asks the relay to vouch for it.
		callerID := req.ID
		req.ID = uuid.New().String()
		req.Origin = monitor.DescribeOrigin(pid)
		d.inFlight.add(req.ID)
		defer d.inFlight.remove(req.ID)
		d.logger.Info("Passing local request to daemon", "type", req.Type, "origin", req.Origin.String())

		resp, err := client.SendRequest(req)
		if err != nil {
			reply(protocol.NewErrorResponse(callerID, err))
			return
		}
		resp.ID = callerID
		reply(resp)
	case protocol.CommandStatus, protocol.CommandList:
		resp, err := client.SendRequest(req)
		if err != nil {
			reply(protocol.NewErrorResponse(req.ID, err))
			return
		}
		reply(resp)
	default:
		reply(protocol.NewErrorResponse(req.ID, fmt.Errorf("%s isn't available on a tailnet host yet", req.Type)))
	}
}
