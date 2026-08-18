# discord-proxy-rpc

[![CI](https://github.com/dipievil/discord-proxy-rpc/actions/workflows/ci.yml/badge.svg)](https://github.com/dipievil/discord-proxy-rpc/actions/workflows/ci.yml)

A lightweight LAN proxy that reads your Discord Rich Presence via local IPC and serves it over WebSocket and HTTP. Other devices on your network can observe what game you are playing, what song you are listening to, or any custom status -- without installing Discord themselves.

```
+-----------+       IPC pipe       +----------------+      WebSocket / HTTP      +-----------+
|  Discord  | <------------------> | discord-proxy  | <------------------------> |  Clients  |
|  Desktop  |  (local socket)      |     (this)     |       (LAN)               |  (phones, |
+-----------+                      +----------------+                            |   tablets, |
                                                         mDNS: _discord-proxy._tcp|   other PC)|
                                                                                    +-----------+
```

## Features

- Reads Discord Rich Presence over the local IPC socket (no token or bot required on the client side)
- Serves presence data over WebSocket for real-time updates
- HTTP API for on-demand polling
- Built-in web dashboard for quick status checks
- Automatic mDNS/DNS-SD advertisement -- clients discover the proxy without manual configuration
- Optional token-based authentication for WebSocket clients
- Cross-platform: Linux, macOS, and Windows

## Quick Start

### 1. Download

Go to the [Releases](https://github.com/dipievil/discord-proxy-rpc/releases) page and download the binary for your platform:

| Platform | File |
|----------|------|
| Linux (x64) | `discord-proxy-linux-amd64` |
| Linux (ARM64) | `discord-proxy-linux-arm64` |
| Windows (x64) | `discord-proxy-windows-amd64.exe` |
| macOS (Intel) | `discord-proxy-darwin-amd64` |
| macOS (Apple Silicon) | `discord-proxy-darwin-arm64` |

### 2. Configure

Create a `config.yaml` file in the same directory as the binary:

```yaml
discord:
  client_id: "YOUR_CLIENT_ID"
```

You need a Discord Application Client ID. See [docs/implementation/discord-app-setup.md](docs/implementation/discord-app-setup.md) for step-by-step instructions.

### 3. Run

**Linux / macOS:**

```bash
chmod +x discord-proxy-linux-amd64
./discord-proxy-linux-amd64
```

**Windows:**

```powershell
.\discord-proxy-windows-amd64.exe
```

Open `http://localhost:8765` in a browser to see the dashboard. Other devices on your LAN can reach it at `http://<your-local-ip>:8765`.

## Dashboard

The built-in web dashboard is served at the root path:

```
http://localhost:8765/
```

It displays the current Rich Presence status in real time. No configuration is needed -- it is available as soon as the server starts.

## Configuration

The proxy searches for `config.yaml` in the following directories (in order):

| Priority | Path |
|----------|------|
| 1 | `.` (current working directory) |
| 2 | `$HOME/.config/discord-proxy-rpc/` |
| 3 | `/etc/discord-proxy-rpc/` |

The first file found is loaded. Copy the example to get started:

```bash
cp configs/config.example.yaml config.yaml
```

For the full configuration reference (environment variables, all options), see [docs/development/configuration.md](docs/development/configuration.md).

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `failed to load config` | Ensure `config.yaml` exists in one of the search paths above or set `PROXY_DISCORD_CLIENT_ID` as an env var. |
| `discord client_id is empty` | Set your Discord Application Client ID in `config.yaml` or via `PROXY_DISCORD_CLIENT_ID`. |
| Cannot connect to Discord IPC | Make sure the Discord desktop client is running and logged in. On Linux, check that the IPC socket exists at `/tmp/discord-ipc-0`. |
| Other devices cannot reach the proxy | Verify that your firewall allows inbound connections on port 8765 (or your configured port). Ensure all devices are on the same LAN/subnet. |
| mDNS not discovered by clients | Confirm `mdns.enabled: true`. Some routers block mDNS -- try connecting via IP directly. On Linux, ensure `avahi-daemon` is running. |
| Docker container cannot reach Discord | Mount the Discord IPC socket into the container with `-v /tmp/discord-ipc-0:/tmp/discord-ipc-0`. |
| WebSocket connection closes immediately | Check that `auth.enabled` and `auth.token` match on both server and client. |
| Logs are too verbose | Set `logging.level: warn` or `PROXY_LOGGING_LEVEL=warn` to reduce noise. |

## Security

- **Read-only** -- The proxy only reads presence data from Discord. It cannot send messages, modify your profile, or perform any actions on your behalf.
- **LAN only** -- By default, the server binds to `0.0.0.0`, making it reachable from any device on your local network. To restrict access to the local machine only, set `server.host` to `127.0.0.1`.
- **Optional authentication** -- Enable token-based auth (`auth.enabled: true`) to require a shared secret from WebSocket clients. Set the token via the `PROXY_AUTH_TOKEN` environment variable rather than writing it in a config file.
- **No telemetry** -- The proxy does not phone home, collect analytics, or make any outbound connections beyond the local Discord IPC socket.

## For Developers

Want to build from source, contribute, or understand the architecture? See the [Development Guide](docs/development/).

## License

This project is licensed under the [Apache License 2.0](LICENSE).
