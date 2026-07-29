package network

import (
	"context"
	"fmt"
	"sync"
)

// AssociationState represents the state of a DICOM association.
type AssociationState int

const (
	StateIdle                  AssociationState = iota // No association
	StateAwaitingAssocResponse                         // A-ASSOCIATE-RQ sent, waiting for response
	StateAssociated                                    // Association established
	StateAwaitingRelease                               // A-RELEASE-RQ sent, waiting for response
	StateAwaitingReleaseRP                             // Received A-RELEASE-RQ, processing
)

// String returns a human-readable name for the association state.
func (s AssociationState) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateAwaitingAssocResponse:
		return "AwaitingAssocResponse"
	case StateAssociated:
		return "Associated"
	case StateAwaitingRelease:
		return "AwaitingRelease"
	case StateAwaitingReleaseRP:
		return "AwaitingReleaseRP"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// Association represents a DICOM association between two AEs.
type Association struct {
	mu sync.RWMutex

	state     AssociationState
	transport *Transport

	// Negotiated parameters
	callingAE        string
	calledAE         string
	maxPDUSize       uint32
	acceptedContexts map[byte]*PresentationContext

	// Request parameters (stored for reference)
	requestedContexts []PresentationContextItem

	// Extended negotiation as agreed with the peer.
	peerUserInfo UserInformationItem

	// pending holds messages read ahead of the caller that asked for them.
	//
	// An operation that watches for C-CANCEL while it works has to read the
	// association to see one, and what arrives may be something else. Dropping
	// that would lose a message the peer believes it sent; leaving it in the
	// stream is impossible once read. It is queued here and returned by the
	// next ReceivePData, so reading ahead is transparent to whoever reads next.
	pending []pendingMessage
}

// pendingMessage is a message read before its recipient asked for it.
type pendingMessage struct {
	contextID byte
	data      []byte
	isCommand bool
}

// PeerUserInformation returns the User Information the peer sent during
// association negotiation, including any extended negotiation sub-items
// (async operations window, role selection, user identity response).
func (a *Association) PeerUserInformation() UserInformationItem {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.peerUserInfo
}

// RoleSelectionFor returns the negotiated SCP/SCU role selection for a SOP
// Class, and whether the peer supplied one.
func (a *Association) RoleSelectionFor(sopClassUID string) (SCPSCURoleSelection, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, rs := range a.peerUserInfo.RoleSelections {
		if rs.SOPClassUID == sopClassUID {
			return rs, true
		}
	}
	return SCPSCURoleSelection{}, false
}

// NewAssociation creates a new association in the Idle state.
func NewAssociation(transport *Transport) *Association {
	return &Association{
		state:            StateIdle,
		transport:        transport,
		acceptedContexts: make(map[byte]*PresentationContext),
	}
}

// State returns the current association state.
func (a *Association) State() AssociationState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// CallingAE returns the calling AE title.
func (a *Association) CallingAE() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.callingAE
}

// CalledAE returns the called AE title.
func (a *Association) CalledAE() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.calledAE
}

// MaxPDUSize returns the negotiated maximum PDU size.
func (a *Association) MaxPDUSize() uint32 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.maxPDUSize
}

// AcceptedContexts returns the map of accepted presentation contexts.
func (a *Association) AcceptedContexts() map[byte]*PresentationContext {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.acceptedContexts
}

// TransferSyntaxFor returns the transfer syntax negotiated for a presentation
// context ID. It returns the empty string when the context was not accepted,
// which callers treat as DICOM's implicit VR little endian default.
func (a *Association) TransferSyntaxFor(contextID byte) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if pc, ok := a.acceptedContexts[contextID]; ok {
		return pc.TransferSyntax
	}
	return ""
}

// RequestAssociation sends an A-ASSOCIATE-RQ and processes the response (SCU side).
func (a *Association) RequestAssociation(ctx context.Context, callingAE, calledAE string,
	contexts []PresentationContextItem, maxPDUSize uint32) error {
	return a.RequestAssociationWithNegotiation(ctx, callingAE, calledAE, contexts, maxPDUSize, nil)
}

