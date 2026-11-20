package dataset

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// PixelRegion is a rectangle of an image, in pixels from its top left corner.
type PixelRegion struct {
	X, Y          int
	Width, Height int
}

// RedactPixelRegions fills the regions with black in every frame.
//
// It is for what de-identifying the attributes cannot reach: text drawn into
// the image, as ultrasound machines, screen captures and secondary captures
// do with the patient's name. Finding the text is the caller's part.
//
// A region is clipped to the image, and one that covers no pixel is an
// error. The pixel data has to be native: an encapsulated frame is a
// codestream, not samples, and is refused (DecompressRedactingRegions decodes
// and fills in one pass). Nothing else in the data set changes.
//
// When the data set was read with filereader.ReadOptions.SkipBulkData and
// built with filereader.DICOMFile.GetDatasetWithValuesInFile, its Pixel Data
// is not read: it becomes a dataelem.StreamedValue, and filewriter, given the
// file as its value source, fills the regions as it copies the pixels into
// the output, a chunk at a time. Regions asked for after others are added to
// them.
//
// Black is the lowest value the samples can hold, or the highest under
// MONOCHROME1, where high is dark. For YBR it is zero luminance with neutral
// chroma, and for PALETTE COLOR it is the darkest entry of the palette.
func (ds *Dataset) RedactPixelRegions(regions ...PixelRegion) error {
	return ds.redact(-1, regions)
}

// RedactFrameRegions is RedactPixelRegions for one frame, counted from 0.
func (ds *Dataset) RedactFrameRegions(frame int, regions ...PixelRegion) error {
	if frame < 0 {
		return fmt.Errorf("redact: frame %d does not exist", frame)
	}
	return ds.redact(frame, regions)
}

// redact fills regions in one frame, or in all of them when frame is -1.
//
// Pixel Data in memory is filled on a copy and replaces the element. Pixel
// Data left in the file the data set was read from (dataelem.ValueInFile) is
// not read here: the element becomes a value that fills the regions as a
// writer copies it from the file, a chunk at a time, so no more than a chunk
// of the image is ever held.
func (ds *Dataset) redact(frame int, regions []PixelRegion) error {
	if len(regions) == 0 {
		return fmt.Errorf("redact: no region given")
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("redact: the data set has no Pixel Data (7FE0,0010)")
	}
	if p := pendingOf(elem); p != nil {
		// Already to be filled as it is written, and now with more.
		fill, err := newRegionFill(p.view, p.infoAfter, frame, regions, p.size())
		if err != nil {
			return err
		}
		elem.SetValue(p.withFill(fill).stream())
		return nil
	}
	if ref, inFile := elem.GetValue().(dataelem.ValueInFile); inFile {
		if err := ds.refuseEncapsulatedInFile(); err != nil {
			return fmt.Errorf("redact: %w", err)
		}
		fill, err := newRegionFill(ds, info, frame, regions, ref.Length)
		if err != nil {
			return err
		}
		vr := ds.ResolveVR(pixelData, elem)
		as := ds.cloneWithoutPixelData()
		p := &pendingRedaction{
			ref: ref, vr: vr, before: as, view: as,
			infoBefore: info, infoAfter: info, fills: []*regionFill{fill},
		}
		elem.SetVR(vr)
		elem.SetValue(p.stream())
		return nil
	}
	stored, err := extractBytesValue(elem)
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	if compression, encapsulated := ds.pixelCompression(stored, info); encapsulated {
		if compression == "" {
			return fmt.Errorf("redact: %w", ds.undecodedSyntax())
		}
		return fmt.Errorf("redact: the pixel data is compressed (%s); "+
			"it has to be decompressed before a region of it can be filled", compression)
	}
	fill, err := newRegionFill(ds, info, frame, regions, int64(len(stored)))
	if err != nil {
		return err
	}
	// A copy: the caller may still hold the slice the element was built from.
	out := append([]byte(nil), stored...)
	fill.apply(out, 0)
	elem.SetValue(out)
	return nil
}

