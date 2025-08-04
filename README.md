# bankshot

Open URLs and manage SSH port forwards from remote development environments.

## Overview

`bankshot` enables you to:
- Open URLs in your local browser from SSH sessions
- Forward ports dynamically for OAuth flows and development servers
- Auto-forward ports with the `wrap` command

Components:
- **bankshotd**: Local daemon that handles requests
- **bankshot**: CLI client used in remote SSH sessions

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

- **Daemon**: Handles URL opening and port forwarding on local machine
- **CLI**: Sends commands from remote SSH sessions
- **Communication**: JSON over Unix socket (forwarded via SSH)

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
