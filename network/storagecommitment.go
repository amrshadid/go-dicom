package network

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Storage Commitment (PS3.4 Annex J) is how an SCU asks an archive to take
// permanent responsibility for instances it has already sent, so it can delete
// its own copies.
//
// The flow is two messages in opposite directions. The SCU sends N-ACTION with
// action type 1, naming a Transaction UID and the instances in question; the
// SCP answers N-ACTION-RSP immediately, which means only "request received".
// The actual answer arrives later as N-EVENT-REPORT, event type 1 if everything
// committed or 2 if any instance failed, carrying the per-instance outcome.
//
// The standard permits the report on the same association or on a new one the
// SCP opens back to the SCU. Both are implemented here: SendResult uses the
// association it is given, and an SCP that has released can open another.
const (
	// StorageCommitmentActionType is the N-ACTION action type ID for a
	// commitment request (PS3.4 §J.3.2).
	StorageCommitmentActionType uint16 = 1

	// StorageCommitmentEventComplete is the N-EVENT-REPORT event type ID
	// reporting that every instance was committed.
	StorageCommitmentEventComplete uint16 = 1

	// StorageCommitmentEventFailures reports that at least one instance failed.
	// The report still lists what succeeded.
	StorageCommitmentEventFailures uint16 = 2

	// StorageCommitmentSOPInstanceUID is the well-known instance UID the push
	// model uses; there is no real instance behind it (PS3.4 §J.3.1).
	StorageCommitmentSOPInstanceUID = "1.2.840.10008.1.20.1.1"
)

// Failure reasons for an instance that could not be committed (PS3.4 §J.3.3).
const (
	// StorageCommitmentFailureNoSuchObject means the SCP holds no such instance.
	StorageCommitmentFailureNoSuchObject uint16 = 0x0112

	// StorageCommitmentFailureClassNotSupported means the SCP does not commit
	// instances of that SOP Class.
	StorageCommitmentFailureClassNotSupported uint16 = 0x0122

	// StorageCommitmentFailureProcessingFailure is the general failure.
	StorageCommitmentFailureProcessingFailure uint16 = 0x0110

	// StorageCommitmentFailureResourceLimitation means the SCP cannot take
	// responsibility now, though the instance is otherwise acceptable.
	StorageCommitmentFailureResourceLimitation uint16 = 0xA700
)

// Tags carrying the commitment request and result (PS3.3 §C.14).
var (
	tagTransactionUID        = tag.New(0x0008, 0x1195)
	tagFailureReason         = tag.New(0x0008, 0x1197)
	tagFailedSOPSequence     = tag.New(0x0008, 0x1198)
	tagReferencedSOPSequence = tag.New(0x0008, 0x1199)
	tagReferencedSOPClass    = tag.New(0x0008, 0x1150)
	tagReferencedSOPInstance = tag.New(0x0008, 0x1155)
)

// SOPInstanceReference names one instance by its SOP Class and SOP Instance.
type SOPInstanceReference struct {
	SOPClassUID    string
	SOPInstanceUID string
}

// StorageCommitmentFailure is an instance the SCP declined to commit, with the
// reason it gave.
type StorageCommitmentFailure struct {
	SOPInstanceReference
	Reason uint16
}

// StorageCommitmentRequest is what an SCU asks the archive to take
// responsibility for.
type StorageCommitmentRequest struct {
	// TransactionUID ties the request to the result that arrives later,
	// possibly on a different association. It must be unique per request.
	TransactionUID string

	// Instances are the instances the SCU has already stored and wants
	// committed.
	Instances []SOPInstanceReference
}

// StorageCommitmentResult is the outcome the SCP reports back.
type StorageCommitmentResult struct {
	// Deferred stops the SCP reporting this result on the current association.
	//
	// Set it when the commitment is not decided yet. PS3.4 §J.3 allows the
	// N-EVENT-REPORT to arrive long after the N-ACTION, which is what an
	// archive that verifies durability before promising it has to do — writing
	// to permanent storage, confirming a backup, whatever its policy requires.
	// Reporting success on the spot would be a promise made before it was true.
	//
	// The SCP still acknowledges the request. Send the result later with
	// SCP.ReportStorageCommitment, which opens an association back to the
	// requestor. Nothing else will send it: a deferred result the caller
	// forgets leaves the requestor waiting indefinitely, which is why this is
	// opt-in rather than the default.
	Deferred bool

	// TransactionUID echoes the request this result answers.
	TransactionUID string

	// Successful instances the SCP has taken responsibility for.
	Successful []SOPInstanceReference

	// Failed instances, each with a reason. A result with any failure is sent
	// as event type 2 rather than 1.
	Failed []StorageCommitmentFailure
}

