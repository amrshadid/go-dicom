package compress_test

import (
	"bytes"
	"compress/zlib"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// ============================================================================
// Decompressor Registry Tests
// ============================================================================

func TestDecompressorRegistry(t *testing.T) {
	registry := compress.NewDecompressorRegistry()

	// Test Get existing decompressor
	deflateDecompressor, err := registry.Get(compress.DEFLATE)
	if err != nil {
		t.Fatalf("Get DEFLATE failed: %v", err)
	}
	if deflateDecompressor == nil {
		t.Error("Expected DEFLATE decompressor, got nil")
	}

	// Test Get non-existing decompressor
	_, err = registry.Get(compress.CompressionType("NONEXISTENT"))
	if err == nil {
		t.Error("Expected error for non-existing decompressor")
	}
}

func TestDecompressorRegistryRegister(t *testing.T) {
	registry := compress.NewDecompressorRegistry()

	// Create a custom decompressor
	customDecompressor := compress.NewDeflateDecompressor()

	// Register it
	err := registry.Register(compress.CompressionType("CUSTOM"), customDecompressor)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Get it back
	retrieved, err := registry.Get(compress.CompressionType("CUSTOM"))
	if err != nil {
		t.Fatalf("Get custom decompressor failed: %v", err)
	}
	if retrieved == nil {
		t.Error("Expected custom decompressor, got nil")
	}
}

func TestDecompressorRegistryRegisterNil(t *testing.T) {
	registry := compress.NewDecompressorRegistry()

	err := registry.Register(compress.CompressionType("NIL"), nil)
	if err == nil {
		t.Error("Expected error when registering nil decompressor")
	}
}

func TestDecompressorRegistryList(t *testing.T) {
	registry := compress.NewDecompressorRegistry()
	types := registry.List()

	if len(types) == 0 {
		t.Error("Expected at least one registered decompressor")
	}

	// Check that DEFLATE and RLE are registered
	hasDeflate := false
	hasRLE := false
	for _, compressionType := range types {
		if compressionType == compress.DEFLATE {
			hasDeflate = true
		}
		if compressionType == compress.RLE {
			hasRLE = true
		}
	}

	if !hasDeflate {
		t.Error("DEFLATE not found in registry")
	}
	if !hasRLE {
		t.Error("RLE not found in registry")
	}
}

func TestDecompressorRegistryDecompress(t *testing.T) {
	registry := compress.NewDecompressorRegistry()

	// Create test data
	original := []byte("Test data for decompression")
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	writer.Write(original)
	writer.Close()
	compressed := buf.Bytes()

	// Decompress using registry
	result, err := registry.Decompress(compress.DEFLATE, compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("Result mismatch: got %v, want %v", result, original)
	}
}

// ============================================================================
// Compression Info Tests
// ============================================================================

func TestGetCompressionInfo(t *testing.T) {
	tests := []struct {
		compressionType   compress.CompressionType
		expectedLossless  bool
		expectedSupported bool
	}{
		{compress.DEFLATE, true, true},
		{compress.RLE, true, true},
		{compress.JPEG, false, true},
		{compress.JPEG_LOSSLESS, true, false},
		{compress.JPEG_LS, true, false},
		{compress.JPEG_2000, true, false},
	}

	for _, test := range tests {
		t.Run(string(test.compressionType), func(t *testing.T) {
			info := compress.GetCompressionInfo(test.compressionType)
			if info.IsLossless != test.expectedLossless {
				t.Errorf("IsLossless mismatch: got %v, want %v", info.IsLossless, test.expectedLossless)
			}
			if info.IsSupported != test.expectedSupported {
				t.Errorf("IsSupported mismatch: got %v, want %v", info.IsSupported, test.expectedSupported)
			}
		})
	}
}

func TestGetCompressionInfoUnknown(t *testing.T) {
	unknownType := compress.CompressionType("UNKNOWN_COMPRESSION")
	info := compress.GetCompressionInfo(unknownType)

	if info.IsSupported {
		t.Error("Unknown compression should not be supported")
	}
	if !info.RequiresExternal {
		t.Error("Unknown compression should require external library")
	}
}

func TestCompressionInfoValues(t *testing.T) {
	info := compress.GetCompressionInfo(compress.DEFLATE)

	if info.Name == "" {
		t.Error("CompressionInfo Name should not be empty")
	}
	if info.Description == "" {
		t.Error("CompressionInfo Description should not be empty")
	}
	if info.Type != compress.DEFLATE {
		t.Errorf("CompressionInfo Type mismatch: got %v, want %v", info.Type, compress.DEFLATE)
	}
}