// regionFill is where regions lie in the bytes of native Pixel Data, and
// what to write there. It fills any run of those bytes, given where in the
// value the run starts, so a frame, a chunk or a whole image is filled by
// the same code.
type regionFill struct {
	info *PixelDataInfo
	// regions are clipped to the image.
	regions []PixelRegion
	width   int
	// pixel is the bytes of one pixel's samples, adjacent; planes holds the
	// bytes of each sample alone, for an image with a plane for each
	// sample (Planar Configuration 1).
	pixel     []byte
	planes    [][]byte
	byPlane   bool
	planeSize int
	// first and last are the frames filled.
	first, last int
}

// newRegionFill works out where regions lie in an image of info, in frame
// (counted from 0) or in every frame when it is -1, and refuses what cannot
// be filled. stored is how many bytes of Pixel Data there are.
func newRegionFill(ds *Dataset, info *PixelDataInfo, frame int, regions []PixelRegion, stored int64) (*regionFill, error) {
	if frame >= info.NumberOfFrames {
		return nil, fmt.Errorf("redact: frame %d does not exist; the image has %d, counted from 0",
			frame, info.NumberOfFrames)
	}
	black, err := blackSamples(ds, info)
	if err != nil {
		return nil, err
	}
	if need := int64(info.BytesPerFrame) * int64(info.NumberOfFrames); stored < need {
		return nil, fmt.Errorf("redact: Pixel Data holds %d bytes and the image needs %d", stored, need)
	}
	f := &regionFill{
		info:      info,
		width:     info.BitsAllocated / 8,
		byPlane:   info.SamplesPerPixel > 1 && info.PlanarConfiguration == 1,
		planeSize: info.Rows * info.Columns * (info.BitsAllocated / 8),
		first:     0,
		last:      info.NumberOfFrames - 1,
	}
	if frame >= 0 {
		f.first, f.last = frame, frame
	}
	for _, r := range regions {
		c, ok := clip(r, info.Columns, info.Rows)
		if !ok {
			return nil, fmt.Errorf("redact: the region at (%d,%d), %d by %d, covers no pixel of a %d by %d image",
				r.X, r.Y, r.Width, r.Height, info.Columns, info.Rows)
		}
		f.regions = append(f.regions, c)
	}
	// Little endian, as the data set holds every sample whatever byte order
	// the file was in.
	for s := 0; s < info.SamplesPerPixel; s++ {
		sample := make([]byte, f.width)
		for b := range sample {
			sample[b] = byte(black[s] >> (8 * b))
		}
		f.planes = append(f.planes, sample)
		f.pixel = append(f.pixel, sample...)
	}
	return f, nil
}

// apply fills the regions in buf, which holds the bytes of Pixel Data from
// byte start on. Bytes of a region that lie outside buf are left to the run
// that holds them.
func (f *regionFill) apply(buf []byte, start int64) {
	if len(buf) == 0 {
		return
	}
	end := start + int64(len(buf))
	perFrame := int64(f.info.BytesPerFrame)
	cols, spp, width := int64(f.info.Columns), int64(f.info.SamplesPerPixel), int64(f.width)
	for frame := max(f.first, int(start/perFrame)); frame <= min(f.last, int((end-1)/perFrame)); frame++ {
		base := int64(frame) * perFrame
		for _, r := range f.regions {
			for y := int64(r.Y); y < int64(r.Y+r.Height); y++ {
				if !f.byPlane {
					// The samples of a pixel are adjacent.
					at := base + (y*cols+int64(r.X))*spp*width
					fillRun(buf, start, at, int64(r.Width)*spp*width, f.pixel)
					continue
				}
				for s := int64(0); s < spp; s++ {
					at := base + s*int64(f.planeSize) + (y*cols+int64(r.X))*width
					fillRun(buf, start, at, int64(r.Width)*width, f.planes[s])
				}
			}
		}
	}
}

// fillRun writes pattern, over and over from its start, into the length
// bytes that begin at byte at of the value, wherever they lie in buf, which
// holds the value from byte start on.
func fillRun(buf []byte, start, at, length int64, pattern []byte) {
	lo, hi := max(at, start), min(at+length, start+int64(len(buf)))
	for i := lo; i < hi; i++ {
		buf[i-start] = pattern[(i-at)%int64(len(pattern))]
	}
}

