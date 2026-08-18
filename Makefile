# Discord Proxy RPC - Build Automation
# Usage: make <target>

BINARY_NAME := discord-proxy
BIN_DIR     := bin
MODULE      := github.com/discord-proxy-rpc/discord-proxy-rpc

.PHONY: build build-linux build-windows build-all test lint run dev release clean

## build: Build for current platform
build:
	go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/proxy

## build-linux: Build for Linux amd64
build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/proxy

## build-windows: Build for Windows amd64
build-windows:
	GOOS=windows GOARCH=amd64 go build -o $(BIN_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/proxy

## build-all: Build for all supported platforms
build-all:
	GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/proxy
	GOOS=linux GOARCH=arm64 go build -o $(BIN_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/proxy
	GOOS=windows GOARCH=amd64 go build -o $(BIN_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/proxy
	GOOS=darwin GOARCH=amd64 go build -o $(BIN_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/proxy
	GOOS=darwin GOARCH=arm64 go build -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/proxy

## test: Run all tests with race detection and coverage
##      Falls back to no race detector if gcc is unavailable
test:
	@if command -v gcc > /dev/null 2>&1; then \
		CGO_ENABLED=1 go test -race -cover ./...; \
	else \
		echo "gcc not found, running tests without -race flag"; \
		go test -cover ./...; \
	fi

## lint: Run golangci-lint (if installed)
lint:
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping. Install: https://golangci-lint.run/usage/install/"; \
	fi

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
