package daemon

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/go-homedir"
	"github.com/phinze/bankshot/pkg/protocol"
	"github.com/phinze/bankshot/pkg/sshagent"
)

// agentToastEvery throttles agent notifications per caller: an scp loop
// signs once per connection, and one toast says enough.
const agentToastEvery = time.Minute

// agentToasts remembers when each caller last got an agent notification.
type agentToasts struct {
	mu   sync.Mutex
	last map[string]time.Time
}

// due reports whether caller is owed a notification now, and if so starts
// its quiet period.
func (a *agentToasts) due(caller string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.last == nil {
		a.last = make(map[string]time.Time)
	}
	if at, ok := a.last[caller]; ok && time.Since(at) < agentToastEvery {
		return false
	}
	a.last[caller] = time.Now()
	return true
}

// serveAgent answers a tailnet peer's agent request and then relays the
// connection to the local ssh agent, filtered to listing and signing. The
// first sign from a caller in a while raises a notification naming it, ahead
// of whatever prompt the agent shows.
func (d *Daemon) serveAgent(conn net.Conn, reader *bufio.Reader, req *protocol.Request, p *peer) {
	fail := func(err error) {
		d.logger.Warn("Refused agent request", "node", p.node, "error", err)
		d.sendResponse(conn, protocol.NewErrorResponse(req.ID, err))
	}
	if !d.config.SSHAgent.Enabled || d.config.SSHAgent.Socket == "" {
		fail(fmt.Errorf("ssh agent relay is not enabled"))
		return
	}
	if err := d.verifyPeerRequest(req, p); err != nil {
		fail(fmt.Errorf("agent refused: %w", err))
		return
	}
	path, err := homedir.Expand(d.config.SSHAgent.Socket)
	if err != nil {
		fail(err)
		return
	}
	upstream, err := net.Dial("unix", path)
	if err != nil {
		fail(fmt.Errorf("reach local agent: %w", err))
		return
	}
	defer upstream.Close()

	resp, _ := protocol.NewSuccessResponse(req.ID, map[string]string{"message": "agent ready"})
	d.sendResponse(conn, resp)

	origin := req.Origin.String()
	caller := strings.Join(append([]string{p.node}, req.Origin.Chain...), " ")
	d.logger.Info("Relaying ssh agent for remote caller", "origin", origin, "command", req.Origin.Command)
	// Read through reader, which may hold bytes buffered past the request.
	err = sshagent.Relay(struct {
		io.Reader
		io.Writer
	}{reader, conn}, upstream, sshagent.Hooks{
		Sign: func() {
			d.logger.Info("Agent sign for remote caller", "origin", origin, "command", req.Origin.Command)
			if d.agentToasts.due(caller) {
				d.notifier.NotifyAgent(origin, req.Origin.Command)
			}
		},
		Refused: func(msgType byte) {
			d.logger.Warn("Refused agent message from remote caller", "type", msgType, "origin", origin)
		},
	})
	if err != nil {
		d.logger.Debug("Agent relay ended", "origin", origin, "error", err)
	}
}
