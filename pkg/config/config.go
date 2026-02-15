package config

import (
	"fmt"
	"os"
	"path/filepath"

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

	// Monitor configuration (for bankshot monitor on remote servers)
	Monitor MonitorConfig `yaml:"monitor,omitempty"`
}

// MonitorConfig represents the configuration for bankshot monitor
type MonitorConfig struct {
	PortRanges      []PortRange `yaml:"portRanges,omitempty"`
	IgnoreProcesses []string    `yaml:"ignoreProcesses,omitempty"`
	PollInterval    string      `yaml:"pollInterval,omitempty"`
	GracePeriod     string      `yaml:"gracePeriod,omitempty"`
}

// PortRange defines a range of ports
type PortRange struct {
	Start int `yaml:"start"`
	End   int `yaml:"end"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Network:    "unix",
		Address:    "~/.bankshot.sock",
		LogLevel:   "info",
		SSHCommand: "ssh",
	}
}

// Load loads configuration from file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

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
