package network

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestFailedAssociateClosesItsSocket: a rejected or failed Associate left the
// connection it opened to the garbage collector, so a client retrying against
// a peer that refused it ran out of descriptors (#210).
func TestFailedAssociateClosesItsSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{AETitle: "CLOSE_SCP", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// A peer that accepts the connection and then sends nothing.
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	go func() {
		for {
			conn, err := silent.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	for name, cfg := range map[string]SCUConfig{
		"rejected": {CallingAE: "SCU", CalledAE: "WRONG_AE", Address: server.Addr()},
		"timed out": {CallingAE: "SCU", CalledAE: "CLOSE_SCP", Address: silent.Addr().String(),
			Network: NetworkConfig{ARTIMTimeout: 200 * time.Millisecond}},
	} {
		t.Run(name, func(t *testing.T) {
			scu := NewSCU(cfg)
			if err := scu.Associate(ctx, DefaultVerificationContexts()); err == nil {
				t.Fatal("Associate succeeded")
			}
			assoc := scu.Association()
			if assoc != nil && !assoc.transport.IsClosed() {
				t.Error("the failed association's connection is still open")
			}
		})
	}
}
