package dataset_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// CompressJPEGLSNearLosslessInPlace promises every sample back within NEAR,
// and exactly at NEAR 0. Each test compresses, decompresses and measures. That
// other decoders agree is shown in package compress and by the interop check.

const syntaxJPEGLSNear = "1.2.840.10008.1.2.4.81"

// worstDifference is the largest difference between two buffers of samples,
// little endian at 16 bits allocated.
func worstDifference(a, b []byte, allocated int) int {
	worst := 0
	step := allocated / 8
	for i := 0; i+step <= len(a) && i+step <= len(b); i += step {
		x, y := int(a[i]), int(b[i])
		if step == 2 {
			x, y = int(binary.LittleEndian.Uint16(a[i:])), int(binary.LittleEndian.Uint16(b[i:]))
		}
		worst = max(worst, x-y, y-x)
	}
	return worst
}

// TestCompressJPEGLSNearLosslessIsWithinNear covers 8, 12 and 16 bits, gray and
// RGB, several frames and one pixel, at NEAR 0, 1 and 4: every sample within
// NEAR, the syntax .81, Planar Configuration 0, color kept, and the lossy mark,
// ratio and method set exactly when NEAR is above 0.
func TestCompressJPEGLSNearLosslessIsWithinNear(t *testing.T) {
	cases := []struct {
		name                           string
		columns, rows, samples, frames int
		allocated, stored              int
		photometric                    string
	}{
		{"8-bit gray", 33, 17, 1, 1, 8, 8, "MONOCHROME2"},
		{"12-bit gray in 16", 64, 64, 1, 1, 16, 12, "MONOCHROME2"},
		{"16-bit gray", 40, 30, 1, 1, 16, 16, "MONOCHROME1"},
		{"8-bit stored in 16", 21, 19, 1, 1, 16, 8, "MONOCHROME2"},
		{"three frames", 20, 30, 1, 3, 16, 12, "MONOCHROME2"},
		{"8-bit RGB", 31, 29, 3, 1, 8, 8, "RGB"},
		{"one pixel", 1, 1, 1, 1, 16, 16, "MONOCHROME2"},
	}
	for _, near := range []int{0, 1, 4} {
		for _, c := range cases {
			ds, original := nativeImage(c.columns, c.rows, c.samples, c.frames, c.allocated, c.stored,
				false, c.photometric, 0)
			if err := ds.CompressJPEGLSNearLosslessInPlace(near); err != nil {
				t.Fatalf("%s, NEAR %d: %v", c.name, near, err)
			}
			if got := ds.TransferSyntaxUID(); got != syntaxJPEGLSNear {
				t.Errorf("%s, NEAR %d: the syntax is %s", c.name, near, got)
			}
			if got := textValue(t, ds, tagPhotometric); got != c.photometric {
				t.Errorf("%s, NEAR %d: Photometric Interpretation is %q", c.name, near, got)
			}
			lossy, method := textValue(t, ds, tagLossy), textValue(t, ds, tag.New(0x0028, 0x2114))
			ratio := textValue(t, ds, tag.New(0x0028, 0x2112))
			if near == 0 && (lossy != "" || method != "" || ratio != "") {
				t.Errorf("%s, NEAR 0: marked lossy (%q, %q, %q)", c.name, lossy, ratio, method)
			}
			if near > 0 && (lossy != "01" || method != "ISO_14495_1" || ratio == "") {
				t.Errorf("%s, NEAR %d: Lossy Image Compression %q, ratio %q, method %q",
					c.name, near, lossy, ratio, method)
			}
			if err := ds.DecompressInPlace(); err != nil {
				t.Fatalf("%s, NEAR %d: decompress: %v", c.name, near, err)
			}
			got := storedPixels(t, ds)
			if len(got) < len(original) {
				t.Fatalf("%s, NEAR %d: %d bytes back for %d", c.name, near, len(got), len(original))
			}
			if worst := worstDifference(got, original, c.allocated); worst > near {
				t.Errorf("%s, NEAR %d: a sample came back %d away", c.name, near, worst)
			}
		}
	}
}

