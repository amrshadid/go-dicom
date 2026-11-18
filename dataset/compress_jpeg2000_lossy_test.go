package dataset_test

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// CompressJPEG2000InPlace is lossy at the rate it is given: these tests check
// the rate, the PSNR and what the data set says. OpenJPEG decoding the same
// codestreams is package jpeg2000's test and the interop check's.

const syntaxJPEG2000 = "1.2.840.10008.1.2.4.91"

// smoothSigned builds a smooth signed 16-bit image of values a CT holds.
func smoothSigned(columns, rows int) (*dataset.Dataset, []byte) {
	ds, px := nativeImage(columns, rows, 1, 1, 16, 16, true, "MONOCHROME2", 0)
	for y := 0; y < rows; y++ {
		for x := 0; x < columns; x++ {
			v := int16((math.Sin(float64(x)/9)+math.Cos(float64(y)/5))*1500 + 500)
			binary.LittleEndian.PutUint16(px[(y*columns+x)*2:], uint16(v))
		}
	}
	_ = ds.SetValue(tagPixels, px)
	return ds, px
}

// signedPSNR is the PSNR of two signed 16-bit buffers over a 16-bit peak.
func signedPSNR(want, got []byte) float64 {
	var sum float64
	n := len(want) / 2
	for i := 0; i+1 < len(want); i += 2 {
		d := float64(int16(binary.LittleEndian.Uint16(want[i:])) - int16(binary.LittleEndian.Uint16(got[i:])))
		sum += d * d
	}
	if sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(65535.0*65535.0/(sum/float64(n)))
}

// TestCompressJPEG2000MeetsTheRate proves a ratio is met and recorded, a PSNR
// is met, a signed image keeps its sign, and the data set is marked lossy
// with the method ISO_15444_1.
func TestCompressJPEG2000MeetsTheRate(t *testing.T) {
	ds, original := smoothSigned(128, 96)
	if err := ds.CompressJPEG2000InPlace(compress.JPEG2000Rate{Ratio: 12}); err != nil {
		t.Fatal(err)
	}
	if got := ds.TransferSyntaxUID(); got != syntaxJPEG2000 {
		t.Errorf("the syntax is %s", got)
	}
	if got := textValue(t, ds, tagLossy); got != "01" {
		t.Errorf("Lossy Image Compression is %q", got)
	}
	if got := textValue(t, ds, tag.New(0x0028, 0x2114)); got != "ISO_15444_1" {
		t.Errorf("the method is %q", got)
	}
	ratio, err := strconv.ParseFloat(textValue(t, ds, tag.New(0x0028, 0x2112)), 64)
	if err != nil || ratio < 11.5 {
		t.Errorf("the recorded ratio is %q, asked for 12", textValue(t, ds, tag.New(0x0028, 0x2112)))
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	got := storedPixels(t, ds)
	if p := signedPSNR(original, got); p < 60 {
		t.Errorf("a smooth CT at 12:1 came back at %.1f dB", p)
	}
	if int16(binary.LittleEndian.Uint16(got)) > 0 && int16(binary.LittleEndian.Uint16(original)) < 0 {
		t.Error("a negative sample came back positive")
	}

	byPSNR, original := smoothFieldJ2K(96, 80, 1, 16, 12, "MONOCHROME2", 0)
	if err := byPSNR.CompressJPEG2000InPlace(compress.JPEG2000Rate{PSNR: 50}); err != nil {
		t.Fatal(err)
	}
	if err := byPSNR.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if p := fieldPSNR(original, storedPixels(t, byPSNR), 16, 12); p < 50 || p > 51.5 {
		t.Errorf("asked for 50 dB, decoded %.2f dB", p)
	}
}

// TestCompressJPEG2000Color proves an RGB image, stored plane by plane, is
// coded with the irreversible component transform and becomes YBR_ICT with
// Planar Configuration 0, and decodes to RGB close to the original.
func TestCompressJPEG2000Color(t *testing.T) {
	ds, interleaved := smoothFieldJ2K(48, 40, 3, 8, 8, "RGB", 0)
	planes := make([]byte, len(interleaved))
	for i := 0; i < 48*40; i++ {
		for c := 0; c < 3; c++ {
			planes[c*48*40+i] = interleaved[i*3+c]
		}
	}
	_ = ds.SetValue(tagPixels, planes)
	planar := make([]byte, 2)
	binary.LittleEndian.PutUint16(planar, 1)
	_ = ds.SetValue(tagPlanar, planar)
	if err := ds.CompressJPEG2000InPlace(compress.JPEG2000Rate{PSNR: 42}); err != nil {
		t.Fatal(err)
	}
	if got := textValue(t, ds, tagPhotometric); got != "YBR_ICT" {
		t.Errorf("Photometric Interpretation is %q, want YBR_ICT", got)
	}
	if elem, _ := ds.Get(tagPlanar); binary.LittleEndian.Uint16(elem.GetValue().([]byte)) != 0 {
		t.Error("Planar Configuration is not 0")
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	if got := textValue(t, ds, tagPhotometric); got != "RGB" {
		t.Errorf("decoded, Photometric Interpretation is %q", got)
	}
	if p := fieldPSNR(interleaved, storedPixels(t, ds), 8, 8); p < 42 {
		t.Errorf("asked for 42 dB, decoded %.1f dB", p)
	}
}

// TestCompressJPEG2000Refuses proves no rate, two rates, palette color, 32
// bits allocated, and a ratio a small image cannot meet are refused with the
// data set unchanged.
func TestCompressJPEG2000Refuses(t *testing.T) {
	ds, _ := smoothFieldJ2K(64, 64, 1, 8, 8, "MONOCHROME2", 0)
	for _, rate := range []compress.JPEG2000Rate{{}, {Ratio: 10, PSNR: 40}, {Ratio: -2}} {
		if err := ds.CompressJPEG2000InPlace(rate); err == nil {
			t.Errorf("%+v was accepted", rate)
		}
	}
	palette, _ := smoothFieldJ2K(16, 16, 1, 8, 8, "PALETTE COLOR", 0)
	if err := palette.CompressJPEG2000InPlace(compress.JPEG2000Rate{Ratio: 5}); err == nil ||
		!strings.Contains(err.Error(), "PALETTE") {
		t.Errorf("palette color: err = %v", err)
	}
	tiny, _ := smoothFieldJ2K(6, 6, 1, 8, 8, "MONOCHROME2", 0)
	if err := tiny.CompressJPEG2000InPlace(compress.JPEG2000Rate{Ratio: 40}); err == nil ||
		!strings.Contains(err.Error(), "cannot be met") {
		t.Errorf("40:1 of a 6x6 image: err = %v", err)
	}
	for _, d := range []*dataset.Dataset{ds, palette, tiny} {
		if d.TransferSyntaxUID() != "1.2.840.10008.1.2.1" {
			t.Error("a refusal changed the data set")
		}
	}
}

// smoothFieldJ2K builds a native unsigned image of smooth content at stored
// bits in allocated bits.
func smoothFieldJ2K(columns, rows, samples, allocated, stored int, photometric string,
	planar int) (*dataset.Dataset, []byte) {
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

// fieldPSNR is the PSNR of got against want, samples of allocated bits whose
// peak is 2^stored - 1.
func fieldPSNR(want, got []byte, allocated, stored int) float64 {
	var sum float64
	step := allocated / 8
	for i := 0; i+step <= len(want); i += step {
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
	return 10 * math.Log10(peak*peak/(sum/float64(len(want)/step)))
}
