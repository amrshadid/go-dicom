package dataset_test

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// De-identification stopped at the attributes: text drawn into the image was
// out of the library's reach (#233). These build images whose every sample is
// distinct and non-zero, so a fill in the wrong place, or of the wrong width,
// shows.

// layout describes a synthetic native image.
type layout struct {
	rows, cols, frames, samples int
	allocated, stored           int
	representation, planar      uint16
	photometric                 string
}

// imageOf builds the image and returns it with its Pixel Data bytes.
func imageOf(l layout) (*dataset.Dataset, []byte) {
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	addUS := func(element, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	addUS(0x0002, uint16(l.samples))
	addUS(0x0010, uint16(l.rows))
	addUS(0x0011, uint16(l.cols))
	addUS(0x0100, uint16(l.allocated))
	addUS(0x0101, uint16(l.stored))
	addUS(0x0102, uint16(l.stored-1))
	addUS(0x0103, l.representation)
	if l.samples > 1 {
		addUS(0x0006, l.planar)
	}
	if l.frames > 1 {
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, []byte(evenText(strconv.Itoa(l.frames)))))
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0004), dataelem.CS, []byte(evenText(l.photometric))))

	width := l.allocated / 8
	px := make([]byte, l.frames*l.rows*l.cols*l.samples*width)
	for i := range px {
		px[i] = byte(i%200) + 17 // never zero, and not the same in neighbors
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, px))
	return ds, append([]byte(nil), px...)
}

func evenText(s string) string {
	if len(s)%2 == 1 {
		return s + " "
	}
	return s
}

func pixelBytes(t *testing.T, ds *dataset.Dataset) []byte {
	t.Helper()
	elem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("Pixel Data is gone")
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok {
		t.Fatalf("Pixel Data holds %T", elem.GetValue())
	}
	return raw
}

// sampleOffset is where sample s of pixel (x, y) of a frame sits.
func sampleOffset(l layout, frame, x, y, s int) int {
	width := l.allocated / 8
	frameSize := l.rows * l.cols * l.samples * width
	if l.samples > 1 && l.planar == 1 {
		return frame*frameSize + s*l.rows*l.cols*width + (y*l.cols+x)*width
	}
	return frame*frameSize + ((y*l.cols+x)*l.samples+s)*width
}

// TestRedactPixelRegions: inside the region every sample is the fill, in
// every frame, and outside it not one byte moved.
func TestRedactPixelRegions(t *testing.T) {
	region := dataset.PixelRegion{X: 1, Y: 2, Width: 3, Height: 2}
	for name, c := range map[string]struct {
		l    layout
		fill []uint32 // per sample
	}{
		"8-bit grayscale":        {layout{6, 5, 1, 1, 8, 8, 0, 0, "MONOCHROME2"}, []uint32{0}},
		"16-bit grayscale":       {layout{6, 5, 1, 1, 16, 12, 0, 0, "MONOCHROME2"}, []uint32{0}},
		"16-bit signed":          {layout{6, 5, 1, 1, 16, 16, 1, 0, "MONOCHROME2"}, []uint32{0x8000}},
		"12-bit signed":          {layout{6, 5, 1, 1, 16, 12, 1, 0, "MONOCHROME2"}, []uint32{0xF800}},
		"MONOCHROME1 is max":     {layout{6, 5, 1, 1, 16, 12, 0, 0, "MONOCHROME1"}, []uint32{0x0FFF}},
		"32-bit grayscale":       {layout{6, 5, 1, 1, 32, 32, 0, 0, "MONOCHROME2"}, []uint32{0}},
		"multi-frame":            {layout{6, 5, 3, 1, 16, 16, 0, 0, "MONOCHROME2"}, []uint32{0}},
		"RGB by pixel":           {layout{6, 5, 1, 3, 8, 8, 0, 0, "RGB"}, []uint32{0, 0, 0}},
		"RGB by plane":           {layout{6, 5, 2, 3, 8, 8, 0, 1, "RGB"}, []uint32{0, 0, 0}},
		"YBR black is not zeros": {layout{6, 5, 1, 3, 8, 8, 0, 0, "YBR_FULL"}, []uint32{0, 128, 128}},
		"palette index":          {layout{6, 5, 1, 1, 8, 8, 0, 0, "PALETTE COLOR"}, []uint32{0}},
	} {
		t.Run(name, func(t *testing.T) {
			ds, before := imageOf(c.l)
			if err := ds.RedactPixelRegions(region); err != nil {
				t.Fatalf("RedactPixelRegions: %v", err)
			}
			after := pixelBytes(t, ds)
			if len(after) != len(before) {
				t.Fatalf("Pixel Data is %d bytes, was %d", len(after), len(before))
			}

			width := c.l.allocated / 8
			inside := map[int]bool{}
			for frame := 0; frame < c.l.frames; frame++ {
				for y := region.Y; y < region.Y+region.Height; y++ {
					for x := region.X; x < region.X+region.Width; x++ {
						for s := 0; s < c.l.samples; s++ {
							off := sampleOffset(c.l, frame, x, y, s)
							var got uint32
							for b := 0; b < width; b++ {
								got |= uint32(after[off+b]) << (8 * b)
								inside[off+b] = true
							}
							if got != c.fill[s] {
								t.Fatalf("frame %d (%d,%d) sample %d is %#x, want %#x", frame, x, y, s, got, c.fill[s])
							}
						}
					}
				}
			}
			for i := range after {
				if !inside[i] && after[i] != before[i] {
					t.Fatalf("byte %d, outside the region, changed from %#x to %#x", i, before[i], after[i])
				}
			}
		})
	}
}

