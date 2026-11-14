package dataset_test

import (
	"encoding/binary"
	"image"
	"image/color"
	"strconv"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The library decoded every image it read and could not show one: nothing
// turned stored values into what a viewer displays (#234). The expected
// values here are worked by hand from PS3.3 C.11.2.1.2.

// picture describes a one-row native image.
type picture struct {
	allocated, stored int
	signed            bool
	samples           int
	planar            uint16
	photometric       string
	frames            int
	px                []byte
	text              map[tag.Tag]string // DS and CS attributes
}

func (p picture) dataset() *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	us := func(element uint16, v int) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	if p.samples == 0 {
		p.samples = 1
	}
	if p.frames == 0 {
		p.frames = 1
	}
	cols := len(p.px) / (p.frames * p.samples * p.allocated / 8)
	us(0x0002, p.samples)
	us(0x0010, 1)
	us(0x0011, cols)
	us(0x0100, p.allocated)
	us(0x0101, p.stored)
	us(0x0102, p.stored-1)
	rep := 0
	if p.signed {
		rep = 1
	}
	us(0x0103, rep)
	if p.samples > 1 {
		us(0x0006, int(p.planar))
	}
	set := func(t tag.Tag, vr dataelem.VR, v string) {
		if len(v)%2 == 1 {
			v += " "
		}
		_ = ds.Add(dataelem.NewDataElement(t, vr, []byte(v)))
	}
	set(tag.New(0x0028, 0x0004), dataelem.CS, p.photometric)
	if p.frames > 1 {
		set(tag.New(0x0028, 0x0008), dataelem.IS, strconv.Itoa(p.frames))
	}
	for t, v := range p.text {
		vr := dataelem.DS
		if t == tag.New(0x0028, 0x1056) {
			vr = dataelem.CS
		}
		set(t, vr, v)
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, p.px))
	return ds
}

func words(values ...int) []byte {
	out := make([]byte, 2*len(values))
	for i, v := range values {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}

var (
	tagWindowCenter = tag.New(0x0028, 0x1050)
	tagWindowWidth  = tag.New(0x0028, 0x1051)
	tagIntercept    = tag.New(0x0028, 0x1052)
	tagSlope        = tag.New(0x0028, 0x1053)
	tagVOIFunction  = tag.New(0x0028, 0x1056)
)

// grays renders frame 0 and returns its row as gray levels.
func grays(t *testing.T, ds *dataset.Dataset, opts *dataset.RenderOptions) []uint8 {
	t.Helper()
	img, err := ds.RenderFrame(0, opts)
	if err != nil {
		t.Fatalf("RenderFrame: %v", err)
	}
	gray, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("a grayscale image rendered as %T", img)
	}
	return gray.Pix
}

func sameLevels(a, b []uint8) bool {
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

// TestRenderRescalesThenWindows: the window is given in output units, so the
// Rescale Slope and Intercept come first. With intercept -1024 and a 40/400
// window, the stored values below are -200, -160, 40, 239 and 300 HU. The
// window's lower edge is 39.5 - 199.5 = -160 and its upper 239.
func TestRenderRescalesThenWindows(t *testing.T) {
	ds := picture{allocated: 16, stored: 16, photometric: "MONOCHROME2",
		px: words(824, 864, 1064, 1263, 1324),
		text: map[tag.Tag]string{
			tagIntercept: "-1024", tagSlope: "1",
			// The first of several windows, and decimals: both are ordinary.
			tagWindowCenter: `40.0\50`, tagWindowWidth: `400.0\350`,
		}}.dataset()
	if got, want := grays(t, ds, nil), []uint8{0, 0, 128, 255, 255}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}
}

// TestRenderReadsSignedSamples: -1000 stored in two's complement is below a
// 0/2000 window. Read as unsigned it is 64536, above it.
func TestRenderReadsSignedSamples(t *testing.T) {
	ds := picture{allocated: 16, stored: 16, signed: true, photometric: "MONOCHROME2",
		px:   words(-1000, 0, 1000),
		text: map[tag.Tag]string{tagWindowCenter: "0", tagWindowWidth: "2000"}}.dataset()
	if got, want := grays(t, ds, nil), []uint8{0, 128, 255}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}

	// 12 bits stored in 16: the sign is bit 11, and the bits above it are not
	// part of the number. 0x0C18 is -1000.
	narrow := picture{allocated: 16, stored: 12, signed: true, photometric: "MONOCHROME2",
		px:   words(0x0C18, 0xF000, 0x03E8),
		text: map[tag.Tag]string{tagWindowCenter: "0", tagWindowWidth: "2000"}}.dataset()
	if got, want := grays(t, narrow, nil), []uint8{0, 128, 255}; !sameLevels(got, want) {
		t.Errorf("12-bit levels %v, want %v", got, want)
	}
}

