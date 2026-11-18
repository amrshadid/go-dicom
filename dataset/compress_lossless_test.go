package dataset_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The RLE and JPEG-LS methods were the network's alone, so a file could not be
// written in either (#363). Each test here compresses, decodes with this
// library and compares; the proof that other decoders agree is pydicom and
// dcmtk over the corpus, recorded in the pull request.

var losslessCoders = []struct {
	name, syntax string
	compress     func(*dataset.Dataset) error
}{
	{"RLE", "1.2.840.10008.1.2.5", (*dataset.Dataset).CompressRLELosslessInPlace},
	{"JPEG-LS", "1.2.840.10008.1.2.4.80", (*dataset.Dataset).CompressJPEGLSLosslessInPlace},
}

// TestCompressLosslessIsLossless: for both syntaxes, 8 and 16 bits, stored
// 8, 12 and 16, signed and unsigned, gray and color, one frame and several,
// the samples come back exactly, the data set says what it holds, and nothing
// lossy is recorded.
func TestCompressLosslessIsLossless(t *testing.T) {
	cases := []struct {
		name                           string
		columns, rows, samples, frames int
		allocated, stored              int
		signed                         bool
		photometric                    string
	}{
		{"8-bit gray", 33, 17, 1, 1, 8, 8, false, "MONOCHROME2"},
		{"12-bit gray in 16", 64, 64, 1, 1, 16, 12, false, "MONOCHROME2"},
		{"8 stored in 16", 20, 20, 1, 1, 16, 8, false, "MONOCHROME2"},
		{"16-bit signed gray", 50, 70, 1, 1, 16, 16, true, "MONOCHROME2"},
		{"12-bit signed gray in 16", 40, 40, 1, 1, 16, 12, true, "MONOCHROME2"},
		{"three frames of 16-bit gray", 20, 30, 1, 3, 16, 16, false, "MONOCHROME1"},
		{"8-bit RGB", 31, 29, 3, 1, 8, 8, false, "RGB"},
		{"two frames of 16-bit RGB", 16, 16, 3, 2, 16, 16, false, "RGB"},
		{"one pixel", 1, 1, 1, 1, 16, 16, false, "MONOCHROME2"},
	}
	for _, coder := range losslessCoders {
		for _, c := range cases {
			t.Run(coder.name+"/"+c.name, func(t *testing.T) {
				ds, original := nativeImage(c.columns, c.rows, c.samples, c.frames, c.allocated, c.stored,
					c.signed, c.photometric, 0)
				if err := coder.compress(ds); err != nil {
					t.Fatalf("compress: %v", err)
				}
				if got := ds.TransferSyntaxUID(); got != coder.syntax {
					t.Errorf("the transfer syntax is %s", got)
				}
				if elem, _ := ds.Get(tagPixels); elem.GetVR() != dataelem.OB {
					t.Errorf("encapsulated Pixel Data is %s, want OB", elem.GetVR())
				}
				if got := textValue(t, ds, tagPhotometric); got != c.photometric {
					t.Errorf("Photometric Interpretation became %q", got)
				}
				if ds.Contains(tagLossy) {
					t.Error("a lossless compression set Lossy Image Compression")
				}
				if err := ds.DecompressInPlace(); err != nil {
					t.Fatalf("decompress: %v", err)
				}
				got := storedPixels(t, ds)
				if len(got) < len(original) || len(got) > len(original)+1 || !bytes.Equal(got[:len(original)], original) {
					t.Error("the pixels did not come back as they were")
				}
			})
		}
	}
}

