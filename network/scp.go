package network

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/amrshadid/go-dicom/dataset"
)

// SCP (Service Class Provider) is a DICOM network server.
type SCP struct {
	config   SCPConfig
	handler  Handler
	listener *Listener
	mu       sync.RWMutex
	wg       sync.WaitGroup
	running  bool

	// Supported abstract syntaxes and transfer syntaxes
	supportedAbstractSyntaxes map[string]bool
	supportedTransferSyntaxes map[string]bool

	// assocSlots bounds concurrent associations when MaxAssociations > 0.
	// Nil means unlimited.
	assocSlots chan struct{}

	// messageID numbers messages this SCP originates, which it does when a
	// service requires it to send a request of its own — the N-EVENT-REPORT
	// carrying a storage commitment result, for instance. Incremented
	// atomically because associations are handled concurrently.
	messageID atomic.Uint32
}

// nextMessageID returns the next message ID for a request this SCP originates.
//
// DICOM message IDs are 16-bit, so the counter is taken modulo 2^16. Starting
// at 1 rather than 0 follows the convention used by the SCU side.
func (s *SCP) nextMessageID() uint16 {
	return uint16(s.messageID.Add(1) & 0xFFFF)
}

// NewSCP creates a new SCP with the given configuration.
func NewSCP(config SCPConfig) *SCP {
	config.applyDefaults()
	scp := &SCP{
		config:                    config,
		handler:                   &BaseHandler{},
		supportedAbstractSyntaxes: defaultSupportedAbstractSyntaxes(),
		supportedTransferSyntaxes: defaultSupportedTransferSyntaxes(),
	}
	if config.MaxAssociations > 0 {
		scp.assocSlots = make(chan struct{}, config.MaxAssociations)
	}
	return scp
}

// SetHandler sets the handler for incoming DIMSE requests.
func (s *SCP) SetHandler(handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = handler
}

// SetSupportedAbstractSyntaxes sets the abstract syntaxes this SCP supports.
func (s *SCP) SetSupportedAbstractSyntaxes(syntaxes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.supportedAbstractSyntaxes = make(map[string]bool, len(syntaxes))
	for _, syn := range syntaxes {
		s.supportedAbstractSyntaxes[syn] = true
	}
}

// SetSupportedTransferSyntaxes sets the transfer syntaxes this SCP supports.
func (s *SCP) SetSupportedTransferSyntaxes(syntaxes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.supportedTransferSyntaxes = make(map[string]bool, len(syntaxes))
	for _, syn := range syntaxes {
		s.supportedTransferSyntaxes[syn] = true
	}
}

// ListenAndServe starts the SCP server, listening for incoming associations.
// It blocks until the context is canceled.
func (s *SCP) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.BindAddress, s.config.Port)

	ln, err := Listen(addr)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.listener = ln
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		ln.Close()
		s.wg.Wait()
	}()

	for {
		transport, err := ln.Accept(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}

		// Bound concurrent associations when configured. Rejecting at the
		// transport level keeps an unbounded number of peers from each
		// consuming a goroutine and an open socket.
		if s.assocSlots != nil {
			select {
			case s.assocSlots <- struct{}{}:
			default:
				log.Printf("association limit (%d) reached, rejecting %s",
					s.config.MaxAssociations, transport.RemoteAddr())
				s.rejectOverLimit(ctx, transport)
				continue
			}
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if s.assocSlots != nil {
				defer func() { <-s.assocSlots }()
			}
			s.handleConnection(ctx, transport)
		}()
	}
}

// Addr returns the listener address, or empty string if not listening.
func (s *SCP) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// Close stops the SCP server gracefully.
func (s *SCP) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// rejectOverLimit turns away a connection that arrived while the association
// limit was saturated. The peer is told the reason so it can retry, rather than
// having the socket closed without explanation.
func (s *SCP) rejectOverLimit(ctx context.Context, transport *Transport) {
	defer transport.Close()

	// Only an A-ASSOCIATE-RQ can be answered with an A-ASSOCIATE-RJ; anything
	// else gets the connection closed.
	pdu, err := transport.ReadPDU(ctx)
	if err != nil {
		return
	}
	if _, ok := pdu.(*AssociateRQ); !ok {
		return
	}

	assoc := NewAssociation(transport)
	// Result: rejected-transient, Source: service-user,
	// Reason 2: local-limit-exceeded (PS3.8 Table 9-21).
	_ = assoc.RejectAssociation(ctx, RJResultRejectedTransient, RJSourceServiceUser, 2)
}

