package network

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// streamingFindHandler is a C-FIND handler that emits matches one at a time and
// records how far it got, so a test can tell whether cancellation reached it.
type streamingFindHandler struct {
	BaseHandler

	total    int
	emitted  atomic.Int32
	observed atomic.Bool // set if the handler saw its context canceled

	// gate, when non-nil, is waited on before each match after the first, so a
	// test can cancel at a known point instead of racing the handler.
	gate chan struct{}
}

func (h *streamingFindHandler) StreamCFind(ctx context.Context, _ *CFindRequest, out chan<- *CFindResponse) error {
	for i := 0; i < h.total; i++ {
		if i > 0 && h.gate != nil {
			select {
			case <-h.gate:
			case <-ctx.Done():
				h.observed.Store(true)
				return ctx.Err()
			}
		}

		select {
		case <-ctx.Done():
			h.observed.Store(true)
			return ctx.Err()
		default:
		}

		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("ID-000000")))

		select {
		case out <- &CFindResponse{DataSet: ds}:
			h.emitted.Add(1)
		case <-ctx.Done():
			h.observed.Store(true)
			return ctx.Err()
		}
	}
	return nil
}

// TestStreamingCFindDeliversMatches verifies the streaming path works at all
// before any cancellation is involved.
func TestStreamingCFindDeliversMatches(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	handler := &streamingFindHandler{total: 5}
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "STREAM_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "STREAM_SCU", CalledAE: "STREAM_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	results, err := scu.Find(ctx, dataset.NewDataset())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	received := 0
	for r := range results {
		if r.Err != nil {
			t.Fatalf("Find result: %v", r.Err)
		}
		if r.DataSet != nil {
			received++
		}
	}

	if received != 5 {
		t.Errorf("received %d matches, want 5", received)
	}
	if got := handler.emitted.Load(); got != 5 {
		t.Errorf("handler emitted %d, want 5", got)
	}
}

// TestStreamingCFindHandlerObservesCancel is the point of the whole mechanism:
// a C-CANCEL must reach the handler so it stops doing work nobody wants.
//
// Before this, a C-FIND handler returned every match as a slice, so the SCP had
// nothing to interrupt — the query ran to completion over however large an
// archive it was given, and the cancel could at best stop the SCP from sending
// results it had already computed.
func TestStreamingCFindHandlerObservesCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// A very large result set, so the test cannot pass by the handler finishing.
	handler := &streamingFindHandler{total: 100000, gate: make(chan struct{})}

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "CANCEL_STREAM", Port: 0, BindAddress: "127.0.0.1",
	}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "STREAM_SCU", CalledAE: "CANCEL_STREAM", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	messageID := scu.PeekNextMessageID()
	results, err := scu.Find(ctx, dataset.NewDataset())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	// Wait for the first match, so the operation is definitely under way.
	first := <-results
	if first.Err != nil {
		t.Fatalf("first result: %v", first.Err)
	}

	if err := scu.Cancel(ctx, messageID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// Release the gate so the handler runs again and can notice the cancel.
	close(handler.gate)

	// Drain so the SCU's read loop finishes.
	for range results {
	}

	deadline := time.Now().Add(5 * time.Second)
	for !handler.observed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !handler.observed.Load() {
		t.Error("the handler never saw its context canceled, so a C-CANCEL cannot stop a long query")
	}
	if got := handler.emitted.Load(); got >= int32(handler.total) {
		t.Errorf("handler emitted %d of %d matches — it ran to completion despite the cancel",
			got, handler.total)
	}
}

// TestSliceCFindHandlerStillWorks guards the non-streaming path, which is what
// every existing handler uses. Making the streaming interface optional is only
// safe if the old path is untouched.
func TestSliceCFindHandlerStillWorks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "SLICE_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnFind: func(context.Context, string, *dataset.Dataset) ([]*dataset.Dataset, error) {
			out := make([]*dataset.Dataset, 3)
			for i := range out {
				ds := dataset.NewDataset()
				_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("ID-000000")))
				out[i] = ds
			}
			return out, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "SLICE_SCU", CalledAE: "SLICE_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	results, err := scu.Find(ctx, dataset.NewDataset())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	received := 0
	for r := range results {
		if r.Err != nil {
			t.Fatalf("Find result: %v", r.Err)
		}
		if r.DataSet != nil {
			received++
		}
	}
	if received != 3 {
		t.Errorf("received %d matches from a slice handler, want 3", received)
	}
}

// TestAssociationPushBackPreservesOrder covers the read-ahead queue the cancel
// watcher relies on. A watcher must consume a message to learn whether it is
// the cancel it wants, and a message it did not want has to reach whoever reads
// next — in order, or the association desynchronizes.
func TestAssociationPushBackPreservesOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "PUSHBACK", Port: 0, BindAddress: "127.0.0.1",
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "PB_SCU", CalledAE: "PUSHBACK", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	assoc := scu.Association()
	assoc.PushBack(1, []byte("first"), true)
	assoc.PushBack(3, []byte("second"), false)

	for i, want := range []struct {
		ctxID byte
		data  string
		isCmd bool
	}{
		{1, "first", true},
		{3, "second", false},
	} {
		ctxID, data, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if ctxID != want.ctxID || string(data) != want.data || isCmd != want.isCmd {
			t.Errorf("read %d = (%d, %q, %v), want (%d, %q, %v)",
				i, ctxID, data, isCmd, want.ctxID, want.data, want.isCmd)
		}
	}
}
