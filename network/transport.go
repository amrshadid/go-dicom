package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Transport wraps a TCP connection with DICOM-specific read/write operations.
type Transport struct {
	conn       net.Conn
	mu         sync.Mutex
	maxPDUSize uint32
	closed     bool
}

// NewTransport creates a new Transport wrapping an existing connection.
func NewTransport(conn net.Conn, maxPDUSize uint32) *Transport {
	if maxPDUSize == 0 {
		maxPDUSize = DefaultMaxPDUSize
	}
	return &Transport{
		conn:       conn,
		maxPDUSize: maxPDUSize,
	}
}

// Dial establishes a TCP connection to the given address.
func Dial(ctx context.Context, address string, timeout time.Duration) (*Transport, error) {
	if timeout == 0 {
		timeout = DefaultNetworkTimeout
	}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, NewCommunicationError("CONNECT", fmt.Sprintf("failed to connect to %s", address), err)
	}

	return NewTransport(conn, DefaultMaxPDUSize), nil
}

// WritePDU encodes and sends a PDU over the connection.
func (t *Transport) WritePDU(ctx context.Context, pdu PDU) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return NewCommunicationError("CLOSED", "transport is closed", nil)
	}

	data, err := pdu.Encode()
	if err != nil {
		return NewPDUErrorf("ENCODE", "failed to encode PDU: %v", err)
	}

	// Set write deadline from context
	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetWriteDeadline(deadline); err != nil {
			return NewCommunicationError("DEADLINE", "failed to set write deadline", err)
		}
	}

	_, err = t.conn.Write(data)
	if err != nil {
		// errors.As rather than a type assertion, for the same reason as in
		// ReadPDU: a wrapped network error does not satisfy the assertion, so a
		// write that timed out was reported as a generic write failure.
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return NewTimeoutError("write", "write operation timed out")
		}
		return NewCommunicationError("WRITE", "failed to write PDU", err)
	}

	return nil
}

// ReadPDU reads and decodes a PDU from the connection.
func (t *Transport) ReadPDU(ctx context.Context) (PDU, error) {
	if t.closed {
		return nil, NewCommunicationError("CLOSED", "transport is closed", nil)
	}

	// Set read deadline from context
	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetReadDeadline(deadline); err != nil {
			return nil, NewCommunicationError("DEADLINE", "failed to set read deadline", err)
		}
	}

	// A deadline alone does not make this cancellable: a context with no
	// deadline never unblocks the read, and one canceled before its deadline
	// is ignored. Pushing the deadline into the past on cancellation interrupts
	// the read at once, which is what lets a caller stop waiting for a peer
	// that has gone quiet.
	//
	// Interrupting rather than polling is the point. A short deadline used to
	// poll for an incoming message can expire midway through a PDU body,
	// leaving the stream positioned inside a message with no way to
	// resynchronize — so a cancellation that never fires costs nothing, while a
	// poll that fires at the wrong moment corrupts the association.
	if ctx.Done() != nil {
		stop := context.AfterFunc(ctx, func() {
			_ = t.conn.SetReadDeadline(time.Now())
		})
		defer stop()
	}

	pdu, err := DecodePDU(t.conn)
	if err != nil {
		// errors.As, not a type assertion: DecodePDU wraps the network error in
		// its own, so asserting on the returned value never matched and no read
		// timeout was ever classified as one. It surfaced as a generic decode
		// failure instead, which reads like a malformed message from the peer
		// rather than silence from it.
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			// Cancellation is implemented as a deadline, so both arrive here as
			// a timeout. Reporting them alike would tell a caller its peer was
			// slow when in fact the caller itself stopped waiting.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, NewTimeoutError("read", "read operation timed out")
		}
		return nil, err
	}

	return pdu, nil
}

// SetMaxPDUSize updates the maximum PDU size (typically after negotiation).
func (t *Transport) SetMaxPDUSize(size uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.maxPDUSize = size
}

// MaxPDUSize returns the current maximum PDU size.
func (t *Transport) MaxPDUSize() uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.maxPDUSize
}

// Close closes the underlying TCP connection.
func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	return t.conn.Close()
}

// LocalAddr returns the local network address.
func (t *Transport) LocalAddr() net.Addr {
	return t.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (t *Transport) RemoteAddr() net.Addr {
	return t.conn.RemoteAddr()
}

// IsClosed returns whether the transport has been closed.
func (t *Transport) IsClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// Listener wraps a TCP listener for accepting DICOM associations.
type Listener struct {
	listener net.Listener
	mu       sync.Mutex
	closed   bool
}

// Listen creates a new TCP listener on the specified address.
func Listen(address string) (*Listener, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, NewCommunicationError("LISTEN", fmt.Sprintf("failed to listen on %s", address), err)
	}
	return &Listener{listener: ln}, nil
}

// Accept waits for and returns the next incoming connection as a Transport.
func (l *Listener) Accept(ctx context.Context) (*Transport, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, NewCommunicationError("CLOSED", "listener is closed", nil)
	}
	l.mu.Unlock()

	// Use a goroutine to support context cancellation
	type acceptResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan acceptResult, 1)
	go func() {
		conn, err := l.listener.Accept()
		ch <- acceptResult{conn, err}
	}()

	select {
	case <-ctx.Done():
		l.listener.Close()
		return nil, ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return nil, NewCommunicationError("ACCEPT", "failed to accept connection", result.err)
		}
		return NewTransport(result.conn, DefaultMaxPDUSize), nil
	}
}

// Close closes the listener.
func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	l.closed = true
	return l.listener.Close()
}

// Addr returns the listener's address.
func (l *Listener) Addr() net.Addr {
	return l.listener.Addr()
}
