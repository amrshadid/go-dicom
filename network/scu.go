package network

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// SCU (Service Class User) is a DICOM network client.
// SCU is a DICOM Service Class User: the client that initiates an association
// and issues operations over it.
//
// # Concurrency
//
// An SCU is safe for concurrent use. Operations on one are serialized, so several
// goroutines may share it and each waits its turn rather than reading another's
// response.
//
// That matters because the alternative was one association per goroutine, and
// association setup is the expensive part of talking to a PACS — TCP, TLS
// handshake, then negotiation. A caller sending a thousand instances across eight
// workers paid for eight associations where one would do, and a PACS with an
// association limit may refuse the rest.
//
// Serialized is not pipelined: one operation is in flight at a time, so sharing an
// SCU bounds the number of associations rather than increasing throughput. See
// [AsynchronousOperationsWindow] for what is negotiated about that.
//
// Two methods are deliberately outside the serialization. [SCU.Cancel] must reach
// the peer while the operation it cancels is still running, and [SCU.Release] and
// [SCU.Abort] must not wait behind a [SCU.Find] whose results have not been
// drained.
type SCU struct {
	config      SCUConfig
	association *Association
	messageID   atomic.Uint32
	mu          sync.Mutex

	// sendMu serializes the sending of one complete message — its command and the
	// data set that follows.
	//
	// Not the whole operation: an operation waiting for its response holds nothing,
	// which is what lets several be outstanding at once. What it prevents is two
	// senders interleaving the PDVs of different messages on one presentation
	// context, which PS3.8 does not allow and which no reader could untangle.
	sendMu sync.Mutex

	// slots bounds how many operations are outstanding, from the negotiated
	// asynchronous operations window. Nil for an unlimited window.
	slots *operationSlots

	// gate separates the operations that can overlap from the two that cannot.
	//
	// Echo, Store, Find and the N-services take it for reading: each waits for its
	// own response by message ID, so several may be outstanding at once, bounded by
	// the window.
	//
	// C-MOVE and C-GET take it for writing, exclusively. Both interleave other
	// traffic on the association — a C-GET receives C-STORE sub-operation requests
	// between its own responses, and a C-MOVE has a cancel watcher reading the
	// association while nothing else does — so allowing another operation alongside
	// them would have two readers competing for messages neither can attribute.
	gate sync.RWMutex
}

// sendMessage writes one command and its optional data set as a unit.
//
// The lock spans both: a command that says a data set follows must be followed by
// that data set on the wire, with nothing of another message's between.
func (s *SCU) sendMessage(ctx context.Context, assoc *Association, contextID byte,
	command []byte, dataSet []byte, hasDataSet bool) error {

	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if err := assoc.SendPData(ctx, contextID, command, true); err != nil {
		return err
	}
	if !hasDataSet {
		return nil
	}
	return assoc.SendPData(ctx, contextID, dataSet, false)
}

// beginOperation reserves a slot in the negotiated window for an operation that
// can overlap with others, blocking while the window is full.
//
// The returned function releases the slot and must be called exactly once.
func (s *SCU) beginOperation(ctx context.Context) (func(), error) {
	s.gate.RLock()

	s.mu.Lock()
	slots := s.slots
	s.mu.Unlock()

	if err := slots.acquire(ctx); err != nil {
		s.gate.RUnlock()
		return nil, err
	}

	return func() {
		slots.release()
		s.gate.RUnlock()
	}, nil
}

// beginExclusiveOperation reserves the association for a C-MOVE or C-GET, waiting
// for every overlapping operation to finish first.
//
// Exclusive because both interleave traffic that is not their own response on the
// association: a C-GET receives C-STORE sub-operation requests, and a C-MOVE has a
// cancel watcher reading it. Two readers with different ideas of what belongs to
// them is how a retrieval loses a sub-operation.
func (s *SCU) beginExclusiveOperation(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.gate.Lock()
	return s.gate.Unlock, nil
}

// NewSCU creates a new SCU with the given configuration.
func NewSCU(config SCUConfig) *SCU {
	config.applyDefaults()
	return &SCU{config: config}
}

