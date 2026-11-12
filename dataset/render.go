package dataset

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Window is a VOI window in the units the Modality LUT produces: Hounsfield
// units for CT.
type Window struct {
	Center, Width float64
}

// RenderOptions choose how an image is rendered for display.
type RenderOptions struct {
	// Window replaces the data set's own Window Center and Width. It applies
	// to grayscale images only.
	Window *Window
}

// RenderFrame renders one frame, counted from 0, as an 8-bit image a screen
// can show: an *image.Gray for a grayscale image and an *image.NRGBA for a
// color one.
//
// A grayscale frame goes through what PS3.3 C.11 calls the Modality LUT and
// the VOI LUT. The stored values are read with their sign, Rescale Slope and
// Intercept are applied, and the result is windowed: by the option's window,
// or the data set's first Window Center and Width with its VOI LUT Function
// (LINEAR, LINEAR_EXACT or SIGMOID), or, when it has neither, the range of
// the values across all frames. MONOCHROME1 is inverted. An enhanced
// multi-frame image takes its rescale and window from the frame's functional
// group, then the shared one.
//
// A color frame is converted to RGB: YBR_FULL and YBR_FULL_422 by the
// standard's equations, PALETTE COLOR through its lookup tables. Samples
// wider than 8 bits keep their most significant 8.
//
// ApplyWindowing, which this does not replace, reads samples as unsigned and
// applies no rescale. A VOI LUT Sequence and a Modality LUT Sequence are not
// applied, and neither is a Presentation LUT or an ICC profile. Samples of
// 64 bits and segmented palettes are refused.
func (ds *Dataset) RenderFrame(frame int, opts *RenderOptions) (image.Image, error) {
	r, err := ds.newRenderer(opts)
	if err != nil {
		return nil, err
	}
	if frame < 0 || frame >= r.info.NumberOfFrames {
		return nil, fmt.Errorf("render: frame %d does not exist; the image has %d, counted from 0",
			frame, r.info.NumberOfFrames)
	}
	return r.frame(frame)
}

