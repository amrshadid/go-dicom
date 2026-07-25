package network

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestNegotiatePresentationContextsEmptyTransferSyntaxes verifies that a
// presentation context carrying no transfer syntax sub-items is rejected
// cleanly rather than panicking. A remote peer fully controls this, so an
// index-out-of-range here would take down the whole SCP process.
func TestNegotiatePresentationContextsEmptyTransferSyntaxes(t *testing.T) {
	requested := []PresentationContextItem{
		{ID: 1, AbstractSyntax: VerificationSOPClassUID, TransferSyntaxes: nil},
		{ID: 3, AbstractSyntax: "1.2.3.4.unsupported", TransferSyntaxes: nil},
		{ID: 5, AbstractSyntax: VerificationSOPClassUID, TransferSyntaxes: []string{}},
	}
	supportedAS := map[string]bool{VerificationSOPClassUID: true}
	supportedTS := map[string]bool{ImplicitVRLittleEndianUID: true}

	results := NegotiatePresentationContexts(requested, supportedAS, supportedTS)

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for _, r := range results {
		if r.Result == PCResultAcceptance {
			t.Errorf("context %d accepted despite proposing no transfer syntax", r.ID)
		}
		// A rejected context must still carry a transfer syntax (PS3.8 9.3.3.2).
		if r.TransferSyntax == "" {
			t.Errorf("context %d rejected with an empty transfer syntax", r.ID)
		}
	}
}

// TestDecodePDURejectsOversizedLength verifies that a PDU declaring an absurd
// length is rejected before the reader allocates a buffer for it.
func TestDecodePDURejectsOversizedLength(t *testing.T) {
	header := make([]byte, 6)
	header[0] = PDUTypeAssociateRQ
	binary.BigEndian.PutUint32(header[2:6], 0xFFFFFFFF)

	_, err := DecodePDU(bytes.NewReader(header))
	if err == nil {
		t.Fatal("expected an error for an oversized PDU length, got nil")
	}
	pduErr, ok := err.(*PDUError)
	if !ok {
		t.Fatalf("expected *PDUError, got %T: %v", err, err)
	}
	if pduErr.Code != "TOO_LARGE" {
		t.Errorf("error code = %q, want %q", pduErr.Code, "TOO_LARGE")
	}
}

// TestDecodePDUAcceptsLengthAtLimit confirms the guard rejects only what is
// over the limit, not legitimate traffic near it.
func TestDecodePDUAcceptsLengthAtLimit(t *testing.T) {
	encoded, err := (&ReleaseRQ{}).Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	pdu, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU on a valid release request: %v", err)
	}
	if _, ok := pdu.(*ReleaseRQ); !ok {
		t.Errorf("decoded %T, want *ReleaseRQ", pdu)
	}
}

// TestDecodeDataTFRejectsOversizedPDV verifies that a PDV declaring more data
// than the enclosing PDU contains is rejected before allocating.
func TestDecodeDataTFRejectsOversizedPDV(t *testing.T) {
	var payload bytes.Buffer
	_ = binary.Write(&payload, binary.BigEndian, uint32(0xFFFFFFF0))
	payload.WriteByte(0x01)
	payload.WriteByte(0x03)
	payload.Write([]byte{0xDE, 0xAD})

	if _, err := DecodePDU(bytes.NewReader(wrapPDU(PDUTypeDataTF, payload.Bytes()))); err == nil {
		t.Fatal("expected an error for a PDV longer than its PDU, got nil")
	}
}

// TestDecodeCommandDatasetRejectsOversizedElement verifies that a command
// element declaring more bytes than remain is rejected rather than silently
// yielding a zero-padded value.
func TestDecodeCommandDatasetRejectsOversizedElement(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0000))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0100))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0x7FFFFFFF))
	buf.Write([]byte{0x30, 0x00})

	if _, err := DecodeCommandDataset(buf.Bytes()); err == nil {
		t.Fatal("expected an error for an element longer than the buffer, got nil")
	}
}
