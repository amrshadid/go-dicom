package network

import (
	"context"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// worklistServer starts an SCP serving Modality Worklist queries.
func worklistServer(t *testing.T, ctx context.Context, items int) *Server {
	t.Helper()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "MWL_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, &WorklistHandler{
		OnWorklist: func(context.Context, *dataset.Dataset) ([]*dataset.Dataset, error) {
			out := make([]*dataset.Dataset, items)
			for i := range out {
				ds := dataset.NewDataset()
				_ = ds.Add(dataelem.NewDataElement(
					tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
				out[i] = ds
			}
			return out, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}

	// Modality Worklist is not in the default set, so it is named explicitly.
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID,
		ModalityWorklistInformationModelFindUID,
	})
	return server
}

// TestModalityWorklistQuery verifies a worklist query reaches its handler and
// its results reach the requestor.
//
// Every piece of this existed and none of it could be used: the WorklistHandler,
// the SOP class, and BasicWorklistPresentationContexts were all present, but
// SCU.Find chose its information model from a list of three Q/R models that did
// not include the worklist. A caller who negotiated the context correctly got
// "no accepted presentation context for C-FIND" — the one context the peer had
// accepted was the one Find would not use.
//
// Modality Worklist is what a modality queries before an examination, so the
// gap made the library unusable for the acquisition side of a department.
func TestModalityWorklistQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	server := worklistServer(t, ctx, 3)
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "MWL_SCU", CalledAE: "MWL_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, BasicWorklistPresentationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if _, ok := FindPresentationContextID(
		scu.Association().AcceptedContexts(), ModalityWorklistInformationModelFindUID); !ok {
		t.Fatal("the worklist context was not accepted, so the rest of this test proves nothing")
	}

	results, err := scu.Find(ctx, dataset.NewDataset())
	if err != nil {
		t.Fatalf("Find over a worklist context: %v", err)
	}

	received := 0
	for r := range results {
		if r.Err != nil {
			t.Fatalf("result: %v", r.Err)
		}
		if r.DataSet != nil {
			received++
		}
	}
	if received != 3 {
		t.Errorf("received %d worklist items, want 3", received)
	}
}

// TestFindWithSOPClassNamesTheModel verifies a caller can say which information
// model it means.
//
// Choosing by availability is a convenience for the common case. When a peer
// accepts several models it is the wrong basis for a decision: a Patient Root
// query and a worklist query answer different questions, and which one gets sent
// should not depend on the order of a list inside the library.
func TestFindWithSOPClassNamesTheModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var sawSOPClass string
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "MULTI_SCP", Port: 0, BindAddress: "127.0.0.1",
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

	// Both Patient Root and Study Root accepted, so availability cannot decide.
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID,
		PatientRootQueryRetrieveFind,
		StudyRootQueryRetrieveFind,
	})

	scu := NewSCU(SCUConfig{
		CallingAE: "MULTI_SCU", CalledAE: "MULTI_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// Ask for Study Root specifically, which is not what Find would have picked.
	results, err := scu.FindWithSOPClass(ctx, StudyRootQueryRetrieveFind, dataset.NewDataset())
	if err != nil {
		t.Fatalf("FindWithSOPClass: %v", err)
	}
	for r := range results {
		if r.Err != nil {
			t.Fatalf("result: %v", r.Err)
		}
	}

	if sawSOPClass != StudyRootQueryRetrieveFind {
		t.Errorf("the handler saw %q, want Study Root — the named model was not honored",
			sawSOPClass)
	}
}

// TestFindWithSOPClassRejectsAnUnnegotiatedModel verifies naming a model the
// peer did not accept is reported rather than silently substituted.
func TestFindWithSOPClassRejectsAnUnnegotiatedModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server := worklistServer(t, ctx, 1)
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "MWL_SCU", CalledAE: "MWL_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, BasicWorklistPresentationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// Only the worklist context was negotiated; asking for Patient Root must fail
	// rather than quietly sending a worklist query.
	if _, err := scu.FindWithSOPClass(ctx, PatientRootQueryRetrieveFind,
		dataset.NewDataset()); err == nil {
		t.Error("a model the peer never accepted was used anyway")
	}
}
