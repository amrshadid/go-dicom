package dataset_test

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// CompressJPEGExtendedInPlace is lossy: these tests bound the loss and check
// what the data set says. Other decoders are package compress's and the
// interop check's.

const syntaxJPEGExtended = "1.2.840.10008.1.2.4.51"

// smoothNative builds a native image of smooth content at stored bits,
// unsigned, in allocated bits.
func smoothNative(columns, rows, samples, allocated, stored int, photometric string, planar int) (*dataset.Dataset, []byte) {
	ds, px := nativeImage(columns, rows, samples, 1, allocated, stored, false, photometric, planar)
	maxVal := float64(int(1)<<uint(stored) - 1)
	at := 0
	for y := 0; y < rows; y++ {
		for x := 0; x < columns; x++ {
			for c := 0; c < samples; c++ {
				v := int((math.Sin(float64(x)/8+float64(c))+math.Cos(float64(y)/6)+2)/4*maxVal + 0.5)
				if allocated == 8 {
					px[at] = byte(v)
					at++
				} else {
					binary.LittleEndian.PutUint16(px[at:], uint16(v))
					at += 2
				}
			}
		}
	}
	_ = ds.SetValue(tagPixels, px)
	return ds, px
}

// psnrOf is the PSNR of got against want, samples of allocated bits whose
// peak is 2^stored - 1.
func psnrOf(want, got []byte, allocated, stored int) float64 {
	var sum float64
	step := allocated / 8
	n := len(want) / step
	for i := 0; i < len(want); i += step {
		x, y := float64(want[i]), float64(got[i])
		if step == 2 {
			x, y = float64(binary.LittleEndian.Uint16(want[i:])), float64(binary.LittleEndian.Uint16(got[i:]))
		}
		sum += (x - y) * (x - y)
	}
	if sum == 0 {
		return math.Inf(1)
	}
	peak := float64(int(1)<<uint(stored) - 1)
	return 10 * math.Log10(peak*peak/(sum/float64(n)))
}

// TestCompressJPEGExtendedBoundsTheLoss covers a 12-bit image in 16, an 8-bit
// one, an odd size and one pixel: the syntax, the lossy marks with the method
// ISO_10918_1, Bits Stored as the codestream's, and every image back above
// 40 dB at quality 90.
func TestCompressJPEGExtendedBoundsTheLoss(t *testing.T) {
	cases := []struct {
		name                       string
		columns, rows              int
		allocated, stored, codedAt int
	}{
		{"12 of 16", 64, 64, 16, 12, 12},
		{"10 of 16, odd size", 37, 23, 16, 10, 12},
		{"8-bit", 40, 30, 8, 8, 8},
		{"one pixel", 1, 1, 16, 12, 12},
	}
	for _, c := range cases {
		ds, original := smoothNative(c.columns, c.rows, 1, c.allocated, c.stored, "MONOCHROME2", 0)
		if err := ds.CompressJPEGExtendedInPlace(90); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := ds.TransferSyntaxUID(); got != syntaxJPEGExtended {
			t.Errorf("%s: the syntax is %s", c.name, got)
		}
		if got := textValue(t, ds, tagLossy); got != "01" {
			t.Errorf("%s: Lossy Image Compression is %q", c.name, got)
		}
		if got := textValue(t, ds, tag.New(0x0028, 0x2114)); got != "ISO_10918_1" {
			t.Errorf("%s: the method is %q", c.name, got)
		}
		if elem, _ := ds.Get(tag.New(0x0028, 0x0101)); int(binary.LittleEndian.Uint16(elem.GetValue().([]byte))) != c.codedAt {
			t.Errorf("%s: Bits Stored is not the codestream's %d", c.name, c.codedAt)
		}
		if err := ds.DecompressInPlace(); err != nil {
			t.Fatalf("%s: decompress: %v", c.name, err)
		}
		got := storedPixels(t, ds)
		if len(got) < len(original) {
			t.Fatalf("%s: %d bytes back for %d", c.name, len(got), len(original))
		}
		if p := psnrOf(original, got, c.allocated, c.stored); p < 40 {
			t.Errorf("%s: PSNR %.1f dB at quality 90", c.name, p)
		}
	}
}

