# bankshot

Automatic SSH port forwarding for remote development workflows.

## The Problem

When working on remote dev servers via SSH, many tools require browser-based OAuth flows that redirect to `localhost:PORT`. While you can open browsers remotely, these auth flows fail because the remote server can't bind to the same localhost ports your local browser expects.

## The Solution

`bankshot` wraps any command and automatically forwards ports it opens through your existing SSH connection using ControlMaster. No configuration needed - it just works!

## Features

- 🚀 **Zero Configuration** - Automatically detects SSH sessions and ControlMaster sockets
- 🔍 **Smart Port Detection** - Monitors child processes for new port bindings
- 🔄 **Dynamic Forwarding** - Adds and removes port forwards as needed
- 🧹 **Clean Cleanup** - Removes all forwards when process exits
- 📊 **Flexible Logging** - Debug, quiet, and JSON output modes

## Installation

### From Source

```bash
git clone https://github.com/phinze/bankshot
cd bankshot
go build -o bankshot .
sudo mv bankshot /usr/local/bin/
```

### Prerequisites

- SSH connection with ControlMaster enabled
- Go 1.24+ (for building from source)

## Usage

### Basic Usage

Wrap any command with `bankshot`:

```bash
# OAuth flow that opens browser to localhost:8080
bankshot some-oauth-cli-tool --port 8080

# Development server with random port
bankshot npm run dev

# Python HTTP server
bankshot python -m http.server 8888
```

### Setting Up SSH ControlMaster

Add to your `~/.ssh/config`:

```
Host myserver
    HostName example.com
    ControlMaster auto
    ControlPath /tmp/ssh-%r@%h:%p
    ControlPersist 10m
```

Then connect normally:

```bash
ssh myserver
```

### Environment Variables

- `BANKSHOT_DEBUG=1` - Enable debug logging to see port detection and forwarding
- `BANKSHOT_QUIET=1` - Suppress all but error messages
- `BANKSHOT_LOG_FORMAT=json` - Output logs in JSON format
- `BANKSHOT_SSH_SOCKET=/path/to/socket` - Override ControlMaster socket detection

## How It Works

1. **Process Monitoring** - bankshot spawns your command and monitors its port bindings
2. **Port Detection** - Polls `/proc/net/tcp` to detect when the process opens new ports
3. **SSH Forwarding** - Uses `ssh -O forward` to dynamically add port forwards
4. **Automatic Cleanup** - Removes forwards when ports close or process exits

## Examples

### OAuth Flow Example

```bash
# GitHub CLI authentication
bankshot gh auth login

# The CLI starts OAuth flow on localhost:45678
# bankshot detects port 45678 and forwards it
# Browser redirects work seamlessly
# Forward is removed after authentication
```

### Development Server Example

```bash
# Start a Next.js dev server
bankshot npm run dev

# Server starts on port 3000
# bankshot forwards localhost:3000
# Access http://localhost:3000 in your browser
# Live reload and HMR work normally
```

### Multiple Ports Example

```bash
# Run a service with multiple ports
bankshot docker-compose up

# Each service port is forwarded automatically
# Ports are removed as services stop
```

## Debugging

Enable debug mode to see what bankshot is doing:

```bash
BANKSHOT_DEBUG=1 bankshot your-command
```

Debug output shows:
- SSH session detection
- ControlMaster socket location
- Port detection events
- Forward add/remove commands
- Cleanup operations

## Limitations

- Requires SSH ControlMaster (no password/key prompts)
- Linux only (uses `/proc/net/tcp` for port detection)
- Forwards TCP ports only (no UDP support)
- Can't forward privileged ports (<1024) unless SSH allows it

## Contributing

Pull requests welcome! See [PLAN.md](PLAN.md) for the development roadmap.

## License

MIT