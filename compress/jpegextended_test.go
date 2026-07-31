package compress_test

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// JPEG Extended (1.2.840.10008.1.2.4.51) allows 8-bit and 12-bit precision.
//
// The standard library's decoder handles SOF1 frames and rejects only the
// precision, so an 8-bit Extended frame always decoded and only 12-bit ones
// failed. Twelve-bit is now decoded here. Both are pinned against another
// implementation's output so neither can drift.

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

// TestJPEGExtended12BitDecodes checks a 12-bit frame against pydicom's decode
// of the same bytes.
//
// The fixture is the frame from pydicom's JPGExtended.dcm, and the expectation
// is what pydicom's pixel_array gives for it, so neither side of the comparison
// comes from this library. As above, the tolerance is one count: measured here
// the worst difference is 1 on 1.4% of samples.
func TestJPEGExtended12BitDecodes(t *testing.T) {
	stream := readExtendedFixture(t, "mono_extended_12bit.jpg")
	want := readExtendedFixture(t, "mono_extended_12bit_expected.raw")

	got, err := compress.NewJPEGDecompressor().Decompress(stream)
	if err != nil {
		t.Fatalf("a 12-bit JPEG Extended frame failed to decode: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
	}

	worst, worstAt, differing := 0, -1, 0
	for i := 0; i+1 < len(got); i += 2 {
		g := int(binary.LittleEndian.Uint16(got[i:]))
		w := int(binary.LittleEndian.Uint16(want[i:]))
		d := g - w
		if d < 0 {
			d = -d
		}
		if d > 0 {
			differing++
		}
		if d > worst {
			worst, worstAt = d, i/2
		}
	}
	samples := len(got) / 2
	if worst > 1 {
		t.Errorf("sample %d differs from pydicom's decode by %d, more than inverse DCT tolerance",
			worstAt, worst)
	}
	if differing*100 > samples*5 {
		t.Errorf("%d of %d samples differ (%.1f%%), too many for rounding alone",
			differing, samples, 100*float64(differing)/float64(samples))
	}
}

// TestJPEGExtendedRejectsAPartialSpectrumScan covers a stream that says it is
// sequential and is not.
//
// pydicom's JPEG-lossy.dcm carries a frame whose scan header declares Se of 0
// where a sequential frame must declare 63 — it codes the DC coefficient alone,
// which is the progressive structure. Decoding it as sequential would return a
// full-size image built from one coefficient per block: a recognizable, blocky,
// entirely wrong picture. libjpeg refuses it for the same reason, which is why
// pydicom cannot read that file either.
func TestJPEGExtendedRejectsAPartialSpectrumScan(t *testing.T) {
	stream := readExtendedFixture(t, "mono_extended_12bit_badscan.jpg")

	got, err := compress.NewJPEGDecompressor().Decompress(stream)
	if err == nil {
		t.Fatalf("a scan covering coefficients 0..0 decoded to %d bytes", len(got))
	}
	if !strings.Contains(err.Error(), "0..0") {
		t.Errorf("the error does not say which coefficients the scan covers: %v", err)
	}
}
