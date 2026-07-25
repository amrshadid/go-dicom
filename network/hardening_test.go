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

// TestDecodeSCPSCURoleSelectionBounds verifies the role selection decoder
// rejects a UID length that runs past the end of the sub-item.
func TestDecodeSCPSCURoleSelectionBounds(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint16(data[0:2], 200)

	if _, err := DecodeSCPSCURoleSelection(data); err == nil {
		t.Fatal("expected an error for an out-of-bounds UID length, got nil")
	}
}

// TestDecodeUserIdentityNegotiationBounds verifies the user identity decoder
// rejects a primary field length that runs past the end of the sub-item.
func TestDecodeUserIdentityNegotiationBounds(t *testing.T) {
	data := make([]byte, 8)
	data[0] = byte(UserIdentityUsername)
	data[1] = 0x01
	binary.BigEndian.PutUint16(data[2:4], 9999)

	if _, err := DecodeUserIdentityNegotiation(data); err == nil {
		t.Fatal("expected an error for an out-of-bounds primary field length, got nil")
	}
}

// TestDecodeSOPClassExtendedNegotiationRoundTrip covers the decoder added to
// complete the extended negotiation codec.
func TestDecodeSOPClassExtendedNegotiationRoundTrip(t *testing.T) {
	original := &SOPClassExtendedNegotiation{
		SOPClassUID: StudyRootQueryRetrieveFind,
		ServiceData: []byte{0x01, 0x01},
	}

	encoded := original.Encode()
	decoded, err := DecodeSOPClassExtendedNegotiation(encoded[4:])
	if err != nil {
		t.Fatalf("DecodeSOPClassExtendedNegotiation: %v", err)
	}
	if decoded.SOPClassUID != original.SOPClassUID {
		t.Errorf("SOPClassUID = %q, want %q", decoded.SOPClassUID, original.SOPClassUID)
	}
	if !bytes.Equal(decoded.ServiceData, original.ServiceData) {
		t.Errorf("ServiceData = % x, want % x", decoded.ServiceData, original.ServiceData)
	}
}

// TestUserInformationExtendedNegotiationRoundTrip verifies that extended
// negotiation sub-items survive a full encode/decode of the User Information
// item. Before v1.2.0 these were encoded nowhere and parsed nowhere.
func TestUserInformationExtendedNegotiationRoundTrip(t *testing.T) {
	ui := UserInformationItem{
		MaxPDULength:           16384,
		ImplementationClassUID: DefaultImplementationClassUID,
		ImplementationVersion:  DefaultImplementationVersionName,
		AsyncOperations: &AsynchronousOperationsWindow{
			MaxOperationsInvoked:   4,
			MaxOperationsPerformed: 8,
		},
		RoleSelections: []SCPSCURoleSelection{
			{SOPClassUID: CTImageStorageUID, SCURole: true, SCPRole: true},
		},
		UserIdentity: &UserIdentityNegotiation{
			Type:                      UserIdentityUsernamePassword,
			PositiveResponseRequested: true,
			PrimaryField:              []byte("operator"),
			SecondaryField:            []byte("secret"),
		},
	}

	var buf bytes.Buffer
	if err := ui.encode(&buf); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := decodeUserInformation(buf.Bytes()[4:])
	if err != nil {
		t.Fatalf("decodeUserInformation: %v", err)
	}

	if decoded.MaxPDULength != ui.MaxPDULength {
		t.Errorf("MaxPDULength = %d, want %d", decoded.MaxPDULength, ui.MaxPDULength)
	}
	if decoded.AsyncOperations == nil {
		t.Fatal("AsyncOperations was dropped in the round trip")
	}
	if decoded.AsyncOperations.MaxOperationsInvoked != 4 ||
		decoded.AsyncOperations.MaxOperationsPerformed != 8 {
		t.Errorf("AsyncOperations = %+v, want {4 8}", *decoded.AsyncOperations)
	}
	if len(decoded.RoleSelections) != 1 {
		t.Fatalf("got %d role selections, want 1", len(decoded.RoleSelections))
	}
	if got := decoded.RoleSelections[0]; got.SOPClassUID != CTImageStorageUID ||
		!got.SCURole || !got.SCPRole {
		t.Errorf("RoleSelections[0] = %+v, want {%s true true}", got, CTImageStorageUID)
	}
	if decoded.UserIdentity == nil {
		t.Fatal("UserIdentity was dropped in the round trip")
	}
	if string(decoded.UserIdentity.PrimaryField) != "operator" {
		t.Errorf("PrimaryField = %q, want %q", decoded.UserIdentity.PrimaryField, "operator")
	}
	if string(decoded.UserIdentity.SecondaryField) != "secret" {
		t.Errorf("SecondaryField = %q, want %q", decoded.UserIdentity.SecondaryField, "secret")
	}
}
