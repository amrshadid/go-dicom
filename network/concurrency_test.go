package network

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// An SCU used to be documented as one-operation-at-a-time, with callers told to
// use one per goroutine. That works and costs an association per goroutine —
// and association setup is the expensive part of talking to a PACS: TCP, TLS
// handshake, then negotiation. A caller sending instances across eight workers
// paid for eight associations where one would do, and a PACS with an association
// limit may refuse the rest.
//
// Operations on one SCU are now serialized, so it can be shared. This drives eight
// goroutines at one SCU and requires every instance to arrive exactly once: two
// goroutines reading each other's responses would show up as a missing or
// duplicated instance, and under -race as a race.
func TestOneSCUSharedAcrossGoroutines(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		workers          = 8
		instancesPerWork = 4
	)

	var mu sync.Mutex
	received := make(map[string]int)

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "SHARESCP", Port: 0, BindAddress: "127.0.0.1",
	}, &StorageHandler{
		OnStore: func(_ context.Context, _, sopInstanceUID string, _ *dataset.Dataset) uint16 {
			mu.Lock()
			received[sopInstanceUID]++
			mu.Unlock()
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID, CTImageStorageUID,
	})

	// One SCU, one association, shared by every worker.
	scu := NewSCU(SCUConfig{
		CallingAE: "SHARESCU", CalledAE: "SHARESCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   CTImageStorageUID,
		TransferSyntaxes: DefaultTransferSyntaxes(),
	}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	var wg sync.WaitGroup
	errs := make(chan error, workers*instancesPerWork)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < instancesPerWork; i++ {
				uid := fmt.Sprintf("1.2.3.%d.%d", worker, i)
				ds := makeInstance(CTImageStorageUID, uid, "Shared^Test")
				if err := scu.Store(ctx, ds); err != nil {
					errs <- fmt.Errorf("worker %d instance %d: %w", worker, i, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("a concurrent C-STORE failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	want := workers * instancesPerWork
	if len(received) != want {
		t.Errorf("the SCP received %d distinct instances, want %d", len(received), want)
	}
	for uid, count := range received {
		if count != 1 {
			t.Errorf("instance %s arrived %d times, want once", uid, count)
		}
	}
}

// Cancel has to reach the peer while the operation it cancels is still running, so
// it must not wait behind the operation lock. A Cancel that blocked until the
// C-FIND finished would be useless — the point of a cancel is to stop work that
// has not happened yet.
func TestCancelIsNotBlockedByTheOperationInProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A streaming handler that blocks until its context is canceled, so the
	// C-FIND is genuinely in progress while Cancel is called.
	handler := &blockingFindHandler{started: make(chan struct{})}

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "CANCELSCP", Port: 0, BindAddress: "127.0.0.1",
	}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{StudyRootQueryRetrieveFind})

	scu := NewSCU(SCUConfig{
		CallingAE: "CANCELSCU", CalledAE: "CANCELSCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   StudyRootQueryRetrieveFind,
		TransferSyntaxes: DefaultTransferSyntaxes(),
	}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	messageID := scu.PeekNextMessageID()

	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))

	results, err := scu.FindWithSOPClass(ctx, StudyRootQueryRetrieveFind, query)
	if err != nil {
		t.Fatalf("FindWithSOPClass: %v", err)
	}

	// Wait until the handler is actually running before canceling.
	select {
	case <-handler.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the C-FIND handler never started")
	}

	// This is the assertion: Cancel returns rather than deadlocking behind the
	// C-FIND that still holds the operation lock.
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- scu.Cancel(ctx, messageID) }()

	select {
	case err := <-cancelDone:
		if err != nil {
			t.Errorf("Cancel returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Cancel blocked behind the operation it was canceling")
	}

	// Drain, so the streaming goroutine finishes and releases the lock.
	for range results { //nolint:revive // draining
	}
}

// blockingFindHandler streams nothing until its context is canceled.
type blockingFindHandler struct {
	BaseHandler
	once    sync.Once
	started chan struct{}
}

func (h *blockingFindHandler) HandleCFind(context.Context, *CFindRequest) ([]*CFindResponse, error) {
	return nil, nil
}

func (h *blockingFindHandler) StreamCFind(ctx context.Context, _ *CFindRequest,
	_ chan<- *CFindResponse) error {
	h.once.Do(func() { close(h.started) })
	<-ctx.Done()
	return ctx.Err()
}

// The asynchronous operations window used to be sent exactly as asked for, with
// nothing enforcing it: a caller requesting four had four negotiated and reported
// back while the SCU issued one operation at a time. The peer was told something
// about us that was not true.
func TestTheAsyncWindowIsReducedToWhatIsPerformed(t *testing.T) {
	cases := []struct {
		name        string
		proposed    *AsynchronousOperationsWindow
		wantInvoked uint16
	}{
		{"a window of four is reduced to one",
			&AsynchronousOperationsWindow{MaxOperationsInvoked: 4, MaxOperationsPerformed: 4}, 1},
		{"unlimited is reduced too, being furthest from the truth",
			&AsynchronousOperationsWindow{MaxOperationsInvoked: 0, MaxOperationsPerformed: 2}, 1},
		{"one is left alone",
			&AsynchronousOperationsWindow{MaxOperationsInvoked: 1, MaxOperationsPerformed: 1}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truthfulAsyncWindow(tc.proposed)
			if got == nil {
				t.Fatal("truthfulAsyncWindow dropped the window entirely")
			}
			if got.MaxOperationsInvoked != tc.wantInvoked {
				t.Errorf("MaxOperationsInvoked = %d, want %d",
					got.MaxOperationsInvoked, tc.wantInvoked)
			}

			// What we will accept from the peer is left as asked: it bounds what the
			// peer sends us, and reading one message at a time is not made
			// incorrect by the peer sending fewer than it could.
			if got.MaxOperationsPerformed != tc.proposed.MaxOperationsPerformed {
				t.Errorf("MaxOperationsPerformed = %d, want it unchanged at %d",
					got.MaxOperationsPerformed, tc.proposed.MaxOperationsPerformed)
			}
		})
	}

	if truthfulAsyncWindow(nil) != nil {
		t.Error("no window proposed should stay no window, not become a clamped one")
	}
}

// The caller's configuration must survive being used: an SCUConfig reused for a
// second association would otherwise carry a window silently rewritten by the
// first.
func TestTheCallersWindowIsNotModifiedInPlace(t *testing.T) {
	proposed := &AsynchronousOperationsWindow{MaxOperationsInvoked: 4, MaxOperationsPerformed: 4}

	_ = truthfulAsyncWindow(proposed)

	if proposed.MaxOperationsInvoked != 4 {
		t.Errorf("the caller's window was rewritten to %d; it should be copied, not modified",
			proposed.MaxOperationsInvoked)
	}
}
