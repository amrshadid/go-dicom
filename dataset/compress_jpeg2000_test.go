package dataset_test

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"strconv"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// CompressJPEG2000LosslessInPlace is lossless, so each test compresses,
// decompresses and compares bytes: what comes back is what went in, and the
// data set in between says what it holds.

const syntaxJPEG2000Lossless = "1.2.840.10008.1.2.4.90"

var (
	tagPhotometric = tag.New(0x0028, 0x0004)
	tagPlanar      = tag.New(0x0028, 0x0006)
	tagPixels      = tag.New(0x7FE0, 0x0010)
	tagLossy       = tag.New(0x0028, 0x2110)
)

// nativeImage builds a native image of noise, which nothing lossy returns.
func nativeImage(columns, rows, samples, frames, allocated, stored int, signed bool, photometric string,
	planar int) (*dataset.Dataset, []byte) {
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	us := func(element uint16, v int) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	us(0x0002, samples)
	us(0x0010, rows)
	us(0x0011, columns)
	us(0x0100, allocated)
	us(0x0101, stored)
	us(0x0102, stored-1)
	representation := 0
	if signed {
		representation = 1
	}
	us(0x0103, representation)
	if samples > 1 {
		us(0x0006, planar)
	}
	text := func(t tag.Tag, vr dataelem.VR, v string) {
		if len(v)%2 == 1 {
			v += " "
		}
		_ = ds.Add(dataelem.NewDataElement(t, vr, []byte(v)))
	}
	text(tagPhotometric, dataelem.CS, photometric)
	if frames > 1 {
		text(tag.New(0x0028, 0x0008), dataelem.IS, strconv.Itoa(frames))
	}

	random := rand.New(rand.NewSource(int64(columns*rows + stored)))
	n := columns * rows * samples * frames
	px := make([]byte, 0, n*allocated/8)
	for i := 0; i < n; i++ {
		v := random.Intn(1 << uint(stored))
		if signed {
			v -= 1 << uint(stored-1)
		}
		if allocated == 8 {
			px = append(px, byte(v))
		} else {
			px = binary.LittleEndian.AppendUint16(px, uint16(v))
		}
	}
	vr := dataelem.OB
	if allocated > 8 {
		vr = dataelem.OW
	}
	_ = ds.Add(dataelem.NewDataElement(tagPixels, vr, px))
	return ds, px
}

func textValue(t *testing.T, ds *dataset.Dataset, at tag.Tag) string {
	t.Helper()
	elem, ok := ds.Get(at)
	if !ok {
		return ""
	}
	raw, _ := elem.GetValue().([]byte)
	return string(bytes.TrimRight(raw, " \x00"))
}

// storedPixels is the value of Pixel Data as the element holds it.
func storedPixels(t *testing.T, ds *dataset.Dataset) []byte {
	t.Helper()
	elem, ok := ds.Get(tagPixels)
	if !ok {
		t.Fatal("no Pixel Data")
	}
	raw, _ := elem.GetValue().([]byte)
	return raw
}

// TestCompressJPEG2000IsLossless covers the images DICOM has: 8 and 16 bits
// allocated, 8, 12 and 16 stored, signed and unsigned, one sample and three,
// one frame and several, sizes that are not a multiple of anything.
func TestCompressJPEG2000IsLossless(t *testing.T) {
	cases := []struct {
		name                           string
		columns, rows, samples, frames int
		allocated, stored              int
		signed                         bool
		photometric, compressedAs      string
	}{
		{"8-bit gray", 33, 17, 1, 1, 8, 8, false, "MONOCHROME2", "MONOCHROME2"},
		{"12-bit gray in 16", 64, 64, 1, 1, 16, 12, false, "MONOCHROME2", "MONOCHROME2"},
		{"16-bit gray", 70, 50, 1, 1, 16, 16, false, "MONOCHROME1", "MONOCHROME1"},
		{"16-bit signed gray", 50, 70, 1, 1, 16, 16, true, "MONOCHROME2", "MONOCHROME2"},
		{"12-bit signed gray in 16", 40, 40, 1, 1, 16, 12, true, "MONOCHROME2", "MONOCHROME2"},
		{"three frames of 16-bit gray", 20, 30, 1, 3, 16, 16, false, "MONOCHROME2", "MONOCHROME2"},
		{"8-bit RGB", 31, 29, 3, 1, 8, 8, false, "RGB", "YBR_RCT"},
		{"two frames of 8-bit RGB", 16, 16, 3, 2, 8, 8, false, "RGB", "YBR_RCT"},
		{"16-bit RGB", 20, 20, 3, 1, 16, 16, false, "RGB", "YBR_RCT"},
		{"YBR_FULL, which is not RGB", 16, 16, 3, 1, 8, 8, false, "YBR_FULL", "YBR_FULL"},
		{"palette indexes", 16, 16, 1, 1, 8, 8, false, "PALETTE COLOR", "PALETTE COLOR"},
		{"one pixel", 1, 1, 1, 1, 16, 16, false, "MONOCHROME2", "MONOCHROME2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ds, original := nativeImage(c.columns, c.rows, c.samples, c.frames, c.allocated, c.stored,
				c.signed, c.photometric, 0)
			if err := ds.CompressJPEG2000LosslessInPlace(); err != nil {
				t.Fatalf("compress: %v", err)
			}

			if got := ds.TransferSyntaxUID(); got != syntaxJPEG2000Lossless {
				t.Errorf("the transfer syntax is %s", got)
			}
			if got := textValue(t, ds, tagPhotometric); got != c.compressedAs {
				t.Errorf("Photometric Interpretation is %q, want %q", got, c.compressedAs)
			}
			elem, _ := ds.Get(tagPixels)
			if elem.GetVR() != dataelem.OB {
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
			// A value is of even length, so an image of an odd number of
			// bytes comes back with one of padding after it.
			got := storedPixels(t, ds)
			if len(got) < len(original) || len(got) > len(original)+1 || !bytes.Equal(got[:len(original)], original) {
				t.Errorf("the pixels did not come back as they were")
			}
			if got := textValue(t, ds, tagPhotometric); got != c.photometric {
				t.Errorf("decoded, Photometric Interpretation is %q, was %q", got, c.photometric)
			}
		})
	}
}

