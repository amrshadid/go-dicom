package network

import (
	"context"
	"net"
	"testing"
	"time"
)

// Three places where the association accepted, or ignored, what PS3.8 says to
// abort on or to answer (#209).

// echoServer returns an SCU associated with an Echo SCP on context 1.
func echoServer(t *testing.T) (*SCU, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	server, err := StartServer(context.Background(), SCPConfig{AETitle: "STATE_SCP", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "STATE_SCP", Address: server.Addr()})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scu.Association().transport.Close() })
	return scu, ctx
}

// readAbort fails unless the next PDU is an A-ABORT with the given reason.
func readAbort(ctx context.Context, t *testing.T, scu *SCU, reason byte, why string) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pdu, err := scu.Association().transport.ReadPDU(readCtx)
	if err != nil {
		t.Fatalf("%s: no A-ABORT, the connection ended with %v", why, err)
	}
	abort, ok := pdu.(*AbortPDU)
	if !ok {
		t.Fatalf("%s: got %T, want an A-ABORT", why, pdu)
	}
	if abort.Source != AbortSourceServiceProvider || abort.Reason != reason {
		t.Errorf("%s: A-ABORT source %d reason %d, want %d and %d", why,
			abort.Source, abort.Reason, AbortSourceServiceProvider, reason)
	}
}

// TestDataOnAnUnacceptedContextIsAborted: a C-ECHO on a context that was never
// proposed was answered, its data decoded under a transfer syntax nobody
// negotiated. PS3.8 9.3.5 calls for an A-ABORT.
func TestDataOnAnUnacceptedContextIsAborted(t *testing.T) {
	scu, ctx := echoServer(t)
	cmd, err := EncodeCommandDataset(BuildCEchoRQ(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := scu.Association().transport.WritePDU(ctx, &PDataTF{PDVItems: []PDVItem{
		{PresentationContextID: 7, IsCommand: true, IsLast: true, Data: cmd},
	}}); err != nil {
		t.Fatal(err)
	}
	readAbort(ctx, t, scu, abortUnexpectedPDUParameter, "a C-ECHO on context 7")
}

// TestUnexpectedPDUIsAborted: a second A-ASSOCIATE-RQ on an established
// association closed the connection with no A-ABORT. PS3.8 Table 9-10, AA-8,
// sends one naming an unexpected PDU.
func TestUnexpectedPDUIsAborted(t *testing.T) {
	scu, ctx := echoServer(t)
	if err := scu.Association().transport.WritePDU(ctx, &AssociateRQ{
		ProtocolVersion: ProtocolVersion, CalledAE: "STATE_SCP", CallingAE: "SCU",
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts:  DefaultVerificationContexts(),
		UserInformation:       UserInformationItem{MaxPDULength: DefaultMaxPDUSize},
	}); err != nil {
		t.Fatal(err)
	}
	readAbort(ctx, t, scu, abortUnexpectedPDU, "a second A-ASSOCIATE-RQ")
}

// TestReleaseCollisionIsAnswered: when the peer sent its own A-RELEASE-RQ as
// the SCU released, Release failed on it and the peer never got the
// A-RELEASE-RP it was owed (PS3.8 9.2.7).
func TestReleaseCollisionIsAnswered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// The acceptor's side of a collision: answer the SCU's A-RELEASE-RQ with
	// one of its own, wait for the SCU's A-RELEASE-RP, then send its own.
	gotRP := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			gotRP <- err
			return
		}
		peer := NewTransport(conn, DefaultMaxPDUSize)
		defer peer.Close()
		pdu, err := peer.ReadPDU(ctx)
		if err != nil {
			gotRP <- err
			return
		}
		assoc := NewAssociation(peer)
		if err := assoc.AcceptAssociation(ctx, pdu.(*AssociateRQ),
			map[string]bool{VerificationSOPClassUID: true},
			map[string]bool{ImplicitVRLittleEndianUID: true, ExplicitVRLittleEndianUID: true},
			DefaultMaxPDUSize); err != nil {
			gotRP <- err
			return
		}
		if _, err := peer.ReadPDU(ctx); err != nil { // the SCU's A-RELEASE-RQ
			gotRP <- err
			return
		}
		_ = peer.WritePDU(ctx, &ReleaseRQ{})
		reply, err := peer.ReadPDU(ctx)
		if err != nil {
			gotRP <- err
			return
		}
		if _, ok := reply.(*ReleaseRP); !ok {
			gotRP <- NewAssociationError("UNEXPECTED_PDU", "the SCU answered the collision with something else")
			return
		}
		gotRP <- peer.WritePDU(ctx, &ReleaseRP{})
	}()

	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "PEER", Address: ln.Addr().String()})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatal(err)
	}
	if err := scu.Release(ctx); err != nil {
		t.Errorf("Release during a collision: %v", err)
	}
	if err := <-gotRP; err != nil {
		t.Errorf("the peer was not answered: %v", err)
	}
}
