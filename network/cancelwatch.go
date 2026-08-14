package network

import (
	"context"
	"sync/atomic"

	"github.com/amrshadid/go-dicom/dataset"
)

// CFindStreamer is an optional interface for C-FIND handlers that produce
// matches incrementally and can stop early.
//
// A handler that returns a slice, as Handler.HandleCFind does, has to compute
// every match before the first one is sent, so a C-CANCEL cannot reach it and
// a query over a large archive cannot be abandoned. A streaming handler emits
// matches as it finds them and observes cancellation through its context.
//
// Implementing it is optional: an SCP whose handler does not is served by the
// slice path exactly as before.
//
// The channel must not be closed by the handler — the SCP owns it. Return when
// there is nothing more to send, or when ctx is done.
type CFindStreamer interface {
	// StreamCFind sends each match on out and returns when finished.
	//
	// ctx is canceled if the requestor sends C-CANCEL, so a handler that
	// respects it stops doing work nobody is waiting for. Returning ctx.Err()
	// is fine; the SCP reports the cancellation to the requestor either way.
	StreamCFind(ctx context.Context, req *CFindRequest, out chan<- *CFindResponse) error
}

// CGetStreamer is an optional interface for C-GET handlers that produce their
// matching instances incrementally.
//
// Handler.HandleCGet returns a slice, so every instance of a study is held in
// memory before the first one is sent, and the matching phase cannot be
// interrupted. A streaming handler emits instances as it finds them and stops
// when ctx is done because the requestor sent C-CANCEL.
//
// It is two methods rather than one because PS3.4 requires each pending
// response to carry the number of sub-operations still outstanding, and that
// cannot be derived from a stream that has not finished. An archive can answer
// it with a count query before fetching anything, which is the same order it
// would do the work in anyway.
//
// Implementing it is optional; a handler that does not is served by the slice
// path unchanged. The SCP owns the channel and closes it — return when there is
// nothing more to send, or when ctx is done.
type CGetStreamer interface {
	// CountCGetMatches returns how many instances match, before any are sent.
	CountCGetMatches(ctx context.Context, req *CGetRequest) (int, error)

	// StreamCGet sends each matching instance on out.
	StreamCGet(ctx context.Context, req *CGetRequest, out chan<- *dataset.Dataset) error
}

// CMoveStreamer is the C-MOVE equivalent of CGetStreamer.
type CMoveStreamer interface {
	// CountCMoveMatches returns how many instances match, before any are sent.
	CountCMoveMatches(ctx context.Context, req *CMoveRequest) (int, error)

	// StreamCMove sends each matching instance on out.
	StreamCMove(ctx context.Context, req *CMoveRequest, out chan<- *dataset.Dataset) error
}

// cancelFlag records that a C-CANCEL arrived for an operation in progress.
//
// C-GET needs this rather than a cancelWatcher. Its sub-operations travel on the
// same association as the request, so the SCP is already reading that connection
// to collect each C-STORE-RSP; a watcher reading concurrently would be a second
// reader on one connection, which interleaves PDU bodies. The cancel is noticed
// where it actually arrives — in that response read — instead.
type cancelFlag struct {
	canceled atomic.Bool
}

func (f *cancelFlag) set()         { f.canceled.Store(true) }
func (f *cancelFlag) wasSet() bool { return f.canceled.Load() }

// isCancelFor reports whether a command data set is a C-CANCEL naming messageID.
func isCancelFor(cmdDS *dataset.Dataset, messageID uint16) bool {
	commandField, msgID, _, err := ParseCommandDataset(cmdDS)
	if err != nil {
		return false
	}
	return commandField == CommandCCancelRQ && msgID == messageID
}

// cancelWatcher watches an association for a C-CANCEL naming a given message
// while an operation runs, and cancels a context when one arrives.
//
// It reads the association directly, which is safe only because the SCP's own
// read loop is blocked inside the operation being watched: there is exactly one
// reader at a time. Anything read that is not the awaited C-CANCEL is pushed
// back, so a message the peer believes it delivered is not lost.
type cancelWatcher struct {
	cancel   context.CancelFunc
	stop     context.CancelFunc
	done     chan struct{}
	canceled chan struct{}
}

