package compress_test

import (
	"bytes"
	"compress/zlib"
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
			result, err := decompressor.Decompress(test.input)
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
