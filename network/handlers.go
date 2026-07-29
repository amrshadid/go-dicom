package network

import (
	"context"
	"fmt"

	"github.com/amrshadid/go-dicom/dataset"
)

// Handler defines the interface for handling DIMSE requests on the SCP side.
// Implement only the methods you need; use BaseHandler for defaults.
type Handler interface {
	// C-DIMSE services
	HandleCEcho(ctx context.Context, req *CEchoRequest) (*CEchoResponse, error)
	HandleCStore(ctx context.Context, req *CStoreRequest) (*CStoreResponse, error)
	HandleCFind(ctx context.Context, req *CFindRequest) ([]*CFindResponse, error)
	HandleCMove(ctx context.Context, req *CMoveRequest) (*CMoveResponse, error)
	HandleCGet(ctx context.Context, req *CGetRequest) (*CGetResponse, error)

	// N-DIMSE services
	HandleNEventReport(ctx context.Context, req *NEventReportRequest) (*NEventReportResponse, error)
	HandleNGet(ctx context.Context, req *NGetRequest) (*NGetResponse, error)
	HandleNSet(ctx context.Context, req *NSetRequest) (*NSetResponse, error)
	HandleNAction(ctx context.Context, req *NActionRequest) (*NActionResponse, error)
	HandleNCreate(ctx context.Context, req *NCreateRequest) (*NCreateResponse, error)
	HandleNDelete(ctx context.Context, req *NDeleteRequest) (*NDeleteResponse, error)
}

// BaseHandler provides default implementations for all Handler methods.
// Embed this in your handler to only override the methods you need.
type BaseHandler struct{}

// HandleCEcho returns success by default.
func (h *BaseHandler) HandleCEcho(_ context.Context, req *CEchoRequest) (*CEchoResponse, error) {
	return &CEchoResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		Status:               StatusSuccess,
	}, nil
}

// HandleCStore returns success by default.
func (h *BaseHandler) HandleCStore(_ context.Context, req *CStoreRequest) (*CStoreResponse, error) {
	return &CStoreResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		AffectedSOPInstance:  req.AffectedSOPInstance,
		Status:               StatusSuccess,
	}, nil
}

// HandleCFind returns an empty result set by default.
func (h *BaseHandler) HandleCFind(_ context.Context, _ *CFindRequest) ([]*CFindResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "C-FIND not implemented", StatusUnableToProcess)
}

// HandleCMove returns an error by default.
func (h *BaseHandler) HandleCMove(_ context.Context, _ *CMoveRequest) (*CMoveResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "C-MOVE not implemented", StatusUnableToProcess)
}

// HandleCGet returns an error by default.
func (h *BaseHandler) HandleCGet(_ context.Context, _ *CGetRequest) (*CGetResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "C-GET not implemented", StatusUnableToProcess)
}

// HandleNEventReport returns an error by default.
func (h *BaseHandler) HandleNEventReport(_ context.Context, _ *NEventReportRequest) (*NEventReportResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "N-EVENT-REPORT not implemented", StatusUnableToProcess)
}

// HandleNGet returns an error by default.
func (h *BaseHandler) HandleNGet(_ context.Context, _ *NGetRequest) (*NGetResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "N-GET not implemented", StatusUnableToProcess)
}

// HandleNSet returns an error by default.
func (h *BaseHandler) HandleNSet(_ context.Context, _ *NSetRequest) (*NSetResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "N-SET not implemented", StatusUnableToProcess)
}

// HandleNAction returns an error by default.
func (h *BaseHandler) HandleNAction(_ context.Context, _ *NActionRequest) (*NActionResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "N-ACTION not implemented", StatusUnableToProcess)
}

// HandleNCreate returns an error by default.
func (h *BaseHandler) HandleNCreate(_ context.Context, _ *NCreateRequest) (*NCreateResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "N-CREATE not implemented", StatusUnableToProcess)
}

// HandleNDelete returns an error by default.
func (h *BaseHandler) HandleNDelete(_ context.Context, _ *NDeleteRequest) (*NDeleteResponse, error) {
	return nil, NewDIMSEError("NOT_IMPLEMENTED", "N-DELETE not implemented", StatusUnableToProcess)
}

// EchoHandler is a simple handler that only supports C-ECHO (verification).
type EchoHandler struct {
	BaseHandler
}

// StorageHandler is a handler that accepts C-STORE requests and calls a callback.
type StorageHandler struct {
	BaseHandler
	OnStore func(ctx context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) uint16
}

// HandleCStore delegates to the OnStore callback if set.
func (h *StorageHandler) HandleCStore(ctx context.Context, req *CStoreRequest) (*CStoreResponse, error) {
	status := StatusSuccess
	if h.OnStore != nil {
		status = h.OnStore(ctx, req.AffectedSOPClass, req.AffectedSOPInstance, req.DataSet)
	}
	return &CStoreResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		AffectedSOPInstance:  req.AffectedSOPInstance,
		Status:               status,
	}, nil
}

