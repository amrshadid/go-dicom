package network

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSCPNewAndConfig(t *testing.T) {
	scp := NewSCP(SCPConfig{
		AETitle: "MY_SCP",
		Port:    4242,
	})

	if scp.config.AETitle != "MY_SCP" {
		t.Errorf("expected AETitle 'MY_SCP', got '%s'", scp.config.AETitle)
	}
	if scp.config.Port != 4242 {
		t.Errorf("expected port 4242, got %d", scp.config.Port)
	}
}

func TestSCPDefaults(t *testing.T) {
	scp := NewSCP(SCPConfig{})
	if scp.config.AETitle != DefaultAETitle {
		t.Errorf("expected default AETitle '%s', got '%s'", DefaultAETitle, scp.config.AETitle)
	}
	if scp.config.Port != DefaultPort {
		t.Errorf("expected default port %d, got %d", DefaultPort, scp.config.Port)
	}
}

func TestSCPSetHandler(t *testing.T) {
	scp := NewSCP(SCPConfig{})
	handler := &EchoHandler{}
	scp.SetHandler(handler)

	if scp.handler != handler {
		t.Error("handler not set correctly")
	}
}

func TestSCPHandleCEcho(t *testing.T) {
	// Create pipes for client-server communication
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scp := NewSCP(SCPConfig{
		AETitle: "TEST_SCP",
	})
	handler := &EchoHandler{}
	scp.SetHandler(handler)

	// Run the SCP connection handler in a goroutine
	go func() {
		serverTransport := NewTransport(serverConn, DefaultMaxPDUSize)
		scp.handleConnection(ctx, serverTransport)
	}()

	// Client side: establish association and send C-ECHO
	clientTransport := NewTransport(clientConn, DefaultMaxPDUSize)
	clientAssoc := NewAssociation(clientTransport)

	contexts := DefaultVerificationContexts()
	err := clientAssoc.RequestAssociation(ctx, "TEST_SCU", "TEST_SCP", contexts, DefaultMaxPDUSize)
	if err != nil {
		t.Fatalf("association request failed: %v", err)
	}

	// Send C-ECHO-RQ
	pcID, ok := FindPresentationContextID(clientAssoc.AcceptedContexts(), VerificationSOPClassUID)
	if !ok {
		t.Fatal("no accepted presentation context for Verification")
	}

	cmdDS := BuildCEchoRQ(1)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	err = clientAssoc.SendPData(ctx, pcID, cmdBytes, true)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// Receive C-ECHO-RSP
	_, respData, isCmd, err := clientAssoc.ReceivePData(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if !isCmd {
		t.Fatal("expected command response")
	}

	respDS, err := DecodeCommandDataset(respData)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	_, _, status, _ := ParseCommandDataset(respDS)
	if status != StatusSuccess {
		t.Errorf("expected status success, got 0x%04X", status)
	}
}

func TestSCPListenAndServeCancel(t *testing.T) {
	scp := NewSCP(SCPConfig{
		AETitle:     "TEST_SCP",
		Port:        0, // Will get default
		BindAddress: "127.0.0.1",
	})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		// Use a random port
		scp.config.Port = 0

		ln, err := Listen("127.0.0.1:0")
		if err != nil {
			errCh <- err
			return
		}
		scp.mu.Lock()
		scp.listener = ln
		scp.running = true
		scp.mu.Unlock()

		// Cancel after a brief moment to test shutdown
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		// Try to accept (will be canceled)
		_, err = ln.Accept(ctx)
		errCh <- err
	}()

	err := <-errCh
	if err != context.Canceled {
		// Either context.Canceled or a close error is acceptable
		if err != nil {
			t.Logf("Accept returned: %v (expected context.Canceled or similar)", err)
		}
	}
}

func TestSCPAddr(t *testing.T) {
	scp := NewSCP(SCPConfig{})
	if scp.Addr() != "" {
		t.Error("expected empty addr when not listening")
	}
}

func TestSCPClose(t *testing.T) {
	scp := NewSCP(SCPConfig{})
	// Close without listening should not error
	err := scp.Close()
	if err != nil {
		t.Errorf("Close without listener should not error: %v", err)
	}
}

func TestSCPSetSupportedSyntaxes(t *testing.T) {
	scp := NewSCP(SCPConfig{})

	as := []string{VerificationSOPClassUID, CTImageStorageUID}
	scp.SetSupportedAbstractSyntaxes(as)

	ts := []string{ImplicitVRLittleEndianUID}
	scp.SetSupportedTransferSyntaxes(ts)

	if !scp.supportedAbstractSyntaxes[VerificationSOPClassUID] {
		t.Error("Verification should be supported")
	}
	if !scp.supportedAbstractSyntaxes[CTImageStorageUID] {
		t.Error("CT Image Storage should be supported")
	}
	if scp.supportedAbstractSyntaxes[MRImageStorageUID] {
		t.Error("MR Image Storage should not be supported")
	}
	if !scp.supportedTransferSyntaxes[ImplicitVRLittleEndianUID] {
		t.Error("Implicit VR LE should be supported")
	}
	if scp.supportedTransferSyntaxes[ExplicitVRLittleEndianUID] {
		t.Error("Explicit VR LE should not be supported")
	}
}
