# Troubleshooting

## Common Issues

### "Failed to connect to daemon"
- Check daemon is running: `bankshot status` or `brew services list`
- Verify socket exists: `ls -la ~/.bankshot.sock`
- Check SSH forward: `ssh -O check yourserver`

### "Failed to forward port"
- Port in use: `lsof -i :PORT`
- Already forwarded: `bankshot list`
- Try different port: `bankshot forward 8080:8081`

### "No active SSH connection"
- Ensure SSH ControlMaster is configured
- Check control socket: `ls -la /tmp/ssh-*`

## Debug Mode
```bash
BANKSHOT_DEBUG=1 bankshotd    # Run daemon with debug logs
BANKSHOT_DEBUG=1 bankshot status  # Debug client
```

## Socket Issues
If socket forwarding fails, ensure OpenSSH 6.7+ or use TCP mode:
```yaml
# ~/.config/bankshot/config.yaml
network: tcp
address: 127.0.0.1:9999
```