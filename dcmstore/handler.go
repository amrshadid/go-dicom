package dcmstore

import (
	"context"
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
)

// storeLogger is where this package reports.
//
// It writes through network.DefaultLogger, and so through config.Logger, so that
// one config.SetLogger controls this package along with the rest of the library
// rather than this being another thing to silence separately.
var storeLogger = network.DefaultLogger

// Handler serves Verification, Storage and Query/Retrieve over a Store.
//
// It is the reference implementation of QueryRetrieveHandler's callbacks:
// receiving an instance, matching a query against what has been received, and
// resolving a retrieval to the instances to transfer. Wiring one up is the whole
// of a small archive:
//
//	store, err := dcmstore.Open("./archive")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	scp := network.NewSCP(network.SCPConfig{AETitle: "ARCHIVE", Port: 11112})
//	scp.SetHandler(dcmstore.NewHandler(store))
//	log.Fatal(scp.ListenAndServe(ctx))
//
// The streaming interfaces are implemented as well as the slice-returning ones,
// so a C-FIND, C-GET or C-MOVE against this store stops when the requestor sends
// C-CANCEL instead of running to completion with its results discarded.
type Handler struct {
	network.BaseHandler

	store *Store

	// OnStored, when set, is called after each instance is written. Use it to
	// forward, index elsewhere, or log; returning an error does not fail the
	// C-STORE, since the instance is already stored.
	OnStored func(ctx context.Context, inst *Instance) error
}

// NewHandler returns a Handler serving store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// Store returns the store behind the handler.
func (h *Handler) Store() *Store { return h.store }

// HandleCEcho answers a verification request.
func (h *Handler) HandleCEcho(_ context.Context, req *network.CEchoRequest) (*network.CEchoResponse, error) {
	return &network.CEchoResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		Status:               network.StatusSuccess,
	}, nil
}

// HandleCStore writes the instance to the store.
func (h *Handler) HandleCStore(ctx context.Context, req *network.CStoreRequest) (*network.CStoreResponse, error) {
	response := &network.CStoreResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		AffectedSOPInstance:  req.AffectedSOPInstance,
		Status:               network.StatusSuccess,
	}

	if req.DataSet == nil {
		storeLogger.Warn("dcmstore: a C-STORE for %s carried no data set", req.AffectedSOPInstance)
		response.Status = network.StatusUnableToProcess
		return response, nil
	}

	inst, err := h.store.Store(ctx, req.DataSet)
	if err != nil {
		// Refusing with a status rather than returning an error: an error aborts
		// the association, and one instance this store will not take is not a
		// reason to discard the rest of what the peer is sending.
		storeLogger.Error("dcmstore: refused %s: %v", req.AffectedSOPInstance, err)
		response.Status = network.StatusUnableToProcess
		return response, nil
	}

	if h.OnStored != nil {
		if err := h.OnStored(ctx, inst); err != nil {
			storeLogger.Warn("dcmstore: the OnStored callback for %s failed: %v",
				inst.SOPInstanceUID, err)
		}
	}

	return response, nil
}

// HandleStorageCommitment answers a Storage Commitment Push Model request
// (PS3.4 Annex J) from what the store holds.
//
// Successful is a promise that the instance is stored durably and can be
// retrieved, so it is made only for an instance the store holds under the SOP
// Class the requestor named, with its file on disk. The file is checked and not
// only the index: the index is a cache of the files, and committing on it
// alone would promise an instance whose file has gone. Everything else fails
// with the Failure Reason that says why (PS3.3 C.14.1.1):
//
//   - not in the store: 0112H, no such object instance;
//   - held under a different class: 0119H, class/instance conflict;
//   - in the index but its file is missing or unreadable: 0110H, processing
//     failure.
//
// The answer is given at once. A C-STORE here is complete when it is
// acknowledged, so there is nothing to wait for, and the SCP sends the result
// on the same association.
func (h *Handler) HandleStorageCommitment(ctx context.Context,
	req *network.StorageCommitmentRequest) (*network.StorageCommitmentResult, error) {

	result := &network.StorageCommitmentResult{TransactionUID: req.TransactionUID}
	for _, ref := range req.Instances {
		if err := ctx.Err(); err != nil {
			// Neither list may claim an instance that was not checked.
			return nil, err
		}
		reason, ok := h.commitment(ref)
		if ok {
			result.Successful = append(result.Successful, ref)
			continue
		}
		result.Failed = append(result.Failed, network.StorageCommitmentFailure{
			SOPInstanceReference: ref,
			Reason:               reason,
		})
	}
	return result, nil
}

