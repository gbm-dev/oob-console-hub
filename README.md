# POTS - OOB Console Hub

Dockerized out-of-band console access over PSTN. Admins SSH in, pick a site from a modern TUI menu, and get dropped into a live modem session routed through Asterisk + Telnyx SIP.

Built with Go using the [Charm](https://charm.sh) ecosystem (Wish + Bubble Tea + Lip Gloss) for a single-binary SSH server with native modem handling and built-in user management.

## Quick Start

```bash
git clone https://github.com/gbm-dev/oob-console-hub.git
cd oob-console-hub
cp .env.example .env
```

Edit credentials and sites:

```bash
nano .env                       # Telnyx SIP creds + outbound caller ID
nano config/oob-sites.conf      # Remote sites
```

Build and start:

```bash
docker compose build
docker compose up -d
```

## Updating

Pull the latest and rebuild:

```bash
cd /opt/oob-console-hub
git pull
docker compose build
docker compose down && docker compose up -d
```

Pin a specific release version in the build:

```bash
docker compose build --build-arg POTS_VERSION=v1.0.0
```

Go binaries are built by GitHub Actions and downloaded from [releases](https://github.com/gbm-dev/oob-console-hub/releases) during `docker compose build` — no Go toolchain needed on the server.

### Telnyx Caller ID Requirement

Set `TELNYX_OUTBOUND_CID` in `.env` to a valid number on your Telnyx account (typically E.164, e.g. `+15551234567`).

If this is missing or invalid, Telnyx can reject calls with errors like:

`403 Caller Origination Number is Invalid`

## Site Configuration

Edit `config/oob-sites.conf` — one line per remote device:

```
# name|phone_number|description|baud_rate
2broadway|14105551234|2 Broadway Terminal Server|19200
router1|13125559876|Chicago Core Router|9600
```

The phone number is the PSTN line connected to the modem/console server at the remote site.

## User Management

Run inside the container:

```bash
docker exec oob-console-hub oob-manage add first.last
docker exec oob-console-hub oob-manage list
docker exec oob-console-hub oob-manage reset first.last
docker exec oob-console-hub oob-manage lock first.last
docker exec oob-console-hub oob-manage unlock first.last
docker exec oob-console-hub oob-manage remove first.last
```

Users get a temporary password and must change it on first login. The SSH server drops them directly into the TUI — no shell access.

## Connecting

```bash
ssh first.last@<server-ip> -p 2222
```

Select a site from the menu, auto-dials via modem, live session begins. Press Enter then `~.` to disconnect (same as SSH escape). Session logs are saved to `logs/`.

## Monitoring

```bash
docker ps --filter name=oob-console-hub
docker logs -f oob-console-hub
docker exec oob-console-hub oob-healthcheck.sh --verbose
```

Docker runs `oob-healthcheck.sh` every 30 seconds and marks the container healthy/unhealthy.

## Architecture

```
Admin SSH (:2222) → Wish/Bubble Tea TUI → /dev/ttySL0 → slmodemd (-e bridge) → Asterisk ARI/ExternalMedia → Telnyx SIP → PSTN → Remote Device
```

- **oob-hub**: Go binary — Wish SSH server + Bubble Tea TUI + modem pool + user store
- **oob-manage**: Go binary — CLI for user management (add/remove/list/lock/unlock/reset)
- **tini + supervisord**: PID 1 and process supervision for `slmodemd`, Asterisk, and `oob-hub`
- **slmodemd**: software modem daemon exposing `/dev/ttySL0`
- **slmodem-asterisk-bridge**: external helper invoked by `slmodemd -e` to relay modem audio via ARI External Media
- **Asterisk**: PJSIP trunk to Telnyx plus ARI control/media endpoints
- **User store**: `users.json` with bcrypt hashing, atomic writes, file locking

## Development

```bash
go test ./internal/...    # Run all unit tests
go build ./cmd/...        # Build binaries
```