// EventTypeID returns the N-EVENT-REPORT event type this result should be sent
// with: complete only when nothing failed.
func (r *StorageCommitmentResult) EventTypeID() uint16 {
	if len(r.Failed) > 0 {
		return StorageCommitmentEventFailures
	}
	return StorageCommitmentEventComplete
}

// BuildStorageCommitmentRequest encodes a request as the N-ACTION data set.
func BuildStorageCommitmentRequest(req *StorageCommitmentRequest) (*dataset.Dataset, error) {
	if req == nil {
		return nil, NewPDUError("STORAGE_COMMITMENT", "request is nil")
	}
	if req.TransactionUID == "" {
		return nil, NewPDUError("STORAGE_COMMITMENT",
			"a Transaction UID is required; it is what matches the result to this request")
	}
	if len(req.Instances) == 0 {
		return nil, NewPDUError("STORAGE_COMMITMENT", "no instances to commit")
	}

	ds := dataset.NewDataset()
	if err := addUIDElement(ds, tagTransactionUID, req.TransactionUID); err != nil {
		return nil, err
	}
	if err := addReferencedSequence(ds, tagReferencedSOPSequence, req.Instances, nil); err != nil {
		return nil, err
	}
	return ds, nil
}

// ParseStorageCommitmentRequest decodes the N-ACTION data set of a request.
func ParseStorageCommitmentRequest(ds *dataset.Dataset) (*StorageCommitmentRequest, error) {
	if ds == nil {
		return nil, NewPDUError("STORAGE_COMMITMENT", "no data set accompanied the N-ACTION")
	}

	req := &StorageCommitmentRequest{
		TransactionUID: uidValue(ds, tagTransactionUID),
	}
	if req.TransactionUID == "" {
		return nil, NewPDUError("STORAGE_COMMITMENT", "the request carries no Transaction UID")
	}

	refs, _, err := readReferencedSequence(ds, tagReferencedSOPSequence)
	if err != nil {
		return nil, err
	}
	req.Instances = refs
	return req, nil
}

// BuildStorageCommitmentResult encodes a result as the N-EVENT-REPORT data set.
func BuildStorageCommitmentResult(result *StorageCommitmentResult) (*dataset.Dataset, error) {
	if result == nil {
		return nil, NewPDUError("STORAGE_COMMITMENT", "result is nil")
	}
	if result.TransactionUID == "" {
		return nil, NewPDUError("STORAGE_COMMITMENT",
			"a Transaction UID is required; without it the requestor cannot match this result to its request")
	}

	ds := dataset.NewDataset()
	if err := addUIDElement(ds, tagTransactionUID, result.TransactionUID); err != nil {
		return nil, err
	}

	if len(result.Successful) > 0 {
		if err := addReferencedSequence(ds, tagReferencedSOPSequence, result.Successful, nil); err != nil {
			return nil, err
		}
	}
	if len(result.Failed) > 0 {
		refs := make([]SOPInstanceReference, len(result.Failed))
		reasons := make([]uint16, len(result.Failed))
		for i, f := range result.Failed {
			refs[i] = f.SOPInstanceReference
			reasons[i] = f.Reason
		}
		if err := addReferencedSequence(ds, tagFailedSOPSequence, refs, reasons); err != nil {
			return nil, err
		}
	}
	return ds, nil
}

// ParseStorageCommitmentResult decodes the N-EVENT-REPORT data set of a result.
func ParseStorageCommitmentResult(ds *dataset.Dataset) (*StorageCommitmentResult, error) {
	if ds == nil {
		return nil, NewPDUError("STORAGE_COMMITMENT", "no data set accompanied the N-EVENT-REPORT")
	}

	result := &StorageCommitmentResult{
		TransactionUID: uidValue(ds, tagTransactionUID),
	}

	if success, _, err := readReferencedSequence(ds, tagReferencedSOPSequence); err == nil {
		result.Successful = success
	}

	failed, reasons, err := readReferencedSequence(ds, tagFailedSOPSequence)
	if err == nil {
		for i, ref := range failed {
			f := StorageCommitmentFailure{SOPInstanceReference: ref}
			if i < len(reasons) {
				f.Reason = reasons[i]
			}
			result.Failed = append(result.Failed, f)
		}
	}

	return result, nil
}

