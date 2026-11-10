package tray

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
)

// NewHTTPClient builds the HTTP client the state poller uses. Empty file paths
// keep the system trust store and no client certificate, which is what a plain
// http:// proxy needs. caFile replaces the system roots with a private CA, and
// certFile/keyFile present a client certificate for a proxy that requires mTLS.
func NewHTTPClient(caFile, certFile, keyFile string) (*http.Client, error) {
	if (certFile == "") != (keyFile == "") {
		return nil, errors.New("client certificate needs both -cert and -key")
	}
	if caFile == "" && certFile == "" {
		return &http.Client{Timeout: requestTimeout}, nil
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificate found in CA file %s", caFile)
		}
		tlsConfig.RootCAs = pool
	}
	if certFile != "" {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
	}

	return &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: tlsConfig,
		},
		Timeout: requestTimeout,
	}, nil
}
