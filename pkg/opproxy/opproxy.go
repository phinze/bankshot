package opproxy

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/phinze/bankshot/pkg/config"
	"github.com/phinze/bankshot/pkg/protocol"
)

// Default read-only subcommands that are always allowed.
var defaultReadOnlySubcommands = []string{
	"item get",
	"item list",
	"read",
	"whoami",
}

// Additional subcommands allowed when read_only is false.
var writeSubcommands = []string{
	"item create",
	"item edit",
	"item delete",
	"run",
	"inject",
}

// OpProxy handles policy enforcement and execution of proxied op CLI requests.
type OpProxy struct {
	config *config.OpProxyConfig
	logger *slog.Logger
}

// New creates a new OpProxy instance.
func New(cfg *config.OpProxyConfig, logger *slog.Logger) *OpProxy {
	return &OpProxy{
		config: cfg,
		logger: logger,
	}
}

// Execute validates the request against policy and runs the op CLI command.
func (o *OpProxy) Execute(req protocol.OpProxyRequest) (*protocol.OpProxyResponse, error) {
	subcommand := parseSubcommand(req.Args)
	vault := parseVault(req.Args)

	if err := o.checkPolicy(subcommand, vault); err != nil {
		return nil, err
	}

	o.logger.Info("Executing op-proxy request",
		"subcommand", subcommand,
		"vault", vault,
		"args", req.Args,
	)

	opPath := o.config.OpPath
	if opPath == "" {
		opPath = "op"
	}

	cmd := exec.Command(opPath, req.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute op: %w", err)
		}
	}

	return &protocol.OpProxyResponse{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, nil
}

// checkPolicy enforces the op-proxy policy: enabled, vault allowlist, subcommand allowlist.
func (o *OpProxy) checkPolicy(subcommand, vault string) error {
	if !o.config.Enabled {
		return fmt.Errorf("op-proxy is not enabled")
	}

	if subcommand == "" {
		return fmt.Errorf("could not determine op subcommand")
	}

	if !o.isSubcommandAllowed(subcommand) {
		return fmt.Errorf("subcommand %q is not allowed", subcommand)
	}

	// Vault is required unless the subcommand doesn't use vaults
	if !subcommandSkipsVault(subcommand) {
		if vault == "" {
			return fmt.Errorf("vault must be specified (use --vault or op:// URI)")
		}
		if !o.isVaultAllowed(vault) {
			return fmt.Errorf("vault %q is not in the allowed list", vault)
		}
	}

	return nil
}

// isSubcommandAllowed checks whether the subcommand is permitted by policy.
func (o *OpProxy) isSubcommandAllowed(subcommand string) bool {
	allowed := o.effectiveAllowedSubcommands()
	for _, a := range allowed {
		if strings.EqualFold(a, subcommand) {
			return true
		}
	}
	return false
}

// effectiveAllowedSubcommands returns the full list of allowed subcommands,
// considering custom config and read-only mode.
func (o *OpProxy) effectiveAllowedSubcommands() []string {
	if len(o.config.AllowedSubcommands) > 0 {
		return o.config.AllowedSubcommands
	}

	allowed := make([]string, len(defaultReadOnlySubcommands))
	copy(allowed, defaultReadOnlySubcommands)

	if !o.config.ReadOnly {
		allowed = append(allowed, writeSubcommands...)
	}

	return allowed
}

// isVaultAllowed checks whether the vault is in the allowed list.
// If no allowed vaults are configured, all vaults are allowed.
func (o *OpProxy) isVaultAllowed(vault string) bool {
	if len(o.config.AllowedVaults) == 0 {
		return true
	}
	for _, v := range o.config.AllowedVaults {
		if strings.EqualFold(v, vault) {
			return true
		}
	}
	return false
}

// subcommandSkipsVault returns true for subcommands that don't operate on a vault.
func subcommandSkipsVault(subcommand string) bool {
	switch subcommand {
	case "whoami":
		return true
	default:
		return false
	}
}