func (s *SCP) handleConnection(ctx context.Context, transport *Transport) {
	defer transport.Close()

	assoc := NewAssociation(transport)

	// Read the A-ASSOCIATE-RQ
	pdu, err := transport.ReadPDU(ctx)
	if err != nil {
		log.Printf("failed to read association request: %v", err)
		return
	}

	rq, ok := pdu.(*AssociateRQ)
	if !ok {
		log.Printf("expected A-ASSOCIATE-RQ, got %T", pdu)
		_ = assoc.Abort(ctx, AbortSourceServiceProvider, 2)
		return
	}

	// Check AE title
	if rq.CalledAE != s.config.AETitle {
		_ = assoc.RejectAssociation(ctx, RJResultRejectedPermanent, RJSourceServiceUser, 7)
		return
	}

	s.mu.RLock()
	supportedAS := s.supportedAbstractSyntaxes
	supportedTS := s.supportedTransferSyntaxes
	handler := s.handler
	s.mu.RUnlock()

	// Accept association
	if err := assoc.AcceptAssociation(ctx, rq, supportedAS, supportedTS, s.config.Network.MaxPDUSize); err != nil {
		log.Printf("failed to accept association: %v", err)
		return
	}

	// Handle DIMSE messages
	s.handleAssociation(ctx, assoc, handler, transport, rq)
}

