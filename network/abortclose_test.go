package network

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// TestAbortReachesAPeerThatIsStillSending: this side sent its A-ABORT and
// closed the connection behind it. A peer still sending had bytes on their
// way that were never read, a connection closed with bytes unread is reset,
// and the reset discards what the peer has not yet read: the A-ABORT. The
// peer saw a broken connection in place of an abort (#513).
//
// The peer here is associated and then sends a PDU of a type that does not
// exist and a megabyte after it, and only then reads, as a client reads
// after it has sent its request. The SCP aborts at the first six bytes with
// the rest unread. The peer must still read a whole A-ABORT, the ten bytes of
// PS3.8 9.3.8, before the connection ends.
func TestAbortReachesAPeerThatIsStillSending(t *testing.T) {
	server, err := StartServer(context.Background(), SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)

	for round := 0; round < 20; round++ {
		conn, err := net.Dial("tcp", server.Addr())
		if err != nil {
			t.Fatal(err)
		}
		rq, err := EncodePDU(associateRQ("SCANNER", "ARCHIVE"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(rq); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		header := make([]byte, 6)
		if _, err := io.ReadFull(conn, header); err != nil || header[0] != 0x02 {
			t.Fatalf("round %d: no A-ASSOCIATE-AC: %v, type %#02x", round, err, header[0])
		}
		length := int(header[2])<<24 | int(header[3])<<16 | int(header[4])<<8 | int(header[5])
		if _, err := io.CopyN(io.Discard, conn, int64(length)); err != nil {
			t.Fatal(err)
		}

		// A PDU of type 0x7F, which is none, and a megabyte the SCP will
		// not read, written before anything is read: a client sends its
		// request and then waits for the answer. The write may fail once the
		// SCP has stopped reading, and what it reports is not the point.
		_, _ = conn.Write(append([]byte{0x7F, 0, 0, 0, 0, 4, 1, 2, 3, 4}, bytes.Repeat([]byte{0xAA}, 1<<20)...))
		time.Sleep(50 * time.Millisecond)

		abort := make([]byte, 10)
		if _, err := io.ReadFull(conn, abort); err != nil {
			_ = conn.Close()
			t.Fatalf("round %d: the peer read no A-ABORT, the connection gave %v", round, err)
		}
		if abort[0] != 0x07 {
			t.Fatalf("round %d: the peer was sent PDU type %#02x, want an A-ABORT (0x07)", round, abort[0])
		}
		_ = conn.Close()
	}
}
