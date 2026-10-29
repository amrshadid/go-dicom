package cli

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNetworkCommandsRefuseTLSOptionsWithoutTLS: every network command takes
// the TLS options, and one given without -tls is an error. A command that
// accepted -tls-ca and then connected in the clear would be the failure TLS is
// there to prevent.
func TestNetworkCommandsRefuseTLSOptionsWithoutTLS(t *testing.T) {
	binary := buildCLI(t)
	for _, command := range []string{
		"echoscu", "storescu", "findscu", "movescu", "getscu", "commitscu",
		"echoscp", "storescp", "qrscp",
	} {
		t.Run(command, func(t *testing.T) {
			out, err := exec.Command(binary, command, "-tls-ca", "ca.crt", "127.0.0.1:1").CombinedOutput()
			if err == nil || !strings.Contains(string(out), "without -tls") {
				t.Fatalf("exit %v, output:\n%s", err, out)
			}
		})
	}
	for _, command := range []string{"echoscp", "storescp", "qrscp"} {
		t.Run(command+" without a certificate", func(t *testing.T) {
			out, err := exec.Command(binary, command, "-tls").CombinedOutput()
			if err == nil || !strings.Contains(string(out), "-tls-cert and -tls-key") {
				t.Fatalf("exit %v, output:\n%s", err, out)
			}
		})
	}
}

// TestEchoOverMutualTLS runs echoscp and echoscu with -tls, each presenting a
// certificate the other verifies against the same CA, and checks the echo
// succeeds; then that a plain echoscu is refused by the same server.
func TestEchoOverMutualTLS(t *testing.T) {
	binary := buildCLI(t)
	ca, cert, key := writeNodePKI(t)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()

	server := exec.Command(binary, "echoscp", "-port", port,
		"-tls", "-tls-cert", cert, "-tls-key", key, "-tls-ca", ca)
	var serverOut bytes.Buffer
	server.Stdout, server.Stderr = &serverOut, &serverOut
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Process.Kill(); _ = server.Wait() }()

	address := "127.0.0.1:" + port
	deadline := time.Now().Add(10 * time.Second)
	for {
		if conn, err := net.Dial("tcp", address); err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("echoscp never listened:\n%s", serverOut.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	out, err := exec.Command(binary, "echoscu", "-aec", "ECHOSCP",
		"-tls", "-tls-ca", ca, "-tls-cert", cert, "-tls-key", key, address).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "C-ECHO response: Success") {
		t.Fatalf("echo over TLS: exit %v, output:\n%s", err, out)
	}

	out, err = exec.Command(binary, "echoscu", "-aec", "ECHOSCP", "-timeout", "5", address).CombinedOutput()
	if err == nil || strings.Contains(string(out), "C-ECHO response: Success") {
		t.Fatalf("a plain echoscu was answered by a TLS-only echoscp:\n%s", out)
	}
}

// writeNodePKI writes a CA and one certificate for 127.0.0.1 that may act as
// either side, the way a DICOM node that both calls and is called holds one.
func writeNodePKI(t *testing.T) (ca, cert, key string) {
	t.Helper()
	dir := t.TempDir()
	generate := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	write := func(name, kind string, der []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	caKey := generate()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cli test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	nodeKey := generate()
	nodeDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "node"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
	}, caCert, &nodeKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	return write("ca.crt", "CERTIFICATE", caDER), write("node.crt", "CERTIFICATE", nodeDER),
		write("node.key", "EC PRIVATE KEY", keyDER)
}
