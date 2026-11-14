package tray

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func TestNewHTTPClientDefault(t *testing.T) {
	client, err := NewHTTPClient("", "", "")
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if client.Timeout != requestTimeout {
		t.Errorf("Timeout = %s, want %s", client.Timeout, requestTimeout)
	}
	if client.Transport != nil {
		t.Error("default client got a custom transport; the system trust store should be used")
	}
}

func TestNewHTTPClientRequiresCertificatePair(t *testing.T) {
	if _, err := NewHTTPClient("", "client.pem", ""); err == nil {
		t.Error("NewHTTPClient accepted -cert without -key")
	}
	if _, err := NewHTTPClient("", "", "client-key.pem"); err == nil {
		t.Error("NewHTTPClient accepted -key without -cert")
	}
}

func TestNewHTTPClientRejectsBadCAFile(t *testing.T) {
	if _, err := NewHTTPClient(filepath.Join(t.TempDir(), "missing.pem"), "", ""); err == nil {
		t.Error("NewHTTPClient accepted a missing CA file")
	}
	junk := writeTestFile(t, "junk.pem", []byte("not a certificate"))
	if _, err := NewHTTPClient(junk, "", ""); err == nil {
		t.Error("NewHTTPClient accepted a CA file without a certificate")
	}
}

func TestNewHTTPClientTrustsPrivateCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"connected"}`))
	}))
	defer server.Close()

	caFile := writeTestFile(t, "ca.pem", pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	}))
	httpClient, err := NewHTTPClient(caFile, "", "")
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	client, err := NewClient(server.URL, "", httpClient)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	state, err := client.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != StateConnected {
		t.Errorf("state = %q, want connected", state)
	}

	// The same server is unreachable through the system roots, which is the
	// silent failure -cacert exists to fix.
	untrusted, err := NewClient(server.URL, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if state, err := untrusted.State(context.Background()); err == nil || state != StateUnreachable {
		t.Errorf("untrusted state = %q, err = %v; want unreachable with an error", state, err)
	}
}

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tray test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing CA certificate: %v", err)
	}
	return testCA{
		cert: cert,
		key:  key,
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (ca testCA) issue(t *testing.T, commonName string, usage x509.ExtKeyUsage) (tls.Certificate, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating %s key: %v", commonName, err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generating %s serial: %v", commonName, err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     []string{commonName},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("creating %s certificate: %v", commonName, err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling %s key: %v", commonName, err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("loading %s key pair: %v", commonName, err)
	}
	return pair, certPEM, keyPEM
}

func TestNewHTTPClientPresentsClientCertificate(t *testing.T) {
	ca := newTestCA(t)
	serverPair, _, _ := ca.issue(t, "proxy.test", x509.ExtKeyUsageServerAuth)
	_, clientCertPEM, clientKeyPEM := ca.issue(t, "tray.test", x509.ExtKeyUsageClientAuth)

	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(ca.pem) {
		t.Fatal("building the client CA pool")
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"connected"}`))
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	caFile := writeTestFile(t, "ca.pem", ca.pem)
	certFile := writeTestFile(t, "client.pem", clientCertPEM)
	keyFile := writeTestFile(t, "client-key.pem", clientKeyPEM)

	httpClient, err := NewHTTPClient(caFile, certFile, keyFile)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	client, err := NewClient(server.URL, "", httpClient)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	state, err := client.State(context.Background())
	if err != nil {
		t.Fatalf("State with a client certificate: %v", err)
	}
	if state != StateConnected {
		t.Errorf("state = %q, want connected", state)
	}

	caOnly, err := NewHTTPClient(caFile, "", "")
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	withoutCert, err := NewClient(server.URL, "", caOnly)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if state, err := withoutCert.State(context.Background()); err == nil || state != StateUnreachable {
		t.Errorf("state without a client certificate = %q, err = %v; want unreachable with an error", state, err)
	}
}
