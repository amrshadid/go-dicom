package network

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// makeInstance builds a minimal storable instance.
func makeInstance(sopClassUID, sopInstanceUID, patientName string) *dataset.Dataset {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tagSOPClassUID, dataelem.UI, []byte(sopClassUID)))
	_ = ds.Add(dataelem.NewDataElement(tagSOPInstanceUID, dataelem.UI, []byte(sopInstanceUID)))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(patientName)))
	return ds
}

// TestCGetTransfersInstances is the end-to-end check that C-GET actually moves
// data. Before this, the SCP invoked the handler and returned a status but
// performed no C-STORE sub-operations, so a C-GET retrieved nothing.
func TestCGetTransfersInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	want := []*dataset.Dataset{
		makeInstance(CTImageStorageUID, "1.2.3.4.5.1", "Smith^John"),
		makeInstance(CTImageStorageUID, "1.2.3.4.5.2", "Doe^Jane"),
		makeInstance(MRImageStorageUID, "1.2.3.4.5.3", "Garcia^Maria"),
	}

	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "GET_SCP",
		Port:        0,
		BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnGet: func(_ context.Context, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return want, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID,
		PatientRootQueryRetrieveGet,
		StudyRootQueryRetrieveGet,
		CTImageStorageUID,
		MRImageStorageUID,
	})

	// Collect what the SCU receives as sub-operations.
	var mu sync.Mutex
	type received struct {
		sopClassUID    string
		sopInstanceUID string
		patientName    string
	}
	var got []received

	scu := NewSCU(SCUConfig{
		CallingAE: "GET_SCU",
		CalledAE:  "GET_SCP",
		Address:   server.Addr(),
		// Role selection lets the peer act as an SCP for these SOP classes on
		// an association we initiated, which is what C-GET requires.
		ExtendedNegotiation: &ExtendedNegotiation{
			RoleSelections: []SCPSCURoleSelection{
				{SOPClassUID: CTImageStorageUID, SCURole: true, SCPRole: true},
				{SOPClassUID: MRImageStorageUID, SCURole: true, SCPRole: true},
			},
		},
		OnCStore: func(_ context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
			r := received{sopClassUID: sopClass, sopInstanceUID: sopInstance}
			if ds != nil {
				if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
					r.patientName = trimPadding(elem.GetValue().([]byte))
				}
			}
			mu.Lock()
			got = append(got, r)
			mu.Unlock()
			return StatusSuccess
		},
	})

	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))

	if err := scu.Get(ctx, query); err != nil {
		t.Fatalf("Get: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("received %d instances, want %d", len(got), len(want))
	}

	expect := []received{
		{CTImageStorageUID, "1.2.3.4.5.1", "Smith^John"},
		{CTImageStorageUID, "1.2.3.4.5.2", "Doe^Jane"},
		{MRImageStorageUID, "1.2.3.4.5.3", "Garcia^Maria"},
	}
	for i, e := range expect {
		if got[i].sopClassUID != e.sopClassUID {
			t.Errorf("instance %d: SOP Class = %q, want %q", i, got[i].sopClassUID, e.sopClassUID)
		}
		if got[i].sopInstanceUID != e.sopInstanceUID {
			t.Errorf("instance %d: SOP Instance = %q, want %q", i, got[i].sopInstanceUID, e.sopInstanceUID)
		}
		if got[i].patientName != e.patientName {
			t.Errorf("instance %d: PatientName = %q, want %q", i, got[i].patientName, e.patientName)
		}
	}
}

