package network_test

import (
	"context"
	"sync"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// The Transaction UID is the whole reason UPS has a state machine: it is issued
// when a step goes IN PROGRESS and required on every update afterwards, so that
// two performers cannot both claim the same work.
//
// Getting it wrong produces no protocol error. Both peers get success responses
// and both believe they own the step, which is why the transition table is
// tested case by case rather than through a happy path.

// memoryUPSStore is a UPSStore over a map.
type memoryUPSStore struct {
	mu    sync.Mutex
	steps map[string]*network.UPSStep
}

func newMemoryUPSStore() *memoryUPSStore {
	return &memoryUPSStore{steps: map[string]*network.UPSStep{}}
}

func (s *memoryUPSStore) FindUPS(_ context.Context, uid string) (*network.UPSStep, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	step, ok := s.steps[uid]
	return step, ok, nil
}

func (s *memoryUPSStore) CreateUPS(_ context.Context, step *network.UPSStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps[step.SOPInstanceUID] = step
	return nil
}

func (s *memoryUPSStore) UpdateUPS(_ context.Context, step *network.UPSStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps[step.SOPInstanceUID] = step
	return nil
}

const (
	upsInstance = "1.2.826.0.1.3680043.10.511.4.1"
	upsTxn      = "1.2.826.0.1.3680043.10.511.4.99"
	upsOtherTxn = "1.2.826.0.1.3680043.10.511.4.98"
)

// upsDataSet builds an action or create data set.
func upsDataSet(state network.UPSState, transactionUID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	if state != "" {
		value := string(state)
		if len(value)%2 == 1 {
			value += " "
		}
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0074, 0x1000), dataelem.CS, []byte(value)))
	}
	if transactionUID != "" {
		v := transactionUID
		if len(v)%2 == 1 {
			v += "\x00"
		}
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1195), dataelem.UI, []byte(v)))
	}
	return ds
}

// newScheduledStep creates a step and returns the handler serving it.
func newScheduledStep(t *testing.T) (*network.UPSHandler, *memoryUPSStore) {
	t.Helper()

	store := newMemoryUPSStore()
	h := &network.UPSHandler{Store: store}

	create := upsDataSet(network.UPSScheduled, "")
	_ = create.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("CT")))
	resp, err := h.HandleNCreate(context.Background(), &network.NCreateRequest{
		AffectedSOPInstance: upsInstance,
		DataSet:             create,
	})
	if err != nil {
		t.Fatalf("N-CREATE: %v", err)
	}
	if resp.Status != network.StatusSuccess {
		t.Fatalf("N-CREATE status 0x%04X, want success", resp.Status)
	}
	return h, store
}

// changeState issues an N-ACTION type 1.
func changeState(t *testing.T, h *network.UPSHandler, to network.UPSState, txn string) uint16 {
	t.Helper()
	resp, err := h.HandleNAction(context.Background(), &network.NActionRequest{
		RequestedSOPInstance: upsInstance,
		ActionTypeID:         network.UPSActionChangeState,
		DataSet:              upsDataSet(to, txn),
	})
	if err != nil {
		t.Fatalf("N-ACTION: %v", err)
	}
	return resp.Status
}

// TestUPSTransactionUIDLocksTheStep is the case that matters most: once a step
// is claimed, nobody else can finish or cancel it.
func TestUPSTransactionUIDLocksTheStep(t *testing.T) {
	h, store := newScheduledStep(t)

	if got := changeState(t, h, network.UPSInProgress, upsTxn); got != network.StatusSuccess {
		t.Fatalf("starting the step gave 0x%04X", got)
	}

	// A second performer, with its own Transaction UID.
	if got := changeState(t, h, network.UPSCompleted, upsOtherTxn); got != network.StatusUPSTransactionUIDMissing {
		t.Errorf("another performer completed the step with status 0x%04X; "+
			"want 0xC301, the Transaction UID refusal", got)
	}
	// And one presenting none at all.
	if got := changeState(t, h, network.UPSCanceled, ""); got != network.StatusUPSTransactionUIDMissing {
		t.Errorf("a request with no Transaction UID gave 0x%04X, want 0xC301", got)
	}

	step, _, _ := store.FindUPS(context.Background(), upsInstance)
	if step.State != network.UPSInProgress {
		t.Errorf("the step is %q; a refused transition must not have applied", step.State)
	}

	// The holder can finish it.
	if got := changeState(t, h, network.UPSCompleted, upsTxn); got != network.StatusSuccess {
		t.Errorf("the Transaction UID holder could not complete the step: 0x%04X", got)
	}
}

