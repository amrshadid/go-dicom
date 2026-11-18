package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"runtime"
	"testing"
	"time"
)

// countingReader counts what is read from it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// pduHeader is the six bytes that open a PDU of the given type and length.
func pduHeader(pduType byte, length uint32) []byte {
	h := []byte{pduType, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(h[2:], length)
	return h
}

// allocatedBy returns the bytes fn allocated, whether or not they were freed.
func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestPDUBodyIsAllocatedAsItArrives: the length in a PDU's header is the
// peer's word, and the body was allocated from it before a byte of the body
// had been read: six bytes cost 128 MiB. A body is now allocated as its bytes
// arrive, so a peer pays for memory with bytes sent. One that declares
// 128 MiB and sends 1 MiB costs about what it sent.
func TestPDUBodyIsAllocatedAsItArrives(t *testing.T) {
	sent := 1 << 20
	stream := append(pduHeader(PDUTypeDataTF, MaxPDULengthLimit), make([]byte, sent)...)
	var err error
	allocated := allocatedBy(func() { _, err = decodePDU(bytes.NewReader(stream), 0) })
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a PDU cut short partway through its body: %v, want io.ErrUnexpectedEOF", err)
	}
	t.Logf("declared %d MiB, sent 1 MiB: %.1f MiB allocated, %v", MaxPDULengthLimit>>20, float64(allocated)/(1<<20), err)
	if allocated > 8<<20 {
		t.Errorf("%.1f MiB allocated for a PDU of which 1 MiB arrived", float64(allocated)/(1<<20))
	}
}

// TestPDULengthIsBoundedByItsType: only a P-DATA-TF can be long. An
// A-ASSOCIATE-RQ or -AC holds at most 128 presentation contexts and the user
// information, and the other PDUs are four bytes (PS3.8 9.3.4 to 9.3.8). Each
// was read up to MaxPDULengthLimit, 128 MiB, on the word of its header, before
// the association existed and before anything had been checked. A length no
// PDU of its type can have is refused from the header, and the body not read.
func TestPDULengthIsBoundedByItsType(t *testing.T) {
	for _, c := range []struct {
		name    string
		pduType byte
		length  uint32
		refused bool
	}{
		{"A-ASSOCIATE-RQ of 128 MiB", PDUTypeAssociateRQ, MaxPDULengthLimit, true},
		{"A-ASSOCIATE-RQ one byte over", PDUTypeAssociateRQ, MaxAssociatePDULength + 1, true},
		{"A-ASSOCIATE-RQ at the bound", PDUTypeAssociateRQ, MaxAssociatePDULength, false},
		{"A-ASSOCIATE-AC of 128 MiB", PDUTypeAssociateAC, MaxPDULengthLimit, true},
		{"A-ASSOCIATE-RJ of 5 bytes", PDUTypeAssociateRJ, 5, true},
		{"A-RELEASE-RQ of 1 MiB", PDUTypeReleaseRQ, 1 << 20, true},
		{"A-RELEASE-RP of 1 MiB", PDUTypeReleaseRP, 1 << 20, true},
		{"A-ABORT of 1 MiB", PDUTypeAbort, 1 << 20, true},
		{"A-ABORT of 4 bytes", PDUTypeAbort, 4, false},
		{"an unknown type of 1 MiB", 0x09, 1 << 20, true},
	} {
		body := make([]byte, min(c.length, 2<<20))
		r := &countingReader{r: bytes.NewReader(append(pduHeader(c.pduType, c.length), body...))}
		_, err := decodePDU(r, 0)
		var pduErr *PDUError
		tooLong := errors.As(err, &pduErr) && (pduErr.Code == "TOO_LARGE" || pduErr.Code == "UNKNOWN_TYPE")
		if c.refused && (!tooLong || r.n != 6) {
			t.Errorf("%s: %v after reading %d bytes; want it refused from its 6-byte header", c.name, err, r.n)
		}
		if !c.refused && (tooLong || r.n != 6+int(c.length)) {
			t.Errorf("%s: %v after reading %d bytes; want the body read", c.name, err, r.n)
		}
	}
}

