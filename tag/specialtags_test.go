package tag_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// TestSpecialTagValues pins the delimitation item tags to the literal values
// required by DICOM PS3.5 Table 7.5-1.
//
// These are asserted against hard-coded group and element numbers rather than
// against the package constants. A test that compares a constant to itself
// proves nothing: ItemTag was 0xFFFE0000 through v1.2.0 — missing a digit, so
// it decoded as (FFFE,0000) — and every test passed, because the same wrong
// constant was used to write the fixture and to read it back. Real files
// failed immediately.
func TestSpecialTagValues(t *testing.T) {
	tests := []struct {
		name             string
		got              tag.Tag
		wantGroup        uint16
		wantElement      uint16
		wantUint32       uint32
		wantStringFormat string
	}{
		{"ItemTag", tag.ItemTag, 0xFFFE, 0xE000, 0xFFFEE000, "(FFFE,E000)"},
		{"ItemDelimiterTag", tag.ItemDelimiterTag, 0xFFFE, 0xE00D, 0xFFFEE00D, "(FFFE,E00D)"},
		{"SequenceDelimiterTag", tag.SequenceDelimiterTag, 0xFFFE, 0xE0DD, 0xFFFEE0DD, "(FFFE,E0DD)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.got.Group(); got != tc.wantGroup {
				t.Errorf("Group() = %04X, want %04X", got, tc.wantGroup)
			}
			if got := tc.got.Element(); got != tc.wantElement {
				t.Errorf("Element() = %04X, want %04X", got, tc.wantElement)
			}
			if got := tc.got.Uint32(); got != tc.wantUint32 {
				t.Errorf("Uint32() = %#08X, want %#08X", got, tc.wantUint32)
			}
			if got := tc.got.String(); got != tc.wantStringFormat {
				t.Errorf("String() = %s, want %s", got, tc.wantStringFormat)
			}
		})
	}
}

// TestSpecialTagsRoundTripOnTheWire checks the tags survive the byte encoding
// used on disk and on the network, in both byte orders. This is the form a
// parser actually compares against.
func TestSpecialTagsRoundTripOnTheWire(t *testing.T) {
	// (FFFE,E000) little endian is FE FF 00 E0; big endian is FF FE E0 00.
	cases := []struct {
		name         string
		want         tag.Tag
		littleEndian []byte
		bigEndian    []byte
	}{
		{"ItemTag", tag.ItemTag,
			[]byte{0xFE, 0xFF, 0x00, 0xE0}, []byte{0xFF, 0xFE, 0xE0, 0x00}},
		{"ItemDelimiterTag", tag.ItemDelimiterTag,
			[]byte{0xFE, 0xFF, 0x0D, 0xE0}, []byte{0xFF, 0xFE, 0xE0, 0x0D}},
		{"SequenceDelimiterTag", tag.SequenceDelimiterTag,
			[]byte{0xFE, 0xFF, 0xDD, 0xE0}, []byte{0xFF, 0xFE, 0xE0, 0xDD}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tag.FromBytes(tc.littleEndian, true)
			if err != nil {
				t.Fatalf("FromBytes little endian: %v", err)
			}
			if got != tc.want {
				t.Errorf("little endian decoded %s, want %s", got, tc.want)
			}

			got, err = tag.FromBytes(tc.bigEndian, false)
			if err != nil {
				t.Fatalf("FromBytes big endian: %v", err)
			}
			if got != tc.want {
				t.Errorf("big endian decoded %s, want %s", got, tc.want)
			}
		})
	}
}

// TestIsSpecial confirms the delimitation tags are recognized as special and
// that ordinary tags are not.
func TestIsSpecial(t *testing.T) {
	special := []tag.Tag{tag.ItemTag, tag.ItemDelimiterTag, tag.SequenceDelimiterTag}
	for _, tg := range special {
		if !tg.IsSpecial() {
			t.Errorf("%s should be special", tg)
		}
	}

	ordinary := []tag.Tag{
		tag.New(0x0010, 0x0010), // PatientName
		tag.New(0x7FE0, 0x0010), // PixelData
		tag.New(0xFFFE, 0x0000), // the value ItemTag wrongly held before v1.2.1
	}
	for _, tg := range ordinary {
		if tg.IsSpecial() {
			t.Errorf("%s should not be special", tg)
		}
	}
}

// TestToBytesFromBytesAsymmetric guards the group/element ordering with a tag
// whose group and element differ.
//
// The prior FromBytes/ToBytes pair read and wrote all four bytes as a single
// uint32, transposing group and element. It went unnoticed because the only
// test case was PatientName (0010,0010) — where group equals element, so the
// transposition is invisible — and because the two functions were each other's
// inverse, so a round trip through them still succeeded.
func TestToBytesFromBytesAsymmetric(t *testing.T) {
	cases := []struct {
		name         string
		tg           tag.Tag
		littleEndian []byte
		bigEndian    []byte
	}{
		// group 0008, element 0018: SOP Instance UID
		{"SOPInstanceUID", tag.New(0x0008, 0x0018),
			[]byte{0x08, 0x00, 0x18, 0x00}, []byte{0x00, 0x08, 0x00, 0x18}},
		// group 7FE0, element 0010: Pixel Data
		{"PixelData", tag.New(0x7FE0, 0x0010),
			[]byte{0xE0, 0x7F, 0x10, 0x00}, []byte{0x7F, 0xE0, 0x00, 0x10}},
		{"ItemTag", tag.ItemTag,
			[]byte{0xFE, 0xFF, 0x00, 0xE0}, []byte{0xFF, 0xFE, 0xE0, 0x00}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tg.ToBytes(true); !bytes.Equal(got, tc.littleEndian) {
				t.Errorf("ToBytes(little) = % X, want % X", got, tc.littleEndian)
			}
			if got := tc.tg.ToBytes(false); !bytes.Equal(got, tc.bigEndian) {
				t.Errorf("ToBytes(big) = % X, want % X", got, tc.bigEndian)
			}

			got, err := tag.FromBytes(tc.littleEndian, true)
			if err != nil || got != tc.tg {
				t.Errorf("FromBytes(little) = %s (err %v), want %s", got, err, tc.tg)
			}
			got, err = tag.FromBytes(tc.bigEndian, false)
			if err != nil || got != tc.tg {
				t.Errorf("FromBytes(big) = %s (err %v), want %s", got, err, tc.tg)
			}
		})
	}
}