// TestRenderWithoutAWindowUsesTheDataRange: a file with no window still has
// to show something, and a guessed window shows most images as all black or
// all white.
func TestRenderWithoutAWindowUsesTheDataRange(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", px: []byte{10, 20, 30}}.dataset()
	if got, want := grays(t, ds, nil), []uint8{0, 128, 255}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}

	flat := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", px: []byte{7, 7}}.dataset()
	if got, want := grays(t, flat, nil), []uint8{0, 0}; !sameLevels(got, want) {
		t.Errorf("a flat image rendered as %v", got)
	}
}

// TestRenderInvertsMonochrome1: high values are dark there.
func TestRenderInvertsMonochrome1(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME1", px: []byte{10, 20, 30}}.dataset()
	if got, want := grays(t, ds, nil), []uint8{255, 127, 0}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}
}

// TestRenderWindowOption: the caller's window replaces the file's.
func TestRenderWindowOption(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", px: []byte{10, 20, 30},
		text: map[tag.Tag]string{tagWindowCenter: "200", tagWindowWidth: "10"}}.dataset()
	if got, want := grays(t, ds, nil), []uint8{0, 0, 0}; !sameLevels(got, want) {
		t.Errorf("the file's window gave %v, want %v", got, want)
	}
	opts := &dataset.RenderOptions{Window: &dataset.Window{Center: 20, Width: 21}}
	// Lower edge 19.5 - 10 = 9.5, upper 29.5: 10 is just inside, 30 above.
	if got, want := grays(t, ds, opts), []uint8{6, 134, 255}; !sameLevels(got, want) {
		t.Errorf("the option's window gave %v, want %v", got, want)
	}
	if _, err := ds.RenderFrame(0, &dataset.RenderOptions{Window: &dataset.Window{Center: 20, Width: 0}}); err == nil {
		t.Error("a window of no width was accepted")
	}
}

// TestRenderVOIFunctions: LINEAR_EXACT has no half-unit offsets, and SIGMOID
// is at its midpoint at the center.
func TestRenderVOIFunctions(t *testing.T) {
	for function, want := range map[string][]uint8{
		// (x - 20) / 20 + 0.5: 10 is the lower edge, 30 the upper.
		"LINEAR_EXACT": {0, 128, 255},
		// 255 / (1 + exp(-4 (x - 20) / 20)): exp(2) and exp(-2) either side.
		"SIGMOID": {30, 128, 225},
	} {
		ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", px: []byte{10, 20, 30},
			text: map[tag.Tag]string{tagWindowCenter: "20", tagWindowWidth: "20", tagVOIFunction: function}}.dataset()
		if got := grays(t, ds, nil); !sameLevels(got, want) {
			t.Errorf("%s: levels %v, want %v", function, got, want)
		}
	}
}

func colors(t *testing.T, ds *dataset.Dataset, frame int) []color.NRGBA {
	t.Helper()
	img, err := ds.RenderFrame(frame, nil)
	if err != nil {
		t.Fatalf("RenderFrame: %v", err)
	}
	rgb, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("a color image rendered as %T", img)
	}
	out := make([]color.NRGBA, rgb.Rect.Dx())
	for x := range out {
		out[x] = rgb.NRGBAAt(x, 0)
	}
	return out
}

var (
	red   = color.NRGBA{255, 0, 0, 255}
	green = color.NRGBA{0, 255, 0, 255}
	blue  = color.NRGBA{0, 0, 255, 255}
)

