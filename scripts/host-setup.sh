#!/bin/bash
# OOB Console Hub - Ubuntu Host Setup Script
# Run as root on a fresh Ubuntu 22.04/24.04 server
#
# Usage: sudo bash host-setup.sh
#
# This script:
#   1. Creates a locked-down admin user for host management
#   2. Installs Docker + Docker Compose
#   3. Configures nftables firewall
#   4. Applies SSH hardening
#   5. Clones and prepares the OOB hub

set -euo pipefail

# --- Configuration ---
HOST_ADMIN_USER="${HOST_ADMIN_USER:-oobadmin}"
OOB_SSH_PORT="${OOB_SSH_PORT:-2222}"
HOST_SSH_PORT="${HOST_SSH_PORT:-22}"
INSTALL_DIR="/opt/oob-console-hub"
REPO_URL="https://github.com/gbm-dev/oob-console-hub.git"

echo "=============================================="
echo "  OOB Console Hub - Host Setup"
echo "=============================================="
echo ""

# --- Must be root ---
if [[ $EUID -ne 0 ]]; then
    echo "ERROR: Run this script as root (sudo bash host-setup.sh)"
    exit 1
fi

# --- 1. Create host admin user ---
echo "[1/5] Creating host admin user '${HOST_ADMIN_USER}'..."

if id "$HOST_ADMIN_USER" &>/dev/null; then
    echo "  User '${HOST_ADMIN_USER}' already exists, skipping."
else
    adduser --disabled-password --gecos "OOB Host Admin" "$HOST_ADMIN_USER"
    usermod -aG sudo "$HOST_ADMIN_USER"
    echo "  Created user '${HOST_ADMIN_USER}' with sudo access."
    echo ""
    echo "  >>> Set a password now:"
    passwd "$HOST_ADMIN_USER"
fi

# Set up SSH key auth directory
mkdir -p "/home/${HOST_ADMIN_USER}/.ssh"
chmod 700 "/home/${HOST_ADMIN_USER}/.ssh"
touch "/home/${HOST_ADMIN_USER}/.ssh/authorized_keys"
chmod 600 "/home/${HOST_ADMIN_USER}/.ssh/authorized_keys"
chown -R "${HOST_ADMIN_USER}:${HOST_ADMIN_USER}" "/home/${HOST_ADMIN_USER}/.ssh"

echo "  Add your SSH public key to: /home/${HOST_ADMIN_USER}/.ssh/authorized_keys"
echo ""

# --- 2. Install Docker ---
echo "[2/5] Installing Docker..."

if command -v docker &>/dev/null; then
    echo "  Docker already installed: $(docker --version)"
else
    apt-get update
    apt-get install -y ca-certificates curl gnupg

    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | \
        gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg

    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
        https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
        tee /etc/apt/sources.list.d/docker.list > /dev/null

    apt-get update
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

    systemctl enable docker
    systemctl start docker
    echo "  Docker installed."
fi

# Add admin user to docker group
usermod -aG docker "$HOST_ADMIN_USER"
echo "  Added '${HOST_ADMIN_USER}' to docker group."

# --- 3. SSH Hardening ---
echo "[3/6] Hardening SSH..."

# Detect SSH service name (sshd on older Ubuntu, ssh on 24.04+)
if systemctl list-units --type=service --all | grep -q 'sshd\.service'; then
    SSH_SERVICE="sshd"
else
    SSH_SERVICE="ssh"
fi

cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak.$(date +%s)

# Copy root's authorized_keys to the admin user so they don't get locked out
if [[ -f /root/.ssh/authorized_keys ]]; then
    cat /root/.ssh/authorized_keys >> "/home/${HOST_ADMIN_USER}/.ssh/authorized_keys"
    # Deduplicate
    sort -u "/home/${HOST_ADMIN_USER}/.ssh/authorized_keys" -o "/home/${HOST_ADMIN_USER}/.ssh/authorized_keys"
    chown "${HOST_ADMIN_USER}:${HOST_ADMIN_USER}" "/home/${HOST_ADMIN_USER}/.ssh/authorized_keys"
    echo "  Copied root's SSH keys to ${HOST_ADMIN_USER}."