// nextMessageID returns the next message ID.
func (s *SCU) nextMessageID() uint16 {
	return uint16(s.messageID.Add(1))
}

// PeekNextMessageID returns the ID the next operation will use, without
// consuming it.
//
// C-CANCEL names the message it cancels, so a caller that wants to cancel an
// operation needs its ID — and the operations here allocate one internally. The
// alternative is to have each return its ID, which changes every signature for
// the sake of the rare caller that cancels.
//
// Peeking races with a concurrent operation on the same SCU, which would take
// the ID first. Cancellation is only meaningful against an operation the caller
// started and is still waiting on, so the sequence to use is: peek, start, then
// cancel that ID.
func (s *SCU) PeekNextMessageID() uint16 {
	return uint16(s.messageID.Load() + 1)
}

// Associate establishes an association with the SCP, proposing the given presentation contexts.
// If contexts is nil, default verification + storage + query/retrieve contexts are proposed.
func (s *SCU) Associate(ctx context.Context, contexts []PresentationContextItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.association != nil && s.association.State() == StateAssociated {
		return NewAssociationError("ALREADY_ASSOCIATED", "already associated")
	}

	transport, err := Dial(ctx, s.config.Address, s.config.Network.NetworkTimeout)
	if err != nil {
		return err
	}

	s.association = NewAssociation(transport)

	if contexts == nil {
		contexts = s.defaultContexts()
	}

	if err := s.association.RequestAssociationWithNegotiation(ctx, s.config.CallingAE,
		s.config.CalledAE, contexts, s.config.Network.MaxPDUSize,
		s.config.ExtendedNegotiation); err != nil {
		return err
	}

	// How many operations may be outstanding comes from what was negotiated, not
	// from what was asked for: the peer's A-ASSOCIATE-AC is the agreement.
	//
	// One when nothing was negotiated, which is what the standard means by the
	// absence of the item and what every association did before pipelining. A
	// caller who wants more asks for it and the peer has to agree.
	window := uint16(1)
	if agreed := s.association.PeerUserInformation().AsyncOperations; agreed != nil {
		window = agreed.MaxOperationsInvoked
	}
	s.slots = newOperationSlots(window)

	return nil
}

// defaultContexts builds the default set of presentation contexts.
func (s *SCU) defaultContexts() []PresentationContextItem {
	var contexts []PresentationContextItem

	// Always include verification
	contexts = append(contexts, DefaultVerificationContexts()...)

	// Include storage and query/retrieve
	storageCtx := DefaultStorageContexts()
	qrCtx := DefaultQueryRetrieveContexts()

	// Storage Commitment travels with storage: a requestor asks for commitment
	// on instances it has just stored, and proposing it here saves the caller
	// from hand-building contexts for the common case.
	commitCtx := StorageCommitmentPresentationContexts()

	// Re-number IDs to be unique odd numbers
	id := byte(1)
	for i := range contexts {
		contexts[i].ID = id
		id += 2
	}
	for i := range storageCtx {
		storageCtx[i].ID = id
		id += 2
	}
	for i := range qrCtx {
		qrCtx[i].ID = id
		id += 2
	}

	for i := range commitCtx {
		commitCtx[i].ID = id
		id += 2
	}

	contexts = append(contexts, storageCtx...)
	contexts = append(contexts, qrCtx...)
	contexts = append(contexts, commitCtx...)
	return contexts
}

