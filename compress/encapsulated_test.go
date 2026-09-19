package compress_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// TestIsEncapsulated separates the two questions IsCompressed conflated.
// Deflated is compressed, but its Pixel Data is native (#132).
func TestIsEncapsulated(t *testing.T) {
	for uid, want := range map[string]bool{
		"":                        false, // nothing says it is
		"1.2.840.10008.1.2":       false, // Implicit VR Little Endian
		"1.2.840.10008.1.2.1":     false, // Explicit VR Little Endian
		"1.2.840.10008.1.2.2":     false, // Explicit VR Big Endian
		"1.2.840.10008.1.2.1.99":  false, // Deflated: the data set, not the pixels
		"1.2.840.10008.1.2.5":     true,  // RLE Lossless
		"1.2.840.10008.1.2.4.50":  true,  // JPEG Baseline
		"1.2.840.10008.1.2.4.80":  true,  // JPEG-LS Lossless
		"1.2.840.10008.1.2.4.90":  true,  // JPEG 2000 Lossless
		"1.2.840.10008.1.2.4.201": true,  // HTJ2K, newer than this list: treated as fragments
	} {
		if got := compress.IsEncapsulated(uid); got != want {
			t.Errorf("IsEncapsulated(%q) = %v, want %v", uid, got, want)
		}
	}
	if !compress.IsCompressed("1.2.840.10008.1.2.1.99") {
		t.Error("Deflated is still compressed; only its pixel data is native")
	}
}
