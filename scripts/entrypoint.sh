#!/bin/bash
# OOB Console Hub - Container Entrypoint
# Validates env, prepares runtime dirs, then launches the supervisor.
# slmodem-sip-bridge reads Telnyx credentials directly from the
# environment (TELNYX_SIP_USER / TELNYX_SIP_PASS / TELNYX_SIP_DOMAIN /
# TELNYX_OUTBOUND_CID / TELNYX_OUTBOUND_NAME), so no config templating
# is required here.

set -euo pipefail

echo "=== OOB Console Hub Starting ==="

if [[ -z "${TELNYX_SIP_USER:-}" || -z "${TELNYX_SIP_PASS:-}" ]]; then
    echo "WARNING: TELNYX_SIP_USER or TELNYX_SIP_PASS not set!"
    echo "slmodem-sip-bridge will fail to authenticate with Telnyx."
fi

if [[ -z "${TELNYX_OUTBOUND_CID:-}" ]]; then
    echo "WARNING: TELNYX_OUTBOUND_CID not set!"
    echo "Outbound calls may fail with provider errors like 403 Caller Origination Number is Invalid."
fi

mkdir -p /var/log/oob-sessions
chmod 1777 /var/log/oob-sessions

DEVICE_PATH=${DEVICE_PATH:-/dev/ttySL0}
echo "Configured modem device path: ${DEVICE_PATH}"

echo "=== OOB Console Hub Init Complete ==="
echo "Launching process supervisor..."
exec "$@"