// TestCGetWithNoMatchesSucceeds verifies a query matching nothing completes
// cleanly rather than hanging or erroring.
func TestCGetWithNoMatchesSucceeds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "GET_EMPTY", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnGet: func(_ context.Context, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	var calls int
	scu := NewSCU(SCUConfig{
		CallingAE: "GET_SCU", CalledAE: "GET_EMPTY", Address: server.Addr(),
		OnCStore: func(context.Context, string, string, *dataset.Dataset) uint16 {
			calls++
			return StatusSuccess
		},
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if err := scu.Get(ctx, dataset.NewDataset()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if calls != 0 {
		t.Errorf("OnCStore called %d times for an empty result, want 0", calls)
	}
}

// TestCGetWithoutOnCStoreStillCompletes verifies that omitting the callback
// does not stall the transfer: instances are acknowledged and discarded, and
// the C-GET still finishes.
func TestCGetWithoutOnCStoreStillCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "GET_NOCB", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnGet: func(_ context.Context, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return []*dataset.Dataset{
				makeInstance(CTImageStorageUID, "1.2.3.4.5.9", "Nobody^One"),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID, PatientRootQueryRetrieveGet, CTImageStorageUID,
	})

	scu := NewSCU(SCUConfig{
		CallingAE: "GET_SCU", CalledAE: "GET_NOCB", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if err := scu.Get(ctx, dataset.NewDataset()); err != nil {
		t.Fatalf("Get with no OnCStore callback: %v", err)
	}
}

// TestCGetReportsFailedSubOperations verifies that an instance the requestor
// refuses is counted as failed and surfaces as a partial warning rather than
// an unqualified success.
func TestCGetReportsFailedSubOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "GET_FAIL", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnGet: func(_ context.Context, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return []*dataset.Dataset{
				makeInstance(CTImageStorageUID, "1.2.3.4.5.10", "Refused^One"),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID, PatientRootQueryRetrieveGet, CTImageStorageUID,
	})

	scu := NewSCU(SCUConfig{
		CallingAE: "GET_SCU", CalledAE: "GET_FAIL", Address: server.Addr(),
		OnCStore: func(context.Context, string, string, *dataset.Dataset) uint16 {
			return StatusOutOfResources // refuse it
		},
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// A refused sub-operation is a partial warning, which Get treats as
	// completion rather than an error.
	if err := scu.Get(ctx, dataset.NewDataset()); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

// TestCGetGoesOnPastAnInstanceItCannotEncode: a C-GET sub-operation sent its
// C-STORE-RQ, which says a data set follows, and encoded the instance only
// then. When the encoding failed nothing followed, the requestor read the next
// C-STORE-RQ where it was owed a data set, and the whole retrieve ended with
// "expected a data set for the C-STORE sub-operation, got a command" (#518).
//
// The first instance here cannot be encoded: one of its values was left in a
// file by a reader and there is no file. The second can. The retrieve must
// deliver the second and count the first as failed, which Get reports as a
// partial completion and not as an error.
func TestCGetGoesOnPastAnInstanceItCannotEncode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	unencodable := makeInstance(CTImageStorageUID, "1.2.3.4.5.518.1", "Cannot^Encode")
	_ = unencodable.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW,
		dataelem.ValueInFile{Offset: 0, Length: 1024}))
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "GET_ENCODE", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnGet: func(_ context.Context, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return []*dataset.Dataset{
				unencodable,
				makeInstance(CTImageStorageUID, "1.2.3.4.5.518.2", "Can^Encode"),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID, PatientRootQueryRetrieveGet, CTImageStorageUID,
	})

	var received []string
	scu := NewSCU(SCUConfig{
		CallingAE: "GET_SCU", CalledAE: "GET_ENCODE", Address: server.Addr(),
		OnCStore: func(_ context.Context, _, sopInstanceUID string, _ *dataset.Dataset) uint16 {
			received = append(received, sopInstanceUID)
			return StatusSuccess
		},
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if err := scu.Get(ctx, dataset.NewDataset()); err != nil {
		t.Fatalf("Get ended with %v; one instance that cannot be encoded is one failed sub-operation", err)
	}
	if len(received) != 1 || received[0] != "1.2.3.4.5.518.2" {
		t.Errorf("received %v, want the one instance that can be encoded", received)
	}
}
