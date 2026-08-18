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
| 1.1 | `go.mod` + `go.sum` | Initialize module with all dependencies from SPEC | `go mod tidy` passes, all deps resolved |
| 1.2 | `internal/config/config.go` | Viper-based config with YAML/env/flags, defaults, logger factory | Loads `config.yaml`, env overrides work, logger created |
| 1.3 | `internal/platform/ipc_path.go` | Cross-platform Discord IPC socket path resolver | Returns correct paths for Linux/Windows/macOS |
| 1.4 | `pkg/types/activity.go` | Shared Activity/Timestamps/Assets/Party/Button types | Types match SPEC JSON structure |
| 1.5 | `Makefile` | Build, test, lint, run, dev, release targets | All targets work, cross-compile produces binaries |
| 1.6 | `configs/config.example.yaml` | Documented example config with all options | All SPEC config keys present with comments |
| 1.7 | `cmd/proxy/main.go` | Skeleton entry point: config load, logger, graceful shutdown | Runs, logs startup, exits cleanly on SIGTERM |
| 1.8 | `.github/workflows/release.yml` | GoReleaser workflow for tag pushes | Triggers on `v*`, builds all platforms |
| 1.9 | `.goreleaser.yml` | GoReleaser config: builds, archives, checksums | `goreleaser release --snapshot --clean` works |
| 1.10 | `Dockerfile` | Multi-stage build (builder + alpine runtime) | `docker build` produces working image |
| 1.11 | `README.md` | Project overview, quick start, Discord app guide | Covers install, config, run, dashboard access |

### Milestone
`make run` starts the binary, loads config, logs "starting discord-proxy-rpc", exits cleanly on Ctrl+C.

---

## Phase 2: IPC Client (Week 1-2)

### Goal
Connect to Discord IPC, handle handshake, reconnection, and activity parsing.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| 2.1 | `internal/ipc/client.go` | Wrapper around gopresence: connect, handshake, event handlers | Connects to first available `discord-ipc-*`, logs READY |
| 2.2 | `internal/ipc/reconnect.go` | Exponential backoff (5s→60s, ±10% jitter), max retries | Reconnects after Discord restart, respects max interval |
| 2.3 | `internal/ipc/parser.go` | Normalize gopresence Activity → `pkg/types.Activity` | All fields mapped, timestamps as Unix milliseconds |
| 2.4 | `internal/ipc/client.go` | Health check: IPC PING every 30s, detect stale connection | Detects Discord close, triggers reconnect |
| 2.5 | Unit tests | Mock IPC socket, test parser, reconnect logic | `go test ./internal/ipc/...` passes |

### Milestone
Binary connects to running Discord, logs "connected to Discord as <user>", stays connected across Discord restarts.

---

## Phase 3: Presence State Machine (Week 2)

### Goal
Cache, diff, coalesce, and broadcast presence updates.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| 3.1 | `internal/state/presence.go` | State struct: cache, mutex, subscribers, coalesce timer | Thread-safe, no data races under `go test -race` |
| 3.2 | Diff logic | Deep compare new vs cached activity, emit only on change | Identical sequential updates produce zero broadcasts |
| 3.3 | Coalesce window | Buffer updates for 5s, emit latest at window end | Burst of 10 updates in 1s → single broadcast at 5s |
| 3.4 | Subscription API | `Subscribe(fn)`, `Unsubscribe(fn)`, callback execution async | Callbacks never block IPC reader goroutine |
| 3.5 | Unit tests | Test diff, coalesce, concurrent subscriptions | All state tests pass with `-race` |

### Milestone
State machine receives parsed activities, emits `PresenceUpdate` events only on actual changes, respects 5s coalesce.

---

## Phase 4: LAN Server (Week 2-3)

### Goal
HTTP + WebSocket server with hub, auth, and REST endpoints.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| 4.1 | `internal/server/messages.go` | WS message types (encode/decode), Activity JSON marshaling | Round-trip marshal/unmarshal preserves all fields |
| 4.2 | `internal/server/hub.go` | Client registry, broadcast, subscribe/unsubscribe, cleanup | 100 concurrent clients, broadcast <10ms, no leaks |
| 4.3 | `internal/server/auth.go` | Optional Bearer token middleware for WS upgrade | Rejects invalid tokens when enabled, passes when disabled |
| 4.4 | `internal/server/ws.go` | WS handler: upgrade, read loop, ping/pong, message dispatch | Handles subscribe/get_current, pushes presence/state |
| 4.5 | `internal/server/http.go` | HTTP mux: `/`, `/ws`, `/api/presence`, `/api/state`, `/health` | All endpoints return correct JSON, dashboard served |
| 4.6 | Static file embedding | `//go:embed web/*` → serve dashboard from binary | `go build` includes web assets, `/` returns HTML |
| 4.7 | Integration tests | Test WS flow, hub broadcast, auth, REST endpoints | `go test ./internal/server/...` passes |

