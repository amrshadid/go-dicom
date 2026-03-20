package network

import (
	"context"
	"net"
	"testing"
)

func TestAssociationInfoFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	info := AssociationInfoFromContext(ctx)
	if info != nil {
		t.Fatal("expected nil AssociationInfo from empty context")
	}
}

func TestAssociationInfoRoundTrip(t *testing.T) {
	ctx := context.Background()

	remoteAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.100:54321")
	localAddr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:11112")

	original := &AssociationInfo{
		CallingAE:  "SCU_AE",
		CalledAE:   "SCP_AE",
		RemoteAddr: remoteAddr,
		LocalAddr:  localAddr,
		MaxPDUSize: 16384,
		AcceptedContexts: map[byte]*PresentationContext{
			1: {
				ID:             1,
				AbstractSyntax: VerificationSOPClassUID,
				TransferSyntax: ImplicitVRLittleEndianUID,
				Result:         PCResultAcceptance,
			},
		},
		PeerImplementationClassUID: "1.2.3.4.5",
		PeerImplementationVersion:  "TEST_IMPL_1.0",
	}

	ctx = ContextWithAssociationInfo(ctx, original)
	info := AssociationInfoFromContext(ctx)

	if info == nil {
		t.Fatal("expected non-nil AssociationInfo")
	}
	if info.CallingAE != "SCU_AE" {
		t.Errorf("CallingAE = %q, want %q", info.CallingAE, "SCU_AE")
	}
	if info.CalledAE != "SCP_AE" {
		t.Errorf("CalledAE = %q, want %q", info.CalledAE, "SCP_AE")
	}
	if info.RemoteAddr.String() != "192.168.1.100:54321" {
		t.Errorf("RemoteAddr = %q, want %q", info.RemoteAddr.String(), "192.168.1.100:54321")
	}
	if info.LocalAddr.String() != "0.0.0.0:11112" {
		t.Errorf("LocalAddr = %q, want %q", info.LocalAddr.String(), "0.0.0.0:11112")
	}
	if info.MaxPDUSize != 16384 {
		t.Errorf("MaxPDUSize = %d, want %d", info.MaxPDUSize, 16384)
	}
	if len(info.AcceptedContexts) != 1 {
		t.Errorf("AcceptedContexts length = %d, want 1", len(info.AcceptedContexts))
	}
	pc, ok := info.AcceptedContexts[1]
	if !ok {
		t.Fatal("expected presentation context with ID 1")
	}
	if pc.AbstractSyntax != VerificationSOPClassUID {
		t.Errorf("AbstractSyntax = %q, want %q", pc.AbstractSyntax, VerificationSOPClassUID)
	}
	if info.PeerImplementationClassUID != "1.2.3.4.5" {
		t.Errorf("PeerImplementationClassUID = %q, want %q", info.PeerImplementationClassUID, "1.2.3.4.5")
	}
	if info.PeerImplementationVersion != "TEST_IMPL_1.0" {
		t.Errorf("PeerImplementationVersion = %q, want %q", info.PeerImplementationVersion, "TEST_IMPL_1.0")
	}
}
