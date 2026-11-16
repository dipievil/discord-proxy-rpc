# Configuration

Config precedence, every setting, TLS/mTLS, dashboard access and Discord app setup.

[← Back to README](../../README.md)

## Configuration precedence

The proxy loads configuration from (highest to lowest priority):

1. Environment variables (prefix: `PROXY_`)
2. YAML config file (`config.yaml`)
3. Built-in defaults

The only command-line flag is `-version`, which prints the version, commit and build date and exits.

## Config file search paths

- `./config.yaml` (current directory)
- `$HOME/.config/discord-proxy-rpc/config.yaml`
- `/etc/discord-proxy-rpc/config.yaml`

## Full config example

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

## Environment variables

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

## TLS and mTLS

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

## Dashboard access

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

## Discord app setup

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
