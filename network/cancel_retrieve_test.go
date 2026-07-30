package network_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A C-CANCEL during a retrieval used to be ignored outright. C-FIND observed one
// through CFindStreamer, but C-GET and C-MOVE never watched for it, so a cancel
// sent after three instances of sixty still delivered all sixty. The conformance
// statement described this as a missing hook for the matching phase, which was
// milder than the truth: the sub-operations were not interruptible either.
//
// Cancellation is worth testing at the boundary rather than by unit-testing the
// watcher, because the failure was not in the watcher — it was correct, and had
// exactly one call site.

const cancelAfter = 3

// retrieveInstances builds n distinct CT instances.
func retrieveInstances(n int) []*dataset.Dataset {
	instances := make([]*dataset.Dataset, n)
	for i := range instances {
		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI,
			[]byte(network.CTImageStorageUID)))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI,
			[]byte(fmt.Sprintf("1.2.826.0.1.3680043.10.511.7.%d\x00", i))))
		instances[i] = ds
	}
	return instances
}

// TestCGetStopsOnCancel checks that a C-CANCEL abandons the remaining
// sub-operations rather than running the retrieval to completion.
//
// C-GET is the harder of the two: its sub-operations travel on the same
// association as the request, so the C-STORE-RSP read is where a C-CANCEL
// arrives. Before this, it was consumed there and read as a response.
func TestCGetStopsOnCancel(t *testing.T) {
	const total = 60
	var stored atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "CANCEL_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, &network.QueryRetrieveHandler{
		OnGet: func(_ context.Context, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return retrieveInstances(total), nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	var scu *network.SCU
	var getMessageID atomic.Uint32
	scu = network.NewSCU(network.SCUConfig{
		CallingAE: "CANCEL_SCU", CalledAE: "CANCEL_SCP", Address: server.Addr(),
		OnCStore: func(_ context.Context, _, _ string, _ *dataset.Dataset) uint16 {
			if stored.Add(1) == cancelAfter {
				// Cancel from the store callback: the SCU is mid-retrieval here,
				// which is when a user would press stop.
				go func() { _ = scu.Cancel(context.Background(), uint16(getMessageID.Load())) }()
			}
			return network.StatusSuccess
		},
	})

	if err := scu.Associate(ctx, []network.PresentationContextItem{
		{ID: 1, AbstractSyntax: network.PatientRootQueryRetrieveGet,
			TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID}},
		{ID: 3, AbstractSyntax: network.CTImageStorageUID,
			TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	getMessageID.Store(uint32(scu.PeekNextMessageID()))

	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))
	err = scu.Get(ctx, query)
	if err == nil {
		t.Fatal("a canceled C-GET reported success; the caller cannot tell it was cut short")
	}
	if !network.IsCanceled(err) {
		t.Fatalf("C-GET failed for a reason other than the cancel: %v", err)
	}

	delivered := stored.Load()
	if delivered >= total {
		t.Fatalf("C-CANCEL was ignored: all %d instances were delivered after a cancel at %d",
			total, cancelAfter)
	}
	t.Logf("cancel at %d of %d stopped the retrieval at %d", cancelAfter, total, delivered)
}

// TestCMoveStopsOnCancel is the same check for C-MOVE, whose sub-operations go
// to a third party. The requestor's association is idle while they run, so the
// cancel arrives on a connection nobody is reading.
func TestCMoveStopsOnCancel(t *testing.T) {
	const total = 60
	var stored atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	// The move destination: a plain storage SCP counting what arrives.
	dest, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "MOVE_DEST", Port: 0, BindAddress: "127.0.0.1",
	}, &network.StorageHandler{
		OnStore: func(_ context.Context, _, _ string, _ *dataset.Dataset) uint16 {
			stored.Add(1)
			return network.StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("start destination: %v", err)
	}
	defer dest.Stop()

	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "CANCEL_SCP", Port: 0, BindAddress: "127.0.0.1",
		MoveDestinations: map[string]string{"MOVE_DEST": dest.Addr()},
	}, &network.QueryRetrieveHandler{
		OnMoveInstances: func(_ context.Context, _, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return retrieveInstances(total), nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "CANCEL_SCU", CalledAE: "CANCEL_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{
		{ID: 1, AbstractSyntax: network.PatientRootQueryRetrieveMove,
			TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	messageID := scu.PeekNextMessageID()

	// Cancel once some instances have reached the destination.
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if stored.Load() >= cancelAfter {
				_ = scu.Cancel(context.Background(), messageID)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))
	err = scu.Move(ctx, query, "MOVE_DEST")
	if err == nil {
		t.Fatal("a canceled C-MOVE reported success; the caller cannot tell it was cut short")
	}
	if !network.IsCanceled(err) {
		t.Fatalf("C-MOVE failed for a reason other than the cancel: %v", err)
	}

	delivered := stored.Load()
	if delivered >= total {
		t.Fatalf("C-CANCEL was ignored: all %d instances were moved after a cancel at %d",
			total, cancelAfter)
	}
	t.Logf("cancel at %d of %d stopped the move at %d", cancelAfter, total, delivered)
}

// streamingGetHandler produces instances lazily and records how many it was
// asked for before being stopped.
type streamingGetHandler struct {
	network.BaseHandler
	total    int
	produced atomic.Int32
	stopped  atomic.Bool
}

func (h *streamingGetHandler) CountCGetMatches(_ context.Context, _ *network.CGetRequest) (int, error) {
	return h.total, nil
}

func (h *streamingGetHandler) StreamCGet(ctx context.Context, _ *network.CGetRequest,
	out chan<- *dataset.Dataset) error {

	for _, inst := range retrieveInstances(h.total) {
		select {
		case <-ctx.Done():
			// The point of the interface: matching stops when the requestor does.
			h.stopped.Store(true)
			return ctx.Err()
		case out <- inst:
			h.produced.Add(1)
		}
	}
	return nil
}

// TestCGetStreamerStopsMatchingOnCancel covers the gap the conformance statement
// actually described: a handler that is still finding matches when the requestor
// cancels should be told to stop, rather than finishing a search whose results
// are discarded.
func TestCGetStreamerStopsMatchingOnCancel(t *testing.T) {
	const total = 200
	var stored atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	handler := &streamingGetHandler{total: total}
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "STREAM_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	var scu *network.SCU
	var getMessageID atomic.Uint32
	scu = network.NewSCU(network.SCUConfig{
		CallingAE: "STREAM_SCU", CalledAE: "STREAM_SCP", Address: server.Addr(),
		OnCStore: func(_ context.Context, _, _ string, _ *dataset.Dataset) uint16 {
			if stored.Add(1) == cancelAfter {
				go func() { _ = scu.Cancel(context.Background(), uint16(getMessageID.Load())) }()
			}
			return network.StatusSuccess
		},
	})

	if err := scu.Associate(ctx, []network.PresentationContextItem{
		{ID: 1, AbstractSyntax: network.PatientRootQueryRetrieveGet,
			TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID}},
		{ID: 3, AbstractSyntax: network.CTImageStorageUID,
			TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	getMessageID.Store(uint32(scu.PeekNextMessageID()))

	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))
	err = scu.Get(ctx, query)
	if err == nil || !network.IsCanceled(err) {
		t.Fatalf("canceled streaming C-GET reported %v, want a cancel", err)
	}

	if produced := handler.produced.Load(); produced >= int32(total) {
		t.Errorf("the handler produced all %d matches; canceling did not reach the matching phase", total)
	} else {
		t.Logf("handler produced %d of %d matches before stopping; %d instances stored",
			produced, total, stored.Load())
	}
	if !handler.stopped.Load() {
		t.Error("the handler's context was never canceled, so a real archive would have kept searching")
	}
}
