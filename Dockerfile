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

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 -s /sbin/nologin appuser

COPY --from=builder /out/discord-proxy /usr/local/bin/discord-proxy

USER appuser

EXPOSE 8765

ENTRYPOINT ["/usr/local/bin/discord-proxy"]
