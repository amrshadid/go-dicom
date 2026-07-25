package network

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	tagSOPClassUID    = tag.New(0x0008, 0x0016)
	tagSOPInstanceUID = tag.New(0x0008, 0x0018)
	tagPatientName    = tag.New(0x0010, 0x0010)
	tagPatientID      = tag.New(0x0010, 0x0020)
	tagPixelData      = tag.New(0x7FE0, 0x0010)
)

func buildCodecTestDataset() *dataset.Dataset {
	ds := dataset.NewDataset()
	// 25 characters — deliberately odd, to exercise even-length padding.
	_ = ds.Add(dataelem.NewDataElement(tagSOPClassUID, dataelem.UI, []byte(CTImageStorageUID)))
	_ = ds.Add(dataelem.NewDataElement(tagSOPInstanceUID, dataelem.UI, []byte("1.2.3.4.5.6.7.8.1")))
	_ = ds.Add(dataelem.NewDataElement(tagPatientName, dataelem.PN, []byte("Smith^John")))
	_ = ds.Add(dataelem.NewDataElement(tagPatientID, dataelem.LO, []byte("CT-001")))
	// OB uses the long explicit header form (2-byte reserved + 4-byte length).
	_ = ds.Add(dataelem.NewDataElement(tagPixelData, dataelem.OB, []byte{0x01, 0x02, 0x03, 0x04}))
	return ds
}

// TestDatasetCodecRoundTripAllTransferSyntaxes verifies that a data set encoded
// with a given transfer syntax decodes back to the same values under that same
// syntax. Before v1.2.0 encoding was hardcoded to implicit VR little endian
// regardless of what was negotiated, so any peer that accepted explicit VR —
// the syntax proposed first by default — received an unparsable data set.
func TestDatasetCodecRoundTripAllTransferSyntaxes(t *testing.T) {
	syntaxes := []struct {
		name string
		uid  string
	}{
		{"ImplicitVRLittleEndian", ImplicitVRLittleEndianUID},
		{"ExplicitVRLittleEndian", ExplicitVRLittleEndianUID},
		{"ExplicitVRBigEndian", ExplicitVRBigEndianUID},
		{"DeflatedExplicitVRLittleEndian", DeflatedExplicitVRLittleEndianUID},
		{"JPEGBaseline", JPEGBaselineUID},
		{"RLELossless", RLELosslessUID},
	}

	want := map[tag.Tag]string{
		tagSOPClassUID:    CTImageStorageUID,
		tagSOPInstanceUID: "1.2.3.4.5.6.7.8.1",
		tagPatientName:    "Smith^John",
		tagPatientID:      "CT-001",
	}

	for _, ts := range syntaxes {
		t.Run(ts.name, func(t *testing.T) {
			encoded, err := EncodeDataset(buildCodecTestDataset(), ts.uid)
			if err != nil {
				t.Fatalf("EncodeDataset: %v", err)
			}

			decoded, err := DecodeDataset(encoded, ts.uid)
			if err != nil {
				t.Fatalf("DecodeDataset: %v", err)
			}

			for tg, expect := range want {
				elem, ok := decoded.Get(tg)
				if !ok {
					t.Fatalf("tag %s missing after round trip", tg)
				}
				got := trimPadding(elem.GetValue().([]byte))
				if got != expect {
					t.Errorf("tag %s = %q, want %q", tg, got, expect)
				}
			}

			pixels, ok := decoded.Get(tagPixelData)
			if !ok {
				t.Fatal("pixel data missing after round trip")
			}
			if !bytes.Equal(pixels.GetValue().([]byte), []byte{0x01, 0x02, 0x03, 0x04}) {
				t.Errorf("pixel data = % x, want 01 02 03 04", pixels.GetValue())
			}
		})
	}
}

// TestEncodeDatasetPadsToEvenLength verifies DICOM PS3.5 Section 7.1.1: every
// value must have an even length, padded with the VR's designated character.
func TestEncodeDatasetPadsToEvenLength(t *testing.T) {
	tests := []struct {
		name    string
		vr      dataelem.VR
		value   string
		wantPad byte
	}{
		{"UI pads with NUL", dataelem.UI, "1.2.840.10008.5.1.4.1.1.2", 0x00},
		{"PN pads with space", dataelem.PN, "Smith^Jon", ' '},
		{"LO pads with space", dataelem.LO, "ODD", ' '},
		{"CS pads with space", dataelem.CS, "CTX", ' '},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := dataset.NewDataset()
			_ = ds.Add(dataelem.NewDataElement(tagPatientID, tc.vr, []byte(tc.value)))

			encoded, err := EncodeDataset(ds, ExplicitVRLittleEndianUID)
			if err != nil {
				t.Fatalf("EncodeDataset: %v", err)
			}
			if len(encoded)%2 != 0 {
				t.Errorf("encoded stream length %d is odd", len(encoded))
			}

			decoded, err := DecodeDataset(encoded, ExplicitVRLittleEndianUID)
			if err != nil {
				t.Fatalf("DecodeDataset: %v", err)
			}
			elem, _ := decoded.Get(tagPatientID)
			got := elem.GetValue().([]byte)

			if len(got)%2 != 0 {
				t.Errorf("value length %d is odd, want even", len(got))
			}
			if len(got) != len(tc.value)+1 {
				t.Fatalf("value length = %d, want %d", len(got), len(tc.value)+1)
			}
			if got[len(got)-1] != tc.wantPad {
				t.Errorf("pad byte = %#02x, want %#02x", got[len(got)-1], tc.wantPad)
			}
		})
	}
}