### Milestone
`curl /api/presence` returns current activity, WS client receives real-time pushes, dashboard loads in browser.

---

## Phase 5: mDNS Announcer (Week 3)

### Goal
Zero-configuration service discovery on LAN.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| 5.1 | `internal/mdns/announce.go` | Register `_discord-proxy._tcp.local` with zeroconf | `dns-sd -B _discord-proxy._tcp.local` shows service |
| 5.2 | TXT record | Include `v=1` (API version) | TXT record present in browse output |
| 5.3 | Instance name | Configurable, defaults to hostname | Custom name appears in service browser |
| 5.4 | Graceful deregister | Unregister on shutdown (SIGTERM) | Service disappears immediately on stop |
| 5.5 | Config toggle | `mdns.enabled: false` disables entirely | No mDNS traffic when disabled |

### Milestone
Service appears in macOS Bonjour Browser / Windows "Network" / `avahi-browse` automatically.

---

## Phase 6: Dashboard Web (Week 3)

### Goal
Single-page vanilla JS dashboard served from binary.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| 6.1 | `web/index.html` | Semantic HTML: status badge, presence sections, copy button | Valid HTML5, accessible, mobile viewport |
| 6.2 | `web/style.css` | CSS variables for themes, responsive grid, status colors | Dark/light auto (prefers-color-scheme), works <400px |
| 6.3 | `web/app.js` | WS client: connect, subscribe, render presence, reconnect | Auto-reconnects on disconnect, renders all Activity fields |
| 6.4 | Asset rendering | Large/small images via Discord CDN (`cdn.discordapp.com`) | Images load, fallback to placeholder on error |
| 6.5 | Timestamp countdown | Live "elapsed" / "remaining" for start/end timestamps | Updates every second without WS traffic |
| 6.6 | Copy JSON button | Copies full presence JSON to clipboard | Toast notification on success |

### Milestone
Open `http://host:8765/` → shows live presence with auto-updating timestamps, works on phone.

---

## Phase 7: Integration & Polish (Week 3-4)

### Goal
Wire everything together, hardening, documentation.

### Tasks

| ID | Task | Description | Acceptance Criteria |
|----|------|-------------|---------------------|
| 7.1 | `cmd/proxy/main.go` | Full wiring: config → logger → IPC → state → server → mDNS → signals | All components start/stop in correct order |
| 7.2 | Graceful shutdown | SIGTERM: stop mDNS, close WS clients, disconnect IPC, flush logs | No goroutine leaks, exits in <2s |
| 7.3 | Health endpoint | `/health` returns 200 with `{status: "ok"}` | Kubernetes liveness probe compatible |
| 7.4 | Error handling | Structured logging for all error paths, no panics | `zap.Error` used, stack traces on panic |
| 7.5 | Rate limiting | WS message read limit, HTTP body size limit | Prevents DoS from malicious clients |
| 7.6 | README.md | Complete: install, config, Discord app, run, dashboard, build | New user can run from scratch in <5 min |
| 7.7 | E2E test script | `scripts/e2e.sh`: start proxy, connect mock Discord, verify WS | CI can run full stack test |

### Milestone
`make release` produces signed binaries for all platforms, `docker build` works, README is complete.

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
```

- Phase 1 must complete first
- Phases 2-6 can proceed in parallel after Phase 1
- Phase 7 requires all prior phases

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
- [ ] `golangci-lint run` passes
- [ ] Manual verification of milestone
- [ ] Documentation updated (README, code comments)

---

## Next Steps

1. **Start Phase 1** — Already partially done (config, ipc_path, go.mod exist)
2. Complete remaining Phase 1 tasks (Makefile, main.go, CI, Docker, README skeleton)
3. Begin Phase 2 (IPC Client) in parallel with Phase 1 completion