func sameColors(a []color.NRGBA, b ...color.NRGBA) bool {
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

// TestRenderColor: RGB is shown as stored, whichever way its samples are
// laid out, and YBR is converted, since no screen takes it.
func TestRenderColor(t *testing.T) {
	byPixel := picture{allocated: 8, stored: 8, samples: 3, photometric: "RGB",
		px: []byte{255, 0, 0, 0, 255, 0, 0, 0, 255}}.dataset()
	if got := colors(t, byPixel, 0); !sameColors(got, red, green, blue) {
		t.Errorf("RGB by pixel rendered as %v", got)
	}

	byPlane := picture{allocated: 8, stored: 8, samples: 3, planar: 1, photometric: "RGB",
		px: []byte{255, 0, 0, 0, 255, 0, 0, 0, 255}}.dataset()
	if got := colors(t, byPlane, 0); !sameColors(got, red, green, blue) {
		t.Errorf("RGB by plane rendered as %v", got)
	}

	// Y 76, Cb 85, Cr 255 is red to within rounding: R = 76 + 1.402 * 127.
	ybr := picture{allocated: 8, stored: 8, samples: 3, photometric: "YBR_FULL",
		px: []byte{76, 85, 255, 0, 128, 128, 255, 128, 128}}.dataset()
	want := []color.NRGBA{{254, 0, 0, 255}, {0, 0, 0, 255}, {255, 255, 255, 255}}
	if got := colors(t, ybr, 0); !sameColors(got, want...) {
		t.Errorf("YBR_FULL rendered as %v, want %v", got, want)
	}
}

// TestRenderPalette: the samples are indexes into three lookup tables, here
// of 16-bit entries starting at index 0.
func TestRenderPalette(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "PALETTE COLOR", px: []byte{0, 1, 2, 9}}.dataset()
	descriptor := words(3, 0, 16)
	for i, lut := range [][]byte{words(0xFFFF, 0, 0), words(0, 0xFFFF, 0), words(0, 0, 0xFFFF)} {
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1101+uint16(i)), dataelem.US, descriptor))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1201+uint16(i)), dataelem.OW, lut))
	}
	// An index past the table takes its last entry (PS3.3 C.7.6.3.1.5).
	if got := colors(t, ds, 0); !sameColors(got, red, green, blue, blue) {
		t.Errorf("the palette image rendered as %v", got)
	}

	ds.Remove(tag.New(0x0028, 0x1202))
	if _, err := ds.RenderFrame(0, nil); err == nil {
		t.Error("a palette image with no green table was rendered")
	}
}

// TestRenderFrames: each frame is its own image, and a frame that is not
// there is an error, not the last one.
func TestRenderFrames(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", frames: 2, px: []byte{0, 255, 255, 0},
		text: map[tag.Tag]string{tagWindowCenter: "128", tagWindowWidth: "256"}}.dataset()
	second, err := ds.RenderFrame(1, nil)
	if err != nil {
		t.Fatalf("RenderFrame(1): %v", err)
	}
	if got := second.(*image.Gray).Pix; !sameLevels(got, []uint8{255, 0}) {
		t.Errorf("frame 1 rendered as %v", got)
	}
	all, err := ds.RenderFrames(nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("RenderFrames gave %d images, %v", len(all), err)
	}
	for _, frame := range []int{-1, 2} {
		if _, err := ds.RenderFrame(frame, nil); err == nil {
			t.Errorf("frame %d of 2 was rendered", frame)
		}
	}
	if _, err := dataset.NewDataset().RenderFrame(0, nil); err == nil {
		t.Error("a data set with no image was rendered")
	}
}

// TestRenderOneBitImage: a 1-bit image, as segmentations are stored, packs
// eight samples to a byte from the low bit up, and its frames follow one
// another with no padding between them. It was refused outright (#242).
func TestRenderOneBitImage(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	us := func(element uint16, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	us(0x0002, 1)
	us(0x0010, 1) // one row
	us(0x0011, 5) // of five, so the second frame starts mid-byte
	us(0x0100, 1)
	us(0x0101, 1)
	us(0x0102, 0)
	us(0x0103, 0)
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0004), dataelem.CS, []byte("MONOCHROME2 ")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, []byte("2 ")))
	// Frame 0: 1 0 1 1 0. Frame 1: 0 1 1 0 1. Low bit first:
	// byte 0 holds samples 0-7 = 1 0 1 1 0 | 0 1 1 -> 0b11001101
	// byte 1 holds samples 8-9 = 0 1           -> 0b00000010
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, []byte{0xCD, 0x02}))

	for frame, want := range [][]uint8{{255, 0, 255, 255, 0}, {0, 255, 255, 0, 255}} {
		img, err := ds.RenderFrame(frame, nil)
		if err != nil {
			t.Fatalf("RenderFrame(%d): %v", frame, err)
		}
		if got := img.(*image.Gray).Pix; !sameLevels(got, want) {
			t.Errorf("frame %d rendered as %v, want %v", frame, got, want)
		}
	}
}

// TestImageLargerThanAnIntIsRefused: sizes are worked out as ints, and a
// header may describe more than one holds. On a 32-bit platform 65535 by
// 65535 already does, and the product that wraps passed for a small image
// (#258). The image here is too large for 64 bits as well, so the test means
// the same everywhere.
func TestImageLargerThanAnIntIsRefused(t *testing.T) {
	ds := picture{allocated: 16, stored: 16, photometric: "MONOCHROME2", px: words(1, 2)}.dataset()
	for element, v := range map[uint16]int{0x0010: 65535, 0x0011: 65535, 0x0002: 65535, 0x0100: 65535} {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, []byte("2147483647 ")))
	if _, err := ds.GetPixelDataInfo(); err == nil {
		t.Error("an image of 2^95 bits was accepted")
	}
	if _, err := ds.RenderFrame(0, nil); err == nil {
		t.Error("an image of 2^95 bits was rendered")
	}
}