func (s *SCP) handleAssociation(ctx context.Context, assoc *Association, handler Handler,
	transport *Transport, rq *AssociateRQ) {
	// Attach association info to the context so handlers can access it.
	info := &AssociationInfo{
		CallingAE:                  assoc.CallingAE(),
		CalledAE:                   assoc.CalledAE(),
		RemoteAddr:                 transport.RemoteAddr(),
		LocalAddr:                  transport.LocalAddr(),
		MaxPDUSize:                 assoc.MaxPDUSize(),
		AcceptedContexts:           assoc.AcceptedContexts(),
		PeerImplementationClassUID: rq.UserInformation.ImplementationClassUID,
		PeerImplementationVersion:  rq.UserInformation.ImplementationVersion,
	}
	ctx = ContextWithAssociationInfo(ctx, info)

	for {
		if assoc.State() != StateAssociated {
			return
		}

		// Receive a PDU (could be command data or release/abort)
		ctxID, cmdData, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			// Check if it's a normal release
			if assocErr, ok := err.(*AssociationError); ok {
				if assocErr.Code == "RELEASED" || assocErr.Code == "ABORTED" {
					return
				}
			}
			log.Printf("error receiving data: %v", err)
			return
		}

		if !isCmd {
			log.Printf("expected command, got data")
			continue
		}

		cmdDS, err := DecodeCommandDataset(cmdData)
		if err != nil {
			log.Printf("failed to decode command: %v", err)
			return
		}

		commandField, messageID, _, err := ParseCommandDataset(cmdDS)
		if err != nil {
			log.Printf("failed to parse command: %v", err)
			return
		}

		// Handle based on command type
		switch commandField {
		case CommandCEchoRQ:
			s.handleCEcho(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandCStoreRQ:
			s.handleCStore(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandCFindRQ:
			s.handleCFind(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandCMoveRQ:
			s.handleCMove(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandCGetRQ:
			s.handleCGet(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandNEventReportRQ:
			s.handleNEventReport(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandNGetRQ:
			s.handleNGet(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandNSetRQ:
			s.handleNSet(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandNActionRQ:
			s.handleNAction(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandNCreateRQ:
			s.handleNCreate(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandNDeleteRQ:
			s.handleNDelete(ctx, assoc, handler, ctxID, messageID, cmdDS)
		case CommandCCancelRQ:
			// A cancel refers to an operation already in flight. Operations are
			// handled synchronously here, so by the time this is read the
			// operation it names has finished; there is nothing left to stop.
			//
			// Aborting would be worse than useless: it tears down the
			// association and discards results the requestor already has. Log
			// and carry on, which is what PS3.7 9.3.2.3 permits when the
			// operation is no longer in progress.
			log.Printf("C-CANCEL received for message %d; no operation in progress", messageID)
		default:
			log.Printf("unsupported command: 0x%04X", commandField)
			_ = assoc.Abort(ctx, AbortSourceServiceProvider, 0)
			return
		}
	}
}

func (s *SCP) handleCEcho(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, _ *dataset.Dataset) {

	req := &CEchoRequest{
		MessageID:        messageID,
		AffectedSOPClass: VerificationSOPClassUID,
	}

	resp, err := handler.HandleCEcho(ctx, req)
	if err != nil {
		resp = &CEchoResponse{
			MessageIDRespondedTo: messageID,
			AffectedSOPClass:     VerificationSOPClassUID,
			Status:               StatusUnableToProcess,
		}
	}

	rspDS := BuildCEchoRSP(resp.MessageIDRespondedTo, resp.Status)
	rspBytes, err := EncodeCommandDataset(rspDS)
	if err != nil {
		log.Printf("failed to encode C-ECHO-RSP: %v", err)
		return
	}

	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleCStore(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)

	// Read the data set if present
	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			log.Printf("failed to receive C-STORE data: %v", err)
			return
		}
		if isCmd {
			log.Printf("expected data, got command during C-STORE")
			return
		}
		ds, err = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
		if err != nil {
			log.Printf("failed to decode C-STORE dataset: %v", err)
			ds = dataset.NewDataset()
		}
	}

	req := &CStoreRequest{
		MessageID:           messageID,
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		DataSet:             ds,
	}

	resp, err := handler.HandleCStore(ctx, req)
	if err != nil {
		resp = &CStoreResponse{
			MessageIDRespondedTo: messageID,
			AffectedSOPClass:     sopClassUID,
			AffectedSOPInstance:  sopInstanceUID,
			Status:               StatusUnableToProcess,
		}
	}

	rspDS := BuildCStoreRSP(resp.MessageIDRespondedTo, sopClassUID, sopInstanceUID, resp.Status)
	rspBytes, err := EncodeCommandDataset(rspDS)
	if err != nil {
		log.Printf("failed to encode C-STORE-RSP: %v", err)
		return
	}

	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleCFind(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)

	// Read query dataset
	var queryDS *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			log.Printf("failed to receive C-FIND query: %v", err)
			return
		}
		if isCmd {
			log.Printf("expected data, got command during C-FIND")
			return
		}
		queryDS, err = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
		if err != nil {
			log.Printf("failed to decode C-FIND query: %v", err)
			return
		}
	}

	req := &CFindRequest{
		MessageID:        messageID,
		AffectedSOPClass: sopClassUID,
		DataSet:          queryDS,
	}

	responses, err := handler.HandleCFind(ctx, req)
	if err != nil {
		// Send failure response
		rspDS := BuildCFindRSP(messageID, sopClassUID, StatusUnableToProcess, false)
		rspBytes, _ := EncodeCommandDataset(rspDS)
		_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
		return
	}

	// Send pending responses
	for _, resp := range responses {
		// Send pending command
		rspDS := BuildCFindRSP(messageID, sopClassUID, StatusPending, resp.DataSet != nil)
		rspBytes, err := EncodeCommandDataset(rspDS)
		if err != nil {
			continue
		}
		_ = assoc.SendPData(ctx, ctxID, rspBytes, true)

		// Send result dataset if present
		if resp.DataSet != nil {
			dataBytes, err := EncodeDataset(resp.DataSet, assoc.TransferSyntaxFor(ctxID))
			if err != nil {
				continue
			}
			_ = assoc.SendPData(ctx, ctxID, dataBytes, false)
		}
	}

	// Send final success response
	rspDS := BuildCFindRSP(messageID, sopClassUID, StatusSuccess, false)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleCMove(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)

	// Read query dataset
	var queryDS *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			log.Printf("failed to receive C-MOVE query: %v", err)
			return
		}
		if isCmd {
			return
		}
		queryDS, err = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
		if err != nil {
			return
		}
	}

	moveDest := ""
	if elem, ok := cmdDS.Get(tagMoveDestination); ok {
		moveDest = extractStringValue(elem.GetValue())
	}

	req := &CMoveRequest{
		MessageID:        messageID,
		AffectedSOPClass: sopClassUID,
		MoveDestination:  moveDest,
		DataSet:          queryDS,
	}

	resp, err := handler.HandleCMove(ctx, req)
	if err != nil {
		s.sendMoveFinal(ctx, assoc, ctxID, messageID, sopClassUID, StatusUnableToProcess, 0, 0, 0)
		return
	}

	var instances []*dataset.Dataset
	status := StatusSuccess
	if resp != nil {
		status = resp.Status
		instances = resp.Instances
	}

	if len(instances) == 0 {
		s.sendMoveFinal(ctx, assoc, ctxID, messageID, sopClassUID, status, 0, 0, 0)
		return
	}

	// Unlike C-GET, the instances go to a third party named only by AE title,
	// so the title must be resolvable to an address.
	address, ok := s.config.resolveMoveDestination(moveDest)
	if !ok {
		log.Printf("C-MOVE destination %q is not configured; set SCPConfig.MoveDestinations "+
			"or SCPConfig.ResolveMoveDestination", moveDest)
		s.sendMoveFinal(ctx, assoc, ctxID, messageID, sopClassUID,
			StatusMoveDestUnknown, 0, uint16(len(instances)), 0)
		return
	}

	completed, failed, warning := s.sendMoveSubOperations(ctx, assoc, ctxID,
		messageID, sopClassUID, moveDest, address, instances)

	if failed > 0 && status == StatusSuccess {
		status = StatusGetWarningPartial
	}
	s.sendMoveFinal(ctx, assoc, ctxID, messageID, sopClassUID, status, completed, failed, warning)
}

// sendMoveFinal sends the terminating C-MOVE-RSP.
func (s *SCP) sendMoveFinal(ctx context.Context, assoc *Association, ctxID byte,
	messageID uint16, sopClassUID string, status, completed, failed, warning uint16) {

	rspDS := BuildCMoveRSP(messageID, sopClassUID, status, 0, completed, failed, warning)
	rspBytes, err := EncodeCommandDataset(rspDS)
	if err != nil {
		log.Printf("failed to encode C-MOVE-RSP: %v", err)
		return
	}
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

// sendMoveSubOperations opens an association to the move destination and sends
// the instances there as C-STORE sub-operations, reporting progress back on the
// association the C-MOVE arrived on.
//
// This is what distinguishes C-MOVE from C-GET: the data travels to a third
// party over a separate association, while the requestor only watches the
// counts (PS3.4 Annex C.4.2).
func (s *SCP) sendMoveSubOperations(ctx context.Context, assoc *Association, ctxID byte,
	messageID uint16, sopClassUID, destAE, destAddress string,
	instances []*dataset.Dataset) (completed, failed, warning uint16) {

	// Propose a presentation context for each distinct SOP Class being sent,
	// so every instance has a context to travel on.
	contexts := storageContextsFor(instances)
	if len(contexts) == 0 {
		return 0, uint16(len(instances)), 0
	}

	dest := NewSCU(SCUConfig{
		CallingAE: s.config.AETitle,
		CalledAE:  destAE,
		Address:   destAddress,
		Network:   s.config.Network,
	})

	if err := dest.Associate(ctx, contexts); err != nil {
		log.Printf("C-MOVE could not associate with destination %s at %s: %v",
			destAE, destAddress, err)
		return 0, uint16(len(instances)), 0
	}
	defer func() { _ = dest.Release(ctx) }()

	for i, inst := range instances {
		remaining := uint16(len(instances) - i - 1)

		if inst == nil {
			failed++
			continue
		}
		if err := dest.Store(ctx, inst); err != nil {
			log.Printf("C-MOVE sub-operation failed: %v", err)
			failed++
		} else {
			completed++
		}

		// Progress goes back to the requestor, not the destination.
		pendingDS := BuildCMoveRSP(messageID, sopClassUID, StatusPending,
			remaining, completed, failed, warning)
		pendingBytes, err := EncodeCommandDataset(pendingDS)
		if err != nil {
			continue
		}
		if err := assoc.SendPData(ctx, ctxID, pendingBytes, true); err != nil {
			log.Printf("C-MOVE pending response failed, abandoning sub-operations: %v", err)
			failed += remaining
			return completed, failed, warning
		}
	}

	return completed, failed, warning
}

// storageContextsFor builds presentation contexts covering the distinct SOP
// Classes of the given instances, so each can be transferred.
func storageContextsFor(instances []*dataset.Dataset) []PresentationContextItem {
	seen := make(map[string]bool)
	var contexts []PresentationContextItem

	id := byte(1)
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		sopClass, _, ok := instanceUIDs(inst)
		if !ok || seen[sopClass] {
			continue
		}
		seen[sopClass] = true

		contexts = append(contexts, PresentationContextItem{
			ID:               id,
			AbstractSyntax:   sopClass,
			TransferSyntaxes: DefaultTransferSyntaxes(),
		})
		// Presentation context IDs must be odd (PS3.8 Section 9.3.2.2).
		if id > 253 {
			break
		}
		id += 2
	}

	return contexts
}

func (s *SCP) handleCGet(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)

	var queryDS *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			return
		}
		if isCmd {
			return
		}
		queryDS, _ = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
	}

	req := &CGetRequest{
		MessageID:        messageID,
		AffectedSOPClass: sopClassUID,
		DataSet:          queryDS,
	}

	resp, err := handler.HandleCGet(ctx, req)
	if err != nil {
		rspDS := BuildCGetRSP(messageID, sopClassUID, StatusUnableToProcess, 0, 0, 0, 0)
		rspBytes, _ := EncodeCommandDataset(rspDS)
		_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
		return
	}

	var instances []*dataset.Dataset
	status := StatusSuccess
	if resp != nil {
		status = resp.Status
		instances = resp.Instances
	}

	// Transfer each matching instance as a C-STORE sub-operation over this
	// same association (PS3.4 Annex C.4.3), reporting progress as we go.
	completed, failed, warning := s.sendGetSubOperations(ctx, assoc, ctxID,
		messageID, sopClassUID, instances)

	// A partial failure is reported as a warning rather than success.
	if failed > 0 && status == StatusSuccess {
		status = StatusGetWarningPartial
	}

	rspDS := BuildCGetRSP(messageID, sopClassUID, status, 0, completed, failed, warning)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

// sendGetSubOperations transfers instances to the requestor as C-STORE
// sub-operations on the association the C-GET arrived on, emitting a pending
// C-GET-RSP after each one so the requestor can track progress.
//
// Returns the completed, failed, and warning counts.
func (s *SCP) sendGetSubOperations(ctx context.Context, assoc *Association, ctxID byte,
	messageID uint16, sopClassUID string, instances []*dataset.Dataset) (completed, failed, warning uint16) {

	// Sub-operations carry their own message IDs, independent of the C-GET's.
	var subMessageID uint16

	for i, inst := range instances {
		remaining := uint16(len(instances) - i - 1)

		if inst == nil {
			failed++
			continue
		}

		instClass, instUID, ok := instanceUIDs(inst)
		if !ok {
			log.Printf("C-GET sub-operation skipped: instance is missing SOP Class or SOP Instance UID")
			failed++
			continue
		}

		// The instance travels on the presentation context negotiated for its
		// own SOP Class, which may differ from the C-GET's.
		subCtxID, ok := FindPresentationContextID(assoc.AcceptedContexts(), instClass)
		if !ok {
			log.Printf("C-GET sub-operation skipped: no accepted presentation context for %s", instClass)
			failed++
			continue
		}

		subMessageID++
		if err := s.sendCStoreSubOperation(ctx, assoc, subCtxID, subMessageID, instClass, instUID, inst); err != nil {
			log.Printf("C-GET sub-operation for %s failed: %v", instUID, err)
			failed++
		} else {
			completed++
		}

		// Pending response so the requestor sees progress before the final status.
		pendingDS := BuildCGetRSP(messageID, sopClassUID, StatusPending,
			remaining, completed, failed, warning)
		pendingBytes, err := EncodeCommandDataset(pendingDS)
		if err != nil {
			continue
		}
		if err := assoc.SendPData(ctx, ctxID, pendingBytes, true); err != nil {
			// The association is gone; further sub-operations cannot succeed.
			log.Printf("C-GET pending response failed, abandoning sub-operations: %v", err)
			failed += remaining
			return completed, failed, warning
		}
	}

	return completed, failed, warning
}

// sendCStoreSubOperation issues one C-STORE-RQ and waits for its response.
func (s *SCP) sendCStoreSubOperation(ctx context.Context, assoc *Association, ctxID byte,
	messageID uint16, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) error {

	cmdDS := BuildCStoreRQ(messageID, sopClassUID, sopInstanceUID, PriorityMedium)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return fmt.Errorf("encode C-STORE-RQ: %w", err)
	}
	if err := assoc.SendPData(ctx, ctxID, cmdBytes, true); err != nil {
		return fmt.Errorf("send C-STORE-RQ: %w", err)
	}

	dataBytes, err := EncodeDataset(ds, assoc.TransferSyntaxFor(ctxID))
	if err != nil {
		return fmt.Errorf("encode instance: %w", err)
	}
	if err := assoc.SendPData(ctx, ctxID, dataBytes, false); err != nil {
		return fmt.Errorf("send instance: %w", err)
	}

	_, respData, isCmd, err := assoc.ReceivePData(ctx)
	if err != nil {
		return fmt.Errorf("receive C-STORE-RSP: %w", err)
	}
	if !isCmd {
		return fmt.Errorf("expected C-STORE-RSP command, got a data set")
	}

	respDS, err := DecodeCommandDataset(respData)
	if err != nil {
		return fmt.Errorf("decode C-STORE-RSP: %w", err)
	}
	_, _, status, err := ParseCommandDataset(respDS)
	if err != nil {
		return fmt.Errorf("parse C-STORE-RSP: %w", err)
	}
	if status != StatusSuccess && status != StatusWarning {
		return NewDIMSEError("SUBOP_FAILED",
			fmt.Sprintf("C-STORE sub-operation rejected with status 0x%04X", status), status)
	}

	return nil
}

