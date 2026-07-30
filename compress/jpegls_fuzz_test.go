package compress_test

import (
	"os"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// FuzzJPEGLSDecompress fuzzes the JPEG-LS parser.
//
// It has more attacker-reachable arithmetic than the other decoders: the coding
// parameters come from the frame header and an optional LSE segment, and they
// feed shift counts, array indices and loop bounds. A malformed frame must
// produce an error, never a panic.
func FuzzJPEGLSDecompress(f *testing.F) {
	for _, name := range []string{
		"mr_small_lossless.jls", "flat_lossless.jls",
		"ct_nearlossless.jls", "rgb_line_interleaved.jls",
	} {
		if data, err := os.ReadFile("testdata/jpegls/" + name); err == nil {
			f.Add(data)
		}
	}
	// A frame header claiming an enormous image, and an LSE with thresholds
	// that are not ordered.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xF7, 0x00, 0x0B, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x01, 0x11, 0x00})
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xF8, 0x00, 0x0D, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00})

	decoder := compress.NewJPEGLSDecompressor()
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = decoder.CanDecompress(data)
		if out, err := decoder.Decompress(data); err == nil && len(out) == 0 {
			t.Fatal("decoded successfully but produced no pixels")
		}
	})
}
