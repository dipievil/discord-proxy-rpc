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

| Route | Method | Description |
|-------|--------|-------------|
| `/` | GET | Dashboard SPA (embedded static files) |
| `/ws` | GET | WebSocket upgrade → API |
| `/api/presence` | GET | Current presence JSON (snapshot) |
| `/api/state` | GET | IPC connection state JSON |
| `/health` | GET | Liveness probe (returns 200 OK) |

#### WebSocket Protocol

**Server → Client (push)**

```json
{ "type": "presence", "payload": { /* Activity object */ } }
{ "type": "state", "status": "connected|disconnected|reconnecting" }
{ "type": "current", "payload": { /* Activity object */ } }
```

**Client → Server**

```json
{ "type": "subscribe", "events": ["presence", "state"] }
{ "type": "get_current" }
```

**Activity Object** (subset of Discord RPC spec)

```json
{
  "details": "string",
  "state": "string",
  "timestamps": { "start": 1234567890, "end": 1234567890 },
  "assets": {
    "large_image": "string", "large_text": "string",
    "small_image": "string", "small_text": "string"
  },
  "party": { "id": "string", "size": [1, 4] },
  "buttons": [ { "label": "string", "url": "string" } ],
  "type": 0
}
```

#### Hub (Broadcast Manager)

- Maintains set of connected WS clients
- `Broadcast(event)` → fan-out to all subscribers
- `Subscribe(client, events[])` / `Unsubscribe(client)`
- Graceful close on client disconnect

#### Auth (Optional)

- Disabled by default
- If enabled: static token via `Authorization: Bearer <token>` header on WS upgrade
- Token from config/env (`PROXY_TOKEN`)

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

server:
  host: "0.0.0.0"
  port: 8765
  ws_path: "/ws"
  read_timeout: 10s
  write_timeout: 10s

auth:
  enabled: false
  token: ""                  # override via PROXY_TOKEN env

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
│   │   └── parser.go
│   ├── server/
│   │   ├── http.go
│   │   ├── ws.go
│   │   ├── hub.go
│   │   ├── auth.go
│   │   └── messages.go
│   ├── state/
│   │   └── presence.go
│   ├── mdns/
│   │   └── announce.go
│   └── platform/
│       └── ipc_path.go
├── web/
│   ├── index.html
│   ├── app.js
│   └── style.css
├── pkg/
│   └── types/
│       └── activity.go
├── configs/
│   └── config.example.yaml
├── .github/
│   └── workflows/
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
module github.com/seu-usuario/discord-proxy-rpc

go 1.26

require (
    github.com/dragsbruh/gopresence v0.0.0-20260104...
    github.com/gorilla/websocket v1.5.1
    github.com/grandcat/zeroconf v0.0.0-20240319...
    github.com/spf13/viper v1.19.0
    github.com/spf13/cobra v1.8.0
    go.uber.org/zap v1.26.0
    gopkg.in/yaml.v3 v3.0.1
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

### Release (GitHub Actions + GoReleaser)

- Trigger: push tag `v*`
- Outputs: Linux (amd64, arm64), Windows (amd64), macOS (amd64, arm64)
- Artifacts: binaries + checksums + release notes

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
- **Optional token auth**: Enable `auth.enabled: true` + set `PROXY_TOKEN` for shared-secret auth
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