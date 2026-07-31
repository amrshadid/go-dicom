package network

import (
	"context"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
)

// TestPatientStudyOnlyModelIsNegotiated verifies the retired Patient/Study Only
// query/retrieve model is proposed and accepted, since some archives still
// offer only that one.
func TestPatientStudyOnlyModelIsNegotiated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "PSO_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnFind: func(context.Context, string, *dataset.Dataset) ([]*dataset.Dataset, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "PSO_SCU", CalledAE: "PSO_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	accepted := scu.Association().AcceptedContexts()
	for _, uid := range []string{
		PatientStudyOnlyQueryRetrieveFind,
		PatientStudyOnlyQueryRetrieveMove,
		PatientStudyOnlyQueryRetrieveGet,
	} {
		if _, ok := FindPresentationContextID(accepted, uid); !ok {
			t.Errorf("no accepted presentation context for %s", uid)
		}
	}
}

// TestFindFallsBackToPatientStudyOnly verifies the SCU's model fallback reaches
// the third rung when a peer offers only Patient/Study Only.
func TestFindFallsBackToPatientStudyOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var sawSOPClass string
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "PSO_ONLY", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnFind: func(_ context.Context, sopClassUID string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			sawSOPClass = sopClassUID
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	// Offer only the retired model, so the two current ones are rejected.
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID,
		PatientStudyOnlyQueryRetrieveFind,
	})

	scu := NewSCU(SCUConfig{
		CallingAE: "PSO_SCU", CalledAE: "PSO_ONLY", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	results, err := scu.Find(ctx, dataset.NewDataset())
	if err != nil {
		t.Fatalf("Find: %v — the SCU did not fall back to Patient/Study Only", err)
	}
	for r := range results {
		if r.Err != nil {
			t.Fatalf("Find result: %v", r.Err)
		}
	}

	if sawSOPClass != PatientStudyOnlyQueryRetrieveFind {
		t.Errorf("handler saw SOP Class %q, want %q", sawSOPClass, PatientStudyOnlyQueryRetrieveFind)
	}
}