// Echo performs a C-ECHO verification.
func (s *SCU) Echo(ctx context.Context) error {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer release()

	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	// Find presentation context for Verification
	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), VerificationSOPClassUID)
	if !ok {
		return NewAssociationError("NO_CONTEXT",
			fmt.Sprintf("no accepted presentation context for the Verification SOP Class: %s",
				assoc.ExplainNoContextFor(VerificationSOPClassUID)))
	}

	// Build and send C-ECHO-RQ
	messageID := s.nextMessageID()
	cmdDS := BuildCEchoRQ(messageID)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-ECHO-RQ: %w", err)
	}

	if err := s.sendMessage(ctx, assoc, pcID, cmdBytes, nil, false); err != nil {
		return fmt.Errorf("failed to send C-ECHO-RQ: %w", err)
	}

	// Receive this operation's C-ECHO-RSP, by message ID: with another operation
	// outstanding the next message on the wire may not be ours.
	msg, err := assoc.ReceiveResponse(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to receive C-ECHO-RSP: %w", err)
	}

	respDS, err := DecodeCommandDataset(msg.command)
	if err != nil {
		return fmt.Errorf("failed to decode C-ECHO-RSP: %w", err)
	}

	_, _, status, err := ParseCommandDataset(respDS)
	if err != nil {
		return err
	}

	if status != StatusSuccess {
		return NewDIMSEError("ECHO_FAILED", fmt.Sprintf("C-ECHO failed with status 0x%04X", status), status)
	}

	return nil
}

// Store sends a DICOM dataset to the SCP using C-STORE.
func (s *SCU) Store(ctx context.Context, ds *dataset.Dataset) error {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer release()

	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	// Extract SOP Class and Instance UIDs from the dataset
	sopClassElem, ok := ds.Get(tag.New(0x0008, 0x0016)) // SOPClassUID
	if !ok {
		return fmt.Errorf("dataset missing SOP Class UID (0008,0016)")
	}
	sopClassUID := extractStringValue(sopClassElem.GetValue())

	sopInstanceElem, ok := ds.Get(tag.New(0x0008, 0x0018)) // SOPInstanceUID
	if !ok {
		return fmt.Errorf("dataset missing SOP Instance UID (0008,0018)")
	}
	sopInstanceUID := extractStringValue(sopInstanceElem.GetValue())

	// Find presentation context
	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return NewAssociationError("NO_CONTEXT",
			fmt.Sprintf("no accepted presentation context for SOP Class %s: %s",
				sopClassUID, assoc.ExplainNoContextFor(sopClassUID)))
	}

	// Build the command and the data set, then send them as one message: the
	// command says a data set follows, so nothing of another message's may come
	// between them on this context.
	messageID := s.nextMessageID()
	cmdDS := BuildCStoreRQ(messageID, sopClassUID, sopInstanceUID, PriorityMedium)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-STORE-RQ: %w", err)
	}

	dataBytes, err := EncodeDataset(ds, assoc.TransferSyntaxFor(pcID))
	if err != nil {
		return fmt.Errorf("failed to encode dataset: %w", err)
	}

	if err := s.sendMessage(ctx, assoc, pcID, cmdBytes, dataBytes, true); err != nil {
		return fmt.Errorf("failed to send C-STORE-RQ: %w", err)
	}

	// This operation's response, by message ID.
	msg, err := assoc.ReceiveResponse(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to receive C-STORE-RSP: %w", err)
	}

	respDS, err := DecodeCommandDataset(msg.command)
	if err != nil {
		return fmt.Errorf("failed to decode C-STORE-RSP: %w", err)
	}

	_, _, status, err := ParseCommandDataset(respDS)
	if err != nil {
		return err
	}

	if status != StatusSuccess && status != StatusWarning {
		return NewDIMSEError("STORE_FAILED",
			fmt.Sprintf("C-STORE failed with status 0x%04X", status), status)
	}

	return nil
}