// addUIDElement adds a UI value, padded to an even length as PS3.5 §6.2
// requires. A UI pads with NUL rather than a space.
func addUIDElement(ds *dataset.Dataset, t tag.Tag, value string) error {
	b := []byte(value)
	if len(b)%2 != 0 {
		b = append(b, 0x00)
	}
	return ds.Add(dataelem.NewDataElement(t, dataelem.UI, b))
}

// addReferencedSequence writes a sequence of instance references, optionally
// with a failure reason on each item.
func addReferencedSequence(ds *dataset.Dataset, t tag.Tag, refs []SOPInstanceReference, reasons []uint16) error {
	seq := sequence.New()
	for i, ref := range refs {
		item := dataset.NewDataset()
		if err := addUIDElement(item, tagReferencedSOPClass, ref.SOPClassUID); err != nil {
			return err
		}
		if err := addUIDElement(item, tagReferencedSOPInstance, ref.SOPInstanceUID); err != nil {
			return err
		}
		if i < len(reasons) {
			b := make([]byte, 2)
			binary.LittleEndian.PutUint16(b, reasons[i])
			if err := item.Add(dataelem.NewDataElement(tagFailureReason, dataelem.US, b)); err != nil {
				return err
			}
		}
		if err := seq.Append(item); err != nil {
			return err
		}
	}
	return ds.AddSequence(t, seq)
}

// readReferencedSequence reads instance references and any failure reasons.
func readReferencedSequence(ds *dataset.Dataset, t tag.Tag) ([]SOPInstanceReference, []uint16, error) {
	seq, err := ds.GetSequence(t)
	if err != nil {
		return nil, nil, NewPDUErrorf("STORAGE_COMMITMENT", "no sequence at %s: %v", t, err)
	}

	var refs []SOPInstanceReference
	var reasons []uint16
	for i := 0; i < seq.Length(); i++ {
		raw, err := seq.Get(i)
		if err != nil {
			continue
		}
		item, ok := raw.(*dataset.Dataset)
		if !ok {
			continue
		}
		refs = append(refs, SOPInstanceReference{
			SOPClassUID:    uidValue(item, tagReferencedSOPClass),
			SOPInstanceUID: uidValue(item, tagReferencedSOPInstance),
		})

		var reason uint16
		if elem, ok := item.Get(tagFailureReason); ok {
			if b, ok := elem.GetValue().([]byte); ok && len(b) >= 2 {
				reason = binary.LittleEndian.Uint16(b)
			}
		}
		reasons = append(reasons, reason)
	}
	return refs, reasons, nil
}

// uidValue reads a UI value, stripping the NUL padding PS3.5 §6.2 allows.
func uidValue(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	b, ok := elem.GetValue().([]byte)
	if !ok {
		return ""
	}
	return trimUIDPadding(string(b))
}

