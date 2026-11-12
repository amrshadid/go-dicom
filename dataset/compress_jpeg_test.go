package dataset_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"strconv"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The library could not write the compressed syntax most viewers and peers
// take (#243). An encoded image is only usable if the data set around it
// says what it now is.

// smoothImage is a native 8-bit image of 16 by 16 pixels, smooth enough that
// a high quality JPEG is close to it.
func smoothImage(samples, frames int, photometric string) (*dataset.Dataset, []byte) {
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	us := func(element uint16, v int) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	us(0x0002, samples)
	us(0x0010, 16)
	us(0x0011, 16)
	us(0x0100, 8)
	us(0x0101, 8)
	us(0x0102, 7)
	us(0x0103, 0)
	if samples > 1 {
		us(0x0006, 0)
	}
	text := func(t tag.Tag, vr dataelem.VR, v string) {
		if len(v)%2 == 1 {
			v += " "
		}
		_ = ds.Add(dataelem.NewDataElement(t, vr, []byte(v)))
	}
	text(tag.New(0x0028, 0x0004), dataelem.CS, photometric)
	if frames > 1 {
		text(tag.New(0x0028, 0x0008), dataelem.IS, strconv.Itoa(frames))
	}
	px := make([]byte, 0, frames*16*16*samples)
	for f := 0; f < frames; f++ {
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				for c := 0; c < samples; c++ {
					px = append(px, byte(40+x*8+y*3+c*20+f*30))
				}
			}
		}
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, px))
	return ds, px
}

func worst(a, b []byte) int {
	w := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		w = max(w, d)
	}
	return w
}

// TestCompressJPEGBaselineGrayscale: two frames, each its own fragment, with
// a Basic Offset Table that says where each starts, and the attributes a
// lossy image has to carry.
func TestCompressJPEGBaselineGrayscale(t *testing.T) {
	ds, original := smoothImage(1, 2, "MONOCHROME2")
	if err := ds.CompressJPEGBaselineInPlace(95); err != nil {
		t.Fatalf("CompressJPEGBaselineInPlace: %v", err)
	}
	if got := ds.TransferSyntaxUID(); got != "1.2.840.10008.1.2.4.50" {
		t.Errorf("transfer syntax %q, want JPEG Baseline", got)
	}
	for attr, want := range map[tag.Tag]string{
		tag.New(0x0028, 0x0004): "MONOCHROME2",
		tag.New(0x0028, 0x2110): "01",
		tag.New(0x0028, 0x2114): "ISO_10918_1",
	} {
		if got := textAttr(ds, attr); got != want {
			t.Errorf("%s is %q, want %q", attr, got, want)
		}
	}
	if ratio, err := strconv.ParseFloat(textAttr(ds, tag.New(0x0028, 0x2112)), 64); err != nil || ratio <= 0 {
		t.Errorf("Lossy Image Compression Ratio is %q", textAttr(ds, tag.New(0x0028, 0x2112)))
	}

	enc, err := ds.ExtractEncapsulatedFrames()
	if err != nil {
		t.Fatalf("the pixel data is not a valid encapsulation: %v", err)
	}
	if len(enc.Fragments) != 2 || len(enc.BasicOffsetTable) != 2 {
		t.Fatalf("%d fragments and %d table entries for 2 frames", len(enc.Fragments), len(enc.BasicOffsetTable))
	}
	for i, offset := range enc.BasicOffsetTable {
		if uint64(offset) != enc.FragmentOffsets[i] {
			t.Errorf("table entry %d is %d, and the fragment starts at %d", i, offset, enc.FragmentOffsets[i])
		}
	}
	for i, fragment := range enc.Fragments {
		if len(fragment)%2 != 0 {
			t.Errorf("fragment %d is of odd length", i)
		}
	}
	// The value ends with the last fragment. The closing sequence delimiter
	// is the writer's to add, and a second one here made a file dcmtk refused.
	value := pixelBytes(t, ds)
	last := enc.Fragments[len(enc.Fragments)-1]
	if !bytes.HasSuffix(value, last) {
		t.Errorf("the value ends with % x, not with its last fragment", value[len(value)-8:])
	}

	decoded, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding what was encoded: %v", err)
	}
	if w := worst(original, decoded[:len(original)]); w > 4 {
		t.Errorf("at quality 95 a sample decodes %d away from the original", w)
	}
}

