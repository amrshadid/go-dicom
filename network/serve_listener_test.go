package network

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"testing"
	"time"
)

// A program that has to decide how its socket is made, which of IPv4 and IPv6
// a wildcard covers or a descriptor it was handed, binds the listener itself
// and gives it to the server. These check that the server serves on it, wraps
// it for TLS when it has a TLS configuration, and closes it when it stops.

// echoOn associates with addr and echoes, as a caller of the server would.
func echoOn(ctx context.Context, t *testing.T, addr string, tlsCfg *TLSConfig) error {
	t.Helper()
	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "LISTEN_SCP", Address: addr, TLS: tlsCfg})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		return err
	}
	defer func() { _ = scu.Release(ctx) }()
	return scu.Echo(ctx)
}

// closed reports whether a connection to addr is refused: the listener there
// is gone.
func closed(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
		return false
	}
	return true
}

// serveAsync runs scp.Serve on l and returns what it returned, once it has.
func serveAsync(ctx context.Context, scp *SCP, l net.Listener) <-chan error {
	done := make(chan error, 1)
	go func() { done <- scp.Serve(ctx, l) }()
	return done
}

func newListenSCP(tlsCfg *TLSConfig) *SCP {
	scp := NewSCP(SCPConfig{AETitle: "LISTEN_SCP", BindAddress: "192.0.2.1", Port: 1, TLS: tlsCfg})
	scp.SetHandler(&EchoHandler{})
	return scp
}

// TestServeAcceptsOnTheListenerGiven: an SCP serves on the listener the caller
// made, whatever BindAddress and Port say, and the caller's context ending
// ends Serve with the context's error and closes the listener.
func TestServeAcceptsOnTheListenerGiven(t *testing.T) {
	base := runtime.NumGoroutine()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := serveAsync(ctx, newListenSCP(nil), l)

	if err := echoOn(ctx, t, l.Addr().String(), nil); err != nil {
		t.Fatalf("an echo on the caller's listener: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Serve returned %v, want the context's error", err)
	}
	if !closed(l.Addr().String()) {
		t.Error("the listener was left open when Serve ended")
	}
	goroutinesSettle(t, base)
}

