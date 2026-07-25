package network

import (
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
