package network_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// UPS Watch is what makes the service worth having: a scheduler that has to
// poll to learn a step finished is a scheduler that idles a treatment room
// while it waits for the next tick.
//
// The tests below cover the three subscription targets separately, because they
// differ in a way that is easy to collapse by accident. A global subscription is
// not shorthand for subscribing to every step — it covers steps that do not
// exist yet, so it has to be consulted when the event happens rather than
// expanded when the subscription is made.

// recordingNotifier captures the events a handler sends instead of associating.
type recordingNotifier struct {
	mu     sync.Mutex
	sent   []sentUPSEvent
	failFn func(receivingAE string) error
}

type sentUPSEvent struct {
	receivingAE string
	event       *network.UPSEvent
}

func (r *recordingNotifier) ReportUPSEvent(_ context.Context, receivingAE string,
	event *network.UPSEvent) error {

	if r.failFn != nil {
		if err := r.failFn(receivingAE); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentUPSEvent{receivingAE, event})
	return nil
}

func (r *recordingNotifier) receivers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.sent))
	for i, s := range r.sent {
		out[i] = s.receivingAE
	}
	return out
}

// upsDataSet builds a data set from tag/value pairs.
func upsAttrs(pairs ...any) *dataset.Dataset {
	ds := dataset.NewDataset()
	for i := 0; i+1 < len(pairs); i += 2 {
		t := pairs[i].(tag.Tag)
		v := pairs[i+1].(string)
		if len(v)%2 == 1 {
			v += " "
		}
		_ = ds.SetValue(t, []byte(v))
	}
	return ds
}

var (
	tagStepState   = tag.New(0x0074, 0x1000)
	tagTxUID       = tag.New(0x0008, 0x1195)
	tagRecvAE      = tag.New(0x0074, 0x1234)
	tagStationName = tag.New(0x0040, 0x0010)
)

// watchHandler builds a UPS handler with one SCHEDULED step in it.
func watchHandler(t *testing.T, notifier network.UPSEventNotifier) (*network.UPSHandler, *memoryUPSStore) {
	t.Helper()

	store := newMemoryUPSStore()
	_ = store.CreateUPS(context.Background(), &network.UPSStep{
		SOPInstanceUID: "1.2.3.4",
		State:          network.UPSScheduled,
		Attributes: upsAttrs(tagStepState, "SCHEDULED",
			tagStationName, "ROOM1"),
	})
	return &network.UPSHandler{
		Store:         store,
		Subscriptions: network.NewUPSMemorySubscriptions(),
		Notifier:      notifier,
	}, store
}

// subscribe issues an N-ACTION of the given type and returns the status.
func subscribe(t *testing.T, h *network.UPSHandler, actionType uint16,
	target string, ds *dataset.Dataset) uint16 {

	t.Helper()
	resp, err := h.HandleNAction(context.Background(), &network.NActionRequest{
		ActionTypeID:         actionType,
		RequestedSOPInstance: target,
		DataSet:              ds,
	})
	if err != nil {
		t.Fatalf("N-ACTION %d: %v", actionType, err)
	}
	return resp.Status
}

// changeState drives a step to a new state and returns the status.
func changeStepState(t *testing.T, h *network.UPSHandler, uid string, state, txUID string) uint16 {
	t.Helper()
	resp, err := h.HandleNAction(context.Background(), &network.NActionRequest{
		ActionTypeID:         network.UPSActionChangeState,
		RequestedSOPInstance: uid,
		DataSet:              upsAttrs(tagStepState, state, tagTxUID, txUID),
	})
	if err != nil {
		t.Fatalf("state change to %s: %v", state, err)
	}
	return resp.Status
}

// TestSubscriptionActionsAreRefusedWithoutAStore covers the behavior that was
// there before: an SCP that keeps no subscriptions says so.
//
// Accepting a subscription and never sending an event is the worst answer
// available — the subscriber has no way to tell the difference between "nothing
// has happened yet" and "nothing will ever be reported to you".
func TestSubscriptionActionsAreRefusedWithoutAStore(t *testing.T) {
	h := &network.UPSHandler{Store: newMemoryUPSStore()}

	for _, action := range []uint16{
		network.UPSActionSubscribe,
		network.UPSActionUnsubscribe,
		network.UPSActionSuspendGlobal,
	} {
		status := subscribe(t, h, action, network.UPSGlobalSubscriptionInstanceUID, nil)
		if status != network.StatusUPSEventReportsNotSupported {
			t.Errorf("action %d answered 0x%04X, want StatusUPSEventReportsNotSupported",
				action, status)
		}
	}
}