// Find performs a C-FIND query and returns results on a channel.
//
// The information model is chosen from what the peer accepted, trying Patient
// Root, then Study Root, then the retired Patient/Study Only, then Modality
// Worklist. Use FindWithSOPClass to name one, which is the right call whenever
// more than one was negotiated: these models answer different questions, and
// picking by availability is a convenience for the common case rather than a
// substitute for saying what you meant.
func (s *SCU) Find(ctx context.Context, queryDS *dataset.Dataset) (<-chan *CFindResult, error) {
	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	// Modality Worklist is last because it answers a different question from
	// the three Q/R models. It is in the chain at all because an association
	// that accepted only the worklist context leaves no ambiguity about what a
	// query on it can mean — and refusing would make the worklist service
	// unreachable through this method, which is how it came to be unusable:
	// the handler, the SOP class and the presentation contexts all existed,
	// and no caller could get a query to them.
	for _, candidate := range []string{
		PatientRootQueryRetrieveFind,
		StudyRootQueryRetrieveFind,
		PatientStudyOnlyQueryRetrieveFind,
		ModalityWorklistInformationModelFindUID,
	} {
		if pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), candidate); ok {
			return s.findOnContext(ctx, assoc, queryDS, candidate, pcID)
		}
	}
	return nil, NewAssociationError("NO_CONTEXT",
		fmt.Sprintf("no accepted presentation context for C-FIND — %s",
			assoc.ExplainNoContextForAny(
				PatientRootQueryRetrieveFind,
				StudyRootQueryRetrieveFind,
				PatientStudyOnlyQueryRetrieveFind,
				ModalityWorklistInformationModelFindUID)))
}

// FindWithSOPClass performs a C-FIND against a named information model.
//
// Use this rather than Find when the peer accepted more than one model, since
// the models answer different questions: a Patient Root query and a Modality
// Worklist query are not interchangeable, and letting availability decide makes
// the result depend on what the peer happened to offer.
func (s *SCU) FindWithSOPClass(ctx context.Context, sopClassUID string,
	queryDS *dataset.Dataset) (<-chan *CFindResult, error) {

	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return nil, NewAssociationError("NO_CONTEXT",
			fmt.Sprintf("the peer did not accept a context for %s", sopClassUID))
	}
	return s.findOnContext(ctx, assoc, queryDS, sopClassUID, pcID)
}

// findOnContext issues the C-FIND once a model and context have been chosen.
func (s *SCU) findOnContext(ctx context.Context, assoc *Association, queryDS *dataset.Dataset,
	sopClassUID string, pcID byte) (<-chan *CFindResult, error) {

	// A slot is held until the results have been streamed, not just until this
	// returns: a C-FIND is not finished when the channel is handed back. Every
	// early return below has to release it, which is why it is not a defer.
	release, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}

	// Build and send C-FIND-RQ
	messageID := s.nextMessageID()
	cmdDS := BuildCFindRQ(messageID, sopClassUID, PriorityMedium)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		release()
		return nil, fmt.Errorf("failed to encode C-FIND-RQ: %w", err)
	}

	dataBytes, err := EncodeDataset(queryDS, assoc.TransferSyntaxFor(pcID))
	if err != nil {
		release()
		return nil, fmt.Errorf("failed to encode query dataset: %w", err)
	}

	if err := s.sendMessage(ctx, assoc, pcID, cmdBytes, dataBytes, true); err != nil {
		release()
		return nil, fmt.Errorf("failed to send C-FIND-RQ: %w", err)
	}

	// Stream results on channel.
	//
	// The slot is released by the streaming goroutine, not here: the operation is
	// outstanding until its final status arrives, and the negotiated window counts
	// it until then.
	results := make(chan *CFindResult, 16)
	go func() {
		defer release()
		s.receiveFindResults(ctx, assoc, messageID, results)
	}()
	return results, nil
}

// CFindResult wraps a find result or error.
type CFindResult struct {
	DataSet *dataset.Dataset
	Err     error
}

