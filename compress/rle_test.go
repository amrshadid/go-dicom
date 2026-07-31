package compress_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// rleFrame assembles an RLE frame: the 64-byte segment header followed by the
// given already-encoded segments.
func rleFrame(segments ...[]byte) []byte {
	var body bytes.Buffer
	offsets := make([]uint32, len(segments))
	offset := uint32(compress.RLEHeaderSize)
	for i, seg := range segments {
		offsets[i] = offset
		body.Write(seg)
		offset += uint32(len(seg))
	}

	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(segments)))
	for i := 0; i < compress.MaxRLESegments; i++ {
		var v uint32
		if i < len(offsets) {
			v = offsets[i]
		}
		_ = binary.Write(&out, binary.LittleEndian, v)
	}
	out.Write(body.Bytes())
	return out.Bytes()
}

// literal encodes bytes as a single PackBits literal run, padded to an even
// length the way a conformant encoder does (PS3.5 §G.3.2).
func literal(data []byte) []byte {
	seg := append([]byte{byte(len(data) - 1)}, data...)
	if len(seg)%2 != 0 {
		seg = append(seg, 0x00)
	}
	return seg
}

// TestRLEDecodesSixteenBitGrayscale covers the layout that was wrong: a 16-bit
// sample is split across two segments, most significant byte first, and the
// output must be little endian.
//
// The decoder previously ignored the segment header entirely and PackBits-
// decoded the whole frame as one stream, treating the header's offsets as
// control bytes. On MR_small_RLE.dcm that produced 8736 bytes where 8192 is
// correct, and the content was not pixel data at any offset.
func TestRLEDecodesSixteenBitGrayscale(t *testing.T) {
	msb := []byte{0x01, 0x02, 0x03, 0x04}
	lsb := []byte{0xAA, 0xBB, 0xCC, 0xDD}

	frame := rleFrame(literal(msb), literal(lsb))

	got, err := compress.NewRLEDecompressor().DecompressFrame(frame, 1, 16)
	if err != nil {
		t.Fatalf("DecompressFrame: %v", err)
	}

	// Pixel n is lsb[n] then msb[n]: segment order is MSB-first, output is
	// little endian, so the two are mirrored.
	want := []byte{0xAA, 0x01, 0xBB, 0x02, 0xCC, 0x03, 0xDD, 0x04}
	if !bytes.Equal(got, want) {
		t.Errorf("got  % X\nwant % X", got, want)
	}
}

// TestRLEDecodesColorInterleaved covers three 8-bit samples, which arrive as
// three planar segments and must come out pixel-interleaved.
func TestRLEDecodesColorInterleaved(t *testing.T) {
	red := []byte{0x10, 0x11}
	green := []byte{0x20, 0x21}
	blue := []byte{0x30, 0x31}

	frame := rleFrame(literal(red), literal(green), literal(blue))

	got, err := compress.NewRLEDecompressor().DecompressFrame(frame, 3, 8)
	if err != nil {
		t.Fatalf("DecompressFrame: %v", err)
	}

	want := []byte{0x10, 0x20, 0x30, 0x11, 0x21, 0x31}
	if !bytes.Equal(got, want) {
		t.Errorf("got  % X\nwant % X", got, want)
	}
}

// TestRLEReplicateRun covers the run-length half of PackBits, which the literal
// runs used elsewhere in these tests never exercise.
func TestRLEReplicateRun(t *testing.T) {
	// 0xFF repeats the following byte 257-255 = 2 times.
	segment := []byte{0xFF, 0x7E, 0xFF, 0x7E}
	frame := rleFrame(segment)

	got, err := compress.NewRLEDecompressor().DecompressFrame(frame, 1, 8)
	if err != nil {
		t.Fatalf("DecompressFrame: %v", err)
	}
	want := []byte{0x7E, 0x7E, 0x7E, 0x7E}
	if !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

// TestRLETrailingPadByteIsNotData covers the pad byte every conformant encoder
// may append to make a segment an even number of bytes.
//
// A pad byte is indistinguishable from a control byte starting a run, so a
// decoder that treats a run cut short by the segment end as corruption rejects
// valid files. MR_small_RLE.dcm is exactly this case: its first segment encodes
// all 4096 expected bytes in 1883 bytes and pads to 1884 with 0x00, which reads
// as a one-byte literal run with nothing following it.
func TestRLETrailingPadByteIsNotData(t *testing.T) {
	// One literal run of two bytes, then a 0x00 pad with no data behind it.
	segment := []byte{0x01, 0x41, 0x42, 0x00}
	frame := rleFrame(segment)

	got, err := compress.NewRLEDecompressor().DecompressFrame(frame, 1, 8)
	if err != nil {
		t.Fatalf("DecompressFrame: %v — the pad byte was treated as data", err)
	}
	if want := []byte{0x41, 0x42}; !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

// TestRLERejectsMismatchedSegments covers the check that catches genuine
// corruption, now that a truncated trailing run no longer fails on its own.
// Every segment holds one byte per pixel, so unequal lengths mean damage.
func TestRLERejectsMismatchedSegments(t *testing.T) {
	frame := rleFrame(
		literal([]byte{0x01, 0x02, 0x03, 0x04}),
		literal([]byte{0xAA, 0xBB}), // one byte per pixel short
	)

	_, err := compress.NewRLEDecompressor().DecompressFrame(frame, 1, 16)
	if err == nil {
		t.Fatal("expected an error when segments decode to different lengths, got nil")
	}
}

// TestRLERejectsMalformedHeaders covers the header checks, since every offset
// in it is attacker-controlled for a file from an untrusted source.
func TestRLERejectsMalformedHeaders(t *testing.T) {
	valid := rleFrame(literal([]byte{0x01, 0x02}))

	tests := []struct {
		name  string
		frame func() []byte
	}{
		{
			name:  "shorter than the header",
			frame: func() []byte { return make([]byte, compress.RLEHeaderSize-1) },
		},
		{
			name: "zero segments",
			frame: func() []byte {
				f := append([]byte(nil), valid...)
				binary.LittleEndian.PutUint32(f[0:4], 0)
				return f
			},
		},
		{
			name: "more segments than the header can hold",
			frame: func() []byte {
				f := append([]byte(nil), valid...)
				binary.LittleEndian.PutUint32(f[0:4], compress.MaxRLESegments+1)
				return f
			},
		},
		{
			name: "offset inside the header",
			frame: func() []byte {
				f := append([]byte(nil), valid...)
				binary.LittleEndian.PutUint32(f[4:8], 8)
				return f
			},
		},
		{
			name: "offset past the end of the frame",
			frame: func() []byte {
				f := append([]byte(nil), valid...)
				binary.LittleEndian.PutUint32(f[4:8], uint32(len(f))+1024)
				return f
			},
		},
	}

	d := compress.NewRLEDecompressor()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame := tc.frame()
			if _, err := d.DecompressFrame(frame, 1, 8); err == nil {
				t.Error("DecompressFrame accepted a malformed header")
			}
			if d.CanDecompress(frame) {
				t.Error("CanDecompress accepted a malformed header")
			}
		})
	}
}