// TestSubscribingToAStepReportsItsStateChanges is the ordinary case.
func TestSubscribingToAStepReportsItsStateChanges(t *testing.T) {
	notifier := &recordingNotifier{}
	h, _ := watchHandler(t, notifier)

	if status := subscribe(t, h, network.UPSActionSubscribe, "1.2.3.4",
		upsAttrs(tagRecvAE, "WATCHER")); status != network.StatusSuccess {
		t.Fatalf("subscribe answered 0x%04X", status)
	}

	if status := changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9"); status != network.StatusSuccess {
		t.Fatalf("state change answered 0x%04X", status)
	}

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("sent %d events, want 1", len(notifier.sent))
	}
	got := notifier.sent[0]
	if got.receivingAE != "WATCHER" {
		t.Errorf("event went to %q, want WATCHER", got.receivingAE)
	}
	if got.event.EventTypeID != network.UPSEventStateReport {
		t.Errorf("event type is %d, want %d (state report)",
			got.event.EventTypeID, network.UPSEventStateReport)
	}
	// The step's own UID, not the subscription target, or a subscriber watching
	// several steps cannot tell which one changed.
	if got.event.SOPInstanceUID != "1.2.3.4" {
		t.Errorf("event names step %q, want 1.2.3.4", got.event.SOPInstanceUID)
	}
	if state := upsValue(got.event.Attributes, tagStepState); state != "IN PROGRESS" {
		t.Errorf("event reports state %q, want IN PROGRESS", state)
	}
}

// TestUnsubscribingStopsEvents covers the other direction.
func TestUnsubscribingStopsEvents(t *testing.T) {
	notifier := &recordingNotifier{}
	h, _ := watchHandler(t, notifier)

	subscribe(t, h, network.UPSActionSubscribe, "1.2.3.4", upsAttrs(tagRecvAE, "WATCHER"))
	changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9")

	if status := subscribe(t, h, network.UPSActionUnsubscribe, "1.2.3.4",
		upsAttrs(tagRecvAE, "WATCHER")); status != network.StatusSuccess {
		t.Fatalf("unsubscribe answered 0x%04X", status)
	}
	changeStepState(t, h, "1.2.3.4", "COMPLETED", "9.9.9")

	if got := notifier.receivers(); len(got) != 1 {
		t.Errorf("received %d events after unsubscribing, want the 1 sent before it: %v",
			len(got), got)
	}
}

// TestGlobalSubscriptionCoversStepsCreatedLater is the case a subscription list
// expanded at subscribe time gets wrong.
//
// The subscriber asks before the step exists. Anything that resolved the global
// instance into the set of steps present at that moment would report nothing.
func TestGlobalSubscriptionCoversStepsCreatedLater(t *testing.T) {
	notifier := &recordingNotifier{}
	h, store := watchHandler(t, notifier)

	if status := subscribe(t, h, network.UPSActionSubscribe,
		network.UPSGlobalSubscriptionInstanceUID,
		upsAttrs(tagRecvAE, "WATCHER")); status != network.StatusSuccess {
		t.Fatalf("global subscribe answered 0x%04X", status)
	}

	_ = store.CreateUPS(context.Background(), &network.UPSStep{
		SOPInstanceUID: "5.6.7.8",
		State:          network.UPSScheduled,
		Attributes:     upsAttrs(tagStepState, "SCHEDULED"),
	})
	changeStepState(t, h, "5.6.7.8", "IN PROGRESS", "9.9.9")

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("sent %d events for a step created after the global subscription, want 1",
			len(notifier.sent))
	}
	if uid := notifier.sent[0].event.SOPInstanceUID; uid != "5.6.7.8" {
		t.Errorf("event names step %q, want 5.6.7.8", uid)
	}
}

