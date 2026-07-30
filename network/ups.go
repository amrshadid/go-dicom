package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// Unified Procedure Step (PS3.4 Annex CC).
//
// UPS is the only service in this library with a state machine, and the state
// machine is the whole point of it: a step is scheduled by one system, claimed
// by another, and completed by that one alone. What keeps two performers from
// both claiming it is the Transaction UID — issued when a step goes IN PROGRESS
// and required on every update afterwards. Getting that wrong does not produce a
// protocol error, it produces two systems that each believe they own the work.
//
// So this models the transitions and the lock, and leaves storage to the caller.
// A UPSStore is a few methods over whatever database already holds the worklist;
// everything about which transitions are legal, which status answers an illegal
// one, and when a Transaction UID is required lives here.
//
// Implemented: the Push SOP class — N-CREATE, N-SET, N-GET, and N-ACTION for
// changing state and requesting cancellation. Subscription and event reporting
// (the Watch SOP class) are not, and are answered as unsupported rather than
// silently accepted.

// tagProcedureStepState is (0074,1000). The Transaction UID tag is shared with
// storage commitment, which uses the same attribute for the same purpose.
var tagProcedureStepState = tag.New(0x0074, 0x1000)

// UPS N-ACTION action type IDs, PS3.4 Table CC.2.1-1 and CC.3.1-1.
const (
	UPSActionChangeState   uint16 = 1
	UPSActionRequestCancel uint16 = 2
	UPSActionSubscribe     uint16 = 3
	UPSActionUnsubscribe   uint16 = 4
	UPSActionSuspendGlobal uint16 = 5
)

// UPSState is the Procedure Step State (0074,1000).
type UPSState string

// The four states a Unified Procedure Step can be in, PS3.4 CC.1.1.
const (
	UPSScheduled  UPSState = "SCHEDULED"
	UPSInProgress UPSState = "IN PROGRESS"
	UPSCompleted  UPSState = "COMPLETED"
	UPSCanceled   UPSState = "CANCELED"
)

// IsFinal reports whether no further transition is possible.
func (s UPSState) IsFinal() bool {
	return s == UPSCompleted || s == UPSCanceled
}

// UPSStep is one procedure step as the SCP knows it.
type UPSStep struct {
	// SOPInstanceUID identifies the step.
	SOPInstanceUID string

	// State is the Procedure Step State.
	State UPSState

	// TransactionUID is the lock held by whoever set the step IN PROGRESS. It is
	// empty while the step is SCHEDULED, and every later update has to present
	// it.
	TransactionUID string

	// Attributes are the step's contents.
	Attributes *dataset.Dataset
}

// UPSStore is the storage a UPS SCP is built on.
//
// Only the operations the state machine needs, so an implementation is a thin
// layer over an existing worklist rather than a second copy of one. Returning
// found=false from Find is not an error; it is answered with
// StatusUPSNoSuchProcedureStep.
type UPSStore interface {
	FindUPS(ctx context.Context, sopInstanceUID string) (step *UPSStep, found bool, err error)
	CreateUPS(ctx context.Context, step *UPSStep) error
	UpdateUPS(ctx context.Context, step *UPSStep) error
}

// UPSHandler serves the Unified Procedure Step Push SOP class over a UPSStore.
//
// Embed it, or use it directly with a store. It answers N-CREATE, N-SET, N-GET
// and N-ACTION; anything else falls through to BaseHandler.
type UPSHandler struct {
	BaseHandler

	// Store holds the steps. Required.
	Store UPSStore

	// OnStateChange, if set, is called after a state transition has been
	// accepted and stored — the hook for starting work, or for notifying
	// whatever was watching.
	OnStateChange func(ctx context.Context, step *UPSStep, from, to UPSState)

	// OnCancelRequested, if set, is called for an N-ACTION requesting
	// cancellation. A step is not canceled by the request: the performer decides,
	// and says so with its own state change. Return a status to answer with, or
	// 0 to accept the request.
	OnCancelRequested func(ctx context.Context, step *UPSStep) uint16
}