fi

# Check if the admin user has at least one SSH key before disabling password auth
ADMIN_KEY_COUNT=$(wc -l < "/home/${HOST_ADMIN_USER}/.ssh/authorized_keys" 2>/dev/null || echo 0)

if [[ "$ADMIN_KEY_COUNT" -eq 0 ]]; then
    echo ""
    echo "  !!! WARNING: No SSH keys found for '${HOST_ADMIN_USER}' !!!"
    echo "  !!! Password auth will remain ENABLED to prevent lockout. !!!"
    echo "  !!! Add your key, then re-run this script to finish hardening. !!!"
    echo ""
    ALLOW_PASSWORD="yes"
else
    echo "  Found ${ADMIN_KEY_COUNT} SSH key(s) for '${HOST_ADMIN_USER}'."
    ALLOW_PASSWORD="no"
fi

cat > /etc/ssh/sshd_config.d/hardening.conf <<SSHEOF
# OOB Host SSH Hardening
Port ${HOST_SSH_PORT}
PermitRootLogin prohibit-password
PasswordAuthentication ${ALLOW_PASSWORD}
PubkeyAuthentication yes
MaxAuthTries 3
MaxSessions 5
LoginGraceTime 30
ClientAliveInterval 300
ClientAliveCountMax 2
X11Forwarding no
AllowTcpForwarding no
AllowAgentForwarding no
PermitEmptyPasswords no

# Allow host admin and root (key-only)
AllowUsers ${HOST_ADMIN_USER} root
SSHEOF

# Validate config before restarting
if sshd -t 2>/dev/null; then
    systemctl restart "$SSH_SERVICE"
    if [[ "$ALLOW_PASSWORD" == "no" ]]; then
        echo "  SSH hardened. Key-only auth enabled."
    else
        echo "  SSH configured. Password auth still enabled (no keys found)."
    fi
else
    echo "  WARNING: sshd config test failed. Reverting."
    rm /etc/ssh/sshd_config.d/hardening.conf
fi

echo ""
echo "  !!! TEST SSH in another terminal before logging out !!!"
echo "  !!! ssh ${HOST_ADMIN_USER}@<this-ip> -p ${HOST_SSH_PORT} !!!"
echo ""

# --- 4. Firewall (nftables) ---
echo "[4/5] Configuring nftables firewall..."

apt-get install -y nftables

cat > /etc/nftables.conf <<NFTEOF
#!/usr/sbin/nft -f

flush ruleset

table inet filter {
    chain input {
        type filter hook input priority 0; policy drop;

        # Loopback
        iif lo accept

        # Established/related connections
        ct state established,related accept

        # ICMP (ping)
        ip protocol icmp accept
        ip6 nexthdr icmpv6 accept

        # Host SSH (admin access)
        tcp dport ${HOST_SSH_PORT} ct state new accept

        # OOB SSH (container, for admins to reach the TUI menu)
        tcp dport ${OOB_SSH_PORT} ct state new accept

        # SIP signaling (Telnyx)
        udp dport 5060 accept

        # RTP media range (Telnyx voice/modem)
        udp dport 10000-10100 accept

        # Log and drop everything else
        counter drop
    }

    chain forward {
        type filter hook forward priority 0; policy drop;

        # Allow Docker container traffic
        ct state established,related accept

        # Allow forwarding to/from Docker networks
        iifname "docker*" accept
        oifname "docker*" accept
        iifname "br-*" accept
        oifname "br-*" accept
    }

    chain output {
        type filter hook output priority 0; policy accept;
    }
}
NFTEOF

systemctl enable nftables
nft -f /etc/nftables.conf
echo "  nftables configured. Open ports: ${HOST_SSH_PORT}/tcp (host SSH), ${OOB_SSH_PORT}/tcp (OOB), 5060/udp (SIP), 10000-10100/udp (RTP)"

