# discord-proxy-rpc

**Discord rich presence from all your devices on a LAN.**

A lightweight, read-only LAN proxy that connects to Discord's local IPC socket and exposes Rich Presence data over WebSocket and HTTP. Any device on your network can observe what game, activity, or status is active -- without needing Discord installed.

---

## Table of Contents

- [Overview](#overview)
- [Activity source](#activity-source)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Discord App Setup](#discord-app-setup)
- [Configuration Reference](#configuration-reference)
- [Dashboard](#dashboard)
- [TUI Dashboard](#tui-dashboard)
- [System Tray Indicator](#system-tray-indicator)
- [API Reference](#api-reference)
- [Building from Source](#building-from-source)
- [Troubleshooting](#troubleshooting)
- [Security Considerations](#security-considerations)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

discord-proxy-rpc is a single-binary Go application that runs on the same machine as your Discord desktop client. It connects to Discord via IPC (Inter-Process Communication) and re-broadcasts Rich Presence activity to any connected clients on your local network.

> **Activity source:** Discord's local IPC socket lets a client *set* its own Rich Presence but exposes **no way to read another application's presence**, and Discord does not push `ACTIVITY` frames to a client like this proxy. The proxy therefore has **no activity source today** and reports that explicitly (`source: "none"`) instead of showing a misleading empty feed. See [Activity source](#activity-source).

**Key characteristics:**

- **Read-only** -- cannot modify Discord activity; only observes and broadcasts.
- **Explicit, honest state** -- presence carries a `source` field, so clients can tell "no source wired up" from "a source reported nothing".
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

## Activity source

The proxy is meant to read the Discord desktop client's Rich Presence and re-broadcast it, but the IPC socket cannot deliver that data:

- `discord-ipc-*` carries RPC requests from a client to local Discord. The only activity-related command is `SET_ACTIVITY` (a client publishing *its own* presence). There is no `GET_ACTIVITY`, and Discord does not dispatch another application's `ACTIVITY` as an unsolicited frame.
- The proxy's IPC layer still handshakes, keeps the connection alive, and can parse an `ACTIVITY` frame if one ever arrives, so it is ready the moment a real source exists. Until then every snapshot is reported with `source: "none"`.

Presence is therefore wrapped in a small snapshot envelope:

```json
{ "source": "none", "activity": null }
```

| `source` | Meaning |
|----------|---------|
| `"none"` | No activity source. `activity` is always `null`. Expected today. |
| `"ipc"`  | The activity came from a Discord IPC `ACTIVITY` frame; `activity` is non-null. |

This envelope is what `/api/presence` and the WebSocket `presence` / `current` messages carry. The dashboard reads `source` and shows a "No activity source" notice instead of an empty activity card.

### Proving it on your host (capture harness)

`cmd/ipc-capture` records every raw frame Discord's IPC socket sends for a bounded window and reports whether any were `ACTIVITY` frames. Run it on the machine with the Discord desktop client while a game is open:

```bash
go run ./cmd/ipc-capture -client-id <your-discord-app-id> -duration 60s -out capture.jsonl
```

The summary ends with `No ACTIVITY frame was observed…` (confirming the limitation) or `Found N ACTIVITY frame(s)…`. If a source does exist on your setup, share `capture.jsonl` so the `ACTIVITY` frame can be wired in.

### Finding proxies on the LAN (discovery helper)

`cmd/discover` browses for the proxy's mDNS advertisement (`_discord-proxy._tcp.local`) and prints the endpoints it finds:

```bash
go run ./cmd/discover -timeout 5s        # table of instance, host, address, port, TXT
go run ./cmd/discover -timeout 5s -json  # machine-readable: {"count":N,"proxies":[...]}
```

Link-local IPv6 addresses are shown as a count (`(+N link-local)`) because they need a zone to be dialable; the JSON output carries them in `link_local`. The helper exits `0` even when nothing is found — parse the output, not the exit status, to tell an empty LAN from a populated one. `1` means the browse or the write failed, `2` a usage error such as a non-positive `-timeout`.

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

1. Download the latest `.zip` for your platform from [Releases](https://github.com/dipievil/discord-proxy-rpc/releases) and unzip it. The archive also contains `discover`, `LICENSE` and `README.md`; Linux users who want the tray grab the separate `discord-proxy-rpc-tray_<version>_linux_<arch>.zip`. `checksums.txt` on the release page lists the SHA-256 of every archive.
2. Copy the bundled `config.example.yaml` to `config.yaml` next to the binary, then set `discord.client_id` to your Discord Application ID (see [Configuration Reference](#configuration-reference)). Every other key already has a working default.
3. Run the binary:
   ```bash
   ./discord-proxy
   ```
4. Open `http://<your-host-ip>:8765` in any browser on your LAN.

### Option 2: Docker

The image runs as a non-root user. To reach Discord's IPC socket it must run as **your desktop-session user** and have that session's runtime directory mounted, otherwise the proxy serves HTTP but can never connect to Discord.

Build (the `APP_UID`/`APP_GID` build args bake your host user into the image):

```bash
docker build --build-arg APP_UID=$(id -u) --build-arg APP_GID=$(id -g) -t discord-proxy .
```

Run:

```bash
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e XDG_RUNTIME_DIR=/run/user/$(id -u) \
  -v "/run/user/$(id -u):/run/user/$(id -u):ro" \
  -v "$PWD/config.yaml:/etc/discord-proxy-rpc/config.yaml:ro" \
  -p 8765:8765 \
  discord-proxy
```

- `--user` matches the container process to the host user that owns Discord's socket (it overrides the image default); the `APP_UID`/`APP_GID` build args are an alternative if you prefer baking it in.
- The runtime directory (`$XDG_RUNTIME_DIR`, usually `/run/user/<uid>`) is mounted read-only — Discord creates `discord-ipc-0` … `discord-ipc-9` there and only the owning UID may connect.
- `config.yaml` is mounted read-only at `/etc/discord-proxy-rpc/`, one of the built-in config search paths. Alternatively pass `-e PROXY_DISCORD_CLIENT_ID=<id>`.
- If you skip the runtime-directory mount, the container can still serve the dashboard but reports `disconnected` for IPC.

### Option 3: Build from source

```bash
git clone https://github.com/dipievil/discord-proxy-rpc.git
cd discord-proxy-rpc
make build
./bin/discord-proxy -version   # prints version, commit and build date
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

1. Environment variables (prefix: `PROXY_`)
2. YAML config file (`config.yaml`)
3. Built-in defaults

The only command-line flag is `-version`, which prints the version, commit and build date and exits.

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
  max_ws_connections: 100                # Cap on concurrent WebSocket clients
  rate_limit_per_ip: 60                  # Max requests per IP within the window (dashboard, REST, WS)
  rate_limit_window: 1m                  # Rate-limit window
  trusted_proxies: []                    # CIDRs whose X-Forwarded-For is honoured (empty = RemoteAddr only)
  tls:
    enabled: false                       # Serve HTTPS/WSS instead of HTTP/WS
    cert_file: ""                        # PEM server certificate (chain)
    key_file: ""                         # PEM private key for cert_file
    client_ca_file: ""                   # PEM CA bundle; set = require and verify client certs (mTLS)

auth:
  enabled: false                         # Enable Bearer token auth
  token: ""                              # Auth token (also via PROXY_AUTH_TOKEN env)

mdns:
  enabled: true                          # Advertise via mDNS/Bonjour
  instance_name: ""                      # Service name (empty = hostname)
  service_type: "_discord-proxy._tcp"    # mDNS service type

logging:
  level: "info"                          # debug, info, warn, error
  format: "json"                         # json or console

metrics:
  enabled: false                         # Expose Prometheus metrics on /metrics
```

### Environment variables

Every config key can be set via an environment variable with the `PROXY_` prefix and underscores instead of dots.

| Config Key | Env Variable | Default |
|------------|-------------|---------|
| `discord.client_id` | `PROXY_DISCORD_CLIENT_ID` | (required — the proxy refuses to start without it) |
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
| `server.max_ws_connections` | `PROXY_SERVER_MAX_WS_CONNECTIONS` | `100` |
| `server.rate_limit_per_ip` | `PROXY_SERVER_RATE_LIMIT_PER_IP` | `60` |
| `server.rate_limit_window` | `PROXY_SERVER_RATE_LIMIT_WINDOW` | `1m` |
| `server.trusted_proxies` | `PROXY_SERVER_TRUSTED_PROXIES` | `""` (comma-separated CIDRs; empty = use the TCP peer) |
| `server.tls.enabled` | `PROXY_SERVER_TLS_ENABLED` | `false` |
| `server.tls.cert_file` | `PROXY_SERVER_TLS_CERT_FILE` | `""` |
| `server.tls.key_file` | `PROXY_SERVER_TLS_KEY_FILE` | `""` |
| `server.tls.client_ca_file` | `PROXY_SERVER_TLS_CLIENT_CA_FILE` | `""` |
| `auth.enabled` | `PROXY_AUTH_ENABLED` | `false` |
| `auth.token` | `PROXY_AUTH_TOKEN` | `""` |
| `mdns.enabled` | `PROXY_MDNS_ENABLED` | `true` |
| `mdns.instance_name` | `PROXY_MDNS_INSTANCE_NAME` | `""` |
| `mdns.service_type` | `PROXY_MDNS_SERVICE_TYPE` | `_discord-proxy._tcp` |
| `logging.level` | `PROXY_LOGGING_LEVEL` | `info` |
| `logging.format` | `PROXY_LOGGING_FORMAT` | `json` |
| `metrics.enabled` | `PROXY_METRICS_ENABLED` | `false` |

> **Breaking change**: the legacy `PROXY_TOKEN` env var was removed. `PROXY_AUTH_TOKEN` is now the sole env source for `auth.token` — update any scripts or containers still setting `PROXY_TOKEN`.

### TLS and mTLS

The proxy terminates TLS itself. One listener serves the dashboard, the REST API, `/ws`, `/metrics` and the health probes; enabling TLS switches all of them to HTTPS/WSS at once. TLS 1.2 is the minimum version.

```yaml
server:
  tls:
    enabled: true
    cert_file: /etc/discord-proxy-rpc/tls/server.crt   # PEM chain, leaf first
    key_file: /etc/discord-proxy-rpc/tls/server.key    # PEM private key
    client_ca_file: ""                                 # optional: enables mTLS
```

The same settings can come from the environment:

```bash
PROXY_SERVER_TLS_ENABLED=true
PROXY_SERVER_TLS_CERT_FILE=/etc/discord-proxy-rpc/tls/server.crt
PROXY_SERVER_TLS_KEY_FILE=/etc/discord-proxy-rpc/tls/server.key
PROXY_SERVER_TLS_CLIENT_CA_FILE=/etc/discord-proxy-rpc/tls/clients-ca.crt
```

**Server-side TLS only** (`client_ca_file` empty): clients verify the server certificate; nothing else changes. Bearer-token auth (`auth.enabled`) still works and is optional.

**Mutual TLS** (`client_ca_file` set): the listener uses `RequireAndVerifyClientCert`, so every connection — including `/health` and `/metrics` — must present a client certificate that chains to that CA. Clients without a certificate, or with one from another CA, are rejected during the TLS handshake, before any HTTP route runs. The WebSocket handler additionally checks `r.TLS.VerifiedChains` as defence in depth: a certificate that was merely presented by a listener configured `RequestClientCert` is not enough.

Mutual TLS and bearer-token auth are **mutually exclusive**: a client certificate is the only supported identity when `client_ca_file` is set. Configuring both fails at startup with an error naming the conflicting keys, never with an ambiguous half-enabled state.

Validation is fail-fast at load time, not at first request:

- `server.tls.enabled: true` requires both `cert_file` and `key_file`.
- A certificate/key/CA path set while `server.tls.enabled` is `false` is rejected instead of silently serving plaintext.
- An unreadable file, a cert/key mismatch or a PEM file without certificates aborts startup with the offending path in the error.

Examples with mTLS enabled:

```bash
curl --cacert ca.crt --cert client.crt --key client.key https://proxy.local:8765/health
# WebSocket clients (wscat, Node, Go) pass the same pair as their TLS client certificate
wscat -c wss://proxy.local:8765/ws --ca ca.crt --cert client.crt --key client.key
```

The client certificate must carry the `clientAuth` extended key usage; a certificate issued only for `serverAuth` is rejected.

**Bundled clients:** the dashboard picks `wss://` from the page scheme automatically. The TUI (`-addr`) and the example consumer accept a `wss://` URL, but they verify with the system trust store and have no option to present a client certificate, so they work with server-side TLS only — use `curl`/`wscat` or an external client for mTLS.

**mDNS:** DNS-SD has no scheme field, so when TLS is on the advertised TXT record gains `tls=server` or `tls=mtls` (alongside `v=1`). Discovery clients must treat those instances as `https`/`wss`; without the key the endpoint is plain `http`/`ws`. The announce still carries only the API version and mode — no tokens, hostnames or certificate material.

---

## Dashboard

The dashboard is an embedded web SPA served at the root path (`/`). It provides:

- Real-time presence display via WebSocket
- Connection status indicator (green = connected, red = disconnected, yellow = reconnecting)
- Activity source notice ("No activity source" when the proxy has none, instead of an empty activity card)
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

When `server.tls.enabled` is on, every URL above uses `https://`, and the WebSocket uses `wss://`. The browser must trust the server certificate; with mTLS enabled it must also present a client certificate, so import the client pair into the browser or use a reverse proxy in front of the proxy.

mDNS auto-discovery works out of the box on most networks. On Linux, ensure Avahi is running. On macOS, it is built-in. On Windows, install Bonjour Print Services or use mDNS manually. With TLS enabled the advertised TXT record carries `tls=server` or `tls=mtls`; honour it and connect with `https`/`wss`.

When `auth.enabled` is true, the dashboard cannot send an `Authorization` header. Open the dashboard once with the token in the URL — `http://<host>:8765/?token=<your-token>` — and it stores the token in `sessionStorage`, strips it from the address bar, and authenticates the WebSocket with it on every (re)connect. The same request sets a short-lived (15 min) `HttpOnly` cookie so the page's static assets, and a reload within that window, load without the token in the URL. The token is scoped to that tab and discarded when it closes; other API clients still send their own `Authorization` header or `?token=`.

---

## TUI Dashboard

The same WebSocket API is also available as a terminal dashboard, so hosts without a browser can watch the proxy. The TUI (`cmd/tui`) renders the current presence snapshot, the WebSocket and Discord IPC connection health, and reconnects with exponential backoff on disconnect.

Build it with:

```bash
make tui    # produces bin/discord-proxy-tui
```

Run it:

```bash
# Against a local proxy with no auth
bin/discord-proxy-tui

# Remote proxy with authentication
bin/discord-proxy-tui -addr 192.168.0.103:8765 -token <your-token>

# Or via environment variables
PROXY_HOST=192.168.0.103 PROXY_PORT=8765 PROXY_AUTH_TOKEN=<your-token> bin/discord-proxy-tui
```

| Flag | Env | Default |
|------|-----|---------|
| `-addr` | `PROXY_HOST` + `PROXY_PORT` (or `PROXY_SERVER_PORT`) | `127.0.0.1:8765` |
| `-token` | `PROXY_TOKEN` (legacy, wins) / `PROXY_AUTH_TOKEN` | none |
| `-ws-path` | `PROXY_SERVER_WS_PATH` | `/ws` |

`-addr` also accepts a full `ws://` or `wss://` URL, which is used as-is. Press `q`, `Esc` or `Ctrl+C` to quit. Like the web dashboard, the TUI shows an explicit "No activity source" notice while the proxy has no activity source (see [Activity source](#activity-source)).

---

## System Tray Indicator

On Linux desktops, `discord-proxy-tray` puts a status icon in the system tray so you can see at a glance whether the proxy can reach Discord. It publishes a **StatusNotifierItem** on the D-Bus session bus — the freedesktop/KDE tray protocol used by KDE, Xfce and GNOME's AppIndicator extension — so it needs no GTK, no cgo and no icon theme. The status icons are drawn at runtime, so the binary carries no asset files either.

Build it with:

```bash
make tray    # produces bin/discord-proxy-tray
```

Run it next to a proxy:

```bash
# Local proxy without auth
bin/discord-proxy-tray

# Remote proxy with a token: keep it in the environment, not in argv
PROXY_URL=http://192.168.0.103:8765 PROXY_AUTH_TOKEN=<your-token> bin/discord-proxy-tray

# HTTPS proxy with a private CA (self-signed or internal CA)
PROXY_AUTH_TOKEN=<your-token> bin/discord-proxy-tray \
  -server https://192.168.0.103:8765 -cacert /etc/discord-proxy-rpc/ca.pem

# mTLS proxy: no bearer token, present a client certificate instead
bin/discord-proxy-tray -server https://192.168.0.103:8765 \
  -cacert /etc/discord-proxy-rpc/ca.pem \
  -cert ~/.config/discord-proxy-rpc/tray.pem \
  -key ~/.config/discord-proxy-rpc/tray-key.pem
```

A `-token` on the command line is readable by every local user through `ps` and lands in shell history, so prefer `PROXY_AUTH_TOKEN`. `-cacert` replaces the system roots with that file; use it when the proxy certificate is self-signed or issued by an internal CA. `-cert`/`-key` are only needed when the proxy requires mutual TLS.

It polls `GET /api/state` (default every 5 seconds) and changes the icon as soon as the state changes:

| State | Icon | Tooltip |
|-------|------|---------|
| `connected` | green disc, check mark | `Discord Proxy RPC — Discord IPC connected` |
| `reconnecting` | amber disc, minus | `Discord Proxy RPC — Discord IPC reconnecting` |
| `disconnected` | red disc, slash | `Discord Proxy RPC — Discord IPC disconnected` |
| proxy unreachable | red disc, slash | `Discord Proxy RPC — proxy unreachable` |

The proxy reports `connecting` while a Discord dial is in progress; the tray shows it as reconnecting, matching the dashboard's mapping.

| Flag | Env | Default |
|------|-----|---------|
| `-server` | `PROXY_URL` | `http://127.0.0.1:8765` |
| `-token` | `PROXY_AUTH_TOKEN` | none (prefer the env var) |
| `-cacert` | — | system roots |
| `-cert`, `-key` | — | none (no client certificate) |
| `-interval` | — | `5s` (minimum `2s`, maximum `10m`) |
| `-version` | — | prints the tray version |

The tray is Linux-only for now. On Windows and macOS the binary still builds, but exits with `tray indicator is not supported on this platform yet`. Press `Ctrl+C` or send `SIGTERM` to stop it. If the icon does not appear, make sure the desktop session exports `DBUS_SESSION_BUS_ADDRESS` (a normal graphical login does) and that a StatusNotifierWatcher is running.

---

## API Reference

### REST Endpoints

| Route | Method | Description | Response (200) | Response (503) |
|-------|--------|-------------|----------------|----------------|
| `/` | GET | Dashboard SPA | `text/html` | — |
| `/api/presence` | GET | Current presence snapshot | `{"source":"none\|ipc","activity":Activity\|null}` | — |
| `/api/state` | GET | IPC connection state as JSON | `{"status":"connected\|connecting\|reconnecting\|disconnected"}` | — |
| `/health` | GET | **Rich liveness probe** — includes IPC state | `{"status":"ok","ipc":"<state>"}` | — |
| `/healthz` | GET | **Minimal liveness probe** (k8s-compatible) | `{"status":"ok"}` | — |
| `/readyz` | GET | **Readiness probe** — reflects IPC connectivity | `{"status":"ok","ipc":"connected"}` | `{"status":"not ready","ipc":"disconnected"}` |
| `{ws_path}` | GET | WebSocket upgrade endpoint (default: `/ws`) | `101 Switching Protocols` | — |

**Endpoint rationale:**
- `/health` — Rich variant for debugging/monitoring; always returns 200 with IPC state embedded
- `/healthz` — Minimal, spec-conformant liveness probe for Kubernetes; always returns 200 if process is alive
- `/readyz` — Readiness probe for Kubernetes; returns 200 only when IPC is connected, 503 otherwise

**Access control:** with `auth.enabled: true`, `/`, `/index.html`, the static assets, `/api/presence` and `/api/state` require the token (`Authorization: Bearer <token>` or `?token=<token>`); an unauthenticated request gets HTTP 401 — a JSON `AUTH_001` body on `/api/*`, plain text on the dashboard. `/health`, `/healthz` and `/readyz` are never auth-gated.

**Rate limiting:** the per-IP limiter (`server.rate_limit_per_ip` / `server.rate_limit_window`, defaults 60 requests per minute) covers the dashboard and its static assets, `/api/presence`, `/api/state` and the WebSocket upgrade. All of them share one budget per client IP, and the check runs before auth, so a flood is rejected before the token comparison. A request over the limit gets HTTP 429 with a `Retry-After` header carrying the remaining seconds of the window and `Cache-Control: no-store` so a cached asset response cannot replay the rejection; REST and WebSocket routes answer the `REQ_002` JSON envelope, while the dashboard answers plain text because the browser renders that body directly. The first rejection from an IP in a window is logged at warning level and later ones at debug, so a reconnecting tab cannot flood the log; `discord_proxy_rate_limit_rejections_total` carries the exact count. `/health`, `/healthz`, `/readyz` and `/metrics` are exempt so container probes and Prometheus scrapes keep answering while an IP is limited. Setting `server.rate_limit_per_ip` to `0` or a negative value disables limiting (a non-positive window does too). A dashboard page load costs four requests (HTML, CSS, JS and the WebSocket upgrade), so the default budget covers several reloads and reconnect attempts from one IP.

**Behind a reverse proxy or ingress:** the limiter keys on the TCP peer by default. With a proxy in front, every client would share the proxy's budget, so list the proxy's addresses in `server.trusted_proxies` (CIDR blocks, e.g. `["10.0.0.0/8"]`); the last `X-Forwarded-For` hop that is not itself trusted then becomes the client. An empty list ignores forwarding headers entirely, so a client cannot forge its identity by sending one.

With `server.tls.client_ca_file` set (mTLS), every route, health probes included, requires a client certificate; unauthenticated access is impossible because the TLS handshake fails first. With server-side TLS only, the bearer-token rules above apply unchanged, over HTTPS. Bearer auth and mTLS cannot be combined.

#### Kubernetes Probes

For Kubernetes deployments, use `/healthz` for liveness and `/readyz` for readiness:

```yaml
livenessProbe:
  httpGet:
    scheme: HTTPS       # use HTTP only when TLS is disabled; add client cert material for mTLS
    path: /healthz
    port: 8765
  initialDelaySeconds: 10
  periodSeconds: 30
  timeoutSeconds: 5
  failureThreshold: 3

readinessProbe:
  httpGet:
    scheme: HTTPS
    path: /readyz
    port: 8765
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3
```

- **Liveness** (`/healthz`): Process is alive. Restarts container if probe fails.
- **Readiness** (`/readyz`): Discord IPC is connected. Removes pod from Service endpoints if probe fails (traffic stops routing until Discord reconnects).

The probes stay unauthenticated by design, but they are not exempt from transport security: with TLS enabled they are HTTPS-only, and with mTLS the kubelet must be able to present a client certificate, which most probe configurations cannot do — in that mode probe from inside the pod over a trusted connection or terminate mTLS in front of the proxy.

### WebSocket Protocol

Connect to `ws://<host>:8765/ws` (or your configured `ws_path`), or `wss://` when TLS is enabled.

**Authentication (if enabled):**

Include a Bearer token in the upgrade request header:
```
Authorization: Bearer <your-token>
```

Browser clients (such as the dashboard), which cannot set request headers, send the token as a WebSocket subprotocol instead:
```
Sec-WebSocket-Protocol: proxy-token-<base64url(token)>
```
The dashboard produces this automatically when given `?token=<your-token>` in the page URL. For plain HTTP requests (REST and the dashboard itself) the token may also be supplied as `?token=<token>`; a successful query-token request sets a short-lived auth cookie so the page's static assets load without it.

With mTLS (`server.tls.client_ca_file` set) there is no token: the upgrade must arrive over a TLS connection whose client certificate chains to the configured CA. A missing or untrusted certificate fails the TLS handshake; if a handler is reached without one anyway, it answers HTTP 401 `AUTH_001`.

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

Presence update (pushed on change). `payload` is a presence snapshot — `activity` is `null` while there is no activity source (see [Activity source](#activity-source)):
```json
{
  "type": "presence",
  "payload": {
    "source": "ipc",
    "activity": {
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
}
```

With no activity source yet, the same message is:
```json
{ "type": "presence", "payload": { "source": "none", "activity": null } }
```

Connection state change:
```json
{
  "type": "state",
  "status": "connected",
  "client_id": "123456789012345678"
}
```

Current state response (`payload` is the same presence snapshot shape):
```json
{
  "type": "current",
  "payload": { "source": "none", "activity": null },
  "client_id": "123456789012345678"
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

### Example Consumer

`examples/consumer/` contains a minimal, copy-pasteable CLI client that shows how to consume the WebSocket API from any LAN machine:

```bash
go run ./examples/consumer -addr ws://127.0.0.1:8765/ws -token <your-token>
# or, with the token in the environment:
PROXY_AUTH_TOKEN=<your-token> go run ./examples/consumer
```

It authenticates with a Bearer header, subscribes to `presence`/`state`/`current`, requests the current snapshot on connect, prints every update as it arrives, reconnects with exponential backoff if the proxy restarts, and exits cleanly on `Ctrl-C`:

```
connected to ws://127.0.0.1:8765/ws
current: no activity source (none)
state: discord connected (client 123456789012345678)
```

Each line is the human-readable rendering of one server message, including the snapshot's `source` field (see [Activity source](#activity-source)). With a real activity source wired up, updates render as:

```
presence: In a match | Winning 3-1 [Playing] party 3/5 (source: ipc)
```

The `-token` flag is only needed when the proxy runs with `auth.enabled: true`; the example intentionally duplicates the small wire-protocol structs instead of importing this module's internals so it can be pasted into any Go project as-is.

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
| `make e2e` | Run end-to-end tests (requires GCC for race detector) |
| `make capture` | Record raw Discord IPC frames (`CLIENT_ID=... [DURATION=60s] [OUT=capture.jsonl]`) |
| `make discover` | Browse the LAN for running proxies (`[TIMEOUT=5s] [JSON=1]`) |
| `make tray` | Build the Linux system-tray indicator (`bin/discord-proxy-tray`) |

### Release process

Releases are automated via GitHub Actions using semantic-release:

1. Push to `main` triggers the release workflow.
2. The `version` job runs semantic-release to determine the version bump (based on conventional commit messages in squash-merged PRs), creates the Git tag and publishes the GitHub Release with the generated notes.
3. If a new release is detected, the `goreleaser` job cross-compiles both binaries for five targets (Linux amd64/arm64, Windows amd64, macOS amd64/arm64) and uploads one `.zip` per target.
4. Each zip holds `discord-proxy`, `discover`, `config.example.yaml`, `LICENSE` and `README.md`; `checksums.txt` carries the SHA-256 of every archive.
5. GoReleaser runs with `release.mode: append`, so the assets attach to the release semantic-release already published instead of failing on an existing release.

### Docker build

```bash
docker build -t discord-proxy .
```

Multi-stage build: `golang:1.26-alpine` compiles a static binary, `alpine:3.20` runs it as the non-root `appuser`. Pass `--build-arg APP_UID=$(id -u) --build-arg APP_GID=$(id -g)` so the image user matches the host session that runs Discord (required for IPC; see [Quick Start](#option-2-docker) for the run command and mounts).

---

## Troubleshooting

### 1. "Could not connect to Discord IPC"

- Ensure the Discord desktop client is running and logged in.
- The proxy must run under the **same OS user** as Discord.
- On Linux, verify `XDG_RUNTIME_DIR` is set (`echo $XDG_RUNTIME_DIR`).
- Discord creates IPC sockets in `/run/user/<uid>/` (Linux) or `%TEMP%` (Windows).
- In Docker: run with `--user "$(id -u):$(id -g)"` and mount the host runtime directory (`-v "/run/user/$(id -u):/run/user/$(id -u):ro"` with `XDG_RUNTIME_DIR` set) — a mismatched UID cannot connect to the socket.

### 2. Dashboard shows "Disconnected"

- Check that the proxy is running and port 8765 is not blocked.
- If using a firewall, allow inbound TCP on port 8765.
- Try `curl http://localhost:8765/health` — should return `200 OK` with IPC state.
- Try `curl http://localhost:8765/healthz` — minimal liveness check, returns `200 OK`.
- Try `curl http://localhost:8765/readyz` — readiness check, returns `200 OK` if IPC connected, `503` if disconnected.
- Open browser developer tools (F12) and check the Console for WebSocket errors.

### 3. mDNS not discovered by LAN clients

- Verify mDNS is enabled in config (`mdns.enabled: true`).
- On Linux, ensure Avahi is running: `systemctl status avahi-daemon`.
- On Windows, ensure Bonjour Print Services is installed.
- Try direct IP access as a fallback: `http://<host-ip>:8765`.

### 4. Dashboard shows "No activity source"

This is expected today. Discord's local IPC socket does not expose another application's Rich Presence, so the proxy has no source to read and reports `source: "none"`.

- Confirm with the capture harness on the host running Discord while a game is open:
  ```bash
  go run ./cmd/ipc-capture -client-id <your-discord-app-id> -duration 60s -out capture.jsonl
  ```
  `No ACTIVITY frame was observed…` confirms there is no IPC source on your setup.
- Verify the snapshot directly: `curl http://localhost:8765/api/presence` returns `{"source":"none","activity":null}`.
- If the harness *does* report `ACTIVITY` frames, please share `capture.jsonl` in an issue so the source can be wired in.

### 5. Presence not updating after a source exists

- If a snapshot reports `source: "ipc"`, the proxy coalesces updates in a 5-second window by default — wait at least 5s.
- Check logs for IPC disconnections (`logging.level: "debug"` for verbose output).

### 6. Auth token rejected

- Verify `auth.enabled: true` and `auth.token` / `PROXY_AUTH_TOKEN` match on both server and client (the legacy `PROXY_TOKEN` env var is no longer read).
- For REST and dashboard requests, send the token as `Authorization: Bearer <token>`, as `?token=<token>`, or open the dashboard with `?token=<token>` in the URL.
- The WebSocket upgrade takes it as `Authorization: Bearer <token>` or the `proxy-token-<base64url(token)>` subprotocol.
- Check for trailing whitespace or newline characters in the token.

### 7. TLS or mTLS connection fails

- `tls: failed to verify certificate` on the client: the client does not trust the issuer of `server.tls.cert_file`. Point the client at the CA (`curl --cacert`, browser trust store) or use a certificate the client already trusts.
- `remote error: tls: certificate required` or `bad certificate`: mTLS is on and the client sent no certificate, or one not signed by `server.tls.client_ca_file`. Check the chain and that the client certificate has the `clientAuth` extended key usage.
- Startup fails with a `server.tls` error: the configured cert/key/CA path is wrong, the PEM is invalid, or cert and key do not match. The error names the file.
- `mutually exclusive` at startup: `auth.enabled` is still true. Disable bearer auth when using mTLS.
- With mTLS, plain health probes and plain `curl` fail by design; use `--cert`/`--key` or probe from inside the trusted network.

---

## Security Considerations

- **Read-only by design** -- The proxy never sends `SET_ACTIVITY` or any write command to Discord. It only observes.
- **LAN only** -- Binds to `0.0.0.0` by default. Restrict via firewall rules or bind to `127.0.0.1` and use a VPN (e.g., Tailscale) for remote access.
- **Optional token auth** -- Enable `auth.enabled: true` and set a strong token via `PROXY_AUTH_TOKEN` (or `auth.token` in the config file). It protects the WebSocket upgrade, the REST endpoints (`/api/*`) and the dashboard; the health probes stay open for orchestrators. Without auth, any device on the LAN can read the presence data.
- **No persistence** -- No database, no log files, no presence data stored on disk. Everything is in-memory and lost on restart.
- **mDNS is informational only** -- The mDNS TXT record contains only the API version (`v=1`) and, with TLS enabled, the mode (`tls=server` or `tls=mtls`). No sensitive data is broadcast.
- **Transport encryption (opt-in)** -- `server.tls.enabled: true` serves the whole surface over HTTPS/WSS with TLS 1.2 as the minimum. Without it, WebSocket traffic is plaintext; alternatively terminate TLS in a reverse proxy (nginx, Caddy).
- **Mutual TLS (opt-in)** -- `server.tls.client_ca_file` requires a client certificate chained to that CA on every connection, including health probes and `/metrics`. This authenticates the client and encrypts the transport, and it replaces bearer-token auth (the two cannot be combined). It is authentication, not authorisation: any certificate issued by the CA can read the presence data, so keep the client CA separate and narrow.
- **Certificate material** -- Files are read once at startup; an unreadable or mismatched pair aborts startup. Private keys are never logged. Reloading a renewed certificate requires a restart.

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
