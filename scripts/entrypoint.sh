#!/bin/bash
# OOB Console Hub - Container Entrypoint
# Generates configs, substitutes env vars, performs preflight checks, then launches the supervisor

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

# --- Runtime checks and prep for supervised processes ---
DEVICE_PATH=${DEVICE_PATH:-/dev/ttySL0}
echo "Preparing runtime for supervised services..."
echo "Configured modem device path: ${DEVICE_PATH}"

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

# Clear old database if it exists to prevent Stasis init failure
rm -f /var/lib/asterisk/astdb.sqlite3 || true

# Ensure required runtime directories exist
mkdir -p /var/run/asterisk /var/log/asterisk /var/lib/asterisk /var/spool/asterisk

echo "=== OOB Console Hub Init Complete ==="
echo "Launching process supervisor..."
exec "$@"