// TestLargePDataIsReadWhole: a body read in pieces is the body that was
// sent, at every size around a piece's edge.
func TestLargePDataIsReadWhole(t *testing.T) {
	for _, size := range []int{0, 1, pduBodyStep - 7, pduBodyStep - 6, pduBodyStep - 5, pduBodyStep + 1, 3*pduBodyStep + 12345, 5 * pduBodyStep} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i * 31)
		}
		wire, err := EncodePDU(&PDataTF{PDVItems: []PDVItem{{PresentationContextID: 1, IsLast: true, Data: data}}})
		if err != nil {
			t.Fatal(err)
		}
		pdu, err := decodePDU(bytes.NewReader(wire), 0)
		if err != nil {
			t.Fatalf("%d bytes: %v", size, err)
		}
		got := pdu.(*PDataTF).PDVItems[0].Data
		if !bytes.Equal(got, data) {
			t.Errorf("%d bytes sent, %d read back, equal: false", size, len(got))
		}
	}
}

// TestLargestAssociateRQFitsItsBound: the bound on an A-ASSOCIATE-RQ leaves
// room for the largest one a peer has reason to send: all 128 presentation
// contexts, each proposing 40 transfer syntaxes.
func TestLargestAssociateRQFitsItsBound(t *testing.T) {
	rq := &AssociateRQ{ProtocolVersion: 1, CalledAE: "ARCHIVE", CallingAE: "SCANNER",
		ApplicationContextUID: DefaultApplicationContextUID}
	var syntaxes []string
	for i := 0; i < 40; i++ {
		syntaxes = append(syntaxes, "1.2.840.10008.1.2.4.100000000000000000000000000000000000000000."+string(rune('0'+i%10))+string(rune('0'+i/10)))
	}
	for id := 1; id <= 255; id += 2 {
		rq.PresentationContexts = append(rq.PresentationContexts, PresentationContextItem{
			ID: byte(id), AbstractSyntax: CTImageStorageUID, TransferSyntaxes: syntaxes})
	}
	rq.UserInformation.MaxPDULength = 16384
	rq.UserInformation.ImplementationClassUID = "1.2.3"
	wire, err := EncodePDU(rq)
	if err != nil {
		t.Fatal(err)
	}
	size := len(wire)
	pdu, err := decodePDU(bytes.NewReader(wire), 0)
	if err != nil {
		t.Fatalf("an A-ASSOCIATE-RQ of %d bytes: %v", size, err)
	}
	if got := len(pdu.(*AssociateRQ).PresentationContexts); got != 128 {
		t.Errorf("%d presentation contexts read back, want 128", got)
	}
	t.Logf("128 contexts of 40 transfer syntaxes: %d bytes, bound %d", size, MaxAssociatePDULength)
}

// TestSCPHoldsNoMemoryForADeclaredLength: eight connections each open with
// the header of a 128 MiB A-ASSOCIATE-RQ and 1 MiB of body. The SCP held
// 128 MiB for each, 1 GiB in all, until ARTIM expired, having checked
// nothing about the caller. Each is now closed from its header, and a
// P-DATA-TF sent before any association the same way.
func TestSCPHoldsNoMemoryForADeclaredLength(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := StartServer(ctx, SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
		Network: NetworkConfig{ARTIMTimeout: 10 * time.Second}}, &BaseHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	for _, pduType := range []byte{PDUTypeAssociateRQ, PDUTypeDataTF} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		var conns []net.Conn
		for i := 0; i < 8; i++ {
			conn, err := net.Dial("tcp", server.Addr())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conns = append(conns, conn)
			_, _ = conn.Write(pduHeader(pduType, MaxPDULengthLimit))
			_, _ = conn.Write(make([]byte, 1<<20))
		}
		// Closed, or aborted and closed, well inside ARTIM: the header was
		// enough to refuse it.
		for i, conn := range conns {
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			if _, err := io.Copy(io.Discard, conn); err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					t.Fatalf("PDU type 0x%02X, connection %d: still open 3s after a 128 MiB header; ARTIM is 10s", pduType, i)
				}
			}
		}
		runtime.ReadMemStats(&after)
		grew := int64(after.HeapAlloc) - int64(before.HeapAlloc)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("PDU type 0x%02X: 8 connections declaring 128 MiB each: %d MiB allocated, heap grew %d MiB",
			pduType, allocated>>20, grew>>20)
		if allocated > 64<<20 {
			t.Errorf("PDU type 0x%02X: %d MiB allocated on the word of eight 6-byte headers", pduType, allocated>>20)
		}
	}
}
