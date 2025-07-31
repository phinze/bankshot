# bankshot Implementation Plan

## Problem Statement
Remote development workflows often require browser-based OAuth flows that redirect to `localhost:PORT`. When working on remote servers via SSH, these flows fail because the browser can't reach the remote server's localhost ports.

## Solution
`bankshot` wraps any command and automatically sets up SSH port forwarding for any ports the command binds to, making OAuth flows "just work" transparently.

## Architecture

### Core Components

1. **Main CLI (`main.go`)**
   - Parse command-line arguments
   - Orchestrate component initialization
   - Handle graceful shutdown

2. **Process Manager (`process/manager.go`)**
   - Spawn child process with proper environment
   - Forward signals (SIGTERM, SIGINT, etc.)
   - Monitor process lifecycle
   - Return exit code

3. **Port Monitor (`monitor/ports.go`)**
   - Detect when child process binds to new ports
   - Support both TCP and TCP6
   - Use `/proc/[PID]/net/tcp[6]` for efficient monitoring
   - Fallback to `ss` or `lsof` if needed
   - Track port lifecycle (opened/closed)

4. **SSH Manager (`ssh/manager.go`)**
   - Detect SSH session via environment
   - Locate and validate ControlMaster socket
   - Execute dynamic port forwarding commands
   - Handle SSH command failures gracefully

5. **Port Forwarder (`ssh/forwarder.go`)**
   - Manage active port forwards
   - Add forwards via `ssh -O forward`
   - Clean up forwards on exit
   - Track forward state

## Implementation Steps

### Phase 1: Core Infrastructure
- [x] Set up project structure with Nix/Go
- [x] Create basic CLI skeleton with cobra/urfave
- [x] Implement process spawning and signal forwarding
- [x] Add structured logging with slog

### Phase 2: Port Detection
- [x] Implement `/proc/net/tcp` parser
- [x] Add port monitoring goroutine
- [x] Create port change detection logic
- [x] Add debouncing for rapid port changes

### Phase 3: SSH Integration
- [x] Detect SSH environment variables
- [x] Find ControlMaster socket path
- [x] Implement `ssh -O check` validation
- [x] Add `ssh -O forward` command execution

### Phase 4: Integration & Polish
- [x] Wire all components together
- [x] Add comprehensive error handling
- [x] Implement cleanup on all exit paths
- [x] Add debug/verbose logging modes

### Phase 5: Testing & Documentation
- [ ] Unit tests for each component
- [ ] Integration tests with mock SSH
- [ ] Real-world testing with OAuth flows
- [ ] Write README with examples

## Technical Decisions

### Language: Go
- Good for system-level programming
- Excellent concurrency primitives
- Single binary distribution
- Strong standard library for process/network handling

### Port Detection Method
Primary: `/proc/[PID]/net/tcp[6]` parsing
- Most efficient (no subprocess overhead)
- Direct kernel information
- Fallback to `ss` for non-Linux systems

### SSH ControlMaster Approach
- Reuse existing SSH connection (no new auth needed)
- Use OpenSSH's `-O` control commands
- Support standard ControlPath patterns

## Usage Examples

```bash
# OAuth flow that binds to port 8080
bankshot some-cli-tool auth login

# Development server with random port
bankshot npm run dev

# Multiple services
bankshot docker-compose up
```

## Success Criteria

1. **Transparent Operation**: User shouldn't need to think about port forwarding
2. **Reliable Detection**: Catch all port bindings, even delayed ones
3. **Clean Cleanup**: No orphaned port forwards after exit
4. **Performance**: Minimal overhead on child process
5. **Compatibility**: Work with any SSH ControlMaster setup

## Edge Cases to Handle

1. Child process that rapidly binds/unbinds ports
2. Multiple processes spawned by child
3. SSH connection drops during execution
4. Child process crashes or is killed
5. Ports already forwarded by user
6. IPv6 port bindings
7. Privileged ports (< 1024)

## Configuration Options

Environment variables:
- `BANKSHOT_DEBUG`: Enable debug logging
- `BANKSHOT_SSH_SOCKET`: Override ControlMaster socket path
- `BANKSHOT_POLL_INTERVAL`: Port monitoring frequency

Command flags:
- `--debug`: Enable debug output
- `--dry-run`: Show what would be forwarded
- `--include-ports`: Only forward specific ports
- `--exclude-ports`: Skip specific ports