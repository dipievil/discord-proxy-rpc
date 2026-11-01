package tui

import (
	"net"
	"os"
	"strings"
)

const (
	defaultHost     = "127.0.0.1"
	defaultPort     = "8765"
	defaultWSPath   = "/ws"
	defaultWSScheme = "ws"
)

// ResolveAddr picks the proxy address with flag > env > default precedence:
// the -addr flag, else $PROXY_HOST:$PROXY_PORT (falling back to the server's
// own $PROXY_SERVER_PORT), else 127.0.0.1:8765.
func ResolveAddr(addr string) string {
	if addr != "" {
		return addr
	}
	host := os.Getenv("PROXY_HOST")
	if host == "" {
		host = defaultHost
	}
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = os.Getenv("PROXY_SERVER_PORT")
	}
	if port == "" {
		port = defaultPort
	}
	return net.JoinHostPort(host, port)
}

// ResolveToken picks the bearer token with the same precedence as the proxy
// server: the -token flag, else $PROXY_TOKEN (legacy, takes precedence when
// set), else $PROXY_AUTH_TOKEN.
func ResolveToken(token string) string {
	if token != "" {
		return token
	}
	if legacy := os.Getenv("PROXY_TOKEN"); legacy != "" {
		return legacy
	}
	return os.Getenv("PROXY_AUTH_TOKEN")
}

// ResolveWSPath picks the WebSocket path: the -ws-path flag, else
// $PROXY_SERVER_WS_PATH, else /ws.
func ResolveWSPath(path string) string {
	if path != "" {
		return path
	}
	if p := os.Getenv("PROXY_SERVER_WS_PATH"); p != "" {
		return p
	}
	return defaultWSPath
}

// WSURL turns a resolved address into a WebSocket URL. A value that already
// carries a ws:// or wss:// scheme is returned untouched.
func WSURL(addr, path string) string {
	if strings.HasPrefix(addr, "ws://") || strings.HasPrefix(addr, "wss://") {
		return addr
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return defaultWSScheme + "://" + addr + path
}
