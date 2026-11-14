# Discord Proxy RPC - Build Automation
# Usage: make <target>

BINARY_NAME := discord-proxy
BIN_DIR     := bin
MODULE      := github.com/discord-proxy-rpc/discord-proxy-rpc

# Version stamping: goreleaser sets these at release time; local builds get the
# closest tag (or commit) so `discord-proxy -version` is never a lie.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS        := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
TRAY_LDFLAGS   := -X main.version=$(VERSION)
PROXY_LDFLAGS  := -s -w $(LDFLAGS)
PROXY_BUILD    := go build -trimpath -ldflags "$(PROXY_LDFLAGS)"
TRAY_BUILD     := go build -trimpath -ldflags "$(TRAY_LDFLAGS)"

.PHONY: build build-linux build-windows build-all tui tray test lint run dev release clean e2e capture discover

## build: Build for current platform
build:
	$(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/proxy

## tui: Build the terminal dashboard (TUI consumer) for the current platform
tui:
	go build -o $(BIN_DIR)/$(BINARY_NAME)-tui ./cmd/tui

## tray: Build the system-tray indicator (Linux StatusNotifierItem)
tray:
	$(TRAY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-tray ./cmd/tray

## build-linux: Build for Linux amd64
build-linux:
	GOOS=linux GOARCH=amd64 $(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/proxy

## build-windows: Build for Windows amd64
build-windows:
	GOOS=windows GOARCH=amd64 $(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/proxy

## build-all: Build for all supported platforms
build-all:
	GOOS=linux GOARCH=amd64 $(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/proxy
	GOOS=linux GOARCH=arm64 $(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/proxy
	GOOS=windows GOARCH=amd64 $(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/proxy
	GOOS=darwin GOARCH=amd64 $(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/proxy
	GOOS=darwin GOARCH=arm64 $(PROXY_BUILD) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/proxy

## test: Run all tests with race detection and coverage
##      Falls back to no race detector if gcc is unavailable
test:
	@if command -v gcc > /dev/null 2>&1; then \
		CGO_ENABLED=1 go test -race -cover -timeout 5m ./...; \
	else \
		echo "gcc not found, running tests without -race flag"; \
		go test -cover -timeout 5m ./...; \
	fi

## lint: Run golangci-lint (required; fails if not installed)
lint:
	@command -v golangci-lint > /dev/null 2>&1 || { \
		echo "golangci-lint not installed. Install: https://golangci-lint.run/usage/install/"; \
		exit 1; \
	}
	@golangci-lint run

## run: Run the proxy server
run:
	go run ./cmd/proxy

## dev: Run with hot reload via air (if installed)
dev:
	@if command -v air > /dev/null 2>&1; then \
		air; \
	else \
		echo "air not installed, falling back to 'make run'. Install: go install github.com/air-verse/air@latest"; \
		$(MAKE) run; \
	fi

## release: Build and publish release via goreleaser
release:
	goreleaser release --clean

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR)

## e2e: Run end-to-end tests (requires gcc for race detector)
e2e:
	@if command -v gcc > /dev/null 2>&1; then \
		E2E_PORT=$${E2E_PORT:-18765} scripts/e2e.sh; \
	else \
		echo "gcc not found, e2e tests require race detector. Install gcc to run e2e tests."; \
		exit 1; \
	fi

## capture: Record raw Discord IPC frames (requires CLIENT_ID=<discord-app-id>)
##          Optional: DURATION (default 60s), OUT (JSON Lines capture file)
capture:
	go run ./cmd/ipc-capture -client-id "$(CLIENT_ID)" -duration "$${DURATION:-60s}" $${OUT:+-out "$$OUT"}

## discover: Browse the LAN for running proxies via mDNS
##           Optional: TIMEOUT (default 5s), JSON (non-empty enables -json)
discover:
	go run ./cmd/discover -timeout "$${TIMEOUT:-5s}" $${JSON:+-json}
