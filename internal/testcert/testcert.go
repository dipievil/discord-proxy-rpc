// Package testcert generates throwaway X.509 material for TLS and mTLS tests.
// It has no production callers; it lives in a normal package so that several
// test packages can share one certificate factory instead of duplicating it.
package testcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Authority is an in-memory certificate authority that issues leaf
// certificates.
type Authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewAuthority creates a self-signed CA valid for one day.
func NewAuthority(name string) (*Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("testcert: generating CA key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serialNumber(),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("testcert: creating CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("testcert: parsing CA certificate: %w", err)
	}

	return &Authority{cert: cert, key: key}, nil
}

// CertPEM returns the CA certificate encoded as PEM.
func (a *Authority) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw})
}

// Issue creates a leaf certificate signed by the CA. usages must list the
// extended key usages the certificate is valid for (server and/or client auth).
func (a *Authority) Issue(name string, usages []x509.ExtKeyUsage, dnsNames []string, ips []net.IP) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("testcert: generating leaf key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber(),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usages,
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, nil, fmt.Errorf("testcert: creating leaf certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("testcert: marshalling leaf key: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// WritePair writes a PEM certificate and key into dir and returns their paths.
func WritePair(dir, prefix string, certPEM, keyPEM []byte) (certFile, keyFile string, err error) {
	certFile = filepath.Join(dir, prefix+".crt")
	keyFile = filepath.Join(dir, prefix+".key")

	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("testcert: writing certificate %q: %w", certFile, err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("testcert: writing key %q: %w", keyFile, err)
	}

	return certFile, keyFile, nil
}

func serialNumber() *big.Int {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}
