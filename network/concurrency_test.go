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

// The asynchronous operations window was once sent exactly as asked for with
// nothing enforcing it, and then clamped to one because nothing enforced it. It is
// now enforced, so the caller's number is proposed — with one exception.
func TestTheAsyncWindowIsProposedAsAsked(t *testing.T) {
	cases := []struct {
		name        string
		proposed    *AsynchronousOperationsWindow
		wantInvoked uint16
	}{
		{"a window of four is proposed, because it is now honored",
			&AsynchronousOperationsWindow{MaxOperationsInvoked: 4, MaxOperationsPerformed: 4}, 4},
		{"one stays one",
			&AsynchronousOperationsWindow{MaxOperationsInvoked: 1, MaxOperationsPerformed: 1}, 1},
		{
			// Zero means unlimited in PS3.7 D.3.3.3, and every outstanding operation
			// holds a goroutine waiting on a response, so unlimited is not a bound
			// this implementation can hold to.
			name:        "unlimited is refused, since it is not a bound we can keep",
			proposed:    &AsynchronousOperationsWindow{MaxOperationsInvoked: 0, MaxOperationsPerformed: 2},
			wantInvoked: 1,
		},
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
			if got.MaxOperationsPerformed != tc.proposed.MaxOperationsPerformed {
				t.Errorf("MaxOperationsPerformed = %d, want it unchanged at %d",
					got.MaxOperationsPerformed, tc.proposed.MaxOperationsPerformed)
			}
		})
	}

	if truthfulAsyncWindow(nil) != nil {
		t.Error("no window proposed should stay no window, not become one")
	}
}

// Proposing a window is a claim about behavior, so it has to bound something.
// This is the bound itself: N operations may hold a slot, and the next waits.
func TestTheWindowBoundsOutstandingOperations(t *testing.T) {
	const window = 2
	slots := newOperationSlots(window)
	ctx := context.Background()

	for i := 0; i < window; i++ {
		if err := slots.acquire(ctx); err != nil {
			t.Fatalf("acquiring slot %d of %d: %v", i+1, window, err)
		}
	}

	// The next one has to wait. A context that is already done proves it without
	// the test hanging if the bound is missing.
	expired, cancel := context.WithCancel(ctx)
	cancel()
	if err := slots.acquire(expired); err == nil {
		t.Errorf("a %drd operation acquired a slot with a window of %d", window+1, window)
	}

	// Releasing one lets the next in.
	slots.release()
	if err := slots.acquire(ctx); err != nil {
		t.Errorf("after releasing a slot, acquiring failed: %v", err)
	}
}

// A window of zero is unlimited in PS3.7 D.3.3.3, and newOperationSlots expresses
// that as no bound rather than a bound of zero — which would block every operation
// forever.
func TestAnUnlimitedWindowDoesNotBlock(t *testing.T) {
	slots := newOperationSlots(0)
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		if err := slots.acquire(ctx); err != nil {
			t.Fatalf("acquire %d with an unlimited window: %v", i, err)
		}
	}
	slots.release()
}

// The end-to-end counterpart: several operations issued concurrently on one SCU
// with a negotiated window all complete, and each gets its own response.
//
// This does not demonstrate throughput, and it is worth saying why rather than
// leaving a reader to assume it does. The SCP in this library dispatches one
// message at a time per association, so it answers serially however many the
// requestor has outstanding — the peak in flight at the handler is one. What this
// establishes is that the requestor's side is correct: eight operations sent from
// eight goroutines are matched to eight responses by message ID, with none
// delivered to the wrong caller.
//
// Concurrent dispatch on the SCP side, bounded by MaxOperationsPerformed, is the
// other half and is not implemented — see the follow-up noted in CONFORMANCE.md.
func TestConcurrentOperationsEachGetTheirOwnResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var mu sync.Mutex
	stored := make(map[string]int)

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "WINDOWSCP", Port: 0, BindAddress: "127.0.0.1",
	}, &StorageHandler{
		OnStore: func(_ context.Context, _, sopInstanceUID string, _ *dataset.Dataset) uint16 {
			mu.Lock()
			stored[sopInstanceUID]++
			mu.Unlock()
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID, CTImageStorageUID})

	scu := NewSCU(SCUConfig{
		CallingAE: "WINDOWSCU", CalledAE: "WINDOWSCP", Address: server.Addr(),
		ExtendedNegotiation: &ExtendedNegotiation{
			AsyncOperations: &AsynchronousOperationsWindow{
				MaxOperationsInvoked:   4,
				MaxOperationsPerformed: 4,
			},
		},
	})
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   CTImageStorageUID,
		TransferSyntaxes: DefaultTransferSyntaxes(),
	}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// The window has to have been agreed, or this measures the default of one.
	agreed := scu.Association().PeerUserInformation().AsyncOperations
	if agreed == nil || agreed.MaxOperationsInvoked != 4 {
		t.Fatalf("the peer did not agree a window of 4: %+v", agreed)
	}

	const total = 8
	var wg sync.WaitGroup
	errs := make(chan error, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			uid := fmt.Sprintf("1.2.3.%d", n)
			if err := scu.Store(ctx, makeInstance(CTImageStorageUID, uid, "Window^Test")); err != nil {
				errs <- fmt.Errorf("%s: %w", uid, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("a concurrent C-STORE failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(stored) != total {
		t.Errorf("the SCP stored %d distinct instances, want %d", len(stored), total)
	}
	for uid, count := range stored {
		if count != 1 {
			t.Errorf("%s arrived %d times, want once", uid, count)
		}
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
