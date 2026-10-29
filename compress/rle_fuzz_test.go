package compress_test

import (
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// FuzzRLEDecompress fuzzes the RLE frame decoder.
//
// The header's segment count and offsets come from the file, and each segment
// is PackBits whose control bytes say how much to copy and from where. It was
// the one decoder of pixel data with no fuzz target. A malformed frame must
// produce an error, never a panic, and a frame that decodes must hold a whole
// number of pixels for the layout it was decoded as.
func FuzzRLEDecompress(f *testing.F) {
	encoder := compress.NewRLECompressor()
	for _, layout := range []struct{ samples, bits int }{{1, 8}, {1, 16}, {3, 8}, {1, 32}} {
		frame := make([]byte, 16*layout.samples*layout.bits/8)
		for i := range frame {
			frame[i] = byte(i * 7 / 3) // runs and literals both
		}
		if encoded, err := encoder.CompressFrame(frame, layout.samples, layout.bits); err == nil {
			f.Add(encoded, uint8(layout.samples), uint8(layout.bits))
		}
	}
	// A header naming one segment that starts past the end of the frame.
	header := make([]byte, compress.RLEHeaderSize)
	binary.LittleEndian.PutUint32(header[0:], 1)
	binary.LittleEndian.PutUint32(header[4:], 0xFFFFFFFF)
	f.Add(header, uint8(1), uint8(8))

	decoder := compress.NewRLEDecompressor()
	f.Fuzz(func(t *testing.T, data []byte, samples, bits uint8) {
		// Both entry points: Decompress infers the layout from the segment
		// count, DecompressFrame is told it, and the registry uses either.
		_ = decoder.CanDecompress(data)
		_, _ = decoder.Decompress(data)

		out, err := decoder.DecompressFrame(data, int(samples), int(bits))
		if err != nil {
			return
		}
		if pixel := int(samples) * int(bits) / 8; len(out)%pixel != 0 {
			t.Fatalf("decoded %d bytes, not a whole number of %d-byte pixels", len(out), pixel)
		}
	})
}