func (s *SCU) receiveFindResults(ctx context.Context, assoc *Association, messageID uint16,
	results chan<- *CFindResult) {
	defer close(results)

	// Every response is taken by message ID rather than by reading whatever comes
	// next. A C-FIND's ID stays live across all of its pending responses, and with
	// another operation outstanding the next message on the wire may be that
	// operation's — reading it here would deliver a store response as a match.
	for {
		msg, err := assoc.ReceiveResponse(ctx, messageID)
		if err != nil {
			results <- &CFindResult{Err: err}
			return
		}

		cmdDS, err := DecodeCommandDataset(msg.command)
		if err != nil {
			results <- &CFindResult{Err: err}
			return
		}

		_, _, status, err := ParseCommandDataset(cmdDS)
		if err != nil {
			results <- &CFindResult{Err: err}
			return
		}

		if !IsPending(status) {
			// Final status: success, failure, or cancel.
			if status != StatusSuccess && status != StatusCancel {
				results <- &CFindResult{Err: NewDIMSEError("FIND_FAILED",
					fmt.Sprintf("C-FIND failed with status 0x%04X", status), status)}
			}
			return
		}

		if !msg.hasDataSet {
			results <- &CFindResult{Err: NewDIMSEError("UNEXPECTED",
				"a pending C-FIND response carried no identifier", status)}
			return
		}

		resultDS, err := DecodeDataset(msg.dataSet, assoc.TransferSyntaxFor(msg.contextID))
		if err != nil {
			results <- &CFindResult{Err: err}
			return
		}

		select {
		case results <- &CFindResult{DataSet: resultDS}:
		case <-ctx.Done():
			results <- &CFindResult{Err: ctx.Err()}
			return
		}
	}
}

// Move performs a C-MOVE request.
func (s *SCU) Move(ctx context.Context, queryDS *dataset.Dataset, moveDestination string) error {
	release, err := s.beginExclusiveOperation(ctx)
	if err != nil {
		return err
	}
	defer release()

	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	sopClassUID := PatientRootQueryRetrieveMove
	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		sopClassUID = StudyRootQueryRetrieveMove
		pcID, ok = FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
		if !ok {
			// Patient/Study Only is retired but still served by some archives.
			sopClassUID = PatientStudyOnlyQueryRetrieveMove
			pcID, ok = FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
		}
		if !ok {
			return NewAssociationError("NO_CONTEXT",
				fmt.Sprintf("no accepted presentation context for C-MOVE — %s",
					assoc.ExplainNoContextForAny(
						PatientRootQueryRetrieveMove,
						StudyRootQueryRetrieveMove,
						PatientStudyOnlyQueryRetrieveMove)))
		}
	}

	cmdDS := BuildCMoveRQ(s.nextMessageID(), sopClassUID, moveDestination, PriorityMedium)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-MOVE-RQ: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return fmt.Errorf("failed to send C-MOVE-RQ command: %w", err)
	}

	dataBytes, err := EncodeDataset(queryDS, assoc.TransferSyntaxFor(pcID))
	if err != nil {
		return fmt.Errorf("failed to encode query dataset: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
		return fmt.Errorf("failed to send C-MOVE query data: %w", err)
	}

	// Wait for final response (may receive pending status updates)
	for {
		_, cmdData, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			return err
		}
		if !isCmd {
			continue // Skip data PDUs (sub-operations handled by destination SCP)
		}

		respDS, err := DecodeCommandDataset(cmdData)
		if err != nil {
			return fmt.Errorf("failed to decode C-MOVE-RSP: %w", err)
		}

		_, _, status, err := ParseCommandDataset(respDS)
		if err != nil {
			return err
		}

		if !IsPending(status) {
			if status != StatusSuccess && status != StatusWarning {
				return NewDIMSEError("MOVE_FAILED",
					fmt.Sprintf("C-MOVE failed with status 0x%04X", status), status)
			}
			return nil
		}
	}
}