// QueryRetrieveHandler handles C-FIND, C-MOVE, and C-GET requests with callbacks.
type QueryRetrieveHandler struct {
	BaseHandler
	OnFind func(ctx context.Context, sopClassUID string, query *dataset.Dataset) ([]*dataset.Dataset, error)
	OnGet  func(ctx context.Context, sopClassUID string, query *dataset.Dataset) ([]*dataset.Dataset, error)

	// OnMove reports that a C-MOVE was requested. It transfers nothing on its
	// own; use OnMoveInstances to supply the instances to send.
	//
	// Deprecated: use OnMoveInstances, which returns the matching instances so
	// the SCP can perform the C-STORE sub-operations. OnMove is still honored
	// when OnMoveInstances is nil.
	OnMove func(ctx context.Context, sopClassUID, moveDestination string, query *dataset.Dataset) error

	// OnMoveInstances returns the instances matching a C-MOVE query. The SCP
	// opens an association to the move destination and sends them there as
	// C-STORE sub-operations.
	//
	// The destination AE title must be resolvable through
	// SCPConfig.MoveDestinations or SCPConfig.ResolveMoveDestination, or the
	// request is answered with StatusMoveDestUnknown.
	OnMoveInstances func(ctx context.Context, sopClassUID, moveDestination string, query *dataset.Dataset) ([]*dataset.Dataset, error)
}

// HandleCFind delegates to the OnFind callback if set.
func (h *QueryRetrieveHandler) HandleCFind(ctx context.Context, req *CFindRequest) ([]*CFindResponse, error) {
	if h.OnFind == nil {
		return nil, NewDIMSEError("NOT_IMPLEMENTED", "C-FIND not implemented", StatusUnableToProcess)
	}

	datasets, err := h.OnFind(ctx, req.AffectedSOPClass, req.DataSet)
	if err != nil {
		return nil, err
	}

	responses := make([]*CFindResponse, len(datasets))
	for i, ds := range datasets {
		responses[i] = &CFindResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               StatusPending,
			DataSet:              ds,
		}
	}
	return responses, nil
}

// HandleCMove delegates to OnMoveInstances, falling back to OnMove.
//
// The instances returned by OnMoveInstances are sent to the move destination
// as C-STORE sub-operations over a new association.
func (h *QueryRetrieveHandler) HandleCMove(ctx context.Context, req *CMoveRequest) (*CMoveResponse, error) {
	if h.OnMoveInstances != nil {
		datasets, err := h.OnMoveInstances(ctx, req.AffectedSOPClass, req.MoveDestination, req.DataSet)
		if err != nil {
			return nil, err
		}
		return &CMoveResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               StatusSuccess,
			Instances:            datasets,
		}, nil
	}

	if h.OnMove == nil {
		return nil, NewDIMSEError("NOT_IMPLEMENTED", "C-MOVE not implemented", StatusUnableToProcess)
	}

	if err := h.OnMove(ctx, req.AffectedSOPClass, req.MoveDestination, req.DataSet); err != nil {
		return nil, err
	}

	return &CMoveResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		Status:               StatusSuccess,
	}, nil
}

// HandleCGet delegates to the OnGet callback if set.
//
// The datasets returned by OnGet are transferred to the requestor as C-STORE
// sub-operations over the same association. Each must carry SOP Class UID
// (0008,0016) and SOP Instance UID (0008,0018), and its SOP Class must have an
// accepted presentation context on the association.
func (h *QueryRetrieveHandler) HandleCGet(ctx context.Context, req *CGetRequest) (*CGetResponse, error) {
	if h.OnGet == nil {
		return nil, NewDIMSEError("NOT_IMPLEMENTED", "C-GET not implemented", StatusUnableToProcess)
	}

	datasets, err := h.OnGet(ctx, req.AffectedSOPClass, req.DataSet)
	if err != nil {
		return nil, err
	}

	return &CGetResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		Status:               StatusSuccess,
		Instances:            datasets,
	}, nil
}

// WorklistHandler handles Modality Worklist (MWL) queries.
type WorklistHandler struct {
	BaseHandler
	OnWorklist func(ctx context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error)
}

// HandleCFind delegates worklist queries to the OnWorklist callback.
func (h *WorklistHandler) HandleCFind(ctx context.Context, req *CFindRequest) ([]*CFindResponse, error) {
	if h.OnWorklist == nil {
		return nil, NewDIMSEError("NOT_IMPLEMENTED", "Worklist not implemented", StatusUnableToProcess)
	}

	datasets, err := h.OnWorklist(ctx, req.DataSet)
	if err != nil {
		return nil, err
	}

	responses := make([]*CFindResponse, len(datasets))
	for i, ds := range datasets {
		responses[i] = &CFindResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               StatusPending,
			DataSet:              ds,
		}
	}
	return responses, nil
}