// TestCompressJPEGExtendedColor proves an RGB image, here stored plane by
// plane, is coded as YBR_FULL with Planar Configuration 0 and comes back, as
// YBR, close to the YBR of the original.
func TestCompressJPEGExtendedColor(t *testing.T) {
	ds, interleaved := smoothNative(24, 16, 3, 8, 8, "RGB", 0)
	planes := make([]byte, len(interleaved))
	for i := 0; i < 24*16; i++ {
		for c := 0; c < 3; c++ {
			planes[c*24*16+i] = interleaved[i*3+c]
		}
	}
	_ = ds.SetValue(tagPixels, planes)
	planar := make([]byte, 2)
	binary.LittleEndian.PutUint16(planar, 1)
	_ = ds.SetValue(tagPlanar, planar)
	if err := ds.CompressJPEGExtendedInPlace(95); err != nil {
		t.Fatal(err)
	}
	if got := textValue(t, ds, tagPhotometric); got != "YBR_FULL" {
		t.Errorf("Photometric Interpretation is %q", got)
	}
	if elem, _ := ds.Get(tagPlanar); binary.LittleEndian.Uint16(elem.GetValue().([]byte)) != 0 {
		t.Error("Planar Configuration is not 0")
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	got := storedPixels(t, ds)
	// The YBR the encoder was given, by PS3.3 C.7.6.3.1.2.
	want := make([]byte, len(interleaved))
	for i := 0; i+2 < len(interleaved); i += 3 {
		r, g, b := float64(interleaved[i]), float64(interleaved[i+1]), float64(interleaved[i+2])
		want[i] = byte(math.Round(0.299*r + 0.587*g + 0.114*b))
		want[i+1] = byte(math.Round(-0.168736*r - 0.331264*g + 0.5*b + 128))
		want[i+2] = byte(math.Round(0.5*r - 0.418688*g - 0.081312*b + 128))
	}
	if p := psnrOf(want, got[:len(want)], 8, 8); p < 35 {
		t.Errorf("PSNR %.1f dB against the YBR of the original", p)
	}
}

// TestCompressJPEGExtendedRefuses proves what it cannot code is refused by
// name with the data set unchanged: a signed image, 16 bits stored, 12-bit
// color, palette color, a sample above Bits Stored, and quality 0.
func TestCompressJPEGExtendedRefuses(t *testing.T) {
	signed, _ := nativeImage(8, 8, 1, 1, 16, 12, true, "MONOCHROME2", 0)
	deep, _ := smoothNative(8, 8, 1, 16, 16, "MONOCHROME2", 0)
	color12, _ := smoothNative(8, 8, 3, 16, 12, "RGB", 0)
	palette, _ := smoothNative(8, 8, 1, 8, 8, "PALETTE COLOR", 0)
	over, px := smoothNative(8, 8, 1, 16, 12, "MONOCHROME2", 0)
	binary.LittleEndian.PutUint16(px, 0x1000)
	_ = over.SetValue(tagPixels, px)
	for name, c := range map[string]struct {
		ds   *dataset.Dataset
		want string
	}{
		"signed":          {signed, "signed"},
		"16 bits stored":  {deep, "16 stored"},
		"12-bit color":    {color12, "RGB"},
		"palette":         {palette, "PALETTE"},
		"above the store": {over, "above"},
	} {
		err := c.ds.CompressJPEGExtendedInPlace(90)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one naming %q", name, err, c.want)
		}
		if c.ds.TransferSyntaxUID() != "1.2.840.10008.1.2.1" {
			t.Errorf("%s: a refusal changed the data set", name)
		}
	}
	if err := deep.CompressJPEGExtendedInPlace(0); err == nil {
		t.Error("quality 0 was accepted")
	}
	if elem, _ := signed.Get(tagPixels); elem.GetVR() != dataelem.OW {
		t.Error("a refusal changed the Pixel Data's VR")
	}
}
