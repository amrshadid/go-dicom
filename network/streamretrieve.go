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
	var counts subOperationCounts
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

		subMessageID++
		s.sendOneGetSubOperation(ctx, assoc, inst, subMessageID, messageID, canceled, &counts)

		if !s.sendPendingRetrieveRSP(ctx, assoc, ctxID, BuildCGetRSP,
			messageID, sopClassUID, remainingAfter(total, sent), counts) {
			stopHandler()
			for range out { //nolint:revive // draining so the handler can return
			}
			<-handlerDone
			return StatusUnableToProcess
		}
	}

	handlerErr := <-handlerDone

	remaining := remainingAfter(total, sent)

	switch {
	case canceled.wasSet():
		status = StatusQRCancelMatchingTerminated
	case handlerErr != nil:
		DefaultLogger.Error("streaming C-GET handler failed: %v", handlerErr)
		status = StatusUnableToProcess
	case counts.failed > 0:
		status = StatusGetWarningPartial
	default:
		status = StatusSuccess
		remaining = 0
	}

	rspDS := BuildCGetRSP(messageID, sopClassUID, status, remaining,
		counts.completed, counts.failed, counts.warning)
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

	var counts subOperationCounts
	sent := 0

	for inst := range out {
		if watcher != nil && watcher.wasCanceled() {
			stopHandler()
			continue
		}
		sent++

		if inst == nil {
			counts.failed++
			continue
		}

		if dest == nil {
			dest = s.associateWithMoveDestination(ctx, req.MoveDestination, destAddress,
				storageContextsFor([]*dataset.Dataset{inst}))
			if dest == nil {
				counts.failed++
				continue
			}
		}

		s.sendOneMoveSubOperation(ctx, dest, inst, &counts)

		if !s.sendPendingRetrieveRSP(ctx, assoc, ctxID, BuildCMoveRSP,
			messageID, sopClassUID, remainingAfter(total, sent), counts) {
			stopHandler()
			for range out { //nolint:revive // draining so the handler can return
			}
			<-handlerDone
			return
		}
	}

	handlerErr := <-handlerDone

	remaining := remainingAfter(total, sent)

	status := StatusSuccess
	switch {
	case watcher != nil && watcher.wasCanceled():
		status = StatusQRCancelMatchingTerminated
	case handlerErr != nil:
		DefaultLogger.Error("streaming C-MOVE handler failed: %v", handlerErr)
		status = StatusUnableToProcess
	case counts.failed > 0:
		status = StatusGetWarningPartial
	default:
		remaining = 0
	}

	s.sendMoveFinalRemaining(ctx, assoc, ctxID, messageID, sopClassUID, status,
		remaining, counts.completed, counts.failed, counts.warning)
}