// TestSubscribingToAMissingStepIsRefused covers the asymmetry between the two
// kinds of target: a step has to exist, a global instance does not.
func TestSubscribingToAMissingStepIsRefused(t *testing.T) {
	h, _ := watchHandler(t, &recordingNotifier{})

	status := subscribe(t, h, network.UPSActionSubscribe, "no.such.step",
		upsAttrs(tagRecvAE, "WATCHER"))
	if status != network.StatusUPSNoSuchProcedureStep {
		t.Errorf("subscribing to a step that does not exist answered 0x%04X, "+
			"want StatusUPSNoSuchProcedureStep", status)
	}
}

// TestFilteredGlobalSubscriptionMatchesOnKeys covers the third target.
func TestFilteredGlobalSubscriptionMatchesOnKeys(t *testing.T) {
	notifier := &recordingNotifier{}
	h, store := watchHandler(t, notifier)

	// Only steps in ROOM2.
	if status := subscribe(t, h, network.UPSActionSubscribe,
		network.UPSFilteredGlobalSubscriptionInstanceUID,
		upsAttrs(tagRecvAE, "WATCHER", tagStationName, "ROOM2")); status != network.StatusSuccess {
		t.Fatalf("filtered subscribe answered 0x%04X", status)
	}

	_ = store.CreateUPS(context.Background(), &network.UPSStep{
		SOPInstanceUID: "room2.step",
		State:          network.UPSScheduled,
		Attributes:     upsAttrs(tagStepState, "SCHEDULED", tagStationName, "ROOM2"),
	})

	// The fixture's step is in ROOM1 and must not match.
	changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9")
	changeStepState(t, h, "room2.step", "IN PROGRESS", "8.8.8")

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("sent %d events, want 1 — only the ROOM2 step matches the filter",
			len(notifier.sent))
	}
	if uid := notifier.sent[0].event.SOPInstanceUID; uid != "room2.step" {
		t.Errorf("event names step %q, want room2.step", uid)
	}
}

// TestSuspendGlobalKeepsPerStepSubscriptions covers the distinction that gives
// action type 5 its reason to exist.
//
// Suspending a global subscription is not unsubscribing from everything. An AE
// that asked to stop hearing about every step still wants the ones it named.
func TestSuspendGlobalKeepsPerStepSubscriptions(t *testing.T) {
	notifier := &recordingNotifier{}
	h, store := watchHandler(t, notifier)

	subscribe(t, h, network.UPSActionSubscribe, network.UPSGlobalSubscriptionInstanceUID,
		upsAttrs(tagRecvAE, "WATCHER"))
	subscribe(t, h, network.UPSActionSubscribe, "1.2.3.4", upsAttrs(tagRecvAE, "WATCHER"))

	if status := subscribe(t, h, network.UPSActionSuspendGlobal,
		network.UPSGlobalSubscriptionInstanceUID,
		upsAttrs(tagRecvAE, "WATCHER")); status != network.StatusSuccess {
		t.Fatalf("suspend answered 0x%04X", status)
	}

	// A step the AE never named individually: silence.
	_ = store.CreateUPS(context.Background(), &network.UPSStep{
		SOPInstanceUID: "other.step",
		State:          network.UPSScheduled,
		Attributes:     upsAttrs(tagStepState, "SCHEDULED"),
	})
	changeStepState(t, h, "other.step", "IN PROGRESS", "7.7.7")

	// The step it did name: still reported.
	changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9")

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("sent %d events after suspending the global subscription, want 1", len(notifier.sent))
	}
	if uid := notifier.sent[0].event.SOPInstanceUID; uid != "1.2.3.4" {
		t.Errorf("event names step %q, want the individually subscribed 1.2.3.4", uid)
	}
}

// TestSuspendGlobalOnAStepIsRefused covers an action that would otherwise
// succeed while doing nothing.
func TestSuspendGlobalOnAStepIsRefused(t *testing.T) {
	h, _ := watchHandler(t, &recordingNotifier{})

	status := subscribe(t, h, network.UPSActionSuspendGlobal, "1.2.3.4",
		upsAttrs(tagRecvAE, "WATCHER"))
	if status != network.StatusUPSActionNotAppropriate {
		t.Errorf("suspending globally against a single step answered 0x%04X, want "+
			"StatusUPSActionNotAppropriate; succeeding would tell the subscriber it "+
			"had stopped receiving events when it had not", status)
	}
}

