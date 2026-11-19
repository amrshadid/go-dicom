package network

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// tlsFailures starts a TLS server requiring client certificates from pki's
// CA and returns its address and the EVTTLSHandshakeFailed events it emits.
func tlsFailures(t *testing.T, pki testPKI) (string, <-chan *Event) {
	addr, failed, _ := tlsServerFailures(t, pki)
	return addr, failed
}

// tlsServerFailures is tlsFailures, with the server.
func tlsServerFailures(t *testing.T, pki testPKI) (string, <-chan *Event, *Server) {
	t.Helper()
	logTo(t) // the failures are logged too; not onto the test's output
	serverTLS, _ := mutualTLS(pki)
	failed := make(chan *Event, 4)
	events := NewEventManager()
	events.On(EVTTLSHandshakeFailed, func(e *Event) { failed <- e })
	server, err := StartServer(context.Background(), SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", TLS: serverTLS, Events: events,
	}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server.Addr(), failed, server
}

// handshakeFailed waits for the server's EVTTLSHandshakeFailed and checks
// that it names the check, says who, and carries nothing of a certificate.
func handshakeFailed(t *testing.T, failed <-chan *Event, client net.Conn, want TLSCheck) {
	t.Helper()
	var e *Event
	select {
	case e = <-failed:
	case <-time.After(5 * time.Second):
		t.Fatalf("no EVTTLSHandshakeFailed; want one naming %s", want)
	}
	var failure *TLSHandshakeError
	if !errors.As(e.Error, &failure) || failure.Check != want {
		t.Fatalf("Error %v, want a *TLSHandshakeError naming %s", e.Error, want)
	}
	if e.RemoteAddr != client.LocalAddr().String() {
		t.Errorf("RemoteAddr %q, want the client's %q", e.RemoteAddr, client.LocalAddr())
	}
	if e.Role != "" || e.CallingAE != "" {
		t.Errorf("the event has role %q and calling AE %q; no association exists yet", e.Role, e.CallingAE)
	}
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	if errors.As(e.Error, &verify) || errors.As(e.Error, &unknown) {
		t.Error("the event's error wraps crypto/tls's, which carries the client's certificates")
	}
	// The names in the test certificates (newTestPKI).
	for _, name := range []string{"go-dicom test CA", "CN="} {
		if strings.Contains(e.Error.Error(), name) || strings.Contains(e.Description, name) {
			t.Errorf("the event names %q from a certificate: %q", name, e.Error)
		}
	}
	t.Logf("%s from %s: %v", EventTypeString(e.Type), e.RemoteAddr, e.Error)
}

// clientHandshake dials addr and runs a TLS handshake as a client with
// config, returning the connection whatever the handshake's outcome.
func clientHandshake(t *testing.T, addr string, config *tls.Config) net.Conn {
	t.Helper()
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	conn := tls.Client(raw, config)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_ = conn.Handshake()
	// A TLS 1.3 client finishes before the server has checked its
	// certificate; the server's verdict comes with the first read.
	_, _ = conn.Read(make([]byte, 1))
	return raw
}

// TestTLSHandshakeFailureIsAnEvent: a client a TLS server turns away is an
// event, EVTTLSHandshakeFailed, carrying the client's address and which
// check failed, and none of its certificate. It was only a log line: events
// saw the connection open and close, as for a port scan (#471).
func TestTLSHandshakeFailureIsAnEvent(t *testing.T) {
	pki := newTestPKI(t)

	t.Run("no client certificate", func(t *testing.T) {
		addr, failed := tlsFailures(t, pki)
		conn := clientHandshake(t, addr, &tls.Config{RootCAs: pool(t, pki.caFile), ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
		handshakeFailed(t, failed, conn, TLSCheckNoClientCertificate)
	})

	t.Run("certificate from another CA", func(t *testing.T) {
		addr, failed := tlsFailures(t, pki)
		other := newTestPKI(t)
		cert, err := tls.LoadX509KeyPair(other.clientCert, other.clientKey)
		if err != nil {
			t.Fatal(err)
		}
		conn := clientHandshake(t, addr, &tls.Config{
			RootCAs: pool(t, pki.caFile), ServerName: "127.0.0.1", Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12,
		})
		handshakeFailed(t, failed, conn, TLSCheckUnknownAuthority)
	})

	t.Run("plain TCP", func(t *testing.T) {
		addr, failed := tlsFailures(t, pki)
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		raw, _ := EncodePDU(associateRQ("SCANNER", "ARCHIVE"))
		_, _ = conn.Write(raw)
		handshakeFailed(t, failed, conn, TLSCheckNotTLS)
	})

	t.Run("a server stopping mid-handshake is not reported", func(t *testing.T) {
		addr, failed, server := tlsServerFailures(t, pki)
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		time.Sleep(100 * time.Millisecond) // the server is reading the handshake
		server.Stop()
		select {
		case e := <-failed:
			t.Errorf("a handshake cut by the server stopping was reported: %v", e.Error)
		case <-time.After(200 * time.Millisecond):
		}
	})

	t.Run("a good client is not reported", func(t *testing.T) {
		addr, failed := tlsFailures(t, pki)
		_, clientTLS := mutualTLS(pki)
		scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: addr, TLS: clientTLS})
		if err := scu.Associate(context.Background(), DefaultVerificationContexts()); err != nil {
			t.Fatal(err)
		}
		if err := scu.Echo(context.Background()); err != nil {
			t.Fatal(err)
		}
		_ = scu.Release(context.Background())
		// One that completes its handshake and leaves without a request
		// failed the read, not the handshake.
		cert, err := tls.LoadX509KeyPair(pki.clientCert, pki.clientKey)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			RootCAs: pool(t, pki.caFile), ServerName: "127.0.0.1", Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			t.Fatal(err)
		}
		// TLS 1.3 finishes the client first; the read gives the server its turn.
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, _ = conn.Read(make([]byte, 1))
		conn.Close()
		select {
		case e := <-failed:
			t.Errorf("a client that completed its handshake was reported: %v", e.Error)
		case <-time.After(200 * time.Millisecond):
		}
	})
}

// TestTLSHandshakeChecksAreNamed: each kind of handshake error is given the
// check it failed, and a verification error's text, which quotes the
// certificate, is not passed on.
func TestTLSHandshakeChecksAreNamed(t *testing.T) {
	leaky := &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}
	for _, c := range []struct {
		err  error
		want TLSCheck
	}{
		{tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, TLSCheckNotTLS},
		{leaky, TLSCheckUnknownAuthority},
		{&tls.CertificateVerificationError{Err: x509.CertificateInvalidError{Reason: x509.Expired, Detail: "current time is after 2020"}}, TLSCheckCertificateInvalid},
		{errors.New("tls: client didn't provide a certificate"), TLSCheckNoClientCertificate},
		{errors.New("remote error: tls: bad certificate"), TLSCheckPeerAlert},
		{errors.New("tls: client offered only unsupported versions: [301]"), TLSCheckProtocol},
		{errors.New("EOF"), TLSCheckConnection},
	} {
		got := classifyHandshake(NewCommunicationError("READ_PDU", "failed to read PDU header", c.err))
		if got.Check != c.want {
			t.Errorf("%v: %s, want %s", c.err, got.Check, c.want)
		}
		if strings.Contains(got.Message, "2020") {
			t.Errorf("%v: the message %q quotes the certificate", c.err, got.Message)
		}
	}
}
