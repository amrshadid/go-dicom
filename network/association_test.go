package network

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestAssociationRequestAndAccept(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverTransport := NewTransport(server, DefaultMaxPDUSize)
	clientTransport := NewTransport(client, DefaultMaxPDUSize)

	clientAssoc := NewAssociation(clientTransport)
	serverAssoc := NewAssociation(serverTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	contexts := DefaultVerificationContexts()

	supportedAS := map[string]bool{VerificationSOPClassUID: true}
	supportedTS := map[string]bool{
		ExplicitVRLittleEndianUID: true,
		ImplicitVRLittleEndianUID: true,
	}

	// Run client and server in parallel
	errCh := make(chan error, 2)

	go func() {
		errCh <- clientAssoc.RequestAssociation(ctx, "SCU", "SCP", contexts, DefaultMaxPDUSize)
	}()

	go func() {
		// Server reads A-ASSOCIATE-RQ
		pdu, err := serverTransport.ReadPDU(ctx)
		if err != nil {
			errCh <- err
			return
		}
		rq, ok := pdu.(*AssociateRQ)
		if !ok {
			errCh <- NewAssociationError("TEST", "expected AssociateRQ")
			return
		}
		errCh <- serverAssoc.AcceptAssociation(ctx, rq, supportedAS, supportedTS, DefaultMaxPDUSize)
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("error: %v", err)
		}
	}

	if clientAssoc.State() != StateAssociated {
		t.Errorf("client state: expected Associated, got %s", clientAssoc.State())
	}
	if serverAssoc.State() != StateAssociated {
		t.Errorf("server state: expected Associated, got %s", serverAssoc.State())
	}
	if clientAssoc.CallingAE() != "SCU" {
		t.Errorf("expected CallingAE 'SCU', got '%s'", clientAssoc.CallingAE())
	}
	if clientAssoc.CalledAE() != "SCP" {
		t.Errorf("expected CalledAE 'SCP', got '%s'", clientAssoc.CalledAE())
	}

	// Check accepted contexts
	accepted := clientAssoc.AcceptedContexts()
	if len(accepted) == 0 {
		t.Error("expected at least one accepted presentation context")
	}
}

