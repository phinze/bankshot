# bankshot

Open URLs and manage SSH port forwards from remote development environments.

## Overview

`bankshot` enables you to:
- Open URLs in your local browser from SSH sessions
- Forward ports dynamically for OAuth flows and development servers
- Auto-forward ports automatically when running `bankshotd` on remote servers

Components:
- **Local daemon**: Runs on your laptop, handles URL opening and SSH port forward execution
- **bankshotd**: Runs on remote servers, monitors processes and requests forwards automatically
- **bankshot CLI**: Used in remote SSH sessions for manual operations

## Installation

### macOS (Homebrew)

```bash
brew tap phinze/bankshot
brew install bankshot
brew services start bankshot
```

### Linux/Unix

```bash
git clone https://github.com/phinze/bankshot
cd bankshot
make build
sudo make install
bankshotd
```

See [docs/INSTALL.md](docs/INSTALL.md) for detailed installation instructions and systemd setup.

Need help? Check [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) for common issues and solutions.

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

### 2. Basic Commands

Once you're running the daemon locally and have SSH configured, on a remote SSH
server w/ Bankshot installed you can use the CLI to perform actions on your
local machine:

```bash
# Open URL in local browser
bankshot open https://github.com

# Forward a port
bankshot forward 8080

# Auto-forward ports for a command
bankshot wrap -- npm run dev

# Check status
bankshot status
```

## Architecture

```
┌────────────────────┐                 ┌────────────────────┐
│   Local Machine    │                 │  Remote Server     │
│                    │                 │                    │
│ ┌────────────────┐ │                 │ ┌────────────────┐ │
│ │   Web Browser  │ │                 │ │   bankshotd    │ │
│ └─▲──────────────┘ │                 │ │  - Monitor     │ │
│   │ Open URL       │                 │ │  - Auto-fwd    │ │
│ ┌─┴──────────────┐ │                 │ └─┬──────────────┘ │
│ │ Local Daemon   │ │                 │   │ Send requests  │
│ │ - URL opener   │ │                 │   │                │
│ │ - Port forward │ │                 │ ┌─┴──────────────┐ │
│ │ - SSH executor │ │                 │ │bankshot (CLI)  │ │
│ └─┬──────────────┘ │                 │ │ Manual control │ │
│   │                │                 │ └─┬──────────────┘ │
│ ┌─┴──────────────┐ │ SSH connection  │ ┌─▼──────────────┐ │
│ │~/.bankshot.sock│ ├─────────────────► │~/.bankshot.sock│ │
│ └────────────────┘ │ Remote forward  │ └────────────────┘ │
│                    │                 │                    │
└────────────────────┘                 └────────────────────┘
```

- **Local Daemon**: Runs on laptop, handles URL opening and SSH port forward execution
- **bankshotd**: Runs on remote server, monitors processes and auto-forwards ports
- **CLI**: Manual control from remote SSH sessions
- **Communication**: JSON over Unix socket (forwarded via SSH)

### Auto-Discovery of Existing Forwards

When the daemon starts, it automatically discovers and registers any existing SSH port forwards. This means:

- If you restart the daemon, it won't lose track of your active forwards
- Forwards created before the daemon started are automatically detected
- The daemon scans for SSH ControlMaster processes and their listening ports
- Discovery happens on startup and registers forwards without re-executing SSH commands

This ensures seamless integration with your existing SSH workflows and prevents forward duplication.

### Automatic Port Forwarding with bankshotd

On remote servers, `bankshotd` automatically forwards ports without needing `bankshot wrap`:

- Monitors all processes owned by your user on the remote server
- Automatically detects when processes bind to ports
- Requests forwards from the local daemon immediately
- Cleans up forwards when processes exit (after a grace period)

**Setup:**

1. Install bankshot on your remote servers
2. Start `bankshotd` (via systemd or manually)
3. Any port your processes bind to (in the configured range, default 3000-9999) will be automatically forwarded

**Configuration:**

Configure `bankshotd` behavior in `~/.config/bankshot/config.yaml`:

```yaml
monitor:
  portRanges:
    - start: 3000
      end: 9999
  ignoreProcesses:
    - sshd
    - systemd
    - ssh-agent
  pollInterval: 1s
  gracePeriod: 30s
```

With NixOS/home-manager, configure via `programs.bankshot.monitor.*` options.

## Usage Examples

### OAuth Flow

If you shadow the xdg-open command, you can get tools like gcloud to route browser open requests through bankshot:

```bash
# Shadow xdg-open with bankshot
sudo ln -s $(which bankshot) /usr/local/bin/xdg-open

# When OAuth tool needs browser authentication, the browser will open locally
# and ports will be forwarded automatically
$ bankshot wrap -- gcloud auth login
```

### Development Server
```bash
# Auto-forward all ports
$ bankshot wrap -- npm run dev

# Or manually forward specific port
$ bankshot forward 3000

# Forward to different local port
$ bankshot forward 8080:9090
```

## Configuration

### Daemon Configuration

Optional config file at `~/.config/bankshot/config.yaml`:

```yaml
network: unix                    # or "tcp"
address: ~/.bankshot.sock       # or "127.0.0.1:9999" for tcp
log_level: info                 # debug, info, warn, error
```

### Environment Variables

- `BANKSHOT_DEBUG`: Enable debug logging
- `BANKSHOT_SOCKET`: Override socket path

## Contributing

Pull requests welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## Acknowledgements

Inspired by and building upon the excellent work of [superbrothers/opener](https://github.com/superbrothers/opener). Thank you to the authors for the original concept of remote URL opening.

## License

MIT License - see [LICENSE](LICENSE) file for details.
