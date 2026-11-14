package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/testcert"
)

type tlsTestMaterial struct {
	cfg             config.TLSConfig
	serverOnly      config.TLSConfig
	roots           *x509.CertPool
	clientCert      tls.Certificate
	wrongClientCert tls.Certificate
}

// newTLSTestMaterial issues a server certificate and a client certificate from
// one CA, plus a client certificate from a second CA for the wrong-CA case.
func newTLSTestMaterial(t *testing.T) tlsTestMaterial {
	t.Helper()

	dir := t.TempDir()
	ca, err := testcert.NewAuthority("proxy-test-ca")
	if err != nil {
		t.Fatalf("new CA: %v", err)
	}
	serverCertPEM, serverKeyPEM, err := ca.Issue("proxy",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("issue server certificate: %v", err)
	}
	serverCertFile, serverKeyFile, err := testcert.WritePair(dir, "server", serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("write server pair: %v", err)
	}
	caFile := filepath.Join(dir, "clients-ca.crt")
	if err := os.WriteFile(caFile, ca.CertPEM(), 0o600); err != nil {
		t.Fatalf("write client CA: %v", err)
	}

	clientCertPEM, clientKeyPEM, err := ca.Issue("authorized-client",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil, nil)
	if err != nil {
		t.Fatalf("issue client certificate: %v", err)
	}
	clientCert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("load client pair: %v", err)
	}

	otherCA, err := testcert.NewAuthority("untrusted-ca")
	if err != nil {
		t.Fatalf("new untrusted CA: %v", err)
	}
	wrongCertPEM, wrongKeyPEM, err := otherCA.Issue("untrusted-client",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil, nil)
	if err != nil {
		t.Fatalf("issue untrusted client certificate: %v", err)
	}
	wrongClientCert, err := tls.X509KeyPair(wrongCertPEM, wrongKeyPEM)
	if err != nil {
		t.Fatalf("load untrusted pair: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca.CertPEM())

	cfg := config.TLSConfig{
		Enabled:      true,
		CertFile:     serverCertFile,
		KeyFile:      serverKeyFile,
		ClientCAFile: caFile,
	}
	serverOnly := cfg
	serverOnly.ClientCAFile = ""

	return tlsTestMaterial{
		cfg:             cfg,
		serverOnly:      serverOnly,
		roots:           roots,
		clientCert:      clientCert,
		wrongClientCert: wrongClientCert,
	}
}

func TestNewTLSConfigDisabled(t *testing.T) {
	tlsCfg, err := NewTLSConfig(config.TLSConfig{})
	if err != nil {
		t.Fatalf("NewTLSConfig() = %v, want nil error", err)
	}
	if tlsCfg != nil {
		t.Fatalf("NewTLSConfig() = %+v, want nil", tlsCfg)
	}
}

func TestNewTLSConfigServerOnly(t *testing.T) {
	material := newTLSTestMaterial(t)

	tlsCfg, err := NewTLSConfig(material.serverOnly)
	if err != nil {
		t.Fatalf("NewTLSConfig(): %v", err)
	}
	if tlsCfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", tlsCfg.MinVersion, tls.VersionTLS12)
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("Certificates = %d, want 1", len(tlsCfg.Certificates))
	}
	if tlsCfg.ClientAuth != tls.NoClientCert {
		t.Errorf("ClientAuth = %v, want NoClientCert", tlsCfg.ClientAuth)
	}
	if tlsCfg.ClientCAs != nil {
		t.Error("ClientCAs = non-nil, want nil for server-only TLS")
	}
}

func TestNewTLSConfigMTLS(t *testing.T) {
	material := newTLSTestMaterial(t)

	tlsCfg, err := NewTLSConfig(material.cfg)
	if err != nil {
		t.Fatalf("NewTLSConfig(): %v", err)
	}
	if tlsCfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", tlsCfg.ClientAuth)
	}
	if tlsCfg.ClientCAs == nil {
		t.Fatal("ClientCAs = nil, want the configured client CA pool")
	}
}

