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


 ~$ cat /etc/systemd/system/ktbx-ci.service
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

# Automatic directory management in /var/lib/
StateDirectory=ktbx-ci
WorkingDirectory=%S/ktbx-ci
# Define variable for reuse within the file
Environment="BASE_DIR=/var/lib/ktbx-ci"

# Prepare missing directories
ExecStartPre=-/usr/bin/mkdir -p ${BASE_DIR}/bin ${BASE_DIR}/.kube

# Environment configuration
Environment=GOCACHE=${BASE_DIR}/go-build-cache
Environment=GOPATH=${BASE_DIR}/go
Environment=KTBX_INSTALL_DIR=${BASE_DIR}/bin
Environment="CONFIG_FILE=/opt/home-ci/ktbx-ci.yaml"
Environment="KUBECONFIG=${BASE_DIR}/.kube/config"
Environment="PATH=${BASE_DIR}/bin:${BASE_DIR}/go/bin:/usr/local/go/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin"

# Execution
ExecStart=/opt/home-ci/bin/home-ci --config ${CONFIG_FILE} -v 3

# Hardening
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
DevicePolicy=closed

# Reliability
Restart=on-failure
RestartSec=5s

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=home-ci

[Install]
WantedBy=multi-user.target