// Get performs a C-GET request (retrieve objects on the same association).
func (s *SCU) Get(ctx context.Context, queryDS *dataset.Dataset) error {
	release, err := s.beginExclusiveOperation(ctx)
	if err != nil {
		return err
	}
	defer release()

	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	sopClassUID := PatientRootQueryRetrieveGet
	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		sopClassUID = StudyRootQueryRetrieveGet
		pcID, ok = FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
		if !ok {
			// Patient/Study Only is retired but still served by some archives.
			sopClassUID = PatientStudyOnlyQueryRetrieveGet
			pcID, ok = FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
		}
		if !ok {
			return NewAssociationError("NO_CONTEXT",
				fmt.Sprintf("no accepted presentation context for C-GET — %s",
					assoc.ExplainNoContextForAny(
						PatientRootQueryRetrieveGet,
						StudyRootQueryRetrieveGet,
						PatientStudyOnlyQueryRetrieveGet)))
		}
	}

	cmdDS := BuildCGetRQ(s.nextMessageID(), sopClassUID, PriorityMedium)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-GET-RQ: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return fmt.Errorf("failed to send C-GET-RQ command: %w", err)
	}

	dataBytes, err := EncodeDataset(queryDS, assoc.TransferSyntaxFor(pcID))
	if err != nil {
		return fmt.Errorf("failed to encode query dataset: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
		return fmt.Errorf("failed to send C-GET query data: %w", err)
	}

	// The peer interleaves two kinds of message on this association: C-STORE-RQ
	// sub-operations carrying the retrieved instances, and C-GET-RSP messages
	// reporting progress. Dispatch on the command field rather than assuming.
	for {
		subCtxID, cmdData, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			return err
		}
		if !isCmd {
			// A data set with no preceding command is not something we can
			// attribute; skip it rather than misreading it as a response.
			continue
		}

		cmdDS, err := DecodeCommandDataset(cmdData)
		if err != nil {
			return fmt.Errorf("failed to decode C-GET response: %w", err)
		}

		commandField, subMessageID, status, err := ParseCommandDataset(cmdDS)
		if err != nil {
			return err
		}

		if commandField == CommandCStoreRQ {
			if err := s.handleGetSubOperation(ctx, assoc, subCtxID, subMessageID, cmdDS); err != nil {
				return err
			}
			continue
		}

		if !IsPending(status) {
			if status != StatusSuccess && status != StatusWarning &&
				status != StatusGetWarningPartial {
				return NewDIMSEError("GET_FAILED",
					fmt.Sprintf("C-GET failed with status 0x%04X", status), status)
			}
			return nil
		}
	}
}

// handleGetSubOperation receives one instance pushed by the peer during a
// C-GET and answers it with a C-STORE-RSP. Failing to respond would stall the
// peer, which waits for each sub-operation to be acknowledged.
func (s *SCU) handleGetSubOperation(ctx context.Context, assoc *Association,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) error {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)

	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			return fmt.Errorf("failed to receive C-STORE sub-operation data: %w", err)
		}
		if isCmd {
			return NewDIMSEError("UNEXPECTED",
				"expected a data set for the C-STORE sub-operation, got a command", 0)
		}
		ds, err = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
		if err != nil {
			// Report the failure to the peer rather than dropping the
			// association; the remaining instances may still be usable.
			return s.respondToSubOperation(ctx, assoc, ctxID, messageID,
				sopClassUID, sopInstanceUID, StatusUnableToProcess)
		}
	}

	status := StatusSuccess
	if s.config.OnCStore != nil {
		status = s.config.OnCStore(ctx, sopClassUID, sopInstanceUID, ds)
	}

	return s.respondToSubOperation(ctx, assoc, ctxID, messageID,
		sopClassUID, sopInstanceUID, status)
}

// respondToSubOperation sends the C-STORE-RSP for a received sub-operation.
func (s *SCU) respondToSubOperation(ctx context.Context, assoc *Association, ctxID byte,
	messageID uint16, sopClassUID, sopInstanceUID string, status uint16) error {

	rspDS := BuildCStoreRSP(messageID, sopClassUID, sopInstanceUID, status)
	rspBytes, err := EncodeCommandDataset(rspDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-STORE-RSP: %w", err)
	}
	return assoc.SendPData(ctx, ctxID, rspBytes, true)
}

// Cancel sends a C-CANCEL-RQ to cancel an in-progress C-FIND, C-MOVE, or C-GET.
func (s *SCU) Cancel(ctx context.Context, messageID uint16) error {
	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	cmdDS := BuildCCancelRQ(messageID)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-CANCEL-RQ: %w", err)
	}

	// Send on any accepted presentation context
	var pcID byte
	for id := range assoc.AcceptedContexts() {
		pcID = id
		break
	}

	return assoc.SendPData(ctx, pcID, cmdBytes, true)
}

