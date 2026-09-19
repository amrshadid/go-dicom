package network

import "testing"

// TestStoreContextFor covers how Store picks among several accepted contexts for
// one SOP class. It took the first one a map iteration returned, which is
// random: a JPEG instance could go out on the Explicit VR context and be
// decoded, or an uncompressed one on the JPEG context and fail for want of an
// encoder, from one run to the next. storescu proposes a context per class and
// syntax (#116), so a class commonly has more than one.
func TestStoreContextFor(t *testing.T) {
	const ct = CTImageStorageUID
	accepted := map[byte]*PresentationContext{
		1: {ID: 1, AbstractSyntax: ct, TransferSyntax: JPEGLosslessSV1UID},
		3: {ID: 3, AbstractSyntax: ct, TransferSyntax: ImplicitVRLittleEndianUID},
		5: {ID: 5, AbstractSyntax: ct, TransferSyntax: ExplicitVRLittleEndianUID},
		7: {ID: 7, AbstractSyntax: MRImageStorageUID, TransferSyntax: ExplicitVRLittleEndianUID},
		9: {ID: 9, AbstractSyntax: ct, TransferSyntax: RLELosslessUID},
	}
	tests := []struct {
		name, class, source string
		want                byte
	}{
		{"the data's own syntax, so nothing is transcoded", ct, JPEGLosslessSV1UID, 1},
		{"its own syntax, uncompressed", ct, ImplicitVRLittleEndianUID, 3},
		{"no match: Explicit VR, which anything can be sent as", ct, ExplicitVRBigEndianUID, 5},
		{"a data set with no syntax of its own", ct, "", 5},
		{"compressed with no matching context: decoded to Explicit VR", ct, JPEG2000LosslessUID, 5},
		{"the class decides first", MRImageStorageUID, JPEGLosslessSV1UID, 7},
	}
	for _, tc := range tests {
		for run := 0; run < 20; run++ { // map order must not matter
			got, ok := storeContextFor(accepted, tc.class, tc.source)
			if !ok || got != tc.want {
				t.Fatalf("%s: context %d (%v), want %d", tc.name, got, ok, tc.want)
			}
		}
	}

	if _, ok := storeContextFor(accepted, "1.2.840.10008.5.1.4.1.1.6.1", ""); ok {
		t.Error("a class with no accepted context was given one")
	}

	// Only compressed contexts for the class, none matching: the lowest ID, so
	// the choice is at least the same every time.
	onlyCompressed := map[byte]*PresentationContext{
		9: {ID: 9, AbstractSyntax: ct, TransferSyntax: RLELosslessUID},
		3: {ID: 3, AbstractSyntax: ct, TransferSyntax: JPEGLSLosslessUID},
	}
	for run := 0; run < 20; run++ {
		if got, _ := storeContextFor(onlyCompressed, ct, ExplicitVRLittleEndianUID); got != 3 {
			t.Fatalf("got context %d, want the lowest ID, 3", got)
		}
	}
}
