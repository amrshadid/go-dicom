package fileutil_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/fileutil"
)

// TestNewCodecIntegration tests codec integration creation.
func TestNewCodecIntegration(t *testing.T) {
	ci := fileutil.NewCodecIntegration()
	if ci == nil {
		t.Fatal("NewCodecIntegration returned nil")
	}
}

// TestIsCompressionSupported tests compression support check.
func TestIsCompressionSupported(t *testing.T) {
	ci := fileutil.NewCodecIntegration()

	if !ci.IsCompressionSupported(compress.UNCOMPRESSED) {
		t.Error("expected UNCOMPRESSED to be supported")
	}
}

// TestGetSupportedCompressions tests getting supported compressions.
func TestGetSupportedCompressions(t *testing.T) {
	ci := fileutil.NewCodecIntegration()

	compressions := ci.GetSupportedCompressions()
	if compressions == nil {
		t.Fatal("GetSupportedCompressions returned nil")
	}

	if len(compressions) == 0 {
		t.Error("expected at least one supported compression")
	}
}

// TestDecompressUncompressed tests decompressing uncompressed data.
func TestDecompressUncompressed(t *testing.T) {
	ci := fileutil.NewCodecIntegration()

	originalData := []byte("Test data")
	decompressed, err := ci.Decompress(originalData, compress.UNCOMPRESSED)
	if err != nil {
		t.Fatalf("Decompress error: %v", err)
	}

	if !bytes.Equal(decompressed, originalData) {
		t.Error("decompressed data mismatch")
	}
}

// TestGetCompressionInfo tests getting compression information.
func TestGetCompressionInfo(t *testing.T) {
	ci := fileutil.NewCodecIntegration()

	info := ci.GetCompressionInfo(compress.UNCOMPRESSED)
	if info.Name == "" {
		t.Error("expected compression info name")
	}
}

// TestNewPixelDataCache tests pixel data cache creation.
func TestNewPixelDataCache(t *testing.T) {
	cache := fileutil.NewPixelDataCache(100)
	if cache == nil {
		t.Fatal("NewPixelDataCache returned nil")
	}
}

// TestPixelDataCacheGetSet tests cache get and set.
func TestPixelDataCacheGetSet(t *testing.T) {
	cache := fileutil.NewPixelDataCache(100)

	testData := []byte("pixel data")
	cache.Set("key1", testData)

	data, exists := cache.Get("key1")
	if !exists {
		t.Error("expected key to exist in cache")
	}

	if !bytes.Equal(data.([]byte), testData) {
		t.Error("cached data mismatch")
	}
}

// TestPixelDataCacheClear tests cache clearing.
func TestPixelDataCacheClear(t *testing.T) {
	cache := fileutil.NewPixelDataCache(100)

	cache.Set("key1", []byte("data"))
	cache.Clear()

	_, exists := cache.Get("key1")
	if exists {
		t.Error("expected key to not exist after clear")
	}
}

// TestNewDeferredPixelDataReader tests deferred reader creation.
func TestNewDeferredPixelDataReader(t *testing.T) {
	ci := fileutil.NewCodecIntegration()

	reader := fileutil.NewDeferredPixelDataReader(
		"/tmp/test.dcm",
		compress.UNCOMPRESSED,
		0,
		1024,
		ci,
	)

	if reader == nil {
		t.Fatal("NewDeferredPixelDataReader returned nil")
	}

	if reader.IsLoaded() {
		t.Error("expected reader to not be loaded initially")
	}
}

// TestValidatePixelData tests pixel data validation.
func TestValidatePixelData(t *testing.T) {
	ci := fileutil.NewCodecIntegration()

	testData := []byte{0x00, 0x00, 0xFF, 0xFF}
	err := ci.ValidatePixelData(testData, compress.UNCOMPRESSED, len(testData))
	if err != nil {
		t.Fatalf("ValidatePixelData error: %v", err)
	}
}

// TestGetTransferSyntaxSupport tests transfer syntax support check.
func TestGetTransferSyntaxSupport(t *testing.T) {
	ci := fileutil.NewCodecIntegration()

	// Implicit VR Little Endian (uncompressed)
	support := ci.GetTransferSyntaxSupport("1.2.840.10008.1.2")
	if support.UID == "" {
		t.Error("expected transfer syntax support")
	}
}
