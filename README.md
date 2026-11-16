# bankshot

Reach back to your laptop from remote development environments: open URLs in
your local browser, paste clipboard images, use your local 1Password CLI and ssh
agent, and forward ports on demand.

## Overview

`bankshot` lets a remote session:
- Open URLs in your local browser
- Forward the ports a command opens (`bankshot wrap`), or individual ports
  (`bankshot forward`), for OAuth logins and dev servers
- Paste clipboard images, run `op` against your local 1Password CLI, and sign
  with your local ssh agent

Three pieces make this work:
- **Local daemon**: Runs on your laptop, opens URLs, and listens on forwarded ports
- **bankshot monitor**: Runs on the remote, sends CLI requests to the daemon, and relays forwarded connections
- **bankshot CLI**: Runs on the remote to trigger actions manually

The remote talks to the daemon over Tailscale. Because it doesn't rely on an
active ssh connection, it works just as well over Rex, mosh, or a web terminal.
If you don't use Tailscale, an ssh `RemoteForward` of the daemon's socket still
handles opening URLs, pasting, and `op`, though port forwarding requires a
tailnet.

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

### 1. Connect the remote to your laptop

Put both machines on the same tailnet. On your laptop, allow the remote node:

```yaml
# laptop: ~/.config/bankshot/config.yaml
tailnet:
  enabled: true
  allow: [devbox]          # tailscale logins, node names, or tags
```

On the remote, point the config at your laptop and start `bankshot monitor` (it
requires `tailnet.enabled` to run):

```yaml
# remote: ~/.config/bankshot/config.yaml
network: tcp
address: laptop:61000
tailnet:
  enabled: true
  allow: [you@github]      # who may use the forwards the laptop opens
```

See [Architecture](#architecture) for how the pieces fit together.

**Without Tailscale**, you can forward the daemon's socket over ssh to open
URLs, paste, and use `op`. Port forwarding won't work in this mode.

```
Host devbox
    RemoteForward ~/.bankshot.sock ~/.bankshot.sock
```

### 2. Basic Commands

With the daemon running on your laptop and the monitor on the remote, use the
CLI there to act on your laptop:

```bash
# Open URL in local browser
bankshot open https://github.com

# Forward a port
bankshot forward 8080

# Forward the ports a command opens, until it exits
bankshot wrap -- npm run dev

# Save the screenshot or recording on your local clipboard here, and print its path
bankshot paste

# Check status
bankshot status
```

## Architecture

```
┌──────────────────────────┐   tailnet    ┌──────────────────────────┐
│ Laptop                   │              │ Remote                   │
│                          │  requests    │                          │
│ bankshotd :61000  ◄──────┼──────────────┼── bankshot monitor       │
│  - checks tailscale whois│              │   - ~/.bankshot.sock     │
│                          │  forwards    │     for the CLI          │
│ localhost:PORT  ─────────┼──────────────┼─► relay :61001           │
│  (one per forward)       │              │   dials localhost:PORT   │
└──────────────────────────┘              └──────────────────────────┘
```

With `tailnet.enabled`, the monitor is the remote's only link to the laptop.
The CLI talks to the monitor on `~/.bankshot.sock` (`tailnet.local_socket`),
and the monitor passes each request on to the daemon's control port (61000).
The daemon looks the caller up with `tailscale whois` and refuses anyone not
in its `allow` list. A tagged node has to be allowed by node name or tag,
since it has no login.

A forward is a loopback listener on the laptop. Each connection to it goes
back over the tailnet to the monitor's relay (61001, on the remote's tailnet
address), which dials the port on the remote's own loopback. The relay
admits only the logins in the remote's `allow`, and only for ports the
monitor is forwarding.

Before acting on a request, the daemon asks the relay to confirm it really
came from the monitor, so another process on the tailnet can't make requests
in the monitor's name.

No ssh session tells the daemon when the remote goes away, so the monitor
renews a lease every two minutes (`tailnet.heartbeat`). A remote that's
silent for six (`tailnet.lease`) loses its forwards. The lease clock stops
while the laptop sleeps.

Two more jobs ride the same link. With `tailnet.agent_socket` set, the
monitor serves an ssh agent there, backed by the laptop's (`ssh_agent` in
the daemon's config), limited to listing keys and signing. And the op proxy
(`enableOpProxy`) sends `op` to the laptop's 1Password CLI.

### Forwarding ports

`bankshot` only forwards ports when you ask it to, binding them to your
laptop's loopback interface.

To forward ports for a single command, run `bankshot wrap -- CMD`. Any port the
command or its child processes start listening on is forwarded until the command
exits, while ports opened by unrelated processes are ignored.

To manage ports manually, `bankshot forward PORT` opens a forward until you run
`bankshot unforward PORT`. Use `bankshot forward REMOTE:LOCAL` to bind a
different local port, and `bankshot list` to see active forwards.

## Usage Examples

### OAuth Flow

CLI login flows like `gcloud auth login` typically open a browser and spin up a
local server to catch the OAuth callback. Running the login under `wrap` sets
`BROWSER` (to `bankshot open`) and `DISPLAY=1` so the link opens on your laptop,
while automatically forwarding the callback port until the login finishes:

```bash
$ DISPLAY=1 bankshot wrap -- gcloud auth login --update-adc
```

If a tool calls `xdg-open` directly instead of checking `BROWSER`, you can
symlink `xdg-open` to `bankshot` (or set `enableXdgOpen` in the Home Manager
module) to send those URLs to your laptop too.

### Development Server
```bash
# Forward the ports the dev server opens
$ bankshot wrap -- npm run dev

# Or manually forward specific port
$ bankshot forward 3000

# Forward to different local port
$ bankshot forward 8080:9090
```

### Pasting Screenshots and Recordings

`bankshot paste` fetches the image on your local machine's clipboard, or the
image or video file copied there (a screen recording, say), saves it in
`~/.cache/bankshot/inbox` on the remote host, and prints the path. Files older
than a week are cleared out on the next paste. Clipboard text never crosses,
and neither does a copied file that isn't an image or video or is over 200 MB.
The local machine posts a notification naming the caller each time something
is sent.

Run as `wl-paste` (the Home Manager module's `enableWlPaste`), bankshot answers
the two calls Claude Code makes for its ctrl+v image paste on Linux, so pasting
a screenshot into a remote Claude session just works.

## Configuration

### Daemon Configuration

Optional config file at `~/.config/bankshot/config.yaml`, or wherever
`BANKSHOT_CONFIG` points (`bankshotd --config` takes precedence over both):

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