// TestCompressJPEGBaselineColor: RGB is coded as YBR_FULL, each component at
// full resolution, and the data set says so, since a decoder takes three
// components for Y, Cb and Cr.
func TestCompressJPEGBaselineColor(t *testing.T) {
	ds, _ := smoothImage(3, 1, "RGB")
	before, err := ds.RenderFrame(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.CompressJPEGBaselineInPlace(95); err != nil {
		t.Fatalf("CompressJPEGBaselineInPlace: %v", err)
	}
	if got := textAttr(ds, tag.New(0x0028, 0x0004)); got != "YBR_FULL" {
		t.Errorf("Photometric Interpretation is %q, want YBR_FULL", got)
	}
	after, err := ds.RenderFrame(0, nil)
	if err != nil {
		t.Fatalf("rendering the compressed image: %v", err)
	}
	if w := worst(before.(*image.NRGBA).Pix, after.(*image.NRGBA).Pix); w > 6 {
		t.Errorf("the compressed image renders %d levels away from the original", w)
	}
}

// TestCompressJPEGBaselineAddsToTheRecord: an image compressed before keeps
// the record of it, and this compression is added to it (PS3.3 C.7.6.1.1.5).
func TestCompressJPEGBaselineAddsToTheRecord(t *testing.T) {
	ds, _ := smoothImage(1, 1, "MONOCHROME2")
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x2112), dataelem.DS, []byte("10.00 ")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x2114), dataelem.CS, []byte("ISO_15444_1 ")))
	if err := ds.CompressJPEGBaselineInPlace(80); err != nil {
		t.Fatal(err)
	}
	if got := textAttr(ds, tag.New(0x0028, 0x2114)); got != `ISO_15444_1\ISO_10918_1` {
		t.Errorf("Lossy Image Compression Method is %q", got)
	}
	if got := textAttr(ds, tag.New(0x0028, 0x2112)); len(got) < 7 || got[:6] != `10.00\` {
		t.Errorf("Lossy Image Compression Ratio is %q", got)
	}
}

// TestCompressJPEGBaselineRefusesWhatBaselineCannotHold: baseline is 8 bits.
// A CT is not, and narrowing it would be a second, silent loss. A palette
// image holds indexes, and averaging indexes is nonsense. A refusal changes
// nothing.
func TestCompressJPEGBaselineRefusesWhatBaselineCannotHold(t *testing.T) {
	deep, _ := imageOf(layout{4, 4, 1, 1, 16, 12, 0, 0, "MONOCHROME2"})
	signed, _ := imageOf(layout{4, 4, 1, 1, 8, 8, 1, 0, "MONOCHROME2"})
	palette, _ := imageOf(layout{4, 4, 1, 1, 8, 8, 0, 0, "PALETTE COLOR"})
	for name, ds := range map[string]*dataset.Dataset{"16-bit": deep, "signed": signed, "palette": palette} {
		before := append([]byte(nil), pixelBytes(t, ds)...)
		if err := ds.CompressJPEGBaselineInPlace(90); err == nil {
			t.Errorf("%s was compressed as JPEG Baseline", name)
		}
		if got := ds.TransferSyntaxUID(); got != "1.2.840.10008.1.2.1" {
			t.Errorf("%s: transfer syntax became %q", name, got)
		}
		if string(pixelBytes(t, ds)) != string(before) {
			t.Errorf("%s: Pixel Data was changed by a refusal", name)
		}
	}

	ok, _ := smoothImage(1, 1, "MONOCHROME2")
	for _, quality := range []int{0, 101} {
		if err := ok.CompressJPEGBaselineInPlace(quality); err == nil {
			t.Errorf("quality %d was accepted", quality)
		}
	}
}
