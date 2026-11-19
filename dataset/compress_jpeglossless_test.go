package dataset_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// Lossless JPEG is lossless, so as for JPEG 2000 each test compresses,
// decompresses and compares: what comes back is what went in, and the data
// set in between says what it holds. That other decoders agree is the
// interop script's to show (dcmdjpeg and pydicom over the corpus).

const (
	syntaxJPEGLossless    = "1.2.840.10008.1.2.4.57"
	syntaxJPEGLosslessSV1 = "1.2.840.10008.1.2.4.70"
)

// interpretedSamples reads samples as a reader of the data set does: the low
// stored bits of each, sign-extended when signed. A signed sample is held in
// a lossless JPEG codestream as its low bits (PS3.5 8.2.1), so the bits above
// Bits Stored are the reader's to restore and not the codestream's.
func interpretedSamples(px []byte, allocated, stored int, signed bool) []int32 {
	n := len(px) / (allocated / 8)
	out := make([]int32, n)
	for i := range out {
		var v uint32
		if allocated == 8 {
			v = uint32(px[i])
		} else {
			v = uint32(binary.LittleEndian.Uint16(px[2*i:]))
		}
		v &= 1<<uint(stored) - 1
		s := int32(v)
		if signed && v&(1<<uint(stored-1)) != 0 {
			s -= 1 << uint(stored)
		}
		out[i] = s
	}
	return out
}

func equalSamples(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// compressLossless calls the method for syntax.
func compressLossless(ds *dataset.Dataset, syntax string) error {
	if syntax == syntaxJPEGLosslessSV1 {
		return ds.CompressJPEGLosslessSV1InPlace()
	}
	return ds.CompressJPEGLosslessInPlace()
}

// TestCompressJPEGLosslessIsLossless covers, in both syntaxes, the images
// DICOM has: 8 and 16 bits allocated, 8, 12 and 16 stored, signed and
// unsigned, one sample and three, one frame and several, one pixel, sizes
// that are not a multiple of anything. The pixels come back, the data set
// says lossless JPEG, and nothing is marked lossy.
func TestCompressJPEGLosslessIsLossless(t *testing.T) {
	cases := []struct {
		name                           string
		columns, rows, samples, frames int
		allocated, stored              int
		signed                         bool
		photometric                    string
	}{
		{"8-bit gray", 33, 17, 1, 1, 8, 8, false, "MONOCHROME2"},
		{"12-bit gray in 16", 64, 64, 1, 1, 16, 12, false, "MONOCHROME2"},
		{"16-bit gray", 70, 50, 1, 1, 16, 16, false, "MONOCHROME1"},
		{"16-bit signed gray", 50, 70, 1, 1, 16, 16, true, "MONOCHROME2"},
		{"12-bit signed gray in 16", 40, 40, 1, 1, 16, 12, true, "MONOCHROME2"},
		{"8-bit stored in 16", 21, 19, 1, 1, 16, 8, false, "MONOCHROME2"},
		{"three frames of 16-bit gray", 20, 30, 1, 3, 16, 16, false, "MONOCHROME2"},
		{"8-bit RGB", 31, 29, 3, 1, 8, 8, false, "RGB"},
		{"two frames of 8-bit RGB", 16, 16, 3, 2, 8, 8, false, "RGB"},
		{"16-bit RGB", 20, 20, 3, 1, 16, 16, false, "RGB"},
		{"YBR_FULL", 16, 16, 3, 1, 8, 8, false, "YBR_FULL"},
		{"palette indexes", 16, 16, 1, 1, 8, 8, false, "PALETTE COLOR"},
		{"one pixel", 1, 1, 1, 1, 16, 16, false, "MONOCHROME2"},
	}
	for _, syntax := range []string{syntaxJPEGLossless, syntaxJPEGLosslessSV1} {
		for _, c := range cases {
			t.Run(syntax+" "+c.name, func(t *testing.T) {
				ds, original := nativeImage(c.columns, c.rows, c.samples, c.frames, c.allocated, c.stored,
					c.signed, c.photometric, 0)
				if err := compressLossless(ds, syntax); err != nil {
					t.Fatalf("compress: %v", err)
				}
				if got := ds.TransferSyntaxUID(); got != syntax {
					t.Errorf("the transfer syntax is %s", got)
				}
				if got := textValue(t, ds, tagPhotometric); got != c.photometric {
					t.Errorf("Photometric Interpretation is %q; lossless JPEG has no color transform", got)
				}
				if elem, _ := ds.Get(tagPixels); elem.GetVR() != dataelem.OB {
					t.Errorf("encapsulated Pixel Data is %s, want OB", elem.GetVR())
				}
				if ds.Contains(tagLossy) {
					t.Error("a lossless compression set Lossy Image Compression")
				}
				if bytes.Equal(storedPixels(t, ds), original) {
					t.Fatal("the pixel data was not replaced")
				}

				if err := ds.DecompressInPlace(); err != nil {
					t.Fatalf("decompress: %v", err)
				}
				got := storedPixels(t, ds)
				if len(got) < len(original) || len(got) > len(original)+1 {
					t.Fatalf("%d bytes came back for %d", len(got), len(original))
				}
				want := interpretedSamples(original, c.allocated, c.stored, c.signed)
				if !equalSamples(interpretedSamples(got[:len(original)], c.allocated, c.stored, c.signed), want) {
					t.Error("the pixels did not come back as they were")
				}
			})
		}
	}
}

// TestCompressJPEGLosslessFromOtherLayouts proves the two things about an
// image the coding replaces: samples stored plane by plane come back pixel by
// pixel with Planar Configuration 0, and an image held in another compressed
// syntax is decoded first and returned as that syntax decodes it.
func TestCompressJPEGLosslessFromOtherLayouts(t *testing.T) {
	byPixel, interleaved := nativeImage(9, 7, 3, 1, 8, 8, false, "RGB", 0)
	planes := make([]byte, len(interleaved))
	for i := 0; i < 9*7; i++ {
		for c := 0; c < 3; c++ {
			planes[c*9*7+i] = interleaved[i*3+c]
		}
	}
	byPlane := byPixel.Clone()
	if err := byPlane.SetValue(tagPixels, planes); err != nil {
		t.Fatal(err)
	}
	planar := make([]byte, 2)
	binary.LittleEndian.PutUint16(planar, 1)
	if err := byPlane.SetValue(tagPlanar, planar); err != nil {
		t.Fatal(err)
	}
	if err := byPlane.CompressJPEGLosslessSV1InPlace(); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if elem, _ := byPlane.Get(tagPlanar); binary.LittleEndian.Uint16(elem.GetValue().([]byte)) != 0 {
		t.Error("Planar Configuration is not 0 under lossless JPEG")
	}
	if err := byPlane.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if got := storedPixels(t, byPlane); !bytes.Equal(got[:len(interleaved)], interleaved) {
		t.Error("an image stored by plane did not come back as the same pixels")
	}

	// From JPEG 2000 Lossless: the same samples, now as lossless JPEG.
	j2k, original := nativeImage(30, 20, 1, 1, 16, 16, true, "MONOCHROME2", 0)
	if err := j2k.CompressJPEG2000LosslessInPlace(); err != nil {
		t.Fatal(err)
	}
	if err := j2k.CompressJPEGLosslessInPlace(); err != nil {
		t.Fatalf("from JPEG 2000: %v", err)
	}
	if got := j2k.TransferSyntaxUID(); got != syntaxJPEGLossless {
		t.Fatalf("the transfer syntax is %s", got)
	}
	if err := j2k.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, j2k), original) {
		t.Error("from JPEG 2000: the pixels did not come back as they were")
	}
}

