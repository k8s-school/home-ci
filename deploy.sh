#!/bin/bash

set -euo pipefail

# Deployment script for home-ci
# This script builds the home-ci binary, installs it, and sets up the systemd service

INSTALL_DIR="/usr/local/bin"
SERVICE_FILE="/etc/systemd/system/home-ci.service"
SERVICE_USER="home-ci"
CONFIG_DIR="/etc/home-ci"
LOG_DIR="/var/log/home-ci"
DATA_DIR="/var/lib/home-ci"

echo "🚀 Starting home-ci deployment..."

# Check if sudo is available
if ! command -v sudo &> /dev/null; then
    echo "❌ sudo is required but not available"
    exit 1
fi

# Build the applications
echo "🏗️  Building home-ci and home-ci-diag..."
make build-home-ci build-diag

if [[ ! -f "./home-ci" ]]; then
    echo "❌ Build failed - home-ci binary not found"
    exit 1
fi

if [[ ! -f "./home-ci-diag" ]]; then
    echo "❌ Build failed - home-ci-diag binary not found"
    exit 1
fi

echo "✅ Build completed successfully"

# Create service user if it doesn't exist
if ! id "$SERVICE_USER" &>/dev/null; then
    echo "👤 Creating service user: $SERVICE_USER"
    sudo useradd --system --shell /bin/false --home-dir "$DATA_DIR" --create-home "$SERVICE_USER"
else
    echo "✅ Service user $SERVICE_USER already exists"
fi

# Create necessary directories
echo "📁 Creating directories..."
sudo mkdir -p "$CONFIG_DIR"
sudo mkdir -p "$LOG_DIR"
sudo mkdir -p "$DATA_DIR"

# Set proper ownership
sudo chown "$SERVICE_USER:$SERVICE_USER" "$LOG_DIR"
sudo chown "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"

# Install the binaries using make install
echo "📦 Installing binaries using make install..."
make install

echo "✅ Binaries installed successfully"

# Create systemd service file
echo "⚙️  Creating systemd service file..."
sudo tee "$SERVICE_FILE" > /dev/null << 'EOF'
[Unit]
Description=Home CI Monitor - Git repository monitoring and CI service
Documentation=https://github.com/k8s-school/home-ci
After=network.target
Wants=network.target

[Service]
Type=simple
User=home-ci
Group=home-ci
ExecStart=/usr/local/bin/home-ci --config=/etc/home-ci/config.yaml --verbose=2
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=home-ci

# Security settings
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/log/home-ci /var/lib/home-ci /tmp
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes

# Resource limits
LimitNOFILE=1048576
LimitNPROC=1048576

# Working directory
WorkingDirectory=/var/lib/home-ci

# Environment
Environment=HOME=/var/lib/home-ci

[Install]
WantedBy=multi-user.target
EOF

echo "✅ Systemd service file created"

# Create a sample configuration file if it doesn't exist
if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
    echo "📝 Creating sample configuration file..."
    sudo tee "$CONFIG_DIR/config.yaml" > /dev/null << 'EOF'
# Home-CI Configuration
# Adjust these settings according to your needs

# Global settings
poll_interval: 30s
keep_time: 2h
max_concurrent_runs: 2

# Repository to monitor
repositories:
  - name: "example-repo"
    url: "https://github.com/username/repository.git"
    branch: "main"

    # Test command to run
    test_command: "make test"

    # Optional: Specific directory to clone to
    # clone_dir: "/tmp/home-ci-repos/example-repo"

    # Optional: GitHub dispatch configuration
    # github_dispatch:
    #   enabled: false
    #   token_file: "/etc/home-ci/github-token"
    #   owner: "username"
    #   repo: "repository"
    #   event_type: "ci-trigger"

# Logging configuration
logging:
  level: "info"
  file: "/var/log/home-ci/home-ci.log"
EOF

    sudo chown "$SERVICE_USER:$SERVICE_USER" "$CONFIG_DIR/config.yaml"
    sudo chmod 644 "$CONFIG_DIR/config.yaml"

    echo "✅ Sample configuration created at $CONFIG_DIR/config.yaml"
    echo "📝 Please edit $CONFIG_DIR/config.yaml to configure your repositories"
else
    echo "✅ Configuration file already exists at $CONFIG_DIR/config.yaml"
fi

# Reload systemd and enable the service
echo "🔄 Reloading systemd daemon..."
sudo systemctl daemon-reload

echo "🔧 Enabling home-ci service..."
sudo systemctl enable home-ci.service

echo "🚀 Starting home-ci service..."
sudo systemctl start home-ci.service

# Check service status
sleep 2
if sudo systemctl is-active --quiet home-ci.service; then
    echo "✅ Home-CI service is running successfully!"
    echo ""
    echo "📊 Service status:"
    sudo systemctl status home-ci.service --no-pager --lines=5
else
    echo "❌ Home-CI service failed to start"
    echo "📋 Service status:"
    sudo systemctl status home-ci.service --no-pager --lines=10
    echo ""
    echo "📝 Check logs with: journalctl -u home-ci.service -f"
    exit 1
fi

echo ""
echo "🎉 Deployment completed successfully!"
echo ""
echo "📋 Useful commands:"
echo "  • Check status:       sudo systemctl status home-ci.service"
echo "  • View logs:          sudo journalctl -u home-ci.service -f"
echo "  • Restart service:    sudo systemctl restart home-ci.service"
echo "  • Stop service:       sudo systemctl stop home-ci.service"
echo "  • Edit config:        sudo nano $CONFIG_DIR/config.yaml"
echo "  • Run diagnostics:    home-ci-diag --config=$CONFIG_DIR/config.yaml --check-timeline"
echo ""
echo "⚠️  Don't forget to:"
echo "  1. Edit $CONFIG_DIR/config.yaml with your repository settings"
echo "  2. Restart the service after configuration changes: sudo systemctl restart home-ci.service"