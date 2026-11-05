package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
)

// NewTLSConfig builds the listener TLS configuration from server.tls. It
// returns nil when TLS is disabled. A configured client CA switches the
// listener to mutual TLS: clients must present a certificate chained to it.
func NewTLSConfig(cfg config.TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("server.tls: loading certificate pair (cert_file %q, key_file %q): %w", cfg.CertFile, cfg.KeyFile, err)
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}

	if cfg.ClientCAFile == "" {
		return tlsCfg, nil
	}

	caPEM, err := os.ReadFile(cfg.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("server.tls.client_ca_file %q: %w", cfg.ClientCAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("server.tls.client_ca_file %q contains no valid PEM certificates", cfg.ClientCAFile)
	}

	tlsCfg.ClientCAs = pool
	tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert

	return tlsCfg, nil
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
