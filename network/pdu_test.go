package network

import (
	"bytes"
	"testing"
)

func TestAssociateRQEncodeDecode(t *testing.T) {
	rq := &AssociateRQ{
		ProtocolVersion:       ProtocolVersion,
		CalledAE:              "CALLED_SCP",
		CallingAE:             "CALLING_SCU",
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts: []PresentationContextItem{
			{
				ID:               1,
				AbstractSyntax:   VerificationSOPClassUID,
				TransferSyntaxes: []string{ExplicitVRLittleEndianUID, ImplicitVRLittleEndianUID},
			},
		},
		UserInformation: UserInformationItem{
			MaxPDULength:           16384,
			ImplementationClassUID: DefaultImplementationClassUID,
			ImplementationVersion:  DefaultImplementationVersionName,
		},
	}

	encoded, err := rq.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if encoded[0] != PDUTypeAssociateRQ {
		t.Errorf("expected PDU type 0x%02X, got 0x%02X", PDUTypeAssociateRQ, encoded[0])
	}

	// Decode
	decoded, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}

	decodedRQ, ok := decoded.(*AssociateRQ)
	if !ok {
		t.Fatalf("expected *AssociateRQ, got %T", decoded)
	}

	if decodedRQ.CalledAE != "CALLED_SCP" {
		t.Errorf("expected CalledAE 'CALLED_SCP', got '%s'", decodedRQ.CalledAE)
	}
	if decodedRQ.CallingAE != "CALLING_SCU" {
		t.Errorf("expected CallingAE 'CALLING_SCU', got '%s'", decodedRQ.CallingAE)
	}
	if decodedRQ.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol version %d, got %d", ProtocolVersion, decodedRQ.ProtocolVersion)
	}
	if decodedRQ.ApplicationContextUID != DefaultApplicationContextUID {
		t.Errorf("expected app context UID '%s', got '%s'", DefaultApplicationContextUID, decodedRQ.ApplicationContextUID)
	}
	if len(decodedRQ.PresentationContexts) != 1 {
		t.Fatalf("expected 1 presentation context, got %d", len(decodedRQ.PresentationContexts))
	}
	pc := decodedRQ.PresentationContexts[0]
	if pc.ID != 1 {
		t.Errorf("expected PC ID 1, got %d", pc.ID)
	}
	if pc.AbstractSyntax != VerificationSOPClassUID {
		t.Errorf("expected abstract syntax '%s', got '%s'", VerificationSOPClassUID, pc.AbstractSyntax)
	}
	if len(pc.TransferSyntaxes) != 2 {
		t.Fatalf("expected 2 transfer syntaxes, got %d", len(pc.TransferSyntaxes))
	}
	if decodedRQ.UserInformation.MaxPDULength != 16384 {
		t.Errorf("expected MaxPDULength 16384, got %d", decodedRQ.UserInformation.MaxPDULength)
	}
}

func TestAssociateACEncodeDecode(t *testing.T) {
	ac := &AssociateAC{
		ProtocolVersion:       ProtocolVersion,
		CalledAE:              "CALLED",
		CallingAE:             "CALLING",
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts: []PresentationContextResultItem{
			{
				ID:             1,
				Result:         PCResultAcceptance,
				TransferSyntax: ExplicitVRLittleEndianUID,
			},
			{
				ID:             3,
				Result:         PCResultAbstractSyntaxNotSupported,
				TransferSyntax: ImplicitVRLittleEndianUID,
			},
		},
		UserInformation: UserInformationItem{
			MaxPDULength:           32768,
			ImplementationClassUID: DefaultImplementationClassUID,
		},
	}

	encoded, err := ac.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}

	decodedAC, ok := decoded.(*AssociateAC)
	if !ok {
		t.Fatalf("expected *AssociateAC, got %T", decoded)
	}

	if len(decodedAC.PresentationContexts) != 2 {
		t.Fatalf("expected 2 presentation context results, got %d", len(decodedAC.PresentationContexts))
	}
	if decodedAC.PresentationContexts[0].Result != PCResultAcceptance {
		t.Errorf("expected first PC accepted, got result %d", decodedAC.PresentationContexts[0].Result)
	}
	if decodedAC.PresentationContexts[1].Result != PCResultAbstractSyntaxNotSupported {
		t.Errorf("expected second PC rejected, got result %d", decodedAC.PresentationContexts[1].Result)
	}
	if decodedAC.UserInformation.MaxPDULength != 32768 {
		t.Errorf("expected MaxPDULength 32768, got %d", decodedAC.UserInformation.MaxPDULength)
	}
}

func TestAssociateRJEncodeDecode(t *testing.T) {
	rj := &AssociateRJ{
		Result: RJResultRejectedPermanent,
		Source: RJSourceServiceUser,
		Reason: 1,
	}

	encoded, err := rj.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}

	decodedRJ, ok := decoded.(*AssociateRJ)
	if !ok {
		t.Fatalf("expected *AssociateRJ, got %T", decoded)
	}

	if decodedRJ.Result != RJResultRejectedPermanent {
		t.Errorf("expected result %d, got %d", RJResultRejectedPermanent, decodedRJ.Result)
	}
	if decodedRJ.Source != RJSourceServiceUser {
		t.Errorf("expected source %d, got %d", RJSourceServiceUser, decodedRJ.Source)
	}
}