// instanceUIDs extracts the SOP Class and SOP Instance UIDs an instance must
// carry to be transferred.
func instanceUIDs(ds *dataset.Dataset) (sopClassUID, sopInstanceUID string, ok bool) {
	classElem, hasClass := ds.Get(tagSOPClassUID)
	instElem, hasInst := ds.Get(tagSOPInstanceUID)
	if !hasClass || !hasInst {
		return "", "", false
	}

	sopClassUID = extractStringValue(classElem.GetValue())
	sopInstanceUID = extractStringValue(instElem.GetValue())
	if sopClassUID == "" || sopInstanceUID == "" {
		return "", "", false
	}
	return sopClassUID, sopInstanceUID, true
}

func (s *SCP) handleNEventReport(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)
	eventTypeID, _ := getUSValue(cmdDS, tagEventTypeID)

	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil || isCmd {
			return
		}
		ds, _ = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
	}

	req := &NEventReportRequest{
		MessageID:           messageID,
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		EventTypeID:         eventTypeID,
		DataSet:             ds,
	}

	resp, err := handler.HandleNEventReport(ctx, req)
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
	}

	rspDS := BuildNEventReportRSP(messageID, sopClassUID, sopInstanceUID, eventTypeID, status)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleNGet(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetRequestedSOPInstanceUID(cmdDS)

	req := &NGetRequest{
		MessageID:            messageID,
		RequestedSOPClass:    sopClassUID,
		RequestedSOPInstance: sopInstanceUID,
	}

	resp, err := handler.HandleNGet(ctx, req)
	status := StatusSuccess
	hasDS := false
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
		hasDS = resp.DataSet != nil
	}

	rspDS := BuildNGetRSP(messageID, sopClassUID, sopInstanceUID, status, hasDS)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)

	if hasDS && resp != nil && resp.DataSet != nil {
		dataBytes, _ := EncodeDataset(resp.DataSet, assoc.TransferSyntaxFor(ctxID))
		_ = assoc.SendPData(ctx, ctxID, dataBytes, false)
	}
}

