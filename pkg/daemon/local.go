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
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/phinze/bankshot/pkg/inbox"
	"github.com/phinze/bankshot/pkg/monitor"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/phinze/bankshot/pkg/tailnet"
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
	d.servePrivate(ctx, d.config.Tailnet.LocalSocket, "local bankshot commands", func(conn net.Conn, pid int) {
		d.handleLocal(conn, pid, client)
	})
}

// servePrivate listens on a unix socket at path that only this user can
// reach, 0600 and with each peer's uid checked, and hands each connection
// with its peer's pid to handle until ctx is done.
func (d *Monitor) servePrivate(ctx context.Context, path, what string, handle func(conn net.Conn, pid int)) {
	path, err := homedir.Expand(path)
	if err != nil {
		d.logger.Error("Bad socket path", "for", what, "path", path, "error", err)
		return
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			d.logger.Error("Socket path exists and isn't a socket; not replacing it", "for", what, "path", path)
			return
		}
		_ = os.Remove(path)
	}
	oldUmask := syscall.Umask(0077)
	ln, err := net.Listen("unix", path)
	syscall.Umask(oldUmask)
	if err != nil {
		d.logger.Error("Failed to listen", "for", what, "path", path, "error", err)
		return
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	d.logger.Info("Listening for "+what, "path", path)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			pid, uid, err := peerCred(conn.(*net.UnixConn))
			if err != nil || uid != os.Getuid() {
				d.logger.Warn("Rejected local connection", "for", what, "pid", pid, "uid", uid, "error", err)
				return
			}
			handle(conn, pid)
		}()
	}
}

// serveAgent serves an ssh agent on the configured socket, backed by the
// daemon's: each connection becomes an agent stream over the tailnet, held in
// flight for the relay to vouch for and attributed to its caller.
func (d *Monitor) serveAgent(ctx context.Context, client *localDaemonClient) {
	d.servePrivate(ctx, d.config.Tailnet.AgentSocket, "ssh agent clients", func(conn net.Conn, pid int) {
		req := &protocol.Request{
			ID:     uuid.New().String(),
			Type:   protocol.CommandAgent,
			Origin: monitor.DescribeOrigin(pid),
		}
		d.inFlight.add(req.ID)
		defer d.inFlight.remove(req.ID)

		_, upstream, reader, err := client.Open(req)
		if err != nil {
			d.logger.Warn("Agent unavailable", "origin", req.Origin.String(), "error", err)
			return
		}
		defer upstream.Close()
		tailnet.Splice(conn, conn, upstream, reader)
	})
}

func (d *Monitor) handleLocal(conn net.Conn, pid int, client *localDaemonClient) {

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
	case protocol.CommandOpen, protocol.CommandOpProxy, protocol.CommandClipboard:
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
	case protocol.CommandClipboardFile:
		callerID := req.ID
		req.ID = uuid.New().String()
		req.Origin = monitor.DescribeOrigin(pid)
		d.inFlight.add(req.ID)
		defer d.inFlight.remove(req.ID)
		d.logger.Info("Passing local request to daemon", "type", req.Type, "origin", req.Origin.String())

		file, err := d.receiveClipboardFile(req, client)
		if err != nil {
			reply(protocol.NewErrorResponse(callerID, err))
			return
		}
		resp, _ := protocol.NewSuccessResponse(callerID, file)
		reply(resp)
	case protocol.CommandForward:
		var fwd protocol.ForwardRequest
		if err := json.Unmarshal(req.Payload, &fwd); err != nil {
			reply(protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid forward request format: %w", err)))
			return
		}
		if fwd.Host != "" && fwd.Host != "localhost" && fwd.Host != "127.0.0.1" {
			reply(protocol.NewErrorResponse(req.ID, fmt.Errorf("tailnet forwards reach only this host's loopback, not %s", fwd.Host)))
			return
		}
		holder := 0
		if fwd.UntilExit {
			holder = pid
		}
		d.logger.Info("Manual forward", "port", fwd.RemotePort, "local", fwd.LocalPort, "origin", monitor.DescribeOrigin(pid).String())
		replyResult(reply, req.ID, d.sessionMonitor.Pin(fwd.RemotePort, fwd.LocalPort, holder))
	case protocol.CommandUnforward:
		var unfwd protocol.UnforwardRequest
		if err := json.Unmarshal(req.Payload, &unfwd); err != nil {
			reply(protocol.NewErrorResponse(req.ID, fmt.Errorf("invalid unforward request format: %w", err)))
			return
		}
		replyResult(reply, req.ID, d.sessionMonitor.Unpin(unfwd.RemotePort))
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

// clipboardFileTimeout bounds receiving a clipboard file, enough for the
// biggest one the daemon sends over a slow link.
const clipboardFileTimeout = 5 * time.Minute

// receiveClipboardFile asks the daemon for its clipboard's image or file and
// saves what comes back in this host's inbox, answering with where.
func (d *Monitor) receiveClipboardFile(req *protocol.Request, client *localDaemonClient) (*protocol.ClipboardFileResponse, error) {
	resp, upstream, reader, err := client.Open(req)
	if err != nil {
		return nil, err
	}
	defer upstream.Close()
	var file protocol.ClipboardFileResponse
	if err := json.Unmarshal(resp.Data, &file); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if file.Name == "" {
		return &file, nil
	}
	_ = upstream.SetDeadline(time.Now().Add(clipboardFileTimeout))
	if file.Path, err = inbox.Save(file.Name, reader, file.Size); err != nil {
		return nil, err
	}
	d.logger.Info("Saved clipboard file", "path", file.Path, "bytes", file.Size)
	return &file, nil
}

// replyResult replies to request id with success, or with err if it failed.
func replyResult(reply func(*protocol.Response), id string, err error) {
	if err != nil {
		reply(protocol.NewErrorResponse(id, err))
		return
	}
	resp, _ := protocol.NewSuccessResponse(id, nil)
	reply(resp)
}
