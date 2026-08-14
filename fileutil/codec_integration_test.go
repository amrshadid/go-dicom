package fileutil_test

import (
	"bytes"
	"os"
	"path/filepath"
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

// The reader's whole purpose is to read on first access, and for a while it could
// not: loadFromFile was a stub returning "file loading not implemented in this
// stub", so Get always failed while the package documented it as working. The
// test above did not catch it because it never called Get — it checked that the
// constructor returned something and stopped there.
//
// So these exercise the load path itself.
func TestDeferredPixelDataReaderReadsTheExtentItWasGiven(t *testing.T) {
	// Pixel data preceded by a header, so a correct read has to honour the offset
	// rather than returning the start of the file.
	const header = "not pixel data"
	pixels := []byte{0x01, 0x02, 0xFE, 0xFF, 0x10, 0x20}

	path := filepath.Join(t.TempDir(), "deferred.dcm")
	if err := os.WriteFile(path, append([]byte(header), pixels...), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	ci := fileutil.NewCodecIntegration()
	reader := fileutil.NewDeferredPixelDataReader(
		path, compress.UNCOMPRESSED, int64(len(header)), int64(len(pixels)), ci)

	got, err := reader.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, pixels) {
		t.Errorf("read % x, the file holds % x at that offset", got, pixels)
	}
	if !reader.IsLoaded() {
		t.Error("IsLoaded is false after a successful Get")
	}

	// Second access comes from memory. It must return the same bytes rather than
	// re-reading and discarding them, which is the point of deferring.
	again, err := reader.Get()
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if !bytes.Equal(again, pixels) {
		t.Errorf("second Get returned % x, first returned % x", again, got)
	}
}

func TestDeferredPixelDataReaderRefusesAnExtentTheFileDoesNotHold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "short.dcm")
	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03, 0x04}, 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	cases := []struct {
		name           string
		offset, length int64
	}{
		{"length past the end", 0, 4096},
		{"offset past the end", 8, 4},
		{"length beyond the deferred read limit", 0, 1 << 40},
		{"negative offset", -1, 4},
		{"zero length", 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ci := fileutil.NewCodecIntegration()
			reader := fileutil.NewDeferredPixelDataReader(
				path, compress.UNCOMPRESSED, tc.offset, tc.length, ci)

			// A short read must be an error, not a short buffer: a caller handed
			// truncated pixel data cannot tell that is what it received.
			data, err := reader.Get()
			if err == nil {
				t.Fatalf("Get returned %d bytes and no error for offset %d length %d",
					len(data), tc.offset, tc.length)
			}
			if reader.IsLoaded() {
				t.Error("IsLoaded is true after a failed Get")
			}
		})
	}
}

func TestDeferredPixelDataReaderReportsAMissingFile(t *testing.T) {
	ci := fileutil.NewCodecIntegration()
	reader := fileutil.NewDeferredPixelDataReader(
		filepath.Join(t.TempDir(), "absent.dcm"), compress.UNCOMPRESSED, 0, 16, ci)

	if _, err := reader.Get(); err == nil {
		t.Fatal("Get succeeded on a file that does not exist")
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
