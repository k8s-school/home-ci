# Home-CI Installation

## 1. Quick installation with go install

```bash
# Direct binary installation
go install github.com/k8s-school/home-ci/cmd/home-ci@latest
go install github.com/k8s-school/home-ci/cmd/home-ci-e2e@latest
go install github.com/k8s-school/home-ci/cmd/home-ci-diag@latest

# Verify installation
home-ci --help
home-ci-diag --help
```

## 2. Installation from source

```bash
# Clone the project
git clone https://github.com/k8s-school/home-ci.git
cd home-ci

# Build all binaries
make build

# System installation
sudo cp home-ci /usr/local/bin/
sudo cp home-ci-e2e /usr/local/bin/
sudo cp home-ci-diag /usr/local/bin/

# Verify installation
home-ci --help
home-ci-diag --help
```

## 3. Basic configuration

```bash
# Create configuration directory
mkdir -p ~/.config/home-ci

# Create minimal configuration file
cat > ~/.config/home-ci/config.yaml << EOF
repo_path: "/path/to/your/repo"
check_interval: 5m
test_script: "scripts/test.sh"
test_timeout: 10m
max_commit_age: 24h
fetch_remote: true
max_concurrent_runs: 2

cleanup:
  after_e2e: true
  script: "scripts/cleanup.sh"

github_actions_dispatch:
  enabled: false
EOF
```

## 4. Usage

```bash
# Run home-ci with configuration
home-ci --config ~/.config/home-ci/config.yaml --verbose 2

# Diagnostics
home-ci-diag --config ~/.config/home-ci/config.yaml --check-timeline
home-ci-diag --config ~/.config/home-ci/config.yaml --check-concurrency

# E2E tests
home-ci-e2e --type=success --verbose 2
```

## 5. Installation via Makefile

The project includes Makefile targets to simplify development:

```bash
# Build and test
make build
make test

# Specific tests
make test-success
make test-quick
make test-normal

# Cleanup
make clean
```

## 6. Systemd service (optional)

For production deployment, create a systemd service:

```bash
# Create service file
sudo tee /etc/systemd/system/home-ci.service << EOF
[Unit]
Description=Home-CI Continuous Integration
After=network.target

[Service]
Type=simple
User=ci
Group=ci
ExecStart=/usr/local/bin/home-ci --config /home/ci/.config/home-ci/config.yaml --verbose 2
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# Enable and start service
sudo systemctl enable home-ci
sudo systemctl start home-ci
```

## 7. Surveillance et diagnostics

```bash
# Vérifier les logs
journalctl -u home-ci -f

# Diagnostics de timeline
home-ci-diag --config ~/.config/home-ci/config.yaml --check-timeline

# Diagnostics de concurrence
home-ci-diag --config ~/.config/home-ci/config.yaml --check-concurrency
```