func (s *SCP) handleNSet(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetRequestedSOPInstanceUID(cmdDS)

	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil || isCmd {
			return
		}
		ds, _ = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
	}

	req := &NSetRequest{
		MessageID:            messageID,
		RequestedSOPClass:    sopClassUID,
		RequestedSOPInstance: sopInstanceUID,
		DataSet:              ds,
	}

	resp, err := handler.HandleNSet(ctx, req)
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
	}

	rspDS := BuildNSetRSP(messageID, sopClassUID, sopInstanceUID, status)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleNAction(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetRequestedSOPInstanceUID(cmdDS)
	actionTypeID, _ := getUSValue(cmdDS, tagActionTypeID)

	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil || isCmd {
			return
		}
		ds, _ = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
	}

	req := &NActionRequest{
		MessageID:            messageID,
		RequestedSOPClass:    sopClassUID,
		RequestedSOPInstance: sopInstanceUID,
		ActionTypeID:         actionTypeID,
		DataSet:              ds,
	}

	// Storage Commitment is an N-ACTION with a service flow of its own: the
	// response only acknowledges receipt, and the commitment itself follows as
	// an N-EVENT-REPORT. Routing it here keeps handlers from having to decode
	// the request data set and drive that second message themselves.
	if sopClassUID == StorageCommitmentPushModelUID && actionTypeID == StorageCommitmentActionType {
		s.handleStorageCommitment(ctx, assoc, handler, ctxID, messageID, sopClassUID, sopInstanceUID, ds)
		return
	}

	resp, err := handler.HandleNAction(ctx, req)
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
	}

	rspDS := BuildNActionRSP(messageID, sopClassUID, sopInstanceUID, actionTypeID, status)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

