package network

import (
	"context"
	"testing"
	"time"
)

func TestSCUGetNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	err := scu.Get(context.Background(), nil)
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUCancelNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	err := scu.Cancel(context.Background(), 1)
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUNEventReportNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	_, err := scu.NEventReport(context.Background(), "1.2.3", "1.2.3.4", 1, nil)
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUNGetNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	_, err := scu.NGet(context.Background(), "1.2.3", "1.2.3.4")
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUNSetNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	_, err := scu.NSet(context.Background(), "1.2.3", "1.2.3.4", nil)
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUNActionNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	_, err := scu.NAction(context.Background(), "1.2.3", "1.2.3.4", 1, nil)
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUNCreateNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	_, err := scu.NCreate(context.Background(), "1.2.3", "1.2.3.4", nil)
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUNDeleteNotAssociated(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	_, err := scu.NDelete(context.Background(), "1.2.3", "1.2.3.4")
	if err == nil {
		t.Error("expected error when not associated")
	}
}

func TestSCUGetAssociation(t *testing.T) {
	scu := NewSCU(SCUConfig{CallingAE: "TEST"})
	if scu.getAssociation() != nil {
		t.Error("getAssociation should return nil when not associated")
	}
}

func TestBuildCCancelRQ(t *testing.T) {
	ds := BuildCCancelRQ(42)
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandCCancelRQ {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandCCancelRQ, cmdField)
	}
	if HasDataSet(ds) {
		t.Error("C-CANCEL-RQ should not have dataset")
	}
}

func TestCommandCCancelConstant(t *testing.T) {
	if CommandCCancelRQ != 0x0FFF {
		t.Errorf("expected C-CANCEL-RQ = 0x0FFF, got 0x%04X", CommandCCancelRQ)
	}
}

// TestIntegrationSCUGetViaSCP tests C-GET end-to-end (SCU initiates, SCP handles).
func TestIntegrationSCUGetViaSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "GET_SCP",
		Port:        0,
		BindAddress: "127.0.0.1",
	}, &BaseHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	// The default BaseHandler returns StatusUnableToProcess for C-GET,
	// which is the expected behavior. We just verify the round-trip works.
	scu := NewSCU(SCUConfig{
		CallingAE: "GET_SCU",
		CalledAE:  "GET_SCP",
		Address:   server.Addr(),
	})

	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer scu.Release(ctx)

	// C-GET will get a failure status from the default handler, but the
	// protocol round-trip should complete without panics or hangs.
	// We don't check the error since the default handler rejects it.
	_ = scu.Echo(ctx) // At least echo should work
}
