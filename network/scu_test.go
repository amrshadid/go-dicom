package network

import (
	"context"
	"net"
	"testing"
	"time"
)

// mockSCPEcho runs a minimal SCP that handles C-ECHO.
func mockSCPEcho(t *testing.T, conn net.Conn, ctx context.Context) {
	t.Helper()
	transport := NewTransport(conn, DefaultMaxPDUSize)
	defer transport.Close()

	// Read A-ASSOCIATE-RQ
	pdu, err := transport.ReadPDU(ctx)
	if err != nil {
		t.Errorf("mock SCP: read RQ: %v", err)
		return
	}
	rq, ok := pdu.(*AssociateRQ)
	if !ok {
		t.Errorf("mock SCP: expected AssociateRQ, got %T", pdu)
		return
	}

	// Accept all contexts
	assoc := NewAssociation(transport)
	supportedAS := map[string]bool{VerificationSOPClassUID: true}
	supportedTS := map[string]bool{ExplicitVRLittleEndianUID: true, ImplicitVRLittleEndianUID: true}
	if err := assoc.AcceptAssociation(ctx, rq, supportedAS, supportedTS, DefaultMaxPDUSize); err != nil {
		t.Errorf("mock SCP: accept: %v", err)
		return
	}

	// Read C-ECHO-RQ
	_, cmdData, isCmd, err := assoc.ReceivePData(ctx)
	if err != nil {
		t.Errorf("mock SCP: receive: %v", err)
		return
	}
	if !isCmd {
		t.Error("mock SCP: expected command")
		return
	}

	cmdDS, err := DecodeCommandDataset(cmdData)
	if err != nil {
		t.Errorf("mock SCP: decode: %v", err)
		return
	}

	_, msgID, _, _ := ParseCommandDataset(cmdDS)

	// Send C-ECHO-RSP
	rspDS := BuildCEchoRSP(msgID, StatusSuccess)
	rspBytes, _ := EncodeCommandDataset(rspDS)

	accepted := assoc.AcceptedContexts()
	var pcID byte
	for id := range accepted {
		pcID = id
		break
	}
	assoc.SendPData(ctx, pcID, rspBytes, true)

	// Handle release
	relPDU, err := transport.ReadPDU(ctx)
	if err != nil {
		return
	}
	if _, ok := relPDU.(*ReleaseRQ); ok {
		transport.WritePDU(ctx, &ReleaseRP{})
	}
}

func TestSCUEcho(t *testing.T) {
	// Set up a mock SCP
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go mockSCPEcho(t, server, ctx)

	// Create SCU using the pipe connection directly
	scu := NewSCU(SCUConfig{
		CallingAE: "TEST_SCU",
		CalledAE:  "TEST_SCP",
	})

	// Manually set up the association using the pipe
	transport := NewTransport(client, DefaultMaxPDUSize)
	scu.association = NewAssociation(transport)

	contexts := DefaultVerificationContexts()
	err := scu.association.RequestAssociation(ctx, "TEST_SCU", "TEST_SCP", contexts, DefaultMaxPDUSize)
	if err != nil {
		t.Fatalf("associate failed: %v", err)
	}

	// Perform C-ECHO
	err = scu.Echo(ctx)
	if err != nil {
		t.Fatalf("Echo failed: %v", err)
	}

	// Release
	err = scu.Release(ctx)
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}
}

func TestSCUEchoNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{
		CallingAE: "TEST_SCU",
		CalledAE:  "TEST_SCP",
		Address:   "localhost:11112",
	})

	ctx := context.Background()
	err := scu.Echo(ctx)
	if err == nil {
		t.Fatal("expected error when not associated")
	}
}

func TestSCUIsAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{
		CallingAE: "TEST_SCU",
		CalledAE:  "TEST_SCP",
	})

	if scu.IsAssociated() {
		t.Error("should not be associated initially")
	}
}

func TestSCUReleaseNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{})
	err := scu.Release(context.Background())
	if err != nil {
		t.Errorf("Release on unassociated SCU should not error, got: %v", err)
	}
}

func TestSCUAbortNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{})
	err := scu.Abort(context.Background())
	if err != nil {
		t.Errorf("Abort on unassociated SCU should not error, got: %v", err)
	}
}

func TestSCUNextMessageID(t *testing.T) {
	scu := NewSCU(SCUConfig{})
	id1 := scu.nextMessageID()
	id2 := scu.nextMessageID()
	if id2 != id1+1 {
		t.Errorf("expected sequential message IDs, got %d and %d", id1, id2)
	}
}