// TestServeHasNoWindowBetweenBindAndServe: a caller that connects the moment
// the socket is bound, and sends its A-ASSOCIATE-RQ, before anything is serving
// on it, is not refused: the connection and the request wait in the listener's
// queue and are answered when Serve begins. This is what binding in one place
// and serving in another is for.
func TestServeHasNoWindowBetweenBindAndServe(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Dial returns once the kernel has the connection, with nothing accepting
	// it: the request is written into a socket nobody is serving yet.
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("a connection to a bound socket before Serve began was refused: %v", err)
	}
	defer conn.Close()
	raw, err := EncodePDU(associateRQ("SCANNER", "LISTEN_SCP"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(raw); err != nil {
		t.Fatal(err)
	}

	scp := newListenSCP(nil)
	done := serveAsync(ctx, scp, l)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	answer := make([]byte, 1)
	if _, err := io.ReadFull(conn, answer); err != nil || answer[0] != PDUTypeAssociateAC {
		t.Errorf("the request made before Serve began was answered with % x (%v), want an A-ASSOCIATE-AC", answer, err)
	}
	_ = scp.Close()
	if err := <-done; err != nil {
		t.Errorf("Serve returned %v after Close, want nil", err)
	}
	if !closed(l.Addr().String()) {
		t.Error("Close left the caller's listener open")
	}
}

// TestServeOnAnIPv6Listener: the caller chooses the family: a server serves on
// a listener bound to the IPv6 loopback, which BindAddress and Port, joined as
// "host:port", could only have named as "::1:port".
func TestServeOnAnIPv6Listener(t *testing.T) {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	scp := newListenSCP(nil)
	done := serveAsync(ctx, scp, l)
	if err := echoOn(ctx, t, l.Addr().String(), nil); err != nil {
		t.Errorf("an echo on %s: %v", l.Addr(), err)
	}
	_ = scp.Close()
	<-done
}

// TestServeWrapsTheListenerForTLS: with a TLS configuration the listener the
// caller made, a plain one, is wrapped for TLS as ListenTLS wraps its own: a
// client with the right certificate associates, and one that speaks plain TCP
// does not.
func TestServeWrapsTheListenerForTLS(t *testing.T) {
	pki := newTestPKI(t)
	serverTLS, clientTLS := mutualTLS(pki)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	scp := newListenSCP(serverTLS)
	done := serveAsync(ctx, scp, l)

	if err := echoOn(ctx, t, l.Addr().String(), clientTLS); err != nil {
		t.Errorf("an echo over TLS on the caller's listener: %v", err)
	}
	if err := echoOn(ctx, t, l.Addr().String(), nil); err == nil {
		t.Error("a plain client associated on a listener served with TLS")
	}
	_ = scp.Close()
	<-done
	if !closed(l.Addr().String()) {
		t.Error("Close left the TLS listener open")
	}
}

// TestServeRefusesWhatItCannotUse: a listener that is nil is an error, and so
// is a TLS configuration that cannot be used, which leaves the caller's
// listener untouched: still open, and the caller's to close.
func TestServeRefusesWhatItCannotUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := newListenSCP(nil).Serve(ctx, nil); err == nil {
		t.Error("Serve accepted a nil listener")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	bad := &TLSConfig{CertFile: "/nonexistent.crt", KeyFile: "/nonexistent.key"}
	if err := newListenSCP(bad).Serve(ctx, l); err == nil {
		t.Error("Serve accepted a TLS configuration that cannot be used")
	}
	if _, err := StartServerOn(ctx, SCPConfig{AETitle: "LISTEN_SCP", TLS: bad}, &EchoHandler{}, l); err == nil {
		t.Error("StartServerOn accepted a TLS configuration that cannot be used")
	}
	if closed(l.Addr().String()) {
		t.Error("the caller's listener was closed by a refusal")
	}
}

// TestStartServerOn: a Server runs on the listener the caller made, plain and
// over TLS, reports its address, and Stop closes the listener.
func TestStartServerOn(t *testing.T) {
	pki := newTestPKI(t)
	serverTLS, clientTLS := mutualTLS(pki)
	for _, c := range []struct {
		name      string
		server, c *TLSConfig
	}{
		{"plain", nil, nil},
		{"TLS", serverTLS, clientTLS},
	} {
		t.Run(c.name, func(t *testing.T) {
			base := runtime.NumGoroutine()
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			server, err := StartServerOn(ctx, SCPConfig{AETitle: "LISTEN_SCP", TLS: c.server}, &EchoHandler{}, l)
			if err != nil {
				t.Fatal(err)
			}
			if server.Addr() != l.Addr().String() {
				t.Errorf("the server reports %s, the listener is %s", server.Addr(), l.Addr())
			}
			if err := echoOn(ctx, t, l.Addr().String(), c.c); err != nil {
				t.Errorf("an echo: %v", err)
			}
			server.Stop()
			server.Wait()
			if !closed(l.Addr().String()) {
				t.Error("Stop left the caller's listener open")
			}
			goroutinesSettle(t, base)
		})
	}
}

// TestShutdownOnAGivenListener: Shutdown drains a server running on the
// caller's listener as it does its own: a caller during the drain is refused
// as transient, and the caller's listener is closed when the drain ends.
func TestShutdownOnAGivenListener(t *testing.T) {
	base := runtime.NumGoroutine()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	handler := newGatedStore(time.Second)
	server, err := StartServerOn(context.Background(), SCPConfig{AETitle: "ARCHIVE"}, handler, l)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: addr})
	if err := scu.Associate(context.Background(), []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	stored := make(chan error, 1)
	go func() { stored <- scu.Store(context.Background(), corpusInstance(t)) }()
	<-handler.started

	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopped <- server.Shutdown(ctx)
	}()
	waitDraining(t, server)
	late, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("a caller during the drain was refused at TCP: %v", err)
	}
	wantTransientRefusal(t, "a caller on the given listener during the drain", requestAssociation(t, late))
	late.Close()

	if err := <-stored; err != nil {
		t.Errorf("the store in flight: %v", err)
	}
	_ = scu.Release(context.Background())
	if err := <-stopped; err != nil {
		t.Errorf("Shutdown returned %v", err)
	}
	if !closed(addr) {
		t.Error("Shutdown left the caller's listener open when the drain ended")
	}
	goroutinesSettle(t, base)
}