// TestRLECanDecompressRecognizesRLE guards the previous behavior, which was to
// return false unconditionally — so a caller asking before decoding was told
// RLE data was not RLE.
func TestRLECanDecompressRecognizesRLE(t *testing.T) {
	frame := rleFrame(literal([]byte{0x01, 0x02}))
	if !compress.NewRLEDecompressor().CanDecompress(frame) {
		t.Error("CanDecompress returned false for a valid RLE frame")
	}
}

// TestRLEDecompressInfersLayout covers the interface-level entry point, which
// has no image metadata and must work out the layout from the segment count.
func TestRLEDecompressInfersLayout(t *testing.T) {
	frame := rleFrame(
		literal([]byte{0x01, 0x02}),
		literal([]byte{0xAA, 0xBB}),
	)

	got, err := compress.NewRLEDecompressor().Decompress(frame)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	// Two segments means one 16-bit sample.
	if want := []byte{0xAA, 0x01, 0xBB, 0x02}; !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

// TestRLEEncoderRunLengthCap covers the ceiling on a replicate run.
//
// A replicate run is encoded as the control byte 257-N, which must land in
// 0x81..0xFF, so N tops out at 128. The encoder allowed 256, emitting
// 257-256 = 1 — a control byte a decoder reads as a two-byte *literal* run. The
// segment then decoded to a different length than its siblings, which is how
// this was found: re-encoding MR_small_RLE.dcm produced a second segment of
// 9578 bytes where 4096 was correct.
func TestRLEEncoderRunLengthCap(t *testing.T) {
	// Long enough to need splitting into several maximum-length runs.
	pixels := bytes.Repeat([]byte{0x5A}, 700)

	frame, err := compress.NewRLECompressor().CompressFrame(pixels, 1, 8)
	if err != nil {
		t.Fatalf("CompressFrame: %v", err)
	}

	got, err := compress.NewRLEDecompressor().DecompressFrame(frame, 1, 8)
	if err != nil {
		t.Fatalf("DecompressFrame: %v", err)
	}
	if !bytes.Equal(got, pixels) {
		t.Errorf("round trip of a %d byte run lost data: got %d bytes", len(pixels), len(got))
	}
}

// TestRLEEncoderLiteralLengthCap covers the ceiling on a literal run.
//
// The literal control byte is N-1 and must stay below 0x80, so a literal run
// holds at most 128 bytes. The encoder allowed 129, emitting 0x80 — the no-op
// marker — so a decoder skipped it and read the following data as control
// bytes.
func TestRLEEncoderLiteralLengthCap(t *testing.T) {
	// Non-repeating, so the encoder has to emit literal runs and split them.
	pixels := make([]byte, 700)
	for i := range pixels {
		pixels[i] = byte(i * 7)
	}

	frame, err := compress.NewRLECompressor().CompressFrame(pixels, 1, 8)
	if err != nil {
		t.Fatalf("CompressFrame: %v", err)
	}

	got, err := compress.NewRLEDecompressor().DecompressFrame(frame, 1, 8)
	if err != nil {
		t.Fatalf("DecompressFrame: %v", err)
	}
	if !bytes.Equal(got, pixels) {
		t.Errorf("round trip of %d literal bytes lost data: got %d bytes", len(pixels), len(got))
	}
}

// TestRLERoundTripSixteenBitColor exercises the widest layout the header can
// describe with both planes and samples in play.
func TestRLERoundTripSixteenBitColor(t *testing.T) {
	// 3 samples x 2 bytes = 6 segments, the maximum for ordinary image data.
	pixels := make([]byte, 6*256)
	for i := range pixels {
		pixels[i] = byte(i % 251) // 251 is prime, so runs and literals both occur
	}

	frame, err := compress.NewRLECompressor().CompressFrame(pixels, 3, 16)
	if err != nil {
		t.Fatalf("CompressFrame: %v", err)
	}

	got, err := compress.NewRLEDecompressor().DecompressFrame(frame, 3, 16)
	if err != nil {
		t.Fatalf("DecompressFrame: %v", err)
	}
	if !bytes.Equal(got, pixels) {
		t.Error("round trip of 16-bit color data lost or reordered bytes")
	}
}
