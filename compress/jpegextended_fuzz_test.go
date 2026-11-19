package compress_test

import (
	"os"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// FuzzJPEGExtendedDecompress fuzzes the 12-bit sequential DCT parser.
//
// The routing matters as much as the decoding: a stream reaches this decoder
// only if its frame header says precision 12, and that header is read from the
// same attacker-controlled bytes. So the fuzzer drives the ordinary JPEG entry
// point rather than the decoder directly, and covers the marker walk that picks
// between the two.
//
// A malformed frame must produce an error — never a panic, and never an
// allocation sized by a header nobody checked.
func FuzzJPEGExtendedDecompress(f *testing.F) {
	for _, name := range []string{
		"mono_extended_12bit.jpg",
		"mono_extended_12bit_badscan.jpg",
		"rgb_extended_8bit.jpg",
	} {
		if data, err := os.ReadFile("testdata/jpegextended/" + name); err == nil {
			f.Add(data)
		}
	}
	// Subsampled color, which goes through the chroma filter.
	for _, name := range []string{"422_9x7.jpg", "420_9x7.jpg", "420_4x9.jpg"} {
		if data, err := os.ReadFile("testdata/upsample/" + name); err == nil {
			f.Add(data)
		}
	}

	// A 12-bit frame header claiming a size nothing backs, so the router sends
	// it here and the decoder meets an empty scan.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC1, 0x00, 0x0B, 0x0C, 0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x01, 0x11, 0x00})
	// Sampling factors of zero, which would divide by zero when sizing the grid.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC1, 0x00, 0x0B, 0x0C, 0x00, 0x08, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00})
	// A segment length shorter than the header it introduces.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC1, 0x00, 0x02, 0xFF, 0xDA, 0x00, 0x02})

	decoder := compress.NewJPEGDecompressor()
	// The decoder a data set gives a color frame, which hands image/jpeg the
	// stream with a JFIF marker put in front: it must fail exactly where the
	// guessing decoder fails, and the marker must not make a frame decode
	// that would not have.
	stored := compress.NewJPEGDecompressorForPhotometric("RGB")
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = decoder.CanDecompress(data)
		out, err := decoder.Decompress(data)
		if err == nil && len(out) == 0 {
			t.Fatal("decoded successfully but produced no pixels")
		}
		asStored, storedErr := stored.Decompress(data)
		if (err == nil) != (storedErr == nil) || len(asStored) != len(out) {
			t.Fatalf("with a data set's Photometric Interpretation: %d bytes, error %v; without: %d bytes, error %v",
				len(asStored), storedErr, len(out), err)
		}
		checkFrameWidths(t, decoder, data, out, err)
	})
}