// CompositeHandler allows registering separate handlers for different service types.
type CompositeHandler struct {
	BaseHandler
	echoHandler  Handler
	storeHandler Handler
	findHandler  Handler
	moveHandler  Handler
	getHandler   Handler
}

// NewCompositeHandler creates a new CompositeHandler.
func NewCompositeHandler() *CompositeHandler {
	return &CompositeHandler{}
}

// SetEchoHandler sets the handler for C-ECHO requests.
func (h *CompositeHandler) SetEchoHandler(handler Handler) { h.echoHandler = handler }

// SetStoreHandler sets the handler for C-STORE requests.
func (h *CompositeHandler) SetStoreHandler(handler Handler) { h.storeHandler = handler }

// SetFindHandler sets the handler for C-FIND requests.
func (h *CompositeHandler) SetFindHandler(handler Handler) { h.findHandler = handler }

// SetMoveHandler sets the handler for C-MOVE requests.
func (h *CompositeHandler) SetMoveHandler(handler Handler) { h.moveHandler = handler }

// SetGetHandler sets the handler for C-GET requests.
func (h *CompositeHandler) SetGetHandler(handler Handler) { h.getHandler = handler }

// HandleCEcho delegates to the echo handler.
func (h *CompositeHandler) HandleCEcho(ctx context.Context, req *CEchoRequest) (*CEchoResponse, error) {
	if h.echoHandler != nil {
		return h.echoHandler.HandleCEcho(ctx, req)
	}
	return h.BaseHandler.HandleCEcho(ctx, req)
}

// HandleCStore delegates to the store handler.
func (h *CompositeHandler) HandleCStore(ctx context.Context, req *CStoreRequest) (*CStoreResponse, error) {
	if h.storeHandler != nil {
		return h.storeHandler.HandleCStore(ctx, req)
	}
	return h.BaseHandler.HandleCStore(ctx, req)
}

// HandleCFind delegates to the find handler.
func (h *CompositeHandler) HandleCFind(ctx context.Context, req *CFindRequest) ([]*CFindResponse, error) {
	if h.findHandler != nil {
		return h.findHandler.HandleCFind(ctx, req)
	}
	return h.BaseHandler.HandleCFind(ctx, req)
}

// HandleCMove delegates to the move handler.
func (h *CompositeHandler) HandleCMove(ctx context.Context, req *CMoveRequest) (*CMoveResponse, error) {
	if h.moveHandler != nil {
		return h.moveHandler.HandleCMove(ctx, req)
	}
	return h.BaseHandler.HandleCMove(ctx, req)
}

// HandleCGet delegates to the get handler.
func (h *CompositeHandler) HandleCGet(ctx context.Context, req *CGetRequest) (*CGetResponse, error) {
	if h.getHandler != nil {
		return h.getHandler.HandleCGet(ctx, req)
	}
	return h.BaseHandler.HandleCGet(ctx, req)
}

// StorageCommitmentProvider is implemented by handlers that can take
// responsibility for stored instances (PS3.4 Annex J).
//
// It is a separate interface rather than a method on Handler because storage
// commitment is a promise about durability that most SCPs have no business
// making. A handler that does not implement it causes commitment requests to be
// refused, which is the honest answer — silently accepting them would tell the
// requestor it may delete its only copy.
type StorageCommitmentProvider interface {
	// HandleStorageCommitment decides which of the requested instances this
	// SCP will take responsibility for.
	//
	// Returning an instance in Successful is a statement that it is stored
	// durably and can be retrieved later. Anything not committed belongs in
	// Failed with a reason, rather than being omitted: an instance absent from
	// both lists leaves the requestor with no answer about it.
	//
	// The Transaction UID is set from the request, so it does not need to be
	// filled in.
	HandleStorageCommitment(ctx context.Context, req *StorageCommitmentRequest) (*StorageCommitmentResult, error)
}

// StorageCommitmentHandler answers storage commitment requests through a
// function, in the style of the other handlers here.
type StorageCommitmentHandler struct {
	BaseHandler

	// OnCommit decides the outcome. If nil, every request is refused.
	OnCommit func(ctx context.Context, req *StorageCommitmentRequest) (*StorageCommitmentResult, error)
}

// HandleStorageCommitment implements StorageCommitmentProvider.
func (h *StorageCommitmentHandler) HandleStorageCommitment(ctx context.Context,
	req *StorageCommitmentRequest) (*StorageCommitmentResult, error) {

	if h.OnCommit == nil {
		return nil, fmt.Errorf("no storage commitment handler is configured")
	}
	return h.OnCommit(ctx, req)
}