// handleStorageCommitment answers a commitment request and then reports the
// outcome (PS3.4 Annex J).
//
// The N-ACTION-RSP is sent first and means only "request received"; sending it
// before the handler runs would be wrong, but so would making the requestor
// wait for a decision that the standard says arrives separately. The handler
// therefore decides first, the acknowledgement goes out, and the result follows
// as an N-EVENT-REPORT on this same association.
//
// Reporting on a new association is also permitted and is what an archive that
// commits slowly would do. That is the caller's to arrange: hold the request,
// release, and use SCU.SendStorageCommitmentResult later.
func (s *SCP) handleStorageCommitment(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) {

	commitHandler, ok := handler.(StorageCommitmentProvider)
	if !ok {
		log.Printf("storage commitment requested but the handler does not provide it")
		s.replyNAction(ctx, assoc, ctxID, messageID, sopClassUID, sopInstanceUID,
			StatusStorageCommitmentRefused)
		return
	}

	req, err := ParseStorageCommitmentRequest(ds)
	if err != nil {
		log.Printf("storage commitment request could not be read: %v", err)
		s.replyNAction(ctx, assoc, ctxID, messageID, sopClassUID, sopInstanceUID,
			StatusUnableToProcess)
		return
	}

	result, err := commitHandler.HandleStorageCommitment(ctx, req)
	if err != nil || result == nil {
		if err != nil {
			log.Printf("storage commitment handler failed: %v", err)
		}
		s.replyNAction(ctx, assoc, ctxID, messageID, sopClassUID, sopInstanceUID,
			StatusUnableToProcess)
		return
	}

	// The Transaction UID is what lets the requestor match this result to its
	// request, so it is echoed rather than trusted from the handler.
	result.TransactionUID = req.TransactionUID

	s.replyNAction(ctx, assoc, ctxID, messageID, sopClassUID, sopInstanceUID, StatusSuccess)

	if err := s.sendCommitmentReport(ctx, assoc, ctxID, sopClassUID, sopInstanceUID, result); err != nil {
		log.Printf("failed to report storage commitment (%s): %v",
			storageCommitmentSummary(result), err)
	}
}

