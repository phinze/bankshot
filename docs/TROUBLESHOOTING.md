# Troubleshooting

## Common Issues

### "Failed to connect to daemon"
- Check daemon is running: `bankshot status` or `brew services list`
- Verify the local socket exists: `ls -la ~/.bankshot.sock`
- On the remote, check the monitor is running: `systemctl status bankshot-monitor`

### "Failed to forward port"
- Port in use: `lsof -i :PORT`
- Already forwarded: `bankshot list`
- Try different port: `bankshot forward 8080:8081`

### "port forwards ride the tailnet"
- Forwards need `tailnet.enabled` in both the laptop's and the remote's
  config, with the CLI talking to the remote's monitor rather than to an ssh
  forwarded socket. See the README's Quick Start.

### Over Tailscale: forwards or requests refused
- The daemon logs "Rejected tailnet connection" for each caller it turns away. A remote missing from the laptop's
  `tailnet.allow` is refused; a tagged node must be listed by node name or
  tag, not by login.
- `tailscale whois <remote's tailnet IP>` on the laptop shows the name and
  tags the daemon checks against.
- Forwards vanish about six minutes after the monitor stops talking to the
  laptop (the lease). Check the monitor is running and that the laptop is
  awake and on the tailnet.
- Nothing is forwarded unless you ask, with `bankshot forward` or
  `bankshot wrap`.

## Debug Mode
```bash
BANKSHOT_DEBUG=1 bankshotd    # Run daemon with debug logs
BANKSHOT_DEBUG=1 bankshot status  # Debug client
```

## Socket Issues

If several independent SSH transports connect to the same host, a remote Unix
socket forward is vulnerable to one transport unlinking another's listener.
On a trusted single-user remote host, a loopback TCP listener avoids that race:
only one transport can bind the port, and the listener disappears cleanly when
its owner exits.

```sshconfig
RemoteForward 127.0.0.1:61000 ~/.bankshot.sock
```

```yaml
# ~/.config/bankshot/config.yaml
network: tcp
address: 127.0.0.1:61000
```

A loopback TCP port is reachable by other users on the
remote host, so prefer the Unix socket setup on shared systems.
