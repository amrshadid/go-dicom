package network

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// refusedConnection opens a connection to an SCP that refuses everyone, sends
// an A-ASSOCIATE-RQ and reads the A-ASSOCIATE-RJ, and returns the connection
// as it then is.
func refusedConnection(t *testing.T, artim time.Duration) net.Conn {
	t.Helper()
	addr, _ := admitServer(t, SCPConfig{
		AETitle: "ARCHIVE",
		Network: NetworkConfig{ARTIMTimeout: artim},
		Admit: func(context.Context, *AssociationRequest) *AssociationRejection {
			return RejectCallingAE()
		},
	})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	raw, err := EncodePDU(associateRQ("SCANNER", "ARCHIVE"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(raw); err != nil {
		t.Fatal(err)
	}
	// An A-ASSOCIATE-RJ is ten bytes: the six of every PDU header and four of
	// its own (PS3.8 9.3.4).
	rj := make([]byte, 10)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, rj); err != nil {
		t.Fatalf("reading the A-ASSOCIATE-RJ: %v", err)
	}
	if rj[0] != 0x03 {
		t.Fatalf("the SCP answered PDU type %#02x, want an A-ASSOCIATE-RJ", rj[0])
	}
	return conn
}

// TestRefusedConnectionIsTheCallersToClose: the SCP closed the connection
// behind its A-ASSOCIATE-RJ, so a caller could see the close before it had
// acted on the rejection (#430). It now waits in Sta13 for the caller to close
// (PS3.8 9.2, AE-8): half a second after the rejection the connection is
// still open, which a read that times out, and does not end, shows.
func TestRefusedConnectionIsTheCallersToClose(t *testing.T) {
	conn := refusedConnection(t, 30*time.Second)

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err := conn.Read(make([]byte, 1))
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("half a second after the rejection the connection gave %v; "+
			"want it still open, the read timing out", err)
	}
}

// TestRefusedConnectionIsClosedWhenARTIMExpires: a refused caller that never
// closes does not hold the connection for good. The SCP closes it when ARTIM
// expires (PS3.8 9.2, AA-2).
func TestRefusedConnectionIsClosedWhenARTIMExpires(t *testing.T) {
	conn := refusedConnection(t, 300*time.Millisecond)

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Read(make([]byte, 1))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("after ARTIM the connection gave %v; want it closed by the SCP", err)
	}
}