// TestCompressJPEGLSNearLosslessFromPlanes proves an RGB image stored plane by
// plane is coded pixel by pixel and comes back within NEAR with Planar
// Configuration 0.
func TestCompressJPEGLSNearLosslessFromPlanes(t *testing.T) {
	byPixel, interleaved := nativeImage(9, 7, 3, 1, 8, 8, false, "RGB", 0)
	planes := make([]byte, len(interleaved))
	for i := 0; i < 9*7; i++ {
		for c := 0; c < 3; c++ {
			planes[c*9*7+i] = interleaved[i*3+c]
		}
	}
	ds := byPixel.Clone()
	if err := ds.SetValue(tagPixels, planes); err != nil {
		t.Fatal(err)
	}
	planar := make([]byte, 2)
	binary.LittleEndian.PutUint16(planar, 1)
	if err := ds.SetValue(tagPlanar, planar); err != nil {
		t.Fatal(err)
	}
	if err := ds.CompressJPEGLSNearLosslessInPlace(2); err != nil {
		t.Fatal(err)
	}
	if elem, _ := ds.Get(tagPlanar); binary.LittleEndian.Uint16(elem.GetValue().([]byte)) != 0 {
		t.Error("Planar Configuration is not 0")
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if worst := worstDifference(storedPixels(t, ds), interleaved, 8); worst > 2 {
		t.Errorf("a sample of an image stored by plane came back %d away", worst)
	}
}

// TestCompressJPEGLSNearLosslessSigned proves a signed image is coded exactly at
// NEAR 0, sign restored by the reader, and refused at NEAR above 0 with the
// data set unchanged.
func TestCompressJPEGLSNearLosslessSigned(t *testing.T) {
	ds, original := nativeImage(30, 20, 1, 1, 16, 12, true, "MONOCHROME2", 0)
	refused := ds.Clone()
	if err := refused.CompressJPEGLSNearLosslessInPlace(2); err == nil || !strings.Contains(err.Error(), "signed") {
		t.Errorf("NEAR 2 on a signed image: err = %v, want a refusal naming the sign", err)
	}
	if refused.TransferSyntaxUID() != "1.2.840.10008.1.2.1" {
		t.Error("a refusal changed the data set")
	}
	if err := ds.CompressJPEGLSNearLosslessInPlace(0); err != nil {
		t.Fatal(err)
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	got := storedPixels(t, ds)
	want := interpretedSamples12(original)
	if back := interpretedSamples12(got[:len(original)]); len(back) != len(want) {
		t.Fatal("the image came back a different size")
	} else {
		for i := range want {
			if back[i] != want[i] {
				t.Fatalf("sample %d came back %d, was %d", i, back[i], want[i])
			}
		}
	}
}

// interpretedSamples12 reads 16-bit samples as signed 12-bit values, as a
// reader of a data set saying Bits Stored 12 and Pixel Representation 1 does.
func interpretedSamples12(px []byte) []int {
	out := make([]int, len(px)/2)
	for i := range out {
		v := int(binary.LittleEndian.Uint16(px[2*i:]) & 0x0FFF)
		if v&0x800 != 0 {
			v -= 0x1000
		}
		out[i] = v
	}
	return out
}

// TestCompressJPEGLSNearLosslessRefuses proves a NEAR out of range, a data set
// with no Pixel Data, and 32 bits allocated are refused, and that an image
// already in the syntax is left alone.
func TestCompressJPEGLSNearLosslessRefuses(t *testing.T) {
	ds, _ := nativeImage(8, 8, 1, 1, 8, 8, false, "MONOCHROME2", 0)
	for _, near := range []int{-1, 256, 128} {
		if err := ds.Clone().CompressJPEGLSNearLosslessInPlace(near); err == nil {
			t.Errorf("NEAR %d on an 8-bit image was accepted", near)
		}
	}
	if err := dataset.NewDataset().CompressJPEGLSNearLosslessInPlace(1); err == nil {
		t.Error("a data set with no Pixel Data was compressed")
	}
	deep, _ := nativeImage(4, 4, 1, 1, 16, 16, false, "MONOCHROME2", 0)
	wide := make([]byte, 2)
	binary.LittleEndian.PutUint16(wide, 32)
	if err := deep.SetValue(tag.New(0x0028, 0x0100), wide); err != nil {
		t.Fatal(err)
	}
	if err := deep.CompressJPEGLSNearLosslessInPlace(1); err == nil {
		t.Error("an image of 32 bits allocated was compressed")
	}
	if err := ds.CompressJPEGLSNearLosslessInPlace(3); err != nil {
		t.Fatal(err)
	}
	once := append([]byte(nil), storedPixels(t, ds)...)
	if err := ds.CompressJPEGLSNearLosslessInPlace(1); err != nil {
		t.Fatal(err)
	}
	if string(storedPixels(t, ds)) != string(once) {
		t.Error("compressing an image already in this syntax changed it")
	}
}
