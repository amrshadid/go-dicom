package compress_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// TestYCbCrToRGBMatchesLibjpeg proves a JPEG decoded straight to RGB is
// converted with libjpeg's arithmetic, sample for sample.
//
// testdata/ycc/flat_444.jpg is 256 blocks of 8x8, each flat in Y, Cb and Cr,
// coded 4:4:4 at quality 100 with no color transform (EncodeJPEGBaseline), so
// every decoder's inverse DCT returns the same planes and only the color
// conversion can differ. flat_444_libjpeg.raw is Pillow's decode of it
// (libjpeg-turbo), which dcmtk's dcmdjpeg (libjpeg 6b) matches exactly.
// image/color's conversion differed from it by one level on 15.6% of samples.
func TestYCbCrToRGBMatchesLibjpeg(t *testing.T) {
	stream, err := os.ReadFile("testdata/ycc/flat_444.jpg")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/ycc/flat_444_libjpeg.raw")
	if err != nil {
		t.Fatal(err)
	}
	// No data set: the stream has no marker and identifiers 1, 2, 3, so the
	// decoder takes it for YCbCr and converts.
	got, err := compress.NewJPEGDecompressor().Decompress(stream)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, want) {
		return
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, libjpeg %d", len(got), len(want))
	}
	differing := 0
	for i := range got {
		if got[i] != want[i] {
			differing++
		}
	}
	t.Errorf("%d of %d samples differ from libjpeg's conversion (first at pixel %d)",
		differing, len(got), firstDifference(got, want)/3)
}

func firstDifference(a, b []byte) int {
	for i := range a {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}
