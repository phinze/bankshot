# Bankshot Installation Guide

## macOS Installation via Homebrew

### Quick Install

```bash
# Add the tap
brew tap phinze/tap

# Install bankshot
brew install phinze/tap/bankshot

# Start the daemon service
brew services start phinze/tap/bankshot
```

### Post-Install Setup

1. **Configure SSH ControlMaster**
   
   Add to your `~/.ssh/config`:
   ```
   Host *
       ControlMaster auto
       ControlPath /tmp/ssh-%r@%h:%p
       ControlPersist 10m
       RemoteForward ~/.bankshot.sock ~/.bankshot.sock
   ```

2. **Verify Installation**
   
   ```bash
   # Check daemon status
   bankshot status
   
   # The daemon should be running via launchd
   brew services list | grep bankshot
   ```

3. **Socket Permissions**
   
   The daemon automatically creates the socket with user-only permissions (0600).
   The socket is located at `~/.bankshot.sock` by default.

### Troubleshooting

If the daemon isn't responding:

1. Check the service status:
   ```bash
   brew services list
   ```

2. Check logs:
   ```bash
   tail -f /usr/local/var/log/bankshot/bankshot.log
   tail -f /usr/local/var/log/bankshot/bankshot.error.log
   ```

3. Restart the service:
   ```bash
   brew services restart phinze/tap/bankshot
   ```

4. Verify socket exists:
   ```bash
   ls -la ~/.bankshot.sock
   ```

## Manual Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/phinze/bankshot
cd bankshot

# Build both binaries
go build -o bankshotd ./cmd/bankshotd
go build -o bankshot ./cmd/bankshot

# Install to system
sudo cp bankshotd bankshot /usr/local/bin/

# Create config directory
mkdir -p ~/.config/bankshot

# Create config file
cat > ~/.config/bankshot/config.yaml << EOF
network: unix
address: "~/.bankshot.sock"
ssh_command: ssh
log_level: info
EOF
```

### Manual Service Setup (macOS)

Create `~/Library/LaunchAgents/com.github.phinze.bankshot.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.github.phinze.bankshot</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/bankshotd</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/bankshot.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/bankshot.error.log</string>
</dict>
</plist>
```

Then load it:
```bash
launchctl load ~/Library/LaunchAgents/com.github.phinze.bankshot.plist
```

## Usage After Installation

Once installed and configured, you can use bankshot from any SSH session:

```bash
# On your remote server
bankshot open https://github.com           # Opens in local browser
bankshot forward 8080                      # Forwards port 8080
bankshot forward 3000:3001                 # Forward remote 3000 to local 3001
bankshot status                            # Check daemon status
bankshot list                              # List active forwards
```

## Uninstalling

### Via Homebrew
```bash
brew services stop phinze/tap/bankshot
brew uninstall phinze/tap/bankshot
brew untap phinze/tap
```

### Manual Uninstall
```bash
# Stop the service
launchctl unload ~/Library/LaunchAgents/com.github.phinze.bankshot.plist

# Remove files
rm -f /usr/local/bin/bankshot /usr/local/bin/bankshotd
rm -f ~/Library/LaunchAgents/com.github.phinze.bankshot.plist
rm -rf ~/.config/bankshot
rm -f ~/.bankshot.sock
```