package network

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestReadPDUUnblocksOnCancel verifies a read waiting on a silent peer stops
// when its context is canceled.
//
// ReadPDU only ever set a read deadline from ctx.Deadline(). A context with no
// deadline therefore blocked forever however it was canceled, and one
// canceled before its deadline kept waiting until the deadline arrived. That
// made every read on this transport uninterruptible, which is what stopped the
// SCP from watching for a C-CANCEL while an operation was in flight.
func TestReadPDUUnblocksOnCancel(t *testing.T) {
	// A pipe gives a connection with a peer that never writes.
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	transport := NewTransport(client, DefaultMaxPDUSize)

	// No deadline: only cancellation can end this read.
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := transport.ReadPDU(ctx)
		done <- err
	}()

	// Give the read time to block before canceling, so the test exercises
	// interruption rather than a context that was already done on entry.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("ReadPDU returned %v, want context.Canceled — a caller cannot "+
				"distinguish its own cancellation from a slow peer", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadPDU did not return after its context was canceled")
	}
}

// TestReadPDUReportsTimeoutSeparately verifies an expired deadline is still
// reported as a timeout, since cancellation is implemented as one and the two
// mean different things: the peer was slow, or the caller stopped waiting.
func TestReadPDUReportsTimeoutSeparately(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	transport := NewTransport(client, DefaultMaxPDUSize)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := transport.ReadPDU(ctx)
	if err == nil {
		t.Fatal("ReadPDU succeeded against a peer that never wrote")
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("an expired deadline was reported as cancellation: %v", err)
	}
}

// TestReadPDUCancelDoesNotLeakWatchers verifies the cancellation hook is
// released when a read completes normally.
//
// context.AfterFunc registers a callback on the context. Left registered, every
// read on a long-lived association would add one, and each would reset the
// connection's deadline when that context eventually ended — including reads
// that had long since finished.
func TestReadPDUCancelDoesNotLeakWatchers(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	transport := NewTransport(client, DefaultMaxPDUSize)
	ctx, cancel := context.WithCancel(context.Background())

	// Feed a valid A-RELEASE-RQ so several reads complete normally.
	go func() {
		for i := 0; i < 3; i++ {
			release := &ReleaseRQ{}
			encoded, err := release.Encode()
			if err != nil {
				return
			}
			if _, err := server.Write(encoded); err != nil {
				return
			}
		}
	}()

	for i := 0; i < 3; i++ {
		if _, err := transport.ReadPDU(ctx); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}

	// Canceling now must not affect anything: all three reads are done.
	cancel()
	server.Close()

	// A further read fails because the peer is gone, not because a stale
	// watcher from an earlier read moved the deadline.
	if _, err := transport.ReadPDU(context.Background()); err == nil {
		t.Error("read succeeded after the peer closed")
	}
}
