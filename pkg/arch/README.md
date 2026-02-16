# Emergent Arch Linux Package

This directory contains the packaging files for building Emergent as an Arch Linux package.

## Overview

The Emergent Arch package provides:

- System-wide installation (`/usr/bin/`)
- Systemd service integration
- Automatic dependency management
- Centralized configuration (`/etc/emergent/`)
- Proper user/group management

## Package Structure

```
pkg/arch/
├── PKGBUILD                    # Arch package build specification
├── emergent-server.service     # Systemd service for server
├── emergent-admin.service      # Systemd service for admin UI
├── emergent.config.json        # Default configuration
├── emergent.env.template       # Environment variables template
└── README.md                   # This file
```

## Building the Package

### Prerequisites

```bash
sudo pacman -S base-devel go nodejs pnpm git
```

### Build from Source

From the repository root:

```bash
cd pkg/arch
makepkg -si
```

This will:

1. Build the Go server binary
2. Build the CLI tool
3. Build the admin frontend (production)
4. Create a `.pkg.tar.zst` package
5. Install it on your system

### Build Flags

- `-s` - Install dependencies automatically
- `-i` - Install package after building
- `-f` - Force rebuild (ignore existing package)
- `-c` - Clean up work files after build

Example: `makepkg -sif`

## Installation

### Via Installation Script (Recommended)

The installation script automatically detects Arch Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent/main/install.sh | sh
```

### Manual Installation

1. Download the pre-built package:

```bash
wget https://github.com/emergent-company/emergent/releases/latest/download/emergent-VERSION-1-x86_64.pkg.tar.zst
```

2. Install with pacman:

```bash
sudo pacman -U emergent-VERSION-1-x86_64.pkg.tar.zst
```

## Post-Installation Setup

### 1. Configure Environment

Copy and edit the environment file:

```bash
sudo cp /etc/emergent/.env.template /etc/emergent/.env
sudo nano /etc/emergent/.env
```

Key settings to configure:

- Database credentials (`POSTGRES_*`)
- Authentication (Zitadel)
- AI services (Vertex AI)
- Observability (optional)

### 2. Create System User (if needed)

The package expects a `emergent` user. Create it if it doesn't exist:

```bash
sudo useradd -r -s /bin/false -d /var/lib/emergent -m emergent
sudo chown -R emergent:emergent /var/lib/emergent
sudo chown -R emergent:emergent /var/log/emergent
```

### 3. Start Services

Enable and start the services:

```bash
# Start server
sudo systemctl enable --now emergent-server.service

# Start admin UI (optional)
sudo systemctl enable --now emergent-admin.service
```

### 4. Verify Installation

Check service status:

```bash
systemctl status emergent-server
systemctl status emergent-admin
```

Check logs:

```bash
sudo journalctl -u emergent-server -f
sudo journalctl -u emergent-admin -f
```

Test CLI:

```bash
emergent version
emergent status
```

## Configuration Files

### System-Wide Config

- **Location**: `/etc/emergent/config.json`
- **Owner**: `root:root`
- **Mode**: `0644`
- **Backed up on upgrade**: Yes

### Environment Variables

- **Location**: `/etc/emergent/.env`
- **Owner**: `root:root`
- **Mode**: `0600` (contains secrets)
- **Backed up on upgrade**: Yes

### Data Directories

- **Data**: `/var/lib/emergent/`
- **Logs**: `/var/log/emergent/`
- **Owner**: `emergent:emergent`

## Systemd Services

### emergent-server.service

Main server service that runs the Go backend.

```bash
# Status
sudo systemctl status emergent-server

# Start/Stop
sudo systemctl start emergent-server
sudo systemctl stop emergent-server

# Enable/Disable autostart
sudo systemctl enable emergent-server
sudo systemctl disable emergent-server

# Restart
sudo systemctl restart emergent-server

