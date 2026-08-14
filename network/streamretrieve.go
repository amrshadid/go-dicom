package network

import (
	"context"

	"github.com/amrshadid/go-dicom/dataset"
)

// Streaming retrieval, for handlers that can produce their matches one at a
// time rather than as a slice.
//
// The slice path holds every instance of a study in memory before sending the
// first, and cannot be interrupted partway through matching. Both matter on a
// real archive: a study is hundreds of instances, and a requestor that cancels
// is asking for the work to stop, not for it to finish unobserved.

// streamGetSubOperations transfers instances from a streaming C-GET handler,
// sending each as it arrives.
//
// The cancel is noticed in the C-STORE-RSP read, as in the slice path, because
// the sub-operations share the requestor's association. Once it is seen the
// handler's context is canceled too, so a handler still walking the archive
// stops walking it.
func (s *SCP) streamGetSubOperations(ctx context.Context, assoc *Association, streamer CGetStreamer,
	req *CGetRequest, ctxID byte, messageID uint16, sopClassUID string) (status uint16) {

	total, err := streamer.CountCGetMatches(ctx, req)
	if err != nil {
		DefaultLogger.Error("streaming C-GET could not count matches: %v", err)
		return StatusUnableToProcess
	}

	handlerCtx, stopHandler := context.WithCancel(ctx)
	defer stopHandler()

	out := make(chan *dataset.Dataset)
	handlerDone := make(chan error, 1)
	go func() {
		defer close(out)
		handlerDone <- streamer.StreamCGet(handlerCtx, req, out)
	}()

	canceled := &cancelFlag{}
	var completed, failed, warning uint16
	var subMessageID uint16
	sent := 0

	for inst := range out {
		if canceled.wasSet() {
			// Stop the handler, but keep draining: it may be blocked mid-send,
			// and abandoning the channel would leak its goroutine.
			stopHandler()
			continue
		}
		sent++

		remaining := uint16(0)
		if total > sent {
			remaining = uint16(total - sent)
		}

		if inst == nil {
			failed++
			continue
		}
		instClass, instUID, ok := instanceUIDs(inst)
		if !ok {
			DefaultLogger.Warn("C-GET sub-operation skipped: instance is missing SOP Class or SOP Instance UID")
			failed++
			continue
		}
		subCtxID, ok := FindPresentationContextID(assoc.AcceptedContexts(), instClass)
		if !ok {
			DefaultLogger.Warn("C-GET sub-operation skipped: no accepted presentation context for %s", instClass)
			failed++
			continue
		}

		subMessageID++
		if err := s.sendCStoreSubOperation(ctx, assoc, subCtxID, subMessageID,
			instClass, instUID, inst, messageID, canceled); err != nil {
			DefaultLogger.Error("C-GET sub-operation for %s failed: %v", instUID, err)
			failed++
		} else {
			completed++
		}

		pendingDS := BuildCGetRSP(messageID, sopClassUID, StatusPending,
			remaining, completed, failed, warning)
		pendingBytes, encErr := EncodeCommandDataset(pendingDS)
		if encErr != nil {
			continue
		}
		if err := assoc.SendPData(ctx, ctxID, pendingBytes, true); err != nil {
			DefaultLogger.Error("C-GET pending response failed, abandoning sub-operations: %v", err)
			stopHandler()
			for range out { //nolint:revive // draining so the handler can return
			}
			<-handlerDone
			return StatusUnableToProcess
		}
	}

	handlerErr := <-handlerDone

	remaining := uint16(0)
	if total > sent {
		remaining = uint16(total - sent)
	}

	switch {
	case canceled.wasSet():
		status = StatusQRCancelMatchingTerminated
	case handlerErr != nil:
		DefaultLogger.Error("streaming C-GET handler failed: %v", handlerErr)
		status = StatusUnableToProcess
	case failed > 0:
		status = StatusGetWarningPartial
	default:
		status = StatusSuccess
		remaining = 0
	}

	rspDS := BuildCGetRSP(messageID, sopClassUID, status, remaining, completed, failed, warning)
	rspBytes, err := EncodeCommandDataset(rspDS)
	if err != nil {
		return status
	}
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
	return status
}

