package network

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestStartServerAndEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "SRV_TEST",
		Port:        0,
		BindAddress: "127.0.0.1",
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	addr := server.Addr()
	if addr == "" {
		t.Fatal("server address should not be empty")
	}

	// Connect SCU
	scu := NewSCU(SCUConfig{
		CallingAE: "SRV_SCU",
		CalledAE:  "SRV_TEST",
		Address:   addr,
	})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if err := scu.Echo(ctx); err != nil {
		t.Fatalf("Echo: %v", err)
	}
	scu.Release(ctx)
}

func TestServerGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	group := NewServerGroup()

	// Start 3 servers on different ports
	s1, err := group.Add(ctx, SCPConfig{AETitle: "SRV1", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatalf("Add server 1: %v", err)
	}
	s2, err := group.Add(ctx, SCPConfig{AETitle: "SRV2", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatalf("Add server 2: %v", err)
	}
	s3, err := group.Add(ctx, SCPConfig{AETitle: "SRV3", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatalf("Add server 3: %v", err)
	}

	if group.Count() != 3 {
		t.Errorf("expected 3 servers, got %d", group.Count())
	}

	// Verify all 3 servers respond to C-ECHO
	for i, s := range []*Server{s1, s2, s3} {
		scu := NewSCU(SCUConfig{
			CallingAE: "GROUP_SCU",
			CalledAE:  s.scp.config.AETitle,
			Address:   s.Addr(),
		})
		if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
			t.Fatalf("Server %d Associate: %v", i+1, err)
		}
		if err := scu.Echo(ctx); err != nil {
			t.Fatalf("Server %d Echo: %v", i+1, err)
		}
		scu.Release(ctx)
	}

	group.StopAll()
}

func TestServerEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "EVT_SCP",
		Port:        0,
		BindAddress: "127.0.0.1",
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	connCount := 0
	server.Events.On(EVTConnOpen, func(e *Event) {
		connCount++
	})

	scu := NewSCU(SCUConfig{
		CallingAE: "EVT_SCU",
		CalledAE:  "EVT_SCP",
		Address:   server.Addr(),
	})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	scu.Echo(ctx)
	scu.Release(ctx)

	// Give event goroutine a moment to fire
	time.Sleep(50 * time.Millisecond)

	if connCount < 1 {
		t.Errorf("expected at least 1 connection event, got %d", connCount)
	}
}

// TestStartServerHonorsMaxAssociations: StartServer applies MaxAssociations,
// as ListenAndServe does (#170).
func TestStartServerHonorsMaxAssociations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle:         "LIMIT_SRV",
		Port:            0,
		BindAddress:     "127.0.0.1",
		MaxAssociations: 1,
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	first := NewSCU(SCUConfig{CallingAE: "LIMIT_SCU", CalledAE: "LIMIT_SRV", Address: server.Addr()})
	if err := first.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("first association: %v", err)
	}

	// The first is still held, so the second is over the limit.
	second := NewSCU(SCUConfig{CallingAE: "LIMIT_SCU", CalledAE: "LIMIT_SRV", Address: server.Addr()})
	if err := second.Associate(ctx, DefaultVerificationContexts()); err == nil {
		_ = second.Release(ctx)
		t.Fatal("a second association was accepted with MaxAssociations 1")
	}

	// And releasing the first frees its slot.
	if err := first.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		third := NewSCU(SCUConfig{CallingAE: "LIMIT_SCU", CalledAE: "LIMIT_SRV", Address: server.Addr()})
		err := third.Associate(ctx, DefaultVerificationContexts())
		if err == nil {
			_ = third.Release(ctx)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no association accepted after the first was released: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestListenAndServeReturnsAfterClose: Close stops ListenAndServe, which
// returns nil, rather than leaving it retrying a closed listener (#170).
func TestListenAndServeReturnsAfterClose(t *testing.T) {
	var log countingWriter
	saved := DefaultLogger
	DefaultLogger = NewLogger(LogLevelWarn, &log)
	defer func() { DefaultLogger = saved }()

	scp := NewSCP(SCPConfig{AETitle: "CLOSE_SCP", Port: freeTCPPort(t), BindAddress: "127.0.0.1"})
	done := make(chan error, 1)
	go func() { done <- scp.ListenAndServe(context.Background()) }()

	deadline := time.Now().Add(5 * time.Second)
	for scp.Addr() == "" {
		if time.Now().After(deadline) {
			t.Fatal("the SCP never bound its port")
		}
		time.Sleep(time.Millisecond)
	}
	if err := scp.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("ListenAndServe returned %v after Close, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ListenAndServe still running 2s after Close, %d lines logged", log.lines())
	}
}

// countingWriter counts the lines a Logger writes to it.
type countingWriter struct {
	mu sync.Mutex
	n  int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += bytes.Count(p, []byte("\n"))
	return len(p), nil
}

func (w *countingWriter) lines() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

// freeTCPPort returns a port that was free a moment ago. SCPConfig reads a
// Port of 0 as DefaultPort, not as "any", so a test that wants its own port
// has to find one.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
