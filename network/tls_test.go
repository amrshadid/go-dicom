package network

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testPKI is a private CA with a server certificate for 127.0.0.1 and a client
// certificate, written as PEM files the way a deployment would hold them.
type testPKI struct {
	caFile, serverCert, serverKey, clientCert, clientKey string
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	dir := t.TempDir()

	caKey := newKey(t)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "go-dicom test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	issue := func(serial int64, name string, usage x509.ExtKeyUsage, ips []net.IP) (string, string) {
		key := newKey(t)
		template := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: name},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{usage},
			IPAddresses:  ips,
		}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return writePEM(t, dir, name+".crt", "CERTIFICATE", der), writeKey(t, dir, name+".key", key)
	}

	pki := testPKI{caFile: writePEM(t, dir, "ca.crt", "CERTIFICATE", caDER)}
	pki.serverCert, pki.serverKey = issue(2, "server", x509.ExtKeyUsageServerAuth, []net.IP{net.IPv4(127, 0, 0, 1)})
	pki.clientCert, pki.clientKey = issue(3, "client", x509.ExtKeyUsageClientAuth, nil)
	return pki
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func writePEM(t *testing.T, dir, name, kind string, der []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeKey(t *testing.T, dir, name string, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return writePEM(t, dir, name, "EC PRIVATE KEY", der)
}

// tlsHandshake listens with the server configuration, dials with the client
// one, and returns the error each side saw once the handshake has finished.
func tlsHandshake(t *testing.T, server, client *TLSConfig) (serverErr, clientErr error) {
	t.Helper()
	ln, err := ListenTLS("127.0.0.1:0", server)
	if err != nil {
		t.Fatalf("ListenTLS: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan error, 1)
	go func() {
		transport, err := ln.Accept(ctx)
		if err != nil {
			accepted <- err
			return
		}
		defer transport.Close()
		// The handshake runs on first use; a read is what makes the server
		// decide whether to accept the client's certificate.
		accepted <- transport.conn.(interface{ HandshakeContext(context.Context) error }).HandshakeContext(ctx)
	}()

	transport, clientErr := DialTLS(ctx, ln.Addr().String(), 5*time.Second, client)
	if clientErr == nil {
		// TLS 1.3 reports a rejected client certificate after the client's
		// handshake has returned, on the next read.
		_ = transport.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		// EOF is the server closing after a handshake it accepted; a refused
		// certificate arrives as an alert instead.
		if _, err := transport.conn.Read(make([]byte, 1)); err != nil && !isTimeout(err) && !errors.Is(err, io.EOF) {
			clientErr = err
		}
		transport.Close()
	}
	return <-accepted, clientErr
}

func isTimeout(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// TestTLSServerWithCAFileRequiresAClientCertificate: CAFile is documented as
// the CA that client certificates are verified against, and it was never
// read, so a server configured for mutual TLS accepted a client with no
// certificate at all (#180).
func TestTLSServerWithCAFileRequiresAClientCertificate(t *testing.T) {
	pki := newTestPKI(t)
	server := &TLSConfig{CertFile: pki.serverCert, KeyFile: pki.serverKey, CAFile: pki.caFile}

	// This client trusts the server through crypto/tls directly, so whether
	// the handshake completes depends on the server alone.
	trusting := &TLSConfig{Config: &tls.Config{RootCAs: pool(t, pki.caFile), MinVersion: tls.VersionTLS12}}
	if serverErr, clientErr := tlsHandshake(t, server, trusting); serverErr == nil && clientErr == nil {
		t.Error("a client with no certificate completed a handshake with a server that has a CAFile")
	}

	serverErr, clientErr := tlsHandshake(t, server, &TLSConfig{

		CAFile: pki.caFile, CertFile: pki.clientCert, KeyFile: pki.clientKey,
	})
	if serverErr != nil || clientErr != nil {
		t.Errorf("a client with a certificate from the CA was refused: server %v, client %v", serverErr, clientErr)
	}
}

// TestTLSClientWithCAFileTrustsThatCA: a client given CAFile verifies the
// server against it. Without it read, a server whose certificate comes from a
// private CA, the usual case for a PACS, failed verification (#180).
func TestTLSClientWithCAFileTrustsThatCA(t *testing.T) {
	pki := newTestPKI(t)
	server := &TLSConfig{CertFile: pki.serverCert, KeyFile: pki.serverKey}

	if _, clientErr := tlsHandshake(t, server, &TLSConfig{CAFile: pki.caFile}); clientErr != nil {
		t.Errorf("the client did not trust the CA it was given: %v", clientErr)
	}
	// And trusts nothing else in its place: without CAFile the private CA is
	// unknown, so the handshake must fail rather than skip verification.
	if _, clientErr := tlsHandshake(t, server, &TLSConfig{}); clientErr == nil {
		t.Error("a client with no CAFile accepted a certificate from a private CA")
	}
}

// TestTLSConfigRefusesWhatItCannotUse: a certificate without its key, a key
// without its certificate, or a CA file that cannot be read was ignored, and a
// client then connected with no certificate, or with no CA, as if none had
// been configured.
func TestTLSConfigRefusesWhatItCannotUse(t *testing.T) {
	pki := newTestPKI(t)
	for name, cfg := range map[string]*TLSConfig{
		"certificate only": {CertFile: pki.clientCert},
		"key only":         {KeyFile: pki.clientKey},
		"unreadable CA":    {CAFile: filepath.Join(t.TempDir(), "missing.crt")},
		"CA with no PEM":   {CAFile: pki.clientKey},
	} {
		t.Run(name, func(t *testing.T) {
			for _, server := range []bool{false, true} {
				if _, err := cfg.buildTLSConfig(server); err == nil {
					t.Errorf("server=%v: the configuration was accepted", server)
				}
			}
		})
	}
}

func pool(t *testing.T, caFile string) *x509.CertPool {
	t.Helper()
	data, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(data) {
		t.Fatal("no certificate in the CA file")
	}
	return p
}
