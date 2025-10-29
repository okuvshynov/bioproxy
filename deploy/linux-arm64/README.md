# Bioproxy Deployment for Linux ARM64

Deployment scripts for running bioproxy as a systemd service on Linux ARM64 systems (Raspberry Pi, etc.).

## Files

- **`setup.sh`** - Initial setup script (run once)
- **`deploy.sh`** - Deployment script for updates
- **`bioproxy.service`** - Systemd service definition

## Quick Start

### 1. Initial Setup

Copy these files to your Raspberry Pi:

```bash
# From your local machine
scp -r deploy/linux-arm64/* user@raspberry-pi:/home/user/
```

On your Raspberry Pi, run the setup script:

```bash
cd /home/$USER
./setup.sh
```

This will:
- Create required directories (`~/bio`, `~/.bio`, `~/.bio/templates`)
- Install the systemd service
- Create an example configuration file
- Enable the service to start on boot

### 2. Configure

Edit the configuration file:

```bash
nano ~/.bio/proxy_conf.json
```

Example configuration:

```json
{
  "proxy_host": "0.0.0.0",
  "proxy_port": 8088,
  "admin_host": "0.0.0.0",
  "admin_port": 8089,
  "backend_url": "http://localhost:8081",
  "warmup_check_interval": 30,
  "prefixes": {
    "@code": "/home/youruser/.bio/templates/code.txt"
  }
}
```

Create your template files in `~/.bio/templates/`.

### 3. Deploy

Deploy a specific version:

```bash
./deploy.sh v0.0.1
```

The deployment script will:
1. ✓ Verify the version exists on GitHub
2. ✓ Download the binary and checksum
3. ✓ Verify checksum integrity
4. ✓ Stop the running service
5. ✓ Backup the current binary
6. ✓ Install the new binary
7. ✓ Start the service
8. ✓ Verify the service is running

## Service Management

```bash
# Start the service
sudo systemctl start bioproxy

# Stop the service
sudo systemctl stop bioproxy

# Restart the service
sudo systemctl restart bioproxy

# Check status
sudo systemctl status bioproxy

# View logs (last 50 lines)
sudo journalctl -u bioproxy -n 50

# Follow logs in real-time
sudo journalctl -u bioproxy -f

# Enable service to start on boot (already done by setup.sh)
sudo systemctl enable bioproxy
```

## Directory Structure

After setup, your home directory will contain:

```
~/
├── bio/                          # Binary installation directory
│   ├── bioproxy                  # Current binary
│   └── bioproxy.backup.*         # Backup binaries (created by deploy.sh)
└── .bio/                         # Configuration directory
    ├── proxy_conf.json           # Configuration file
    └── templates/                # Template files
        ├── code.txt
        └── debug.txt
```

## Remote Deployment

Deploy from your local machine:

```bash
ssh user@raspberry-pi "./deploy.sh v0.0.1"
```

Or add an SSH alias in `~/.ssh/config`:

```
Host rpi
    HostName raspberry-pi.local
    User youruser
```

Then deploy with:

```bash
ssh rpi "./deploy.sh v0.0.1"
```

## Troubleshooting

### Service fails to start

Check the logs:

```bash
sudo journalctl -u bioproxy -n 50 --no-pager
```

Common issues:
- Backend llama.cpp not running on port 8081
- Configuration file missing or invalid JSON
- Template files not found
- Port 8088/8089 already in use

### Manual test

Test the binary manually:

```bash
~/bio/bioproxy --version
~/bio/bioproxy -config ~/.bio/proxy_conf.json
```

### Rollback

The deploy script creates timestamped backups. To rollback:

```bash
sudo systemctl stop bioproxy
cp ~/bio/bioproxy.backup.20241029_143000 ~/bio/bioproxy
sudo systemctl start bioproxy
```

Or deploy the previous version:

```bash
./deploy.sh v0.0.0
```

## Security

The service file includes basic security hardening:

- `NoNewPrivileges=true` - Prevents privilege escalation
- `PrivateTmp=true` - Isolates /tmp directory
- `Restart=always` - Automatically restarts on failure
- `RestartSec=5` - Waits 5 seconds between restarts

For production deployments, consider:

- Running as a dedicated service user (not your personal account)
- Configuring firewall rules (ufw, iptables)
- Setting up TLS/SSL reverse proxy (nginx, caddy)
- Enabling additional systemd security options

## Updates

To update to a new version:

```bash
./deploy.sh v0.0.2
```

The script handles everything automatically, including service restart.

## Uninstall

To remove the service:

```bash
sudo systemctl stop bioproxy
sudo systemctl disable bioproxy
sudo rm /etc/systemd/system/bioproxy.service
sudo systemctl daemon-reload
```

Remove files:

```bash
rm -rf ~/bio
rm -rf ~/.bio
```
