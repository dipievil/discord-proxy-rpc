package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Load parses the configured certificate material into a listener config. It is
// the single parser for server.tls: validation and the listener both use it, so
// a file that validates is the same file that serves.
func (t TLSConfig) Load() (*tls.Config, error) {
	if !t.Enabled {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("server.tls: loading certificate pair (cert_file %q, key_file %q): %w", t.CertFile, t.KeyFile, err)
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}

	if t.ClientCAFile == "" {
		return tlsCfg, nil
	}

	caPEM, err := os.ReadFile(t.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("server.tls.client_ca_file %q: %w", t.ClientCAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("server.tls.client_ca_file %q contains no valid PEM certificates", t.ClientCAFile)
	}

	tlsCfg.ClientCAs = pool
	tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert

	return tlsCfg, nil
}