// TestCompressJPEGLosslessKeepsBitsAboveBitsStored proves an image whose
// samples use bits above Bits Stored, as old files with overlays in them do,
// is coded at Bits Allocated, so those bits come back.
func TestCompressJPEGLosslessKeepsBitsAboveBitsStored(t *testing.T) {
	ds, original := nativeImage(16, 16, 1, 1, 16, 12, false, "MONOCHROME2", 0)
	original[1] |= 0x80 // bit 15 of the first sample
	if err := ds.SetValue(tagPixels, original); err != nil {
		t.Fatal(err)
	}
	if err := ds.CompressJPEGLosslessSV1InPlace(); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, ds), original) {
		t.Error("the bits above Bits Stored were lost")
	}
}

// TestCompressJPEGLosslessLeavesAloneWhatItCannotDo proves a refusal changes
// nothing, and that compressing twice is compressing once.
func TestCompressJPEGLosslessLeavesAloneWhatItCannotDo(t *testing.T) {
	empty := dataset.NewDataset()
	if err := empty.CompressJPEGLosslessInPlace(); err == nil {
		t.Error("a data set with no Pixel Data was compressed")
	}

	deep, original := nativeImage(4, 4, 1, 1, 16, 16, false, "MONOCHROME2", 0)
	wide := make([]byte, 2)
	binary.LittleEndian.PutUint16(wide, 32)
	if err := deep.SetValue(tag.New(0x0028, 0x0100), wide); err != nil {
		t.Fatal(err)
	}
	if err := deep.CompressJPEGLosslessSV1InPlace(); err == nil {
		t.Error("an image of 32 bits allocated was compressed")
	}
	if deep.TransferSyntaxUID() != "1.2.840.10008.1.2.1" || !bytes.Equal(storedPixels(t, deep), original) {
		t.Error("a refusal changed the data set")
	}

	ds, _ := nativeImage(16, 16, 1, 1, 8, 8, false, "MONOCHROME2", 0)
	if err := ds.CompressJPEGLosslessSV1InPlace(); err != nil {
		t.Fatal(err)
	}
	once := append([]byte(nil), storedPixels(t, ds)...)
	if err := ds.CompressJPEGLosslessSV1InPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, ds), once) {
		t.Error("compressing an image already in this syntax changed it")
	}
}
