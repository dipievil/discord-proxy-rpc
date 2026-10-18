# ---- Builder stage ----
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w" \
    -o /out/discord-proxy ./cmd/proxy

# ---- Runtime stage ----
FROM alpine:3.20

# Discord's IPC socket is owned by the host desktop-session user, so the image
# user must match it (see README "Option 2: Docker"). Override at build time
# with --build-arg APP_UID=$(id -u) --build-arg APP_GID=$(id -g), or at run
# time with --user "$(id -u):$(id -g)".
ARG APP_UID=10001
ARG APP_GID=10001

RUN apk add --no-cache ca-certificates \
    && addgroup -g "${APP_GID}" appuser \
    && adduser -D -H -u "${APP_UID}" -G appuser -s /sbin/nologin appuser

COPY --from=builder /out/discord-proxy /usr/local/bin/discord-proxy

USER appuser

EXPOSE 8765

ENTRYPOINT ["/usr/local/bin/discord-proxy"]
