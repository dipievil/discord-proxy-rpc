package server

import (
	"crypto/tls"
	"net"
	"net/http"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
)

// NewTLSConfig builds the listener TLS configuration from server.tls. It
// returns nil when TLS is disabled. A configured client CA switches the
// listener to mutual TLS: clients must present a certificate chained to it.
// Certificate material is parsed by config.TLSConfig.Load, the same parser
// config validation uses, so a file that validates is the file that serves.
func NewTLSConfig(cfg config.TLSConfig) (*tls.Config, error) {
	return cfg.Load()
}

// Serve runs srv on ln, over TLS when tlsCfg is non-nil. Certificate loading
// happens once in NewTLSConfig, so a bad PEM fails before the listener starts.
func Serve(srv *http.Server, ln net.Listener, tlsCfg *tls.Config) error {
	if tlsCfg == nil {
		return srv.Serve(ln)
	}
	srv.TLSConfig = tlsCfg
	return srv.ServeTLS(ln, "", "")
}

// WithRequireClientCert makes the WebSocket handler reject upgrades whose TLS
// connection carries no verified client certificate. The TLS listener already
// enforces this in mTLS mode; the handler check is defence in depth against a
// misconfigured listener.
func WithRequireClientCert() ServerOption {
	return func(s *Server) {
		s.wsHandler = s.wsHandler.WithRequireClientCert()
	}
}
