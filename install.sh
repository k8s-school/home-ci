#!/bin/bash

set -euxo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- Configuration ---
SERVICE_NAME="home-ci"
USER_NAME="fink-ci"
OPT_DIR="/opt/${SERVICE_NAME}"
VAR_DIR="/var/lib/${SERVICE_NAME}"
BIN_DIR="${OPT_DIR}/bin"
BIN_DEST="${BIN_DIR}/home-ci"
DIAG_DEST="${BIN_DIR}/home-ci-diag"
CONFIG_SRC="$DIR/examples/ktbx.yaml"
CONFIG_DEST="${OPT_DIR}/home-ci.yaml"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# Check for root privileges
if [ "$EUID" -ne 0 ]; then
  echo "Error: Please run as root (sudo)."
  exit 1
fi

echo "--- Starting installation of ${SERVICE_NAME} ---"

# 1. Create Group
if ! getent group "${USER_NAME}" >/dev/null; then
  echo "Creating group ${USER_NAME}..."
  groupadd --system "${USER_NAME}"
fi

# 2. Create System User
# --system: No password, no login shell, low UID
if ! getent passwd "${USER_NAME}" >/dev/null; then
  echo "Creating system user ${USER_NAME}..."
  useradd --system \
    --gid "${USER_NAME}" \
    --home-dir "${VAR_DIR}" \
    --shell /bin/false \
    --comment "Service user for ${SERVICE_NAME}" \
    "${USER_NAME}"
fi

# 3. Create Directory Hierarchy
echo "Creating directories in /opt and /var/lib..."
mkdir -p "${BIN_DIR}" "${VAR_DIR}"

# 4. Handle Binaries & Config (if they exist in current folder)
if [ -f "./home-ci" ]; then
    echo "Moving home-ci binary to ${BIN_DEST}..."
    cp "./home-ci" "${BIN_DEST}"
    chmod 755 "${BIN_DEST}"
else
    echo "Warning: './home-ci' binary not found in current directory. Please copy it manually to ${BIN_DEST}."
fi

if [ -f "./home-ci-diag" ]; then
    echo "Moving home-ci-diag binary to ${DIAG_DEST}..."
    cp "./home-ci-diag" "${DIAG_DEST}"
    chmod 755 "${DIAG_DEST}"
else
    echo "Warning: './home-ci-diag' binary not found in current directory. Please copy it manually to ${DIAG_DEST}."
fi

if [ -f "${CONFIG_SRC}" ]; then
    echo "Moving config to ${CONFIG_DEST}..."
    cp "${CONFIG_SRC}" "${CONFIG_DEST}"
else
    echo "ERROR: No config file found in current directory. Creating an empty one."
    exit 1
fi

# Set Permissions
chown -R "${USER_NAME}":"${USER_NAME}" "${OPT_DIR}" "${VAR_DIR}"
chmod 750 "${VAR_DIR}"

# 5. Create Systemd Service File
echo "Generating systemd service file..."
cat <<EOF > "${SERVICE_FILE}"
[Unit]
Description=Home CI Monitor - Git repository monitoring and CI service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${USER_NAME}
Group=${USER_NAME}
WorkingDirectory=${VAR_DIR}

# Configuration
Environment="CONFIG_FILE=${CONFIG_DEST}"
# Including /opt bin folder in PATH just in case
Environment="PATH=${BIN_DIR}:${PATH}"

ExecStart=${BIN_DEST} --config \${CONFIG_FILE} -v 3

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
StartLimitIntervalSec=300
StartLimitBurst=5

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
EOF

echo "--- Setup Complete ---"
echo "Binary location:     ${BIN_DEST}"
echo "Diagnostic location: ${DIAG_DEST}"
echo "Config location:     ${CONFIG_DEST}"
echo "Working Dir:         ${VAR_DIR}"
echo ""
echo "Commands to start the service:"
echo "sudo systemctl enable ${SERVICE_NAME}"
echo "sudo systemctl start ${SERVICE_NAME}"
echo "sudo journalctl -u ${SERVICE_NAME} -f"