// TestCompressLosslessDescribesWhatItCodes: an image stored plane by plane is
// coded pixel by pixel and says Planar Configuration 0, a native 4:2:2 image
// is coded with its chroma expanded and says YBR_FULL, and an image held in
// another compressed syntax is decoded first and comes back as it decodes.
func TestCompressLosslessDescribesWhatItCodes(t *testing.T) {
	for _, coder := range losslessCoders {
		t.Run(coder.name, func(t *testing.T) {
			byPixel, interleaved := nativeImage(8, 6, 3, 1, 8, 8, false, "RGB", 0)
			planes := make([]byte, len(interleaved))
			for i := 0; i < 8*6; i++ {
				for c := 0; c < 3; c++ {
					planes[c*8*6+i] = interleaved[i*3+c]
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
			if err := coder.compress(byPlane); err != nil {
				t.Fatalf("compress: %v", err)
			}
			if elem, _ := byPlane.Get(tagPlanar); binary.LittleEndian.Uint16(elem.GetValue().([]byte)) != 0 {
				t.Error("an image stored by plane is still described as Planar Configuration 1")
			}
			if err := byPlane.DecompressInPlace(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(storedPixels(t, byPlane), interleaved) {
				t.Error("an image stored by plane did not come back as the same pixels")
			}

			// 4:2:2: two luminance samples share one chroma pair, Y1 Y2 Cb Cr.
			ybr, _ := nativeImage(4, 2, 3, 1, 8, 8, false, "YBR_FULL_422", 0)
			sub := []byte{10, 20, 100, 200, 30, 40, 110, 210, 50, 60, 120, 220, 70, 80, 130, 230}
			if err := ybr.SetValue(tagPixels, sub); err != nil {
				t.Fatal(err)
			}
			if err := coder.compress(ybr); err != nil {
				t.Fatalf("compress 4:2:2: %v", err)
			}
			if got := textValue(t, ybr, tagPhotometric); got != "YBR_FULL" {
				t.Errorf("4:2:2 coded with full chroma is described as %q", got)
			}
			if err := ybr.DecompressInPlace(); err != nil {
				t.Fatal(err)
			}
			if got := storedPixels(t, ybr); !bytes.Equal(got[:6], []byte{10, 100, 200, 20, 100, 200}) {
				t.Errorf("4:2:2 decoded to % d, want each chroma pair on both pixels", got[:6])
			}

			jpeg, _ := smoothImage(3, 1, "RGB")
			if err := jpeg.CompressJPEGBaselineInPlace(90); err != nil {
				t.Fatal(err)
			}
			decoded := jpeg.Clone()
			if err := decoded.DecompressInPlace(); err != nil {
				t.Fatal(err)
			}
			if err := coder.compress(jpeg); err != nil {
				t.Fatalf("from JPEG Baseline: %v", err)
			}
			if got := textValue(t, jpeg, tag.New(0x0028, 0x2110)); got != "01" {
				t.Errorf("Lossy Image Compression is %q: it was lossily compressed before and still is", got)
			}
			if err := jpeg.DecompressInPlace(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(storedPixels(t, jpeg), storedPixels(t, decoded)) {
				t.Error("from JPEG Baseline: the pixels are not what the JPEG decodes to")
			}
		})
	}
}

// TestCompressJPEGLSPrecisionKeepsEveryBit: the codestream's precision is
// Bits Stored only when every sample fits in it, and a 16-bit image is coded
// at no less than 9 bits, so a decoder gives two bytes a sample back.
func TestCompressJPEGLSPrecisionKeepsEveryBit(t *testing.T) {
	precision := func(t *testing.T, ds *dataset.Dataset) int {
		t.Helper()
		frames, err := ds.ExtractEncapsulatedFrames()
		if err != nil || len(frames.Fragments) < 1 {
			t.Fatalf("no fragments: %v", err)
		}
		frame := frames.Fragments[0]
		at := bytes.Index(frame, []byte{0xFF, 0xF7}) // SOF55, T.87 C.2.2
		if at < 0 || at+4 >= len(frame) {
			t.Fatal("no SOF55 marker")
		}
		return int(frame[at+4])
	}

	overlay, original := nativeImage(16, 16, 1, 1, 16, 12, false, "MONOCHROME2", 0)
	original[1] |= 0x80 // bit 15 of the first sample
	if err := overlay.SetValue(tagPixels, original); err != nil {
		t.Fatal(err)
	}
	if err := overlay.CompressJPEGLSLosslessInPlace(); err != nil {
		t.Fatal(err)
	}
	if p := precision(t, overlay); p != 16 {
		t.Errorf("a sample uses bit 15 and the precision is %d", p)
	}
	if err := overlay.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, overlay), original) {
		t.Error("the bits above Bits Stored were lost")
	}

	narrow, _ := nativeImage(16, 16, 1, 1, 16, 8, false, "MONOCHROME2", 0)
	if err := narrow.CompressJPEGLSLosslessInPlace(); err != nil {
		t.Fatal(err)
	}
	if p := precision(t, narrow); p != 9 {
		t.Errorf("8 bits stored in 16 is coded at %d bits, want 9", p)
	}
	twelve, _ := nativeImage(16, 16, 1, 1, 16, 12, false, "MONOCHROME2", 0)
	if err := twelve.CompressJPEGLSLosslessInPlace(); err != nil {
		t.Fatal(err)
	}
	if p := precision(t, twelve); p != 12 {
		t.Errorf("12 bits stored is coded at %d bits", p)
	}
}

// TestCompressLosslessLeavesAloneWhatItCannotDo: a refusal changes nothing,
// names why, and compressing twice is compressing once.
func TestCompressLosslessLeavesAloneWhatItCannotDo(t *testing.T) {
	for _, coder := range losslessCoders {
		t.Run(coder.name, func(t *testing.T) {
			if err := coder.compress(dataset.NewDataset()); err == nil {
				t.Error("a data set with no Pixel Data was compressed")
			}

			bitmap, original := nativeImage(16, 16, 1, 1, 8, 8, false, "MONOCHROME2", 0)
			one := make([]byte, 2)
			binary.LittleEndian.PutUint16(one, 1)
			if err := bitmap.SetValue(tag.New(0x0028, 0x0100), one); err != nil {
				t.Fatal(err)
			}
			if err := coder.compress(bitmap); err == nil {
				t.Error("an image of 1 bit allocated was compressed")
			}
			if bitmap.TransferSyntaxUID() != "1.2.840.10008.1.2.1" || !bytes.Equal(storedPixels(t, bitmap), original) {
				t.Error("a refusal changed the data set")
			}

			ds, _ := nativeImage(16, 16, 1, 1, 8, 8, false, "MONOCHROME2", 0)
			if err := coder.compress(ds); err != nil {
				t.Fatal(err)
			}
			once := append([]byte(nil), storedPixels(t, ds)...)
			if err := coder.compress(ds); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(storedPixels(t, ds), once) {
				t.Error("compressing an image already in this syntax changed it")
			}
		})
	}

	deep, _ := nativeImage(4, 4, 1, 1, 16, 16, false, "MONOCHROME2", 0)
	wide := make([]byte, 2)
	binary.LittleEndian.PutUint16(wide, 32)
	if err := deep.SetValue(tag.New(0x0028, 0x0100), wide); err != nil {
		t.Fatal(err)
	}
	if err := deep.CompressJPEGLSLosslessInPlace(); err == nil {
		t.Error("JPEG-LS compressed an image of 32 bits allocated")
	}
}
