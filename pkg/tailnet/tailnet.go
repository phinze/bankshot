// Package tailnet identifies Tailscale peers and decides which ones bankshot
// will talk to. It shells out to the tailscale CLI rather than linking
// tailscale's client, the same trade rig makes for `rig serve`.
package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Identity is who a tailnet address belongs to.
type Identity struct {
	Login string   // the owning user's login, "tagged-devices" for a tagged node
	Node  string   // the node's short name, e.g. "foxtrotbase"
	Tags  []string // the node's ACL tags, tag:NAME
}

// WhoisFunc reports who owns a tailnet IP.
type WhoisFunc func(ctx context.Context, ip string) (Identity, error)

// cliFallbacks are where the tailscale CLI lives when it isn't on PATH, as
// under launchd, whose agents get only the system directories: Homebrew on
// Apple silicon and Intel, then the Mac app's bundled binary.
var cliFallbacks = []string{
	"/opt/homebrew/bin/tailscale",
	"/usr/local/bin/tailscale",
	"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
}

// CLI returns the tailscale command to run: the one on PATH, else the first
// well-known install that exists, else plain "tailscale" so the error names it.
func CLI() string {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}
	for _, path := range cliFallbacks {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "tailscale"
}

// Whois asks the local tailscaled who an address is.
func Whois(ctx context.Context, ip string) (Identity, error) {
	out, err := exec.CommandContext(ctx, CLI(), "whois", "--json", ip).Output()
	if err != nil {
		return Identity{}, fmt.Errorf("tailscale whois %s: %w", ip, err)
	}
	var raw struct {
		Node struct {
			Name string
			Tags []string
		}
		UserProfile struct{ LoginName string }
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Identity{}, fmt.Errorf("tailscale whois %s: %w", ip, err)
	}
	node, _, _ := strings.Cut(raw.Node.Name, ".")
	return Identity{Login: raw.UserProfile.LoginName, Node: node, Tags: raw.Node.Tags}, nil
}

// IPv4 returns this machine's tailnet IPv4 address.
func IPv4(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, CLI(), "ip", "-4").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale ip -4: %w", err)
	}
	ip, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if ip == "" {
		return "", errors.New("tailscale reported no IPv4 address")
	}
	return ip, nil
}

// Gate decides whether a peer may talk to us, and says who it is. Answers
// are cached per address for a few minutes since each whois is a process; a
// failed whois is never cached, so a hiccup doesn't lock anyone out for longer
// than one request.
type Gate struct {
	allow    map[string]bool
	whois    WhoisFunc
	loopback bool // accept loopback peers, for tests and local experiments

	mu    sync.Mutex
	known map[string]gateEntry
}

type gateEntry struct {
	id Identity
	ok bool
	at time.Time
}

const gateTTL = 5 * time.Minute

// LoopbackNode is the node name a loopback peer gets when the gate admits
// loopback.
const LoopbackNode = "localhost"

// NewGate builds a gate admitting peers whose login, node name, or tag is in
// allow. With loopback set, loopback peers are admitted as LoopbackNode
// without asking tailscale; a gate on a tailnet listener leaves it off.
func NewGate(allow []string, whois WhoisFunc, loopback bool) *Gate {
	g := &Gate{allow: map[string]bool{}, whois: whois, loopback: loopback, known: map[string]gateEntry{}}
	for _, a := range allow {
		g.allow[a] = true
	}
	return g
}

// Check identifies the peer at remoteAddr (host:port) and returns its
// identity if it's allowed, or an error if it isn't or can't be identified.
func (g *Gate) Check(ctx context.Context, remoteAddr string) (Identity, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return Identity{}, fmt.Errorf("unreadable peer address %q", remoteAddr)
	}
	if g.loopback {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return Identity{Node: LoopbackNode}, nil
		}
	}
	g.mu.Lock()
	e, ok := g.known[host]
	g.mu.Unlock()
	if !ok || time.Since(e.at) >= gateTTL {
		id, err := g.whois(ctx, host)
		if err != nil {
			return Identity{}, fmt.Errorf("can't identify %s: %w", host, err)
		}
		e = gateEntry{id: id, ok: g.allows(id), at: time.Now()}
		g.mu.Lock()
		g.known[host] = e
		g.mu.Unlock()
	}
	if !e.ok {
		return Identity{}, fmt.Errorf("%s (%s) is not allowed", host, e.id.Node)
	}
	return e.id, nil
}

// allows reports whether the allow list names the peer. A tagged node's login
// is the placeholder "tagged-devices", which names no one, so it never
// matches; a tagged peer has to be allowed by node name or tag.
func (g *Gate) allows(id Identity) bool {
	if id.Login != "" && id.Login != "tagged-devices" && g.allow[id.Login] {
		return true
	}
	if id.Node != "" && g.allow[id.Node] {
		return true
	}
	for _, t := range id.Tags {
		if g.allow[t] {
			return true
		}
	}
	return false
}
