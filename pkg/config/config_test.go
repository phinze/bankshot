package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Network != "unix" {
		t.Errorf("DefaultConfig() Network = %v, want %v", cfg.Network, "unix")
	}
	if cfg.Address != "~/.bankshot.sock" {
		t.Errorf("DefaultConfig() Address = %v, want %v", cfg.Address, "~/.bankshot.sock")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("DefaultConfig() LogLevel = %v, want %v", cfg.LogLevel, "info")
	}
	if cfg.SSHCommand != "ssh" {
		t.Errorf("DefaultConfig() SSHCommand = %v, want %v", cfg.SSHCommand, "ssh")
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *Config
		wantErr bool
	}{
		{
			name: "valid config",
			content: `network: tcp
address: 127.0.0.1:8888
log_level: debug
ssh_command: /usr/bin/ssh`,
			want: &Config{
				Network:    "tcp",
				Address:    "127.0.0.1:8888",
				LogLevel:   "debug",
				SSHCommand: "/usr/bin/ssh",
			},
			wantErr: false,
		},
		{
			name: "partial config",
			content: `network: tcp
log_level: warn`,
			want: &Config{
				Network:    "tcp",
				Address:    "~/.bankshot.sock",
				LogLevel:   "warn",
				SSHCommand: "ssh",
			},
			wantErr: false,
		},
		{
			name:    "empty config",
			content: "",
			want:    DefaultConfig(),
			wantErr: false,
		},
		{
			name:    "invalid yaml",
			content: "network: [invalid yaml",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "config.yaml")

			if tt.content != "" {
				if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
					t.Fatalf("Failed to write test config: %v", err)
				}
			}

			got, err := Load(tmpFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != nil {
				if got.Network != tt.want.Network {
					t.Errorf("Load() Network = %v, want %v", got.Network, tt.want.Network)
				}
				if got.LogLevel != tt.want.LogLevel {
					t.Errorf("Load() LogLevel = %v, want %v", got.LogLevel, tt.want.LogLevel)
				}
				if got.SSHCommand != tt.want.SSHCommand {
					t.Errorf("Load() SSHCommand = %v, want %v", got.SSHCommand, tt.want.SSHCommand)
				}
			}
		})
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	// Test with explicit non-existent path
	cfg, err := Load("/non/existent/path/config.yaml")
	if err != nil {
		t.Errorf("Load() with non-existent file should return default config, got error: %v", err)
	}
	if cfg == nil {
		t.Errorf("Load() with non-existent file should return default config, got nil")
	}

	// Test with empty path (default locations)
	cfg, err = Load("")
	if err != nil {
		t.Errorf("Load() with empty path should return default config, got error: %v", err)
	}
	if cfg == nil {
		t.Errorf("Load() with empty path should return default config, got nil")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid unix config",
			config: &Config{
				Network:    "unix",
				Address:    "~/.bankshot.sock",
				LogLevel:   "info",
				SSHCommand: "ssh",
			},
			wantErr: false,
		},
		{
			name: "valid tcp config",
			config: &Config{
				Network:    "tcp",
				Address:    "127.0.0.1:9999",
				LogLevel:   "debug",
				SSHCommand: "ssh",
			},
			wantErr: false,
		},
		{
			name: "invalid network type",
			config: &Config{
				Network:    "udp",
				Address:    "127.0.0.1:9999",
				LogLevel:   "info",
				SSHCommand: "ssh",
			},
			wantErr: true,
			errMsg:  "invalid network type: udp",
		},
		{
			name: "invalid log level",
			config: &Config{
				Network:    "unix",
				Address:    "~/.bankshot.sock",
				LogLevel:   "verbose",
				SSHCommand: "ssh",
			},
			wantErr: true,
			errMsg:  "invalid log level: verbose",
		},
		{
			name: "all log levels",
			config: &Config{
				Network:    "unix",
				Address:    "~/.bankshot.sock",
				LogLevel:   "debug",
				SSHCommand: "ssh",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if err.Error() != tt.errMsg && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestValidateLogLevels(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error"}

	for _, level := range validLevels {
		cfg := &Config{
			Network:    "unix",
			Address:    "~/.bankshot.sock",
			LogLevel:   level,
			SSHCommand: "ssh",
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with log level %s should not error, got: %v", level, err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}
