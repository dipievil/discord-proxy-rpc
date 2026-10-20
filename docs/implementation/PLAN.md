# discord-proxy-rpc — Implementation Plan

## Overview

This plan breaks down the SPEC.md into actionable phases with clear deliverables, dependencies, and acceptance criteria.

---

## Phase 1: Foundation (Week 1)

### Goal
Project scaffolding, configuration, logging, platform abstraction, and build system.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 1.1 | `go.mod` + `go.sum` | Initialize module with all dependencies from SPEC | `go mod tidy` passes, all deps resolved |
| - [x] 1.2 | `internal/config/config.go` | Viper-based config with YAML/env/flags, defaults, logger factory | Loads `config.yaml`, env overrides work, logger created |
| - [x] 1.3 | `internal/platform/ipc_path.go` | Cross-platform Discord IPC socket path resolver | Returns correct paths for Linux/Windows/macOS |
| - [x] 1.4 | `pkg/types/activity.go` | Shared Activity/Timestamps/Assets/Party/Button types | Types match SPEC JSON structure |
| - [x] 1.5 | `Makefile` | Build, test, lint, run, dev, release targets | All targets work, cross-compile produces binaries |
| - [x] 1.6 | `configs/config.example.yaml` | Documented example config with all options | All SPEC config keys present with comments |
| - [x] 1.7 | `cmd/proxy/main.go` | Skeleton entry point: config load, logger, graceful shutdown | Runs, logs startup, exits cleanly on SIGTERM |
| - [x] 1.8 | `.github/workflows/release.yml` | Semantic-release workflow on main push | Triggers on push to main, creates tag, builds binaries, draft release |
| - [x] 1.9 | `.goreleaser.yml` | GoReleaser config: builds, archives, checksums | `goreleaser release --snapshot --clean` works |
| - [ ] 1.10 | `Dockerfile` | Multi-stage build (builder + alpine runtime) | `docker build` produces working image; PR #46 open — tick when merged |
| - [x] 1.11 | `README.md` | Project overview, quick start, Discord app guide | Covers install, config, run, dashboard access |
| - [x] 1.12 | `.github/workflows/ci.yml` | GitHub Actions CI: lint (`go vet`), unit tests with `-race`, build, e2e (`scripts/e2e.sh`) — renumbered from Phase 8 | CI runs on push/PR to dev+main; all four jobs green |

### Milestone
`make run` starts the binary, loads config, logs "starting discord-proxy-rpc", exits cleanly on Ctrl+C.

---

## Phase 2: IPC Client (Week 1-2)

### Goal
Connect to Discord IPC, handle handshake, reconnection, and activity parsing.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 2.1 | `internal/ipc/client.go` | Wrapper around gopresence: connect, handshake, event handlers | Connects to first available `discord-ipc-*`, logs READY |
| - [x] 2.2 | `internal/ipc/reconnect.go` | Exponential backoff (5s→60s, ±10% jitter), max retries | Reconnects after Discord restart, respects max interval |
| - [x] 2.3 | `internal/ipc/parser.go` | Normalize gopresence Activity → `pkg/types.Activity` | All fields mapped, timestamps as Unix milliseconds |
| - [x] 2.4 | `internal/ipc/client.go` | Health check: IPC PING every 30s, detect stale connection | Detects Discord close, triggers reconnect |
| - [x] 2.5 | Unit tests | Mock IPC socket, test parser, reconnect logic | `go test ./internal/ipc/...` passes |

### Milestone
Binary connects to running Discord, logs "connected to Discord as <user>", stays connected across Discord restarts.

---

## Phase 3: Presence State Machine (Week 2)

### Goal
Cache, diff, coalesce, and broadcast presence updates.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 3.1 | `internal/state/presence.go` | State struct: cache, mutex, subscribers, coalesce timer | Thread-safe, no data races under `go test -race` |
| - [x] 3.2 | Diff logic | Deep compare new vs cached activity, emit only on change | Identical sequential updates produce zero broadcasts |
| - [x] 3.3 | Coalesce window | Buffer updates for 5s, emit latest at window end | Burst of 10 updates in 1s → single broadcast at 5s |
| - [x] 3.4 | Subscription API | `Subscribe(fn)`, `Unsubscribe(fn)`, callback execution async | Callbacks never block IPC reader goroutine |
| - [x] 3.5 | Unit tests | Test diff, coalesce, concurrent subscriptions | All state tests pass with `-race` |

### Milestone
State machine receives parsed activities, emits `PresenceUpdate` events only on actual changes, respects 5s coalesce.

---

## Phase 4: LAN Server (Week 2-3)

