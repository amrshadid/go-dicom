package network

import (
	"context"
	"fmt"
	"runtime/debug"
)

// A handler is the application's code, run on what a peer chose to send, and
// a panic in it was a panic on a goroutine nothing recovered: the process
// ended, with its listener and every other association in progress. A panic
// now ends the association it happened on and nothing else.

// handlerPanic is a panic recovered on a goroutine the SCP started for a
// handler, carried to the association's goroutine as the handler's error.
type handlerPanic struct {
	value any
	stack []byte
}

func (p *handlerPanic) Error() string { return fmt.Sprintf("handler panicked: %v", p.value) }

// guardHandler runs a handler the SCP started on a goroutine of its own, and
// returns a panic in it as a *handlerPanic with the stack of where it
// happened. awaitHandler raises it again on the association's goroutine, so
// that what a panic does is decided in one place, recoverAssociation.
func guardHandler(run func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = &handlerPanic{value: p, stack: debug.Stack()}
		}
	}()
	return run()
}

// awaitHandler waits for a handler started with guardHandler and returns its
// error, panicking with the handler's own panic if it had one.
func awaitHandler(done <-chan error) error {
	err := <-done
	if p, ok := err.(*handlerPanic); ok {
		panic(p)
	}
	return err
}

// recoverAssociation is deferred on the goroutine that serves an association.
// It ends that association after a panic: the panic is logged with its stack,
// the peer is sent an A-ABORT if there is an association to abort (PS3.8
// 9.3.8: service-provider, reason-not-specified), and the connection is
// closed by the caller's own deferred Close. No status is sent for the
// message in progress: how far it got is not known, and a response the
// handler did not choose could report an instance stored that was not.
func (s *SCP) recoverAssociation(assoc *Association) {
	p := recover()
	if p == nil {
		return
	}
	stack := debug.Stack()
	if hp, ok := p.(*handlerPanic); ok {
		p, stack = hp.value, hp.stack
	}
	DefaultLogger.Error("panic serving the association with %q from %s; aborting it: %v\n%s",
		assoc.CallingAE(), assoc.transport.RemoteAddr(), p, stack)
	if assoc.State() == StateAssociated {
		// Its own context: the one the association was served with may be
		// what the handler was given, and done.
		timeout := s.config.Network.ARTIMTimeout
		if timeout <= 0 {
			timeout = DefaultARTIMTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_ = assoc.Abort(ctx, AbortSourceServiceProvider, 0)
	}
}
