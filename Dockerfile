# Stage 1: Build Go binaries
FROM golang:1.25-alpine AS go-builder
WORKDIR /app
COPY . .
RUN go build -o /usr/local/bin/oob-hub ./cmd/oob-hub/main.go \
    && go build -o /usr/local/bin/oob-probe ./cmd/oob-probe/main.go \
    && go build -o /usr/local/bin/oob-manage ./cmd/oob-manage/main.go

# Stage 2: Final image
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Install Asterisk 22 build dependencies and minimal utilities
RUN dpkg --add-architecture i386 && apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    wget \
    tini \
    supervisor \
    libncurses5-dev \
    libssl-dev \
    libxml2-dev \
    libsqlite3-dev \
    uuid-dev \
    libjansson-dev \
    libedit-dev \
    libcurl4-openssl-dev \
    libxslt1-dev \
    ca-certificates \
    psmisc \
    procps \
    pkg-config \
    libc6:i386 \
    && rm -rf /var/lib/apt/lists/*

# Build and Install Asterisk 22 LTS (Minimal PJSIP only)
WORKDIR /usr/local/src
RUN rm -rf /usr/lib/asterisk/modules
RUN wget -q "http://downloads.asterisk.org/pub/telephony/asterisk/asterisk-22-current.tar.gz" \
    && tar xzf asterisk-22-current.tar.gz \
    && rm asterisk-22-current.tar.gz \
    && cd asterisk-22.*/ \
    && (yes | DEBIAN_FRONTEND=noninteractive ./contrib/scripts/install_prereq install || true) \
    && ./configure --prefix=/usr \
        --with-jansson-bundled \
        --with-pjproject-bundled \
        --with-libcurl \
        --with-libxml2 \
        --with-xslt \
    && make menuselect.makeopts \
    && ./menuselect/menuselect --disable-all menuselect.makeopts \
    && ./menuselect/menuselect \
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
        --enable res_pjsip_geolocation \
        --enable res_geolocation \
        --enable res_statsd \
        --enable res_http_websocket \
        --enable res_ari \
        --enable res_ari_model \
        --enable res_ari_bridges \
        --enable res_ari_channels \
        --enable res_stasis \
        --enable res_stasis_device_state \
        --enable res_stasis_playback \
        --enable res_stasis_recording \
        --enable app_stasis \
        --enable app_external_media \
        --enable bridge_holding \
        --enable chan_pjsip \
        --enable res_rtp_asterisk \
        --enable res_sorcery_config \
        --enable res_sorcery_memory \
        --enable res_sorcery_astdb \
        --enable res_timing_timerfd \
        --enable codec_ulaw \
        --enable pbx_config \
        --enable app_dial \
        --enable app_echo \
        --enable app_playback \
        --enable format_pcm \
        --enable format_wav \
        --enable bridge_simple \
        --enable bridge_native_rtp \
        --enable func_callerid \
        --enable func_logic \
        menuselect.makeopts \
    && make -j$(nproc) \
    && make install \
    && cd .. && rm -rf asterisk-22.*/

# Install prebuilt slmodemd + slmodem-asterisk-bridge binaries (latest release)
# ADD checksums API responses; cache busts when a new release is published.
ADD https://api.github.com/repos/gbm-dev/slmodemd/releases/latest /tmp/slmodemd-release.json
ADD https://api.github.com/repos/gbm-dev/slmodem-asterisk-bridge/releases/latest /tmp/bridge-release.json
RUN wget -O /usr/local/bin/slmodemd \
        "https://github.com/gbm-dev/slmodemd/releases/latest/download/slmodemd-linux-i386" \
    && wget -O /usr/local/bin/slmodem-asterisk-bridge \
        "https://github.com/gbm-dev/slmodem-asterisk-bridge/releases/latest/download/slmodem-asterisk-bridge-linux-x86_64" \
    && chmod +x /usr/local/bin/slmodemd /usr/local/bin/slmodem-asterisk-bridge

# Copy Go binaries from builder
COPY --from=go-builder /usr/local/bin/oob-hub /usr/local/bin/oob-hub
COPY --from=go-builder /usr/local/bin/oob-probe /usr/local/bin/oob-probe
COPY --from=go-builder /usr/local/bin/oob-manage /usr/local/bin/oob-manage

# Create directories
RUN mkdir -p /var/log/oob-sessions /var/log/asterisk /var/lib/asterisk /var/spool/asterisk /etc/asterisk

# Copy Asterisk configuration
COPY config/asterisk/*.conf /etc/asterisk/

# Copy site configuration
COPY config/oob-sites.conf /etc/oob-sites.conf

# Copy supervisor configuration
COPY config/supervisor/supervisord.conf /etc/supervisor/supervisord.conf

# Copy scripts
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY scripts/oob-healthcheck.sh /usr/local/bin/oob-healthcheck.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/oob-healthcheck.sh

WORKDIR /app

# Expose ports
# 22 - SSH (Go TUI)
# 5060 - SIP (UDP)
# 10000-10100 - RTP media
EXPOSE 22/tcp 5060/udp 10000-10100/udp

# Docker-level health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD /usr/local/bin/oob-healthcheck.sh || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD ["/usr/bin/supervisord", "-n", "-c", "/etc/supervisor/supervisord.conf"]
