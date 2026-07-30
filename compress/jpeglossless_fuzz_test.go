package compress_test

import (
	"os"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// FuzzJPEGLosslessDecompress fuzzes the lossless JPEG parser.
//
// It reads attacker-controlled bytes out of a DICOM file and walks them with
// offsets and lengths taken from the same bytes, which is the shape of input
// this project already fuzzes for the PDU and data set parsers. A malformed
// frame must produce an error, never a panic and never an allocation sized by a
// header nobody checked.
func FuzzJPEGLosslessDecompress(f *testing.F) {
	for _, name := range []string{"mr_small_sv1.jpgls", "sc_rgb.jpgls"} {
		if data, err := os.ReadFile("testdata/jpeglossless/" + name); err == nil {
			f.Add(data)
		}
	}
	// Headers that lie: a frame far larger than the data, and a component count
	// with nothing behind it.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC3, 0x00, 0x0B, 0x10, 0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x01, 0x11, 0x00})
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC3, 0x00, 0x08, 0x08, 0x00, 0x01, 0x00, 0x01, 0xFF})

	decoder := compress.NewJPEGLosslessDecompressor()
	f.Fuzz(func(t *testing.T, data []byte) {
		// Both entry points: CanDecompress scans markers without decoding, and
		// is what the registry consults first.
		_ = decoder.CanDecompress(data)
		if out, err := decoder.Decompress(data); err == nil && len(out) == 0 {
			t.Fatal("decoded successfully but produced no pixels")
		}
	})
}