// TestCompressJPEG2000FromOtherLayouts checks the two things about an image
// that the coding replaces: samples stored plane by plane come back pixel by
// pixel with Planar Configuration 0, and an image held in another compressed
// syntax is decoded first.
func TestCompressJPEG2000FromOtherLayouts(t *testing.T) {
	// The same RGB image, once by pixel and once by plane.
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

	if err := byPlane.CompressJPEG2000LosslessInPlace(); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if elem, _ := byPlane.Get(tagPlanar); binary.LittleEndian.Uint16(elem.GetValue().([]byte)) != 0 {
		t.Error("Planar Configuration is not 0 under JPEG 2000")
	}
	if err := byPlane.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, byPlane), interleaved) {
		t.Error("an image stored by plane did not come back as the same pixels")
	}

	// Held as JPEG Baseline, which is lossy: what JPEG 2000 must return is
	// what the JPEG decodes to, exactly.
	jpeg, _ := smoothImage(3, 1, "RGB")
	if err := jpeg.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	decoded := jpeg.Clone()
	if err := decoded.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.CompressJPEG2000LosslessInPlace(); err != nil {
		t.Fatalf("from JPEG Baseline: %v", err)
	}
	if got := textValue(t, jpeg, tagPhotometric); got != "YBR_FULL" {
		t.Errorf("from a YBR_FULL JPEG, Photometric Interpretation is %q", got)
	}
	if got := textValue(t, jpeg, tag.New(0x0028, 0x2110)); got != "01" {
		t.Errorf("Lossy Image Compression is %q: the image was lossily compressed before and still is", got)
	}
	if err := jpeg.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, jpeg), storedPixels(t, decoded)) {
		t.Error("from JPEG Baseline: the pixels are not what the JPEG decodes to")
	}
}

// TestCompressJPEG2000KeepsBitsAboveBitsStored checks an image whose samples
// use bits above Bits Stored, as old files with overlays in them do. They
// are not the pixel, and they were in the file: they come back.
func TestCompressJPEG2000KeepsBitsAboveBitsStored(t *testing.T) {
	ds, original := nativeImage(16, 16, 1, 1, 16, 12, false, "MONOCHROME2", 0)
	original[1] |= 0x80 // bit 15 of the first sample
	if err := ds.SetValue(tagPixels, original); err != nil {
		t.Fatal(err)
	}
	if err := ds.CompressJPEG2000LosslessInPlace(); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, ds), original) {
		t.Error("the bits above Bits Stored were lost")
	}
}

// TestCompressJPEG2000LeavesAloneWhatItCannotDo checks that a refusal
// changes nothing, and that compressing twice is compressing once.
func TestCompressJPEG2000LeavesAloneWhatItCannotDo(t *testing.T) {
	empty := dataset.NewDataset()
	if err := empty.CompressJPEG2000LosslessInPlace(); err == nil {
		t.Error("a data set with no Pixel Data was compressed")
	}

	deep, original := nativeImage(4, 4, 1, 1, 16, 16, false, "MONOCHROME2", 0)
	wide := make([]byte, 2)
	binary.LittleEndian.PutUint16(wide, 32)
	if err := deep.SetValue(tag.New(0x0028, 0x0100), wide); err != nil {
		t.Fatal(err)
	}
	if err := deep.CompressJPEG2000LosslessInPlace(); err == nil {
		t.Error("an image of 32 bits allocated was compressed")
	}
	if deep.TransferSyntaxUID() != "1.2.840.10008.1.2.1" || !bytes.Equal(storedPixels(t, deep), original) {
		t.Error("a refusal changed the data set")
	}

	ds, _ := nativeImage(16, 16, 1, 1, 8, 8, false, "MONOCHROME2", 0)
	if err := ds.CompressJPEG2000LosslessInPlace(); err != nil {
		t.Fatal(err)
	}
	once := append([]byte(nil), storedPixels(t, ds)...)
	if err := ds.CompressJPEG2000LosslessInPlace(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedPixels(t, ds), once) {
		t.Error("compressing an image already in this syntax changed it")
	}
}