// TestUPSTransitions walks the transition table case by case.
func TestUPSTransitions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		start    network.UPSState
		startTxn string
		to       network.UPSState
		txn      string
		want     uint16
	}{
		{"scheduled to in progress", network.UPSScheduled, "", network.UPSInProgress, upsTxn, network.StatusSuccess},
		{"scheduled to in progress without a UID", network.UPSScheduled, "", network.UPSInProgress, "", network.StatusUPSTransactionUIDMissing},
		{"scheduled straight to completed", network.UPSScheduled, "", network.UPSCompleted, upsTxn, network.StatusUPSNotInProgress},
		{"scheduled straight to canceled", network.UPSScheduled, "", network.UPSCanceled, upsTxn, network.StatusUPSNotInProgress},
		{"in progress to completed", network.UPSInProgress, upsTxn, network.UPSCompleted, upsTxn, network.StatusSuccess},
		{"in progress to canceled", network.UPSInProgress, upsTxn, network.UPSCanceled, upsTxn, network.StatusSuccess},
		{"in progress started twice", network.UPSInProgress, upsTxn, network.UPSInProgress, upsTxn, network.StatusUPSAlreadyInProgress},
		{"completed to completed", network.UPSCompleted, "", network.UPSCompleted, upsTxn, network.StatusUPSAlreadyInStateCompleted},
		{"completed to canceled", network.UPSCompleted, "", network.UPSCanceled, upsTxn, network.StatusUPSAlreadyCompleted},
		{"canceled to canceled", network.UPSCanceled, "", network.UPSCanceled, upsTxn, network.StatusUPSAlreadyCanceled},
		{"canceled to completed", network.UPSCanceled, "", network.UPSCompleted, upsTxn, network.StatusUPSNotUpdatable},
		{"back to scheduled", network.UPSInProgress, upsTxn, network.UPSScheduled, upsTxn, network.StatusUPSMayOnlyBeScheduledByCreate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryUPSStore()
			store.steps[upsInstance] = &network.UPSStep{
				SOPInstanceUID: upsInstance,
				State:          tc.start,
				TransactionUID: tc.startTxn,
				Attributes:     upsDataSet(tc.start, ""),
			}
			h := &network.UPSHandler{Store: store}

			if got := changeState(t, h, tc.to, tc.txn); got != tc.want {
				t.Errorf("%s -> %s gave 0x%04X, want 0x%04X", tc.start, tc.to, got, tc.want)
			}
		})
	}
}

// TestUPSSetRequiresInProgressAndTheUID covers N-SET's two preconditions.
func TestUPSSetRequiresInProgressAndTheUID(t *testing.T) {
	h, _ := newScheduledStep(t)
	ctx := context.Background()

	update := func(txn string) uint16 {
		ds := upsDataSet("", txn)
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0040, 0x4051), dataelem.DT,
			[]byte("20260730120000")))
		resp, err := h.HandleNSet(ctx, &network.NSetRequest{
			RequestedSOPInstance: upsInstance, DataSet: ds,
		})
		if err != nil {
			t.Fatalf("N-SET: %v", err)
		}
		return resp.Status
	}

	// While SCHEDULED there is nothing to update yet.
	if got := update(upsTxn); got != network.StatusUPSNotInProgress {
		t.Errorf("N-SET on a scheduled step gave 0x%04X, want 0xC310", got)
	}

	if got := changeState(t, h, network.UPSInProgress, upsTxn); got != network.StatusSuccess {
		t.Fatalf("starting the step gave 0x%04X", got)
	}

	if got := update(upsOtherTxn); got != network.StatusUPSTransactionUIDMissing {
		t.Errorf("N-SET with the wrong Transaction UID gave 0x%04X, want 0xC301", got)
	}
	if got := update(upsTxn); got != network.StatusSuccess {
		t.Errorf("N-SET by the holder gave 0x%04X", got)
	}

	// Finished, so no longer updatable at all.
	if got := changeState(t, h, network.UPSCompleted, upsTxn); got != network.StatusSuccess {
		t.Fatalf("completing gave 0x%04X", got)
	}
	if got := update(upsTxn); got != network.StatusUPSNotUpdatable {
		t.Errorf("N-SET on a completed step gave 0x%04X, want 0xC300", got)
	}
}

// TestUPSSetCannotChangeState checks the state is not settable through N-SET,
// which would be a second route to a transition with different rules attached.
func TestUPSSetCannotChangeState(t *testing.T) {
	h, store := newScheduledStep(t)
	if got := changeState(t, h, network.UPSInProgress, upsTxn); got != network.StatusSuccess {
		t.Fatalf("starting the step gave 0x%04X", got)
	}

	resp, err := h.HandleNSet(context.Background(), &network.NSetRequest{
		RequestedSOPInstance: upsInstance,
		DataSet:              upsDataSet(network.UPSCompleted, upsTxn),
	})
	if err != nil {
		t.Fatalf("N-SET: %v", err)
	}
	if resp.Status != network.StatusUPSMayOnlyBeScheduledByCreate {
		t.Errorf("N-SET carrying a state gave 0x%04X, want it refused", resp.Status)
	}

	step, _, _ := store.FindUPS(context.Background(), upsInstance)
	if step.State != network.UPSInProgress {
		t.Errorf("the step became %q through N-SET", step.State)
	}
}