// HandleNCreate creates a procedure step.
func (h *UPSHandler) HandleNCreate(ctx context.Context, req *NCreateRequest) (*NCreateResponse, error) {
	if h.Store == nil {
		return &NCreateResponse{Status: StatusUnableToProcess}, nil
	}
	if req.DataSet == nil {
		return &NCreateResponse{Status: StatusMissingAttribute}, nil
	}

	state := UPSState(upsStringValue(req.DataSet, tagProcedureStepState))
	switch state {
	case UPSScheduled, UPSInProgress:
		// Both are allowed at creation: a step may be scheduled for someone else
		// or claimed immediately by its creator.
	case "":
		return &NCreateResponse{Status: StatusMissingAttribute}, nil
	default:
		// A step cannot be created already finished.
		return &NCreateResponse{Status: StatusUPSStateWasNotScheduled}, nil
	}

	if _, found, err := h.Store.FindUPS(ctx, req.AffectedSOPInstance); err != nil {
		return nil, err
	} else if found {
		return &NCreateResponse{Status: StatusDuplicateSOPInstance}, nil
	}

	step := &UPSStep{
		SOPInstanceUID: req.AffectedSOPInstance,
		State:          state,
		Attributes:     req.DataSet,
	}
	// A step created IN PROGRESS is already claimed, so it must carry the
	// Transaction UID that claims it.
	if state == UPSInProgress {
		step.TransactionUID = upsStringValue(req.DataSet, tagTransactionUID)
		if step.TransactionUID == "" {
			return &NCreateResponse{Status: StatusUPSTransactionUIDMissing}, nil
		}
	}

	if err := h.Store.CreateUPS(ctx, step); err != nil {
		return nil, err
	}
	return &NCreateResponse{Status: StatusSuccess}, nil
}

// HandleNSet updates a procedure step's attributes.
//
// Only while it is IN PROGRESS, and only for whoever holds the Transaction UID.
// The state itself is not settable here: that is what N-ACTION is for, and
// allowing both would leave two ways to make the same transition with different
// rules attached.
func (h *UPSHandler) HandleNSet(ctx context.Context, req *NSetRequest) (*NSetResponse, error) {
	if h.Store == nil {
		return &NSetResponse{Status: StatusUnableToProcess}, nil
	}

	step, found, err := h.Store.FindUPS(ctx, req.RequestedSOPInstance)
	if err != nil {
		return nil, err
	}
	if !found {
		return &NSetResponse{Status: StatusUPSNoSuchProcedureStep}, nil
	}
	if req.DataSet == nil {
		return &NSetResponse{Status: StatusMissingAttribute}, nil
	}

	if step.State.IsFinal() {
		return &NSetResponse{Status: StatusUPSNotUpdatable}, nil
	}
	if step.State != UPSInProgress {
		return &NSetResponse{Status: StatusUPSNotInProgress}, nil
	}
	if upsStringValue(req.DataSet, tagProcedureStepState) != "" {
		return &NSetResponse{Status: StatusUPSMayOnlyBeScheduledByCreate}, nil
	}
	if got := upsStringValue(req.DataSet, tagTransactionUID); got != step.TransactionUID {
		return &NSetResponse{Status: StatusUPSTransactionUIDMissing}, nil
	}

	for _, elem := range req.DataSet.GetAll() {
		t, ok := elem.Tag()
		if !ok || t == tagTransactionUID {
			continue
		}
		step.Attributes.Remove(t)
		if err := step.Attributes.Add(elem); err != nil {
			return nil, fmt.Errorf("applying %s: %w", t, err)
		}
	}

	if err := h.Store.UpdateUPS(ctx, step); err != nil {
		return nil, err
	}
	return &NSetResponse{Status: StatusSuccess}, nil
}

// HandleNGet returns a procedure step's attributes.
func (h *UPSHandler) HandleNGet(ctx context.Context, req *NGetRequest) (*NGetResponse, error) {
	if h.Store == nil {
		return &NGetResponse{Status: StatusUnableToProcess}, nil
	}

	step, found, err := h.Store.FindUPS(ctx, req.RequestedSOPInstance)
	if err != nil {
		return nil, err
	}
	if !found {
		return &NGetResponse{Status: StatusUPSNoSuchProcedureStep}, nil
	}
	return &NGetResponse{Status: StatusSuccess, DataSet: step.Attributes}, nil
}

// HandleNAction changes a step's state or requests its cancellation.
func (h *UPSHandler) HandleNAction(ctx context.Context, req *NActionRequest) (*NActionResponse, error) {
	if h.Store == nil {
		return &NActionResponse{Status: StatusUnableToProcess}, nil
	}

	switch req.ActionTypeID {
	case UPSActionChangeState:
		return h.changeState(ctx, req)
	case UPSActionRequestCancel:
		return h.requestCancel(ctx, req)
	case UPSActionSubscribe, UPSActionUnsubscribe, UPSActionSuspendGlobal:
		// Answered rather than accepted: a subscription this SCP never honors
		// would leave the requestor waiting for reports that cannot arrive.
		return &NActionResponse{Status: StatusUPSEventReportsNotSupported}, nil
	default:
		return &NActionResponse{Status: StatusUPSActionNotAppropriate}, nil
	}
}

