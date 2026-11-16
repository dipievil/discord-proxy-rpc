# Troubleshooting

Fixes for the most common connection, dashboard, mDNS, activity-source, auth and TLS problems.

[← Back to README](../../README.md)

## 1. "Could not connect to Discord IPC"

- Ensure the Discord desktop client is running and logged in.
- The proxy must run under the **same OS user** as Discord.
- On Linux, verify `XDG_RUNTIME_DIR` is set (`echo $XDG_RUNTIME_DIR`).
- Discord creates IPC sockets in `/run/user/<uid>/` (Linux) or `%TEMP%` (Windows).
- In Docker: run with `--user "$(id -u):$(id -g)"` and mount the host runtime directory (`-v "/run/user/$(id -u):/run/user/$(id -u):ro"` with `XDG_RUNTIME_DIR` set) — a mismatched UID cannot connect to the socket.

## 2. Dashboard shows "Disconnected"

- Check that the proxy is running and port 8765 is not blocked.
- If using a firewall, allow inbound TCP on port 8765.
- Try `curl http://localhost:8765/health` — should return `200 OK` with IPC state.
- Try `curl http://localhost:8765/healthz` — minimal liveness check, returns `200 OK`.
- Try `curl http://localhost:8765/readyz` — readiness check, returns `200 OK` if IPC connected, `503` if disconnected.
- Open browser developer tools (F12) and check the Console for WebSocket errors.

## 3. mDNS not discovered by LAN clients

- Verify mDNS is enabled in config (`mdns.enabled: true`).
- On Linux, ensure Avahi is running: `systemctl status avahi-daemon`.
- On Windows, ensure Bonjour Print Services is installed.
- Try direct IP access as a fallback: `http://<host-ip>:8765`.

## 4. Dashboard shows "No activity source"

This is expected today. Discord's local IPC socket does not expose another application's Rich Presence, so the proxy has no source to read and reports `source: "none"`.

- Confirm with the capture harness on the host running Discord while a game is open:
  ```bash
  go run ./cmd/ipc-capture -client-id <your-discord-app-id> -duration 60s -out capture.jsonl
  ```
  `No ACTIVITY frame was observed…` confirms there is no IPC source on your setup.
- Verify the snapshot directly: `curl http://localhost:8765/api/presence` returns `{"source":"none","activity":null}`.
- If the harness *does* report `ACTIVITY` frames, please share `capture.jsonl` in an issue so the source can be wired in.

## 5. Presence not updating after a source exists

- If a snapshot reports `source: "ipc"`, the proxy coalesces updates in a 5-second window by default — wait at least 5s.
- Check logs for IPC disconnections (`logging.level: "debug"` for verbose output).

## 6. Auth token rejected

- Verify `auth.enabled: true` and `auth.token` / `PROXY_AUTH_TOKEN` match on both server and client (the legacy `PROXY_TOKEN` env var is no longer read).
- For REST and dashboard requests, send the token as `Authorization: Bearer <token>`, as `?token=<token>`, or open the dashboard with `?token=<token>` in the URL.
- The WebSocket upgrade takes it as `Authorization: Bearer <token>` or the `proxy-token-<base64url(token)>` subprotocol.
- Check for trailing whitespace or newline characters in the token.
- See [CONFIGURATION.md](CONFIGURATION.md#dashboard-access) for the dashboard token flow.

## 7. TLS or mTLS connection fails

- `tls: failed to verify certificate` on the client: the client does not trust the issuer of `server.tls.cert_file`. Point the client at the CA (`curl --cacert`, browser trust store) or use a certificate the client already trusts.
- `remote error: tls: certificate required` or `bad certificate`: mTLS is on and the client sent no certificate, or one not signed by `server.tls.client_ca_file`. Check the chain and that the client certificate has the `clientAuth` extended key usage.
- Startup fails with a `server.tls` error: the configured cert/key/CA path is wrong, the PEM is invalid, or cert and key do not match. The error names the file.
- `mutually exclusive` at startup: `auth.enabled` is still true. Disable bearer auth when using mTLS.
- With mTLS, plain health probes and plain `curl` fail by design; use `--cert`/`--key` or probe from inside the trusted network.
- See [CONFIGURATION.md](CONFIGURATION.md#tls-and-mtls) for the TLS and mTLS settings.