// RequestAssociationWithNegotiation sends an A-ASSOCIATE-RQ carrying optional
// extended negotiation items (async operations window, SCP/SCU role selection,
// user identity) and processes the response.
//
// Role selection is required to act as an SCP for a SOP Class on an association
// this AE initiated — notably for C-GET, where the peer sends C-STORE
// sub-operations back over the same association.
func (a *Association) RequestAssociationWithNegotiation(ctx context.Context, callingAE, calledAE string,
	contexts []PresentationContextItem, maxPDUSize uint32, ext *ExtendedNegotiation) error {

	a.mu.Lock()
	if a.state != StateIdle {
		a.mu.Unlock()
		return NewAssociationError("INVALID_STATE", fmt.Sprintf("cannot request association in state %s", a.state))
	}
	a.callingAE = callingAE
	a.calledAE = calledAE
	a.requestedContexts = contexts
	a.state = StateAwaitingAssocResponse
	a.mu.Unlock()

	userInfo := UserInformationItem{
		MaxPDULength:           maxPDUSize,
		ImplementationClassUID: DefaultImplementationClassUID,
		ImplementationVersion:  DefaultImplementationVersionName,
	}
	if ext != nil {
		userInfo.AsyncOperations = ext.AsyncOperations
		userInfo.RoleSelections = ext.RoleSelections
		userInfo.UserIdentity = ext.UserIdentity
	}

	// Build and send A-ASSOCIATE-RQ
	rq := &AssociateRQ{
		ProtocolVersion:       ProtocolVersion,
		CalledAE:              calledAE,
		CallingAE:             callingAE,
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts:  contexts,
		UserInformation:       userInfo,
	}

	if err := a.transport.WritePDU(ctx, rq); err != nil {
		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		return fmt.Errorf("failed to send A-ASSOCIATE-RQ: %w", err)
	}

	// Read response
	pdu, err := a.transport.ReadPDU(ctx)
	if err != nil {
		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		return fmt.Errorf("failed to read association response: %w", err)
	}

	switch resp := pdu.(type) {
	case *AssociateAC:
		a.mu.Lock()
		a.state = StateAssociated

		// Negotiate max PDU size (use the smaller of our and peer's)
		peerMaxPDU := resp.UserInformation.MaxPDULength
		if peerMaxPDU > 0 && (maxPDUSize == 0 || peerMaxPDU < maxPDUSize) {
			a.maxPDUSize = peerMaxPDU
		} else {
			a.maxPDUSize = maxPDUSize
		}
		a.transport.SetMaxPDUSize(a.maxPDUSize)

		// Build accepted contexts map
		a.acceptedContexts = BuildAcceptedContextMap(contexts, resp.PresentationContexts)
		a.peerUserInfo = resp.UserInformation
		a.mu.Unlock()
		return nil

	case *AssociateRJ:
		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		return NewAssociationRejection(resp.Result, resp.Source, resp.Reason)

	case *AbortPDU:
		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		return NewAssociationError("ABORTED", fmt.Sprintf("association aborted by peer: source=%d, reason=%d", resp.Source, resp.Reason))

	default:
		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		return NewAssociationError("UNEXPECTED_PDU", fmt.Sprintf("unexpected PDU type: %T", pdu))
	}
}