// TestUPSCancelIsARequestNotATransition checks that asking for cancellation does
// not cancel anything: the performer owns the step and decides.
func TestUPSCancelIsARequestNotATransition(t *testing.T) {
	h, store := newScheduledStep(t)
	if got := changeState(t, h, network.UPSInProgress, upsTxn); got != network.StatusSuccess {
		t.Fatalf("starting the step gave 0x%04X", got)
	}

	var asked bool
	h.OnCancelRequested = func(_ context.Context, _ *network.UPSStep) uint16 {
		asked = true
		return 0
	}

	resp, err := h.HandleNAction(context.Background(), &network.NActionRequest{
		RequestedSOPInstance: upsInstance,
		ActionTypeID:         network.UPSActionRequestCancel,
	})
	if err != nil {
		t.Fatalf("N-ACTION: %v", err)
	}
	if resp.Status != network.StatusSuccess {
		t.Errorf("the cancellation request gave 0x%04X", resp.Status)
	}
	if !asked {
		t.Error("the performer was never asked")
	}

	step, _, _ := store.FindUPS(context.Background(), upsInstance)
	if step.State != network.UPSInProgress {
		t.Errorf("the step is %q; a cancellation request must not change the state itself", step.State)
	}
}

// TestUPSSubscriptionIsRefusedNotIgnored checks the Watch actions are answered.
//
// Accepting a subscription this SCP never honors would leave the requestor
// waiting for event reports that cannot arrive, which is worse than a refusal it
// can act on.
func TestUPSSubscriptionIsRefusedNotIgnored(t *testing.T) {
	h, _ := newScheduledStep(t)

	for _, action := range []uint16{
		network.UPSActionSubscribe,
		network.UPSActionUnsubscribe,
		network.UPSActionSuspendGlobal,
	} {
		resp, err := h.HandleNAction(context.Background(), &network.NActionRequest{
			RequestedSOPInstance: upsInstance,
			ActionTypeID:         action,
		})
		if err != nil {
			t.Fatalf("N-ACTION %d: %v", action, err)
		}
		if resp.Status != network.StatusUPSEventReportsNotSupported {
			t.Errorf("action %d gave 0x%04X, want 0xC315", action, resp.Status)
		}
	}
}

// TestUPSUnknownInstanceAndAction cover the two rejections that do not depend on
// state.
func TestUPSUnknownInstanceAndAction(t *testing.T) {
	h, _ := newScheduledStep(t)
	ctx := context.Background()

	resp, err := h.HandleNAction(ctx, &network.NActionRequest{
		RequestedSOPInstance: "1.2.3.does.not.exist",
		ActionTypeID:         network.UPSActionChangeState,
		DataSet:              upsDataSet(network.UPSInProgress, upsTxn),
	})
	if err != nil {
		t.Fatalf("N-ACTION: %v", err)
	}
	if resp.Status != network.StatusUPSNoSuchProcedureStep {
		t.Errorf("an unknown instance gave 0x%04X, want 0xC307", resp.Status)
	}

	resp, err = h.HandleNAction(ctx, &network.NActionRequest{
		RequestedSOPInstance: upsInstance,
		ActionTypeID:         99,
	})
	if err != nil {
		t.Fatalf("N-ACTION: %v", err)
	}
	if resp.Status != network.StatusUPSActionNotAppropriate {
		t.Errorf("an unknown action type gave 0x%04X, want 0xC314", resp.Status)
	}
}

// TestUPSGetReportsTheCurrentState checks N-GET sees transitions, since a
// watcher polls it to find out whether work has started.
func TestUPSGetReportsTheCurrentState(t *testing.T) {
	h, _ := newScheduledStep(t)
	ctx := context.Background()

	get := func() network.UPSState {
		resp, err := h.HandleNGet(ctx, &network.NGetRequest{RequestedSOPInstance: upsInstance})
		if err != nil {
			t.Fatalf("N-GET: %v", err)
		}
		if resp.Status != network.StatusSuccess {
			t.Fatalf("N-GET status 0x%04X", resp.Status)
		}
		elem, ok := resp.DataSet.Get(tag.New(0x0074, 0x1000))
		if !ok {
			t.Fatal("the returned step has no Procedure Step State")
		}
		v, _ := elem.GetValue().([]byte)
		return network.UPSState(trimUPS(string(v)))
	}

	if got := get(); got != network.UPSScheduled {
		t.Errorf("a new step reports %q", got)
	}
	if s := changeState(t, h, network.UPSInProgress, upsTxn); s != network.StatusSuccess {
		t.Fatalf("starting gave 0x%04X", s)
	}
	if got := get(); got != network.UPSInProgress {
		t.Errorf("after starting, the step reports %q", got)
	}
	if s := changeState(t, h, network.UPSCompleted, upsTxn); s != network.StatusSuccess {
		t.Fatalf("completing gave 0x%04X", s)
	}
	if got := get(); got != network.UPSCompleted {
		t.Errorf("after completing, the step reports %q", got)
	}
}

func trimUPS(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == 0) {
		s = s[:len(s)-1]
	}
	return s
}
