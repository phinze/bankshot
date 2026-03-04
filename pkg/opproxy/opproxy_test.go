package opproxy

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/protocol"
)

func newTestOpProxy(cfg *config.OpProxyConfig) *OpProxy {
	return New(cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

// --- Policy enforcement tests ---

func TestCheckPolicy_RejectWhenDisabled(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled: false,
	})
	err := op.checkPolicy("item get", "Dev")
	if err == nil {
		t.Fatal("expected error when disabled")
	}
	if err.Error() != "op-proxy is not enabled" {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestCheckPolicy_RejectDisallowedVault(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:       true,
		ReadOnly:      true,
		AllowedVaults: []string{"Dev"},
	})
	err := op.checkPolicy("item get", "Production")
	if err == nil {
		t.Fatal("expected error for disallowed vault")
	}
}

func TestCheckPolicy_RejectDisallowedSubcommand(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  true,
		ReadOnly: true,
	})
	err := op.checkPolicy("item delete", "Dev")
	if err == nil {
		t.Fatal("expected error for disallowed subcommand")
	}
}

func TestCheckPolicy_RejectNoVault(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  true,
		ReadOnly: true,
	})
	err := op.checkPolicy("item get", "")
	if err == nil {
		t.Fatal("expected error when no vault specified")
	}
}

func TestCheckPolicy_AllowValidRequest(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:       true,
		ReadOnly:      true,
		AllowedVaults: []string{"Dev"},
	})
	err := op.checkPolicy("item get", "Dev")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestCheckPolicy_AllowWhoamiWithoutVault(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  true,
		ReadOnly: true,
	})
	err := op.checkPolicy("whoami", "")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestCheckPolicy_AllowAllVaultsWhenEmpty(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:       true,
		ReadOnly:      true,
		AllowedVaults: []string{},
	})
	err := op.checkPolicy("item get", "AnyVault")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestCheckPolicy_ReadOnlyBlocksWriteCommands(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  true,
		ReadOnly: true,
	})
	for _, cmd := range []string{"item create", "item edit", "item delete", "run", "inject"} {
		err := op.checkPolicy(cmd, "Dev")
		if err == nil {
			t.Errorf("expected error for write command %q in read-only mode", cmd)
		}
	}
}

func TestCheckPolicy_ReadWriteAllowsWriteCommands(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  true,
		ReadOnly: false,
	})
	for _, cmd := range []string{"item create", "item edit", "item delete", "run", "inject"} {
		err := op.checkPolicy(cmd, "Dev")
		if err != nil {
			t.Errorf("unexpected error for write command %q in read-write mode: %s", cmd, err)
		}
	}
}

func TestCheckPolicy_CustomAllowedSubcommands(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:            true,
		ReadOnly:           true,
		AllowedSubcommands: []string{"read"},
	})
	// "read" should be allowed
	if err := op.checkPolicy("read", "Dev"); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// "item get" should NOT be allowed since custom list overrides defaults
	if err := op.checkPolicy("item get", "Dev"); err == nil {
		t.Fatal("expected error for non-custom subcommand")
	}
}

// --- Subcommand parsing tests ---

func TestParseSubcommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"item get", []string{"item", "get", "--vault", "Dev", "MyItem"}, "item get"},
		{"read", []string{"read", "op://Dev/item/field"}, "read"},
		{"whoami", []string{"whoami"}, "whoami"},
		{"item list", []string{"item", "list", "--vault", "Dev"}, "item list"},
		{"flags before subcommand", []string{"--cache", "item", "get"}, "item get"},
		{"single word", []string{"inject"}, "inject"},
		{"empty", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSubcommand(tt.args)
			if got != tt.want {
				t.Errorf("parseSubcommand(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// --- Vault extraction tests ---

func TestParseVault(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"--vault flag", []string{"item", "get", "--vault", "Dev", "MyItem"}, "Dev"},
		{"--vault= flag", []string{"item", "get", "--vault=Dev", "MyItem"}, "Dev"},
		{"op:// URI", []string{"read", "op://Dev/item/field"}, "Dev"},
		{"op:// URI no field", []string{"read", "op://Dev/item"}, "Dev"},
		{"no vault", []string{"item", "get", "MyItem"}, ""},
		{"empty", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseVault(tt.args)
			if got != tt.want {
				t.Errorf("parseVault(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// --- Execution tests ---

func TestExecute_PassThrough(t *testing.T) {
	// Create a fake "op" script that echoes args
	tmpDir := t.TempDir()
	fakeOp := filepath.Join(tmpDir, "fake-op")
	err := os.WriteFile(fakeOp, []byte(`#!/bin/sh
echo "hello from op"
echo "stderr output" >&2
exit 0
`), 0755)
	if err != nil {
		t.Fatalf("failed to create fake op: %s", err)
	}

	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  true,
		OpPath:   fakeOp,
		ReadOnly: true,
	})

	resp, err := op.Execute(protocol.OpProxyRequest{
		Args: []string{"whoami"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if resp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", resp.ExitCode)
	}
	if resp.Stdout != "hello from op\n" {
		t.Errorf("unexpected stdout: %q", resp.Stdout)
	}
	if resp.Stderr != "stderr output\n" {
		t.Errorf("unexpected stderr: %q", resp.Stderr)
	}
}

func TestExecute_NonZeroExit(t *testing.T) {
	tmpDir := t.TempDir()
	fakeOp := filepath.Join(tmpDir, "fake-op")
	err := os.WriteFile(fakeOp, []byte(`#!/bin/sh
echo "error: not found" >&2
exit 1
`), 0755)
	if err != nil {
		t.Fatalf("failed to create fake op: %s", err)
	}

	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  true,
		OpPath:   fakeOp,
		ReadOnly: true,
	})

	resp, err := op.Execute(protocol.OpProxyRequest{
		Args: []string{"whoami"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if resp.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", resp.ExitCode)
	}
	if resp.Stderr != "error: not found\n" {
		t.Errorf("unexpected stderr: %q", resp.Stderr)
	}
}

func TestExecute_PolicyRejection(t *testing.T) {
	op := newTestOpProxy(&config.OpProxyConfig{
		Enabled:  false,
		ReadOnly: true,
	})

	_, err := op.Execute(protocol.OpProxyRequest{
		Args: []string{"whoami"},
	})
	if err == nil {
		t.Fatal("expected error for disabled proxy")
	}
}
