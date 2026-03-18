package network

import (
	"context"
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
