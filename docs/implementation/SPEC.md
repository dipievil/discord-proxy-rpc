# discord-proxy-rpc — Specification

## Overview

A LAN proxy that connects to Discord's local IPC socket (`discord-ipc-*`) and exposes Rich Presence data over the local network via WebSocket + HTTP. Read-only — other devices can observe presence but not modify it.

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

## Components

### 1. IPC Client (`internal/ipc/`)

- **Library**: `github.com/dragsbruh/gopresence` (exposes raw IPC for flexibility)
- **Responsibilities**:
  - Connect to Discord IPC socket (tries `discord-ipc-0` through `discord-ipc-9`)
  - Handshake (v=1, client_id from config)
  - Handle `READY` event → transition to `connected` state
  - Handle disconnect → trigger reconnection logic
  - Parse `ACTIVITY` payloads → normalized internal struct
- **Reconnection**: Exponential backoff: 5s, 10s, 20s, 40s, 60s (max), jitter ±10%
- **Health**: Periodic PING (Discord IPC opcode 3) every 30s

### 2. Presence State Machine (`internal/state/`)

- **Cache**: Single `Presence` struct (last known activity)
- **Diff**: Deep compare new vs cached; only emit `PresenceUpdate` event on change
- **Coalesce**: Buffer incoming IPC updates for 5s; emit latest at window end
- **Thread-safety**: All access via mutex; callbacks executed async (non-blocking)

### 3. LAN Server (`internal/server/`)

#### HTTP Endpoints

| Route | Method | Description | Response (200) | Response (503) |
|-------|--------|-------------|----------------|----------------|
| `/` | GET | Dashboard SPA (embedded static files) | `text/html` | — |
| `/ws` | GET | WebSocket upgrade → API | `101 Switching Protocols` | — |
| `/api/presence` | GET | Current presence JSON (snapshot) | `Activity` object | — |
| `/api/state` | GET | IPC connection state JSON | `{"status":"connected\|disconnected\|reconnecting"}` | — |
| `/health` | GET | **Rich liveness probe** — includes IPC state | `{"status":"ok","ipc":"<state>"}` | — |
| `/healthz` | GET | **Minimal liveness probe** (k8s-compatible) | `{"status":"ok"}` | — |
| `/readyz` | GET | **Readiness probe** — reflects IPC connectivity | `{"status":"ok","ipc":"connected"}` | `{"status":"not ready","ipc":"disconnected"}` |

**Endpoint rationale:**
- `/health` — Rich variant for debugging/monitoring; always returns 200 with IPC state embedded
- `/healthz` — Minimal, spec-conformant liveness probe for Kubernetes; always returns 200 if process is alive
- `/readyz` — Readiness probe for Kubernetes; returns 200 only when IPC is connected, 503 otherwise

#### WebSocket Protocol

**Authentication handshake**

When `auth.enabled: true`, the WS upgrade must carry the token either as an `Authorization: Bearer <token>` header (non-browser clients) or — for browser clients, which cannot set request headers — as a WebSocket subprotocol:

```
Sec-WebSocket-Protocol: proxy-token-<base64url(token)>
```

The server echoes the offered `proxy-token-*` subprotocol back on success so the browser handshake completes.

**Server → Client (push)**

```json
{ "type": "presence", "payload": { /* Activity object */ } }
{ "type": "state", "status": "connected|disconnected|reconnecting", "client_id": "optional Discord App ID" }
{ "type": "current", "payload": { /* Activity object */ }, "client_id": "optional Discord App ID" }
```

`client_id` (the Discord Application ID from `discord.client_id`) is included on `state` and `current` messages when configured, so clients can identify which Discord account the proxy is attached to.

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
| `AUTH_001` | `ErrCodeUnauthorized` | WS upgrade rejected: auth enabled and token missing or invalid |
| `MSG_001` | `ErrCodeInvalidMessage` | Malformed JSON, missing/unknown `type`, unknown event in `subscribe`, or any parse failure |
| `INT_001` | `ErrCodeInternal` | Internal server error (including a recovered panic) |
| `REQ_001` | `ErrCodePayloadTooLarge` | Request body exceeds the 1 MiB cap (HTTP 413) |
| `REQ_002` | `ErrCodeRateLimited` | WS upgrade rate limit exceeded (HTTP 429) |
| `REQ_003` | `ErrCodeTooManyConns` | Max concurrent WS connections reached (HTTP 503) |

HTTP-level errors (auth failure, oversized body, rate limit, connection cap, recovered panic) use the same codes in a top-level JSON envelope: `{ "code": "<CODE>", "error": "<message>" }`. WS `error` frames instead nest them under `payload` as `{ "code", "message" }`.

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
- If enabled: static token required on the WS upgrade (see the authentication handshake above)
- Primary token source: `auth.token` in config, overridable via the `PROXY_AUTH_TOKEN` env var
- Legacy `PROXY_TOKEN` env var is still accepted; **when set it takes precedence over both `auth.token` and `PROXY_AUTH_TOKEN`** (explicit viper `Set` beats the env binding) — it is not a true fallback. Scheduled for removal (task 9.6); use `PROXY_AUTH_TOKEN` only
- Token comparison is constant-time (`crypto/subtle`)
- Browser clients (the dashboard) cannot set request headers; they send the token as a WebSocket subprotocol `proxy-token-<base64url(token)>` instead
- **Known gap (deliberate in v1)**: REST/dashboard endpoints (`/`, `/api/*`, `/health*`) are NOT auth-gated in the current implementation — only the WS upgrade is. Tracked as future task 9.1.

