#!/bin/bash
# OOB Console Hub - Container Entrypoint
# Generates configs, substitutes env vars, starts telephony, then launches Go SSH server

set -euo pipefail

echo "=== OOB Console Hub Starting ==="

# --- Substitute Telnyx credentials into PJSIP config ---
PJSIP_CONF=/etc/asterisk/pjsip_wizard.conf
EXTENSIONS_CONF=/etc/asterisk/extensions.conf

escape_sed_replacement() {
    printf '%s' "$1" | sed -e 's/[\/&|\\]/\\&/g'
}

if [[ -z "${TELNYX_SIP_USER:-}" || -z "${TELNYX_SIP_PASS:-}" ]]; then
    echo "WARNING: TELNYX_SIP_USER or TELNYX_SIP_PASS not set!"
    echo "Asterisk will start but Telnyx trunk will not register."
fi

TELNYX_SIP_USER_ESCAPED=$(escape_sed_replacement "${TELNYX_SIP_USER:-unset}")
TELNYX_SIP_PASS_ESCAPED=$(escape_sed_replacement "${TELNYX_SIP_PASS:-unset}")
TELNYX_SIP_DOMAIN_ESCAPED=$(escape_sed_replacement "${TELNYX_SIP_DOMAIN:-sip.telnyx.com}")
TELNYX_OUTBOUND_CID_ESCAPED=$(escape_sed_replacement "${TELNYX_OUTBOUND_CID:-unset}")
TELNYX_OUTBOUND_NAME_ESCAPED=$(escape_sed_replacement "${TELNYX_OUTBOUND_NAME:-OOB-Console-Hub}")

if [[ -z "${TELNYX_OUTBOUND_CID:-}" ]]; then
    echo "WARNING: TELNYX_OUTBOUND_CID not set!"
    echo "Outbound calls may fail with provider errors like 403 Caller Origination Number is Invalid."
fi

sed -i "s|TELNYX_SIP_USER_PLACEHOLDER|${TELNYX_SIP_USER_ESCAPED}|g" "$PJSIP_CONF"
sed -i "s|TELNYX_SIP_PASS_PLACEHOLDER|${TELNYX_SIP_PASS_ESCAPED}|g" "$PJSIP_CONF"
sed -i "s|TELNYX_SIP_DOMAIN_PLACEHOLDER|${TELNYX_SIP_DOMAIN_ESCAPED}|g" "$PJSIP_CONF"
sed -i "s|TELNYX_OUTBOUND_CID_PLACEHOLDER|${TELNYX_OUTBOUND_CID_ESCAPED}|g" "$EXTENSIONS_CONF"
sed -i "s|TELNYX_OUTBOUND_NAME_PLACEHOLDER|${TELNYX_OUTBOUND_NAME_ESCAPED}|g" "$EXTENSIONS_CONF"

echo "Telnyx telephony config populated."

# --- Create session log directory ---
mkdir -p /var/log/oob-sessions
chmod 1777 /var/log/oob-sessions

# --- Start slmodemd + slmodem-asterisk-bridge ---
DEVICE_PATH=${DEVICE_PATH:-/dev/ttySL0}

echo "Starting slmodemd bridge..."
# Bridge runtime uses ARI_* environment variables for Asterisk control.
# Limit file descriptors to 1024 to avoid FD_SETSIZE crash in 32-bit slmodemd
sh -c "ulimit -n 1024; slmodemd -e /usr/local/bin/slmodem-asterisk-bridge" &
SLMODEM_PID=$!
echo "  slmodemd bridge started (PID ${SLMODEM_PID})"

# Wait for modem device to appear
echo "Waiting for ${DEVICE_PATH}..."
for i in $(seq 1 10); do
    if [[ -e "${DEVICE_PATH}" ]]; then
        echo "  ${DEVICE_PATH} - OK"
        break
    fi
    if [ "$i" -eq 10 ]; then
        echo "  ERROR: ${DEVICE_PATH} did not appear after 10s"
        exit 1
    fi
    sleep 1
done

# --- Start Asterisk ---
echo "Starting Asterisk..."
# Clear old database if it exists to prevent Stasis init failure
rm -f /var/lib/asterisk/astdb.sqlite3 || true

# Ensure required runtime directories exist
mkdir -p /var/run/asterisk /var/log/asterisk /var/lib/asterisk /var/spool/asterisk

# Diagnostic: Verify modules directory
REAL_MOD_DIR=""
for dir in /usr/lib/asterisk/modules /usr/lib64/asterisk/modules /usr/lib/x86_64-linux-gnu/asterisk/modules; do
    if [[ -d "$dir" ]] && [[ -n "$(ls -A "$dir" 2>/dev/null)" ]]; then
        REAL_MOD_DIR="$dir"
        break
    fi
done

if [[ -n "$REAL_MOD_DIR" ]]; then
    echo "  Found Asterisk modules in ${REAL_MOD_DIR}"
    # Update asterisk.conf to use the correct module directory if it differs
    sed -i "s|astmoddir =>.*|astmoddir => ${REAL_MOD_DIR}|" /etc/asterisk/asterisk.conf
else
    echo "ERROR: Could not find Asterisk modules directory!"
    exit 1
fi

# Diagnostic: Verify binary and libraries
if ! command -v asterisk >/dev/null 2>&1; then
    echo "ERROR: asterisk binary not found in PATH!"
    exit 1
fi

if ! ldd "$(command -v asterisk)" >/dev/null 2>&1; then
    echo "WARNING: Could not run ldd on asterisk binary."
else
    MISSING_LIBS=$(ldd "$(command -v asterisk)" | grep "not found" || true)
    if [[ -n "${MISSING_LIBS}" ]]; then
        echo "ERROR: Missing libraries for Asterisk:"
        echo "${MISSING_LIBS}"
        exit 1
    fi
fi

# Start Asterisk with explicit config path and high verbosity
# Use -f to stay in foreground (non-daemon), -vvv for verbosity
# Redirect to startup.log for capturing early failures
asterisk -f -C /etc/asterisk/asterisk.conf -vvv >/var/log/asterisk/startup.log 2>&1 &
ASTERISK_PID=$!

# Wait for Asterisk to be ready
echo "  Waiting for Asterisk to initialize..."
for i in $(seq 1 10); do
    if kill -0 $ASTERISK_PID 2>/dev/null; then
        if asterisk -rx "core show version" &>/dev/null; then
            echo "  Asterisk initialized successfully."
            break
        fi
    else
        echo "ERROR: Asterisk process died during startup!"
        echo "--- Last 50 lines of startup.log ---"
        tail -n 50 /var/log/asterisk/startup.log
        echo "------------------------------------"
        exit 1
    fi
    sleep 1
    if [ "$i" -eq 10 ]; then
        echo "WARNING: Asterisk process still starting after 10s..."
    fi
done

# --- Start Go SSH server (replaces sshd) ---
echo "=== OOB Console Hub Ready ==="
echo "SSH server listening on ${SSH_ADDRESS:-}:${SSH_PORT:-2222}"
echo "Modem device: ${DEVICE_PATH}"
echo "Manage users with: docker exec oob-console-hub oob-manage <command>"

exec /usr/local/bin/oob-hub
