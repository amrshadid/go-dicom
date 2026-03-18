package network

import (
	"context"
	"fmt"
	"log"
	"sync"

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
// It blocks until the context is cancelled.
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

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
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
		assoc.Abort(ctx, AbortSourceServiceProvider, 2)
		return
	}

	// Check AE title
	if rq.CalledAE != s.config.AETitle {
		assoc.RejectAssociation(ctx, RJResultRejectedPermanent, RJSourceServiceUser, 7)
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
	s.handleAssociation(ctx, assoc, handler)
}

func (s *SCP) handleAssociation(ctx context.Context, assoc *Association, handler Handler) {
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
		default:
			log.Printf("unsupported command: 0x%04X", commandField)
			assoc.Abort(ctx, AbortSourceServiceProvider, 0)
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

	assoc.SendPData(ctx, ctxID, rspBytes, true)
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
		ds, err = decodeDatasetBytes(dataBytes)
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

	assoc.SendPData(ctx, ctxID, rspBytes, true)
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
		queryDS, err = decodeDatasetBytes(dataBytes)
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
		assoc.SendPData(ctx, ctxID, rspBytes, true)
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
		assoc.SendPData(ctx, ctxID, rspBytes, true)

		// Send result dataset if present
		if resp.DataSet != nil {
			dataBytes, err := encodeDataset(resp.DataSet)
			if err != nil {
				continue
			}
			assoc.SendPData(ctx, ctxID, dataBytes, false)
		}
	}

	// Send final success response
	rspDS := BuildCFindRSP(messageID, sopClassUID, StatusSuccess, false)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	assoc.SendPData(ctx, ctxID, rspBytes, true)
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
		queryDS, err = decodeDatasetBytes(dataBytes)
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
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
	}

	rspDS := BuildCMoveRSP(messageID, sopClassUID, status, 0, 0, 0, 0)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	assoc.SendPData(ctx, ctxID, rspBytes, true)
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
		queryDS, _ = decodeDatasetBytes(dataBytes)
	}

	req := &CGetRequest{
		MessageID:        messageID,
		AffectedSOPClass: sopClassUID,
		DataSet:          queryDS,
	}

	resp, err := handler.HandleCGet(ctx, req)
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	} else if resp != nil {
		status = resp.Status
	}

	rspDS := BuildCGetRSP(messageID, sopClassUID, status, 0, 0, 0, 0)
	rspBytes, _ := EncodeCommandDataset(rspDS)
	assoc.SendPData(ctx, ctxID, rspBytes, true)
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
		ds, _ = decodeDatasetBytes(dataBytes)
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
	assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleNGet(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)

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
	assoc.SendPData(ctx, ctxID, rspBytes, true)

	if hasDS && resp != nil && resp.DataSet != nil {
		dataBytes, _ := encodeDataset(resp.DataSet)
		assoc.SendPData(ctx, ctxID, dataBytes, false)
	}
}

func (s *SCP) handleNSet(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)

	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil || isCmd {
			return
		}
		ds, _ = decodeDatasetBytes(dataBytes)
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
	assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleNAction(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)
	actionTypeID, _ := getUSValue(cmdDS, tagActionTypeID)

	var ds *dataset.Dataset
	if HasDataSet(cmdDS) {
		_, dataBytes, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil || isCmd {
			return
		}
		ds, _ = decodeDatasetBytes(dataBytes)
	}

	req := &NActionRequest{
		MessageID:            messageID,
		RequestedSOPClass:    sopClassUID,
		RequestedSOPInstance: sopInstanceUID,
		ActionTypeID:         actionTypeID,
		DataSet:              ds,
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
	assoc.SendPData(ctx, ctxID, rspBytes, true)
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
		ds, _ = decodeDatasetBytes(dataBytes)
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
	assoc.SendPData(ctx, ctxID, rspBytes, true)
}

func (s *SCP) handleNDelete(ctx context.Context, assoc *Association, handler Handler,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := getUIValue(cmdDS, tagRequestedSOPClassUID)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)

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
	assoc.SendPData(ctx, ctxID, rspBytes, true)
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
	}
}

func defaultSupportedTransferSyntaxes() map[string]bool {
	return map[string]bool{
		ImplicitVRLittleEndianUID: true,
		ExplicitVRLittleEndianUID: true,
	}
}