// TestEncodeDatasetDoesNotMutateSource verifies that padding an odd-length
// value during encoding does not write back into the caller's data set.
func TestEncodeDatasetDoesNotMutateSource(t *testing.T) {
	original := []byte("1.2.3.4.5.6.7.8.1") // 17 bytes, odd
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tagSOPInstanceUID, dataelem.UI, original))

	if _, err := EncodeDataset(ds, ExplicitVRLittleEndianUID); err != nil {
		t.Fatalf("EncodeDataset: %v", err)
	}

	elem, _ := ds.Get(tagSOPInstanceUID)
	if got := elem.GetValue().([]byte); len(got) != 17 {
		t.Errorf("source value length changed to %d, want 17", len(got))
	}
}

// TestDecodeDatasetRejectsOversizedElement verifies that a data set element
// declaring more bytes than remain is rejected rather than truncated silently.
func TestDecodeDatasetRejectsOversizedElement(t *testing.T) {
	// Explicit VR LE: tag (0010,0020), VR "LO", length 0xFFFF, no value bytes.
	data := []byte{
		0x10, 0x00, 0x20, 0x00,
		'L', 'O',
		0xFF, 0xFF,
	}

	if _, err := DecodeDataset(data, ExplicitVRLittleEndianUID); err == nil {
		t.Fatal("expected an error for an element longer than the buffer, got nil")
	}
}

// TestEncodingForTransferSyntax pins the mapping from transfer syntax UID to
// wire encoding. Compressed syntaxes carry an explicit VR little endian data
// set; only the pixel data itself is compressed.
func TestEncodingForTransferSyntax(t *testing.T) {
	tests := []struct {
		uid            string
		wantExplicitVR bool
		wantBigEndian  bool
		wantDeflated   bool
	}{
		{ImplicitVRLittleEndianUID, false, false, false},
		{ExplicitVRLittleEndianUID, true, false, false},
		{ExplicitVRBigEndianUID, true, true, false},
		{DeflatedExplicitVRLittleEndianUID, true, false, true},
		{JPEG2000LosslessUID, true, false, false},
		{RLELosslessUID, true, false, false},
		{"", false, false, false}, // unknown: DICOM's implicit VR LE default
	}

	for _, tc := range tests {
		got := encodingForTransferSyntax(tc.uid)
		if got.ExplicitVR != tc.wantExplicitVR || got.BigEndian != tc.wantBigEndian ||
			got.Deflated != tc.wantDeflated {
			t.Errorf("encodingForTransferSyntax(%q) = %+v, want {ExplicitVR:%v BigEndian:%v Deflated:%v}",
				tc.uid, got, tc.wantExplicitVR, tc.wantBigEndian, tc.wantDeflated)
		}
	}
}

// TestTransferSyntaxForContext verifies that the association reports the
// syntax negotiated per presentation context, which is what the DIMSE layer
// uses to pick an encoding.
func TestTransferSyntaxForContext(t *testing.T) {
	assoc := NewAssociation(nil)
	assoc.acceptedContexts = map[byte]*PresentationContext{
		1: {ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntax: ExplicitVRLittleEndianUID},
		3: {ID: 3, AbstractSyntax: MRImageStorageUID, TransferSyntax: ImplicitVRLittleEndianUID},
	}

	if got := assoc.TransferSyntaxFor(1); got != ExplicitVRLittleEndianUID {
		t.Errorf("TransferSyntaxFor(1) = %q, want %q", got, ExplicitVRLittleEndianUID)
	}
	if got := assoc.TransferSyntaxFor(3); got != ImplicitVRLittleEndianUID {
		t.Errorf("TransferSyntaxFor(3) = %q, want %q", got, ImplicitVRLittleEndianUID)
	}
	// An unknown context ID yields the empty string, which callers treat as
	// DICOM's implicit VR little endian default.
	if got := assoc.TransferSyntaxFor(99); got != "" {
		t.Errorf("TransferSyntaxFor(99) = %q, want %q", got, "")
	}
}

// trimPadding removes trailing NUL and space padding from a decoded value.
func trimPadding(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
