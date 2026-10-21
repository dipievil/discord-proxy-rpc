#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="/tmp/discord-proxy-e2e"
BINARY_LOG="/tmp/discord-proxy-e2e.log"
COOKIE_JAR="/tmp/discord-proxy-e2e-cookies"
PORT="${E2E_PORT:-18765}"
SKIP_UNIT="${E2E_SKIP_UNIT:-0}"
# Bounded startup/health deadline: the proxy must serve /health within this
# many seconds of being launched, otherwise this script exits non-zero
# immediately. Any CI step that occupies the E2E_PORT must hold it for longer
# than this deadline.
STARTUP_TIMEOUT="${E2E_STARTUP_TIMEOUT:-10}"
PID=""
PASS=0
FAIL=0

cleanup() {
    if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
        kill "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null || true
    fi
    rm -f "$BINARY" "$BINARY_LOG" "$COOKIE_JAR"
}
trap cleanup EXIT

pass() { PASS=$((PASS + 1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL + 1)); echo "  FAIL: $1"; }

http_code() { curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$@"; }

summary() {
    echo ""
    echo "=== Results: ${PASS}/$((PASS + FAIL)) passed, ${FAIL}/$((PASS + FAIL)) failed ==="
}

dump_binary_log() {
    echo "  Binary output:"
    tail -20 "$BINARY_LOG" | sed 's/^/    /'
}

echo "=== Discord Proxy RPC - E2E Tests ==="
echo ""

# --- Phase 1: Build verification ---
echo "--- Phase 1: Build ---"
if (cd "$ROOT_DIR" && go build -o "$BINARY" ./cmd/proxy); then
    pass "Binary compiled successfully"
else
    fail "Binary compilation failed"
    echo "Build failed, aborting."
    exit 1
fi
echo ""

# --- Phase 2: Go unit + integration tests ---
# Run the whole package: a name-based -run regex silently misses tests and
# picks up unrelated ones whose names happen to share a prefix.
if [[ "$SKIP_UNIT" == "1" ]]; then
    echo "--- Phases 2 & 3 skipped (E2E_SKIP_UNIT=1; covered by the 'test' CI job) ---"
    echo ""
else
echo "--- Phase 2: Unit & Integration Tests ---"
if (cd "$ROOT_DIR" && go test -race -count=1 -timeout 120s ./internal/server/ -v 2>&1 | tail -40); then
    pass "Go integration tests passed"
else
    fail "Go integration tests failed"
fi
echo ""

# --- Phase 3: E2E programmatic test ---
echo "--- Phase 3: E2E Full Stack Test ---"
if (cd "$ROOT_DIR" && go test -race -count=1 -timeout 60s ./internal/server/ -run '^TestE2E' -v 2>&1 | tail -40); then
    pass "E2E full stack test passed"
else
    fail "E2E full stack test failed"
fi
echo ""
fi

# --- Phase 4: Script-level E2E against running binary ---
# The binary has no CLI flags; all settings come from PROXY_* env vars
# (internal/config binds them via viper).
echo "--- Phase 4: Binary E2E (HTTP endpoint smoke test) ---"

if (exec 3<>"/dev/tcp/127.0.0.1/${PORT}") 2>/dev/null; then
    exec 3<&- 3>&-
    fail "Port ${PORT} is already in use; set E2E_PORT to a free port"
    summary
    exit 1
fi

PROXY_DISCORD_CLIENT_ID="000000000000000000" \
PROXY_DISCORD_AUTO_RECONNECT="false" \
PROXY_SERVER_HOST="127.0.0.1" \
PROXY_SERVER_PORT="$PORT" \
PROXY_SERVER_WS_PATH="/ws" \
PROXY_AUTH_ENABLED="false" \
PROXY_MDNS_ENABLED="false" \
PROXY_LOGGING_LEVEL="error" \
PROXY_LOGGING_FORMAT="console" \
    "$BINARY" >"$BINARY_LOG" 2>&1 &
PID=$!

BASE="http://127.0.0.1:${PORT}"

echo "  Waiting up to ${STARTUP_TIMEOUT}s for server on port ${PORT}..."
READY=0
DEADLINE=$((SECONDS + STARTUP_TIMEOUT))
while (( SECONDS < DEADLINE )); do
    if ! kill -0 "$PID" 2>/dev/null; then
        set +e
        wait "$PID"
        exit_code=$?
        set -e
        PID=""
        dump_binary_log
        fail "Binary exited during startup (exit code ${exit_code})"
        summary
        exit 1
    fi
    if curl -sf --max-time 1 "$BASE/health" >/dev/null 2>&1; then
        READY=1
        break
    fi
    sleep 0.5
done

if [[ "$READY" -eq 1 ]]; then
    pass "Server became ready on port ${PORT}"

    HEALTH=$(curl -sf "$BASE/health" || true)
    if echo "$HEALTH" | grep -q '"ok"'; then
        pass "/health returns 200 with status ok"
    else
        fail "/health unexpected response: ${HEALTH:-<no response>}"
    fi

    STATE=$(curl -sf "$BASE/api/state" || true)
    if echo "$STATE" | grep -q '"status"'; then
        pass "/api/state returns valid JSON"
    else
        fail "/api/state unexpected response: ${STATE:-<no response>}"
    fi

    PRES=$(curl -sf "$BASE/api/presence" || true)
    if echo "$PRES" | grep -q '"source"' && echo "$PRES" | grep -q '"activity"'; then
        pass "/api/presence returns a presence snapshot (source/activity)"
    else
        fail "/api/presence unexpected response: ${PRES:-<no response>}"
    fi

    HTML=$(curl -sf "$BASE/" || true)
    if echo "$HTML" | grep -q "Discord Proxy RPC"; then
        pass "/ serves dashboard HTML"
    else
        fail "/ did not return expected dashboard HTML"
    fi

    CODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/nope" || true)
    if [[ "$CODE" == "404" ]]; then
        pass "unknown route returns 404"
    else
        fail "unknown route returned ${CODE:-000}, want 404"
    fi
else
    dump_binary_log
    fail "Server did not become ready on port ${PORT} within ${STARTUP_TIMEOUT}s"
    summary
    exit 1
fi

# --- Phase 4b: Binary E2E with auth enabled ---
# With auth on, the data surface (dashboard + /api/*) must reject anonymous
# requests, health probes stay open for k8s, and the token must be accepted as
# an Authorization header and as ?token= on the dashboard.
echo "--- Phase 4b: Binary E2E (auth enabled) ---"

AUTH_TOKEN="e2e-auth-token"

# The phase-4 server is still running on ${PORT}; stop it before reusing the port.
kill "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true
PID=""
rm -f "$BINARY_LOG" "$COOKIE_JAR"

PROXY_DISCORD_CLIENT_ID="000000000000000000" \
PROXY_DISCORD_AUTO_RECONNECT="false" \
PROXY_SERVER_HOST="127.0.0.1" \
PROXY_SERVER_PORT="$PORT" \
PROXY_SERVER_WS_PATH="/ws" \
PROXY_AUTH_ENABLED="true" \
PROXY_AUTH_TOKEN="$AUTH_TOKEN" \
PROXY_MDNS_ENABLED="false" \
PROXY_LOGGING_LEVEL="error" \
PROXY_LOGGING_FORMAT="console" \
    "$BINARY" >"$BINARY_LOG" 2>&1 &
PID=$!

echo "  Waiting up to ${STARTUP_TIMEOUT}s for auth-enabled server on port ${PORT}..."
READY=0
DEADLINE=$((SECONDS + STARTUP_TIMEOUT))
while (( SECONDS < DEADLINE )); do
    if ! kill -0 "$PID" 2>/dev/null; then
        set +e
        wait "$PID"
        exit_code=$?
        set -e
        PID=""
        dump_binary_log
        fail "Binary (auth-enabled) exited during startup (exit code ${exit_code})"
        summary
        exit 1
    fi
    if curl -sf --max-time 1 "$BASE/health" >/dev/null 2>&1; then
        READY=1
        break
    fi
    sleep 0.5
done

if [[ "$READY" -eq 1 ]]; then
    pass "Auth-enabled server became ready on port ${PORT}"

    for ep in /health /healthz; do
        CODE=$(http_code "$BASE$ep")
        if [[ "$CODE" == "200" ]]; then
            pass "${ep} stays open without a token (auth enabled)"
        else
            fail "${ep} returned ${CODE} without a token, want 200"
        fi
    done

    for ep in /api/presence /api/state /; do
        CODE=$(http_code "$BASE$ep")
        if [[ "$CODE" == "401" ]]; then
            pass "${ep} returns 401 without a token (auth enabled)"
        else
            fail "${ep} returned ${CODE} without a token, want 401"
        fi
    done

    for ep in /api/presence /; do
        CODE=$(http_code -H "Authorization: Bearer ${AUTH_TOKEN}" "$BASE$ep")
        if [[ "$CODE" == "200" ]]; then
            pass "${ep} accepts the bearer token (auth enabled)"
        else
            fail "${ep} returned ${CODE} with a valid bearer token, want 200"
        fi
    done

    CODE=$(http_code -H "Authorization: Bearer wrong-token" "$BASE/api/presence")
    if [[ "$CODE" == "401" ]]; then
        pass "/api/presence rejects a wrong bearer token (auth enabled)"
    else
        fail "/api/presence returned ${CODE} with a wrong token, want 401"
    fi

    CODE=$(http_code -c "$COOKIE_JAR" "$BASE/?token=${AUTH_TOKEN}")
    if [[ "$CODE" == "200" && -s "$COOKIE_JAR" ]]; then
        pass "/ accepts the ?token= parameter and sets an auth cookie (auth enabled)"
    else
        fail "/ with ?token= returned ${CODE} and did not set an auth cookie"
    fi

    CODE=$(http_code -b "$COOKIE_JAR" "$BASE/app.js")
    if [[ "$CODE" == "200" ]]; then
        pass "/app.js authenticates with the auth cookie (auth enabled)"
    else
        fail "/app.js returned ${CODE} with the auth cookie, want 200"
    fi

    CODE=$(http_code "$BASE/app.js")
    if [[ "$CODE" == "401" ]]; then
        pass "/app.js returns 401 without a token (auth enabled)"
    else
        fail "/app.js returned ${CODE} without a token, want 401"
    fi
else
    dump_binary_log
    fail "Auth-enabled server did not become ready on port ${PORT} within ${STARTUP_TIMEOUT}s"
    summary
    exit 1
fi

kill "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true
PID=""
echo ""

# --- Phase 5: Full race-condition test ---
if [[ "$SKIP_UNIT" == "1" ]]; then
    echo "--- Phase 5 skipped (E2E_SKIP_UNIT=1; covered by the 'test' CI job) ---"
    echo ""
else
echo "--- Phase 5: Race Detection (full suite) ---"
if (cd "$ROOT_DIR" && go test -race -count=1 -timeout 120s ./... 2>&1 | tail -20); then
    pass "Full test suite passed with race detector"
else
    fail "Full test suite failed or had race conditions"
fi
echo ""
fi

# --- Summary ---
summary
if [[ "$FAIL" -gt 0 ]]; then
    exit 1
fi
exit 0
