package dataset

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// jpegExtendedSyntax is JPEG Extended (Process 2 and 4).
const jpegExtendedSyntax = "1.2.840.10008.1.2.4.51"

// CompressJPEGExtendedInPlace replaces the Pixel Data with JPEG Extended
// (ITU-T T.81 Processes 2 and 4) frames and makes the data set describe them:
// the lossy JPEG for images of up to 12 bits.
//
// quality runs from 1 to 100 and means what it means for
// CompressJPEGBaselineInPlace and for libjpeg. The coding is lossy at every
// quality.
//
// An image of 16 bits allocated and no more than 12 stored, MONOCHROME1 or
// MONOCHROME2, is coded at 12 bits. An image of 8 bits allocated is coded at
// 8, as for JPEG Baseline: MONOCHROME1, MONOCHROME2, RGB, which becomes
// YBR_FULL, or YBR_FULL. Refused, by name:
//
//   - a signed image: the DCT codes unsigned samples, and two's complement
//     bits put -1 next to the top of the range, so the error near zero would
//     be the whole range. Turning it unsigned would change what the values
//     mean wherever no Modality LUT says how to read them;
//   - more than 12 bits stored: narrowing it would be a second loss nobody
//     asked for;
//   - color at 12 bits, and PALETTE COLOR, whose samples are indexes.
//
// Compressed pixel data is decoded first. What changes besides the element:
// the transfer syntax, with one fragment for each frame and a Basic Offset
// Table; Planar Configuration 0 for color; Bits Stored and High Bit become
// 12 and 11, or 8 and 7, when fewer bits were stored, since a decoded sample
// may land just above the old top and a reader masking to the old Bits Stored
// would wrap it to the bottom; Lossy Image Compression (0028,2110)
// becomes 01, and this compression's ratio and method, ISO_10918_1, are added
// to any already recorded (PS3.3 C.7.6.1.1.5).
//
// The SOP Instance UID is not changed. A lossy copy is a different instance
// and needs its own; whether this is one is for the caller to say. An image
// already in this syntax is left alone, and on an error nothing is changed.
func (ds *Dataset) CompressJPEGExtendedInPlace(quality int) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("compress: quality %d; it runs from 1 to 100", quality)
	}
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("compress: the data set has no Pixel Data (7FE0,0010)")
	}
	if ds.TransferSyntaxUID() == jpegExtendedSyntax {
		return nil
	}

	// The decoded image, described as decoded, on a copy when decoding is
	// needed so that a refusal below leaves the caller's data set alone.
	source := ds
	if stored, err := extractBytesValue(elem); err == nil {
		if info, err := ds.GetPixelDataInfo(); err == nil {
			if _, encapsulated := ds.pixelCompression(stored, info); encapsulated {
				source = ds.Clone()
				if err := source.DecompressInPlace(); err != nil {
					return fmt.Errorf("compress: %w", err)
				}
			}
		}
	}
	info, err := source.GetPixelDataInfo()
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	if info.PixelRepresentation != 0 {
		return fmt.Errorf("compress: JPEG Extended codes unsigned samples, and the image is signed: " +
			"two's complement through a DCT puts -1 beside the top of the range")
	}
	precision := 8
	switch {
	case info.BitsAllocated == 8:
	case info.BitsAllocated == 16 && info.BitsStored >= 1 && info.BitsStored <= 12:
		precision = 12
	default:
		return fmt.Errorf("compress: JPEG Extended holds 8 or 12 bits, and the image is %d bits allocated, "+
			"%d stored", info.BitsAllocated, info.BitsStored)
	}
	var rgb bool
	switch p := info.PhotometricInterpretation; {
	case info.SamplesPerPixel == 1 && (p == "MONOCHROME1" || p == "MONOCHROME2"):
	case precision == 8 && info.SamplesPerPixel == 3 && p == "RGB":
		rgb = true
	case precision == 8 && info.SamplesPerPixel == 3 && (p == "YBR_FULL" || p == "YBR_FULL_422"):
		// 4:2:2 native data comes back from pixelBytesForDecoding with
		// chroma for every pixel, which is YBR_FULL.
	default:
		return fmt.Errorf("compress: a %s image of %d samples per pixel at %d bits cannot be coded as "+
			"JPEG Extended", p, info.SamplesPerPixel, precision)
	}
	if info.Rows < 1 || info.Columns < 1 || info.NumberOfFrames < 1 {
		return fmt.Errorf("compress: an image of %dx%d in %d frames", info.Columns, info.Rows, info.NumberOfFrames)
	}

	samples, err := source.pixelBytesForDecoding(info)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	frameLen := info.Rows * info.Columns * info.SamplesPerPixel * info.BitsAllocated / 8
	if len(samples) < frameLen*info.NumberOfFrames {
		return fmt.Errorf("compress: the image needs %d bytes and the pixel data holds %d",
			frameLen*info.NumberOfFrames, len(samples))
	}
	if precision == 12 {
		// Bits above Bits Stored are not the pixel; a sample that uses them
		// cannot be coded in 12 bits without saying which of them it drops.
		limit := uint16(1<<uint(info.BitsStored) - 1)
		for i := 0; i+1 < frameLen*info.NumberOfFrames; i += 2 {
			if v := binary.LittleEndian.Uint16(samples[i:]); v > limit {
				return fmt.Errorf("compress: a sample of %d uses bits above the %d stored", v, info.BitsStored)
			}
		}
	}

	frames := make([][]byte, info.NumberOfFrames)
	compressed := 0
	for f := range frames {
		frame := samples[f*frameLen : (f+1)*frameLen]
		if rgb {
			frame = rgbToYBR(frame)
		}
		frames[f], err = compress.EncodeJPEGExtended(frame, info.Columns, info.Rows, info.SamplesPerPixel,
			precision, quality)
		if err != nil {
			return fmt.Errorf("compress: frame %d: %w", f, err)
		}
		compressed += len(frames[f])
	}

	// Everything that can fail has; from here the data set changes.
	if source != ds {
		for _, t := range []tag.Tag{tag.New(0x0028, 0x0004), tag.New(0x0028, 0x0006)} {
			if described, ok := source.Get(t); ok {
				ds.Remove(t)
				_ = ds.Add(described)
			}
		}
	}
	elem.SetVR(dataelem.OB) // encapsulated pixel data is OB (PS3.5 A.4)
	if err := ds.SetValue(pixelData, encapsulateFrames(frames)); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	ds.SetTransferSyntaxUID(jpegExtendedSyntax)
	if info.BitsStored != precision {
		// A decoder returns samples of the codestream's precision, and the
		// coding error can carry one past the top of fewer bits stored, where
		// a reader that masks to Bits Stored would wrap it to the bottom.
		// The samples are unchanged in value; the bits they may use are now
		// the codestream's.
		for element, v := range map[uint16]int{0x0101: precision, 0x0102: precision - 1} {
			b := make([]byte, 2)
			binary.LittleEndian.PutUint16(b, uint16(v))
			replaceElement(ds, tag.New(0x0028, element), dataelem.US, b)
		}
	}
	if info.SamplesPerPixel == 3 {
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte("YBR_FULL"))
		planar := make([]byte, 2)
		binary.LittleEndian.PutUint16(planar, 0)
		replaceElement(ds, tag.New(0x0028, 0x0006), dataelem.US, planar)
	}
	replaceElement(ds, tag.New(0x0028, 0x2110), dataelem.CS, []byte("01"))
	ratio := float64(frameLen*info.NumberOfFrames) / math.Max(float64(compressed), 1)
	appendValue(ds, tag.New(0x0028, 0x2112), dataelem.DS, fmt.Sprintf("%.2f", ratio))
	appendValue(ds, tag.New(0x0028, 0x2114), dataelem.CS, "ISO_10918_1")
	return nil
}
