#!/bin/bash
# Install Asterisk 22 LTS from source
# Replaces the Ubuntu apt package (20.x) with the latest 22 LTS release.
#
# Usage: sudo ./scripts/install-asterisk.sh

set -euo pipefail

ASTERISK_VERSION="22"
DOWNLOAD_URL="http://downloads.asterisk.org/pub/telephony/asterisk/asterisk-${ASTERISK_VERSION}-current.tar.gz"
BUILD_DIR="/usr/local/src"

if [[ $EUID -ne 0 ]]; then
    echo "ERROR: This script must be run as root (sudo)."
    exit 1
fi

echo "=== Asterisk ${ASTERISK_VERSION} LTS Installer ==="
echo ""

# --- Remove apt Asterisk completely ---
if dpkg -l asterisk 2>/dev/null | grep -qE '^ii|^rc'; then
    echo "Removing apt-installed Asterisk..."
    apt-get remove --purge -y 'asterisk*' 2>/dev/null || true
    apt-get autoremove -y 2>/dev/null || true
    echo "  Done."
fi

# Clean up any leftover apt or old source modules
if [[ -d /usr/lib/x86_64-linux-gnu/asterisk ]]; then
    echo "Removing leftover apt Asterisk modules..."
    rm -rf /usr/lib/x86_64-linux-gnu/asterisk
fi
if [[ -d /usr/lib/asterisk/modules ]]; then
    echo "Removing old Asterisk modules from /usr/lib/asterisk/modules..."
    rm -rf /usr/lib/asterisk/modules
fi

# --- Install build dependencies ---
echo "Installing build dependencies..."
apt-get update -qq
apt-get install -y \
    build-essential \
    wget \
    libncurses5-dev \
    libssl-dev \
    libxml2-dev \
    libsqlite3-dev \
    uuid-dev \
    libjansson-dev \
    libedit-dev

# --- Download Asterisk ---
echo "Downloading Asterisk ${ASTERISK_VERSION} LTS..."
cd "${BUILD_DIR}"
rm -rf asterisk-${ASTERISK_VERSION}*/
wget -q "${DOWNLOAD_URL}" -O "asterisk-${ASTERISK_VERSION}-current.tar.gz"
tar xzf "asterisk-${ASTERISK_VERSION}-current.tar.gz"
rm -f "asterisk-${ASTERISK_VERSION}-current.tar.gz"

# Find extracted directory (e.g., asterisk-22.2.0)
AST_SRC=$(ls -d asterisk-${ASTERISK_VERSION}.*/ 2>/dev/null | head -1)
if [[ -z "${AST_SRC}" ]]; then
    echo "ERROR: Could not find extracted Asterisk source directory"
    exit 1
fi
cd "${AST_SRC}"

# --- Install Asterisk prerequisites ---
echo "Installing Asterisk prerequisites..."
if [[ -f "contrib/scripts/install_prereq" ]]; then
    # Use yes to answer any prompts and || true to prevent exit if it returns non-zero
    # but still mostly worked.
    yes | DEBIAN_FRONTEND=noninteractive contrib/scripts/install_prereq install || {
        echo "WARNING: install_prereq returned an error, attempting to continue..."
    }
else
    echo "WARNING: contrib/scripts/install_prereq not found!"
fi

# --- Clean and Configure ---
echo "Cleaning old build state..."
# Only run distclean if a Makefile exists
if [[ -f Makefile ]]; then
    make distclean || true
fi

# --- Configure (install to /usr so it replaces the apt paths) ---
echo "Configuring Asterisk..."
./configure --prefix=/usr --with-jansson-bundled --with-pjproject-bundled 2>&1 | tail -5

# --- Build (minimal PJSIP only) ---
echo "Configuring minimal Asterisk build (this takes a few minutes)..."
make menuselect.makeopts

# Disable everything first
./menuselect/menuselect --disable-all menuselect.makeopts

# Enable ONLY what we need for PJSIP and modem dialing
./menuselect/menuselect \
    --enable res_pjproject \
    --enable res_pjsip \
    --enable res_pjsip_authenticator_digest \
    --enable res_pjsip_outbound_authenticator_digest \
    --enable res_pjsip_endpoint_identifier_ip \
    --enable res_pjsip_endpoint_identifier_user \
    --enable res_pjsip_outbound_registration \
    --enable res_pjsip_session \
    --enable res_pjsip_sdp_rtp \
    --enable res_pjsip_caller_id \
    --enable res_pjsip_nat \
    --enable res_pjsip_rfc3326 \
    --enable res_pjsip_dtmf_info \
    --enable res_pjsip_logger \
    --enable res_pjsip_config_wizard \
    --enable res_pjsip_pubsub \
    --enable res_pjsip_outbound_publish \
    --enable res_geolocation \
    --enable res_statsd \
    --enable chan_pjsip \
    --enable res_rtp_asterisk \
    --enable res_sorcery_config \
    --enable res_sorcery_memory \
    --enable res_sorcery_astdb \
    --enable res_timing_timerfd \
    --enable codec_ulaw \
    --enable codec_alaw \
    --enable codec_gsm \
    --enable pbx_config \
    --enable app_dial \
    --enable app_echo \
    --enable app_playback \
    --enable format_pcm \
    --enable format_wav \
    --enable format_gsm \
    --enable bridge_simple \
    --enable bridge_native_rtp \
    --enable func_callerid \
    --enable func_logic \
    menuselect.makeopts

make -j"$(nproc)"

# --- Install ---
echo "Installing Asterisk..."
make install

# --- Verify ---
echo ""
echo "=== Installed ==="
asterisk -V
echo ""
echo "Verifying critical modules in /usr/lib/asterisk/modules/:"
for mod in chan_pjsip res_pjsip res_pjsip_session res_geolocation res_statsd res_pjproject; do
    if [[ -f "/usr/lib/asterisk/modules/${mod}.so" ]]; then
        echo "  ${mod}.so: OK"
    else
        echo "  ${mod}.so: MISSING"
    fi
done
echo ""
echo "Asterisk install complete. Use the Docker/systemd deployment flow from README.md."
