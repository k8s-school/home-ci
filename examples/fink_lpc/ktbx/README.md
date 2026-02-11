 ~$ cat /opt/home-ci/ktbx-ci.yaml 
# Repository configuration
repository: "https://github.com/k8s-school/ktbx.git"
repo_name: "ktbx"

# Working directory - all subdirectories calculated from this base path
work_dir: "/var/lib/home-ci/workdir"

# Test configuration
check_interval: 30s
test_script: "e2e/run.sh"
has_result_file: true
recent_commits_within: 240h
test_timeout: 60m
fetch_remote: true
max_concurrent_runs: 1
keep_time: 2h

# TODO
cleanup:
  after_e2e: true 
  script: "e2e/clean.sh"

github_actions_dispatch:
  enabled: true
  github_token_file: "/opt/home-ci/secret.yaml"
  dispatch_type: "dispatch-with-artifacts"
  has_result_file: true
fink-ci@clrlsstsrv02:~$ cat /etc/systemd/system/ktbx-ci.service 
[Unit]
Description=Home CI Monitor - Git repository monitoring and CI service
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=fink-ci
Group=fink-ci

# Automatically creates /var/lib/ktbx-ci owned by fink-ci
StateDirectory=%N
StateDirectoryMode=0750
WorkingDirectory=%S/%N

# Ensure required subdirectories exist with correct permissions
ExecStartPre=/usr/bin/install -d -m 0750 %S/%N/bin %S/%N/.kube

# Environment
Environment=GOCACHE=%S/%N/go-build-cache
Environment=GOPATH=%S/%N/go
Environment=KTBX_INSTALL_DIR=%S/%N/bin
Environment=KUBECONFIG=%S/%N/.kube/config
Environment=PATH=%S/%N/bin:%S/%N/go/bin:/usr/local/go/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Execution
ExecStart=/opt/home-ci/bin/home-ci --config /opt/home-ci/ktbx-ci.yaml -v 3

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
DevicePolicy=closed
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
LockPersonality=true
MemoryDenyWriteExecute=true
RestrictRealtime=true
RestrictSUIDSGID=true

# Reliability
Restart=on-failure
RestartSec=5s

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%N

[Install]
WantedBy=multi-user.target

