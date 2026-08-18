# Development Guide

This directory contains documentation for developers working on or contributing to discord-proxy-rpc.

## Contents

- [Configuration Reference](configuration.md) -- All config options and environment variables
- [../implementation/discord-app-setup.md](../implementation/discord-app-setup.md) -- Discord Application setup guide

## Build from Source

### Prerequisites

- **Go 1.26+**
- **golangci-lint** (optional, for linting)
- **air** (optional, for hot reload during development)

### Build

```bash
# Build for current platform
make build

# Build for all supported platforms
make build-all
```

Output binaries are placed in the `bin/` directory.

### Test

```bash
# Run all tests (with race detection when gcc is available)
make test
```

### Lint

```bash
# Run golangci-lint
make lint
```

### Dev

```bash
# Run with hot reload (requires air)
make dev
```

### Clean

```bash
# Remove build artifacts
make clean
```

## Architecture

```
+-----------+       IPC pipe       +----------------+      WebSocket / HTTP      +-----------+
|  Discord  | <------------------> | discord-proxy  | <------------------------> |  Clients  |
|  Desktop  |  (local socket)      |     (this)     |       (LAN)               |  (phones, |
+-----------+                      +----------------+                            |   tablets, |
                                                         mDNS: _discord-proxy._tcp|   other PC)|
                                                                                    +-----------+
```

### Directory Layout

```
discord-proxy-rpc/
  cmd/proxy/              Main binary entry point
  internal/
    config/               Viper-based configuration
    platform/             Cross-platform IPC path resolver
  pkg/types/              Shared Activity types
  configs/                Example configuration files
  docs/
    development/          Developer documentation (this directory)
    implementation/       Implementation specs and guides
  Makefile                Build automation
  Dockerfile              Multi-stage Docker build
  .goreleaser.yml         GoReleaser cross-compilation config
```

## Docker

### Build the image

```bash
docker build -t discord-proxy .
```

### Run the container

```bash
docker run -d \
  --name discord-proxy \
  -e PROXY_DISCORD_CLIENT_ID="123456789012345678" \
  -p 8765:8765 \
  discord-proxy
```

The Discord IPC socket from the host must be mounted into the container:

| OS | IPC socket path |
|----|----------------|
| Linux | `/tmp/discord-ipc-0` (or `$XDG_RUNTIME_DIR/discord-ipc-0`) |
| macOS | `/tmp/discord-ipc-0` |
| Windows | `\\?\pipe\discord-ipc-0` |

To mount the Linux socket:

```bash
docker run -d \
  --name discord-proxy \
  -e PROXY_DISCORD_CLIENT_ID="123456789012345678" \
  -p 8765:8765 \
  -v /tmp/discord-ipc-0:/tmp/discord-ipc-0 \
  discord-proxy
```

## API Reference

### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Web dashboard (HTML) |
| `GET` | `/api/presence` | Current presence as JSON |
| `GET` | `/api/state` | Full proxy state as JSON |
| `GET` | `/health` | Health check (returns `200 OK`) |

#### `GET /api/presence`

Returns the current Discord Rich Presence activity:

```json
{
  "details": "Playing a game",
  "state": "In a match",
  "timestamps": { "start": 1700000000 },
  "assets": {
    "large_image": "game-logo",
    "large_text": "My Game",
    "small_image": "class-warrior",
    "small_text": "Warrior"
  },
  "type": 0
}
```

Returns an empty JSON object `{}` when no activity is set.

### WebSocket

Connect to:

```
ws://localhost:8765/ws
```

#### Protocol

1. **Connect** -- The client opens a WebSocket connection.
2. **Authenticate** (if `auth.enabled: true`) -- Send a JSON message as the first frame:
   ```json
   { "type": "auth", "token": "your-secret-token" }
   ```
   The server responds with `{ "type": "auth_ok" }` on success or `{ "type": "auth_error", "error": "..." }` on failure.
3. **Receive updates** -- The server pushes a JSON message every time the presence changes:
   ```json
   {
     "type": "presence",
     "data": {
       "details": "Playing a game",
       "state": "In a match",
       "type": 0
     }
   }
   ```

## mDNS Discovery

When `mdns.enabled: true` (the default), the proxy advertises itself on the local network using mDNS/DNS-SD under the service type `_discord-proxy._tcp.local`.

Clients that support mDNS browsing (most mobile apps, `avahi-browse` on Linux, Bonjour on macOS) can discover the proxy automatically without knowing its IP address.

### Manual discovery with avahi-browse (Linux)

```bash
avahi-browse -r _discord-proxy._tcp.local
```

### Disabling mDNS

Set `mdns.enabled: false` in your config or pass the environment variable:

```bash
PROXY_MDNS_ENABLED=false ./discord-proxy
```
