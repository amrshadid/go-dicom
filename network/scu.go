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
type SCU struct {
	config      SCUConfig
	association *Association
	messageID   atomic.Uint32
	mu          sync.Mutex
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

	return s.association.RequestAssociationWithNegotiation(ctx, s.config.CallingAE, s.config.CalledAE,
		contexts, s.config.Network.MaxPDUSize, s.config.ExtendedNegotiation)
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
	cmdDS := BuildCEchoRQ(s.nextMessageID())
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-ECHO-RQ: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return fmt.Errorf("failed to send C-ECHO-RQ: %w", err)
	}

	// Receive C-ECHO-RSP
	_, respData, isCmd, err := assoc.ReceivePData(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive C-ECHO-RSP: %w", err)
	}
	if !isCmd {
		return NewDIMSEError("UNEXPECTED", "expected command, got data", 0)
	}

	respDS, err := DecodeCommandDataset(respData)
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

	// Build and send C-STORE-RQ command
	cmdDS := BuildCStoreRQ(s.nextMessageID(), sopClassUID, sopInstanceUID, PriorityMedium)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("failed to encode C-STORE-RQ: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return fmt.Errorf("failed to send C-STORE-RQ command: %w", err)
	}

	// Send data set
	dataBytes, err := EncodeDataset(ds, assoc.TransferSyntaxFor(pcID))
	if err != nil {
		return fmt.Errorf("failed to encode dataset: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
		return fmt.Errorf("failed to send C-STORE data: %w", err)
	}

	// Receive C-STORE-RSP
	_, respData, isCmd, err := assoc.ReceivePData(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive C-STORE-RSP: %w", err)
	}
	if !isCmd {
		return NewDIMSEError("UNEXPECTED", "expected command response, got data", 0)
	}

	respDS, err := DecodeCommandDataset(respData)
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
	// Build and send C-FIND-RQ
	cmdDS := BuildCFindRQ(s.nextMessageID(), sopClassUID, PriorityMedium)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return nil, fmt.Errorf("failed to encode C-FIND-RQ: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, cmdBytes, true); err != nil {
		return nil, fmt.Errorf("failed to send C-FIND-RQ command: %w", err)
	}

	// Send query dataset
	dataBytes, err := EncodeDataset(queryDS, assoc.TransferSyntaxFor(pcID))
	if err != nil {
		return nil, fmt.Errorf("failed to encode query dataset: %w", err)
	}

	if err := assoc.SendPData(ctx, pcID, dataBytes, false); err != nil {
		return nil, fmt.Errorf("failed to send C-FIND query data: %w", err)
	}

	// Stream results on channel
	results := make(chan *CFindResult, 16)
	go s.receiveFindResults(ctx, assoc, results)
	return results, nil
}

// CFindResult wraps a find result or error.
type CFindResult struct {
	DataSet *dataset.Dataset
	Err     error
}

func (s *SCU) receiveFindResults(ctx context.Context, assoc *Association, results chan<- *CFindResult) {
	defer close(results)

	for {
		// Receive command
		_, cmdData, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			results <- &CFindResult{Err: err}
			return
		}
		if !isCmd {
			results <- &CFindResult{Err: NewDIMSEError("UNEXPECTED", "expected command, got data", 0)}
			return
		}

		cmdDS, err := DecodeCommandDataset(cmdData)
		if err != nil {
			results <- &CFindResult{Err: err}
			return
		}

		_, _, status, err := ParseCommandDataset(cmdDS)
		if err != nil {
			results <- &CFindResult{Err: err}
			return
		}

		if IsPending(status) {
			// Receive the result dataset
			resultCtxID, resultData, isCmd, err := assoc.ReceivePData(ctx)
			if err != nil {
				results <- &CFindResult{Err: err}
				return
			}
			if isCmd {
				results <- &CFindResult{Err: NewDIMSEError("UNEXPECTED", "expected data, got command", 0)}
				return
			}

			resultDS, err := DecodeDataset(resultData, assoc.TransferSyntaxFor(resultCtxID))
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
		} else {
			// Final status (success, failure, or cancel)
			if status != StatusSuccess {
				if status != StatusCancel {
					results <- &CFindResult{Err: NewDIMSEError("FIND_FAILED",
						fmt.Sprintf("C-FIND failed with status 0x%04X", status), status)}
				}
			}
			return
		}
	}
}

// Move performs a C-MOVE request.
func (s *SCU) Move(ctx context.Context, queryDS *dataset.Dataset, moveDestination string) error {
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