# View logs
sudo journalctl -u emergent-server -f
```

### emergent-admin.service

Admin UI service (optional).

```bash
# Same commands as above, replace 'emergent-server' with 'emergent-admin'
sudo systemctl status emergent-admin
```

## Upgrading

### Automatic Upgrade Detection

The `emergent upgrade` command detects package manager installations:

```bash
$ emergent upgrade
Checking for updates...

⚠️  Emergent is installed via pacman package manager

To upgrade, use your system package manager:
  sudo pacman -Syu emergent

The 'emergent upgrade' command is for standalone installations only.
```

### Proper Upgrade Method

Use pacman to upgrade:

```bash
# Update package database and upgrade
sudo pacman -Syu emergent

# Or upgrade just emergent
sudo pacman -S emergent
```

### Post-Upgrade

Services are automatically restarted by systemd if needed.

## Uninstallation

### Remove Package

```bash
sudo pacman -R emergent
```

### Remove Package and Dependencies

```bash
sudo pacman -Rns emergent
```

### Remove Configuration (optional)

Configuration files in `/etc/emergent/` are preserved. To remove:

```bash
sudo rm -rf /etc/emergent
```

### Remove Data (optional)

Data in `/var/lib/emergent/` is preserved. To remove:

```bash
sudo rm -rf /var/lib/emergent
sudo rm -rf /var/log/emergent
```

## Troubleshooting

### Service Won't Start

Check the journal for errors:

```bash
sudo journalctl -u emergent-server -n 50 --no-pager
```

Common issues:

1. **Database not accessible** - Check `/etc/emergent/.env` credentials
2. **Port already in use** - Another service using port 3002 or 5176
3. **Permissions** - Check ownership of `/var/lib/emergent/`

### Configuration Not Loading

Verify environment file:

```bash
sudo cat /etc/emergent/.env
```

Restart service after changing config:

```bash
sudo systemctl restart emergent-server
```

### CLI Not in PATH

The package installs to `/usr/bin/emergent`, which should be in PATH automatically.

Verify:

```bash
which emergent
emergent version
```

If not found, check your PATH:

```bash
echo $PATH | grep -q "/usr/bin" && echo "OK" || echo "NOT IN PATH"
```

## Development

### Building from Local Changes

From repository root:

```bash
cd pkg/arch
makepkg -sif
```

This builds with your local code changes.

### Testing the Package

After building:

```bash
# Install
sudo pacman -U emergent-*.pkg.tar.zst

# Test
emergent version
sudo systemctl start emergent-server
sudo systemctl status emergent-server
```

## CI/CD Integration

The package can be built in CI/CD pipelines:

```yaml
# GitHub Actions example
- name: Build Arch Package
  run: |
    cd pkg/arch
    makepkg -s --noconfirm

- name: Upload Package
  uses: actions/upload-artifact@v3
  with:
    name: arch-package
    path: pkg/arch/*.pkg.tar.zst
```

## Comparison: Package vs Standalone

| Feature   | Package Install      | Standalone Install  |
| --------- | -------------------- | ------------------- |
| Location  | `/usr/bin/`          | `~/.emergent/bin/`  |
| Config    | `/etc/emergent/`     | `~/.emergent/`      |
| Data      | `/var/lib/emergent/` | `~/.emergent/data/` |
| Services  | systemd              | Manual/Docker       |
| Upgrade   | `pacman -Syu`        | `emergent upgrade`  |
| Uninstall | `pacman -R`          | Manual deletion     |
| User      | System-wide          | Per-user            |

## Contributing

To improve the package:

1. Test changes: `makepkg -sif`
2. Verify services: `systemctl status emergent-server`
3. Check logs: `journalctl -u emergent-server`
4. Update this README with changes

## License

Same as Emergent project license.

## Support

- **Issues**: https://github.com/emergent-company/emergent/issues
- **Docs**: https://github.com/emergent-company/emergent/docs
- **Community**: (Add your community links here)
