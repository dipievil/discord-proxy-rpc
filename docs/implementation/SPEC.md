# discord-proxy-rpc — Specification

## Overview

A LAN proxy that connects to Discord's local IPC socket (`discord-ipc-*`) and exposes Rich Presence data over the local network via WebSocket + HTTP. Read-only — other devices can observe presence but not modify it.

> **Activity source (important):** Discord's local IPC socket is a client→Discord channel. It allows a client to *set* its own activity (`SET_ACTIVITY`) but exposes **no command to read another application's Rich Presence**. Discord does not push `ACTIVITY` frames to a connected client such as this proxy. As a result the proxy currently has **no activity source** and cannot observe what the desktop client is doing; it says so explicitly instead of showing an empty feed. See [Activity source](#activity-source) for the contract and how to prove the limitation on your own setup.

**Target platforms**: Linux, Windows (macOS supported via cross-compile)
**Deployment**: Single static binary (Go), systemd/service or manual run
**Discovery**: mDNS (zeroconf) for auto-discovery on LAN

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  HOST (machine running Discord desktop client)                  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│  │ Discord IPC  │◄───│ IPC Client   │◄───│ LAN Server       │  │
│  │ (unix socket │    │ (gopresence) │    │ (WS + HTTP)      │  │
│  │  or named    │    │              │    │                  │  │
│  │  pipe)       │    │              │    │                  │  │
│  └──────────────┘    └──────────────┘    └────────┬─────────┘  │
│                                                    │            │
│  ┌────────────────────────────────────────────────▼─────────┐  │
│  │  Presence State Machine                                   │  │
│  │  - Cache last presence payload                            │  │
│  │  - Diff detection (only broadcast on change)              │  │
│  │  - Coalesce updates: 5s window                            │  │
│  │  - Auto-reconnect with exponential backoff (5s→60s)       │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              ▲
                              │ mDNS: _discord-proxy._tcp.local
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  LAN CLIENTS (multiple)                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │ Dashboard   │  │ CLI Tool    │  │ Custom App  │  ...        │
│  │ (Web SPA)   │  │ (Go/other)  │  │ (any WS)    │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────────────────────────────────────────┘
```

---

## Activity source

The proxy's job is to read the Discord desktop client's Rich Presence and re-broadcast it, but the IPC socket cannot deliver that data:

- `discord-ipc-*` carries RPC requests from a client to the local Discord client. The only activity-related command is `SET_ACTIVITY` (a client publishing *its own* presence). There is no `GET_ACTIVITY`, and Discord does not dispatch another application's `ACTIVITY` as an unsolicited frame.
- The proxy's IPC layer (`internal/ipc/`) still handshakes, keeps the connection alive, and parses any `ACTIVITY` frame it might receive, so it is ready the moment a real source exists. Until then **every snapshot is reported with `source: "none"`**.

To make that state explicit — rather than a silent empty feed that looks like "no one is playing" — the presence contract carries a `source` alongside the (nullable) `activity`:

```json
{ "source": "none", "activity": null }
```

| `source` | Meaning |
|----------|---------|
| `"none"` | The proxy has no activity source. `activity` is always `null`. This is the expected value today. |
| `"ipc"`  | The snapshot was produced from a Discord IPC `ACTIVITY` frame. `activity` is the parsed activity (non-null). |

Both `/api/presence` and the WebSocket `presence` / `current` messages carry this snapshot envelope. The dashboard reads `source` and shows an explicit "No activity source" notice instead of an empty activity card.

### Proving the limitation (capture harness)

`cmd/ipc-capture` connects to the local Discord IPC socket, performs the handshake, and records every raw frame Discord sends for a bounded window, then reports whether any of them was an `ACTIVITY` frame:

```bash
# Run on the machine running the Discord desktop client, while a game is open:
go run ./cmd/ipc-capture -client-id <discord-app-id> -duration 60s -out capture.jsonl
```

The summary ends with either `No ACTIVITY frame was observed…` (confirming there is no IPC source) or `Found N ACTIVITY frame(s)…`. Share `capture.jsonl` if a source turns out to exist, so the `ACTIVITY` frame can be wired into the proxy.

---

## Components

### 1. IPC Client (`internal/ipc/`)

- **Library**: `github.com/dragsbruh/gopresence` (exposes raw IPC for flexibility)
- **Responsibilities**:
  - Connect to Discord IPC socket (tries `discord-ipc-0` through `discord-ipc-9`)
  - Handshake (v=1, client_id from config)
  - Handle `READY` event → transition to `connected` state
  - Handle disconnect → trigger reconnection logic
  - Parse `ACTIVITY` frames → normalized internal struct **when any arrive**; Discord does not push another application's activity today, so this path is currently dormant (see [Activity source](#activity-source))
- **Reconnection**: Exponential backoff: 5s, 10s, 20s, 40s, 60s (max), jitter ±10%
- **Health**: Periodic PING (Discord IPC opcode 3) every 30s

### 2. Presence State Machine (`internal/state/`)

- **Cache**: Single `Presence` struct (last known activity + whether a source has ever supplied one)
- **Diff**: Deep compare new vs cached; only emit `PresenceUpdate` event on change
- **Coalesce**: Buffer incoming IPC updates for 5s; emit latest at window end
- **Thread-safety**: All access via mutex; callbacks executed async (non-blocking)

### 3. LAN Server (`internal/server/`)

#### HTTP Endpoints

| Route | Method | Description | Response (200) | Response (503) |
|-------|--------|-------------|----------------|----------------|
| `/` | GET | Dashboard SPA (embedded static files) | `text/html` | — |
| `/ws` | GET | WebSocket upgrade → API | `101 Switching Protocols` | — |
| `/api/presence` | GET | Current presence snapshot (`{"source":"none\|ipc","activity":Activity\|null}`) | `PresenceSnapshot` object | — |
| `/api/state` | GET | IPC connection state JSON | `{"status":"connected\|disconnected\|reconnecting"}` | — |
| `/health` | GET | **Rich liveness probe** — includes IPC state | `{"status":"ok","ipc":"<state>"}` | — |
| `/healthz` | GET | **Minimal liveness probe** (k8s-compatible) | `{"status":"ok"}` | — |
| `/readyz` | GET | **Readiness probe** — reflects IPC connectivity | `{"status":"ok","ipc":"connected"}` | `{"status":"not ready","ipc":"disconnected"}` |
| `/metrics` | GET | **Prometheus metrics** — only registered when `metrics.enabled: true` | Prometheus text exposition | — |

**Endpoint rationale:**
- `/health` — Rich variant for debugging/monitoring; always returns 200 with IPC state embedded
- `/healthz` — Minimal, spec-conformant liveness probe for Kubernetes; always returns 200 if process is alive
- `/readyz` — Readiness probe for Kubernetes; returns 200 only when IPC is connected, 503 otherwise
- `/metrics` — Prometheus scrape endpoint (v1.1); disabled by default so nothing is exposed unless asked for

#### Metrics (v1.1)

When `metrics.enabled: true`, the server registers `/metrics` (Prometheus text format, `client_golang`). Collectors are fed from the live components — no duplicated counting logic:

| Metric | Type | Source |
|--------|------|--------|
| `discord_proxy_ws_connected_clients` | gauge | `Hub.ClientCount()` at scrape time |
| `discord_proxy_presence_coalesced_sends_total` | counter | presence subscription callback (fires once per coalesced flush) |
| `discord_proxy_ipc_connected` | gauge (0/1) | IPC state getter at scrape time (`connected` ⇒ 1) |
| `discord_proxy_rate_limit_rejections_total` | counter | `RateLimiter` denial counter (dashboard, REST and WS rejections) |

**Access control:** when `auth.enabled: true`, `/`, `/index.html`, the embedded static assets, `/api/presence` and `/api/state` require the bearer token; `/health`, `/healthz` and `/readyz` stay open so probes work without credentials. API routes answer an unauthenticated request with the `AUTH_001` JSON envelope (HTTP 401), while the dashboard answers a plain-text 401.

**Rate limiting:** a single per-IP limiter (`server.rate_limit_per_ip` / `server.rate_limit_window`, defaults 60 requests per minute) guards the dashboard and its static assets, `/api/presence`, `/api/state` and the WebSocket upgrade; all routes share one budget per IP. The middleware runs before auth so unauthenticated floods are rejected without the constant-time token comparison. Rejections answer HTTP 429 with a `Retry-After` header in whole seconds and `Cache-Control: no-store`; REST and WS routes carry the `REQ_002` envelope, while the dashboard answers plain text for direct rendering in the tab. A rejection increments `discord_proxy_rate_limit_rejections_total`; the first rejection per IP per window is logged at warning level and later ones at debug, so a reconnect loop cannot flood the log. `/health`, `/healthz`, `/readyz` and `/metrics` are deliberately registered without the middleware: probes and Prometheus scrapes must keep working while an IP is limited, and they are not useful amplification targets. A limiter that is `nil` or has a non-positive rate or window is disabled. The default budget covers a dashboard load, which costs four requests (HTML, CSS, JS and the WS upgrade), plus several reloads and reconnect attempts.

**Client identity:** the limiter keys on the TCP peer (`RemoteAddr`) by default. `server.trusted_proxies` lists CIDR blocks whose `X-Forwarded-For` is honoured; when the peer is trusted the limiter takes the last hop that is not itself trusted, so clients behind an ingress keep separate budgets and an untrusted client cannot forge its IP. An empty list ignores forwarding headers entirely.

#### TLS / mTLS (v1.2, task 11.1)

One listener serves every route above — dashboard, REST, `/ws`, `/metrics` and the health probes — so enabling TLS changes the scheme for all of them at once (`https`/`wss`).

| `server.tls` | Behaviour |
|--------------|-----------|
| `enabled: false` (default) | Plain HTTP/WS, unchanged from v1.1 |
| `enabled: true`, `client_ca_file` empty | Server-side TLS (`MinVersion` TLS 1.2). Clients verify the server; bearer auth remains optional |
| `enabled: true`, `client_ca_file` set | Mutual TLS: `ClientAuth: RequireAndVerifyClientCert` with that PEM as `ClientCAs`, so every connection including health probes and `/metrics` needs a certificate chained to it. The WS handler also rejects upgrades whose `r.TLS.VerifiedChains` is empty (defence in depth, so a listener that only requested a certificate cannot let an unverified one through) with HTTP 401 `AUTH_001`. Bearer auth is rejected as a conflicting config |

- Env surface: `PROXY_SERVER_TLS_ENABLED`, `PROXY_SERVER_TLS_CERT_FILE`, `PROXY_SERVER_TLS_KEY_FILE`, `PROXY_SERVER_TLS_CLIENT_CA_FILE`.
- `/health`, `/healthz` and `/readyz` stay unauthenticated but are **TLS-only** when TLS is enabled; probes must switch to HTTPS, and with mTLS the prober needs a client certificate (kubelet probes normally cannot supply one, so probe in-pod or terminate mTLS upstream).
- Validation order and failure modes: `enabled` requires `cert_file` and `key_file`; certificate settings with `enabled: false` are rejected rather than silently serving plaintext; `client_ca_file` + `auth.enabled` fails fast as mutually exclusive; the cert/key pair, CA readability and CA PEM validity are checked at config load (`server.tls` errors name the offending file), and the listener is built from the same material once at startup, so a bad PEM never reaches the accept loop.
- Client certificates must carry the `clientAuth` extended key usage; `serverAuth`-only leafs are rejected by the handshake.

#### WebSocket Protocol

**Authentication handshake**

When `auth.enabled: true`, the WS upgrade must carry the token either as an `Authorization: Bearer <token>` header (non-browser clients) or — for browser clients, which cannot set request headers — as a WebSocket subprotocol:

```
Sec-WebSocket-Protocol: proxy-token-<base64url(token)>
```

The server echoes the offered `proxy-token-*` subprotocol back on success so the browser handshake completes.

**Server → Client (push)**

```json
{ "type": "presence", "payload": { "source": "none", "activity": null } }
{ "type": "state", "status": "connected|disconnected|reconnecting", "client_id": "Discord App ID" }
{ "type": "current", "payload": { "source": "none", "activity": null }, "client_id": "Discord App ID" }
```

`payload` on `presence` and `current` is a **presence snapshot** (see [Activity source](#activity-source)); `activity` is `null` whenever `source` is `"none"`. `client_id` (the Discord Application ID from `discord.client_id`) is always included on `state` and `current` messages — it is required at startup (task 9.3) — so clients can identify which Discord account the proxy is attached to.

**State on subscribe.** A `subscribe` that includes `state` is answered with one `state` frame carrying the proxy's current IPC state, mapped from `/api/state` (`connected`, `reconnecting`, or `disconnected`), before any later push. The snapshot is produced by the same fan-out loop that delivers state pushes, so an older transition can never overtake it and leave the client on a stale state. The frame goes only to the subscribing client and includes `client_id`. A client must not infer IPC connectivity from the WebSocket being open: an open socket only means the proxy accepted the connection.

**Terminal state.** `reconnecting` means the proxy will attempt another IPC session. When no further session will be attempted — `discord.auto_reconnect: false`, reconnect attempts exhausted, or the IPC client stopped for another reason — the proxy emits exactly one final `disconnected` and stops broadcasting `reconnecting`. That frame is queued with a blocking send, so a full broadcast buffer delays it instead of dropping it.

**Client → Server**

```json
{ "type": "subscribe", "events": ["presence", "state"] }
{ "type": "get_current" }
```

**Server → Client (error response)**

Error frames are sent in direct response to a client message that could not be processed. They do not close the connection; the client may continue sending messages.

```json
{ "type": "error", "payload": { "code": "MSG_001", "message": "invalid message: ..." } }
```

| Code | Constant | Description |
|------|----------|-------------|
| `AUTH_001` | `ErrCodeUnauthorized` | Auth enabled and the token is missing or invalid — WS upgrade rejected, or HTTP route answered 401 |
| `MSG_001` | `ErrCodeInvalidMessage` | Malformed JSON, missing/unknown `type`, unknown event in `subscribe`, or any parse failure |
| `INT_001` | `ErrCodeInternal` | Internal server error (including a recovered panic) |
| `REQ_001` | `ErrCodePayloadTooLarge` | Request body exceeds the 1 MiB cap (HTTP 413) |
| `REQ_002` | `ErrCodeRateLimited` | Request rate limit exceeded on the dashboard, REST or WS upgrade (HTTP 429, with `Retry-After`) |
| `REQ_003` | `ErrCodeTooManyConns` | Max concurrent WS connections reached (HTTP 503) |

HTTP-level errors on the API and WS surface (auth failure, oversized body, rate limit, connection cap, recovered panic) use the same codes in a top-level JSON envelope: `{ "code": "<CODE>", "error": "<message>" }`; dashboard routes answer 401 and 429 as plain text because the browser renders the body directly. A rate-limit rejection also carries `Retry-After` with the remaining seconds in the current window and `Cache-Control: no-store`. WS `error` frames instead nest them under `payload` as `{ "code", "message" }`.

**Presence Snapshot** (payload of `presence` / `current` and body of `/api/presence`)

```json
{
  "source": "none",
  "activity": null
}
```

`source` is `"none"` or `"ipc"`; `activity` carries the Activity Object below when `source` is `"ipc"`, and is `null` otherwise. `activity` is always present on the wire (it is not omitted).

**Activity Object** (subset of Discord RPC spec)

```json
{
  "details": "string",
  "state": "string",
  "timestamps": { "start": 1700000000000, "end": 1700003600000 },
  "assets": {
    "large_image": "string", "large_text": "string",
    "small_image": "string", "small_text": "string"
  },
  "party": { "id": "string", "size": [1, 4] },
  "buttons": [ { "label": "string", "url": "string" } ],
  "type": 0
}
```

> `timestamps.start` / `timestamps.end` are Unix **milliseconds**, matching Discord's IPC protocol. The dashboard converts them for display.

#### Hub (Broadcast Manager)

- Maintains set of connected WS clients
- `Broadcast(event)` → fan-out to all subscribers
- `Subscribe(client, events[])` / `Unsubscribe(client)`
- Graceful close on client disconnect

#### Auth (Optional)

- Disabled by default
- If enabled: static token required on the WS upgrade (see the authentication handshake above) as well as for REST endpoints and the dashboard (`/` and `/api/*`); `/health`, `/healthz`, and `/readyz` remain unauthenticated for probes
- Primary token source: `auth.token` in config, overridable via the `PROXY_AUTH_TOKEN` env var
- The legacy `PROXY_TOKEN` env var was removed in task 9.6; it no longer sets `auth.token` — use `PROXY_AUTH_TOKEN` only
- Token comparison is constant-time (`crypto/subtle`)
- Token sources, in precedence order: `Authorization: Bearer <token>` header, WebSocket subprotocol, auth cookie, `?token=` query parameter
- Mutually exclusive with mTLS: when `server.tls.client_ca_file` is set, `auth.enabled` must be false (config validation fails fast). The client certificate is then the only client identity.
- Browser clients (the dashboard) cannot set request headers; they send the token as a WebSocket subprotocol `proxy-token-<base64url(token)>` instead. For HTTP navigation they may also open the dashboard once as `/?token=<token>`: a successful query-token request sets a short-lived (`15m`), `HttpOnly`, `SameSite=Strict` cookie whose value is the base64url-encoded token, so the static assets and later reloads authenticate without the token in the URL. The cookie is `Secure` only when the request itself arrived over TLS

### 4. mDNS Announcer (`internal/mdns/`)

- **Library**: `github.com/grandcat/zeroconf`
- Registers service: `_discord-proxy._tcp.local` on configured port
- TXT record: `v=1` (API version) and, when TLS is enabled, `tls=server` or `tls=mtls` (transport authentication mode)
- DNS-SD has no scheme field, so the `tls` TXT key is the scheme signal: its presence means the advertised port is `https`/`wss`, its absence means plain `http`/`ws`
- Instance name: configurable (default: hostname)
- Cleanup on graceful shutdown

### 4.1 Discovery Helper (`cmd/discover/`)

Client-side counterpart of the announcer: browses `_discord-proxy._tcp.local` and prints the endpoints it finds. Run it with `make discover` or `go run`; release archives also ship it as the `discover` binary.

```bash
go run ./cmd/discover -timeout 5s        # table
go run ./cmd/discover -timeout 5s -json  # {"count":N,"proxies":[...]}
```

- Columns: instance, host, addresses, port, TXT. Addresses are the dialable ones; link-local IPv6 addresses are counted as `(+N link-local)` in the table and kept in the `link_local` JSON field, because they need a zone to be usable.
- The TXT column carries `tls=server|mtls` when the announced proxy uses TLS; treat those endpoints as `https`/`wss` (mTLS additionally needs a client certificate).
- Results are deduplicated on instance/host/port, merging addresses and TXT records, and sorted by instance name for stable output.
- Table cells are sanitized (control characters replaced) so an unauthenticated mDNS announcement cannot inject ANSI escapes or extra columns; JSON output keeps the raw values.
- `-timeout` must be positive (a browse without a deadline would never end).
- Exit codes: `0` success — **including "no proxies found"**, `1` browse or write failure, `2` usage error (e.g. `-timeout 0`). Scripts must parse the output, not the exit code, to tell an empty LAN from a populated one.

### 5. Config (`internal/config/`)

- **Library**: `github.com/spf13/viper`
- Sources (priority): env > YAML > defaults (the binary has no config flags; `-version` only prints the build stamp and exits)
- Config file: `config.yaml` (search: `.`, `/etc/discord-proxy-rpc/`, `$HOME/.config/discord-proxy-rpc/`)

```yaml
discord:
  client_id: ""              # REQUIRED - Discord Application ID
  auto_reconnect: true
  reconnect_base_interval: 5s
  max_reconnect_interval: 60s
  health_check_interval: 30s # IPC PING interval
  health_check_timeout: 10s  # IPC PING response timeout
  coalesce_interval: 5s      # Buffer window for batching presence updates

server:
  host: "0.0.0.0"
  port: 8765
  ws_path: "/ws"
  read_timeout: 10s
  write_timeout: 10s
  max_ws_connections: 100    # cap on concurrent WebSocket clients
  rate_limit_per_ip: 60      # max requests per IP within the window (dashboard, REST, WS)
  rate_limit_window: 1m      # rate-limit window
  trusted_proxies: []        # CIDRs whose X-Forwarded-For is honoured (empty = RemoteAddr only)
  tls:
    enabled: false           # serve HTTPS/WSS
    cert_file: ""            # PEM server certificate (chain)
    key_file: ""             # PEM private key
    client_ca_file: ""       # PEM CA bundle; set = require and verify client certs (mTLS)

auth:
  enabled: false
  token: ""                  # override via PROXY_AUTH_TOKEN env (legacy PROXY_TOKEN removed — task 9.6)

mdns:
  enabled: true
  instance_name: ""          # empty = hostname
  service_type: "_discord-proxy._tcp"

logging:
  level: "info"              # debug, info, warn, error
  format: "json"             # json or console
```

### 6. Dashboard Web (`web/`)

- **Serving**: `//go:embed web/*` → single binary, no external files
- **Tech**: Vanilla HTML/CSS/JS (no build step, no framework)
- **Features**:
  - Connection status indicator (● green/red) driven by the proxy's `state` frames, not by the WebSocket opening
  - **Activity source notice** — shows "No activity source" when the snapshot reports `source: "none"`, instead of an empty activity card
  - Presence fields: Details, State, Timestamps (live countdown), Assets (images), Party, Buttons
  - Real-time updates via WebSocket
  - "Copy JSON" button for debugging
  - Responsive (mobile-friendly)
  - Dark/light theme (CSS variables)

### 7. Platform Abstraction (`internal/platform/`)

- Resolves Discord IPC socket path cross-platform:
  - Linux: `$XDG_RUNTIME_DIR/discord-ipc-{0..9}` → `/tmp/discord-ipc-{0..9}`
  - Windows: `\\?\pipe\discord-ipc-{0..9}`
  - macOS: same as Linux

### 8. System Tray Indicator (`cmd/tray/`, `internal/tray/`)

- **Backend**: KDE/StatusNotifierItem (`org.kde.StatusNotifierWatcher`) on the D-Bus session bus, via `github.com/godbus/dbus/v5` — pure Go, so `CGO_ENABLED=0` builds and releases stay unchanged (no GTK, no cgo).
- **Status source**: polls `GET /api/state` every `-interval` (default `5s`, minimum `2s`), sending `Authorization: Bearer <token>` when a token is configured. The first result is applied immediately; later polls only reach the tray when the state changes.
- **Mapping** (`internal/tray/status.go`): `connected` → green disc + check, `reconnecting` → amber disc + minus, `disconnected` → red disc + slash. `connecting` (a Discord dial in progress) is shown as reconnecting, matching the server's WebSocket mapping. An unreachable proxy, an HTTP error or an unknown `status` value reports `unreachable` with the disconnected icon and a "proxy unreachable" tooltip.
- **Icons**: generated ARGB32 pixmaps (22 px and 44 px) exported through the SNI `IconPixmap` and `ToolTip` properties, so no icon theme or asset files are needed.
- **Platforms**: the Linux implementation is behind a `linux` build tag; other platforms build a stub that exits with "tray indicator is not supported on this platform yet", keeping `go build ./...` and all cross-compiles green.
- **CLI**: `-server`, `-token`, `-cacert`, `-cert`, `-key`, `-interval`, `-version`; env fallbacks `PROXY_URL` and `PROXY_AUTH_TOKEN` (the legacy `PROXY_TOKEN` was removed in task 9.6).
- **TLS**: `-cacert` replaces the system roots with a private CA file; `-cert`/`-key` present a client certificate for a proxy that requires mTLS (task 11.1). Plain `http://` keeps the system client.
- **Testing**: the D-Bus export itself is exercised only on a real desktop session (not testable headlessly). Everything else is unit tested without a bus: state normalization and mapping, icon pixmaps, URL/token/interval resolution, the HTTP client (including auth, TLS with a private CA and mTLS, and failures) and the poll loop against `httptest`, including a connected → disconnected → unreachable transition and a retry after a failed tray update.

---

## Project Structure

```
discord-proxy-rpc/
├── cmd/
│   ├── proxy/
│   │   └── main.go           # Entry point, wiring, signal handling
│   ├── ipc-capture/
│   │   └── main.go           # Capture harness: records raw Discord IPC frames
│   ├── discover/
│   │   └── main.go           # Discovery helper: browses the LAN for proxies via mDNS
│   └── tray/
│       └── main.go           # System-tray indicator (Linux StatusNotifierItem)
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── ipc/
│   │   ├── client.go
│   │   ├── reconnect.go
│   │   ├── healthcheck.go
│   │   └── parser.go
│   ├── server/
│   │   ├── http.go
│   │   ├── ws.go
│   │   ├── hub.go
│   │   ├── auth.go
│   │   ├── tls.go            # TLS/mTLS listener config and serve path
│   │   ├── errors.go
│   │   ├── messages.go
│   │   └── ratelimit.go
│   ├── state/
│   │   └── presence.go
│   ├── mdns/
│   │   ├── announce.go
│   │   └── signal.go
│   ├── tray/
│   │   ├── app.go            # Poller + notifier wiring
│   │   ├── client.go         # /api/state client
│   │   ├── icons.go          # Generated ARGB32 pixmaps
│   │   ├── notifier.go       # Notifier interface
│   │   ├── notifier_linux.go # StatusNotifierItem over D-Bus (linux)
│   │   ├── notifier_stub.go  # "not supported" stub (other platforms)
│   │   ├── options.go        # Flag/env resolution and validation
│   │   ├── poller.go         # Poll loop, emits only transitions
│   │   └── status.go         # State -> icon + tooltip
│   └── platform/
│       └── ipc_path.go
├── web/
│   ├── embed.go
│   ├── index.html
│   ├── app.js
│   └── style.css
├── pkg/
│   └── types/
│       └── activity.go
├── configs/
│   └── config.example.yaml
├── scripts/
│   └── e2e.sh                # End-to-end smoke test against a real binary
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── .goreleaser.yml
└── README.md
```

---

## Dependencies

```go
module github.com/discord-proxy-rpc/discord-proxy-rpc

go 1.26.5

require (
    github.com/dop251/goja v0.0.0-20241024094426-79f3a7efcdbd
    github.com/dragsbruh/gopresence v0.0.0-20260104125204-049c09d5d051
    github.com/godbus/dbus/v5 v5.2.4
    github.com/google/uuid v1.6.0
    github.com/gorilla/websocket v1.5.3
    github.com/grandcat/zeroconf v1.0.0
    github.com/spf13/viper v1.21.0
    go.uber.org/zap v1.28.0
)
```

TLS/mTLS (task 11.1) uses only the standard library (`crypto/tls`, `crypto/x509`, `net`) — no new dependency.

---

## Build & Release

### Local Build

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 go build -o bin/discord-proxy-linux-amd64 ./cmd/proxy

# Windows amd64
GOOS=windows GOARCH=amd64 go build -o bin/discord-proxy-windows-amd64.exe ./cmd/proxy

# All platforms
make build-all

# System-tray indicator (Linux; other platforms build a stub)
make tray
```

- Version stamping: `cmd/proxy` carries `version`, `commit` and `date` variables set with `-X` ldflags. The Makefile derives them from `git describe --tags --always --dirty`, `git rev-parse --short HEAD` and the build time, so a local build reports the tag it came from; goreleaser stamps the release version, commit and date.
- `discord-proxy -version` prints one line — `discord-proxy <version> (commit <sha>, built <timestamp>)` — and exits 0 without loading configuration. An unstamped build (`go run`, plain `go build`) reports `dev`. The same stamp is logged at startup.
- `cmd/discover` and `cmd/tray` accept `-version` and are stamped with the release version by goreleaser.

### CI (GitHub Actions — `.github/workflows/ci.yml`)

- Triggers: push and PR to `dev` and `main`
- Jobs: `lint` (`gofmt` check, `go vet`, `golangci-lint run`), `test` (`make test`, unit + integration with `-race`), `build` (`make build`), `e2e` (`scripts/e2e.sh`, incl. verification that the script fails on a broken start)
- A PR may merge only with green CI

### Release (GitHub Actions + GoReleaser)

- Trigger: push to `main`
- Process:
    1. `version` job runs semantic-release: computes the version bump, creates the Git tag and publishes the GitHub Release with generated notes.
    2. If a new release, the `goreleaser` job runs `goreleaser release --clean` with `GORELEASER_CURRENT_TAG` set to that tag; `release.mode: append` attaches the archives to the release semantic-release created instead of failing on an existing release.
- Outputs: one `.zip` per target — Linux (amd64, arm64), Windows (amd64), macOS (amd64, arm64); on Linux each archive also carries the tray binary (`discord-proxy-tray`)
- Each archive carries everything a user needs: the proxy binaries (`discord-proxy` and `discover`), the tray on Linux, `config.example.yaml`, `LICENSE` and `README.md`
- `checksums.txt` (SHA-256 of every archive) is attached to the release
- The `dev` → `main` merge decides whether a release happens: its squash title (or a `Release-As:` footer) must be a bumping type (`feat:`, `fix:`, `feat!:`) — a `chore:` merge leaves semantic-release with no bump, so the tag and release are never created

### Docker

Multi-stage build (builder + Alpine runtime). The runtime user is configurable so it can match the host
desktop-session user that owns Discord's IPC socket (`--build-arg APP_UID/APP_GID`, or `docker run --user`);
documented mounts: the host `$XDG_RUNTIME_DIR` (read-only) for `discord-ipc-*` and `config.yaml` at
`/etc/discord-proxy-rpc/config.yaml` (read-only).

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /out/discord-proxy ./cmd/proxy

FROM alpine:3.20
ARG APP_UID=10001
ARG APP_GID=10001
RUN addgroup -g "${APP_GID}" appuser && adduser -D -H -u "${APP_UID}" -G appuser appuser
COPY --from=builder /out/discord-proxy /usr/local/bin/
USER appuser
EXPOSE 8765
ENTRYPOINT ["discord-proxy"]
```

---

## Discord App Setup (for README)

1. Go to https://discord.com/developers/applications
2. "New Application" → Name: "Discord Proxy RPC"
3. Copy **Application ID** (Client ID) → paste into `config.yaml` → `discord.client_id`
4. (Optional) Rich Presence → Art Assets → upload images for custom large/small images
5. No Redirect URI, Bot, or OAuth2 needed — only the Client ID

---

## Runtime Requirements

- Discord **desktop client** running (web Discord has no IPC)
- Same user session as Discord (IPC is per-user)
- Linux: XDG_RUNTIME_DIR set (typical in graphical sessions)
- Port 8765 (configurable) open on host firewall for LAN access

---

## Security Considerations

- **Read-only**: No `SET_ACTIVITY` command exposed
- **LAN only**: Binds to `0.0.0.0` by default; restrict via firewall or `host: "127.0.0.1"` + VPN (Tailscale)
- **Optional token auth**: Enable `auth.enabled: true` + set `PROXY_AUTH_TOKEN` for shared-secret auth on the WS upgrade, REST endpoints, and dashboard; comparison is constant-time. Health probes (`/health`, `/healthz`, `/readyz`) stay unauthenticated so container/k8s probes keep working. The legacy `PROXY_TOKEN` env var was removed in task 9.6 — `PROXY_AUTH_TOKEN` is the sole env source
- **Per-IP rate limiting** -- `server.rate_limit_per_ip` / `server.rate_limit_window` (defaults: 60 requests per minute) throttle the dashboard, the REST endpoints (`/api/*`) and the WebSocket upgrade from one client IP, sharing a single budget. Excess requests get HTTP 429 `REQ_002` with a `Retry-After` header, `Cache-Control: no-store`, and a plain-text body on the dashboard. `/health`, `/healthz`, `/readyz` and `/metrics` are exempt so probes and scrapes keep working. Set the rate to `0` to disable it. Behind an ingress, `server.trusted_proxies` makes the limiter key on the forwarded client instead of the proxy address; the header is ignored otherwise, so it cannot be spoofed.
- **Max WS connections**: Concurrent client cap (`server.max_ws_connections`, default 100); excess upgrades get HTTP 503 `REQ_003`
- **Request body cap**: Requests declaring `Content-Length` over 1 MiB are rejected with HTTP 413 `REQ_001`; unknown-length (chunked) bodies are capped via `MaxBytesReader`, so any read past 1 MiB fails. The current handlers never read request bodies (all routes are read-only), so an oversized chunked body is ignored rather than rejected with 413
- **Panic recovery**: `RecoveryMiddleware` wraps the HTTP mux (plus an inner recover in the WS handler); a panicking handler returns HTTP 500 `INT_001` instead of crashing the process
- **Cross-origin protection**: The WS upgrader rejects cross-origin requests (gorilla/websocket default `CheckOrigin`); no wildcard CORS headers are emitted
- **No persistence**: No database, no logs of presence data beyond memory
- **mDNS**: Only announces service existence; no sensitive data in TXT records beyond the API version and, with TLS enabled, the transport mode
- **TLS (opt-in)**: `server.tls.enabled` serves every route over HTTPS/WSS with TLS 1.2 as the minimum. Without it the surface is plaintext; terminating TLS in a reverse proxy is the alternative
- **mTLS (opt-in)**: `server.tls.client_ca_file` requires and verifies a client certificate on every connection, health probes and `/metrics` included, and replaces bearer-token auth (the two are mutually exclusive and rejected at startup)
- **Certificate material**: loaded and parsed once at startup; unreadable files, invalid PEM, a cert/key mismatch or a CA bundle without certificates abort startup with the file named. Private keys are never logged; renewal needs a restart

### Transport threat model (task 11.1)

| Threat | Mitigation |
|--------|------------|
| Passive LAN sniffing of presence data or bearer tokens | `server.tls.enabled` encrypts every route over HTTPS/WSS |
| Unauthorised LAN client reading presence or opening the WS | `server.tls.client_ca_file` requires a certificate chained to the configured CA; a missing or untrusted certificate fails the TLS handshake |
| A config mistake silently serving plaintext or skipping client verification | Fail-fast validation (cert settings without `enabled`, missing cert/key, unreadable or invalid PEM, mTLS + bearer auth) plus the WS handler's `r.TLS.VerifiedChains` check |
| A probe route bypassing transport auth | Health probes stay unauthenticated but are TLS-only and, under mTLS, require a client certificate too |

Out of scope: the local Discord IPC socket never leaves the host; private-key protection depends on filesystem permissions; there is no CRL/OCSP check (revocation means editing the client CA bundle and restarting); mTLS authenticates but does not authorise per client — any certificate from the CA can read all presence data, so issue client certificates narrowly. The dashboard follows the page scheme (`wss://`), but the TUI and the example consumer only verify with the system trust store and cannot present a client certificate, so mTLS clients are external tools (`curl`, `wscat`, Node, Go).

---

## Testing Strategy

| Level | Scope |
|-------|-------|
| Unit | Parser, diff logic, config loading (including TLS validation), message codecs |
| Integration | IPC client mock (fake socket), WS hub broadcast, mDNS registration, TLS/mTLS listener and WS client-certificate path with in-test self-signed certificates |
| E2E | Full stack: Discord IPC → proxy → WS client → dashboard render |

---

## Future Extensions (Out of Scope v1)

- Write support (remote `SET_ACTIVITY` with per-device auth)
- Multiple Discord accounts (multiple IPC connections)
- ~~Prometheus metrics endpoint (`/metrics`)~~ — implemented in v1.1 (task 10.1), see Metrics section
- TUI dashboard (`bubbletea`)
- ~~System tray indicator (host machine)~~ — implemented in v1.1 (task 10.3), Linux StatusNotifierItem
- Encrypted WS (mTLS)