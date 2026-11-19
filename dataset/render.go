package dataset

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Window is a VOI window in the units the Modality LUT produces: Hounsfield
// units for CT.
type Window struct {
	Center, Width float64
	// Function is the VOI LUT Function to apply it by: LINEAR, LINEAR_EXACT
	// or SIGMOID (PS3.3 C.11.2.1.3). Empty is LINEAR.
	Function string
}

// RenderOptions choose how an image is rendered for display.
type RenderOptions struct {
	// Window replaces the data set's own Window Center and Width. It applies
	// to grayscale images only.
	Window *Window
	// StoredRange shows a grayscale image that has no window and no VOI LUT,
	// and is given none, over the range its Bits Stored can hold, put
	// through its Modality LUT or rescale: the identity VOI LUT of PS3.3
	// C.11.2.1.2.2. Without it such an image spans the range of the values
	// it holds. The stored range is the same for every frame and every image
	// of the same kind, and is found without reading a pixel.
	StoredRange bool
}

// RenderFrame renders one frame, counted from 0, as an 8-bit image a screen
// can show: an *image.Gray for a grayscale image and an *image.NRGBA for a
// color one.
//
// A grayscale frame goes through the Modality LUT, the VOI LUT and the
// Presentation LUT of PS3.3 C.11. The stored values are read with their sign
// and put through the Modality LUT Sequence, or Rescale Slope and Intercept
// when there is no table. The result goes through the option's window, or
// else the data set's first VOI LUT Sequence item, or else its first Window
// Center and Width with its VOI LUT Function (LINEAR, LINEAR_EXACT or
// SIGMOID), or, when it has none of these, the range of the values across all
// frames. A Presentation LUT Sequence then maps that to what is shown; without
// one, Presentation LUT Shape INVERSE inverts and IDENTITY does not, and when
// there is no shape either, MONOCHROME1 is inverted. An enhanced multi-frame
// image takes its rescale, window and VOI LUT from the frame's functional
// group, then the shared one.
//
// PS3.3 C.11.2 lets a viewer apply either a VOI LUT Sequence or a window when
// a data set has both. The table is chosen, as pydicom's apply_voi_lut does;
// pass Window in the options to choose a window instead. A Presentation LUT
// Sequence takes the whole output range of the VOI LUT as its input range
// (C.11.4.1), where pydicom scales by the lowest and highest value in the
// image; the two agree whenever the image reaches both ends of the range.
//
// A color frame is converted to RGB: YBR_FULL and YBR_FULL_422 by the
// standard's equations, PALETTE COLOR through its lookup tables. Samples
// wider than 8 bits keep their most significant 8.
//
// ApplyWindowing, which this does not replace, reads samples as unsigned and
// applies no rescale. An ICC Profile (PS3.3 C.11.15) is not applied: color is
// shown as the samples say, without color management. ICCProfile returns
// it, for a PNG or JPEG to carry, so that a viewer that manages color does
// the managing. Samples of 64 bits,
// segmented palettes and a Presentation LUT Shape other than IDENTITY or
// INVERSE are refused.
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
	var out []image.Image
	err := ds.RenderEachFrame(opts, func(_ int, img image.Image) error {
		out = append(out, img)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RenderEachFrame renders every frame in turn, as RenderFrames does, and
// hands each to use as it is made, counted from 0. An error from use stops
// it and is returned.
//
// It is RenderFrames for a caller that writes each frame and lets it go:
// RenderFrames holds all of them at once, and a hundred frames of a
// megapixel image are a hundred megabytes before the first is written
// (#331). Calling RenderFrame for each frame instead decodes a compressed
// image once per frame, and finds the range of an image with no window
// once per frame, over all of them.
func (ds *Dataset) RenderEachFrame(opts *RenderOptions, use func(frame int, img image.Image) error) error {
	r, err := ds.newRenderer(opts)
	if err != nil {
		return err
	}
	for f := 0; f < r.info.NumberOfFrames; f++ {
		img, err := r.frame(f)
		if err != nil {
			return err
		}
		if err := use(f, img); err != nil {
			return err
		}
	}
	return nil
}

// FrameReader reads the frames of an image one at a time from wherever its
// Pixel Data is kept, for rendering an image whose data set does not hold
// it. filereader.DICOMFile.FrameReader reads them from the file by
// position.
type FrameReader interface {
	// ReadFrame returns frame i, counted from 0, as it is stored: the
	// samples of a native frame, little endian, or the codestream of a
	// compressed one, its fragments joined.
	ReadFrame(i int) ([]byte, error)
	// ReadFrameBytes reads len(p) bytes of native frame i, counted from
	// 0, little endian, from byte offset within the frame, into p.
	ReadFrameBytes(i int, offset int64, p []byte) error
}

// RenderFramesFrom renders the frames listed, counted from 0, in that
// order, as RenderFrame renders them, reading their samples from frames
// instead of the data set's Pixel Data, which the data set need not hold:
// read the file with filereader.ReadOptions.SkipBulkData and give its
// filereader.DICOMFile.FrameReader. Memory then follows a frame and not
// the image (#533). Each picture is handed to use as it is made; an error
// from use stops it and is returned.
//
// A compressed frame is read and decoded whole. A native frame of 8, 16 or
// 32 bits a sample, with one sample a pixel or its samples pixel after
// pixel, is read in bands of rows, about a megabyte at a time, and its
// picture draws a band when a row of it is first asked for, so memory
// follows a band and not the frame. That picture is neither an
// *image.Gray nor an *image.NRGBA: it has their color model, reports
// itself opaque, and is to be read only until use returns. image/png
// reads its rows in order and encodes it byte for byte as it encodes
// those. image/jpeg encodes a grayscale picture in one component only when
// it is an *image.Gray, so copy it into one first to encode it as JPEG. A
// band that cannot be read while use reads the picture leaves its rows
// black, and the error is returned when use returns.
//
// An image with no window and no VOI LUT, given none and not StoredRange,
// spans the range of the values across all its frames, as RenderFrame
// does, so before the first row is drawn every frame is read once to find
// it, a band or a frame at a time: memory still follows a band, and a
// compressed image is decoded twice.
func (ds *Dataset) RenderFramesFrom(frames FrameReader, list []int, opts *RenderOptions,
	use func(frame int, img image.Image) error) error {
	r, err := ds.newRendererFrom(frames, opts)
	if err != nil {
		return err
	}
	for _, f := range list {
		if f < 0 || f >= r.info.NumberOfFrames {
			return fmt.Errorf("render: frame %d does not exist; the image has %d, counted from 0",
				f, r.info.NumberOfFrames)
		}
	}
	for _, f := range list {
		p, err := r.painterFor(f)
		if err != nil {
			return err
		}
		if !r.banded {
			img, err := r.paintFrame(f, p)
			if err != nil {
				return err
			}
			if err := use(f, img); err != nil {
				return err
			}
			continue
		}
		picture := &bandedPicture{r: r, frame: f, paint: p, y0: -1}
		err = use(f, picture)
		if picture.err != nil {
			return picture.err
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// renderBandBytes is about how many bytes of samples a band of rows read
// from a FrameReader holds. A variable so that a test can make bands of a
// few rows out of a small image.
var renderBandBytes = 1 << 20

// renderer holds what every frame of one image shares.
type renderer struct {
	ds    *Dataset
	info  *PixelDataInfo
	px    []byte // samples by pixel, little endian, decompressed
	width int    // bytes per sample
	opts  RenderOptions

	// frames, when set, is where the samples are read from, a frame or a
	// band of rows at a time, and px holds only what was read last: pxFirst
	// is the index its first sample has in the whole pixel data, and held
	// says which frame and rows it is. Without frames, px is the whole
	// pixel data and pxFirst 0.
	frames       FrameReader
	pxFirst      int
	held         [3]int
	encapsulated bool
	compression  compress.CompressionType
	// banded says native frames are read a band of bandRows rows at a time;
	// otherwise bandRows is a whole frame.
	banded   bool
	bandRows int
	band     []byte // what px is, for a band

	// dataRange is the lowest and highest output value over all frames, for
	// an image with no window. Found on first use.
	dataRange *[2]float64

	// The grayscale tables, read once: the Modality LUT, the Presentation LUT
	// or the inversion that stands for one, and the VOI LUTs by the data set
	// that holds them, since an enhanced image may give each frame its own.
	modalityLUT *lut
	presentLUT  *lut
	invert      bool
	voiLUTs     map[*Dataset]*lut
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
	return ds.setUpRenderer(&renderer{ds: ds, info: info, px: px, bandRows: info.Rows}, opts)
}

// newRendererFrom is newRenderer for samples read from frames.
func (ds *Dataset) newRendererFrom(frames FrameReader, opts *RenderOptions) (*renderer, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	switch info.BitsAllocated {
	case 1, 8, 16, 32:
	default:
		return nil, fmt.Errorf("render: %d bits allocated is not supported", info.BitsAllocated)
	}
	// The transfer syntax alone says whether a frame is compressed: there
	// are no samples in hand to tell by, as there are for a data set that
	// holds its Pixel Data.
	syntax := ds.TransferSyntaxUID()
	if syntax == "" {
		return nil, fmt.Errorf("render: the data set names no transfer syntax, so whether its frames are compressed is not known")
	}
	encapsulated := compress.IsEncapsulated(syntax)
	compression, err := compress.TransferSyntaxToCompressionType(syntax)
	if err != nil && encapsulated {
		return nil, fmt.Errorf("render: %w", ds.undecodedSyntax())
	}
	r := &renderer{ds: ds, info: info, frames: frames, held: [3]int{-1, 0, 0},
		encapsulated: encapsulated, compression: compression, bandRows: info.Rows}
	// A band is a run of whole rows as the frame stores them, which is so
	// when every sample of a pixel is beside the others and each pixel has
	// all of its own: not for color by plane, not for YBR_FULL_422, which
	// shares chroma between two pixels, and not for 1-bit samples, packed
	// across the ends of rows.
	if !encapsulated && info.BitsAllocated >= 8 && info.Rows > 0 && info.Columns > 0 &&
		(info.SamplesPerPixel == 1 || info.PlanarConfiguration == 0) &&
		info.PhotometricInterpretation != "YBR_FULL_422" {
		rowBytes := info.Columns * info.SamplesPerPixel * info.BitsAllocated / 8
		r.banded = true
		r.bandRows = min(max(renderBandBytes/rowBytes, 1), info.Rows)
	}
	return ds.setUpRenderer(r, opts)
}

// setUpRenderer reads what every frame shares, whatever the samples are
// read from.
func (ds *Dataset) setUpRenderer(r *renderer, opts *RenderOptions) (*renderer, error) {
	info := r.info
	r.width = info.BitsAllocated / 8
	if opts != nil {
		r.opts = *opts
	}
	if w := r.opts.Window; w != nil && w.Width < 1 {
		return nil, fmt.Errorf("render: a window width of %v; it has to be at least 1", w.Width)
	}
	if info.SamplesPerPixel == 1 && info.PhotometricInterpretation != "PALETTE COLOR" {
		if err := r.readGrayTables(); err != nil {
			return nil, err
		}
	}
	if w := r.opts.Window; w != nil {
		switch strings.ToUpper(w.Function) {
		case "", "LINEAR", "LINEAR_EXACT", "SIGMOID":
		default:
			return nil, fmt.Errorf("render: VOI LUT Function %q; it is LINEAR, LINEAR_EXACT or SIGMOID", w.Function)
		}
	}
	return r, nil
}

var (
	tagModalityLUTSequence     = tag.New(0x0028, 0x3000)
	tagVOILUTSequence          = tag.New(0x0028, 0x3010)
	tagLUTDescriptor           = tag.New(0x0028, 0x3002)
	tagLUTData                 = tag.New(0x0028, 0x3006)
	tagPresentationLUTSequence = tag.New(0x2050, 0x0010)
	tagPresentationLUTShape    = tag.New(0x2050, 0x0020)
)

// readGrayTables reads the Modality LUT and the Presentation LUT, which the
// whole image shares.
func (r *renderer) readGrayTables() error {
	var err error
	if item := nthItem(r.ds, tagModalityLUTSequence, 0); item != nil {
		// The first value mapped is a stored value, so it has the stored
		// values' sign (PS3.3 C.11.1.1.1).
		if r.modalityLUT, err = readLUT(item, r.info.PixelRepresentation == 1, "Modality LUT"); err != nil {
			return err
		}
	}
	if item := nthItem(r.ds, tagPresentationLUTSequence, 0); item != nil {
		// Its output is P-values, brighter as they rise (PS3.3 C.11.6), so it
		// leaves nothing for an inversion to do.
		r.presentLUT, err = readLUT(item, false, "Presentation LUT")
		return err
	}
	r.invert = r.info.PhotometricInterpretation == "MONOCHROME1"
	if elem, ok := r.ds.Get(tagPresentationLUTShape); ok {
		shape, _ := extractStringValue(elem)
		switch shape = strings.ToUpper(strings.TrimSpace(shape)); shape {
		case "IDENTITY":
			r.invert = false
		case "INVERSE":
			// DX, MG and IO require INVERSE with MONOCHROME1 (PS3.3
			// C.8.11.1.1.1): it is that image's inversion, not a second one.
			r.invert = true
		default:
			return fmt.Errorf("render: a Presentation LUT Shape of %q is not supported; IDENTITY and INVERSE are", shape)
		}
	}
	return nil
}

// lut is a Modality, VOI or Presentation lookup table (PS3.3 C.11).
type lut struct {
	first   int // the input value the first entry is for
	entries []uint16
	top     float64 // the largest value an entry can hold: 2^bits - 1
}

// at looks up an input value. One before the first entry takes the first,
// and one past the last takes the last (PS3.3 C.11.1.1.1, C.11.2.1.1).
func (t *lut) at(x float64) float64 {
	i := min(max(int(math.Floor(x))-t.first, 0), len(t.entries)-1)
	return float64(t.entries[i])
}

// readLUT reads the LUT Descriptor and LUT Data of a table's item.
// signedFirst says the first value mapped is a signed number when the
// descriptor's VR does not say so itself: its VR is "US or SS", decided by
// whether the input to the table is signed, and a reader that cannot tell
// records US.
func readLUT(item *Dataset, signedFirst bool, name string) (*lut, error) {
	descriptor, okDescriptor := item.Get(tagLUTDescriptor)
	data, okData := item.Get(tagLUTData)
	if !okDescriptor || !okData {
		return nil, fmt.Errorf("render: the %s Sequence item has no LUT Descriptor or no LUT Data", name)
	}
	d, err := extractBytesValue(descriptor)
	if err != nil || len(d) < 6 {
		return nil, fmt.Errorf("render: the %s's LUT Descriptor is not three values", name)
	}
	count := int(binary.LittleEndian.Uint16(d))
	if count == 0 {
		count = 1 << 16 // PS3.3 C.11.1.1.1: 0 stands for 65536
	}
	first := int(binary.LittleEndian.Uint16(d[2:]))
	if signedFirst || descriptor.GetVR() == dataelem.SS {
		first = int(int16(binary.LittleEndian.Uint16(d[2:])))
	}
	bits := int(binary.LittleEndian.Uint16(d[4:]))
	if bits < 1 || bits > 16 {
		return nil, fmt.Errorf("render: the %s's LUT Descriptor gives %d bits per entry; it can be 1 to 16", name, bits)
	}

	raw, err := extractBytesValue(data)
	if err != nil {
		return nil, fmt.Errorf("render: the %s's LUT Data: %w", name, err)
	}
	t := &lut{first: first, entries: make([]uint16, count), top: float64(uint32(1)<<uint(bits) - 1)}
	mask := uint16(uint32(1)<<uint(bits) - 1)
	switch {
	case len(raw) >= count*2:
		// One entry to a 16-bit word, the usual form whatever the bits.
		for i := range t.entries {
			t.entries[i] = binary.LittleEndian.Uint16(raw[i*2:]) & mask
		}
	case bits <= 8 && len(raw) >= count:
		// 8-bit entries packed one to a byte, as some writers do and
		// pydicom reads for a Presentation LUT.
		for i := range t.entries {
			t.entries[i] = uint16(raw[i]) & mask
		}
	default:
		return nil, fmt.Errorf("render: the %s's LUT Data holds %d bytes for %d entries", name, len(raw), count)
	}
	return t, nil
}

func (r *renderer) frame(f int) (image.Image, error) {
	p, err := r.painterFor(f)
	if err != nil {
		return nil, err
	}
	return r.paintFrame(f, p)
}

// painter draws the pixels of one frame of a picture: pixel(i, out) draws
// pixel i of the whole pixel data into out, one byte for a grayscale
// picture and four, red, green, blue and alpha, for a color one.
type painter struct {
	gray  bool
	pixel func(i int, out []byte)
}

// painterFor reads what drawing frame f needs: its window, tables and
// range.
func (r *renderer) painterFor(f int) (painter, error) {
	switch p := r.info.PhotometricInterpretation; {
	case r.info.SamplesPerPixel == 1 && p == "PALETTE COLOR":
		return r.palette()
	case r.info.SamplesPerPixel == 1:
		return r.gray(f)
	case r.info.SamplesPerPixel == 3:
		return r.color()
	}
	return painter{}, fmt.Errorf("render: %d samples per pixel is not supported", r.info.SamplesPerPixel)
}

// paintFrame draws a whole frame as an *image.Gray or an *image.NRGBA.
func (r *renderer) paintFrame(f int, p painter) (image.Image, error) {
	bounds := image.Rect(0, 0, r.info.Columns, r.info.Rows)
	if p.gray {
		img := image.NewGray(bounds)
		return img, r.paintRows(f, p, 0, r.info.Rows, img.Pix)
	}
	img := image.NewNRGBA(bounds)
	return img, r.paintRows(f, p, 0, r.info.Rows, img.Pix)
}

// paintRows draws rows y0 to y1 of frame f into pix, which holds just
// those rows, reading the samples a band at a time where they are read
// from a FrameReader.
func (r *renderer) paintRows(f int, p painter, y0, y1 int, pix []byte) error {
	columns, size := r.info.Columns, 4
	if p.gray {
		size = 1
	}
	base := f * r.info.Rows * columns
	for y := y0; y < y1; {
		first := y / r.bandRows * r.bandRows
		end := min(first+r.bandRows, r.info.Rows)
		if err := r.load(f, first, end); err != nil {
			return err
		}
		end = min(end, y1)
		for i := y * columns; i < end*columns; i++ {
			o := (i - y0*columns) * size
			p.pixel(base+i, pix[o:o+size])
		}
		y = end
	}
	return nil
}

// load puts rows y0 to y1 of frame f in px, when the samples are read from
// a FrameReader: those rows when frames are read in bands, else the whole
// frame, decoded when it is compressed. A data set that holds its pixel
// data has them all in px already.
func (r *renderer) load(f, y0, y1 int) error {
	if r.frames == nil {
		return nil
	}
	if !r.banded {
		y0, y1 = 0, r.info.Rows
	}
	if r.held == [3]int{f, y0, y1} {
		return nil
	}
	info := r.info
	perFrame := info.Rows * info.Columns
	if r.banded {
		rowBytes := int64(info.Columns * info.SamplesPerPixel * r.width)
		// One buffer for every band: a band read into a new one each time
		// is a megabyte of garbage a band, and the collector lets it pile
		// up to the size of whatever else is held, the whole picture when
		// it is encoded as JPEG.
		need := int(int64(y1-y0) * rowBytes)
		if cap(r.band) < need {
			r.band = make([]byte, need)
		}
		r.band = r.band[:need]
		r.held = [3]int{-1, 0, 0}
		if err := r.frames.ReadFrameBytes(f, int64(y0)*rowBytes, r.band); err != nil {
			return fmt.Errorf("render: %w", err)
		}
		r.px, r.pxFirst = r.band, (f*perFrame+y0*info.Columns)*info.SamplesPerPixel
		r.held = [3]int{f, y0, y1}
		return nil
	}
	data, err := r.frames.ReadFrame(f)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if r.encapsulated {
		// Decoded as pixelBytesForDecoding decodes each frame, and refused
		// in its words.
		decoded, err := decompressPixelFrame(r.compression, data, info)
		if err != nil {
			return fmt.Errorf("render: failed to decode %s frame %d: %w", r.compression, f, err)
		}
		data = decoded
	} else {
		// The corrections pixelBytesForDecoding makes to the whole value,
		// made to this frame.
		one := *info
		one.NumberOfFrames = 1
		data = normalizeNativePixelBytes(data, &one)
	}
	// In bits, since a 1-bit image packs eight samples to a byte.
	if need := (perFrame*info.SamplesPerPixel*info.BitsAllocated + 7) / 8; len(data) < need {
		return fmt.Errorf("render: frame %d holds %d bytes and needs %d", f, len(data), need)
	}
	r.px, r.pxFirst = data, f*perFrame*info.SamplesPerPixel
	r.held = [3]int{f, y0, y1}
	return nil
}

// sample returns stored sample i of the whole pixel data as a number: the
// Bits Stored bits below High Bit, with their sign when the data is signed.
func (r *renderer) sample(i int) float64 {
	// px may hold only part of the pixel data, from sample pxFirst.
	i -= r.pxFirst
	if r.info.BitsAllocated == 1 {
		// Packed from the low bit up, frame after frame with no padding
		// between them (PS3.5 8.2): sample i is bit i of the whole value. A
		// frame read alone starts at bit 0 of its first byte.
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

func (r *renderer) gray(f int) (painter, error) {
	modality := r.modality(f)

	window, function, voiLUT, err := r.voi(f)
	if err != nil {
		return painter{}, err
	}
	var low, high float64
	switch {
	case window != nil || voiLUT != nil:
	case r.opts.StoredRange:
		low, high = r.storedRange(f)
	default:
		if low, high, err = r.outputRange(); err != nil {
			return painter{}, err
		}
	}

	return painter{gray: true, pixel: func(i int, out []byte) {
		x := modality(r.sample(i))
		var y float64
		switch {
		case window != nil:
			y = windowed(x, window.Center, window.Width, function)
		case voiLUT != nil:
			y = voiLUT.at(x) / voiLUT.top * 255
		case high > low:
			y = (x - low) / (high - low) * 255
		}
		y = math.Min(255, math.Max(0, y))
		if p := r.presentLUT; p != nil {
			// The VOI output range, here 0 to 255, is scaled linearly to the
			// table's entries (PS3.3 C.11.4.1), truncating as pydicom does.
			y = p.at(float64(p.first)+y*float64(len(p.entries)-1)/255) / p.top * 255
		}
		level := uint8(math.Round(y))
		if r.invert {
			level = 255 - level
		}
		out[0] = level
	}}, nil
}

// modality returns the Modality LUT for a frame as a function from stored
// value to output value: the Modality LUT Sequence's table, or else Rescale
// Slope and Intercept. PS3.3 C.11.1 allows one or the other, not both.
func (r *renderer) modality(f int) func(float64) float64 {
	if t := r.modalityLUT; t != nil {
		return t.at
	}
	slope, intercept := r.rescale(f)
	return func(x float64) float64 { return x*slope + intercept }
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

// storedRange is the range of output values the Modality LUT stage can give
// frame f, which an identity VOI LUT passes on (PS3.3 C.11.2.1.2.2): the
// values Bits Stored can hold, signed when Pixel Representation says so,
// through Rescale Slope and Intercept, or the output range of a Modality
// LUT Sequence, 0 to 2^n-1 (C.11.1.1.1).
func (r *renderer) storedRange(f int) (low, high float64) {
	if t := r.modalityLUT; t != nil {
		return 0, t.top
	}
	stored := r.info.BitsStored
	if stored <= 0 || stored > r.info.BitsAllocated {
		stored = r.info.BitsAllocated
	}
	low, high = 0, math.Ldexp(1, stored)-1
	if r.info.PixelRepresentation == 1 {
		low, high = -math.Ldexp(1, stored-1), math.Ldexp(1, stored-1)-1
	}
	modality := r.modality(f)
	low, high = modality(low), modality(high)
	return math.Min(low, high), math.Max(low, high)
}

// outputRange is the lowest and highest value of the image after its rescale,
// over all frames, so the frames of one image share one scale. Samples read
// from a FrameReader are read for it a band or a frame at a time.
func (r *renderer) outputRange() (low, high float64, err error) {
	if r.dataRange != nil {
		return r.dataRange[0], r.dataRange[1], nil
	}
	columns := r.info.Columns
	perFrame := r.info.Rows * columns
	low, high = math.Inf(1), math.Inf(-1)
	for f := 0; f < r.info.NumberOfFrames; f++ {
		modality := r.modality(f)
		for y := 0; y < r.info.Rows; y += r.bandRows {
			end := min(y+r.bandRows, r.info.Rows)
			if err := r.load(f, y, end); err != nil {
				return 0, 0, err
			}
			for i := f*perFrame + y*columns; i < f*perFrame+end*columns; i++ {
				x := modality(r.sample(i))
				low, high = math.Min(low, x), math.Max(high, x)
			}
		}
	}
	r.dataRange = &[2]float64{low, high}
	return low, high, nil
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
	return rescaleOf(r.ds, f)
}

// rescaleOf returns the Rescale Slope and Intercept of frame f of a data set,
// 1 and 0 when it has none: an absent Rescale Slope means no scaling, not a
// slope of 0. A single frame image holds them in the data set itself; an
// enhanced multi-frame image holds them per frame, in the Pixel Value
// Transformation Sequence of the frame's functional group or of the shared one
// (PS3.3 C.7.6.16.2.9). They are Type 1 together, so either one finds the
// holder. A Rescale Slope of 0 is read as 1, since a slope of 0 would turn
// every pixel into the intercept.
func rescaleOf(ds *Dataset, f int) (slope, intercept float64) {
	slope = 1
	if src := frameHolder(ds, f, tagPixelValueXform, tagRescaleSlope, tagRescaleIntercept); src != nil {
		if v, ok := firstNumber(src, tagRescaleSlope); ok && v != 0 {
			slope = v
		}
		if v, ok := firstNumber(src, tagRescaleIntercept); ok {
			intercept = v
		}
	}
	return slope, intercept
}

// voi returns the VOI LUT for a frame: a window and its VOI LUT Function, or
// a table, or neither when the image has none and the caller gave none. A
// caller's window names its own function, linear when it names none.
func (r *renderer) voi(f int) (*Window, string, *lut, error) {
	if w := r.opts.Window; w != nil {
		if w.Function == "" {
			return w, "LINEAR", nil, nil
		}
		return w, strings.ToUpper(w.Function), nil, nil
	}
	src := r.holder(f, tagFrameVOILUT, tagWindowWidth, tagVOILUTSequence)
	if src == nil {
		return nil, "", nil, nil
	}
	if item := nthItem(src, tagVOILUTSequence, 0); item != nil {
		if t, ok := r.voiLUTs[src]; ok {
			return nil, "", t, nil
		}
		// The table's input is the Modality LUT's output, signed when the
		// stored values are or the rescale can take them below 0. A Modality
		// LUT Sequence's output is unsigned (PS3.3 C.11.1.1.1).
		_, intercept := r.rescale(f)
		signed := r.modalityLUT == nil && (r.info.PixelRepresentation == 1 || intercept < 0)
		t, err := readLUT(item, signed, "VOI LUT")
		if err != nil {
			return nil, "", nil, err
		}
		if r.voiLUTs == nil {
			r.voiLUTs = map[*Dataset]*lut{}
		}
		r.voiLUTs[src] = t
		return nil, "", t, nil
	}
	center, okCenter := firstNumber(src, tagWindowCenter)
	width, okWidth := firstNumber(src, tagWindowWidth)
	if !okCenter || !okWidth {
		return nil, "", nil, nil
	}
	if width < 1 {
		return nil, "", nil, fmt.Errorf("render: the image's Window Width is %v; it has to be at least 1", width)
	}
	function := "LINEAR"
	if elem, ok := src.Get(tagVOILUTFunction); ok {
		if v, err := extractStringValue(elem); err == nil && v != "" {
			function = strings.ToUpper(v)
		}
	}
	return &Window{Center: center, Width: width}, function, nil, nil
}

// holder returns the data set that holds any of attrs for a frame: the
// frame's own functional group, the shared one, or the image itself. An
// enhanced multi-frame image keeps its rescale, window and VOI LUT in the
// first two. The nearest wins, so a frame's own window is not passed over
// for a table the whole image has.
func (r *renderer) holder(f int, group tag.Tag, attrs ...tag.Tag) *Dataset {
	return frameHolder(r.ds, f, group, attrs...)
}

// frameHolder is holder for a data set that is not being rendered.
func frameHolder(ds *Dataset, f int, group tag.Tag, attrs ...tag.Tag) *Dataset {
	holds := func(ds *Dataset) bool {
		for _, attr := range attrs {
			if _, ok := ds.Get(attr); ok {
				return true
			}
		}
		return false
	}
	if item := nthItem(ds, tagPerFrameGroups, f); item != nil {
		if macro := nthItem(item, group, 0); macro != nil && holds(macro) {
			return macro
		}
	}
	if item := nthItem(ds, tagSharedGroups, 0); item != nil {
		if macro := nthItem(item, group, 0); macro != nil && holds(macro) {
			return macro
		}
	}
	if holds(ds) {
		return ds
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

func (r *renderer) color() (painter, error) {
	var ybr bool
	switch r.info.PhotometricInterpretation {
	case "RGB", "YBR_RCT", "YBR_ICT":
		// The two JPEG 2000 transforms are undone by the decoder, so what is
		// in hand is RGB whatever the attribute still says.
	case "YBR_FULL", "YBR_FULL_422":
		ybr = true
	default:
		return painter{}, fmt.Errorf("render: photometric interpretation %q is not supported",
			r.info.PhotometricInterpretation)
	}

	return painter{pixel: func(i int, out []byte) {
		s := i * 3
		a, b, c := r.channel(s), r.channel(s+1), r.channel(s+2)
		if ybr {
			// PS3.3 C.7.6.3.1.2: full range Y, with Cb and Cr about 128.
			y, cb, cr := a, b-128, c-128
			a = y + 1.402*cr
			b = y - 0.344136*cb - 0.714136*cr
			c = y + 1.772*cb
		}
		out[0], out[1], out[2], out[3] = clamp8(a), clamp8(b), clamp8(c), 255
	}}, nil
}

// paletteTable is one of the three lookup tables of a PALETTE COLOR image.
type paletteTable struct {
	first   int // the stored value the first entry is for
	entries []uint8
}

func (r *renderer) palette() (painter, error) {
	var tables [3]paletteTable
	for c := range tables {
		table, err := r.paletteTable(uint16(c))
		if err != nil {
			return painter{}, err
		}
		tables[c] = table
	}

	return painter{pixel: func(i int, out []byte) {
		value := int(r.sample(i))
		for c, table := range tables {
			// A value before the first entry takes the first, and one past
			// the last takes the last (PS3.3 C.7.6.3.1.5).
			index := min(max(value-table.first, 0), len(table.entries)-1)
			out[c] = table.entries[index]
		}
		out[3] = 255
	}}, nil
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