// streamMoveSubOperations is the C-MOVE equivalent.
//
// Cancellation is detected differently here: sub-operations go to a third party,
// so nothing reads the requestor's association and a cancelWatcher can hold it,
// exactly as in the slice path.
func (s *SCP) streamMoveSubOperations(ctx context.Context, assoc *Association, streamer CMoveStreamer,
	req *CMoveRequest, ctxID byte, messageID uint16, sopClassUID, destAddress string,
	watcher *cancelWatcher) {

	total, err := streamer.CountCMoveMatches(ctx, req)
	if err != nil {
		DefaultLogger.Error("streaming C-MOVE could not count matches: %v", err)
		s.sendMoveFinal(ctx, assoc, ctxID, messageID, sopClassUID, StatusUnableToProcess, 0, 0, 0)
		return
	}

	handlerCtx, stopHandler := context.WithCancel(ctx)
	defer stopHandler()

	out := make(chan *dataset.Dataset)
	handlerDone := make(chan error, 1)
	go func() {
		defer close(out)
		handlerDone <- streamer.StreamCMove(handlerCtx, req, out)
	}()

	// The destination association is opened lazily: a query that matches
	// nothing should not connect to the destination at all.
	var dest *SCU
	defer func() {
		if dest != nil {
			_ = dest.Release(ctx)
		}
	}()

	var completed, failed, warning uint16
	sent := 0

	for inst := range out {
		if watcher != nil && watcher.wasCanceled() {
			stopHandler()
			continue
		}
		sent++

		if inst == nil {
			failed++
			continue
		}

		if dest == nil {
			dest = NewSCU(SCUConfig{
				CallingAE: s.config.AETitle,
				CalledAE:  req.MoveDestination,
				Address:   destAddress,
				Network:   s.config.Network,
			})
			if err := dest.Associate(ctx, storageContextsFor([]*dataset.Dataset{inst})); err != nil {
				DefaultLogger.Error("C-MOVE could not associate with destination %s at %s: %v",
					req.MoveDestination, destAddress, err)
				dest = nil
				failed++
				continue
			}
		}

		if err := dest.Store(ctx, inst); err != nil {
			DefaultLogger.Error("C-MOVE sub-operation failed: %v", err)
			failed++
		} else {
			completed++
		}

		remaining := uint16(0)
		if total > sent {
			remaining = uint16(total - sent)
		}
		pendingDS := BuildCMoveRSP(messageID, sopClassUID, StatusPending,
			remaining, completed, failed, warning)
		pendingBytes, encErr := EncodeCommandDataset(pendingDS)
		if encErr != nil {
			continue
		}
		if err := assoc.SendPData(ctx, ctxID, pendingBytes, true); err != nil {
			DefaultLogger.Error("C-MOVE pending response failed, abandoning sub-operations: %v", err)
			stopHandler()
			for range out { //nolint:revive // draining so the handler can return
			}
			<-handlerDone
			return
		}
	}

	handlerErr := <-handlerDone

	remaining := uint16(0)
	if total > sent {
		remaining = uint16(total - sent)
	}

	status := StatusSuccess
	switch {
	case watcher != nil && watcher.wasCanceled():
		status = StatusQRCancelMatchingTerminated
	case handlerErr != nil:
		DefaultLogger.Error("streaming C-MOVE handler failed: %v", handlerErr)
		status = StatusUnableToProcess
	case failed > 0:
		status = StatusGetWarningPartial
	default:
		remaining = 0
	}

	s.sendMoveFinalRemaining(ctx, assoc, ctxID, messageID, sopClassUID, status,
		remaining, completed, failed, warning)
}
