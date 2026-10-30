package network_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
)

// ARTIMTimeout and DIMSETimeout were documented and defaulted, and nothing
// read them, so a peer that connected and sent nothing held its association
// slot for as long as it liked, and an SCU waited on a silent peer forever
// (#206).

const shortTimeout = 300 * time.Millisecond

func startTimeoutServer(t *testing.T, cfg network.SCPConfig, handler network.Handler) *network.Server {
	t.Helper()
	cfg.AETitle, cfg.Port, cfg.BindAddress = "TO_SCP", 0, "127.0.0.1"
	// Background, as a deployed server runs: a context with a deadline would
	// bound every read and hide a missing timeout.
	server, err := network.StartServer(context.Background(), cfg, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	t.Cleanup(server.Stop)
	return server
}

func echo(t *testing.T, addr string, cfg network.NetworkConfig) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	scu := network.NewSCU(network.SCUConfig{CallingAE: "TO_SCU", CalledAE: "TO_SCP", Address: addr, Network: cfg})
	if err := scu.Associate(ctx, network.DefaultVerificationContexts()); err != nil {
		return err
	}
	defer func() { _ = scu.Release(ctx) }()
	return scu.Echo(ctx)
}

func idleConn(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestIdlePeerDoesNotHoldASlot: PS3.8 9.1.5 starts ARTIM when a connection is
// accepted and closes the connection when it expires. Two peers that sent
// nothing filled a server of two slots for good.
func TestIdlePeerDoesNotHoldASlot(t *testing.T) {
	server := startTimeoutServer(t, network.SCPConfig{
		MaxAssociations: 2, Network: network.NetworkConfig{ARTIMTimeout: shortTimeout},
	}, &network.EchoHandler{})
	idle := []net.Conn{idleConn(t, server.Addr()), idleConn(t, server.Addr())}

	time.Sleep(3 * shortTimeout)
	if err := echo(t, server.Addr(), network.NetworkConfig{}); err != nil {
		t.Errorf("echo after the idle peers' ARTIM expired: %v", err)
	}
	for i, conn := range idle {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := conn.Read(make([]byte, 1)); err == nil || isTimeout(err) {
			t.Errorf("idle connection %d is still open: %v", i, err)
		}
	}
}

// TestOverLimitPeerDoesNotStopTheAcceptLoop: a connection that arrived while
// the server was full was read inline in the accept loop, with no deadline,
// so one silent peer made every later connection wait behind it.
func TestOverLimitPeerDoesNotStopTheAcceptLoop(t *testing.T) {
	server := startTimeoutServer(t, network.SCPConfig{
		// Long enough that the test is over before it fires: the loop must not
		// be waiting on the silent peer at all.
		MaxAssociations: 1, Network: network.NetworkConfig{ARTIMTimeout: 10 * time.Second},
	}, &network.EchoHandler{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	holder := network.NewSCU(network.SCUConfig{CallingAE: "TO_SCU", CalledAE: "TO_SCP", Address: server.Addr()})
	if err := holder.Associate(ctx, network.DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = holder.Release(ctx) }()

	idleConn(t, server.Addr())
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	err := echo(t, server.Addr(), network.NetworkConfig{})
	if err == nil {
		t.Fatal("a second association was accepted by a server limited to one")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the rejection took %v: the accept loop was waiting on the silent peer (%v)", elapsed, err)
	}
}

// TestSCUTimesOutOnASilentPeer: Associate read the A-ASSOCIATE-AC with the
// caller's context alone, so with context.Background it never returned.
func TestSCUTimesOutOnASilentPeer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	done := make(chan error, 1)
	go func() {
		scu := network.NewSCU(network.SCUConfig{CallingAE: "TO_SCU", CalledAE: "TO_SCP", Address: ln.Addr().String(),
			Network: network.NetworkConfig{ARTIMTimeout: shortTimeout}})
		done <- scu.Associate(context.Background(), network.DefaultVerificationContexts())
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("associated with a peer that never answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Associate was still waiting on a silent peer after 5s")
	}
}

// TestSCUTimesOutWaitingForAResponse: an operation waited for its response
// until the caller's context ended, however long DIMSETimeout said.
func TestSCUTimesOutWaitingForAResponse(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	server := startTimeoutServer(t, network.SCPConfig{}, &network.StorageHandler{
		OnStore: func(_ context.Context, _, _ string, _ *dataset.Dataset) uint16 {
			<-release
			return network.StatusSuccess
		},
	})

	scu := network.NewSCU(network.SCUConfig{CallingAE: "TO_SCU", CalledAE: "TO_SCP", Address: server.Addr(),
		Network: network.NetworkConfig{DIMSETimeout: shortTimeout}})
	if err := scu.Associate(context.Background(), nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Abort(context.Background()) }()

	done := make(chan error, 1)
	go func() { done <- scu.Store(context.Background(), retrieveInstances(1)[0]) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the store succeeded though the SCP never answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Store was still waiting for a response after 5s")
	}
}

// TestIdleAssociationIsAborted: once associated, the SCP waited for the next
// PDU with no bound, so a peer that stopped partway through one, or went
// quiet, kept its slot. DIMSETimeout bounds each read.
func TestIdleAssociationIsAborted(t *testing.T) {
	server := startTimeoutServer(t, network.SCPConfig{
		MaxAssociations: 1, Network: network.NetworkConfig{DIMSETimeout: shortTimeout},
	}, &network.EchoHandler{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	quiet := network.NewSCU(network.SCUConfig{CallingAE: "TO_SCU", CalledAE: "TO_SCP", Address: server.Addr()})
	if err := quiet.Associate(ctx, network.DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = quiet.Abort(ctx) }()

	time.Sleep(3 * shortTimeout)
	if err := echo(t, server.Addr(), network.NetworkConfig{}); err != nil {
		t.Errorf("the quiet association still holds the only slot: %v", err)
	}
}

func isTimeout(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}
