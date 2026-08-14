package network

import (
	"context"

	"github.com/amrshadid/go-dicom/dataset"
)

// The parts of a retrieval's sub-operation loop that C-GET and C-MOVE share, and
// that the slice-returning and streaming paths share with each other.
//
// There are four of those loops — sendGetSubOperations, streamGetSubOperations,
// sendMoveSubOperations, streamMoveSubOperations — and the outer structure of each
// genuinely differs: a slice is iterated, a stream is read from a channel, and a
// C-MOVE sends to a third party over an association it opens rather than back over
// the requestor's. Merging those behind a flag would be worse than the duplication.
//
// What was duplicated is everything inside: the counter bookkeeping, the pending
// response, and for C-GET the whole per-instance send. Eight message strings were
// byte-identical across two files. That is the hazard this project keeps finding in
// other forms — the RLE encoder only its own decoder could read, the N-SET whose
// own SCP read the same wrong tag — because a correctness fix applied to one copy
// and not the other leaves a defect that only shows on the less-traveled path.

// subOperationCounts tracks the outcome of a retrieval's sub-operations.
//
// These four numbers go back to the requestor in every pending response and in the
// final status, and PS3.4 C.4.2.1.4 defines what each means. Keeping them together
// is what stops one path incrementing warning where another increments failed.
type subOperationCounts struct {
	completed uint16
	failed    uint16
	warning   uint16
}

// retrieveRSPBuilder builds a C-GET-RSP or C-MOVE-RSP. Both have the same shape,
// so the loops differ only in which one they are given.
type retrieveRSPBuilder func(messageID uint16, sopClassUID string, status uint16,
	remaining, completed, failed, warning uint16) *dataset.Dataset

// sendPendingRetrieveRSP reports progress to the requestor partway through a
// retrieval.
//
// The bool says whether to keep going. A failure to send means the requestor's
// association is gone, so no later sub-operation can be reported even if it would
// succeed — the caller stops and counts the remainder as failed.
//
// An encoding failure is different: the association is fine and the next
// sub-operation may well report cleanly, so it is not fatal to the retrieval. It
// is logged rather than swallowed, which is the part that was missing — all four
// loops used to `continue` on it silently.
func (s *SCP) sendPendingRetrieveRSP(ctx context.Context, assoc *Association, ctxID byte,
	build retrieveRSPBuilder, messageID uint16, sopClassUID string,
	remaining uint16, counts subOperationCounts) bool {

	pendingDS := build(messageID, sopClassUID, StatusPending,
		remaining, counts.completed, counts.failed, counts.warning)

	pendingBytes, err := EncodeCommandDataset(pendingDS)
	if err != nil {
		DefaultLogger.Warn("could not encode a pending retrieval response: %v; "+
			"continuing with the remaining sub-operations", err)
		return true
	}

	if err := assoc.SendPData(ctx, ctxID, pendingBytes, true); err != nil {
		DefaultLogger.Error("pending retrieval response failed, abandoning sub-operations: %v", err)
		return false
	}
	return true
}

// sendOneGetSubOperation transfers one instance back to the requestor as a C-STORE
// sub-operation, updating the counts.
//
// C-GET only: a C-MOVE sends to a third party through an SCU of its own, with no
// presentation context on this association to find and no sub-operation message ID
// of ours to allocate.
//
// subMessageID is advanced by the caller and passed in, because sub-operation
// message IDs are per-retrieval and independent of the C-GET's own.
func (s *SCP) sendOneGetSubOperation(ctx context.Context, assoc *Association,
	inst *dataset.Dataset, subMessageID uint16, parentMessageID uint16,
	canceled *cancelFlag, counts *subOperationCounts) {

	// A nil instance is a handler returning something it should not have. Counted
	// as failed rather than skipped, so the numbers the requestor sees add up to
	// what it was told to expect.
	if inst == nil {
		counts.failed++
		return
	}

	instClass, instUID, ok := instanceUIDs(inst)
	if !ok {
		DefaultLogger.Warn("C-GET sub-operation skipped: instance is missing SOP Class or SOP Instance UID")
		counts.failed++
		return
	}

	// The instance travels on the presentation context negotiated for its own SOP
	// Class, which may differ from the C-GET's.
	subCtxID, ok := FindPresentationContextID(assoc.AcceptedContexts(), instClass)
	if !ok {
		DefaultLogger.Warn("C-GET sub-operation skipped: no accepted presentation context for %s", instClass)
		counts.failed++
		return
	}

	if err := s.sendCStoreSubOperation(ctx, assoc, subCtxID, subMessageID,
		instClass, instUID, inst, parentMessageID, canceled); err != nil {
		DefaultLogger.Error("C-GET sub-operation for %s failed: %v", instUID, err)
		counts.failed++
		return
	}

	counts.completed++
}

// sendOneMoveSubOperation transfers one instance to the move destination,
// updating the counts.
//
// The destination is an SCU the caller has already associated with; this exists so
// that the slice and streaming C-MOVE paths count an outcome the same way.
func (s *SCP) sendOneMoveSubOperation(ctx context.Context, dest *SCU,
	inst *dataset.Dataset, counts *subOperationCounts) {

	if inst == nil {
		counts.failed++
		return
	}

	if err := dest.Store(ctx, inst); err != nil {
		DefaultLogger.Error("C-MOVE sub-operation failed: %v", err)
		counts.failed++
		return
	}

	counts.completed++
}

// resolveMoveDestinationOrReport resolves a move destination's AE title to an
// address, reporting the failure and naming the configuration that fixes it.
//
// A C-MOVE names its destination only by AE title, so an unresolvable one cannot
// be guessed at. Both the slice and streaming paths need this before they start,
// and both need to say the same thing about it.
func (s *SCP) resolveMoveDestinationOrReport(moveDest string) (string, bool) {
	address, ok := s.config.resolveMoveDestination(moveDest)
	if !ok {
		DefaultLogger.Warn("C-MOVE destination %q is not configured; set SCPConfig.MoveDestinations "+
			"or SCPConfig.ResolveMoveDestination", moveDest)
		return "", false
	}
	return address, true
}

// associateWithMoveDestination opens an association to a move destination for the
// given instances.
//
// Returns nil when the destination cannot be reached, having reported it. The
// caller counts the instances as failed: a C-MOVE that cannot reach its
// destination has moved nothing, and saying so is more use than an error naming
// the first instance.
func (s *SCP) associateWithMoveDestination(ctx context.Context, destAE, destAddress string,
	contexts []PresentationContextItem) *SCU {

	dest := NewSCU(SCUConfig{
		CallingAE: s.config.AETitle,
		CalledAE:  destAE,
		Address:   destAddress,
		Network:   s.config.Network,
	})

	if err := dest.Associate(ctx, contexts); err != nil {
		DefaultLogger.Error("C-MOVE could not associate with destination %s at %s: %v",
			destAE, destAddress, err)
		return nil
	}
	return dest
}

// remainingAfter returns how many sub-operations are still outstanding when sent
// of total have been attempted.
//
// Written once because the streaming paths had it inline twice each and it
// underflows if written the obvious way: these are uint16, and a handler that
// streams more instances than it counted would otherwise report a remaining count
// near 65535 instead of zero.
func remainingAfter(total, sent int) uint16 {
	if total <= sent {
		return 0
	}
	return uint16(total - sent)
}
