package network

import (
	"net"
	"testing"
	"time"
)

// A server always has an EventManager, so whether a received PDU's bytes are
// copied must depend on what a handler asks for and not on the manager being
// there (#469). These read P-DATA-TF PDUs through Transport.ReadPDU with no
// observer, with an observer whose handlers are of association events, and
// with a handler of EVTPDURecv, which is given the bytes, and measure what each
// allocates for a PDU.

// repeatConn is a net.Conn that reads the same bytes over and over.
type repeatConn struct {
	data []byte
	at   int
}

func (c *repeatConn) Read(p []byte) (int, error) {
	n := copy(p, c.data[c.at:])
	c.at = (c.at + n) % len(c.data)
	return n, nil
}
func (c *repeatConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *repeatConn) Close() error                     { return nil }
func (c *repeatConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *repeatConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *repeatConn) SetDeadline(time.Time) error      { return nil }
func (c *repeatConn) SetReadDeadline(time.Time) error  { return nil }
func (c *repeatConn) SetWriteDeadline(time.Time) error { return nil }

// pduSize is the data of each P-DATA-TF read: a PDU of the largest size this
// library advertises by default, so a copy of it is plain to see.
const pduSize = 64 << 10

// readPDUs reads P-DATA-TF PDUs through a transport with the events given, nil
// for none.
func readPDUs(b *testing.B, events *EventManager) {
	b.Helper()
	pdu, err := EncodePDU(&PDataTF{PDVItems: []PDVItem{{
		PresentationContextID: 1, IsLast: true, Data: make([]byte, pduSize),
	}}})
	if err != nil {
		b.Fatal(err)
	}
	transport := NewTransport(&repeatConn{data: pdu}, uint32(len(pdu)))
	transport.SetReceiveLimit(uint32(len(pdu)))
	transport.observe(events, nil)
	b.SetBytes(pduSize)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := transport.ReadPDU(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// associationEvents is an EventManager with handlers of association events
// only, as most servers have.
func associationEvents() *EventManager {
	events := NewEventManager()
	events.On(EVTAssocAccepted, func(*Event) {})
	events.On(EVTAssocReleased, func(*Event) {})
	return events
}

// BenchmarkReadPDU: allocations for one 64 KiB P-DATA-TF read with no
// observer, with an EventManager whose handlers want no PDU bytes, and with a
// handler of EVTPDURecv.
func BenchmarkReadPDU(b *testing.B) {
	b.Run("no events", func(b *testing.B) { readPDUs(b, nil) })
	b.Run("association handlers only", func(b *testing.B) { readPDUs(b, associationEvents()) })
	b.Run("EVTPDURecv handler", func(b *testing.B) {
		events := associationEvents()
		events.On(EVTPDURecv, func(*Event) {})
		readPDUs(b, events)
	})
}

// TestReceivedPDUBytesAreCopiedOnlyForAHandlerThatWantsThem: a P-DATA-TF read
// by a transport whose EventManager has no handler of EVTPDURecv allocates what
// one read with no EventManager does, and one read with a handler of
// EVTPDURecv allocates the PDU again, which is the copy that handler is given.
func TestReceivedPDUBytesAreCopiedOnlyForAHandlerThatWantsThem(t *testing.T) {
	measure := func(events *EventManager) int64 {
		return testing.Benchmark(func(b *testing.B) { readPDUs(b, events) }).AllocedBytesPerOp()
	}
	none := measure(nil)
	assoc := measure(associationEvents())
	watched := associationEvents()
	watched.On(EVTPDURecv, func(*Event) {})
	copied := measure(watched)
	t.Logf("bytes allocated reading one %d byte P-DATA-TF: no events %d, association handlers only %d, EVTPDURecv handler %d",
		pduSize, none, assoc, copied)

	if assoc > none+1024 {
		t.Errorf("an EventManager with no handler of EVTPDURecv allocates %d bytes a PDU against %d with none: the bytes were copied",
			assoc, none)
	}
	if copied < none+pduSize {
		t.Errorf("a handler of EVTPDURecv was given the PDU with %d bytes allocated, against %d with none: it is not a copy of %d bytes",
			copied, none, pduSize)
	}
}
