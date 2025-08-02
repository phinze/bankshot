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

## Acknowledgements

Inspired by and building upon the excellent work of [superbrothers/opener](https://github.com/superbrothers/opener). Thank you to the authors for the original concept of remote URL opening.

## Features

- 🌐 **URL Opening** - Open URLs in your local browser from any SSH session
- 🚀 **Dynamic Port Forwarding** - Forward ports on-demand without restarting SSH
- 🔒 **Secure** - Unix socket with user-only permissions
- 🎯 **Drop-in Replacement** - Works as a replacement for the `open` command
- 📊 **Status Monitoring** - Track active forwards and daemon status
- 👥 **Multi-Session Support** - Manage forwards across multiple SSH connections
- 📈 **Live Monitoring** - Real-time status updates with `monitor` command
- ⚙️ **Configuration Management** - View and verify daemon configuration

## Installation

### macOS (Homebrew)

```bash
brew tap phinze/tap
brew install phinze/tap/bankshot
brew services start phinze/tap/bankshot
```

### From Source

```bash
git clone https://github.com/phinze/bankshot
cd bankshot
go build -o bankshotd ./cmd/bankshotd
go build -o bankshot ./cmd/bankshot
sudo cp bankshotd bankshot /usr/local/bin/
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
bankshot wrap -- python -m http.server 8080
```

### 3. Compatibility Mode

Create an alias to replace the `open` command:

```bash
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

### Opening URLs

```bash
# Open project documentation
bankshot open https://docs.myproject.com

# Open local development server
bankshot open http://localhost:8080

# Use as drop-in replacement for 'open'
alias open='bankshot open'
open https://github.com
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

## Troubleshooting

### Check daemon status
```bash
bankshot status
brew services list | grep bankshot
```

### View logs
```bash
# Homebrew installation
tail -f /usr/local/var/log/bankshot/bankshot.log

# Manual installation
tail -f /tmp/bankshot.log
```

### Common issues

- **"Failed to connect to daemon"**: Ensure bankshotd is running
- **"No active SSH connection"**: Check SSH ControlMaster is configured
- **"Failed to forward port"**: Port may already be in use locally

## Contributing

Pull requests welcome! See [PLAN.md](PLAN.md) for the development roadmap.

## License

MIT