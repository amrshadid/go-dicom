package compress_test

import (
	"os"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// JPEG Extended (1.2.840.10008.1.2.4.51) was listed as not decoding. Half of
// that was wrong.
//
// The transfer syntax allows 8-bit and 12-bit precision, and the standard
// library's decoder handles SOF1 frames — it rejects only the precision. So an
// 8-bit Extended frame has always decoded, and only 12-bit ones fail. Both are
// pinned here so the claim cannot drift back.

func readExtendedFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/jpegextended/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// TestJPEGExtended8BitDecodes checks an 8-bit Extended frame against dcmtk's
// decode of the same bytes.
//
// The comparison allows a difference of one. Two conforming JPEG decoders need
// not agree exactly: T.83 specifies the inverse DCT to a tolerance rather than
// bit-for-bit, and Go's implementation is not libjpeg's. Measured here it is 1
// on 0.7% of samples, which is that tolerance and not a defect. A real decoding
// error is not off by one — it is off by a block.
func TestJPEGExtended8BitDecodes(t *testing.T) {
	stream := readExtendedFixture(t, "rgb_extended_8bit.jpg")
	want := readExtendedFixture(t, "rgb_extended_expected.raw")

	got, err := compress.NewJPEGDecompressor().Decompress(stream)
	if err != nil {
		t.Fatalf("an 8-bit JPEG Extended frame failed to decode: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
	}

	worst, worstAt, differing := 0, -1, 0
	for i := range got {
		d := int(got[i]) - int(want[i])
		if d < 0 {
			d = -d
		}
		if d > 0 {
			differing++
		}
		if d > worst {
			worst, worstAt = d, i
		}
	}
	if worst > 1 {
		t.Errorf("sample %d differs from dcmtk's decode by %d, more than inverse DCT tolerance",
			worstAt, worst)
	}
	if differing*100 > len(got)*5 {
		t.Errorf("%d of %d samples differ (%.1f%%), too many for rounding alone",
			differing, len(got), 100*float64(differing)/float64(len(got)))
	}
}

// TestJPEGExtended12BitIsRefused records the half of the limitation that is
// real: 12-bit precision has no decoder, and says so rather than returning
// something.
func TestJPEGExtended12BitIsRefused(t *testing.T) {
	stream := readExtendedFixture(t, "mono_extended_12bit.jpg")

	got, err := compress.NewJPEGDecompressor().Decompress(stream)
	if err == nil {
		t.Fatalf("a 12-bit JPEG Extended frame decoded to %d bytes; no decoder handles that precision",
			len(got))
	}
	if !strings.Contains(err.Error(), "precision") {
		t.Errorf("the error does not name precision as the reason: %v", err)
	}
}