// replyNAction sends an N-ACTION-RSP with the given status.
func (s *SCP) replyNAction(ctx context.Context, assoc *Association, ctxID byte,
	messageID uint16, sopClassUID, sopInstanceUID string, status uint16) {

	rspDS := BuildNActionRSP(messageID, sopClassUID, sopInstanceUID,
		StorageCommitmentActionType, status)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

// sendCommitmentReport sends the N-EVENT-REPORT carrying the outcome and reads
// the acknowledgement.
func (s *SCP) sendCommitmentReport(ctx context.Context, assoc *Association, ctxID byte,
	sopClassUID, sopInstanceUID string, result *StorageCommitmentResult) error {

	ds, err := BuildStorageCommitmentResult(result)
	if err != nil {
		return err
	}

	messageID := s.nextMessageID()
	cmdDS := BuildNEventReportRQ(messageID, sopClassUID, sopInstanceUID, result.EventTypeID(), true)
	cmdBytes, err := EncodeCommandDataset(cmdDS)
	if err != nil {
		return err
	}
	if err := assoc.SendPData(ctx, ctxID, cmdBytes, true); err != nil {
		return err
	}

	dsBytes, err := EncodeDataset(ds, assoc.TransferSyntaxFor(ctxID))
	if err != nil {
		return err
	}
	if err := assoc.SendPData(ctx, ctxID, dsBytes, false); err != nil {
		return err
	}

	// The requestor acknowledges with N-EVENT-REPORT-RSP. Reading it keeps the
	// association in step; leaving it unread would make the next message read
	// from this connection pick up a response to a message it did not send.
	_, respData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return err
	}
	respDS, err := DecodeCommandDataset(respData)
	if err != nil {
		return err
	}
	if _, _, status, _ := ParseCommandDataset(respDS); status != StatusSuccess {
		return NewPDUErrorf("STORAGE_COMMITMENT",
			"the requestor rejected the commitment report with status 0x%04X", status)
	}
	return nil
}

