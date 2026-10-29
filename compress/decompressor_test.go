package compress_test

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/jpeg"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// ============================================================================
// DEFLATE Decompressor Tests
// ============================================================================

func TestDeflateDecompressor(t *testing.T) {
	decompressor := compress.NewDeflateDecompressor()

	// Create some test data and compress it
	original := []byte("Hello, World! This is a test of DEFLATE compression.")

	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	writer.Write(original)
	writer.Close()
	compressed := buf.Bytes()

	// Decompress
	decompressed, err := decompressor.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("Decompressed data mismatch: got %q, want %q", decompressed, original)
	}
}

func TestDeflateDecompressorEmpty(t *testing.T) {
	decompressor := compress.NewDeflateDecompressor()
	_, err := decompressor.Decompress([]byte{})
	if err == nil {
		t.Error("Expected error for empty data")
	}
}

func TestDeflateDecompressorInvalid(t *testing.T) {
	decompressor := compress.NewDeflateDecompressor()
	_, err := decompressor.Decompress([]byte{0xFF, 0xFF, 0xFF})
	if err == nil {
		t.Error("Expected error for invalid DEFLATE data")
	}
}

func TestDeflateCanDecompress(t *testing.T) {
	decompressor := compress.NewDeflateDecompressor()

	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{"DEFLATE header", []byte{0x78, 0x9C}, true},
		{"Empty", []byte{}, false},
		{"Single byte 0x78", []byte{0x78}, false},
		{"Wrong header", []byte{0xFF, 0xD8}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := decompressor.CanDecompress(test.data)
			if result != test.expected {
				t.Errorf("CanDecompress mismatch: got %v, want %v", result, test.expected)
			}
		})
	}
}

// ============================================================================
// RLE Decompressor Tests
// ============================================================================

func TestRLEDecompressor(t *testing.T) {
	decompressor := compress.NewRLEDecompressor()

	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			"Copy operation",
			[]byte{0x02, 0x41, 0x42, 0x43}, // Copy 3 bytes: ABC
			[]byte{0x41, 0x42, 0x43},
		},
		{
			"Run operation",
			[]byte{0xFF, 0x41, 0x00, 0x42}, // Run 2 bytes of 0x41, then copy 1 byte 0x42
			[]byte{0x41, 0x41, 0x42},
		},
		{
			"Multiple operations",
			[]byte{0x02, 0x41, 0x42, 0x43, 0xFE, 0x00}, // Copy ABC, then run 3 times 0x00 (257-0xFE=3)
			[]byte{0x41, 0x42, 0x43, 0x00, 0x00, 0x00},
		},
		{
			"Single byte copy",
			[]byte{0x00, 0x42}, // Copy 1 byte
			[]byte{0x42},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// The inputs above are bare PackBits streams. A DICOM RLE frame is
			// a 64-byte segment header followed by such streams, so each is
			// wrapped in a single-segment frame here. These cases previously
			// passed the bare stream, which is what the decompressor expected
			// before it read the header at all — decoder and tests agreed on a
			// format no DICOM file uses.
			result, err := decompressor.Decompress(rleFrame(test.input))
			if err != nil {
				t.Fatalf("Decompress failed: %v", err)
			}
			if !bytes.Equal(result, test.expected) {
				t.Errorf("Decompressed data mismatch: got %v, want %v", result, test.expected)
			}
		})
	}
}

func TestRLEDecompressorEmpty(t *testing.T) {
	decompressor := compress.NewRLEDecompressor()
	result, err := decompressor.Decompress([]byte{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d bytes", len(result))
	}
}

func TestRLEDecompressorInvalidData(t *testing.T) {
	decompressor := compress.NewRLEDecompressor()

	tests := []struct {
		name  string
		input []byte
	}{
		{"Incomplete copy", []byte{0x05, 0x41, 0x42}}, // Says copy 6 bytes but only 2 provided
		{"Incomplete run", []byte{0xFF}},              // Run operation without data byte
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decompressor.Decompress(test.input)
			if err == nil {
				t.Error("Expected error for invalid RLE data")
			}
		})
	}
}

// ============================================================================
// JPEG Decompressor Tests
// ============================================================================

func TestJPEGCanDecompress(t *testing.T) {
	decompressor := compress.NewJPEGDecompressor()

	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{"JPEG header", []byte{0xFF, 0xD8, 0xFF}, true},
		{"Empty", []byte{}, false},
		{"Two bytes", []byte{0xFF, 0xD8}, false},
		{"Wrong header", []byte{0x78, 0x9C, 0x00}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := decompressor.CanDecompress(test.data)
			if result != test.expected {
				t.Errorf("CanDecompress mismatch: got %v, want %v", result, test.expected)
			}
		})
	}
}

// TestJPEGFrameWithNoSamplesIsAnError: a frame of zero columns or rows is an
// error, not a successful decode of no bytes (#165).
func TestJPEGFrameWithNoSamplesIsAnError(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	sof := bytes.Index(encoded.Bytes(), []byte{0xFF, 0xC0})
	if sof < 0 {
		t.Fatal("the encoder wrote no SOF0")
	}

	// SOF0: marker(2) Lf(2) P(1) Y(2) X(2).
	for name, offset := range map[string]int{"no columns": 7, "no rows": 5} {
		t.Run(name, func(t *testing.T) {
			data := bytes.Clone(encoded.Bytes())
			data[sof+offset], data[sof+offset+1] = 0, 0
			out, err := compress.NewJPEGDecompressor().Decompress(data)
			if err == nil {
				t.Fatalf("decoded a frame with no samples to %d bytes and no error", len(out))
			}
		})
	}

	// And the unpatched frame decodes, so the errors above are about the size.
	out, err := compress.NewJPEGDecompressor().Decompress(encoded.Bytes())
	if err != nil {
		t.Fatalf("the unpatched frame did not decode: %v", err)
	}
	if len(out) != 64 {
		t.Errorf("the unpatched frame decoded to %d bytes, want 64", len(out))
	}
}