// clip returns the part of r inside a cols by rows image.
func clip(r PixelRegion, cols, rows int) (PixelRegion, bool) {
	if r.Width <= 0 || r.Height <= 0 {
		return PixelRegion{}, false
	}
	x0, y0 := max(r.X, 0), max(r.Y, 0)
	x1, y1 := min(r.X+r.Width, cols), min(r.Y+r.Height, rows)
	if x0 >= x1 || y0 >= y1 {
		return PixelRegion{}, false
	}
	return PixelRegion{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

// blackSamples returns the stored value of black for each sample of a pixel,
// and refuses a layout whose samples it cannot place.
func blackSamples(ds *Dataset, info *PixelDataInfo) ([]uint64, error) {
	switch info.BitsAllocated {
	case 8, 16, 32:
	default:
		return nil, fmt.Errorf("redact: %d bits allocated is not supported", info.BitsAllocated)
	}
	stored := info.BitsStored
	if stored <= 0 || stored > info.BitsAllocated {
		stored = info.BitsAllocated
	}

	lowest, highest := uint64(0), uint64(1)<<stored-1
	if info.PixelRepresentation == 1 {
		// Two's complement, sign extended through the unused high bits.
		lowest = ^uint64(0) << (stored - 1)
		highest = uint64(1)<<(stored-1) - 1
	}

	switch info.PhotometricInterpretation {
	case "MONOCHROME1":
		return []uint64{highest}, nil
	case "MONOCHROME2", "PALETTE COLOR", "":
		if info.SamplesPerPixel != 1 {
			return nil, fmt.Errorf("redact: %s with %d samples per pixel",
				info.PhotometricInterpretation, info.SamplesPerPixel)
		}
		if info.PhotometricInterpretation == "PALETTE COLOR" {
			return []uint64{darkestPaletteValue(ds, info, lowest)}, nil
		}
		return []uint64{lowest}, nil
	case "RGB":
		return []uint64{0, 0, 0}[:min(info.SamplesPerPixel, 3)], sampleCount(info, 3)
	case "YBR_FULL":
		// Zero luminance; chroma at its midpoint is no color.
		neutral := uint64(1) << (stored - 1)
		return []uint64{0, neutral, neutral}, sampleCount(info, 3)
	default:
		// YBR_FULL_422 shares chroma between two pixels, so a pixel's samples
		// are not where the loop above would write them.
		return nil, fmt.Errorf("redact: photometric interpretation %s is not supported",
			info.PhotometricInterpretation)
	}
}

func sampleCount(info *PixelDataInfo, want int) error {
	if info.SamplesPerPixel != want {
		return fmt.Errorf("redact: %s with %d samples per pixel", info.PhotometricInterpretation, info.SamplesPerPixel)
	}
	return nil
}

// darkestPaletteValue returns the stored value whose palette entry is the
// darkest, which is what black is in a PALETTE COLOR image: the samples are
// indexes, and index 0 is whatever color the palette gives it (#242). When
// the tables cannot be read there is nothing to choose by, and fallback is
// returned.
func darkestPaletteValue(ds *Dataset, info *PixelDataInfo, fallback uint64) uint64 {
	r := &renderer{ds: ds, info: info}
	var tables [3]paletteTable
	for c := range tables {
		table, err := r.paletteTable(uint16(c))
		if err != nil || len(table.entries) == 0 {
			return fallback
		}
		tables[c] = table
	}
	if tables[0].first != tables[1].first || tables[0].first != tables[2].first ||
		len(tables[0].entries) != len(tables[1].entries) || len(tables[0].entries) != len(tables[2].entries) {
		return fallback
	}

	darkest, least := 0, 256*1000
	for i := range tables[0].entries {
		// Luminance in thousandths, by the weights of PS3.3 C.7.6.3.1.2.
		l := 299*int(tables[0].entries[i]) + 587*int(tables[1].entries[i]) + 114*int(tables[2].entries[i])
		if l < least {
			darkest, least = i, l
		}
	}
	value := tables[0].first + darkest
	if value < 0 {
		// Two's complement in the stored width, as signed samples are held.
		return uint64(int64(value))
	}
	return uint64(value)
}
