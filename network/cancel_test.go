package network

import (
	"context"
	"testing"
	"time"
)

// TestCancelDoesNotTearDownTheAssociation verifies that a C-CANCEL leaves the
// association usable.
//
// The SCP's command switch had no case for C-CANCEL, so it fell through to the
// default branch, which logs "unsupported command" and aborts. Canceling did
// not merely fail — it destroyed the connection and any results already
// delivered, which is worse than ignoring the request.
func TestCancelDoesNotTearDownTheAssociation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "CANCEL_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "CANCEL_SCU", CalledAE: "CANCEL_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// Confirm the association works before canceling.
	if err := scu.Echo(ctx); err != nil {
		t.Fatalf("Echo before cancel: %v", err)
	}

	if err := scu.Cancel(ctx, 1); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// The association must still be usable. Before the fix this failed with the
	// peer having aborted.
	if err := scu.Echo(ctx); err != nil {
		t.Fatalf("Echo after cancel: %v — the association was torn down", err)
	}
	if !scu.IsAssociated() {
		t.Error("association reports not associated after a cancel")
	}
}
