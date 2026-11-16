# discord-proxy-rpc

**Discord rich presence from all your devices on a LAN.**

A lightweight, read-only LAN proxy that connects to Discord's local IPC socket and exposes Rich Presence data over WebSocket and HTTP. Any device on your network can observe what game, activity, or status is active -- without needing Discord installed.

> **Activity source:** Discord's local IPC socket lets a client *set* its own Rich Presence but exposes **no way to read another application's presence**. The proxy therefore has **no activity source today** and reports that explicitly (`source: "none"`) instead of showing a misleading empty feed. See [ARCHITECTURE.md](docs/advanced/ARCHITECTURE.md#activity-source) for the full explanation and the capture harness.

## Quick start

The proxy must run on the same machine and under the same OS user session as the Discord desktop client, because it talks to Discord's local IPC socket -- see [runtime requirements](docs/advanced/ARCHITECTURE.md#runtime-requirements).

Download the latest release for your platform from [Releases](https://github.com/dipievil/discord-proxy-rpc/releases/latest). Each proxy archive contains `discord-proxy`, `discover`, `config.example.yaml`, `LICENSE` and `README.md`.

| Platform | Architecture | Asset |
|----------|--------------|-------|
| Linux | amd64 | `discord-proxy-rpc_<version>_linux_amd64.zip` |
| Linux | arm64 | `discord-proxy-rpc_<version>_linux_arm64.zip` |
| macOS | amd64 | `discord-proxy-rpc_<version>_darwin_amd64.zip` |
| macOS | arm64 | `discord-proxy-rpc_<version>_darwin_arm64.zip` |
| Windows | amd64 | `discord-proxy-rpc_<version>_windows_amd64.zip` |

Linux desktop users who want the system-tray indicator also download `discord-proxy-rpc-tray_<version>_linux_<arch>.zip`; `checksums.txt` on the release page lists the SHA-256 of every archive.

1. Unzip the archive.
2. Copy the example config and set your Application ID:
   ```bash
   cp config.example.yaml config.yaml
   ```
   Set `discord.client_id` in `config.yaml` to your Discord Application ID (see below). Every other key already has a working default.
3. Run the binary:
   ```bash
   ./discord-proxy
   ```
   On Windows, run `discord-proxy.exe` instead.
4. Open `http://<your-host-ip>:8765` in any browser on your LAN.

One-line alternative without a config file:

```bash
PROXY_DISCORD_CLIENT_ID=<your-application-id> ./discord-proxy
```

## Get an Application ID

The proxy needs a Discord Application ID to authenticate via IPC. This takes about 2 minutes.

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications).
2. Click **New Application** and give it a name (e.g., "Discord Proxy RPC").
3. On the **General Information** tab, copy the **Application ID** (also called Client ID).
4. Paste it into `discord.client_id` in `config.yaml`, or set `PROXY_DISCORD_CLIENT_ID`.

**No bot, OAuth2, or redirect URI configuration is required.** Only the Application ID is needed. For custom Rich Presence art assets, see [CONFIGURATION.md](docs/advanced/CONFIGURATION.md#discord-app-setup).

## Dashboard

| Method | URL |
|--------|-----|
| Local | `http://localhost:8765` |
| LAN | `http://<host-ip>:8765` |
| mDNS | `http://<instance-name>.local:8765` |

When `auth.enabled` is true, open the dashboard once with the token in the URL — `http://<host>:8765/?token=<your-token>` — and it stores the token in `sessionStorage` and authenticates the WebSocket on every (re)connect. See [CONFIGURATION.md](docs/advanced/CONFIGURATION.md#dashboard-access) for TLS/mTLS and the full token flow.

## Advanced and reference

| Document | What it covers |
|----------|----------------|
| [ARCHITECTURE.md](docs/advanced/ARCHITECTURE.md) | Diagram, components, activity-source limitation, `cmd/ipc-capture` and `cmd/discover` |
| [CONFIGURATION.md](docs/advanced/CONFIGURATION.md) | Config precedence, full YAML, env vars, TLS/mTLS, dashboard access, Discord app setup |
| [API.md](docs/advanced/API.md) | REST endpoints, health probes, WebSocket protocol, activity types, example consumer, TUI and tray |
| [BUILDING.md](docs/advanced/BUILDING.md) | Build from source, make targets, releases, Docker, contributing |
| [TROUBLESHOOTING.md](docs/advanced/TROUBLESHOOTING.md) | Common connection, dashboard, mDNS, activity-source, auth and TLS problems |
| [SECURITY.md](docs/advanced/SECURITY.md) | Read-only design, LAN exposure, token auth, TLS/mTLS and certificate handling |

## Contributing

See [BUILDING.md](docs/advanced/BUILDING.md#contributing) for the development workflow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