// NEventReport sends an N-EVENT-REPORT-RQ.
func (s *SCU) NEventReport(ctx context.Context, sopClassUID, sopInstanceUID string, eventTypeID uint16, ds *dataset.Dataset) (*NEventReportResponse, error) {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	assoc := s.getAssociation()
	if assoc == nil {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return nil, NewAssociationError("NO_CONTEXT", fmt.Sprintf("no context for %s", sopClassUID))
	}

	hasDS := ds != nil
	messageID := s.nextMessageID()
	cmdDS := BuildNEventReportRQ(messageID, sopClassUID, sopInstanceUID, eventTypeID, hasDS)
	cmdBytes, _ := EncodeCommandDataset(cmdDS)
	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return nil, err
	}
	if hasDS {
		dataBytes, _ := EncodeDataset(ds, assoc.TransferSyntaxFor(pcID))
		if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
			return nil, err
		}
	}

	_, respData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	respDS, _ := DecodeCommandDataset(respData)
	_, _, status, _ := ParseCommandDataset(respDS)

	return &NEventReportResponse{
		MessageIDRespondedTo: messageID,
		AffectedSOPClass:     sopClassUID,
		AffectedSOPInstance:  sopInstanceUID,
		EventTypeID:          eventTypeID,
		Status:               status,
	}, nil
}

// NGet sends an N-GET-RQ.
func (s *SCU) NGet(ctx context.Context, sopClassUID, sopInstanceUID string) (*NGetResponse, error) {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	assoc := s.getAssociation()
	if assoc == nil {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return nil, NewAssociationError("NO_CONTEXT", fmt.Sprintf("no context for %s", sopClassUID))
	}

	cmdDS := BuildNGetRQ(s.nextMessageID(), sopClassUID, sopInstanceUID)
	cmdBytes, _ := EncodeCommandDataset(cmdDS)
	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return nil, err
	}

	_, respData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	respDS, _ := DecodeCommandDataset(respData)
	_, _, status, _ := ParseCommandDataset(respDS)

	resp := &NGetResponse{
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		Status:              status,
	}

	// Check if data set follows
	if HasDataSet(respDS) {
		dsCtxID, dsData, _, err := assoc.ReceivePData(ctx)
		if err == nil {
			resp.DataSet, _ = DecodeDataset(dsData, assoc.TransferSyntaxFor(dsCtxID))
		}
	}

	return resp, nil
}

// NSet sends an N-SET-RQ.
func (s *SCU) NSet(ctx context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) (*NSetResponse, error) {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	assoc := s.getAssociation()
	if assoc == nil {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return nil, NewAssociationError("NO_CONTEXT", fmt.Sprintf("no context for %s", sopClassUID))
	}

	cmdDS := BuildNSetRQ(s.nextMessageID(), sopClassUID, sopInstanceUID)
	cmdBytes, _ := EncodeCommandDataset(cmdDS)
	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return nil, err
	}
	dataBytes, _ := EncodeDataset(ds, assoc.TransferSyntaxFor(pcID))
	if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
		return nil, err
	}

	_, respData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	respDS, _ := DecodeCommandDataset(respData)
	_, _, status, _ := ParseCommandDataset(respDS)

	return &NSetResponse{
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		Status:              status,
	}, nil
}

// NAction sends an N-ACTION-RQ.
func (s *SCU) NAction(ctx context.Context, sopClassUID, sopInstanceUID string, actionTypeID uint16, ds *dataset.Dataset) (*NActionResponse, error) {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	assoc := s.getAssociation()
	if assoc == nil {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return nil, NewAssociationError("NO_CONTEXT", fmt.Sprintf("no context for %s", sopClassUID))
	}

	hasDS := ds != nil
	cmdDS := BuildNActionRQ(s.nextMessageID(), sopClassUID, sopInstanceUID, actionTypeID, hasDS)
	cmdBytes, _ := EncodeCommandDataset(cmdDS)
	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return nil, err
	}
	if hasDS {
		dataBytes, _ := EncodeDataset(ds, assoc.TransferSyntaxFor(pcID))
		if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
			return nil, err
		}
	}

	_, respData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	respDS, _ := DecodeCommandDataset(respData)
	_, _, status, _ := ParseCommandDataset(respDS)

	return &NActionResponse{
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		ActionTypeID:        actionTypeID,
		Status:              status,
	}, nil
}

