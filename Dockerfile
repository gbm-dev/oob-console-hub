# Stage 1: Build Go binaries
FROM golang:1.25-alpine AS go-builder
WORKDIR /app
COPY . .
RUN go build -o /usr/local/bin/oob-hub ./cmd/oob-hub/main.go \
    && go build -o /usr/local/bin/oob-probe ./cmd/oob-probe/main.go \
    && go build -o /usr/local/bin/oob-manage ./cmd/oob-manage/main.go

# Stage 2: Final image (no Asterisk — bridge handles SIP/RTP directly)
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Minimal runtime dependencies: 32-bit libc for slmodemd, process tools
RUN dpkg --add-architecture i386 && apt-get update && apt-get install -y --no-install-recommends \
    tini \
    supervisor \
    ca-certificates \
    psmisc \
    procps \
    wget \
    libc6:i386 \
    && rm -rf /var/lib/apt/lists/*

# Install prebuilt slmodemd + slmodem-sip-bridge binaries (latest release)
# ADD checksums API responses; cache busts when a new release is published.
ADD https://api.github.com/repos/gbm-dev/slmodemd/releases/latest /tmp/slmodemd-release.json
ADD https://api.github.com/repos/gbm-dev/slmodem-sip-bridge/releases/latest /tmp/bridge-release.json
RUN wget -O /usr/local/bin/slmodemd \
        "https://github.com/gbm-dev/slmodemd/releases/latest/download/slmodemd-linux-i386" \
    && wget -O /usr/local/bin/slmodem-sip-bridge \
        "https://github.com/gbm-dev/slmodem-sip-bridge/releases/latest/download/slmodem-sip-bridge-linux-x86_64" \
    && chmod +x /usr/local/bin/slmodemd /usr/local/bin/slmodem-sip-bridge

# Copy Go binaries from builder
COPY --from=go-builder /usr/local/bin/oob-hub /usr/local/bin/oob-hub
COPY --from=go-builder /usr/local/bin/oob-probe /usr/local/bin/oob-probe
COPY --from=go-builder /usr/local/bin/oob-manage /usr/local/bin/oob-manage

# Create directories
RUN mkdir -p /var/log/oob-sessions

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
# 5060 - SIP (UDP, used by bridge)
# 20000-20100 - RTP media (UDP, used by bridge)
EXPOSE 22/tcp 5060/udp 20000-20100/udp

# Docker-level health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD /usr/local/bin/oob-healthcheck.sh || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD ["/usr/bin/supervisord", "-n", "-c", "/etc/supervisor/supervisord.conf"]