func TestAssociationReject(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverTransport := NewTransport(server, DefaultMaxPDUSize)
	clientTransport := NewTransport(client, DefaultMaxPDUSize)

	clientAssoc := NewAssociation(clientTransport)
	serverAssoc := NewAssociation(serverTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	contexts := DefaultVerificationContexts()

	errCh := make(chan error, 2)

	go func() {
		errCh <- clientAssoc.RequestAssociation(ctx, "SCU", "SCP", contexts, DefaultMaxPDUSize)
	}()

	go func() {
		pdu, err := serverTransport.ReadPDU(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if _, ok := pdu.(*AssociateRQ); !ok {
			errCh <- NewAssociationError("TEST", "expected AssociateRQ")
			return
		}
		errCh <- serverAssoc.RejectAssociation(ctx, RJResultRejectedPermanent, RJSourceServiceUser, 1)
	}()

	// Server should succeed (sent rejection)
	if err := <-errCh; err != nil {
		// This could be either the server (no error) or client (rejection error)
		if _, ok := err.(*AssociationError); !ok {
			t.Fatalf("unexpected error type: %v", err)
		}
	}

	// The other channel
	err := <-errCh
	if err == nil {
		// If no error, it was the server side - check the other one
		// The client should have gotten a rejection
	} else {
		if assocErr, ok := err.(*AssociationError); ok {
			if assocErr.Code != "ASSOCIATION_REJECTED" {
				t.Errorf("expected ASSOCIATION_REJECTED, got %s", assocErr.Code)
			}
		}
	}

	if clientAssoc.State() != StateIdle {
		t.Errorf("client state after rejection: expected Idle, got %s", clientAssoc.State())
	}
}

func TestAssociationRelease(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverTransport := NewTransport(server, DefaultMaxPDUSize)
	clientTransport := NewTransport(client, DefaultMaxPDUSize)

	clientAssoc := NewAssociation(clientTransport)
	serverAssoc := NewAssociation(serverTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	contexts := DefaultVerificationContexts()
	supportedAS := map[string]bool{VerificationSOPClassUID: true}
	supportedTS := map[string]bool{ExplicitVRLittleEndianUID: true, ImplicitVRLittleEndianUID: true}

	// Establish association
	errCh := make(chan error, 2)
	go func() {
		errCh <- clientAssoc.RequestAssociation(ctx, "SCU", "SCP", contexts, DefaultMaxPDUSize)
	}()
	go func() {
		pdu, err := serverTransport.ReadPDU(ctx)
		if err != nil {
			errCh <- err
			return
		}
		rq := pdu.(*AssociateRQ)
		errCh <- serverAssoc.AcceptAssociation(ctx, rq, supportedAS, supportedTS, DefaultMaxPDUSize)
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("association setup error: %v", err)
		}
	}

	// Now release
	go func() {
		// Server reads A-RELEASE-RQ and sends A-RELEASE-RP
		pdu, err := serverTransport.ReadPDU(ctx)
		if err != nil {
			return
		}
		if _, ok := pdu.(*ReleaseRQ); ok {
			serverTransport.WritePDU(ctx, &ReleaseRP{})
		}
	}()

	err := clientAssoc.Release(ctx)
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	if clientAssoc.State() != StateIdle {
		t.Errorf("client state after release: expected Idle, got %s", clientAssoc.State())
	}
}

func TestAssociationStateString(t *testing.T) {
	tests := []struct {
		state    AssociationState
		expected string
	}{
		{StateIdle, "Idle"},
		{StateAwaitingAssocResponse, "AwaitingAssocResponse"},
		{StateAssociated, "Associated"},
		{StateAwaitingRelease, "AwaitingRelease"},
		{StateAwaitingReleaseRP, "AwaitingReleaseRP"},
		{AssociationState(99), "Unknown(99)"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expected {
			t.Errorf("state %d: expected %q, got %q", tt.state, tt.expected, tt.state.String())
		}
	}
}

func TestAssociationSendReceivePData(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverTransport := NewTransport(server, DefaultMaxPDUSize)
	clientTransport := NewTransport(client, DefaultMaxPDUSize)

	clientAssoc := NewAssociation(clientTransport)
	serverAssoc := NewAssociation(serverTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	contexts := DefaultVerificationContexts()
	supportedAS := map[string]bool{VerificationSOPClassUID: true}
	supportedTS := map[string]bool{ExplicitVRLittleEndianUID: true, ImplicitVRLittleEndianUID: true}

	// Establish association
	errCh := make(chan error, 2)
	go func() {
		errCh <- clientAssoc.RequestAssociation(ctx, "SCU", "SCP", contexts, DefaultMaxPDUSize)
	}()
	go func() {
		pdu, err := serverTransport.ReadPDU(ctx)
		if err != nil {
			errCh <- err
			return
		}
		rq := pdu.(*AssociateRQ)
		errCh <- serverAssoc.AcceptAssociation(ctx, rq, supportedAS, supportedTS, DefaultMaxPDUSize)
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	// Send data from client to server
	testData := []byte("hello DICOM")
	go func() {
		clientAssoc.SendPData(ctx, 1, testData, true)
	}()

	ctxID, data, isCmd, err := serverAssoc.ReceivePData(ctx)
	if err != nil {
		t.Fatalf("ReceivePData failed: %v", err)
	}
	if ctxID != 1 {
		t.Errorf("expected context ID 1, got %d", ctxID)
	}
	if !isCmd {
		t.Error("expected isCommand=true")
	}
	if string(data) != "hello DICOM" {
		t.Errorf("expected 'hello DICOM', got %q", string(data))
	}
}