// changeState applies action type 1, the transition table in PS3.4 CC.1.1.
func (h *UPSHandler) changeState(ctx context.Context, req *NActionRequest) (*NActionResponse, error) {
	step, found, err := h.Store.FindUPS(ctx, req.RequestedSOPInstance)
	if err != nil {
		return nil, err
	}
	if !found {
		return &NActionResponse{Status: StatusUPSNoSuchProcedureStep}, nil
	}
	if req.DataSet == nil {
		return &NActionResponse{Status: StatusMissingAttribute}, nil
	}

	requested := UPSState(upsStringValue(req.DataSet, tagProcedureStepState))
	presented := upsStringValue(req.DataSet, tagTransactionUID)

	status, allowed := upsTransition(step, requested, presented)
	if !allowed {
		return &NActionResponse{Status: status}, nil
	}

	from := step.State
	step.State = requested
	switch requested {
	case UPSInProgress:
		// The claim: whoever started the step holds this for the rest of it.
		step.TransactionUID = presented
	case UPSCompleted, UPSCanceled:
		// Finished, so the lock is spent.
		step.TransactionUID = ""
	}
	setUPSState(step.Attributes, requested)

	if err := h.Store.UpdateUPS(ctx, step); err != nil {
		return nil, err
	}
	if h.OnStateChange != nil {
		h.OnStateChange(ctx, step, from, requested)
	}
	return &NActionResponse{Status: status}, nil
}

// requestCancel applies action type 2.
//
// A request, not a transition: the performer owns the step and decides whether
// to stop. Answering success here means the request was delivered, not that the
// step was canceled.
func (h *UPSHandler) requestCancel(ctx context.Context, req *NActionRequest) (*NActionResponse, error) {
	step, found, err := h.Store.FindUPS(ctx, req.RequestedSOPInstance)
	if err != nil {
		return nil, err
	}
	if !found {
		return &NActionResponse{Status: StatusUPSNoSuchProcedureStep}, nil
	}

	switch step.State {
	case UPSCanceled:
		return &NActionResponse{Status: StatusUPSAlreadyCanceled}, nil
	case UPSCompleted:
		return &NActionResponse{Status: StatusUPSAlreadyCompleted}, nil
	}

	if h.OnCancelRequested != nil {
		if status := h.OnCancelRequested(ctx, step); status != 0 {
			return &NActionResponse{Status: status}, nil
		}
	}
	return &NActionResponse{Status: StatusSuccess}, nil
}

// upsTransition decides whether a state change is allowed, and with what status.
//
// Separated from the handler because it is the part worth reading and the part
// worth testing: every rule in PS3.4 CC.1.1 is here and nowhere else. The second
// return says whether to apply the change; the status is what to answer either
// way, since two of the legal outcomes are warnings rather than plain success.
func upsTransition(step *UPSStep, requested UPSState, presentedTransactionUID string) (uint16, bool) {
	switch requested {
	case UPSScheduled:
		// A step is only ever scheduled by being created.
		return StatusUPSMayOnlyBeScheduledByCreate, false
	case UPSInProgress, UPSCompleted, UPSCanceled:
	default:
		return StatusInvalidArgumentValue, false
	}

	switch step.State {
	case UPSCompleted:
		if requested == UPSCompleted {
			return StatusUPSAlreadyInStateCompleted, false
		}
		return StatusUPSAlreadyCompleted, false

	case UPSCanceled:
		if requested == UPSCanceled {
			return StatusUPSAlreadyCanceled, false
		}
		return StatusUPSNotUpdatable, false

	case UPSScheduled:
		if requested != UPSInProgress {
			// A step cannot finish without having started: there would be no
			// Transaction UID, so nothing established who did the work.
			return StatusUPSNotInProgress, false
		}
		if presentedTransactionUID == "" {
			// Starting a step is where the lock is taken, so the requestor has
			// to supply one.
			return StatusUPSTransactionUIDMissing, false
		}
		return StatusSuccess, true

	case UPSInProgress:
		if requested == UPSInProgress {
			return StatusUPSAlreadyInProgress, false
		}
		if presentedTransactionUID == "" || presentedTransactionUID != step.TransactionUID {
			// The lock. Without this check any peer could finish or cancel work
			// another one is performing, and both would think they owned it.
			return StatusUPSTransactionUIDMissing, false
		}
		return StatusSuccess, true
	}

	return StatusUnableToProcess, false
}

// upsStringValue reads a trimmed string value, or "" when absent.
func upsStringValue(ds *dataset.Dataset, t tag.Tag) string {
	if ds == nil {
		return ""
	}
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	value, ok := elem.GetValue().([]byte)
	if !ok {
		return ""
	}
	return strings.Trim(string(value), " \x00")
}

// setUPSState writes the Procedure Step State so a later N-GET reports it.
func setUPSState(ds *dataset.Dataset, state UPSState) {
	if ds == nil {
		return
	}
	value := string(state)
	if len(value)%2 == 1 {
		value += " "
	}
	ds.Remove(tagProcedureStepState)
	_ = ds.Add(dataelem.NewDataElement(tagProcedureStepState, dataelem.CS, []byte(value)))
}