func TestPDataTFEncodeDecode(t *testing.T) {
	pdata := &PDataTF{
		PDVItems: []PDVItem{
			{
				PresentationContextID: 1,
				IsCommand:             true,
				IsLast:                true,
				Data:                  []byte{0x01, 0x02, 0x03, 0x04},
			},
		},
	}

	encoded, err := pdata.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}

	decodedPData, ok := decoded.(*PDataTF)
	if !ok {
		t.Fatalf("expected *PDataTF, got %T", decoded)
	}

	if len(decodedPData.PDVItems) != 1 {
		t.Fatalf("expected 1 PDV item, got %d", len(decodedPData.PDVItems))
	}

	pdv := decodedPData.PDVItems[0]
	if pdv.PresentationContextID != 1 {
		t.Errorf("expected context ID 1, got %d", pdv.PresentationContextID)
	}
	if !pdv.IsCommand {
		t.Error("expected IsCommand=true")
	}
	if !pdv.IsLast {
		t.Error("expected IsLast=true")
	}
	if !bytes.Equal(pdv.Data, []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Errorf("data mismatch: got %v", pdv.Data)
	}
}

func TestPDataTFMultiplePDVs(t *testing.T) {
	pdata := &PDataTF{
		PDVItems: []PDVItem{
			{PresentationContextID: 1, IsCommand: true, IsLast: false, Data: []byte{0xAA}},
			{PresentationContextID: 1, IsCommand: true, IsLast: true, Data: []byte{0xBB}},
		},
	}

	encoded, err := pdata.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}

	decodedPData := decoded.(*PDataTF)
	if len(decodedPData.PDVItems) != 2 {
		t.Fatalf("expected 2 PDV items, got %d", len(decodedPData.PDVItems))
	}
	if decodedPData.PDVItems[0].IsLast {
		t.Error("first PDV should not be last")
	}
	if !decodedPData.PDVItems[1].IsLast {
		t.Error("second PDV should be last")
	}
}

func TestReleaseEncodeDecode(t *testing.T) {
	// ReleaseRQ
	rq := &ReleaseRQ{}
	encoded, err := rq.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}
	if _, ok := decoded.(*ReleaseRQ); !ok {
		t.Fatalf("expected *ReleaseRQ, got %T", decoded)
	}

	// ReleaseRP
	rp := &ReleaseRP{}
	encoded, err = rp.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err = DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}
	if _, ok := decoded.(*ReleaseRP); !ok {
		t.Fatalf("expected *ReleaseRP, got %T", decoded)
	}
}

func TestAbortEncodeDecode(t *testing.T) {
	abort := &AbortPDU{
		Source: AbortSourceServiceProvider,
		Reason: 2,
	}

	encoded, err := abort.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}

	decodedAbort, ok := decoded.(*AbortPDU)
	if !ok {
		t.Fatalf("expected *AbortPDU, got %T", decoded)
	}

	if decodedAbort.Source != AbortSourceServiceProvider {
		t.Errorf("expected source %d, got %d", AbortSourceServiceProvider, decodedAbort.Source)
	}
	if decodedAbort.Reason != 2 {
		t.Errorf("expected reason 2, got %d", decodedAbort.Reason)
	}
}

func TestPadAETitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"AE", "AE              "},
		{"LONG_AE_TITLE!!", "LONG_AE_TITLE!! "},
		{"", "                "},
	}

	for _, tt := range tests {
		result := padAETitle(tt.input)
		if string(result) != tt.expected {
			t.Errorf("padAETitle(%q) = %q, want %q", tt.input, string(result), tt.expected)
		}
	}
}

func TestTrimAETitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"AE              ", "AE"},
		{"PACS", "PACS"},
		{"                ", ""},
	}

	for _, tt := range tests {
		result := trimAETitle([]byte(tt.input))
		if result != tt.expected {
			t.Errorf("trimAETitle(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestPDUTypeString(t *testing.T) {
	tests := []struct {
		pduType  byte
		expected string
	}{
		{PDUTypeAssociateRQ, "A-ASSOCIATE-RQ"},
		{PDUTypeAssociateAC, "A-ASSOCIATE-AC"},
		{PDUTypeAssociateRJ, "A-ASSOCIATE-RJ"},
		{PDUTypeDataTF, "P-DATA-TF"},
		{PDUTypeReleaseRQ, "A-RELEASE-RQ"},
		{PDUTypeReleaseRP, "A-RELEASE-RP"},
		{PDUTypeAbort, "A-ABORT"},
		{0xFF, "Unknown(0xFF)"},
	}

	for _, tt := range tests {
		result := PDUTypeString(tt.pduType)
		if result != tt.expected {
			t.Errorf("PDUTypeString(0x%02X) = %q, want %q", tt.pduType, result, tt.expected)
		}
	}
}