// commitment decides one instance: whether it can be committed, and if not,
// the Failure Reason.
func (h *Handler) commitment(ref network.SOPInstanceReference) (uint16, bool) {
	inst, ok := h.store.Instance(ref.SOPInstanceUID)
	if !ok {
		return network.StorageCommitmentFailureNoSuchObject, false
	}
	if inst.SOPClassUID != ref.SOPClassUID {
		return network.StorageCommitmentFailureClassInstanceConflict, false
	}
	info, err := os.Stat(filepath.Join(h.store.Root(), filepath.FromSlash(inst.Path)))
	if err != nil || !info.Mode().IsRegular() {
		return network.StorageCommitmentFailureProcessingFailure, false
	}
	return 0, true
}

// HandleCFind matches a query against the store.
func (h *Handler) HandleCFind(ctx context.Context, req *network.CFindRequest) ([]*network.CFindResponse, error) {
	matches, err := h.store.Query(ctx, req.DataSet)
	if err != nil {
		storeLogger.Warn("dcmstore: a C-FIND could not be answered: %v", err)
		return []*network.CFindResponse{{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               network.StatusQRIdentifierNotMatch,
		}}, nil
	}

	responses := make([]*network.CFindResponse, 0, len(matches))
	for _, match := range matches {
		responses = append(responses, &network.CFindResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               network.StatusPending,
			DataSet:              match,
		})
	}
	return responses, nil
}

// StreamCFind sends matches as they are found, stopping when ctx is canceled.
//
// The requestor sending C-CANCEL cancels ctx, so a query over a large archive
// stops rather than finishing and discarding what it produced.
func (h *Handler) StreamCFind(ctx context.Context, req *network.CFindRequest,
	out chan<- *network.CFindResponse) error {

	matches, err := h.store.Query(ctx, req.DataSet)
	if err != nil {
		return err
	}

	for _, match := range matches {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- &network.CFindResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               network.StatusPending,
			DataSet:              match,
		}:
		}
	}
	return nil
}

// HandleCGet resolves a retrieval to the instances to send back.
func (h *Handler) HandleCGet(ctx context.Context, req *network.CGetRequest) (*network.CGetResponse, error) {
	instances, err := h.loadMatching(ctx, req.DataSet)
	if err != nil {
		storeLogger.Warn("dcmstore: a C-GET could not be answered: %v", err)
		return &network.CGetResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               network.StatusQRIdentifierNotMatch,
		}, nil
	}

	return &network.CGetResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		Status:               network.StatusSuccess,
		Instances:            instances,
	}, nil
}

// CountCGetMatches reports how many instances a C-GET will transfer.
func (h *Handler) CountCGetMatches(ctx context.Context, req *network.CGetRequest) (int, error) {
	matched, err := h.store.MatchingInstances(ctx, req.DataSet)
	if err != nil {
		return 0, err
	}
	return len(matched), nil
}

// StreamCGet sends each matching instance as it is read off disk.
//
// Reading one at a time is the point: a study of a thousand instances would
// otherwise be held in memory in full before the first was sent.
func (h *Handler) StreamCGet(ctx context.Context, req *network.CGetRequest,
	out chan<- *dataset.Dataset) error {
	return h.streamInstances(ctx, req.DataSet, out)
}

