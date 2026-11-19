package network

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// A stop that lets the work in progress finish.
//
// Stop cancels the server's context first, and every association is served
// with it, so a C-STORE in flight was cut wherever it was: the handler's
// context ended, the requestor was told the store failed though the instance
// had arrived whole, and the connection closed with no A-ABORT. Shutdown stops
// taking new work and lets what has started finish, within a bound.
//
// What "in progress" means is the one decision here. An operation is in
// progress from the moment its command set has been read until its last
// response has been sent. An association waiting for its next request holds
// none, and PS3.8 gives the acceptor no release of its own (A-RELEASE is the
// requestor's, 7.2), so the choice for an idle association is between waiting
// for its peer and aborting it. It is aborted at once: everything its peer
// sent has been answered, so nothing is lost and the peer knows exactly which
// instances were stored (PS3.7 9.1.1), while waiting would cost the whole
// bound for a modality that keeps an association open between studies.
//
// An association in the middle of an operation is different: its peer is
// talking, and most peers release as soon as their last response arrives. It
// finishes the operation and is then given until the bound for its next PDU.
// An A-RELEASE-RQ is answered and the association ends normally; anything
// else is aborted without being started.

// stopAbortTimeout bounds the A-ABORT written to each connection left when a
// Shutdown's bound expires. A peer that has stopped reading would otherwise
// hold the write, and the Shutdown, for good; past it the connection is
// closed without one.
const stopAbortTimeout = time.Second

// liveConnection is a connection the SCP is serving, as Shutdown sees it.
type liveConnection struct {
	transport *Transport
	assoc     *Association
}

// drainState is what an SCP keeps for Shutdown.
type drainState struct {
	// ctx is canceled when a Shutdown begins.
	ctx   context.Context
	begin context.CancelFunc

	mu    sync.Mutex
	conns map[*liveConnection]struct{}
}

// drain returns the SCP's drain state, made on first use so that an SCP
// built without NewSCP still works.
func (s *SCP) drain() *drainState {
	s.drainInit.Do(func() {
		ctx, begin := context.WithCancel(context.Background())
		s.drainer = &drainState{ctx: ctx, begin: begin, conns: make(map[*liveConnection]struct{})}
	})
	return s.drainer
}

// draining reports whether a Shutdown has begun.
func (s *SCP) draining() bool {
	return s.drain().ctx.Err() != nil
}

// track records a connection being served until the returned function is
// called.
func (s *SCP) track(transport *Transport, assoc *Association) func() {
	d := s.drain()
	c := &liveConnection{transport: transport, assoc: assoc}
	d.mu.Lock()
	d.conns[c] = struct{}{}
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		delete(d.conns, c)
		d.mu.Unlock()
	}
}

// idleReadContext is the context the next request of an association is read
// with: ctx, ended also by a Shutdown beginning, so that an association
// waiting for a request is aborted at once rather than at the bound.
func (s *SCP) idleReadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	readCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.drain().ctx, cancel)
	return readCtx, func() {
		stop()
		cancel()
	}
}

// abortStopping aborts an association because the server is stopping. The
// application chose to end it, so the abort is the service-user's (PS3.8
// Table 9-26: source 0, reason not significant).
func (s *SCP) abortStopping(assoc *Association) {
	ctx, cancel := context.WithTimeout(context.Background(), stopAbortTimeout)
	defer cancel()
	_ = assoc.Abort(ctx, AbortSourceServiceUser, 0)
}

// abortLeft aborts every connection still being served, each at once and
// each within stopAbortTimeout, and returns how many of them were
// associations.
func (s *SCP) abortLeft() int {
	d := s.drain()
	d.mu.Lock()
	left := make([]*liveConnection, 0, len(d.conns))
	for c := range d.conns {
		left = append(left, c)
	}
	d.mu.Unlock()

	var wg sync.WaitGroup
	var aborted atomic.Int32
	for _, c := range left {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A write stuck behind one to a peer that does not read holds
			// the transport's lock; closing the socket under it releases
			// both.
			cut := time.AfterFunc(stopAbortTimeout, func() { _ = c.transport.conn.Close() })
			defer cut.Stop()
			if c.assoc != nil && c.assoc.State() == StateAssociated {
				aborted.Add(1)
				s.abortStopping(c.assoc)
				return
			}
			_ = c.transport.Close()
		}()
	}
	wg.Wait()
	return int(aborted.Load())
}

// ShutdownError is what Server.Shutdown returns when its context ended
// before every association had: Aborted associations were sent an A-ABORT
// at the bound, with whatever operation they were in cut short. It unwraps
// to the context's error, so errors.Is(err, context.DeadlineExceeded) holds.
//
// Associations aborted before the bound are not counted: one that was
// waiting for a request, and one that sent a request after its operation
// finished, had every request they sent answered or refused unstarted.
type ShutdownError struct {
	// Aborted is the number of associations aborted at the bound. It can be
	// zero when only connections that had not yet asked for an association
	// were left.
	Aborted int
	// Err is the error of the context Shutdown was given.
	Err error
}

// Error says how many associations were cut short, and why.
func (e *ShutdownError) Error() string {
	return fmt.Sprintf("network: shutdown: %d association(s) aborted at the bound: %v", e.Aborted, e.Err)
}

// Unwrap returns the context's error.
func (e *ShutdownError) Unwrap() error { return e.Err }

// Shutdown stops the server without cutting the work in progress.
//
// It closes the listener, so no new connection is accepted, and refuses an
// association requested on a connection that was already open with
// rejected-transient, service-provider (presentation), temporary-congestion
// (PS3.8 Table 9-21), so the caller tries again later. An association
// waiting for its next request is aborted at once: everything it sent has
// been answered. One in the middle of an operation finishes it, its
// responses are sent, and its peer's next PDU is waited for: an
// A-RELEASE-RQ is answered and the association released, anything else is
// aborted without being started. Each abort is the service-user's (PS3.8
// Table 9-26, source 0).
//
// It waits for the associations to end until ctx is done, and returns nil
// once they all have: nothing was cut short. When ctx ends first, each
// association left is sent an A-ABORT and closed (within a second; a peer
// that does not read is closed without one), the context the handlers were
// given is canceled, and a *ShutdownError is returned, which counts the
// associations cut short and unwraps to ctx.Err(). A handler that ignores its context keeps its
// goroutine until it returns; Stop waits for it.
//
// Stop is unchanged: it cancels at once.
func (s *Server) Shutdown(ctx context.Context) error {
	s.scp.drain().begin()
	s.Logger.Info("Server %s stopping: no new associations, waiting for those in progress", s.scp.config.AETitle)
	_ = s.scp.Close()

	finished := make(chan struct{})
	go func() {
		s.wg.Wait()
		s.scp.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		s.cancel()
		return nil
	case <-ctx.Done():
	}
	aborted := s.scp.abortLeft()
	s.cancel()
	return &ShutdownError{Aborted: aborted, Err: ctx.Err()}
}
