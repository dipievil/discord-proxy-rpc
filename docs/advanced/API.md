# API

REST endpoints, health probes, WebSocket protocol and the bundled clients.

[← Back to README](../../README.md)

## REST endpoints

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

## Kubernetes probes

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

## WebSocket protocol

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

Presence update (pushed on change). `payload` is a presence snapshot — `activity` is `null` while there is no activity source (see [Activity source](ARCHITECTURE.md#activity-source)):
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

## Example consumer

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

Each line is the human-readable rendering of one server message, including the snapshot's `source` field (see [Activity source](ARCHITECTURE.md#activity-source)). With a real activity source wired up, updates render as:

```
presence: In a match | Winning 3-1 [Playing] party 3/5 (source: ipc)
```

The `-token` flag is only needed when the proxy runs with `auth.enabled: true`; the example intentionally duplicates the small wire-protocol structs instead of importing this module's internals so it can be pasted into any Go project as-is.

## TUI dashboard

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

`-addr` also accepts a full `ws://` or `wss://` URL, which is used as-is. Press `q`, `Esc` or `Ctrl+C` to quit. Like the web dashboard, the TUI shows an explicit "No activity source" notice while the proxy has no activity source (see [Activity source](ARCHITECTURE.md#activity-source)).

## System tray indicator

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