// HandleCMove resolves a move to the instances to send to the destination.
func (h *Handler) HandleCMove(ctx context.Context, req *network.CMoveRequest) (*network.CMoveResponse, error) {
	instances, err := h.loadMatching(ctx, req.DataSet)
	if err != nil {
		storeLogger.Warn("dcmstore: a C-MOVE could not be answered: %v", err)
		return &network.CMoveResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               network.StatusQRIdentifierNotMatch,
		}, nil
	}

	return &network.CMoveResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		Status:               network.StatusSuccess,
		Instances:            instances,
	}, nil
}

// CountCMoveMatches reports how many instances a C-MOVE will transfer.
func (h *Handler) CountCMoveMatches(ctx context.Context, req *network.CMoveRequest) (int, error) {
	matched, err := h.store.MatchingInstances(ctx, req.DataSet)
	if err != nil {
		return 0, err
	}
	return len(matched), nil
}

// StreamCMove sends each matching instance as it is read off disk.
func (h *Handler) StreamCMove(ctx context.Context, req *network.CMoveRequest,
	out chan<- *dataset.Dataset) error {
	return h.streamInstances(ctx, req.DataSet, out)
}

// loadMatching reads every instance a query matches.
func (h *Handler) loadMatching(ctx context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error) {
	matched, err := h.store.MatchingInstances(ctx, query)
	if err != nil {
		return nil, err
	}

	instances := make([]*dataset.Dataset, 0, len(matched))
	for _, inst := range matched {
		ds, err := h.store.Load(ctx, inst)
		if err != nil {
			// One unreadable instance does not fail the retrieval. The sub-operation
			// counts report it as a failure, which is what the requestor needs to
			// know; refusing the whole retrieval would withhold the rest.
			storeLogger.Warn("dcmstore: skipping %s in a retrieval: %v", inst.SOPInstanceUID, err)
			continue
		}
		instances = append(instances, ds)
	}
	return instances, nil
}

// streamInstances reads and sends matching instances one at a time.
func (h *Handler) streamInstances(ctx context.Context, query *dataset.Dataset,
	out chan<- *dataset.Dataset) error {

	matched, err := h.store.MatchingInstances(ctx, query)
	if err != nil {
		return err
	}

	for _, inst := range matched {
		if err := ctx.Err(); err != nil {
			return err
		}

		ds, err := h.store.Load(ctx, inst)
		if err != nil {
			storeLogger.Warn("dcmstore: skipping %s in a retrieval: %v", inst.SOPInstanceUID, err)
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- ds:
		}
	}
	return nil
}

// SupportedSOPClasses returns the abstract syntaxes a store-backed SCP should
// accept: verification, every storage class, the query/retrieve models, and the
// Storage Commitment Push Model, which Handler answers from the store.
//
// Provided because an archive that accepts a C-STORE for one SOP class and
// refuses another is a configuration mistake more often than a decision, and the
// list is long enough that assembling it by hand invites leaving something out.
func SupportedSOPClasses() []string {
	classes := make([]string, 0, 300)
	classes = append(classes, network.VerificationSOPClassUID)
	classes = append(classes, network.AllStorageSOPClassUIDs()...)
	classes = append(classes, network.AllQueryRetrieveSOPClassUIDs()...)
	// A modality commits what it stored so it can delete its own copy. Without
	// this the context was refused before the handler was reached, and
	// commitscu, which ships in this repository, had no go-dicom peer (#113).
	classes = append(classes, network.StorageCommitmentPushModelUID)
	return classes
}

// SupportedTransferSyntaxes returns every transfer syntax a store-backed SCP
// should accept, compressed ones included.
//
// This store keeps instances as it receives them, and it does not have to decode
// an instance to store it: a compressed instance is written with its encapsulated
// pixel data intact and served back unchanged. Refusing the compressed syntaxes —
// which is what the library proposes by default, matching pynetdicom — would turn
// away a modality that stores JPEG-LS or JPEG 2000 natively, which is most modern
// equipment.
//
// Pair it with SupportedSOPClasses:
//
//	scp.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())
//	scp.SetSupportedTransferSyntaxes(dcmstore.SupportedTransferSyntaxes())
func SupportedTransferSyntaxes() []string {
	return network.AllTransferSyntaxes()
}