// RenderFrames renders every frame; see RenderFrame.
func (ds *Dataset) RenderFrames(opts *RenderOptions) ([]image.Image, error) {
	r, err := ds.newRenderer(opts)
	if err != nil {
		return nil, err
	}
	out := make([]image.Image, r.info.NumberOfFrames)
	for f := range out {
		if out[f], err = r.frame(f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// renderer holds what every frame of one image shares.
type renderer struct {
	ds    *Dataset
	info  *PixelDataInfo
	px    []byte // samples by pixel, little endian, decompressed
	width int    // bytes per sample
	opts  RenderOptions

	// dataRange is the lowest and highest output value over all frames, for
	// an image with no window. Found on first use.
	dataRange *[2]float64
}

func (ds *Dataset) newRenderer(opts *RenderOptions) (*renderer, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	switch info.BitsAllocated {
	case 1, 8, 16, 32:
	default:
		return nil, fmt.Errorf("render: %d bits allocated is not supported", info.BitsAllocated)
	}
	px, err := ds.pixelBytesForDecoding(info)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	// In bits, since a 1-bit image packs eight samples to a byte and its
	// frames need not end on one.
	bits := info.Rows * info.Columns * info.SamplesPerPixel * info.BitsAllocated * info.NumberOfFrames
	if need := (bits + 7) / 8; len(px) < need {
		return nil, fmt.Errorf("render: the pixel data holds %d bytes and the image needs %d", len(px), need)
	}
	r := &renderer{ds: ds, info: info, px: px, width: info.BitsAllocated / 8}
	if opts != nil {
		r.opts = *opts
	}
	if w := r.opts.Window; w != nil && w.Width < 1 {
		return nil, fmt.Errorf("render: a window width of %v; it has to be at least 1", w.Width)
	}
	return r, nil
}

func (r *renderer) frame(f int) (image.Image, error) {
	switch p := r.info.PhotometricInterpretation; {
	case r.info.SamplesPerPixel == 1 && p == "PALETTE COLOR":
		return r.palette(f)
	case r.info.SamplesPerPixel == 1:
		return r.gray(f)
	case r.info.SamplesPerPixel == 3:
		return r.color(f)
	}
	return nil, fmt.Errorf("render: %d samples per pixel is not supported", r.info.SamplesPerPixel)
}

// sample returns stored sample i of the whole pixel data as a number: the
// Bits Stored bits below High Bit, with their sign when the data is signed.
func (r *renderer) sample(i int) float64 {
	if r.info.BitsAllocated == 1 {
		// Packed from the low bit up, frame after frame with no padding
		// between them (PS3.5 8.2): sample i is bit i of the whole value.
		return float64(r.px[i/8] >> (i % 8) & 1)
	}
	var raw uint64
	switch off := i * r.width; r.width {
	case 1:
		raw = uint64(r.px[off])
	case 2:
		raw = uint64(binary.LittleEndian.Uint16(r.px[off:]))
	default:
		raw = uint64(binary.LittleEndian.Uint32(r.px[off:]))
	}
	stored := r.info.BitsStored
	if stored <= 0 || stored > r.info.BitsAllocated {
		stored = r.info.BitsAllocated
	}
	// The bits above and below the stored ones are not part of the value.
	if shift := r.info.HighBit + 1 - stored; shift > 0 && r.info.HighBit < r.info.BitsAllocated {
		raw >>= uint(shift)
	}
	raw &= uint64(1)<<uint(stored) - 1
	if r.info.PixelRepresentation == 1 && raw&(uint64(1)<<uint(stored-1)) != 0 {
		return float64(int64(raw) - int64(1)<<uint(stored))
	}
	return float64(raw)
}

func (r *renderer) gray(f int) (image.Image, error) {
	slope, intercept := r.rescale(f)
	perFrame := r.info.Rows * r.info.Columns
	base := f * perFrame

	window, function, err := r.window(f)
	if err != nil {
		return nil, err
	}
	var low, high float64
	if window == nil {
		low, high = r.outputRange()
	}

	img := image.NewGray(image.Rect(0, 0, r.info.Columns, r.info.Rows))
	invert := r.info.PhotometricInterpretation == "MONOCHROME1"
	for i := 0; i < perFrame; i++ {
		x := r.sample(base+i)*slope + intercept
		var y float64
		if window != nil {
			y = windowed(x, window.Center, window.Width, function)
		} else if high > low {
			y = (x - low) / (high - low) * 255
		}
		level := uint8(math.Round(math.Min(255, math.Max(0, y))))
		if invert {
			level = 255 - level
		}
		img.Pix[i] = level
	}
	return img, nil
}

// windowed maps x through a window to 0..255, by PS3.3 C.11.2.1.2.
func windowed(x, center, width float64, function string) float64 {
	switch function {
	case "SIGMOID":
		return 255 / (1 + math.Exp(-4*(x-center)/width))
	case "LINEAR_EXACT":
		switch {
		case x <= center-width/2:
			return 0
		case x > center+width/2:
			return 255
		}
		return ((x-center)/width + 0.5) * 255
	}
	// LINEAR, with the half-unit offsets that make a width of 1 a threshold.
	switch {
	case x <= center-0.5-(width-1)/2:
		return 0
	case x > center-0.5+(width-1)/2:
		return 255
	}
	return ((x-(center-0.5))/(width-1) + 0.5) * 255
}

// outputRange is the lowest and highest value of the image after its rescale,
// over all frames, so the frames of one image share one scale.
func (r *renderer) outputRange() (low, high float64) {
	if r.dataRange != nil {
		return r.dataRange[0], r.dataRange[1]
	}
	perFrame := r.info.Rows * r.info.Columns
	low, high = math.Inf(1), math.Inf(-1)
	for f := 0; f < r.info.NumberOfFrames; f++ {
		slope, intercept := r.rescale(f)
		for i := f * perFrame; i < (f+1)*perFrame; i++ {
			x := r.sample(i)*slope + intercept
			low, high = math.Min(low, x), math.Max(high, x)
		}
	}
	r.dataRange = &[2]float64{low, high}
	return low, high
}

var (
	tagPerFrameGroups   = tag.New(0x5200, 0x9230)
	tagSharedGroups     = tag.New(0x5200, 0x9229)
	tagPixelValueXform  = tag.New(0x0028, 0x9145)
	tagFrameVOILUT      = tag.New(0x0028, 0x9132)
	tagRescaleIntercept = tag.New(0x0028, 0x1052)
	tagRescaleSlope     = tag.New(0x0028, 0x1053)
	tagWindowCenter     = tag.New(0x0028, 0x1050)
	tagWindowWidth      = tag.New(0x0028, 0x1051)
	tagVOILUTFunction   = tag.New(0x0028, 0x1056)
)

// rescale returns the Rescale Slope and Intercept for a frame, 1 and 0 when
// the image has none.
func (r *renderer) rescale(f int) (slope, intercept float64) {
	slope = 1
	if src := r.holder(f, tagPixelValueXform, tagRescaleSlope); src != nil {
		if v, ok := firstNumber(src, tagRescaleSlope); ok && v != 0 {
			slope = v
		}
		if v, ok := firstNumber(src, tagRescaleIntercept); ok {
			intercept = v
		}
	}
	return slope, intercept
}

// window returns the window for a frame and its VOI LUT Function, or nil
// when the image has none and the caller gave none.
func (r *renderer) window(f int) (*Window, string, error) {
	if r.opts.Window != nil {
		return r.opts.Window, "LINEAR", nil
	}
	src := r.holder(f, tagFrameVOILUT, tagWindowWidth)
	if src == nil {
		return nil, "", nil
	}
	center, okCenter := firstNumber(src, tagWindowCenter)
	width, okWidth := firstNumber(src, tagWindowWidth)
	if !okCenter || !okWidth {
		return nil, "", nil
	}
	if width < 1 {
		return nil, "", fmt.Errorf("render: the image's Window Width is %v; it has to be at least 1", width)
	}
	function := "LINEAR"
	if elem, ok := src.Get(tagVOILUTFunction); ok {
		if v, err := extractStringValue(elem); err == nil && v != "" {
			function = strings.ToUpper(v)
		}
	}
	return &Window{Center: center, Width: width}, function, nil
}

// holder returns the data set that holds attr for a frame: the frame's own
// functional group, the shared one, or the image itself. An enhanced
// multi-frame image keeps its rescale and window in the first two.
func (r *renderer) holder(f int, group, attr tag.Tag) *Dataset {
	if item := nthItem(r.ds, tagPerFrameGroups, f); item != nil {
		if macro := nthItem(item, group, 0); macro != nil {
			if _, ok := macro.Get(attr); ok {
				return macro
			}
		}
	}
	if item := nthItem(r.ds, tagSharedGroups, 0); item != nil {
		if macro := nthItem(item, group, 0); macro != nil {
			if _, ok := macro.Get(attr); ok {
				return macro
			}
		}
	}
	if _, ok := r.ds.Get(attr); ok {
		return r.ds
	}
	return nil
}

// nthItem returns item n of a sequence, or nil.
func nthItem(ds *Dataset, t tag.Tag, n int) *Dataset {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok || n >= seq.Length() {
		return nil
	}
	item, err := seq.Get(n)
	if err != nil {
		return nil
	}
	inner, _ := item.(*Dataset)
	return inner
}

// firstNumber reads the first value of a decimal string attribute. Window
// Center and Width usually hold several, and the first is the default.
func firstNumber(ds *Dataset, t tag.Tag) (float64, bool) {
	elem, ok := ds.Get(t)
	if !ok {
		return 0, false
	}
	text, err := extractStringValue(elem)
	if err != nil {
		return 0, false
	}
	first, _, _ := strings.Cut(text, `\`)
	v, err := strconv.ParseFloat(strings.TrimSpace(first), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// channel reduces a color sample to 8 bits, keeping its most significant.
func (r *renderer) channel(i int) float64 {
	v := r.sample(i)
	stored := r.info.BitsStored
	if stored <= 0 || stored > r.info.BitsAllocated {
		stored = r.info.BitsAllocated
	}
	if stored > 8 {
		v = math.Floor(v / float64(uint64(1)<<uint(stored-8)))
	}
	return v
}

func clamp8(v float64) uint8 {
	return uint8(math.Round(math.Min(255, math.Max(0, v))))
}

func (r *renderer) color(f int) (image.Image, error) {
	var ybr bool
	switch r.info.PhotometricInterpretation {
	case "RGB", "YBR_RCT", "YBR_ICT":
		// The two JPEG 2000 transforms are undone by the decoder, so what is
		// in hand is RGB whatever the attribute still says.
	case "YBR_FULL", "YBR_FULL_422":
		ybr = true
	default:
		return nil, fmt.Errorf("render: photometric interpretation %q is not supported",
			r.info.PhotometricInterpretation)
	}

	perFrame := r.info.Rows * r.info.Columns
	img := image.NewNRGBA(image.Rect(0, 0, r.info.Columns, r.info.Rows))
	for i := 0; i < perFrame; i++ {
		s := (f*perFrame + i) * 3
		a, b, c := r.channel(s), r.channel(s+1), r.channel(s+2)
		if ybr {
			// PS3.3 C.7.6.3.1.2: full range Y, with Cb and Cr about 128.
			y, cb, cr := a, b-128, c-128
			a = y + 1.402*cr
			b = y - 0.344136*cb - 0.714136*cr
			c = y + 1.772*cb
		}
		o := i * 4
		img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = clamp8(a), clamp8(b), clamp8(c), 255
	}
	return img, nil
}

// paletteTable is one of the three lookup tables of a PALETTE COLOR image.
type paletteTable struct {
	first   int // the stored value the first entry is for
	entries []uint8
}

func (r *renderer) palette(f int) (image.Image, error) {
	var tables [3]paletteTable
	for c := range tables {
		table, err := r.paletteTable(uint16(c))
		if err != nil {
			return nil, err
		}
		tables[c] = table
	}

	perFrame := r.info.Rows * r.info.Columns
	img := image.NewNRGBA(image.Rect(0, 0, r.info.Columns, r.info.Rows))
	for i := 0; i < perFrame; i++ {
		value := int(r.sample(f*perFrame + i))
		o := i * 4
		for c, table := range tables {
			// A value before the first entry takes the first, and one past
			// the last takes the last (PS3.3 C.7.6.3.1.5).
			index := min(max(value-table.first, 0), len(table.entries)-1)
			img.Pix[o+c] = table.entries[index]
		}
		img.Pix[o+3] = 255
	}
	return img, nil
}

// paletteTable reads the red (0), green (1) or blue (2) lookup table as
// 8-bit entries.
func (r *renderer) paletteTable(c uint16) (paletteTable, error) {
	name := []string{"Red", "Green", "Blue"}[c]
	descriptor, okDescriptor := r.ds.Get(tag.New(0x0028, 0x1101+c))
	data, okData := r.ds.Get(tag.New(0x0028, 0x1201+c))
	if !okDescriptor || !okData {
		if _, segmented := r.ds.Get(tag.New(0x0028, 0x1221+c)); segmented {
			return paletteTable{}, fmt.Errorf("render: a segmented %s Palette Color Lookup Table is not supported", name)
		}
		return paletteTable{}, fmt.Errorf("render: the image has no %s Palette Color Lookup Table", name)
	}
	d, err := extractBytesValue(descriptor)
	if err != nil || len(d) < 6 {
		return paletteTable{}, fmt.Errorf("render: the %s Palette Color Lookup Table Descriptor is not three values", name)
	}
	count := int(binary.LittleEndian.Uint16(d))
	if count == 0 {
		count = 1 << 16 // PS3.3 C.7.6.3.1.5: 0 stands for 65536
	}
	first := int(binary.LittleEndian.Uint16(d[2:]))
	if r.info.PixelRepresentation == 1 {
		first = int(int16(binary.LittleEndian.Uint16(d[2:])))
	}
	bits := int(binary.LittleEndian.Uint16(d[4:]))

	raw, err := extractBytesValue(data)
	if err != nil {
		return paletteTable{}, fmt.Errorf("render: %s Palette Color Lookup Table Data: %w", name, err)
	}
	table := paletteTable{first: first, entries: make([]uint8, count)}
	switch {
	case len(raw) >= count*2:
		// 16-bit entries. Some writers declare 16 bits and fill only the low
		// 8, which shifted down would be black throughout.
		var highest uint16
		for i := 0; i < count; i++ {
			highest = max(highest, binary.LittleEndian.Uint16(raw[i*2:]))
		}
		shift := uint(8)
		if bits == 8 || highest < 256 {
			shift = 0
		}
		for i := range table.entries {
			table.entries[i] = uint8(binary.LittleEndian.Uint16(raw[i*2:]) >> shift)
		}
	case len(raw) >= count:
		// 8-bit entries packed one to a byte.
		copy(table.entries, raw[:count])
	default:
		return paletteTable{}, fmt.Errorf("render: %s Palette Color Lookup Table Data holds %d bytes for %d entries",
			name, len(raw), count)
	}
	return table, nil
}
