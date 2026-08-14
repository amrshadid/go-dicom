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

// sliceGetHandler answers a C-GET by returning every match at once.
//
// The instances come from the embedded QueryRetrieveHandler's OnGet closure rather
// than a field here, so that this goes through exactly the path an ordinary caller
// uses.
type sliceGetHandler struct {
	QueryRetrieveHandler
}

// streamGetHandler answers the same C-GET one instance at a time, through the
// streaming interface.
type streamGetHandler struct {
	BaseHandler
	instances []*dataset.Dataset
}

func (h *streamGetHandler) HandleCGet(context.Context, *CGetRequest) (*CGetResponse, error) {
	// Never reached: implementing CGetStreamer takes precedence. Present so the
	// type satisfies Handler the same way the slice one does.
	return &CGetResponse{Status: StatusSuccess}, nil
}

func (h *streamGetHandler) CountCGetMatches(context.Context, *CGetRequest) (int, error) {
	return len(h.instances), nil
}

func (h *streamGetHandler) StreamCGet(ctx context.Context, _ *CGetRequest,
	out chan<- *dataset.Dataset) error {
	for _, inst := range h.instances {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- inst:
		}
	}
	return nil
}

// C-GET has two sub-operation loops — one for a handler returning a slice, one
// for a handler that streams — and until they were factored onto a shared body
// they were two copies of the same work, with eight byte-identical log messages
// between them.
//
// That is the hazard this project keeps meeting in other forms: the RLE encoder
// only its own decoder could read, the N-SET whose own SCP read back the same
// wrong tag. A correctness fix applied to the slice path and not the streaming one
// leaves a defect that only appears for handlers implementing CGetStreamer, which
// is the less-traveled path and so the less likely to be noticed.
//
// So this drives the same retrieval down both and requires the same answer.
func TestBothCGetPathsAgree(t *testing.T) {
	instances := []*dataset.Dataset{
		makeInstance(CTImageStorageUID, "1.2.3.4.5.1", "Smith^John"),
		makeInstance(CTImageStorageUID, "1.2.3.4.5.2", "Doe^Jane"),
		makeInstance(MRImageStorageUID, "1.2.3.4.5.3", "Garcia^Maria"),
	}

	slice := retrieveViaCGet(t, &sliceGetHandler{
		QueryRetrieveHandler: QueryRetrieveHandler{
			OnGet: func(context.Context, string, *dataset.Dataset) ([]*dataset.Dataset, error) {
				return instances, nil
			},
		},
	})
	streamed := retrieveViaCGet(t, &streamGetHandler{instances: instances})

	if slice.err != nil {
		t.Errorf("the slice path failed: %v", slice.err)
	}
	if streamed.err != nil {
		t.Errorf("the streaming path failed: %v", streamed.err)
	}

	if len(slice.uids) != len(instances) {
		t.Errorf("the slice path delivered %d instances, want %d", len(slice.uids), len(instances))
	}
	if len(slice.uids) != len(streamed.uids) {
		t.Fatalf("the two paths delivered different counts: slice %d, streaming %d",
			len(slice.uids), len(streamed.uids))
	}

	// Same instances, in the same order. Order matters: a requestor writing them
	// to disk as they arrive has no other way to reassemble a series.
	for i := range slice.uids {
		if slice.uids[i] != streamed.uids[i] {
			t.Errorf("instance %d: slice path delivered %q, streaming path %q",
				i, slice.uids[i], streamed.uids[i])
		}
	}
}

// A handler returning a nil instance is a mistake, and it has to be counted as
// failed rather than skipped, so the numbers the requestor is given add up to what
// it was told to expect.
//
// Tested against the shared helper rather than through an association, because the
// count is not observable from the requestor's side: scu.Get returns an error or
// nil, and the sub-operation counts travel in the C-GET-RSP that the SCU does not
// surface. An integration test would pass whether the nil were counted or skipped —
// I checked, and both paths deliver the same two instances with no error either
// way. Since both loops now call this one function, parity follows from it.
func TestANilInstanceCountsAsFailed(t *testing.T) {
	var counts subOperationCounts

	// A nil instance is rejected before the association is touched, so there is no
	// need for a real one here — and passing nil proves it is not touched.
	s := &SCP{}
	s.sendOneGetSubOperation(context.Background(), nil, nil, 1, 1, &cancelFlag{}, &counts)

	if counts.failed != 1 {
		t.Errorf("a nil instance produced failed=%d, want 1", counts.failed)
	}
	if counts.completed != 0 {
		t.Errorf("a nil instance produced completed=%d, want 0", counts.completed)
	}
}

// An instance with no SOP Class or Instance UID cannot be sent: the C-STORE names
// what it carries with those, and there is no presentation context to choose
// without the class. Counted as failed for the same reason as a nil.
func TestAnInstanceWithoutUIDsCountsAsFailed(t *testing.T) {
	var counts subOperationCounts

	s := &SCP{}
	s.sendOneGetSubOperation(context.Background(), nil, dataset.NewDataset(), 1, 1,
		&cancelFlag{}, &counts)

	if counts.failed != 1 {
		t.Errorf("an instance without UIDs produced failed=%d, want 1", counts.failed)
	}
}

// remainingAfter is uint16 arithmetic on two ints, and a handler that streams more
// instances than it counted would underflow it — reporting nearly 65535
// sub-operations still outstanding at the end of a retrieval that had finished.
func TestRemainingAfterDoesNotUnderflow(t *testing.T) {
	cases := []struct {
		total, sent int
		want        uint16
	}{
		{10, 0, 10},
		{10, 4, 6},
		{10, 10, 0},
		{10, 11, 0}, // more sent than counted: the case that used to underflow
		{0, 5, 0},
		{0, 0, 0},
	}

	for _, tc := range cases {
		if got := remainingAfter(tc.total, tc.sent); got != tc.want {
			t.Errorf("remainingAfter(%d, %d) = %d, want %d", tc.total, tc.sent, got, tc.want)
		}
	}
}

// retrieveResult is what one C-GET delivered.
type retrieveResult struct {
	uids []string
	err  error
}

// retrieveViaCGet runs a STUDY level C-GET against handler and reports what the
// requestor received.
func retrieveViaCGet(t *testing.T, handler Handler) retrieveResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "GET_SCP",
		Port:        0,
		BindAddress: "127.0.0.1",
	}, handler)
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

	var mu sync.Mutex
	var uids []string

	scu := NewSCU(SCUConfig{
		CallingAE: "GET_SCU",
		CalledAE:  "GET_SCP",
		Address:   server.Addr(),
		ExtendedNegotiation: &ExtendedNegotiation{
			RoleSelections: []SCPSCURoleSelection{
				{SOPClassUID: CTImageStorageUID, SCURole: true, SCPRole: true},
				{SOPClassUID: MRImageStorageUID, SCURole: true, SCPRole: true},
			},
		},
		OnCStore: func(_ context.Context, _, sopInstance string, _ *dataset.Dataset) uint16 {
			mu.Lock()
			uids = append(uids, sopInstance)
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

	getErr := scu.Get(ctx, query)

	mu.Lock()
	defer mu.Unlock()
	return retrieveResult{uids: append([]string(nil), uids...), err: getErr}
}