func TestNewTLSConfigInvalidMaterial(t *testing.T) {
	material := newTLSTestMaterial(t)
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	cases := []struct {
		name    string
		cfg     config.TLSConfig
		wantErr string
	}{
		{
			name:    "missing server certificate",
			cfg:     config.TLSConfig{Enabled: true, CertFile: filepath.Join(dir, "absent.crt"), KeyFile: material.cfg.KeyFile},
			wantErr: "absent.crt",
		},
		{
			name:    "invalid client CA",
			cfg:     config.TLSConfig{Enabled: true, CertFile: material.cfg.CertFile, KeyFile: material.cfg.KeyFile, ClientCAFile: garbage},
			wantErr: "no valid PEM certificates",
		},
		{
			name:    "missing client CA",
			cfg:     config.TLSConfig{Enabled: true, CertFile: material.cfg.CertFile, KeyFile: material.cfg.KeyFile, ClientCAFile: filepath.Join(dir, "absent-ca.crt")},
			wantErr: "absent-ca.crt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTLSConfig(tc.cfg)
			if err == nil {
				t.Fatalf("NewTLSConfig() = nil error, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// serveTLSHandler serves handler on a TLS listener through the production
// Serve path and returns the dialable address.
func serveTLSHandler(t *testing.T, handler http.Handler, tlsCfg *tls.Config) string {
	t.Helper()

	httpSrv := &http.Server{
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- Serve(httpSrv, ln, tlsCfg) }()

	t.Cleanup(func() {
		httpSrv.Close()
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Serve: %v", err)
		}
	})

	return ln.Addr().String()
}

// startTLSServer serves a real Server on a TLS listener built by NewTLSConfig.
func startTLSServer(t *testing.T, tlsCfg *tls.Config, opts ...ServerOption) (*Hub, string) {
	t.Helper()

	hub, cancel := newTestHub(t)
	srv := NewServer(hub, zap.NewNop(), opts...)
	t.Cleanup(func() {
		srv.Close()
		cancel()
	})

	return hub, serveTLSHandler(t, srv, tlsCfg)
}

func tlsHTTPClient(roots *x509.CertPool, certs ...tls.Certificate) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS12,
				RootCAs:      roots,
				Certificates: certs,
			},
		},
	}
}

