# bankshot

A daemon-based tool for opening URLs and managing SSH port forwards from remote development environments.

## Overview

`bankshot` solves the remote development workflow problem where you need to:
- Open URLs in your local browser from SSH sessions
- Forward ports dynamically for OAuth flows and development servers
- Replace manual SSH port forwarding with automatic, on-demand forwarding

It consists of:
- **bankshotd**: A lightweight daemon that runs on your local machine
- **bankshot**: A CLI client that communicates with the daemon from remote SSH sessions

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Usage Examples](#usage-examples)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Contributing](#contributing)
- [Acknowledgements](#acknowledgements)
- [License](#license)

## Features

- 🌐 **URL Opening** - Open URLs in your local browser from any SSH session
- 🚀 **Dynamic Port Forwarding** - Forward ports on-demand without restarting SSH
- 🔒 **Secure** - Unix socket with user-only permissions
- 🎯 **Drop-in Replacement** - Works as a replacement for the `open` command
- 📊 **Status Monitoring** - Track active forwards and daemon status
- 👥 **Multi-Session Support** - Manage forwards across multiple SSH connections
- 📈 **Live Monitoring** - Real-time status updates with `monitor` command
- ⚙️ **Configuration Management** - View and verify daemon configuration
- 🔄 **Automatic Port Detection** - Auto-forward ports with the `wrap` command

## Installation

### macOS (Homebrew)

```bash
brew tap phinze/tap
brew install phinze/tap/bankshot
brew services start phinze/tap/bankshot
```

### Linux/Unix (From Source)

```bash
# Clone the repository
git clone https://github.com/phinze/bankshot
cd bankshot

# Build binaries
make build

# Install (requires sudo)
sudo make install

# Start the daemon
bankshotd
```

### Manual Installation

```bash
# Build
go build -o bankshotd ./cmd/bankshotd
go build -o bankshot ./cmd/bankshot

# Install
sudo cp bankshotd bankshot /usr/local/bin/

# Create systemd service (Linux)
sudo tee /etc/systemd/system/bankshotd.service <<EOF
[Unit]
Description=Bankshot Daemon
After=network.target

[Service]
Type=simple
User=$USER
ExecStart=/usr/local/bin/bankshotd
Restart=on-failure

[Install]
WantedBy=default.target
EOF

sudo systemctl enable bankshotd
sudo systemctl start bankshotd
```

See [docs/INSTALL.md](docs/INSTALL.md) for detailed installation instructions.

## Quick Start

### 1. Configure SSH

Add to your `~/.ssh/config`:

```
Host *
    ControlMaster auto
    ControlPath /tmp/ssh-%r@%h:%p
    ControlPersist 10m
    RemoteForward ~/.bankshot.sock ~/.bankshot.sock
```

### 2. Use from Remote Sessions

```bash
# Open URL in local browser
bankshot open https://github.com

# Forward a port
bankshot forward 8080

# Remove a port forward
bankshot unforward 8080

# Check status
bankshot status

# List active forwards
bankshot list

# Monitor status continuously
bankshot monitor

# View configuration
bankshot config

# Wrap a command to auto-forward its ports
bankshot wrap -- npm run dev
```

### 3. Compatibility Mode

Create an alias to replace the `open` command:

```bash
# Add to your shell profile (.bashrc, .zshrc, etc.)
alias open='bankshot open'
```

## Usage Examples

### OAuth Flow

When a tool needs browser authentication:

```bash
# On remote server
$ some-oauth-tool login
OAuth URL: http://localhost:8080/callback
# Tool hangs waiting for callback...

# In another terminal on remote server
$ bankshot forward 8080
Port forward created: 8080 -> 8080

# Now the OAuth flow completes in your local browser
```

### Development Server

Forward your dev server to access it locally:

```bash
# On remote server
$ npm run dev
Server running on http://localhost:3000

# In another terminal
$ bankshot forward 3000
Port forward created: 3000 -> 3000

# Access http://localhost:3000 in your local browser
```

### Custom Port Mapping

Forward to a different local port:

```bash
# Forward remote 8080 to local 9090
$ bankshot forward 8080:9090
Port forward created: 8080 -> 9090
```

### Opening URLs

```bash
# Open project documentation
bankshot open https://docs.myproject.com

# Open local development server
bankshot open http://localhost:8080

# Open file (on local machine)
bankshot open file:///path/to/document.pdf
```

### Automatic Port Forwarding

Wrap any command to automatically forward ports it opens:

```bash
# Development server with auto-forwarding
$ bankshot wrap -- npm run dev
Starting wrapped process: npm run dev
Process started with PID: 12345
Auto-forwarded port 3000
Auto-forwarded port 3001

# Python HTTP server
$ bankshot wrap -- python -m http.server 8888
Starting wrapped process: python -m http.server 8888
Auto-forwarded port 8888

# Rails development server
$ bankshot wrap -- rails server
Auto-forwarded port 3000
```

## Configuration

### Daemon Configuration

The daemon can be configured via `~/.config/bankshot/config.yaml`:

```yaml
# Network type: "unix" or "tcp"
network: unix

# Address to listen on
# For unix: socket path (default: ~/.bankshot.sock)
# For tcp: host:port (default: 127.0.0.1:9999)
address: ~/.bankshot.sock

# Log level: debug, info, warn, error
log_level: info

# SSH command path
ssh_command: ssh
```

### Environment Variables

- `BANKSHOT_SOCKET`: Override socket path
- `BANKSHOT_DEBUG`: Enable debug logging
- `BANKSHOT_QUIET`: Suppress non-error output
- `BANKSHOT_LOG_FORMAT`: Set to `json` for JSON logs

### SSH Configuration

For optimal performance, configure SSH ControlMaster:

```
Host *
    # Enable connection multiplexing
    ControlMaster auto
    ControlPath /tmp/ssh-%r@%h:%p
    ControlPersist 10m
    
    # Forward the bankshot socket
    RemoteForward ~/.bankshot.sock ~/.bankshot.sock
    
    # Optional: Compression for slow connections
    Compression yes
```

## Architecture

```
┌────────────────────┐                 ┌────────────────────┐
│   Local Machine    │                 │  Remote Machine    │
│                    │                 │                    │
│ ┌────────────────┐ │                 │ ┌────────────────┐ │
│ │   Web Browser  │ │                 │ │ bankshot (CLI) │ │
│ └─▲──────────────┘ │                 │ └─┬──────────────┘ │
│   │ Open URL       │                 │   │ Send command   │
│ ┌─┴──────────────┐ │                 │   │                │
│ │bankshot daemon │ │                 │   │                │
│ │ - URL opener   │ │                 │   │                │
│ │ - Port forward │ │                 │   │                │
│ └─┬──────────────┘ │                 │   │                │
│   │                │                 │   │                │
│ ┌─┴──────────────┐ │ SSH connection  │ ┌─▼──────────────┐ │
│ │~/.bankshot.sock│ ├─────────────────► │~/.bankshot.sock│ │
│ └────────────────┘ │ Remote forward  │ └────────────────┘ │
│                    │                 │                    │
└────────────────────┘                 └────────────────────┘
```

### Components

1. **Daemon (bankshotd)**
   - Listens on Unix socket or TCP port
   - Handles URL opening via system browser
   - Manages SSH port forwarding
   - Tracks active connections and forwards

2. **CLI Client (bankshot)**
   - Sends commands to daemon over socket
   - Provides user-friendly interface
   - Monitors port usage for wrapped commands

3. **Protocol**
   - JSON-based communication
   - Request/response pattern
   - Supports multiple command types

## Troubleshooting

### Check daemon status
```bash
# Via bankshot
bankshot status

# Via Homebrew
brew services list | grep bankshot

# Via systemctl (Linux)
systemctl status bankshotd
```

### View logs
```bash
# Homebrew installation
tail -f /usr/local/var/log/bankshot/bankshot.log

# Systemd installation
journalctl -u bankshotd -f

# Manual installation
tail -f ~/.bankshot/daemon.log
```

### Debug mode
```bash
# Run daemon in foreground with debug logging
BANKSHOT_DEBUG=1 bankshotd

# Run client with debug output
BANKSHOT_DEBUG=1 bankshot status
```

### Common Issues

#### "Failed to connect to daemon"
- Ensure bankshotd is running: `brew services start phinze/tap/bankshot`
- Check socket exists: `ls -la ~/.bankshot.sock`
- Verify socket is forwarded in SSH: `ssh -O check yourserver`

#### "No active SSH connection"
- Ensure SSH ControlMaster is configured (see SSH Configuration)
- Check control socket exists: `ls -la /tmp/ssh-*`
- Verify connection is active: `ssh -O check yourserver`

#### "Failed to forward port"
- Port may already be in use locally: `lsof -i :PORT`
- Check if already forwarded: `bankshot list`
- Try a different local port: `bankshot forward 8080:8081`

#### "Permission denied" on socket
- Check socket permissions: `ls -la ~/.bankshot.sock`
- Ensure daemon is running as your user
- Remove and restart daemon

### Socket Forwarding Issues

If the socket forward isn't working:

1. Test socket forwarding manually:
   ```bash
   ssh -R /tmp/test.sock:/tmp/test.sock yourserver
   ```

2. Check SSH version supports socket forwarding:
   ```bash
   ssh -V  # Should be OpenSSH 6.7 or later
   ```

3. Try TCP mode instead:
   ```yaml
   # ~/.config/bankshot/config.yaml
   network: tcp
   address: 127.0.0.1:9999
   ```

## Development

### Building from Source

```bash
# Clone repository
git clone https://github.com/phinze/bankshot
cd bankshot

# Install dependencies
go mod download

# Build
make build

# Run tests
make test

# Install locally
make install
```

### Running Tests

```bash
# Unit tests
go test ./...

# Integration tests
go test ./test/...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Project Structure

```
bankshot/
├── cmd/
│   ├── bankshot/       # CLI client
│   └── bankshotd/      # Daemon
├── pkg/
│   ├── config/         # Configuration
│   ├── daemon/         # Daemon logic
│   ├── forwarder/      # Port forwarding
│   ├── opener/         # URL opening
│   └── protocol/       # Communication protocol
├── internal/
│   ├── logger/         # Logging utilities
│   ├── monitor/        # Port monitoring
│   └── ssh/            # SSH helpers
└── test/               # Integration tests
```

## Contributing

Pull requests welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

See [PLAN.md](PLAN.md) for the development roadmap and current priorities.

### Development Guidelines

- Write tests for new features
- Update documentation as needed
- Follow Go conventions and idioms
- Keep commits focused and descriptive

## Acknowledgements

Inspired by and building upon the excellent work of [superbrothers/opener](https://github.com/superbrothers/opener). Thank you to the authors for the original concept of remote URL opening.

## License

MIT License - see [LICENSE](LICENSE) file for details.