// TestASubscriberIsNotifiedOnce covers an AE holding several subscriptions that
// all cover the same step.
func TestASubscriberIsNotifiedOnce(t *testing.T) {
	notifier := &recordingNotifier{}
	h, _ := watchHandler(t, notifier)

	subscribe(t, h, network.UPSActionSubscribe, network.UPSGlobalSubscriptionInstanceUID,
		upsAttrs(tagRecvAE, "WATCHER"))
	subscribe(t, h, network.UPSActionSubscribe, network.UPSFilteredGlobalSubscriptionInstanceUID,
		upsAttrs(tagRecvAE, "WATCHER", tagStationName, "ROOM1"))
	subscribe(t, h, network.UPSActionSubscribe, "1.2.3.4", upsAttrs(tagRecvAE, "WATCHER"))

	changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9")

	if got := notifier.receivers(); len(got) != 1 {
		t.Errorf("sent %d events to one subscriber holding three matching subscriptions, "+
			"want 1: %v", len(got), got)
	}
}

// TestReceivingAETitleDefaultsToTheCaller covers the attribute that decides
// where events go.
func TestReceivingAETitleDefaultsToTheCaller(t *testing.T) {
	notifier := &recordingNotifier{}
	h, _ := watchHandler(t, notifier)

	ctx := network.ContextWithAssociationInfo(context.Background(),
		&network.AssociationInfo{CallingAE: "CALLER"})
	resp, err := h.HandleNAction(ctx, &network.NActionRequest{
		ActionTypeID:         network.UPSActionSubscribe,
		RequestedSOPInstance: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if resp.Status != network.StatusSuccess {
		t.Fatalf("subscribe without a Receiving AE Title answered 0x%04X", resp.Status)
	}

	changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9")

	if got := notifier.receivers(); len(got) != 1 || got[0] != "CALLER" {
		t.Errorf("events went to %v, want [CALLER] — an AE that names no receiver "+
			"means itself", got)
	}
}

// TestCancelRequestIsReportedToSubscribers covers event type 2.
//
// The performer holds the step and is the only one that can stop it, and a
// subscription is the only channel the SCP has to reach it.
func TestCancelRequestIsReportedToSubscribers(t *testing.T) {
	notifier := &recordingNotifier{}
	h, _ := watchHandler(t, notifier)

	changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9")
	subscribe(t, h, network.UPSActionSubscribe, "1.2.3.4", upsAttrs(tagRecvAE, "PERFORMER"))

	status := subscribe(t, h, network.UPSActionRequestCancel, "1.2.3.4", nil)
	if status != network.StatusSuccess {
		t.Fatalf("cancel request answered 0x%04X", status)
	}

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("sent %d events, want 1", len(notifier.sent))
	}
	if got := notifier.sent[0].event.EventTypeID; got != network.UPSEventCancelRequested {
		t.Errorf("event type is %d, want %d (cancel requested)",
			got, network.UPSEventCancelRequested)
	}
}

// TestAnUnreachableSubscriberDoesNotFailTheStateChange covers the failure that
// has to be survivable.
//
// The transition has already been accepted and stored by the time events go
// out. Failing the N-ACTION would leave the SCP and the performer disagreeing
// about the step — worse than a missed notification, and not recoverable by
// retrying, since the retry would be refused as an illegal transition.
func TestAnUnreachableSubscriberDoesNotFailTheStateChange(t *testing.T) {
	notifier := &recordingNotifier{
		failFn: func(string) error { return context.DeadlineExceeded },
	}
	h, store := watchHandler(t, notifier)

	var reported []string
	h.OnEventReportError = func(_ context.Context, sub *network.UPSSubscription,
		_ *network.UPSEvent, _ error) {
		reported = append(reported, sub.ReceivingAE)
	}

	subscribe(t, h, network.UPSActionSubscribe, "1.2.3.4", upsAttrs(tagRecvAE, "GONE"))

	if status := changeStepState(t, h, "1.2.3.4", "IN PROGRESS", "9.9.9"); status != network.StatusSuccess {
		t.Fatalf("state change answered 0x%04X after the subscriber was unreachable; "+
			"the transition is already stored and cannot be undone", status)
	}
	step, _, _ := store.FindUPS(context.Background(), "1.2.3.4")
	if step.State != network.UPSInProgress {
		t.Errorf("the step is %q, want IN PROGRESS", step.State)
	}
	if len(reported) != 1 || reported[0] != "GONE" {
		t.Errorf("OnEventReportError saw %v, want [GONE]; without it the failure is invisible",
			reported)
	}
}

// TestUPSEventReachesASubscriberOverTheWire drives the whole path: a watcher
// running its own SCP, a subscription, a state change, and the N-EVENT-REPORT
// that arrives on an association the archive opens back to it.
//
// The unit tests above use a notifier that records instead of associating, so
// none of them would catch the SOP class, the presentation contexts, or the
// address lookup being wrong.
func TestUPSEventReachesASubscriberOverTheWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The watcher: an SCP that accepts N-EVENT-REPORT.
	events := make(chan *network.NEventReportRequest, 4)
	watcher, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "WATCHER", Port: 0, BindAddress: "127.0.0.1",
	}, &upsEventReceiver{events: events})
	if err != nil {
		t.Fatalf("starting the watcher: %v", err)
	}
	defer watcher.Stop()
	watcher.SetSupportedAbstractSyntaxes([]string{network.UnifiedProcedureStepEventUID})

	// The archive: a UPS SCP that knows how to reach the watcher.
	store := newMemoryUPSStore()
	_ = store.CreateUPS(ctx, &network.UPSStep{
		SOPInstanceUID: "1.2.3.4",
		State:          network.UPSScheduled,
		Attributes:     upsAttrs(tagStepState, "SCHEDULED"),
	})
	handler := &network.UPSHandler{
		Store:         store,
		Subscriptions: network.NewUPSMemorySubscriptions(),
	}
	archive, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
		CommitmentRequestors: map[string]string{"WATCHER": watcher.Addr()},
	}, handler)
	if err != nil {
		t.Fatalf("starting the archive: %v", err)
	}
	defer archive.Stop()
	archive.SetSupportedAbstractSyntaxes([]string{network.UnifiedProcedureStepPushUID})
	handler.Notifier = archive

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "WATCHER", CalledAE: "ARCHIVE", Address: archive.Addr(),
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: network.UnifiedProcedureStepPushUID,
		TransferSyntaxes: []string{network.ImplicitVRLittleEndianUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}

	if _, err := scu.NAction(ctx, network.UnifiedProcedureStepPushUID, "1.2.3.4",
		network.UPSActionSubscribe, upsAttrs(tagRecvAE, "WATCHER")); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := scu.NAction(ctx, network.UnifiedProcedureStepPushUID, "1.2.3.4",
		network.UPSActionChangeState,
		upsAttrs(tagStepState, "IN PROGRESS", tagTxUID, "9.9.9")); err != nil {
		t.Fatalf("state change: %v", err)
	}
	_ = scu.Release(ctx)

	select {
	case got := <-events:
		if got.EventTypeID != network.UPSEventStateReport {
			t.Errorf("event type is %d, want %d", got.EventTypeID, network.UPSEventStateReport)
		}
		if got.AffectedSOPInstance != "1.2.3.4" {
			t.Errorf("event names step %q, want 1.2.3.4", got.AffectedSOPInstance)
		}
		if state := upsValue(got.DataSet, tagStepState); state != "IN PROGRESS" {
			t.Errorf("event reports state %q, want IN PROGRESS", state)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no N-EVENT-REPORT arrived at the subscriber")
	}
}

// upsEventReceiver accepts N-EVENT-REPORT and forwards it to a channel.
type upsEventReceiver struct {
	network.BaseHandler
	events chan *network.NEventReportRequest
}

func (r *upsEventReceiver) HandleNEventReport(_ context.Context,
	req *network.NEventReportRequest) (*network.NEventReportResponse, error) {

	select {
	case r.events <- req:
	default:
	}
	return &network.NEventReportResponse{Status: network.StatusSuccess}, nil
}

// upsValue reads a string value out of a data set, trimming the padding.
func upsValue(ds *dataset.Dataset, t tag.Tag) string {
	if ds == nil {
		return ""
	}
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	b, _ := elem.GetValue().([]byte)
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == 0) {
		s = s[:len(s)-1]
	}
	return s
}