// NCreate sends an N-CREATE-RQ.
func (s *SCU) NCreate(ctx context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) (*NCreateResponse, error) {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	assoc := s.getAssociation()
	if assoc == nil {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return nil, NewAssociationError("NO_CONTEXT", fmt.Sprintf("no context for %s", sopClassUID))
	}

	hasDS := ds != nil
	cmdDS := BuildNCreateRQ(s.nextMessageID(), sopClassUID, sopInstanceUID, hasDS)
	cmdBytes, _ := EncodeCommandDataset(cmdDS)
	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return nil, err
	}
	if hasDS {
		dataBytes, _ := EncodeDataset(ds, assoc.TransferSyntaxFor(pcID))
		if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
			return nil, err
		}
	}

	_, respData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	respDS, _ := DecodeCommandDataset(respData)
	_, _, status, _ := ParseCommandDataset(respDS)

	return &NCreateResponse{
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		Status:              status,
	}, nil
}

// NDelete sends an N-DELETE-RQ.
func (s *SCU) NDelete(ctx context.Context, sopClassUID, sopInstanceUID string) (*NDeleteResponse, error) {
	release, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	assoc := s.getAssociation()
	if assoc == nil {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := FindPresentationContextID(assoc.AcceptedContexts(), sopClassUID)
	if !ok {
		return nil, NewAssociationError("NO_CONTEXT", fmt.Sprintf("no context for %s", sopClassUID))
	}

	cmdDS := BuildNDeleteRQ(s.nextMessageID(), sopClassUID, sopInstanceUID)
	cmdBytes, _ := EncodeCommandDataset(cmdDS)
	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return nil, err
	}

	_, respData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	respDS, _ := DecodeCommandDataset(respData)
	_, _, status, _ := ParseCommandDataset(respDS)

	return &NDeleteResponse{
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		Status:              status,
	}, nil
}

// Association returns the SCU's current association, or nil when not
// associated. Use it to inspect what was negotiated — accepted presentation
// contexts, the agreed transfer syntax per context, and any extended
// negotiation the peer returned.
func (s *SCU) Association() *Association {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.association
}

// getAssociation returns the current association or nil.
func (s *SCU) getAssociation() *Association {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.association != nil && s.association.State() == StateAssociated {
		return s.association
	}
	return nil
}

// Release performs an orderly release of the association.
func (s *SCU) Release(ctx context.Context) error {
	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil {
		return nil
	}

	err := assoc.Release(ctx)
	s.mu.Lock()
	s.association = nil
	s.mu.Unlock()
	return err
}

// Abort sends an A-ABORT and closes the connection.
func (s *SCU) Abort(ctx context.Context) error {
	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil {
		return nil
	}

	err := assoc.Abort(ctx, AbortSourceServiceUser, 0)
	s.mu.Lock()
	s.association = nil
	s.mu.Unlock()
	return err
}

// IsAssociated returns whether the SCU has an active association.
func (s *SCU) IsAssociated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.association != nil && s.association.State() == StateAssociated
}

// Helper functions

// extractStringValue reads a string value, removing the padding DICOM adds to
// make every value an even number of bytes.
//
// The padding character depends on the VR: UI pads with NUL, while AE and the
// other text VRs pad with a space. Both are stripped, because a value of odd
// length is padded on the wire and the padding is not part of the value —
// PS3.5 Section 6.2 makes trailing spaces non-significant for AE. Trimming only
// NUL left every odd-length AE title with a trailing space, so a C-MOVE to a
// destination whose title had an odd number of characters never matched.
func extractStringValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		return strings.TrimRight(v, "\x00 ")
	case []byte:
		return strings.TrimRight(string(v), "\x00 ")
	default:
		return fmt.Sprintf("%v", v)
	}
}
