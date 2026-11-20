package network

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// An acceptor that has accepted a connection and is waiting for the
// A-ASSOCIATE-RQ answers a PDU that is not one with an A-ABORT (PS3.8 Table
// 9-10, Sta2: AA-1, for Evt3, Evt4, Evt10, Evt12, Evt13 and Evt19). These send
// raw bytes, since a PDU that is invalid cannot be built by the encoder.

// rawPDU frames a body as a PDU of the given type.
func rawPDU(pduType byte, body []byte) []byte {
	out := []byte{pduType, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[2:], uint32(len(body)))
	return append(out, body...)
}

// associateRQBody is the body of a valid A-ASSOCIATE-RQ, to cut or break.
func associateRQBody() []byte {
	rq := &AssociateRQ{
		ProtocolVersion: ProtocolVersion, CalledAE: "ABORT_SCP", CallingAE: "PROBE",
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts:  DefaultVerificationContexts(),
		UserInformation:       UserInformationItem{MaxPDULength: DefaultMaxPDUSize},
	}
	data, err := rq.Encode()
	if err != nil {
		panic(err) // a request built here always encodes
	}
	return data[6:]
}

// firstPDU sends data on a new connection to addr and returns the PDU the
// server answers with, or nil with how the connection ended instead.
func firstPDU(t *testing.T, addr string, data []byte, halfClose bool) (PDU, error) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(data); err != nil {
		t.Fatal(err)
	}
	if halfClose {
		_ = conn.(*net.TCPConn).CloseWrite()
	}
	transport := NewTransport(conn, DefaultMaxPDUSize)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pdu, err := transport.ReadPDU(ctx)
	if err != nil {
		return nil, err
	}
	// The peer closes after the A-ABORT, and the connection is then read to
	// its end: a reset here would be the abort arriving as a broken connection.
	if _, isAbort := pdu.(*AbortPDU); isAbort {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.Copy(io.Discard, conn); err != nil {
			t.Errorf("after the A-ABORT the connection ended with %v, not a clean close", err)
		}
	}
	return pdu, nil
}

// TestFirstPDUThatIsNotAnAssociateRequestIsAborted: each of these, sent to an
// SCP waiting for its first PDU, is answered with an A-ABORT from the
// service-provider (AA-1) carrying the reason the PDU earns: an unrecognized
// type, 1; a type that is known and not due, 2; a PDU of a known type whose
// body or length cannot be accepted, 6. Before, the unrecognized and the
// invalid were met by a connection closed in silence.
func TestFirstPDUThatIsNotAnAssociateRequestIsAborted(t *testing.T) {
	valid := associateRQBody()
	badItem := append(append([]byte(nil), valid[:68]...), 0x10, 0, 0xFF, 0xFF, 'a', 'b', 'c')
	oversize := []byte{PDUTypeAssociateRQ, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(oversize[2:], 100<<20)

	cases := []struct {
		name   string
		data   []byte
		reason byte
	}{
		{"an unrecognized PDU type", rawPDU(0x42, []byte{0, 0, 0, 0}), abortUnrecognizedPDU},
		{"type 0x00", rawPDU(0x00, nil), abortUnrecognizedPDU},
		{"type 0x08", rawPDU(0x08, nil), abortUnrecognizedPDU},
		{"an A-ASSOCIATE-RQ whose items are cut short", rawPDU(PDUTypeAssociateRQ, valid[:70]), abortInvalidPDUParameter},
		{"an A-ASSOCIATE-RQ with an item longer than the PDU", rawPDU(PDUTypeAssociateRQ, badItem), abortInvalidPDUParameter},
		{"an A-ASSOCIATE-RQ shorter than its fixed fields", rawPDU(PDUTypeAssociateRQ, []byte{0, 1, 0, 0, 'a'}), abortInvalidPDUParameter},
		{"an A-ASSOCIATE-RQ that states a length of 100 MiB", oversize, abortInvalidPDUParameter},
		{"a P-DATA-TF", rawPDU(PDUTypeDataTF, []byte{0, 0, 0, 2, 1, 3}), abortUnexpectedPDU},
		{"an A-ASSOCIATE-AC", rawPDU(PDUTypeAssociateAC, valid), abortUnexpectedPDU},
		{"an A-ASSOCIATE-RJ", rawPDU(PDUTypeAssociateRJ, []byte{0, 1, 1, 1}), abortUnexpectedPDU},
		{"an A-RELEASE-RQ", rawPDU(PDUTypeReleaseRQ, []byte{0, 0, 0, 0}), abortUnexpectedPDU},
		{"an A-RELEASE-RP", rawPDU(PDUTypeReleaseRP, []byte{0, 0, 0, 0}), abortUnexpectedPDU},
	}

	for _, limited := range []bool{false, true} {
		name := "waiting"
		if limited {
			name = "at the association limit"
		}
		t.Run(name, func(t *testing.T) {
			cfg := SCPConfig{AETitle: "ABORT_SCP", Port: 0, BindAddress: "127.0.0.1"}
			if limited {
				cfg.MaxAssociations = 1
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			server, err := StartServer(ctx, cfg, &EchoHandler{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.Stop)
			if limited {
				// One association holds the only slot, so each connection below is
				// one turned away at the limit, and answered as any other would be.
				scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "ABORT_SCP", Address: server.Addr()})
				if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = scu.Association().transport.Close() })
			}
			for _, c := range cases {
				pdu, err := firstPDU(t, server.Addr(), c.data, false)
				abort, ok := pdu.(*AbortPDU)
				if !ok {
					t.Errorf("%s: answered with %v (%v), want an A-ABORT", c.name, pdu, err)
					continue
				}
				if abort.Source != AbortSourceServiceProvider || abort.Reason != c.reason {
					t.Errorf("%s: A-ABORT source %d reason %d, want source %d reason %d", c.name,
						abort.Source, abort.Reason, AbortSourceServiceProvider, c.reason)
				}
			}
		})
	}
}

// TestFirstPDUThatIsAnAbortIsNotAnswered: an A-ABORT in Sta2 closes the
// connection (AA-2). It was answered with an A-ABORT of the server's own.
func TestFirstPDUThatIsAnAbortIsNotAnswered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := StartServer(ctx, SCPConfig{AETitle: "ABORT_SCP", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	pdu, err := firstPDU(t, server.Addr(), rawPDU(PDUTypeAbort, []byte{0, 0, 0, 0}), false)
	if pdu != nil || !errors.Is(err, io.EOF) {
		t.Errorf("an A-ABORT was answered with %v, %v; want the connection closed", pdu, err)
	}
}

// TestAssociateRequestCutShortIsNotAnswered: a PDU whose bytes stop short of
// the length it states is not yet an invalid PDU; with the connection closed
// behind it there is no one to answer (AA-5), so the SCP closes and goes on.
// A request that follows on a new connection is served.
func TestAssociateRequestCutShortIsNotAnswered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := StartServer(ctx, SCPConfig{AETitle: "ABORT_SCP", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	cut := rawPDU(PDUTypeAssociateRQ, associateRQBody())[:20]
	pdu, err := firstPDU(t, server.Addr(), cut, true)
	if pdu != nil || err == nil || strings.Contains(err.Error(), "A-ABORT") {
		t.Errorf("a cut-short request was answered with %v, %v", pdu, err)
	}
	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "ABORT_SCP", Address: server.Addr()})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("the SCP did not go on to serve: %v", err)
	}
	if err := scu.Echo(ctx); err != nil {
		t.Error(err)
	}
	_ = scu.Release(ctx)
}
