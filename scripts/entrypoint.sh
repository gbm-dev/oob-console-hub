#!/bin/bash
# OOB Console Hub - Container Entrypoint
# Validates SIP configuration, performs preflight checks, then launches the supervisor

set -euo pipefail

echo "=== OOB Console Hub Starting ==="

# --- Validate SIP credentials ---
if [[ -z "${TELNYX_SIP_USER:-}" || -z "${TELNYX_SIP_PASS:-}" ]]; then
    echo "WARNING: TELNYX_SIP_USER or TELNYX_SIP_PASS not set!"
    echo "The SIP bridge will not be able to register with Telnyx."
fi

if [[ -z "${TELNYX_OUTBOUND_CID:-}" ]]; then
    echo "WARNING: TELNYX_OUTBOUND_CID not set!"
    echo "Outbound calls may fail with provider errors like 403 Caller Origination Number is Invalid."
fi

echo "SIP server: ${TELNYX_SIP_DOMAIN:-sip.telnyx.com}"
echo "SIP user: ${TELNYX_SIP_USER:-<not set>}"
echo "Caller ID: ${TELNYX_OUTBOUND_CID:-<not set>}"

# --- Create session log directory ---
mkdir -p /var/log/oob-sessions
chmod 1777 /var/log/oob-sessions

# --- Runtime checks ---
DEVICE_PATH=${DEVICE_PATH:-/dev/ttySL0}
echo "Configured modem device path: ${DEVICE_PATH}"

echo "=== OOB Console Hub Init Complete ==="
echo "Launching process supervisor..."
exec "$@"