// AcceptAssociation handles an incoming A-ASSOCIATE-RQ (SCP side).
func (a *Association) AcceptAssociation(ctx context.Context, rq *AssociateRQ,
	supportedAbstractSyntaxes map[string]bool, supportedTransferSyntaxes map[string]bool,
	maxPDUSize uint32) error {

	a.mu.Lock()
	a.callingAE = rq.CallingAE
	a.calledAE = rq.CalledAE
	a.requestedContexts = rq.PresentationContexts
	a.mu.Unlock()

	// Negotiate presentation contexts
	results := NegotiatePresentationContexts(rq.PresentationContexts, supportedAbstractSyntaxes, supportedTransferSyntaxes)

	// Determine negotiated PDU size
	peerMaxPDU := rq.UserInformation.MaxPDULength
	negotiatedPDU := maxPDUSize
	if peerMaxPDU > 0 && (negotiatedPDU == 0 || peerMaxPDU < negotiatedPDU) {
		negotiatedPDU = peerMaxPDU
	}

	userInfo := UserInformationItem{
		MaxPDULength:           maxPDUSize,
		ImplementationClassUID: DefaultImplementationClassUID,
		ImplementationVersion:  DefaultImplementationVersionName,
	}

	// Acknowledge extended negotiation the peer proposed. Role selection must be
	// echoed for each SOP Class the requestor asked about (PS3.7 D.3.3.4), and
	// the async operations window is confirmed by returning it.
	if rq.UserInformation.AsyncOperations != nil {
		userInfo.AsyncOperations = rq.UserInformation.AsyncOperations
	}
	for _, rs := range rq.UserInformation.RoleSelections {
		// Only confirm roles for SOP Classes actually supported; a role for an
		// unsupported abstract syntax is meaningless.
		if supportedAbstractSyntaxes[rs.SOPClassUID] {
			userInfo.RoleSelections = append(userInfo.RoleSelections, rs)
		}
	}

	// Send A-ASSOCIATE-AC
	ac := &AssociateAC{
		ProtocolVersion:       ProtocolVersion,
		CalledAE:              rq.CalledAE,
		CallingAE:             rq.CallingAE,
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts:  results,
		UserInformation:       userInfo,
	}

	if err := a.transport.WritePDU(ctx, ac); err != nil {
		return fmt.Errorf("failed to send A-ASSOCIATE-AC: %w", err)
	}

	a.mu.Lock()
	a.state = StateAssociated
	a.maxPDUSize = negotiatedPDU
	a.transport.SetMaxPDUSize(negotiatedPDU)
	a.acceptedContexts = BuildAcceptedContextMap(rq.PresentationContexts, results)
	a.peerUserInfo = rq.UserInformation
	a.mu.Unlock()

	return nil
}

// RejectAssociation sends an A-ASSOCIATE-RJ PDU (SCP side).
func (a *Association) RejectAssociation(ctx context.Context, result, source, reason byte) error {
	rj := &AssociateRJ{
		Result: result,
		Source: source,
		Reason: reason,
	}
	if err := a.transport.WritePDU(ctx, rj); err != nil {
		return fmt.Errorf("failed to send A-ASSOCIATE-RJ: %w", err)
	}
	a.mu.Lock()
	a.state = StateIdle
	a.mu.Unlock()
	return nil
}

// Release performs an orderly release of the association.
func (a *Association) Release(ctx context.Context) error {
	a.mu.Lock()
	if a.state != StateAssociated {
		a.mu.Unlock()
		return NewAssociationError("INVALID_STATE", fmt.Sprintf("cannot release in state %s", a.state))
	}
	a.state = StateAwaitingRelease
	a.mu.Unlock()

	if err := a.transport.WritePDU(ctx, &ReleaseRQ{}); err != nil {
		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		return fmt.Errorf("failed to send A-RELEASE-RQ: %w", err)
	}

	pdu, err := a.transport.ReadPDU(ctx)
	if err != nil {
		a.mu.Lock()
		a.state = StateIdle
		a.mu.Unlock()
		return fmt.Errorf("failed to read release response: %w", err)
	}

	a.mu.Lock()
	a.state = StateIdle
	a.mu.Unlock()

	switch pdu.(type) {
	case *ReleaseRP:
		return a.transport.Close()
	case *AbortPDU:
		a.transport.Close()
		return NewAssociationError("ABORTED", "association aborted during release")
	default:
		a.transport.Close()
		return NewAssociationError("UNEXPECTED_PDU", fmt.Sprintf("unexpected PDU during release: %T", pdu))
	}
}

// Abort sends an A-ABORT PDU.
func (a *Association) Abort(ctx context.Context, source, reason byte) error {
	if err := a.transport.WritePDU(ctx, &AbortPDU{Source: source, Reason: reason}); err != nil {
		a.transport.Close()
		return err
	}
	a.mu.Lock()
	a.state = StateIdle
	a.mu.Unlock()
	return a.transport.Close()
}

