package compress_test

import (
	"bytes"
	"compress/zlib"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// ============================================================================
// DEFLATE Compressor Tests
// ============================================================================

func TestDeflateCompressor(t *testing.T) {
	compressor := compress.NewDeflateCompressor(-1)

	original := []byte("Test data for compression with DEFLATE")
	compressed, err := compressor.Compress(original)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	if len(compressed) == 0 {
		t.Error("Expected non-empty compressed data")
	}

	// Verify it's actually compressed
	if len(compressed) >= len(original) {
		// Compression didn't reduce size (could happen with small data)
		t.Logf("Compression didn't reduce size: original=%d, compressed=%d", len(original), len(compressed))
	}

	// Verify we can decompress it
	decompressor := compress.NewDeflateDecompressor()
	decompressed, err := decompressor.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(decompressed, original) {
		t.Errorf("Decompressed data mismatch: got %v, want %v", decompressed, original)
	}
}

func TestDeflateCompressorEmpty(t *testing.T) {
	compressor := compress.NewDeflateCompressor(-1)
	result, err := compressor.Compress([]byte{})
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if len(result) != 0 {
		t.Error("Expected empty result for empty input")
	}
}

func TestDeflateCompressionLevel(t *testing.T) {
	tests := []struct {
		name  string
		level int
	}{
		{"Default", -1},
		{"No compression", 0},
		{"Best compression", 9},
		{"Invalid high", 10},
		{"Invalid negative", -100},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compressor := compress.NewDeflateCompressor(test.level)
			if compressor == nil {
				t.Error("NewDeflateCompressor returned nil")
			}
		})
	}
}

// ============================================================================
// RLE Compressor Tests
// ============================================================================

func TestRLECompressor(t *testing.T) {
	compressor := compress.NewRLECompressor()

	tests := []struct {
		name     string
		input    []byte
		canRound bool
	}{
		{"Simple data", []byte{0x41, 0x42, 0x43}, true},
		{"Repeated data", []byte{0x41, 0x41, 0x41, 0x42, 0x42}, true},
		{"Empty", []byte{}, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compressed, err := compressor.Compress(test.input)
			if err != nil {
				t.Fatalf("Compress failed: %v", err)
			}

			if !test.canRound {
				return
			}

			// Try to decompress
			decompressor := compress.NewRLEDecompressor()
			decompressed, err := decompressor.Decompress(compressed)
			if err != nil {
				t.Fatalf("Decompress failed: %v", err)
			}

			if !bytes.Equal(decompressed, test.input) {
				t.Errorf("Roundtrip failed: got %v, want %v", decompressed, test.input)
			}
		})
	}
}

func TestRLECompressorEmpty(t *testing.T) {
	compressor := compress.NewRLECompressor()
	result, err := compressor.Compress([]byte{})
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if len(result) != 0 {
		t.Error("Expected empty result for empty input")
	}
}

func TestRLECompressionRoundtrip(t *testing.T) {
	compressor := compress.NewRLECompressor()
	decompressor := compress.NewRLEDecompressor()

	testData := []byte{
		0x41, 0x41, 0x41, 0x41, 0x41, // 5 A's
		0x42, 0x43, // 1 B, 1 C
		0x44, 0x44, 0x44, // 3 D's
	}

	compressed, err := compressor.Compress(testData)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	decompressed, err := decompressor.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(decompressed, testData) {
		t.Errorf("Roundtrip failed: got %v, want %v", decompressed, testData)
	}
}

// ============================================================================
// Compression Statistics Tests
// ============================================================================

func TestCompressionStatistics(t *testing.T) {
	stats := compress.Statistics{
		OriginalSize:     1000,
		CompressedSize:   250,
		CompressionType:  compress.DEFLATE,
		CompressionRatio: 0.25,
	}

	if stats.CompressionRatio != 0.25 {
		t.Errorf("Compression ratio mismatch: got %v, want 0.25", stats.CompressionRatio)
	}

	if stats.CompressionType != compress.DEFLATE {
		t.Errorf("Compression type mismatch: got %v, want DEFLATE", stats.CompressionType)
	}
}

func TestCompressionTypes(t *testing.T) {
	types := []compress.CompressionType{
		compress.UNCOMPRESSED,
		compress.DEFLATE,
		compress.RLE,
		compress.JPEG,
		compress.JPEG_LOSSLESS,
		compress.JPEG_LS,
		compress.JPEG_2000,
	}

	for _, ct := range types {
		info := compress.GetCompressionInfo(ct)
		if info.Type != ct {
			t.Errorf("CompressionInfo type mismatch: got %v, want %v", info.Type, ct)
		}
		if info.Name == "" {
			t.Errorf("CompressionInfo name is empty for %v", ct)
		}
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkDeflateCompress(b *testing.B) {
	compressor := compress.NewDeflateCompressor(-1)
	testData := make([]byte, 10000)
	for i := 0; i < len(testData); i++ {
		testData[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressor.Compress(testData)
	}
}

func BenchmarkDeflateDecompress(b *testing.B) {
	decompressor := compress.NewDeflateDecompressor()
	testData := make([]byte, 10000)
	for i := 0; i < len(testData); i++ {
		testData[i] = byte(i % 256)
	}

	// Create compressed data
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	writer.Write(testData)
	writer.Close()
	compressed := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decompressor.Decompress(compressed)
	}
}

func BenchmarkRLECompress(b *testing.B) {
	compressor := compress.NewRLECompressor()
	testData := make([]byte, 10000)
	for i := 0; i < len(testData); i++ {
		testData[i] = byte((i / 10) % 256) // Create some runs
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressor.Compress(testData)
	}
}

func BenchmarkRLEDecompress(b *testing.B) {
	decompressor := compress.NewRLEDecompressor()
	compressor := compress.NewRLECompressor()
	testData := make([]byte, 10000)
	for i := 0; i < len(testData); i++ {
		testData[i] = byte((i / 10) % 256)
	}

	compressed, _ := compressor.Compress(testData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decompressor.Decompress(compressed)
	}
}
