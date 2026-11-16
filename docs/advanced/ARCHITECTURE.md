# Architecture

How the proxy sits between Discord's IPC socket and LAN clients.

[← Back to README](../../README.md)

## Key characteristics

- **Read-only** -- cannot modify Discord activity; only observes and broadcasts.
- **Explicit, honest state** -- presence carries a `source` field, so clients can tell "no source wired up" from "a source reported nothing".
- **Single binary** -- no runtime dependencies, no database, no external files.
- **Zero-config discovery** -- mDNS (Bonjour/Avahi) announces the service automatically on LAN.
- **Embedded dashboard** -- a responsive web UI is built into the binary via `go:embed`.
- **Optional authentication** -- Bearer token auth for shared-secret protection.
- **Cross-platform** -- Linux (amd64/arm64), Windows (amd64), macOS (amd64/arm64).

## Runtime requirements

- Discord **desktop client** running and logged in (web Discord does not expose IPC).
- The proxy must run under the same OS user session as Discord.
- Linux: `XDG_RUNTIME_DIR` must be set (typical in graphical sessions).

## Diagram

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
| LAN Server | `internal/server/` | HTTP server with WebSocket upgrade, REST API endpoints, embedded dashboard, optional Bearer token auth and TLS/mTLS |
| mDNS Announcer | `internal/mdns/` | Advertises `_discord-proxy._tcp.local` via zeroconf for automatic LAN discovery |
| Config | `internal/config/` | Viper-based config with YAML files, environment variables and defaults (priority: env > YAML > defaults; the only flag is `-version`) |
| Dashboard | `web/` | Vanilla HTML/CSS/JS SPA embedded via `go:embed` -- no build step required |

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