### Goal
HTTP + WebSocket server with hub, auth, and REST endpoints.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 4.1 | `internal/server/messages.go` | WS message types (encode/decode), Activity JSON marshaling | Round-trip marshal/unmarshal preserves all fields |
| - [x] 4.2 | `internal/server/hub.go` | Client registry, broadcast, subscribe/unsubscribe, cleanup | 100 concurrent clients, broadcast <10ms, no leaks |
| - [x] 4.3 | `internal/server/auth.go` | Optional Bearer token middleware for WS upgrade | Rejects invalid tokens when enabled, passes when disabled |
| - [x] 4.4 | `internal/server/ws.go` | WS handler: upgrade, read loop, ping/pong, message dispatch | Handles subscribe/get_current, pushes presence/state |
| - [x] 4.5 | `internal/server/http.go` | HTTP mux: `/`, `/ws`, `/api/presence`, `/api/state`, `/health` | All endpoints return correct JSON, dashboard served |
| - [x] 4.6 | Static file embedding | `//go:embed web/*` → serve dashboard from binary | `go build` includes web assets, `/` returns HTML |
| - [x] 4.7 | Integration tests | Test WS flow, hub broadcast, auth, REST endpoints | `go test ./internal/server/...` passes |

### Milestone
`curl /api/presence` returns current activity, WS client receives real-time pushes, dashboard loads in browser.

---

## Phase 5: mDNS Announcer (Week 3)

### Goal
Zero-configuration service discovery on LAN.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 5.1 | `internal/mdns/announce.go` | Register `_discord-proxy._tcp.local` with zeroconf | `dns-sd -B _discord-proxy._tcp.local` shows service |
| - [x] 5.2 | TXT record | Include `v=1` (API version) | TXT record present in browse output |
| - [x] 5.3 | Instance name | Configurable, defaults to hostname | Custom name appears in service browser |
| - [x] 5.4 | Graceful deregister | Unregister on shutdown (SIGTERM) | Service disappears immediately on stop |
| - [x] 5.5 | Config toggle | `mdns.enabled: false` disables entirely | No mDNS traffic when disabled |

### Milestone
Service appears in macOS Bonjour Browser / Windows "Network" / `avahi-browse` automatically.

---

## Phase 6: Dashboard Web (Week 3)

### Goal
Single-page vanilla JS dashboard served from binary.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 6.1 | `web/index.html` | Semantic HTML: status badge, presence sections, copy button | Valid HTML5, accessible, mobile viewport |
| - [x] 6.2 | `web/style.css` | CSS variables for themes, responsive grid, status colors | Dark/light auto (prefers-color-scheme), works <400px |
| - [x] 6.3 | `web/app.js` | WS client: connect, subscribe, render presence, reconnect | Auto-reconnects on disconnect, renders all Activity fields |
| - [x] 6.4 | Asset rendering | Large/small images via Discord CDN (`cdn.discordapp.com`) | Images load, fallback to placeholder on error |
| - [x] 6.5 | Timestamp countdown | Live "elapsed" / "remaining" for start/end timestamps | Updates every second without WS traffic |
| - [x] 6.6 | Copy JSON button | Copies full presence JSON to clipboard | Toast notification on success |

### Milestone
Open `http://host:8765/` → shows live presence with auto-updating timestamps, works on phone.

---

## Phase 7: Integration & Polish (Week 3-4)

### Goal
Wire everything together, hardening, documentation.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 7.1 | `cmd/proxy/main.go` | Full wiring: config → logger → IPC → state → server → mDNS → signals | All components start/stop in correct order |
| - [x] 7.2 | Graceful shutdown | SIGTERM: stop mDNS, close WS clients, disconnect IPC, flush logs | No goroutine leaks, exits in <2s |
| - [x] 7.3 | Health endpoint | `/health` returns 200 with `{status: "ok"}` | Kubernetes liveness probe compatible |
| - [x] 7.4 | Error handling | Structured logging for all error paths, no panics | `zap.Error` used, stack traces on panic |
| - [x] 7.5 | Rate limiting | WS message read limit, HTTP body size limit | Prevents DoS from malicious clients |
| - [x] 7.6 | README.md | Complete: install, config, Discord app, run, dashboard, build | New user can run from scratch in <5 min |
| - [x] 7.7 | E2E test script | `scripts/e2e.sh`: start proxy, connect mock Discord, verify WS | CI can run full stack test |
| - [x] 7.8 | Panic recovery middleware | Wire `RecoveryMiddleware` into `Server.ServeHTTP` (was dead code) | Panicking HTTP handler returns 500 `INT_001` instead of crashing the process |
| - [x] 7.9 | `client_id` in WS state messages | Set `Hub.ClientID` so `state`/`current` messages carry the Discord App ID | `state`/`current` frames include `client_id` when configured |
| - [x] 7.10 | Auth deduplication | Collapse `main.go` `newAuthFunc` vs `server.AuthMiddleware` into a single `BearerAuthFunc` validation path | One shared token validation path for HTTP and WS |
| - [x] 7.11 | Health endpoint docs | Document `/healthz` and `/readyz`; reconcile the `/health` payload with the SPEC | SPEC/README endpoint tables match the implementation |
| - [x] 7.12 | Error codes + WS error frames | Wire up the error-code constants (`AUTH_001`…`REQ_003`); reply to invalid WS messages with an `error` frame | Invalid client messages get `{type:"error",payload:{code,message}}`; no unused error constants |
| - [x] 7.13 | E2E in CI | `make e2e` target + CI job running `scripts/e2e.sh` (incl. broken-start detection) | CI e2e job fails on a broken start, passes on a green build |

