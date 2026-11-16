# Building

Build from source, the make targets, releases, Docker and the contributing workflow.

[← Back to README](../../README.md)

## Prerequisites

- Go 1.26.5 or later
- GCC (optional, for race detector in tests)

## Build from source

```bash
git clone https://github.com/dipievil/discord-proxy-rpc.git
cd discord-proxy-rpc
make build
./bin/discord-proxy -version   # prints version, commit and build date
./bin/discord-proxy
```

## Make commands

| Command | Description |
|---------|-------------|
| `make build` | Build for current platform |
| `make build-linux` | Build for Linux amd64 |
| `make build-windows` | Build for Windows amd64 |
| `make build-all` | Build for all platforms (Linux, Windows, macOS) |
| `make tui` | Build the terminal dashboard (`bin/discord-proxy-tui`) |
| `make test` | Run all tests with coverage (race detector if GCC available) |
| `make lint` | Run golangci-lint (required; fails if not installed) |
| `make run` | Run the proxy server |
| `make dev` | Run with hot reload via Air (if installed) |
| `make release` | Build and publish via GoReleaser |
| `make clean` | Remove build artifacts |
| `make e2e` | Run end-to-end tests (requires GCC for race detector) |
| `make capture` | Record raw Discord IPC frames (`CLIENT_ID=... [DURATION=60s] [OUT=capture.jsonl]`) |
| `make discover` | Browse the LAN for running proxies (`[TIMEOUT=5s] [JSON=1]`) |
| `make tray` | Build the Linux system-tray indicator (`bin/discord-proxy-tray`) |

## Release process

Releases are automated via GitHub Actions using semantic-release:

1. Push to `main` triggers the release workflow.
2. The `version` job runs semantic-release to determine the version bump (based on conventional commit messages in squash-merged PRs), creates the Git tag and publishes the GitHub Release with the generated notes.
3. If a new release is detected, the `goreleaser` job cross-compiles the proxy binaries for five targets (Linux amd64/arm64, Windows amd64, macOS amd64/arm64) and uploads one `.zip` per target. The Linux tray is built separately and uploaded as `discord-proxy-rpc-tray_<version>_linux_<arch>.zip`, because its platform set differs from the proxy builds.
4. Each proxy zip holds `discord-proxy`, `discover`, `config.example.yaml`, `LICENSE` and `README.md`; `checksums.txt` carries the SHA-256 of every archive.
5. GoReleaser runs with `release.mode: append`, so the assets attach to the release semantic-release already published instead of failing on an existing release.

## Docker build and run

The image runs as a non-root user. To reach Discord's IPC socket it must run as **your desktop-session user** and have that session's runtime directory mounted, otherwise the proxy serves HTTP but can never connect to Discord.

Build (the `APP_UID`/`APP_GID` build args bake your host user into the image):

```bash
docker build --build-arg APP_UID=$(id -u) --build-arg APP_GID=$(id -g) -t discord-proxy .
```

Run:

```bash
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e XDG_RUNTIME_DIR=/run/user/$(id -u) \
  -v "/run/user/$(id -u):/run/user/$(id -u):ro" \
  -v "$PWD/config.yaml:/etc/discord-proxy-rpc/config.yaml:ro" \
  -p 8765:8765 \
  discord-proxy
```

- `--user` matches the container process to the host user that owns Discord's socket (it overrides the image default); the `APP_UID`/`APP_GID` build args are an alternative if you prefer baking it in.
- The runtime directory (`$XDG_RUNTIME_DIR`, usually `/run/user/<uid>`) is mounted read-only — Discord creates `discord-ipc-0` … `discord-ipc-9` there and only the owning UID may connect.
- `config.yaml` is mounted read-only at `/etc/discord-proxy-rpc/`, one of the built-in config search paths. Alternatively pass `-e PROXY_DISCORD_CLIENT_ID=<id>`.
- If you skip the runtime-directory mount, the container can still serve the dashboard but reports `disconnected` for IPC.

A plain `docker build -t discord-proxy .` builds with the image default user; pass the build args when you want the image user to match your host session.

Multi-stage build: `golang:1.26-alpine` compiles a static binary, `alpine:3.20` runs it as the non-root `appuser`.

## Contributing

1. Fork the repository.
2. Create a feature branch from `dev`: `git checkout -b feature/my-change dev`.
3. Make your changes following the existing code style.
4. Add or update tests for any new functionality.
5. Run `make test` and `make lint` to verify.
6. Commit with a [conventional commit](https://www.conventionalcommits.org/) message (e.g., `feat: add ...`, `fix: resolve ...`, `docs: update ...`).
7. Push and open a Pull Request against `dev`.

All contributions are welcome -- bug reports, feature requests, documentation improvements, and code.

Pull requests run the GitHub Actions CI (lint, unit tests with `-race`, build and e2e); a PR may merge only with green CI.
