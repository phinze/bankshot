package sshagent

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// relayed puts Relay between a client and a real in-memory agent holding one
// key, and reports how many signs and refusals it saw.
func relayed(t *testing.T) (agent.ExtendedAgent, *ssh.PublicKey, func() (signs, refused int)) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	pub := signer.PublicKey()

	clientSide, relayClient := net.Pipe()
	relayAgent, agentSide := net.Pipe()
	go func() { _ = agent.ServeAgent(keyring, agentSide) }()

	var mu sync.Mutex
	var signs, refused int
	go func() {
		_ = Relay(relayClient, relayAgent, Hooks{
			Sign:    func() { mu.Lock(); signs++; mu.Unlock() },
			Refused: func(byte) { mu.Lock(); refused++; mu.Unlock() },
		})
		relayClient.Close()
		relayAgent.Close()
	}()
	t.Cleanup(func() { clientSide.Close() })
	return agent.NewClient(clientSide), &pub, func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return signs, refused
	}
}

func TestRelayListsAndSigns(t *testing.T) {
	client, pub, counts := relayed(t)

	keys, err := client.List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("List = %v, %v; want one key", keys, err)
	}
	sig, err := client.Sign(*pub, []byte("data"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := (*pub).Verify([]byte("data"), sig); err != nil {
		t.Errorf("signature doesn't verify: %v", err)
	}
	if signs, refused := counts(); signs != 1 || refused != 0 {
		t.Errorf("signs, refused = %d, %d; want 1, 0", signs, refused)
	}
}

func TestRelayRefusesAgentChanges(t *testing.T) {
	client, _, counts := relayed(t)

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := client.Add(agent.AddedKey{PrivateKey: priv}); err == nil {
		t.Error("Add went through")
	}
	if err := client.RemoveAll(); err == nil {
		t.Error("RemoveAll went through")
	}
	if err := client.Lock([]byte("pw")); err == nil {
		t.Error("Lock went through")
	}
	if _, err := client.Extension("query", nil); err == nil {
		t.Error("an arbitrary extension went through")
	}
	if keys, err := client.List(); err != nil || len(keys) != 1 {
		t.Errorf("after refusals, List = %v, %v; want the key still there", keys, err)
	}
	if _, refused := counts(); refused != 4 {
		t.Errorf("refused = %d, want 4", refused)
	}
}

func TestAllowsOnlySessionBindExtension(t *testing.T) {
	ext := func(name string) []byte {
		return append([]byte{agentExtension}, ssh.Marshal(struct{ Name string }{name})...)
	}
	if !allowed(ext("session-bind@openssh.com")) {
		t.Error("session-bind refused")
	}
	if allowed(ext("restrict-destination-v00@openssh.com")) {
		t.Error("another extension allowed")
	}
	if allowed([]byte{agentExtension, 0, 0}) {
		t.Error("a truncated extension allowed")
	}
}
