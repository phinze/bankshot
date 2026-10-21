package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mitchellh/go-homedir"
	"gopkg.in/yaml.v3"
)

// Config represents the daemon configuration
type Config struct {
	// Network type: "unix" or "tcp"
	Network string `yaml:"network"`

	// Address to listen on
	// For unix: socket path (default: ~/.bankshot.sock)
	// For tcp: host:port (default: 127.0.0.1:9999)
	Address string `yaml:"address"`

	// LogLevel: debug, info, warn, error
	LogLevel string `yaml:"log_level"`

	// SSHCommand is the path to ssh binary
	SSHCommand string `yaml:"ssh_command"`

	// NotifyCommand is the path to the notification helper binary.
	// When set, desktop notifications are posted for new port forwards.
	NotifyCommand string `yaml:"notify_command,omitempty"`

	// Monitor configuration (for bankshot monitor on remote servers)
	Monitor MonitorConfig `yaml:"monitor,omitempty"`

	// OpProxy configuration (for proxying 1Password CLI requests)
	OpProxy OpProxyConfig `yaml:"op_proxy,omitempty"`

	// Tailnet configures forwarding over Tailscale instead of ssh.
	Tailnet TailnetConfig `yaml:"tailnet,omitempty"`

	// SSHAgent configures relaying ssh-agent requests from tailnet monitors
	// to an agent on this machine.
	SSHAgent SSHAgentConfig `yaml:"ssh_agent,omitempty"`
}

// SSHAgentConfig lets the daemon relay a remote host's ssh-agent requests to
// a local agent: listing keys and signing only, never adding, removing, or
// locking.
type SSHAgentConfig struct {
	Enabled bool `yaml:"enabled"`
	// Socket is the local agent, e.g. 1Password's agent.sock.
	Socket string `yaml:"socket,omitempty"`
}

// TailnetConfig configures the tailnet transport. On the daemon it opens a
// whois-gated control listener on the machine's tailnet address; on the
// monitor it starts a relay and keeps the daemon's lease alive. The monitor
// reaches the daemon through Network and Address as usual, e.g. tcp to
// "laptop:61000".
type TailnetConfig struct {
	Enabled bool `yaml:"enabled"`
	// ControlPort is where the daemon listens for monitors. Default 61000.
	ControlPort int `yaml:"control_port,omitempty"`
	// RelayPort is where the monitor listens for the daemon's relayed
	// connections. Default 61001.
	RelayPort int `yaml:"relay_port,omitempty"`
	// Allow lists the tailscale logins, node names, and tags allowed to
	// connect. A tagged node must be named by node or tag.
	Allow []string `yaml:"allow,omitempty"`
	// Lease is how long a monitor keeps its forwards without being heard
	// from. Default 6m.
	Lease string `yaml:"lease,omitempty"`
	// Heartbeat is how often the monitor syncs its forwards to renew the
	// lease. Default 2m.
	Heartbeat string `yaml:"heartbeat,omitempty"`
	// LocalSocket is where the monitor listens for this host's bankshot
	// CLI (open, xdg-open, op) and passes requests on to the daemon. The
	// CLI uses it instead of Address when tailnet is enabled. Default
	// ~/.bankshot.sock.
	LocalSocket string `yaml:"local_socket,omitempty"`
	// AgentSocket, when set, is where the monitor serves an ssh agent backed
	// by the daemon's (see SSHAgentConfig). Unset, there's none.
	AgentSocket string `yaml:"agent_socket,omitempty"`
}

// LeaseDuration parses Lease, falling back to the default.
func (t TailnetConfig) LeaseDuration() time.Duration {
	if d, err := time.ParseDuration(t.Lease); err == nil && d > 0 {
		return d
	}
	return 6 * time.Minute
}

// HeartbeatDuration parses Heartbeat, falling back to the default.
func (t TailnetConfig) HeartbeatDuration() time.Duration {
	if d, err := time.ParseDuration(t.Heartbeat); err == nil && d > 0 {
		return d
	}
	return 2 * time.Minute
}

// MonitorConfig represents the configuration for bankshot monitor
type MonitorConfig struct {
	PortRanges      []PortRange `yaml:"portRanges,omitempty"`
	IgnorePorts     []int       `yaml:"ignorePorts,omitempty"`
	IgnoreProcesses []string    `yaml:"ignoreProcesses,omitempty"`
	PollInterval    string      `yaml:"pollInterval,omitempty"`
	GracePeriod     string      `yaml:"gracePeriod,omitempty"`
	// BatchWindow coalesces a burst of newly-opened ports into a single forward
	// request (and a single rolled-up notification). Defaults to 500ms. Set to
	// "0" to forward each port immediately as it's discovered.
	BatchWindow string `yaml:"batchWindow,omitempty"`
}

// PortRange defines a range of ports
type PortRange struct {
	Start int `yaml:"start"`
	End   int `yaml:"end"`
}

// OpProxyConfig represents the configuration for proxying 1Password CLI requests
type OpProxyConfig struct {
	Enabled            bool     `yaml:"enabled"`
	OpPath             string   `yaml:"op_path,omitempty"`
	AllowedVaults      []string `yaml:"allowed_vaults"`
	ReadOnly           bool     `yaml:"read_only"`
	AllowedSubcommands []string `yaml:"allowed_subcommands,omitempty"`
	// Account pins op to one 1Password account (shorthand, sign-in address,
	// or ID), passed as OP_ACCOUNT. Requests may not name an account
	// themselves when it's set.
	Account string `yaml:"account,omitempty"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Network:    "unix",
		Address:    "~/.bankshot.sock",
		LogLevel:   "info",
		SSHCommand: "ssh",
		OpProxy: OpProxyConfig{
			Enabled:  false,
			OpPath:   "op",
			ReadOnly: true,
		},
		Tailnet: TailnetConfig{
			ControlPort: 61000,
			RelayPort:   61001,
			LocalSocket: "~/.bankshot.sock",
		},
	}
}

// EnvConfigPath names the environment variable that points bankshot at a
// config file. The nix modules set it on the services they define.
const EnvConfigPath = "BANKSHOT_CONFIG"

// Load loads configuration from path. With no path it uses $BANKSHOT_CONFIG,
// then ~/.config/bankshot/config.yaml, then the defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		path = os.Getenv(EnvConfigPath)
	}

	// If no path specified, try default locations
	if path == "" {
		home, err := homedir.Dir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}

		// Try ~/.config/bankshot/config.yaml first
		path = filepath.Join(home, ".config", "bankshot", "config.yaml")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// If not found, return default config
			return cfg, nil
		}
	}

	// Read config file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, use defaults
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate network type
	switch c.Network {
	case "unix", "tcp":
		// Valid
	default:
		return fmt.Errorf("invalid network type: %s (must be 'unix' or 'tcp')", c.Network)
	}

	// Expand home directory in address if unix socket
	if c.Network == "unix" {
		expanded, err := homedir.Expand(c.Address)
		if err != nil {
			return fmt.Errorf("failed to expand address: %w", err)
		}
		c.Address = expanded
	}

	// Validate log level
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
		// Valid
	default:
		return fmt.Errorf("invalid log level: %s", c.LogLevel)
	}

	return nil
}