// SendPData sends data as P-DATA-TF PDUs, fragmenting if necessary.
func (a *Association) SendPData(ctx context.Context, contextID byte, data []byte, isCommand bool) error {
	a.mu.RLock()
	if a.state != StateAssociated {
		a.mu.RUnlock()
		return NewAssociationError("INVALID_STATE", fmt.Sprintf("cannot send data in state %s", a.state))
	}
	maxPDU := a.maxPDUSize
	a.mu.RUnlock()

	// Fragment data into PDUs if needed
	// Each P-DATA-TF PDU has 6-byte header + 4-byte PDV length + 2-byte PDV header
	// So max data per PDU = maxPDU - 6(PDU header) - 4(PDV length) - 2(PDV header) = maxPDU - 12
	maxDataPerPDU := int(maxPDU) - 12
	if maxDataPerPDU <= 0 {
		maxDataPerPDU = 4084 // Fallback minimum
	}

	// An empty payload must still produce one PDV. The peer has already been
	// told a data set follows, so sending nothing leaves it waiting on a read
	// that never completes.
	if len(data) == 0 {
		return a.transport.WritePDU(ctx, &PDataTF{
			PDVItems: []PDVItem{{
				PresentationContextID: contextID,
				IsCommand:             isCommand,
				IsLast:                true,
				Data:                  nil,
			}},
		})
	}

	offset := 0
	for offset < len(data) {
		end := offset + maxDataPerPDU
		isLast := end >= len(data)
		if isLast {
			end = len(data)
		}

		pdu := &PDataTF{
			PDVItems: []PDVItem{
				{
					PresentationContextID: contextID,
					IsCommand:             isCommand,
					IsLast:                isLast,
					Data:                  data[offset:end],
				},
			},
		}

		if err := a.transport.WritePDU(ctx, pdu); err != nil {
			return err
		}
		offset = end
	}

	return nil
}

// ReceivePData reads and reassembles P-DATA-TF PDUs until a complete message is received.
// Returns the context ID, the assembled data, whether it's a command, and any error.
func (a *Association) ReceivePData(ctx context.Context) (byte, []byte, bool, error) {
	a.mu.Lock()
	if a.state != StateAssociated {
		a.mu.Unlock()
		return 0, nil, false, NewAssociationError("INVALID_STATE", fmt.Sprintf("cannot receive data in state %s", a.state))
	}
	// Anything read ahead is delivered before touching the connection, so a
	// message queued by a cancel watcher reaches its real recipient in order.
	if len(a.pending) > 0 {
		msg := a.pending[0]
		a.pending = a.pending[1:]
		a.mu.Unlock()
		return msg.contextID, msg.data, msg.isCommand, nil
	}
	a.mu.Unlock()

	var assembled []byte
	var contextID byte
	var isCommand bool

	for {
		pdu, err := a.transport.ReadPDU(ctx)
		if err != nil {
			return 0, nil, false, err
		}

		dataTF, ok := pdu.(*PDataTF)
		if !ok {
			// Handle non-data PDUs during data transfer
			switch p := pdu.(type) {
			case *AbortPDU:
				a.mu.Lock()
				a.state = StateIdle
				a.mu.Unlock()
				return 0, nil, false, NewAssociationError("ABORTED",
					fmt.Sprintf("association aborted: source=%d, reason=%d", p.Source, p.Reason))
			case *ReleaseRQ:
				// Peer wants to release during data transfer
				_ = a.transport.WritePDU(ctx, &ReleaseRP{})
				a.mu.Lock()
				a.state = StateIdle
				a.mu.Unlock()
				return 0, nil, false, NewAssociationError("RELEASED", "peer released association")
			default:
				return 0, nil, false, NewAssociationError("UNEXPECTED_PDU",
					fmt.Sprintf("expected P-DATA-TF, got %T", pdu))
			}
		}

		for _, pdv := range dataTF.PDVItems {
			contextID = pdv.PresentationContextID
			isCommand = pdv.IsCommand
			assembled = append(assembled, pdv.Data...)

			if pdv.IsLast {
				return contextID, assembled, isCommand, nil
			}
		}
	}
}

// PushBack queues a message to be returned by the next ReceivePData.
//
// It exists for readers that have to consume a message to find out whether it
// is the one they wanted — a C-CANCEL watcher, for instance. Without it such a
// reader must either drop what it did not want, losing a message the peer
// believes it delivered, or not read at all, which is what made cancellation
// undetectable.
//
// Order is preserved: messages come back in the order they were pushed.
func (a *Association) PushBack(contextID byte, data []byte, isCommand bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = append(a.pending, pendingMessage{
		contextID: contextID,
		data:      data,
		isCommand: isCommand,
	})
}
