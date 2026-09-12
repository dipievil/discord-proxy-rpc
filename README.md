# discord-proxy-rpc

**Discord rich presence from all your devices on a LAN.**

A lightweight, read-only LAN proxy that connects to Discord's local IPC socket and exposes Rich Presence data over WebSocket and HTTP. Any device on your network can observe what game, activity, or status is active -- without needing Discord installed.

---

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Discord App Setup](#discord-app-setup)
- [Configuration Reference](#configuration-reference)
- [Dashboard](#dashboard)
- [API Reference](#api-reference)
- [Building from Source](#building-from-source)
- [Troubleshooting](#troubleshooting)
- [Security Considerations](#security-considerations)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

discord-proxy-rpc is a single-binary Go application that runs on the same machine as your Discord desktop client. It connects to Discord via IPC (Inter-Process Communication), reads the current Rich Presence activity, and broadcasts it to any connected clients on your local network.

**Key characteristics:**

- **Read-only** -- cannot modify Discord activity; only observes and broadcasts.
- **Single binary** -- no runtime dependencies, no database, no external files.
- **Zero-config discovery** -- mDNS (Bonjour/Avahi) announces the service automatically on LAN.
- **Embedded dashboard** -- a responsive web UI is built into the binary via `go:embed`.
- **Optional authentication** -- Bearer token auth for shared-secret protection.
- **Cross-platform** -- Linux (amd64/arm64), Windows (amd64), macOS (amd64/arm64).

**Runtime requirements:**

- Discord **desktop client** running and logged in (web Discord does not expose IPC).
- The proxy must run under the same OS user session as Discord.
- Linux: `XDG_RUNTIME_DIR` must be set (typical in graphical sessions).

---

## Architecture

```
+-------------------------------------------------------------------+
|  HOST (machine running Discord desktop client)                     |
|  +--------------+    +--------------+    +--------------------+   |
|  | Discord IPC  |<---| IPC Client   |<---| LAN Server         |   |
|  | (unix socket |    | (gopresence) |    | (WS + HTTP)        |   |
|  |  or named    |    |              |    |                    |   |
|  |  pipe)       |    |              |    |                    |   |
|  +--------------+    +--------------+    +----------+---------+   |
|                                                   |               |
|  +------------------------------------------------v-----------+   |
|  |  Presence State Machine                                    |   |
|  |  - Cache last presence payload                             |   |
|  |  - Diff detection (only broadcast on change)               |   |
|  |  - Coalesce updates: 5s window                             |   |
|  |  - Auto-reconnect with exponential backoff (5s -> 60s)     |   |
|  +------------------------------------------------------------+   |
+-------------------------------------------------------------------+
                              ^
                              | mDNS: _discord-proxy._tcp.local
                              v
+-------------------------------------------------------------------+
|  LAN CLIENTS (multiple)                                           |
|  +-------------+  +-------------+  +-------------+                |
|  | Dashboard   |  | CLI Tool    |  | Custom App  |  ...           |
|  | (Web SPA)   |  | (Go/other)  |  | (any WS)    |                |
|  +-------------+  +-------------+  +-------------+                |
+-------------------------------------------------------------------+
```

**Components:**

| Component | Package | Description |
|-----------|---------|-------------|
| IPC Client | `internal/ipc/` | Connects to Discord IPC sockets (`discord-ipc-0` through `discord-ipc-9`), handles handshake and reconnection with exponential backoff |
| State Machine | `internal/state/` | Caches presence, detects changes, coalesces rapid updates (5s window), dispatches to subscribers |
| LAN Server | `internal/server/` | HTTP server with WebSocket upgrade, REST API endpoints, embedded dashboard, optional Bearer token auth |
| mDNS Announcer | `internal/mdns/` | Advertises `_discord-proxy._tcp.local` via zeroconf for automatic LAN discovery |
| Config | `internal/config/` | Viper-based config with YAML files, environment variables, and CLI flags (priority: flags > env > YAML > defaults) |
| Dashboard | `web/` | Vanilla HTML/CSS/JS SPA embedded via `go:embed` -- no build step required |

---

## Quick Start

### Option 1: Download a release

1. Download the latest binary for your platform from [Releases](https://github.com/discord-proxy-rpc/discord-proxy-rpc/releases).
2. Create a `config.yaml` (see [Configuration Reference](#configuration-reference)):
   ```yaml
   discord:
     client_id: "YOUR_DISCORD_APP_ID"
   ```
3. Run the binary:
   ```bash
   ./discord-proxy-linux-amd64
   ```
4. Open `http://<your-host-ip>:8765` in any browser on your LAN.

### Option 2: Docker

```bash
docker run -d \
  -e PROXY_DISCORD_CLIENT_ID=YOUR_DISCORD_APP_ID \
  -p 8765:8765 \
  ghcr.io/discord-proxy-rpc/discord-proxy-rpc:latest
```

Note: The Docker container must share the host's IPC namespace to reach Discord sockets:
```bash
docker run -d \
  --ipc=host \
  -e PROXY_DISCORD_CLIENT_ID=YOUR_DISCORD_APP_ID \
  -p 8765:8765 \
  ghcr.io/discord-proxy-rpc/discord-proxy-rpc:latest
```

### Option 3: Build from source

```bash
git clone https://github.com/discord-proxy-rpc/discord-proxy-rpc.git
cd discord-proxy-rpc
make build
./bin/discord-proxy
```

---

## Discord App Setup

The proxy requires a Discord Application ID to authenticate via IPC. This takes about 2 minutes.

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications).
2. Click **New Application** and give it a name (e.g., "Discord Proxy RPC").
3. On the **General Information** tab, copy the **Application ID** (also called Client ID).
4. Paste this ID into your `config.yaml`:
   ```yaml
   discord:
     client_id: "123456789012345678"
   ```
   Or set it via environment variable:
   ```bash
   export PROXY_DISCORD_CLIENT_ID=123456789012345678
   ```
5. (Optional) Go to **Rich Presence > Art Assets** and upload images. Use their names as `large_image` / `small_image` values in your activity payloads.

**No bot, OAuth2, or redirect URI configuration is required.** Only the Application ID is needed.

---

## Configuration Reference

The proxy loads configuration from (highest to lowest priority):

1. CLI flags
2. Environment variables (prefix: `PROXY_`)
3. YAML config file (`config.yaml`)
4. Built-in defaults

### Config file search paths

- `./config.yaml` (current directory)
- `$HOME/.config/discord-proxy-rpc/config.yaml`
- `/etc/discord-proxy-rpc/config.yaml`

### Full config example

```yaml
discord:
  client_id: ""                          # REQUIRED - Discord Application ID
  auto_reconnect: true                   # Reconnect on IPC disconnect
  reconnect_base_interval: 5s            # Initial backoff interval
  max_reconnect_interval: 60s            # Maximum backoff cap
  health_check_interval: 30s             # PING interval to Discord IPC
  health_check_timeout: 10s              # Timeout for PONG response
  coalesce_interval: 5s                  # Buffer window for batching presence updates

server:
  host: "0.0.0.0"                        # Bind address
  port: 8765                             # Listen port
  ws_path: "/ws"                         # WebSocket upgrade path
  read_timeout: 10s                      # HTTP read timeout
  write_timeout: 10s                     # HTTP write timeout

auth:
  enabled: false                         # Enable Bearer token auth
  token: ""                              # Auth token (also via PROXY_TOKEN env)

mdns:
  enabled: true                          # Advertise via mDNS/Bonjour
  instance_name: ""                      # Service name (empty = hostname)
  service_type: "_discord-proxy._tcp"    # mDNS service type

logging:
  level: "info"                          # debug, info, warn, error
  format: "json"                         # json or console
```

### Environment variables

Every config key can be set via an environment variable with the `PROXY_` prefix and underscores instead of dots.

| Config Key | Env Variable | Default |
|------------|-------------|---------|
| `discord.client_id` | `PROXY_DISCORD_CLIENT_ID` | `""` |
| `discord.auto_reconnect` | `PROXY_DISCORD_AUTO_RECONNECT` | `true` |
| `discord.reconnect_base_interval` | `PROXY_DISCORD_RECONNECT_BASE_INTERVAL` | `5s` |
| `discord.max_reconnect_interval` | `PROXY_DISCORD_MAX_RECONNECT_INTERVAL` | `60s` |
| `discord.health_check_interval` | `PROXY_DISCORD_HEALTH_CHECK_INTERVAL` | `30s` |
| `discord.health_check_timeout` | `PROXY_DISCORD_HEALTH_CHECK_TIMEOUT` | `10s` |
| `discord.coalesce_interval` | `PROXY_DISCORD_COALESCE_INTERVAL` | `5s` |
| `server.host` | `PROXY_SERVER_HOST` | `0.0.0.0` |
| `server.port` | `PROXY_SERVER_PORT` | `8765` |
| `server.ws_path` | `PROXY_SERVER_WS_PATH` | `/ws` |
| `server.read_timeout` | `PROXY_SERVER_READ_TIMEOUT` | `10s` |
| `server.write_timeout` | `PROXY_SERVER_WRITE_TIMEOUT` | `10s` |
| `auth.enabled` | `PROXY_AUTH_ENABLED` | `false` |
| `auth.token` | `PROXY_AUTH_TOKEN` | `""` |
| `auth.token` | `PROXY_TOKEN` | `""` |
| `mdns.enabled` | `PROXY_MDNS_ENABLED` | `true` |
| `mdns.instance_name` | `PROXY_MDNS_INSTANCE_NAME` | `""` |
| `mdns.service_type` | `PROXY_MDNS_SERVICE_TYPE` | `_discord-proxy._tcp` |
| `logging.level` | `PROXY_LOGGING_LEVEL` | `info` |
| `logging.format` | `PROXY_LOGGING_FORMAT` | `json` |

The legacy `PROXY_TOKEN` env var is also supported as a shortcut for `auth.token`.

---

## Dashboard

The dashboard is an embedded web SPA served at the root path (`/`). It provides:

- Real-time presence display via WebSocket
- Connection status indicator (green = connected, red = disconnected, yellow = reconnecting)
- Activity details: type, state, timestamps (live countdown), assets, party, buttons
- "Copy JSON" button for debugging
- Responsive design (mobile-friendly)
- Dark/light theme (follows OS preference)

### Accessing the dashboard

| Method | URL |
|--------|-----|
| Local | `http://localhost:8765` |
| LAN | `http://<host-ip>:8765` |
| mDNS | `http://<instance-name>.local:8765` |

mDNS auto-discovery works out of the box on most networks. On Linux, ensure Avahi is running. On macOS, it is built-in. On Windows, install Bonjour Print Services or use mDNS manually.

---

## API Reference

### REST Endpoints

| Route | Method | Description |
|-------|--------|-------------|
| `/` | GET | Dashboard SPA |
| `/api/presence` | GET | Current presence as JSON snapshot |
| `/api/state` | GET | IPC connection state as JSON |
| `/health` | GET | Liveness probe, returns `200 OK` |
| `{ws_path}` | GET | WebSocket upgrade endpoint (default: `/ws`) |

### WebSocket Protocol

Connect to `ws://<host>:<host>:<host>:<host>:8765/ws` (or your configured `ws_path`).

**Authentication (if enabled):**

Include a Bearer token in the upgrade request header:
```
Authorization: Bearer <your-token>
```

**Client -> Server messages:**

Subscribe to events:
```json
{ "type": "subscribe", "events": ["presence", "state"] }
```

Request current state:
```json
{ "type": "get_current" }
```

**Server -> Client messages:**

Presence update (pushed on change):
```json
{
  "type": "presence",
  "payload": {
    "details": "In a match",
    "state": "Winning 3-1",
    "timestamps": { "start": 1234567890 },
    "assets": {
      "large_image": "game_logo",
      "large_text": "Game Name",
      "small_image": "rank_icon",
      "small_text": "Diamond II"
    },
    "party": { "id": "party123", "size": [3, 5] },
    "buttons": [{ "label": "Join Game", "url": "https://example.com" }],
    "type": 0
  }
}
```

Connection state change:
```json
{
  "type": "state",
  "status": "connected",
  "client_id": "123456789012345678"
}
```

Current state response:
```json
{
  "type": "current",
  "payload": { ... }
}
```

**Activity types:**

| Value | Label |
|-------|-------|
| 0 | Playing |
| 1 | Streaming |
| 2 | Listening |
| 3 | Watching |
| 4 | Custom |
| 5 | Competing |

---

## Building from Source

### Prerequisites

- Go 1.26.5 or later
- GCC (optional, for race detector in tests)

### Make commands

| Command | Description |
|---------|-------------|
| `make build` | Build for current platform |
| `make build-linux` | Build for Linux amd64 |
| `make build-windows` | Build for Windows amd64 |
| `make build-all` | Build for all platforms (Linux, Windows, macOS) |
| `make test` | Run all tests with coverage (race detector if GCC available) |
| `make lint` | Run golangci-lint (if installed) |
| `make run` | Run the proxy server |
| `make dev` | Run with hot reload via Air (if installed) |
| `make release` | Build and publish via GoReleaser |
| `make clean` | Remove build artifacts |

### Release process

Releases are automated via GitHub Actions + GoReleaser:

1. Push a tag matching `v*` (e.g., `v1.0.0`).
2. GitHub Actions builds binaries for Linux (amd64, arm64), Windows (amd64), and macOS (amd64, arm64).
3. Artifacts (binaries, checksums, release notes) are published to GitHub Releases.

### Docker build

```bash
docker build -t discord-proxy-rpc .
```

---

## Troubleshooting

### 1. "Could not connect to Discord IPC"

- Ensure the Discord desktop client is running and logged in.
- The proxy must run under the **same OS user** as Discord.
- On Linux, verify `XDG_RUNTIME_DIR` is set (`echo $XDG_RUNTIME_DIR`).
- Discord creates IPC sockets in `/run/user/<uid>/` (Linux) or `%TEMP%` (Windows).

### 2. Dashboard shows "Disconnected"

- Check that the proxy is running and port 8765 is not blocked.
- If using a firewall, allow inbound TCP on port 8765.
- Try `curl http://localhost:8765/health` -- should return `200 OK`.
- Open browser developer tools (F12) and check the Console for WebSocket errors.

### 3. mDNS not discovered by LAN clients

- Verify mDNS is enabled in config (`mdns.enabled: true`).
- On Linux, ensure Avahi is running: `systemctl status avahi-daemon`.
- On Windows, ensure Bonjour Print Services is installed.
- Try direct IP access as a fallback: `http://<host-ip>:8765`.

### 4. Presence not updating

- The proxy coalesces updates in a 5-second window by default. Wait at least 5s.
- Check logs for IPC disconnections (`logging.level: "debug"` for verbose output).
- Ensure the Discord activity is set via the Rich Presence API (not just a game detected by Discord).

### 5. Auth token rejected

- Verify `auth.enabled: true` and `auth.token` / `PROXY_TOKEN` match on both server and client.
- The token must be sent as `Authorization: Bearer <token>` on the WebSocket upgrade request.
- Check for trailing whitespace or newline characters in the token.

---

## Security Considerations

- **Read-only by design** -- The proxy never sends `SET_ACTIVITY` or any write command to Discord. It only observes.
- **LAN only** -- Binds to `0.0.0.0` by default. Restrict via firewall rules or bind to `127.0.0.1` and use a VPN (e.g., Tailscale) for remote access.
- **Optional token auth** -- Enable `auth.enabled: true` and set a strong token via `PROXY_TOKEN`. Without auth, any device on the LAN can connect.
- **No persistence** -- No database, no log files, no presence data stored on disk. Everything is in-memory and lost on restart.
- **mDNS is informational only** -- The mDNS TXT record contains only the API version (`v=1`). No sensitive data is broadcast.
- **WebSocket traffic is plaintext** -- For encrypted transport, place the proxy behind a TLS-terminating reverse proxy (e.g., nginx, Caddy).

---

## Contributing

1. Fork the repository.
2. Create a feature branch from `dev`: `git checkout -b feature/my-change dev`.
3. Make your changes following the existing code style.
4. Add or update tests for any new functionality.
5. Run `make test` and `make lint` to verify.
6. Commit with a [conventional commit](https://www.conventionalcommits.org/) message (e.g., `feat: add ...`, `fix: resolve ...`, `docs: update ...`).
7. Push and open a Pull Request against `dev`.

All contributions are welcome -- bug reports, feature requests, documentation improvements, and code.

---

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
