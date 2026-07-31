package network

import (
	"context"
	"sort"
	"sync"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// UPS Watch is the half of Unified Procedure Step that lets an AE follow a step
// it does not own: it subscribes, and the SCP reports what happens by
// N-EVENT-REPORT.
//
// Without it a watcher has to poll, which is the thing the service exists to
// avoid — a scheduler learning a step finished thirty seconds late is a
// scheduler that idled a treatment room for thirty seconds.
//
// PS3.4 CC.2.3 defines three subscription targets, and the difference between
// them is the whole design:
//
//   - a step's own SOP Instance UID, watching that one step;
//   - the well-known Global Subscription instance, watching every step the SCP
//     has now and every one it creates later;
//   - the well-known Filtered Global Subscription instance, the same but only
//     for steps matching keys supplied with the subscription.
//
// A global subscription is not a shorthand for subscribing to each step. It
// covers steps that do not exist yet, so it has to be consulted at event time
// rather than expanded at subscribe time.

// UPS N-EVENT-REPORT event type IDs, PS3.4 Table CC.2.4-1.
const (
	// UPSEventStateReport reports a change of Procedure Step State.
	UPSEventStateReport uint16 = 1
	// UPSEventCancelRequested reports that cancellation was requested of a step
	// the receiver is performing.
	UPSEventCancelRequested uint16 = 2
	// UPSEventProgressReport reports progress within a step.
	UPSEventProgressReport uint16 = 3
	// UPSEventSOPInstanceUIDAssigned reports instances the step created.
	UPSEventSOPInstanceUIDAssigned uint16 = 4
)

var (
	tagReceivingAE         = tag.New(0x0074, 0x1234)
	tagDeletionLock        = tag.New(0x0074, 0x1200)
	tagInputReadinessState = tag.New(0x0040, 0x4041)
)

// UPSSubscription is one AE's standing interest in procedure steps.
type UPSSubscription struct {
	// ReceivingAE is the AE title events are sent to. It is the subscription's
	// identity together with SOPInstanceUID: an AE subscribing twice to the same
	// target has one subscription, not two.
	ReceivingAE string

	// SOPInstanceUID is what is being watched — a step's own UID, or one of the
	// two well-known global instances.
	SOPInstanceUID string

	// DeletionLock asks the SCP to keep a completed or canceled step rather than
	// deleting it, so the subscriber can still read it. The SCP is not obliged
	// to honor it; UPSHandler records it and leaves deletion to the store.
	DeletionLock bool

	// MatchKeys filters a Filtered Global subscription. Ignored for the other
	// two targets.
	MatchKeys *dataset.Dataset
}

// IsGlobal reports whether the subscription covers steps rather than a step.
func (s *UPSSubscription) IsGlobal() bool {
	return s.SOPInstanceUID == UPSGlobalSubscriptionInstanceUID ||
		s.SOPInstanceUID == UPSFilteredGlobalSubscriptionInstanceUID
}

// UPSSubscriptionStore holds subscriptions for a UPS SCP.
//
// SubscribersOf is asked at event time rather than at subscribe time, because a
// global subscription covers steps that did not exist when it was made.
type UPSSubscriptionStore interface {
	// SubscribeUPS records an interest, replacing any the same AE already has in
	// the same target.
	SubscribeUPS(ctx context.Context, sub *UPSSubscription) error

	// UnsubscribeUPS removes one interest. Removing one that is not there is not
	// an error: the end state is what was asked for.
	UnsubscribeUPS(ctx context.Context, receivingAE, sopInstanceUID string) error

	// SuspendGlobalUPS ends an AE's global subscriptions without touching the
	// subscriptions it holds on individual steps. That distinction is the point
	// of action type 5 — see PS3.4 CC.2.3.3.
	SuspendGlobalUPS(ctx context.Context, receivingAE string) error

	// SubscribersOfUPS returns every AE that should hear about this step.
	SubscribersOfUPS(ctx context.Context, step *UPSStep) ([]*UPSSubscription, error)
}

// UPSMemorySubscriptions is a UPSSubscriptionStore held in memory.
//
// Enough for a single-process SCP and for tests. An SCP that outlives its
// process needs subscriptions that do too, since a subscriber has no way to
// learn that the SCP forgot it and will wait for events that never come.
type UPSMemorySubscriptions struct {
	mu sync.RWMutex
	// keyed by receiving AE, then by watched instance
	subs map[string]map[string]*UPSSubscription
}

// NewUPSMemorySubscriptions returns an empty in-memory subscription store.
func NewUPSMemorySubscriptions() *UPSMemorySubscriptions {
	return &UPSMemorySubscriptions{subs: make(map[string]map[string]*UPSSubscription)}
}

// SubscribeUPS records an interest.
func (m *UPSMemorySubscriptions) SubscribeUPS(_ context.Context, sub *UPSSubscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	byInstance := m.subs[sub.ReceivingAE]
	if byInstance == nil {
		byInstance = make(map[string]*UPSSubscription)
		m.subs[sub.ReceivingAE] = byInstance
	}
	copied := *sub
	byInstance[sub.SOPInstanceUID] = &copied
	return nil
}

// UnsubscribeUPS removes one interest.
func (m *UPSMemorySubscriptions) UnsubscribeUPS(_ context.Context, receivingAE, sopInstanceUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if byInstance := m.subs[receivingAE]; byInstance != nil {
		delete(byInstance, sopInstanceUID)
		if len(byInstance) == 0 {
			delete(m.subs, receivingAE)
		}
	}
	return nil
}

// SuspendGlobalUPS drops the global subscriptions and keeps the rest.
func (m *UPSMemorySubscriptions) SuspendGlobalUPS(_ context.Context, receivingAE string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	byInstance := m.subs[receivingAE]
	if byInstance == nil {
		return nil
	}
	delete(byInstance, UPSGlobalSubscriptionInstanceUID)
	delete(byInstance, UPSFilteredGlobalSubscriptionInstanceUID)
	if len(byInstance) == 0 {
		delete(m.subs, receivingAE)
	}
	return nil
}

// SubscribersOfUPS returns the AEs watching this step, each at most once.
func (m *UPSMemorySubscriptions) SubscribersOfUPS(_ context.Context, step *UPSStep) ([]*UPSSubscription, error) {
	if step == nil {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []*UPSSubscription
	for ae, byInstance := range m.subs {
		// A subscription to the step itself is the most specific, then the
		// filtered global, then the global. An AE holding several hears once.
		var chosen *UPSSubscription
		if sub, ok := byInstance[step.SOPInstanceUID]; ok {
			chosen = sub
		} else if sub, ok := byInstance[UPSFilteredGlobalSubscriptionInstanceUID]; ok &&
			upsMatchesFilter(step, sub.MatchKeys) {
			chosen = sub
		} else if sub, ok := byInstance[UPSGlobalSubscriptionInstanceUID]; ok {
			chosen = sub
		}
		if chosen != nil {
			copied := *chosen
			copied.ReceivingAE = ae
			out = append(out, &copied)
		}
	}
	// Map iteration is unordered, and an event fan-out that varies in order
	// between runs is one that cannot be tested or reproduced from a log.
	sort.Slice(out, func(i, j int) bool { return out[i].ReceivingAE < out[j].ReceivingAE })
	return out, nil
}

// upsMatchesFilter reports whether a step satisfies a filtered subscription.
//
// Single-value matching on the keys present, which is what PS3.4 CC.2.3.2
// requires of the attributes a Filtered Global subscription may carry: they are
// the identifying and state attributes, none of which are ranges or lists. A
// key the step does not have does not match, rather than matching vacuously —
// the safe direction, since the alternative sends a subscriber events it
// explicitly asked not to receive.
func upsMatchesFilter(step *UPSStep, keys *dataset.Dataset) bool {
	if keys == nil || step == nil {
		return true
	}
	for _, elem := range keys.GetAll() {
		t, ok := elem.Tag()
		if !ok {
			continue
		}
		want := upsStringValue(keys, t)
		if want == "" {
			continue // universal matching: present but empty matches anything
		}
		if step.Attributes == nil {
			return false
		}
		if upsStringValue(step.Attributes, t) != want {
			return false
		}
	}
	return true
}

// UPSEventNotifier delivers an N-EVENT-REPORT to a subscriber.
//
// *SCP and *Server implement it, so a UPSHandler given one reports events over
// associations it opens back to subscribers. It is an interface rather than a
// concrete type so a handler can be tested, and so an application that already
// has a channel to its watchers can use that instead.
type UPSEventNotifier interface {
	ReportUPSEvent(ctx context.Context, receivingAE string, event *UPSEvent) error
}

// UPSEvent is one N-EVENT-REPORT about a procedure step.
type UPSEvent struct {
	// SOPInstanceUID is the step the event is about. For a global subscription
	// this is still the step, not the well-known instance: the subscriber needs
	// to know which step changed.
	SOPInstanceUID string

	// EventTypeID is one of the UPSEvent constants.
	EventTypeID uint16

	// Attributes carries the event's information. PS3.4 CC.2.4 fixes what each
	// event type must contain; UPSHandler fills it for the events it raises.
	Attributes *dataset.Dataset
}

// upsStateReportEvent builds the event a state change raises.
//
// PS3.4 Table CC.2.4-2 requires the new state, and Input Readiness State when
// the step is still SCHEDULED — a subscriber deciding whether to claim a step
// needs to know whether its inputs are there.
func upsStateReportEvent(step *UPSStep) *UPSEvent {
	attrs := dataset.NewDataset()
	setUPSState(attrs, step.State)
	if step.State == UPSScheduled && step.Attributes != nil {
		if v := upsStringValue(step.Attributes, tagInputReadinessState); v != "" {
			_ = attrs.SetValue(tagInputReadinessState, []byte(padUPSValue(v)))
		}
	}
	return &UPSEvent{
		SOPInstanceUID: step.SOPInstanceUID,
		EventTypeID:    UPSEventStateReport,
		Attributes:     attrs,
	}
}

// upsCancelRequestedEvent builds the event a cancellation request raises.
func upsCancelRequestedEvent(step *UPSStep, reason *dataset.Dataset) *UPSEvent {
	attrs := reason
	if attrs == nil {
		attrs = dataset.NewDataset()
	}
	return &UPSEvent{
		SOPInstanceUID: step.SOPInstanceUID,
		EventTypeID:    UPSEventCancelRequested,
		Attributes:     attrs,
	}
}

// padUPSValue pads a value to an even length, as PS3.5 7.1.1 requires.
func padUPSValue(s string) string {
	if len(s)%2 == 1 {
		return s + " "
	}
	return s
}

// handleSubscription applies N-ACTION types 3, 4 and 5.
func (h *UPSHandler) handleSubscription(ctx context.Context, req *NActionRequest) (*NActionResponse, error) {
	target := req.RequestedSOPInstance

	// The Receiving AE Title is the one attribute that decides where events go.
	// It defaults to the AE that asked, which is what a subscriber that is also
	// the receiver means, and is the only sensible reading when the attribute is
	// absent — but an AE may name a third party, so a supplied value wins.
	receiving := ""
	if req.DataSet != nil {
		receiving = upsStringValue(req.DataSet, tagReceivingAE)
	}
	if receiving == "" {
		if info := AssociationInfoFromContext(ctx); info != nil {
			receiving = info.CallingAE
		}
	}
	if receiving == "" {
		return &NActionResponse{Status: StatusMissingAttribute}, nil
	}

	switch req.ActionTypeID {
	case UPSActionSuspendGlobal:
		// Only meaningful against the global instance. Suspending "globally" on
		// one step would silently do nothing, and a subscriber that believes it
		// has stopped receiving events but has not is worse served by success
		// than by being told the action does not apply.
		if target != UPSGlobalSubscriptionInstanceUID &&
			target != UPSFilteredGlobalSubscriptionInstanceUID {
			return &NActionResponse{Status: StatusUPSActionNotAppropriate}, nil
		}
		if err := h.Subscriptions.SuspendGlobalUPS(ctx, receiving); err != nil {
			return nil, err
		}
		return &NActionResponse{Status: StatusSuccess}, nil

	case UPSActionUnsubscribe:
		if err := h.Subscriptions.UnsubscribeUPS(ctx, receiving, target); err != nil {
			return nil, err
		}
		return &NActionResponse{Status: StatusSuccess}, nil

	case UPSActionSubscribe:
		// Subscribing to a step means that step has to exist. Subscribing to a
		// global instance does not: it covers steps not yet created, which is
		// the reason the well-known instances exist.
		if target != UPSGlobalSubscriptionInstanceUID &&
			target != UPSFilteredGlobalSubscriptionInstanceUID {
			if _, found, err := h.Store.FindUPS(ctx, target); err != nil {
				return nil, err
			} else if !found {
				return &NActionResponse{Status: StatusUPSNoSuchProcedureStep}, nil
			}
		}

		sub := &UPSSubscription{ReceivingAE: receiving, SOPInstanceUID: target}
		if req.DataSet != nil {
			sub.DeletionLock = upsStringValue(req.DataSet, tagDeletionLock) == "TRUE"
			if target == UPSFilteredGlobalSubscriptionInstanceUID {
				sub.MatchKeys = upsFilterKeys(req.DataSet)
			}
		}
		if err := h.Subscriptions.SubscribeUPS(ctx, sub); err != nil {
			return nil, err
		}
		return &NActionResponse{Status: StatusSuccess}, nil
	}
	return &NActionResponse{Status: StatusUPSActionNotAppropriate}, nil
}

// upsFilterKeys extracts the matching keys from a Filtered Global subscription,
// dropping the two attributes that control the subscription rather than select
// steps. Leaving them in would filter every step out, since no step carries a
// Receiving AE Title of its own.
func upsFilterKeys(ds *dataset.Dataset) *dataset.Dataset {
	keys := dataset.NewDataset()
	for _, elem := range ds.GetAll() {
		t, ok := elem.Tag()
		if !ok || t == tagReceivingAE || t == tagDeletionLock {
			continue
		}
		value, ok := elem.GetValue().([]byte)
		if !ok {
			continue
		}
		_ = keys.SetValue(t, value)
	}
	if len(keys.GetAll()) == 0 {
		return nil
	}
	return keys
}

// notifySubscribers sends one event to everyone watching the step.
//
// Failures are reported through OnEventReportError and do not fail the
// operation that raised the event. The state change has already happened and is
// stored; refusing it because a watcher was unreachable would leave the SCP and
// the performer disagreeing about the step, which is worse than a missed
// notification.
func (h *UPSHandler) notifySubscribers(ctx context.Context, step *UPSStep, event *UPSEvent) {
	if h.Subscriptions == nil || h.Notifier == nil || event == nil {
		return
	}
	subs, err := h.Subscriptions.SubscribersOfUPS(ctx, step)
	if err != nil {
		if h.OnEventReportError != nil {
			h.OnEventReportError(ctx, nil, event, err)
		}
		return
	}
	for _, sub := range subs {
		if err := h.Notifier.ReportUPSEvent(ctx, sub.ReceivingAE, event); err != nil &&
			h.OnEventReportError != nil {
			h.OnEventReportError(ctx, sub, event, err)
		}
	}
}

// ReportUPSEvent delivers an N-EVENT-REPORT to a subscriber.
//
// The roles invert for this exchange, as they do for a deferred storage
// commitment result: the SCP that holds the step is the one initiating, so it
// associates as an SCU on the UPS Event SOP Class.
//
// The subscriber's address comes from SCPConfig.CommitmentRequestors or
// ResolveCommitmentRequestor — the same map, since both services need to reach
// an AE that is not currently connected, and an application that runs both
// should not have to maintain two copies of the same directory.
func (s *SCP) ReportUPSEvent(ctx context.Context, receivingAE string, event *UPSEvent) error {
	if event == nil {
		return NewPDUError("UPS_EVENT", "event is nil")
	}
	if event.SOPInstanceUID == "" {
		return NewPDUError("UPS_EVENT",
			"an event needs the SOP Instance UID of the step it is about; "+
				"a subscriber cannot tell which step changed without it")
	}

	address, ok := s.config.resolveCommitmentRequestor(receivingAE)
	if !ok {
		return NewPDUErrorf("UPS_EVENT",
			"no address known for subscriber %q; set SCPConfig.CommitmentRequestors or "+
				"ResolveCommitmentRequestor so events can be delivered", receivingAE)
	}

	subscriber := NewSCU(SCUConfig{
		CallingAE: s.config.AETitle,
		CalledAE:  receivingAE,
		Address:   address,
		Network:   s.config.Network,
	})

	if err := subscriber.Associate(ctx, UnifiedProcedureStepPresentationContexts()); err != nil {
		return NewPDUErrorf("UPS_EVENT",
			"could not associate with subscriber %s at %s: %v", receivingAE, address, err)
	}
	defer func() { _ = subscriber.Release(ctx) }()

	// Reported under the Event SOP Class rather than under Push or Watch: PS3.4
	// CC.3.4 gives event reporting its own class so a subscriber can accept
	// events without also offering to serve steps.
	if _, err := subscriber.NEventReport(ctx, UnifiedProcedureStepEventUID,
		event.SOPInstanceUID, event.EventTypeID, event.Attributes); err != nil {
		return NewPDUErrorf("UPS_EVENT",
			"failed to report event %d for step %s to %s: %v",
			event.EventTypeID, event.SOPInstanceUID, receivingAE, err)
	}
	return nil
}