### 4. mDNS Announcer (`internal/mdns/`)

- **Library**: `github.com/grandcat/zeroconf`
- Registers service: `_discord-proxy._tcp.local` on configured port
- TXT record: `v=1` (API version)
- Instance name: configurable (default: hostname)
- Cleanup on graceful shutdown

### 5. Config (`internal/config/`)

- **Library**: `github.com/spf13/viper`
- Sources (priority): flags > env > YAML > defaults
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
  rate_limit_per_ip: 10      # max WS upgrades per IP within the window
  rate_limit_window: 1m      # rate-limit window

auth:
  enabled: false
  token: ""                  # override via PROXY_AUTH_TOKEN env (legacy PROXY_TOKEN wins if both set; removal planned — task 9.6)

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
  - Connection status indicator (● green/red)
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

---

## Project Structure

```
discord-proxy-rpc/
├── cmd/
│   └── proxy/
│       └── main.go           # Entry point, wiring, signal handling
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
│   │   ├── errors.go
│   │   ├── messages.go
│   │   └── ratelimit.go
│   ├── state/
│   │   └── presence.go
│   ├── mdns/
│   │   ├── announce.go
│   │   └── signal.go
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
    github.com/google/uuid v1.6.0
    github.com/gorilla/websocket v1.5.3
    github.com/grandcat/zeroconf v1.0.0
    github.com/spf13/viper v1.21.0
    go.uber.org/zap v1.28.0
)
```

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
```

### CI (GitHub Actions — `.github/workflows/ci.yml`)

- Triggers: push and PR to `dev` and `main`
- Jobs: `lint` (go vet), `test` (`make test`, unit + integration with `-race`), `build` (`make build`), `e2e` (`scripts/e2e.sh`, incl. verification that the script fails on a broken start)
- A PR may merge only with green CI

### Release (GitHub Actions + GoReleaser)

- Trigger: push to `main`
- Process:
    1. `version` job runs semantic-release to determine version bump, creates Git tag.
    2. If a new release, `build` job cross-compiles binaries for 5 platforms.
    3. `draft-release` job creates a draft GitHub Release with the generated notes and uploads binaries.
- Outputs: Linux (amd64, arm64), Windows (amd64), macOS (amd64, arm64)
- Artifacts: binaries attached to draft release (review before publishing)

### Docker

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
RUN go build -o /out/discord-proxy ./cmd/proxy

FROM alpine:3.20
COPY --from=builder /out/discord-proxy /usr/local/bin/
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
- **Optional token auth**: Enable `auth.enabled: true` + set `PROXY_AUTH_TOKEN` for shared-secret auth on the WS upgrade; comparison is constant-time. The legacy `PROXY_TOKEN` env var currently wins when both are set (removal tracked as task 9.6). REST/dashboard endpoints are NOT auth-gated yet (tracked as task 9.1)
- **WS rate limiting**: Per-IP limiter on WS upgrades (`server.rate_limit_per_ip` / `server.rate_limit_window`, defaults 10 upgrades per minute per IP); excess upgrades get HTTP 429 `REQ_002`
- **Max WS connections**: Concurrent client cap (`server.max_ws_connections`, default 100); excess upgrades get HTTP 503 `REQ_003`
- **Request body cap**: Requests declaring `Content-Length` over 1 MiB are rejected with HTTP 413 `REQ_001`; unknown-length (chunked) bodies are capped via `MaxBytesReader`, so any read past 1 MiB fails. The current handlers never read request bodies (all routes are read-only), so an oversized chunked body is ignored rather than rejected with 413
- **Panic recovery**: `RecoveryMiddleware` wraps the HTTP mux (plus an inner recover in the WS handler); a panicking handler returns HTTP 500 `INT_001` instead of crashing the process
- **Cross-origin protection**: The WS upgrader rejects cross-origin requests (gorilla/websocket default `CheckOrigin`); no wildcard CORS headers are emitted
- **No persistence**: No database, no logs of presence data beyond memory
- **mDNS**: Only announces service existence; no sensitive data in TXT records

---

## Testing Strategy

| Level | Scope |
|-------|-------|
| Unit | Parser, diff logic, config loading, message codecs |
| Integration | IPC client mock (fake socket), WS hub broadcast, mDNS registration |
| E2E | Full stack: Discord IPC → proxy → WS client → dashboard render |

---

## Future Extensions (Out of Scope v1)

- Write support (remote `SET_ACTIVITY` with per-device auth)
- Multiple Discord accounts (multiple IPC connections)
- Prometheus metrics endpoint (`/metrics`)
- TUI dashboard (`bubbletea`)
- System tray indicator (host machine)
- Encrypted WS (mTLS)