func getStatus(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestServeTLSRoutesOnSameListener(t *testing.T) {
	material := newTLSTestMaterial(t)
	tlsCfg, err := NewTLSConfig(material.serverOnly)
	if err != nil {
		t.Fatalf("NewTLSConfig(): %v", err)
	}

	hub, cancel := newTestHub(t)
	defer cancel()
	srv := NewServer(hub, zap.NewNop(),
		WithGetIPCState(func() string { return string(StateConnected) }),
		WithMetrics(NewMetrics(hub, func() string { return string(StateConnected) }, nil)))
	t.Cleanup(srv.Close)

	addr := serveTLSHandler(t, srv, tlsCfg)
	base := "https://" + addr
	client := tlsHTTPClient(material.roots)

	for _, path := range []string{"/health", "/healthz", "/readyz", "/api/presence", "/", "/metrics"} {
		if code := getStatus(t, client, base+path); code != http.StatusOK {
			t.Errorf("GET %s over TLS = %d, want 200", path, code)
		}
	}

	// A plaintext request to the TLS port never reaches a handler; Go's TLS
	// server answers it with a bare 400 "client sent an HTTP request to an
	// HTTPS server", so assert on the status rather than a transport error.
	plain := &http.Client{Timeout: 5 * time.Second}
	if code := getStatus(t, plain, "http://"+addr+"/health"); code != http.StatusBadRequest {
		t.Errorf("plain HTTP request to the TLS listener = %d, want 400", code)
	}
}

func TestServeMTLSRequiresClientCertificate(t *testing.T) {
	material := newTLSTestMaterial(t)
	tlsCfg, err := NewTLSConfig(material.cfg)
	if err != nil {
		t.Fatalf("NewTLSConfig(): %v", err)
	}

	_, addr := startTLSServer(t, tlsCfg)
	base := "https://" + addr

	t.Run("valid client certificate", func(t *testing.T) {
		client := tlsHTTPClient(material.roots, material.clientCert)
		if code := getStatus(t, client, base+"/health"); code != http.StatusOK {
			t.Errorf("GET /health with valid client cert = %d, want 200", code)
		}
	})

	t.Run("no client certificate", func(t *testing.T) {
		client := tlsHTTPClient(material.roots)
		if _, err := client.Get(base + "/health"); err == nil {
			t.Error("GET /health without a client certificate succeeded, want a TLS handshake error")
		}
	})

	t.Run("client certificate from another CA", func(t *testing.T) {
		client := tlsHTTPClient(material.roots, material.wrongClientCert)
		if _, err := client.Get(base + "/health"); err == nil {
			t.Error("GET /health with an untrusted client certificate succeeded, want a TLS handshake error")
		}
	})
}

func TestWSMTLSClientCertificatePath(t *testing.T) {
	material := newTLSTestMaterial(t)
	tlsCfg, err := NewTLSConfig(material.cfg)
	if err != nil {
		t.Fatalf("NewTLSConfig(): %v", err)
	}

	hub, addr := startTLSServer(t, tlsCfg)
	url := "wss://" + addr + "/ws"

	dial := func(certs ...tls.Certificate) (*websocket.Conn, *http.Response, error) {
		dialer := websocket.Dialer{
			HandshakeTimeout: 5 * time.Second,
			TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: material.roots, Certificates: certs},
		}
		return dialer.Dial(url, nil)
	}

	t.Run("valid client certificate upgrades", func(t *testing.T) {
		conn, _, err := dial(material.clientCert)
		if err != nil {
			t.Fatalf("dial with valid client cert: %v", err)
		}
		defer conn.Close()
		waitForClientCount(t, hub, 1)
	})

	t.Run("no client certificate is rejected", func(t *testing.T) {
		conn, resp, err := dial()
		if conn != nil {
			conn.Close()
		}
		if err == nil {
			t.Fatal("dial without a client certificate succeeded, want rejection")
		}
		if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
			t.Error("upgrade returned 101 without a client certificate")
		}
	})

	t.Run("client certificate from another CA is rejected", func(t *testing.T) {
		conn, resp, err := dial(material.wrongClientCert)
		if conn != nil {
			conn.Close()
		}
		if err == nil {
			t.Fatal("dial with an untrusted client certificate succeeded, want rejection")
		}
		if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
			t.Error("upgrade returned 101 with an untrusted client certificate")
		}
	})
}

// TestWSHandlerRequiresClientCertificate covers the handler's own check: even
// on a plaintext listener, a handler configured for mTLS refuses the upgrade.
func TestWSHandlerRequiresClientCertificate(t *testing.T) {
	hub, cancel := newTestHub(t)
	defer cancel()
	srv := NewServer(hub, zap.NewNop(), WithRequireClientCert())
	ts := httptest.NewServer(srv)
	defer ts.Close()

	conn, resp, err := websocket.DefaultDialer.Dial("ws"+ts.URL[4:]+"/ws", nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("upgrade succeeded without a TLS client certificate, want 401")
	}
	if resp == nil {
		t.Fatalf("dial error %v carries no HTTP response, want 401", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// TestWSHandlerRejectsUnverifiedClientCertificate pins the difference between a
// presented and a verified client certificate. A listener configured with
// tls.RequestClientCert puts whatever the client sent into r.TLS.PeerCertificates
// without validating it, so the handler must inspect VerifiedChains — the field
// that is only populated after the chain is checked against ClientCAs — and
// still answer 401.
func TestWSHandlerRejectsUnverifiedClientCertificate(t *testing.T) {
	material := newTLSTestMaterial(t)

	serverCert, err := tls.LoadX509KeyPair(material.serverOnly.CertFile, material.serverOnly.KeyFile)
	if err != nil {
		t.Fatalf("load server pair: %v", err)
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequestClientCert,
	}

	_, addr := startTLSServer(t, tlsCfg, WithRequireClientCert())

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			RootCAs:      material.roots,
			Certificates: []tls.Certificate{material.wrongClientCert},
		},
	}

	conn, resp, err := dialer.Dial("wss://"+addr+"/ws", nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("upgrade succeeded with an unverified client certificate, want 401")
	}
	if resp == nil {
		t.Fatalf("dial error %v carries no HTTP response, want 401", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}