All Phase 7 tasks are merged on `dev` (Trello cards in Done).

### Milestone
`make release` produces signed binaries for all platforms, `docker build` works, README is complete.

---

## Phase 8: Post-review bugfix & CI hardening

### Goal
Fix defects found after integration review and harden the CI pipeline.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| - [x] 8.1 | Windows Discord pipe path | Return the actual Windows named-pipe path (`\\.\pipe\discord-ipc-{0..9}`) from the platform resolver | Windows build resolves a real Discord pipe path |
| - [x] 8.2 | Bind documented auth token env | Bind `PROXY_AUTH_TOKEN` in config, keeping legacy `PROXY_TOKEN` as fallback | Documented env vars actually set `auth.token` |
| - [ ] 8.3 | Flaky IPC healthcheck test | Fix `panic: close of nil channel` flake in the `internal/ipc` healthcheck test | `go test ./internal/ipc/... -count=1` passes repeatedly |
| - [ ] 8.4 | Presence Start/Stop deadlock | Fix deadlock that hangs `TestConcurrentUpdateAndRead` (dev CI red) | `go test ./internal/state/... -race` green, no hangs |
| - [ ] 8.5 | Explicit test/job timeouts | Add `timeout-minutes` to CI jobs and `-timeout` to `go test` invocations | No job can hang for the default runner timeout again |
| - [x] 8.6 | Harden CI lint | Run real `golangci-lint` in CI (lint job only runs `go vet`; `make lint` silently skips when not installed) | `golangci-lint run` is a required CI gate — also unlocks the phase DoD lint checkbox |
| - [ ] 8.7 | CI hygiene | Remove redundant gcc install steps, add concurrency cancellation for superseded runs | No redundant steps; in-flight runs cancelled on rapid pushes |
| - [ ] 8.8 | Fix e2e broken-start detection | `scripts/e2e.sh` must fail when the binary does not start (port occupied / early exit) | `e2e.sh` exits non-zero on a broken start and CI verifies it (code merged via #40; Trello card still open under legacy numbering) |
| - [ ] 8.11 | Activity-feed contract (Codex P1 on #44) | Discord IPC cannot observe another application's Rich Presence, so the presence contract must say so: add an explicit `source: "none"` state to `/api/presence`, WS `presence`/`current` and the dashboard, and ship `cmd/ipc-capture` to prove the limitation on a host | Snapshots carry `{"source":"none\|ipc","activity":Activity\|null}`; an empty feed is never indistinguishable from "no source"; SPEC/README/AGENTS revised; unit tests green |

> Note: the GitHub Actions CI task was renumbered from Phase 8 to **1.12** and lives in Phase 1 (card in Done).

---

## Phase 9: Hardening & release

### Goal
Close the remaining security gaps and cut the first public release.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| [ ] 9.1 | REST/dashboard auth coverage | Extend token auth beyond the WS upgrade to `/` and `/api/*` (decide for `/health*`) | With `auth.enabled: true`, sensitive endpoints reject unauthenticated requests |
| [ ] 9.2 | Rate-limit REST/dashboard endpoints | Apply the per-IP rate limiter to HTTP routes, not only WS upgrades | Abuse of REST endpoints is throttled with 429 |
| [ ] 9.3 | Fail fast on missing `discord.client_id` | Refuse to start with a clear error when `discord.client_id` is empty | Misconfigured runs abort at startup instead of silently flapping |
| [ ] 9.4 | Cut first release | Merge `dev` → `main`, tag `v0.1.0`, publish release binaries | v0.1.0 published with binaries for all platforms |
| [ ] 9.5 | Branch protection + required review | Protect `main`/`dev`: require PR + green CI + at least 1 review | Direct pushes blocked; CI required to merge |
| [ ] 9.6 | Remove legacy `PROXY_TOKEN` env var | Drop the `PROXY_TOKEN` override in `internal/config/config.go` (`setupEnv`); bind `auth.token` to `PROXY_AUTH_TOKEN` only; update `TestProxyTokenEnvVar` in `config_test.go` and all docs mentioning the legacy var (SPEC §Auth, README, `configs/config.example.yaml`). Currently `PROXY_TOKEN` wins over `PROXY_AUTH_TOKEN` when both are set — a migration footgun | `PROXY_TOKEN` is ignored; `PROXY_AUTH_TOKEN` is the sole env source; released as a breaking change (`feat!:` + `BREAKING CHANGE` footer) |

---

## Phase 10: Future extensions [v1.1]

### Goal
Post-v0.1 extensions; out of scope for the first release.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| [ ] 10.1 | Prometheus `/metrics` | Expose presence updates, WS client count, IPC state as Prometheus metrics | `/metrics` is scrapable by a Prometheus server |
| [ ] 10.2 | TUI dashboard | Terminal UI (bubbletea) mirroring the web dashboard | Runs from the binary/flag and renders live presence |
| [ ] 10.3 | System tray indicator | Tray icon on the host machine showing Discord connection state | Icon reflects connected/disconnected in real time |
| [ ] 10.4 | Client SDK / CLI example | Reference client (Go lib + CLI) for LAN consumers | Example app consumes the WS API in <20 lines |
| [ ] 10.5 | mDNS browse/discovery helper | CLI helper to browse `_discord-proxy._tcp` and print endpoints | Helper prints host:port of running proxies on the LAN |

---

## Dependencies Between Phases

```
Phase 1 (Foundation)
    ├──→ Phase 2 (IPC Client) ──────┐
    ├──→ Phase 3 (State Machine) ←──┤
    ├──→ Phase 4 (LAN Server)  ←────┤
    ├──→ Phase 5 (mDNS)        ←────┤
    └──→ Phase 6 (Dashboard)   ←────┘
                │
                └──→ Phase 7 (Integration)
                        └──→ Phase 8 (Post-review bugfix & CI hardening)
                                └──→ Phase 9 (Hardening & release)
                                        └──→ Phase 10 (Future extensions, v1.1)
```

- Phase 1 must complete first
- Phases 2-6 can proceed in parallel after Phase 1
- Phase 7 requires all prior phases
- Phase 8 fixes defects found in Phases 1-7; Phase 9 gates the first release; Phase 10 is out of scope for v0.1

---

## Estimated Timeline

| Phase | Duration | Cumulative |
|-------|----------|------------|
| 1. Foundation | 2-3 days | 2-3 days |
| 2. IPC Client | 3-4 days | 5-7 days |
| 3. State Machine | 1-2 days | 6-9 days |
| 4. LAN Server | 3-4 days | 9-13 days |
| 5. mDNS | 1 day | 10-14 days |
| 6. Dashboard | 2-3 days | 12-17 days |
| 7. Integration | 2-3 days | 14-20 days |
| **Total** | | **~3-4 weeks** |

*Assumes part-time (2-3h/day). Full-time could be ~1.5 weeks.*

*Timeline covers Phases 1-7 (v0.1 core). Phases 8-10 are tracked incrementally on the Trello board.*

---

## Risk Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| gopresence API changes | Breaks IPC client | Pin exact commit in go.mod, vendor if needed |
| Discord IPC protocol changes | Connection fails | Monitor discord-rpc repo, test with Canary |
| mDNS not working on some networks | No auto-discovery | Document manual IP fallback, make mDNS optional |
| WebSocket issues behind proxies | LAN clients can't connect | Document WebSocket proxy config, offer HTTP polling fallback |
| Race conditions in state machine | Data corruption | `go test -race` in CI, extensive concurrency tests |

---

## Definition of Done (Per Phase)

- [ ] All tasks completed
- [ ] Unit tests pass (`go test ./... -race`)
- [ ] Code compiles without warnings
- [x] `golangci-lint run` passes
- [ ] Manual verification of milestone
- [ ] Documentation updated (README, code comments)

---

## Next Steps

1. **Phases 1-7 are complete and merged on `dev`** — all Trello cards are in Done (including 1.12 = CI workflow)
2. Clear the open Phase 8 bugs: flaky IPC healthcheck test (8.3) and the presence Start/Stop deadlock (8.4) to get dev CI fully green
3. Harden CI: explicit test/job timeouts (8.5), real `golangci-lint` gate (8.6), CI hygiene (8.7) — 8.6 also unlocks the per-phase "Definition of Done" lint checkbox
4. Then Phase 9: REST/dashboard auth (9.1/9.2), fail-fast `discord.client_id` (9.3), cut v0.1.0 (9.4) and set up branch protection (9.5)