func trimUIDPadding(s string) string {
	for len(s) > 0 && (s[len(s)-1] == 0x00 || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// RequestStorageCommitment asks the peer to take responsibility for instances
// the SCU has already stored.
//
// The N-ACTION-RSP this returns means only that the request was received. The
// commitment itself arrives later as an N-EVENT-REPORT, which the SCU receives
// through SCUConfig.OnStorageCommitmentResult — on this association if the peer
// reports before release, or on a new association the peer opens back.
func (s *SCU) RequestStorageCommitment(ctx context.Context, req *StorageCommitmentRequest) (*NActionResponse, error) {
	ds, err := BuildStorageCommitmentRequest(req)
	if err != nil {
		return nil, err
	}
	return s.NAction(ctx, StorageCommitmentPushModelUID, StorageCommitmentSOPInstanceUID,
		StorageCommitmentActionType, ds)
}

// SendStorageCommitmentResult sends a commitment result to the requestor over
// an association already established with it.
//
// The event type is derived from the result rather than taken as a parameter,
// because reporting event type 1 alongside a non-empty failed list is a
// contradiction a caller should not be able to express.
func (s *SCU) SendStorageCommitmentResult(ctx context.Context, result *StorageCommitmentResult) (*NEventReportResponse, error) {
	ds, err := BuildStorageCommitmentResult(result)
	if err != nil {
		return nil, err
	}
	return s.NEventReport(ctx, StorageCommitmentPushModelUID, StorageCommitmentSOPInstanceUID,
		result.EventTypeID(), ds)
}

// storageCommitmentDataSet renders a result for logging.
func storageCommitmentSummary(result *StorageCommitmentResult) string {
	return fmt.Sprintf("transaction %s: %d committed, %d failed",
		result.TransactionUID, len(result.Successful), len(result.Failed))
}

// ReceiveStorageCommitmentResult waits for the peer's N-EVENT-REPORT carrying
// a commitment result, acknowledges it, and returns it.
//
// It is separate from RequestStorageCommitment because the two are separate
// exchanges: the standard permits the result to arrive much later, and on a
// different association. Call this when the result is expected on the current
// one; to receive it on a new association, run an SCP with a handler that
// implements the N-EVENT-REPORT side.
//
// The context bounds the wait, so a peer that never reports does not block
// forever.
func (s *SCU) ReceiveStorageCommitmentResult(ctx context.Context) (*StorageCommitmentResult, error) {
	assoc := s.getAssociation()
	if assoc == nil {
		return nil, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	ctxID, cmdData, isCmd, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	if !isCmd {
		return nil, NewPDUError("STORAGE_COMMITMENT",
			"expected an N-EVENT-REPORT command, got a data set")
	}

	cmdDS, err := DecodeCommandDataset(cmdData)
	if err != nil {
		return nil, err
	}
	commandField, messageID, _, _ := ParseCommandDataset(cmdDS)
	if commandField != CommandNEventReportRQ {
		return nil, NewPDUErrorf("STORAGE_COMMITMENT",
			"expected N-EVENT-REPORT-RQ (0x%04X), got 0x%04X", CommandNEventReportRQ, commandField)
	}

	if !HasDataSet(cmdDS) {
		return nil, NewPDUError("STORAGE_COMMITMENT",
			"the commitment report carries no data set, so it names no instances")
	}

	dsCtxID, dsData, _, err := assoc.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	ds, err := DecodeDataset(dsData, assoc.TransferSyntaxFor(dsCtxID))
	if err != nil {
		return nil, err
	}

	result, err := ParseStorageCommitmentResult(ds)

	// Acknowledge either way. Leaving the report unanswered strands the peer,
	// which is waiting on the response before it continues.
	status := StatusSuccess
	if err != nil {
		status = StatusUnableToProcess
	}
	eventTypeID, _ := getUSValue(cmdDS, tagEventTypeID)
	rspDS := BuildNEventReportRSP(messageID, StorageCommitmentPushModelUID,
		StorageCommitmentSOPInstanceUID, eventTypeID, status)
	if rspBytes, encErr := EncodeCommandDataset(rspDS); encErr == nil {
		_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
	}

	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReportStorageCommitment sends a commitment result on a new association it
// opens to the requestor.
//
// This is the other half of a deferred result. An archive that verifies
// durability before promising it answers the N-ACTION, returns a result marked
// Deferred, and calls this once the instances are genuinely safe — by which
// time the original association is long gone.
//
// The requestor is named by the Calling AE title of the association that asked,
// which is what handlers receive. Its address comes from
// SCPConfig.CommitmentRequestors or ResolveCommitmentRequestor; without one
// there is nowhere to send the result and this reports that rather than
// silently dropping a promise the requestor is waiting on.
//
// The Transaction UID must be the one from the request. It is the only thing
// tying this result to it, and the requestor has no other way to match them.
func (s *SCP) ReportStorageCommitment(ctx context.Context, requestorAE string,
	result *StorageCommitmentResult) error {

	if result == nil {
		return NewPDUError("STORAGE_COMMITMENT", "result is nil")
	}
	if result.TransactionUID == "" {
		return NewPDUError("STORAGE_COMMITMENT",
			"a Transaction UID is required; the requestor cannot match a result without it")
	}

	address, ok := s.config.resolveCommitmentRequestor(requestorAE)
	if !ok {
		return NewPDUErrorf("STORAGE_COMMITMENT",
			"no address known for requestor %q; set SCPConfig.CommitmentRequestors or "+
				"ResolveCommitmentRequestor so a deferred result can be delivered", requestorAE)
	}

	// The roles invert for this exchange: the archive is the one initiating,
	// so it associates as an SCU on the Storage Commitment SOP Class.
	requestor := NewSCU(SCUConfig{
		CallingAE: s.config.AETitle,
		CalledAE:  requestorAE,
		Address:   address,
		Network:   s.config.Network,
	})

	if err := requestor.Associate(ctx, StorageCommitmentPresentationContexts()); err != nil {
		return NewPDUErrorf("STORAGE_COMMITMENT",
			"could not associate with requestor %s at %s: %v", requestorAE, address, err)
	}
	defer func() { _ = requestor.Release(ctx) }()

	if _, err := requestor.SendStorageCommitmentResult(ctx, result); err != nil {
		return NewPDUErrorf("STORAGE_COMMITMENT",
			"failed to report %s to %s: %v", storageCommitmentSummary(result), requestorAE, err)
	}
	return nil
}