// TestRedactOneFrame leaves the other frames alone.
func TestRedactOneFrame(t *testing.T) {
	l := layout{4, 4, 3, 1, 8, 8, 0, 0, "MONOCHROME2"}
	ds, before := imageOf(l)
	if err := ds.RedactFrameRegions(1, dataset.PixelRegion{X: 0, Y: 0, Width: 4, Height: 4}); err != nil {
		t.Fatalf("RedactFrameRegions: %v", err)
	}
	after := pixelBytes(t, ds)
	if !bytes.Equal(after[:16], before[:16]) || !bytes.Equal(after[32:], before[32:]) {
		t.Error("a frame other than the one named was changed")
	}
	if !bytes.Equal(after[16:32], make([]byte, 16)) {
		t.Error("the named frame was not filled")
	}
	if err := ds.RedactFrameRegions(3, dataset.PixelRegion{Width: 1, Height: 1}); err == nil {
		t.Error("frame 3 of 3, counting from 0, was accepted")
	}
}

// TestRedactClipsToTheImage: a region reaching past an edge covers what is
// there, since the caller's box need not be exact. One that covers nothing is
// an error: the caller believes something was removed, and nothing was.
func TestRedactClipsToTheImage(t *testing.T) {
	l := layout{4, 4, 1, 1, 8, 8, 0, 0, "MONOCHROME2"}
	ds, before := imageOf(l)
	if err := ds.RedactPixelRegions(dataset.PixelRegion{X: 2, Y: 3, Width: 100, Height: 100}); err != nil {
		t.Fatalf("a region reaching past the edge: %v", err)
	}
	after := pixelBytes(t, ds)
	if after[3*4+2] != 0 || after[3*4+3] != 0 || !bytes.Equal(after[:14], before[:14]) {
		t.Errorf("the clipped region filled %v", after)
	}

	for name, region := range map[string]dataset.PixelRegion{
		"outside":     {X: 4, Y: 0, Width: 2, Height: 2},
		"no width":    {X: 0, Y: 0, Width: 0, Height: 2},
		"negative":    {X: 0, Y: 0, Width: -1, Height: 2},
		"left of it":  {X: -5, Y: 0, Width: 5, Height: 2},
		"above, wide": {X: 0, Y: -3, Width: 4, Height: 3},
	} {
		fresh, _ := imageOf(l)
		if err := fresh.RedactPixelRegions(region); err == nil {
			t.Errorf("%s: a region covering no pixel was accepted", name)
		}
	}
}

// TestRedactRefusesWhatItCannotAddress: compressed frames are not samples,
// and a layout this does not know would be filled in the wrong place.
func TestRedactRefusesWhatItCannotAddress(t *testing.T) {
	region := dataset.PixelRegion{Width: 1, Height: 1}

	compressed, _ := imageOf(layout{4, 4, 1, 1, 8, 8, 0, 0, "MONOCHROME2"})
	compressed.SetTransferSyntaxUID("1.2.840.10008.1.2.4.90")
	if err := compressed.RedactPixelRegions(region); err == nil {
		t.Error("encapsulated pixel data was redacted as if it were samples")
	}

	subsampled, _ := imageOf(layout{4, 4, 1, 3, 8, 8, 0, 0, "YBR_FULL_422"})
	if err := subsampled.RedactPixelRegions(region); err == nil {
		t.Error("subsampled chroma was addressed as full samples")
	}

	short, _ := imageOf(layout{4, 4, 1, 1, 8, 8, 0, 0, "MONOCHROME2"})
	_ = short.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, make([]byte, 7)))
	if err := short.RedactPixelRegions(region); err == nil {
		t.Error("pixel data shorter than the image was accepted")
	}

	none := dataset.NewDataset()
	if err := none.RedactPixelRegions(region); err == nil {
		t.Error("a data set with no image was accepted")
	}
}

// TestRedactPaletteWithItsDarkestEntry: a palette image holds indexes, and
// index 0 is whatever color the palette gives it. Filling with it left a
// redacted region white in a palette that starts at white (#242).
func TestRedactPaletteWithItsDarkestEntry(t *testing.T) {
	l := layout{2, 2, 1, 1, 8, 8, 0, 0, "PALETTE COLOR"}
	region := dataset.PixelRegion{Width: 2, Height: 1}
	lut := func(values ...uint16) []byte {
		out := make([]byte, 2*len(values))
		for i, v := range values {
			binary.LittleEndian.PutUint16(out[2*i:], v)
		}
		return out
	}

	ds, _ := imageOf(l)
	// Three entries: white, mid-gray, black. The darkest is index 2.
	for c := uint16(0); c < 3; c++ {
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1101+c), dataelem.US, lut(3, 0, 16)))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1201+c), dataelem.OW, lut(0xFFFF, 0x8000, 0x0000)))
	}
	if err := ds.RedactPixelRegions(region); err != nil {
		t.Fatalf("RedactPixelRegions: %v", err)
	}
	if got := pixelBytes(t, ds)[:2]; got[0] != 2 || got[1] != 2 {
		t.Errorf("the region holds indexes %v, want the palette's black, 2", got)
	}

	// With no table to consult there is nothing better than index 0.
	bare, _ := imageOf(l)
	if err := bare.RedactPixelRegions(region); err != nil {
		t.Fatalf("RedactPixelRegions with no palette: %v", err)
	}
	if got := pixelBytes(t, bare)[:2]; got[0] != 0 || got[1] != 0 {
		t.Errorf("the region holds %v, want 0 when the palette cannot be read", got)
	}
}