// watchForCancel starts watching for a C-CANCEL naming messageID.
//
// The returned context is canceled if one arrives. Call finish when the
// operation is over, before returning to the association's read loop — the
// watcher holds the reader until then, and a second reader on the same
// connection interleaves PDU bodies.
func watchForCancel(ctx context.Context, assoc *Association, messageID uint16) (context.Context, *cancelWatcher) {
	opCtx, cancel := context.WithCancel(ctx)

	// A separate context stops the watcher's own read without canceling the
	// operation: finishing normally must not look like a cancellation.
	readCtx, stop := context.WithCancel(ctx)

	w := &cancelWatcher{
		cancel:   cancel,
		stop:     stop,
		done:     make(chan struct{}),
		canceled: make(chan struct{}),
	}

	go func() {
		defer close(w.done)

		// Exactly one read. A loop would be wrong rather than merely
		// unnecessary: a message that is not the awaited cancel gets pushed
		// back, and reading again would return that same message immediately,
		// forever.
		//
		// Stopping after one costs nothing in practice. Presentation contexts
		// are negotiated with an asynchronous operations window of one unless a
		// peer asks otherwise, so a C-CANCEL is the only message that should
		// arrive while an operation is in flight. If something else does, it is
		// preserved and the operation simply runs to completion — the outcome
		// before any of this existed.
		ctxID, data, isCmd, err := assoc.ReceivePData(readCtx)
		if err != nil {
			// Stopped because the operation finished, or the association
			// failed. Either way there is nothing left to watch.
			return
		}

		if isCmd {
			if cmdDS, decErr := DecodeCommandDataset(data); decErr == nil {
				commandField, msgID, _, _ := ParseCommandDataset(cmdDS)
				if commandField == CommandCCancelRQ && msgID == messageID {
					close(w.canceled)
					w.cancel()
					return
				}
			}
		}

		// Not the awaited cancel. Hand it back so whoever reads next sees it.
		assoc.PushBack(ctxID, data, isCmd)
	}()

	return opCtx, w
}

// finish stops the watcher and waits for it to release the association.
//
// Waiting matters: returning while the watcher is still inside ReceivePData
// leaves two readers on one connection, and two readers interleave PDU bodies.
func (w *cancelWatcher) finish() {
	w.stop()
	<-w.done
	w.cancel()
}

// wasCanceled reports whether a C-CANCEL for the watched message arrived.
func (w *cancelWatcher) wasCanceled() bool {
	select {
	case <-w.canceled:
		return true
	default:
		return false
	}
}

// streamCFindResponses runs a streaming C-FIND handler, sending each match as
// it arrives and stopping if the requestor cancels.
//
// Returns the status to report in the final response: success when the handler
// finished, or the cancel status when the requestor asked it to stop.
func (s *SCP) streamCFindResponses(ctx context.Context, assoc *Association, streamer CFindStreamer,
	req *CFindRequest, ctxID byte, messageID uint16, sopClassUID string) uint16 {

	opCtx, watcher := watchForCancel(ctx, assoc, messageID)
	defer watcher.finish()

	out := make(chan *CFindResponse)
	handlerDone := make(chan error, 1)
	go func() {
		defer close(out)
		handlerDone <- streamer.StreamCFind(opCtx, req, out)
	}()

	for resp := range out {
		if watcher.wasCanceled() {
			// Drain rather than return: the handler may still be sending, and
			// leaving it blocked on an unread channel would leak the goroutine.
			continue
		}
		if err := s.sendCFindPending(ctx, assoc, ctxID, messageID, sopClassUID, resp); err != nil {
			DefaultLogger.Error("failed to send C-FIND match: %v", err)
			break
		}
	}

	if err := <-handlerDone; err != nil && !watcher.wasCanceled() {
		DefaultLogger.Error("streaming C-FIND handler failed: %v", err)
		return StatusUnableToProcess
	}

	if watcher.wasCanceled() {
		return StatusQRCancelMatchingTerminated
	}
	return StatusSuccess
}

// sendCFindPending sends one pending C-FIND response and its data set.
func (s *SCP) sendCFindPending(ctx context.Context, assoc *Association, ctxID byte,
	messageID uint16, sopClassUID string, resp *CFindResponse) error {

	rspDS := BuildCFindRSP(messageID, sopClassUID, StatusPending, resp.DataSet != nil)
	rspBytes, err := EncodeCommandDataset(rspDS)
	if err != nil {
		return err
	}
	if err := assoc.SendPData(ctx, ctxID, rspBytes, true); err != nil {
		return err
	}

	if resp.DataSet == nil {
		return nil
	}
	dataBytes, err := EncodeDataset(resp.DataSet, assoc.TransferSyntaxFor(ctxID))
	if err != nil {
		return err
	}
	return assoc.SendPData(ctx, ctxID, dataBytes, false)
}
