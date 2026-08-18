# Configuration Reference

All configuration options for discord-proxy-rpc, including environment variables.

## Config File

The proxy searches for `config.yaml` in the following directories (in order):

| Priority | Path |
|----------|------|
| 1 | `.` (current working directory) |
| 2 | `$HOME/.config/discord-proxy-rpc/` |
| 3 | `/etc/discord-proxy-rpc/` |

The first file found is loaded.

## Environment Variables

All config options can be overridden via environment variables with the `PROXY_` prefix. Dots in YAML keys map to underscores:

| YAML path | Environment variable | Default | Description |
|-----------|---------------------|---------|-------------|
| `discord.client_id` | `PROXY_DISCORD_CLIENT_ID` | `""` | **Required.** Your Discord Application Client ID |
| `discord.auto_reconnect` | `PROXY_DISCORD_AUTO_RECONNECT` | `true` | Reconnect to Discord IPC on disconnect |
| `discord.reconnect_base_interval` | `PROXY_DISCORD_RECONNECT_BASE_INTERVAL` | `5s` | Base interval between reconnection attempts |
| `discord.max_reconnect_interval` | `PROXY_DISCORD_MAX_RECONNECT_INTERVAL` | `60s` | Cap for exponential backoff |
| `server.host` | `PROXY_SERVER_HOST` | `0.0.0.0` | Listen address (`127.0.0.1` for localhost only) |
| `server.port` | `PROXY_SERVER_PORT` | `8765` | Listen port |
| `server.ws_path` | `PROXY_SERVER_WS_PATH` | `/ws` | WebSocket endpoint path |
| `server.read_timeout` | `PROXY_SERVER_READ_TIMEOUT` | `10s` | Max time to read a client message |
| `server.write_timeout` | `PROXY_SERVER_WRITE_TIMEOUT` | `10s` | Max time to write a response |
| `auth.enabled` | `PROXY_AUTH_ENABLED` | `false` | Enable token-based authentication |
| `auth.token` | `PROXY_AUTH_TOKEN` | `""` | Shared secret for client authentication |
| `mdns.enabled` | `PROXY_MDNS_ENABLED` | `true` | Advertise via mDNS on the LAN |
| `mdns.instance_name` | `PROXY_MDNS_INSTANCE_NAME` | (hostname) | mDNS service instance name |
| `mdns.service_type` | `PROXY_MDNS_SERVICE_TYPE` | `_discord-proxy._tcp` | DNS-SD service type |
| `logging.level` | `PROXY_LOGGING_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `logging.format` | `PROXY_LOGGING_FORMAT` | `json` | `json` (production) or `console` (development) |

## Example: Running with Environment Variables

```bash
PROXY_DISCORD_CLIENT_ID="123456789012345678" \
PROXY_SERVER_PORT="9000" \
PROXY_MDNS_ENABLED="false" \
  ./discord-proxy
```