# --- 5. Clone and prepare OOB hub ---
echo "[5/6] Setting up OOB Console Hub..."

if [[ -d "$INSTALL_DIR" ]]; then
    echo "  ${INSTALL_DIR} already exists. Pulling latest..."
    cd "$INSTALL_DIR"
    git pull
else
    git clone "$REPO_URL" "$INSTALL_DIR"
    cd "$INSTALL_DIR"
fi

# Create .env if it doesn't exist
if [[ ! -f .env ]]; then
    cp .env.example .env
    echo ""
    echo "  >>> Edit your Telnyx credentials:"
    echo "  >>> nano ${INSTALL_DIR}/.env"
fi

# --- 6. Install systemd service and watchdog ---
echo "[6/6] Installing systemd service and watchdog..."

cp "${INSTALL_DIR}/systemd/oob-hub.service" /etc/systemd/system/
cp "${INSTALL_DIR}/systemd/oob-watchdog.service" /etc/systemd/system/
cp "${INSTALL_DIR}/systemd/oob-watchdog.timer" /etc/systemd/system/
chmod +x "${INSTALL_DIR}/scripts/oob-watchdog.sh"

systemctl daemon-reload
systemctl enable oob-hub.service
systemctl enable oob-watchdog.timer

echo "  Installed: oob-hub.service (starts container on boot)"
echo "  Installed: oob-watchdog.timer (health checks every 2 min)"

# Install user management script to host PATH
cp "${INSTALL_DIR}/scripts/oob-user-manage" /usr/local/bin/oob-user-manage
chmod +x /usr/local/bin/oob-user-manage

# Set ownership
chown -R "${HOST_ADMIN_USER}:${HOST_ADMIN_USER}" "$INSTALL_DIR"

echo ""
echo "=============================================="
echo "  Setup Complete!"
echo "=============================================="
echo ""
echo "  Next steps:"
echo ""
echo "  1. Add your SSH public key:"
echo "     /home/${HOST_ADMIN_USER}/.ssh/authorized_keys"
echo ""
echo "  2. TEST host SSH access in another terminal before logging out:"
echo "     ssh ${HOST_ADMIN_USER}@<server-ip> -p ${HOST_SSH_PORT}"
echo ""
echo "  3. Edit Telnyx credentials:"
echo "     nano ${INSTALL_DIR}/.env"
echo ""
echo "  4. Add your remote sites:"
echo "     nano ${INSTALL_DIR}/config/oob-sites.conf"
echo ""
echo "  5. Build and start via systemd:"
echo "     cd ${INSTALL_DIR} && docker compose build"
echo "     systemctl start oob-hub"
echo "     systemctl start oob-watchdog.timer"
echo ""
echo "  6. Create OOB users:"
echo "     oob-user-manage add first.last"
echo "     oob-user-manage list"
echo ""
echo "  7. Users connect to the OOB menu with their own credentials:"
echo "     ssh first.last@<server-ip> -p ${OOB_SSH_PORT}"
echo ""
echo "  User management:"
echo "     oob-user-manage add <user>      Create user (temp pass, forced change)"
echo "     oob-user-manage remove <user>   Remove user"
echo "     oob-user-manage reset <user>    Reset password"
echo "     oob-user-manage lock <user>     Disable account"
echo "     oob-user-manage unlock <user>   Re-enable account"
echo "     oob-user-manage list            Show all users"
echo ""
echo "  Monitoring:"
echo "     systemctl status oob-hub              # service status"
echo "     systemctl status oob-watchdog.timer    # watchdog timer"
echo "     journalctl -u oob-watchdog -f          # watchdog logs"
echo "     docker exec oob-console-hub oob-healthcheck.sh --verbose  # manual health check"
echo "     docker exec oob-console-hub oob-manage list              # list users directly"
echo ""
echo "  Firewall: host SSH(:${HOST_SSH_PORT}), OOB(:${OOB_SSH_PORT}), SIP(:5060/udp), RTP(:10000-10100/udp)"
echo ""
