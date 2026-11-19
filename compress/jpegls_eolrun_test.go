package compress_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// A run that reaches the end of a line partway through a block is coded, by
// T.87 A.7.1.2, as a single 1 bit. This package's encoder used to write a 0 and
// the remainder in J bits there, as for an interrupted run, which every decoder
// reads to the same samples.
//
// testdata/jpegls/eolrun holds frames in that older form. source.raw is a 29x16
// 8-bit image whose lines turn flat, at the value 100, from a random column, so
// that many runs end at the end of the line. lossless_old_form.jls is it coded
// by this package as released in 1.10.0 (develop wrote the same bytes until the
// change); near2_old_form.jls is it coded at NEAR 2 by develop before the
// change, and near2_expected.raw is CharLS 2.2's decoding of that frame, which
// libjpeg agrees with.
const eolRunWidth, eolRunHeight = 29, 16

func readEOLRun(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "jpegls", "eolrun", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestJPEGLSEndOfLineRunIsOneBit proves a run that ends at the end of the line
// inside a block costs one 1 bit, so the frame is byte for byte the one CharLS
// writes. The images are flat, W by 2, and the expected entropy-coded bytes are
// CharLS 2.2.0's, from its C API. Widths 5, 7, 9 and up are the ones whose
// second line ends partway through a block; the encoder wrote 0 plus a length
// there and differed from CharLS at every one of them.
func TestJPEGLSEndOfLineRunIsOneBit(t *testing.T) {
	for _, tc := range []struct {
		width   int
		entropy string
	}{
		{1, "c0"}, {2, "f0"}, {3, "f8"}, {4, "fc"}, {5, "ff00"}, {6, "ff00"}, {7, "ff60"},
		{8, "ff40"}, {9, "ff70"}, {10, "ff60"}, {11, "ff78"}, {12, "ff70"}, {13, "ff7c"},
		{14, "ff7c"}, {15, "ff7c"}, {16, "ff7c"}, {17, "ff7e"}, {18, "ff7e"}, {19, "ff7e"},
	} {
		stream, err := compress.EncodeJPEGLS(make([]byte, tc.width*2), tc.width, 2, 1, 8)
		if err != nil {
			t.Fatal(err)
		}
		// SOI, a 13-byte SOF55 and a 10-byte SOS come first, EOI last.
		got := hex.EncodeToString(stream[25 : len(stream)-2])
		if got != tc.entropy {
			t.Errorf("%dx2 flat: entropy-coded data %s, CharLS writes %s", tc.width, got, tc.entropy)
		}
	}
}

// TestJPEGLSOldEndOfLineRunFormStillDecodes proves frames this package wrote
// before the change, with an end-of-line run coded as 0 plus a length, still
// decode to their samples: the lossless one written by 1.10.0 to the source,
// the NEAR 2 one to what CharLS decodes it to. It also proves the fixtures hold
// the older form, since the encoder now writes each of them shorter.
func TestJPEGLSOldEndOfLineRunFormStillDecodes(t *testing.T) {
	source := readEOLRun(t, "source.raw")
	for _, tc := range []struct {
		file string
		near int
		want []byte
	}{
		{"lossless_old_form.jls", 0, source},
		{"near2_old_form.jls", 2, readEOLRun(t, "near2_expected.raw")},
	} {
		old := readEOLRun(t, tc.file)
		got, err := compress.NewJPEGLSDecompressor().Decompress(old)
		if err != nil {
			t.Errorf("%s: %v", tc.file, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("%s: decoded differently, worst sample %d away", tc.file, worstError(got, tc.want, 8))
		}

		now, err := compress.EncodeJPEGLSNearLossless(source, eolRunWidth, eolRunHeight, 1, 8, tc.near)
		if err != nil {
			t.Fatal(err)
		}
		if len(now) >= len(old) {
			t.Errorf("%s: the encoder now writes %d bytes, the old form took %d; the fixture no longer "+
				"holds an end-of-line run coded with a length", tc.file, len(now), len(old))
		}
		if got, err := compress.NewJPEGLSDecompressor().Decompress(now); err != nil || !bytes.Equal(got, tc.want) {
			t.Errorf("%s: the new coding does not decode to the same samples (%v)", tc.file, err)
		}
	}
}
