# bankshot Implementation Plan

## Problem Statement
Remote development workflows require:
1. Browser-based OAuth flows that redirect to `localhost:PORT`
2. Opening URLs from terminal in local browser
3. Port forwarding for development servers

When working on remote servers via SSH, these workflows fail because:
- The browser can't reach the remote server's localhost ports
- URLs opened on remote servers don't open in local browser
- Manual port forwarding is tedious and error-prone

## Solution
`bankshot` is a daemon-based tool that:
- Runs on your local machine as a service
- Communicates via socket forwarded through SSH
- Opens URLs in your local browser from remote sessions
- Dynamically forwards ports on demand
- Replaces and extends tools like `superbrothers/opener`

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

### Core Components

1. **Bankshot Daemon (`cmd/bankshotd/`)**
   - Runs as a service on local machine
   - Listens on Unix socket (default: `~/.bankshot.sock`)
   - Optionally supports TCP for container scenarios
   - Handles URL opening requests
   - Manages SSH port forwarding

2. **Bankshot CLI (`cmd/bankshot/`)**
   - Lightweight client for remote environments
   - Sends commands to daemon via socket
   - Compatible with `open` command interface
   - Supports port forwarding requests

3. **Command Protocol (`pkg/protocol/`)**
   - Define message format for socket communication
   - Support multiple command types:
     - `open`: Open URL in browser
     - `forward`: Request port forward
     - `status`: Get daemon status
     - `list`: List active forwards

4. **URL Handler (`pkg/opener/`)**
   - Cross-platform URL opening
   - Use `pkg/browser` or similar
   - Error handling and feedback

5. **Port Forward Manager (`pkg/forwarder/`)**
   - SSH ControlMaster integration
   - Dynamic port forward management
   - Connection lifecycle tracking
   - Automatic cleanup

## Implementation Phases

### Phase 1: Daemon Foundation ✓
- [x] Create `bankshotd` with cobra/viper
- [x] Implement Unix socket listener
- [x] Add TCP socket support option
- [x] Configuration file support (`~/.config/bankshot/config.yaml`)
- [x] Signal handling (SIGTERM, SIGINT)
- [x] Structured logging with slog

### Phase 2: URL Opening ✓
- [x] Design command protocol (JSON over socket)
- [x] Implement URL command handler
- [x] Integrate browser opening library
- [x] Error handling and client responses
- [x] Test cross-platform compatibility

### Phase 3: Port Forwarding
- [ ] Extend protocol for port forward commands
- [ ] SSH ControlMaster socket detection
- [ ] Implement `ssh -O forward` execution
- [ ] Track active forwards per connection
- [ ] Automatic cleanup on disconnect

### Phase 4: CLI Tool
- [ ] Create `bankshot` CLI client
- [ ] Implement socket communication
- [ ] `open` command compatibility mode
- [ ] Port forward command interface
- [ ] Configuration and socket discovery

### Phase 5: Homebrew Integration
- [ ] Create Homebrew formula
- [ ] LaunchAgent plist for daemon
- [ ] Post-install setup instructions
- [ ] Socket permissions handling
- [ ] Migration guide from opener

### Phase 6: Enhanced Features
- [ ] Multiple simultaneous SSH sessions
- [ ] Port forward status monitoring
- [ ] Automatic port detection (from wrapped commands)
- [ ] Security: socket access controls
- [ ] Configuration management

### Phase 7: Testing & Documentation
- [ ] Unit tests for all components
- [ ] Integration tests with mock SSH
- [ ] Real-world OAuth flow testing
- [ ] Comprehensive README
- [ ] Man pages for both commands
- [ ] Troubleshooting guide

## Technical Decisions

### Language: Go
- Consistent with existing codebase
- Excellent for daemon development
- Strong networking libraries
- Easy cross-compilation

### Socket Communication
- Primary: Unix domain socket for security
- Optional: TCP for container compatibility
- JSON protocol for extensibility
- Async responses for long operations

### Homebrew Service
- LaunchAgent for user-level daemon
- Automatic startup on login
- Easy install/uninstall
- Service management via `brew services`

## Usage Examples

```bash
# Local machine setup
brew install phinze/tap/bankshot
brew services start bankshot

# SSH config (~/.ssh/config)
Host dev-server
  RemoteForward /home/user/.bankshot.sock /Users/me/.bankshot.sock

# On remote server
# Open URL in local browser
bankshot open https://github.com

# Or use as drop-in replacement for 'open'
alias open='bankshot open'
open https://localhost:8080/oauth/callback

# Request port forward
bankshot forward 8080
bankshot forward 3000:3001  # remote:local

# List active forwards
bankshot list

# Wrapped command with auto-forwarding
bankshot wrap -- npm run dev
```

## Success Criteria

1. **Drop-in Replacement**: Works as `open` command replacement
2. **Zero Configuration**: Works out of the box with SSH RemoteForward
3. **Reliable**: Handles connection drops gracefully
4. **Secure**: Proper socket permissions and access control
5. **Fast**: Minimal latency for operations
6. **Compatible**: Works with existing SSH setups

## Migration from Current Approach

The current bankshot implementation relies on SSH ControlMaster from within the remote session. This approach has limitations:
- Cannot modify SSH connection from inside
- Requires SSH access from remote back to local
- Complex detection and setup

The daemon approach solves these issues:
- All SSH operations happen on local machine
- Simple socket protocol
- Works with any SSH configuration
- Can be extended with more features

## Security Considerations

1. **Socket Permissions**: User-only access (0600)
2. **Command Validation**: Sanitize URLs and ports
3. **Rate Limiting**: Prevent abuse
4. **Access Control**: Optional authentication
5. **Audit Logging**: Track all operations

## Future Enhancements

1. **GUI System Tray**: Status and management
2. **Browser Extension**: Enhanced integration
3. **Reverse Forwards**: Local to remote
4. **Tunnel Management**: Full SSH tunnel control
5. **Multi-User Support**: Shared development servers