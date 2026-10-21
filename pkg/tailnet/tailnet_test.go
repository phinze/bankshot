package tailnet

import (
	"context"
	"errors"
	"testing"
)

func TestGate(t *testing.T) {
	ids := map[string]Identity{
		"100.0.0.1": {Login: "me@github", Node: "laptop"},
		"100.0.0.2": {Login: "tagged-devices", Node: "foxtrotbase", Tags: []string{"tag:terraform"}},
		"100.0.0.3": {Login: "someone@github", Node: "theirs"},
		"100.0.0.4": {Login: "tagged-devices", Node: "printer", Tags: []string{"tag:iot"}},
	}
	calls := 0
	whois := func(_ context.Context, ip string) (Identity, error) {
		calls++
		id, ok := ids[ip]
		if !ok {
			return Identity{}, errors.New("no such peer")
		}
		return id, nil
	}
	g := NewGate([]string{"me@github", "foxtrotbase", "tagged-devices"}, whois, false)
	for addr, want := range map[string]string{
		"100.0.0.1:5000": "laptop",      // by login
		"100.0.0.2:5000": "foxtrotbase", // tagged, by node name
		"100.0.0.3:5000": "",            // someone else
		"100.0.0.4:5000": "",            // tagged, and "tagged-devices" on the list names nobody
		"100.0.0.9:5000": "",            // whois can't place it
		"127.0.0.1:5000": "",            // loopback, on a tailnet listener
	} {
		id, err := g.Check(context.Background(), addr)
		if (err == nil) != (want != "") || id.Node != want {
			t.Errorf("%s: got node %q err %v, want node %q", addr, id.Node, err, want)
		}
	}

	before := calls
	_, _ = g.Check(context.Background(), "100.0.0.1:6000")
	_, _ = g.Check(context.Background(), "100.0.0.3:6000")
	if calls != before {
		t.Errorf("whois ran %d more times for addresses already answered, want 0", calls-before)
	}
	_, _ = g.Check(context.Background(), "100.0.0.9:6000")
	if calls != before+1 {
		t.Errorf("a failed whois was cached; want it asked again")
	}
}

func TestGateLoopback(t *testing.T) {
	whois := func(context.Context, string) (Identity, error) {
		t.Fatal("whois called for a loopback peer")
		return Identity{}, nil
	}
	g := NewGate(nil, whois, true)
	for _, addr := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		id, err := g.Check(context.Background(), addr)
		if err != nil || id.Node != LoopbackNode {
			t.Errorf("%s: got %+v, %v; want %q admitted", addr, id, err, LoopbackNode)
		}
	}
}
