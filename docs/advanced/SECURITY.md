# Security

The proxy's security posture, LAN exposure, authentication, TLS/mTLS and certificate handling.

[← Back to README](../../README.md)

## Security considerations

- **Read-only by design** -- The proxy never sends `SET_ACTIVITY` or any write command to Discord. It only observes.
- **LAN only** -- Binds to `0.0.0.0` by default. Restrict via firewall rules or bind to `127.0.0.1` and use a VPN (e.g., Tailscale) for remote access.
- **Optional token auth** -- Enable `auth.enabled: true` and set a strong token via `PROXY_AUTH_TOKEN` (or `auth.token` in the config file). It protects the WebSocket upgrade, the REST endpoints (`/api/*`) and the dashboard; the health probes stay open for orchestrators. Without auth, any device on the LAN can read the presence data. See [CONFIGURATION.md](CONFIGURATION.md#dashboard-access) for the dashboard token flow.
- **No persistence** -- No database, no log files, no presence data stored on disk. Everything is in-memory and lost on restart.
- **mDNS is informational only** -- The mDNS TXT record contains only the API version (`v=1`) and, with TLS enabled, the mode (`tls=server` or `tls=mtls`). No sensitive data is broadcast.
- **Transport encryption (opt-in)** -- `server.tls.enabled: true` serves the whole surface over HTTPS/WSS with TLS 1.2 as the minimum. Without it, WebSocket traffic is plaintext; alternatively terminate TLS in a reverse proxy (nginx, Caddy). See [CONFIGURATION.md](CONFIGURATION.md#tls-and-mtls) for setup.
- **Mutual TLS (opt-in)** -- `server.tls.client_ca_file` requires a client certificate chained to that CA on every connection, including health probes and `/metrics`. This authenticates the client and encrypts the transport, and it replaces bearer-token auth (the two cannot be combined). It is authentication, not authorisation: any certificate issued by the CA can read the presence data, so keep the client CA separate and narrow. See [CONFIGURATION.md](CONFIGURATION.md#tls-and-mtls).
- **Certificate material** -- Files are read once at startup; an unreadable or mismatched pair aborts startup. Private keys are never logged. Reloading a renewed certificate requires a restart.

## See also

- Token auth, dashboard access and TLS/mTLS settings: [CONFIGURATION.md](CONFIGURATION.md#dashboard-access) and [CONFIGURATION.md](CONFIGURATION.md#tls-and-mtls)
- API access control, rate limiting and health probes: [API.md](API.md#rest-endpoints)