func (s *SCP) handleNCreate(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)

	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil || isCmd {
			return
		}
		ds, _ = DecodeDataset(dataBytes, assoc.TransferSyntaxFor(ctxID))
	}

	req := &NCreateRequest{
		MessageID:           messageID,
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		DataSet:             ds,
	}

	resp, err := handler.HandleNCreate(ctx, req)
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
	}

	rspDS := BuildNCreateRSP(messageID, sopClassUID, sopInstanceUID, status)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleNDelete(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetRequestedSOPInstanceUID(cmdDS)

	req := &NDeleteRequest{
		MessageID:            messageID,
		RequestedSOPClass:    sopClassUID,
		RequestedSOPInstance: sopInstanceUID,
	}

	resp, err := handler.HandleNDelete(ctx, req)
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
	}

	rspDS := BuildNDeleteRSP(messageID, sopClassUID, sopInstanceUID, status)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func defaultSupportedAbstractSyntaxes() map[string]bool {
	return map[string]bool{
		VerificationSOPClassUID:         true,
		CTImageStorageUID:               true,
		EnhancedCTImageStorageUID:       true,
		MRImageStorageUID:               true,
		EnhancedMRImageStorageUID:       true,
		USImageStorageUID:               true,
		SecondaryCaptureImageStorageUID: true,
		CRImageStorageUID:               true,
		DigitalXRayImageStorageUID:      true,
		PatientRootQueryRetrieveFind:    true,
		PatientRootQueryRetrieveMove:    true,
		PatientRootQueryRetrieveGet:     true,
		StudyRootQueryRetrieveFind:      true,
		StudyRootQueryRetrieveMove:      true,
		StudyRootQueryRetrieveGet:       true,

		// Retired in the current standard, but still offered by some archives.
		PatientStudyOnlyQueryRetrieveFind: true,
		PatientStudyOnlyQueryRetrieveMove: true,
		PatientStudyOnlyQueryRetrieveGet:  true,

		// Offered so a requestor can negotiate it; requests are refused unless
		// the handler implements StorageCommitmentProvider.
		StorageCommitmentPushModelUID: true,
	}
}

func defaultSupportedTransferSyntaxes() map[string]bool {
	return map[string]bool{
		ImplicitVRLittleEndianUID: true,
		ExplicitVRLittleEndianUID: true,
	}
}
