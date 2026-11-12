package dataset

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// jpegBaselineSyntax is JPEG Baseline (Process 1).
const jpegBaselineSyntax = "1.2.840.10008.1.2.4.50"

// CompressJPEGBaselineInPlace replaces the Pixel Data with JPEG Baseline
// frames and makes the data set describe them.
//
// quality runs from 1 to 100. The coding is lossy at every quality.
//
// Baseline holds 8-bit unsigned samples, so the image has to be 8 bits
// allocated: MONOCHROME1, MONOCHROME2, RGB or YBR_FULL. A deeper image is
// refused, since narrowing it would be a second loss made without being
// asked, and so is PALETTE COLOR, whose samples are indexes. Compressed
// pixel data is decoded first.
//
// What changes besides the element:
//
//   - the transfer syntax becomes JPEG Baseline, with one fragment per frame
//     and a Basic Offset Table;
//   - a color image becomes YBR_FULL, each component coded at full
//     resolution, with Planar Configuration 0;
//   - Lossy Image Compression (0028,2110) becomes 01, and this compression's
//     ratio and method, ISO_10918_1, are added to any already recorded.
//
// The SOP Instance UID is not changed. A lossy copy of an image is a
// different instance and needs its own (PS3.3 C.7.6.1.1.5); whether this is
// such a copy is for the caller to say. An image already in this syntax is
// left alone, and on an error nothing is changed.
func (ds *Dataset) CompressJPEGBaselineInPlace(quality int) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("compress: quality %d; it runs from 1 to 100", quality)
	}
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("compress: the data set has no Pixel Data (7FE0,0010)")
	}
	if ds.TransferSyntaxUID() == jpegBaselineSyntax {
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
	if info.BitsAllocated != 8 || info.PixelRepresentation != 0 {
		return fmt.Errorf("compress: JPEG Baseline holds 8-bit unsigned samples, and the image is "+
			"%d bits allocated, pixel representation %d", info.BitsAllocated, info.PixelRepresentation)
	}
	var rgb bool
	switch p := info.PhotometricInterpretation; {
	case info.SamplesPerPixel == 1 && (p == "MONOCHROME1" || p == "MONOCHROME2"):
	case info.SamplesPerPixel == 3 && p == "RGB":
		rgb = true
	case info.SamplesPerPixel == 3 && (p == "YBR_FULL" || p == "YBR_FULL_422"):
		// 4:2:2 native data comes back from pixelBytesForDecoding with
		// chroma for every pixel, which is YBR_FULL.
	default:
		return fmt.Errorf("compress: a %s image of %d samples per pixel cannot be coded as JPEG Baseline",
			p, info.SamplesPerPixel)
	}

	samples, err := source.pixelBytesForDecoding(info)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	frameLen := info.Rows * info.Columns * info.SamplesPerPixel
	if len(samples) < frameLen*info.NumberOfFrames {
		return fmt.Errorf("compress: the image needs %d bytes and the pixel data holds %d",
			frameLen*info.NumberOfFrames, len(samples))
	}

	frames := make([][]byte, info.NumberOfFrames)
	compressed := 0
	for f := range frames {
		frame := samples[f*frameLen : (f+1)*frameLen]
		if rgb {
			frame = rgbToYBR(frame)
		}
		frames[f], err = compress.EncodeJPEGBaseline(frame, info.Columns, info.Rows, info.SamplesPerPixel, quality)
		if err != nil {
			return fmt.Errorf("compress: frame %d: %w", f, err)
		}
		compressed += len(frames[f])
	}

	// Everything that can fail has; from here the data set changes.
	elem.SetVR(dataelem.OB) // encapsulated pixel data is OB (PS3.5 A.4)
	if err := ds.SetValue(pixelData, encapsulateFrames(frames)); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	ds.SetTransferSyntaxUID(jpegBaselineSyntax)
	if info.SamplesPerPixel == 3 {
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte("YBR_FULL"))
		planar := make([]byte, 2)
		binary.LittleEndian.PutUint16(planar, 0)
		replaceElement(ds, tag.New(0x0028, 0x0006), dataelem.US, planar)
	}
	replaceElement(ds, tag.New(0x0028, 0x2110), dataelem.CS, []byte("01"))
	ratio := float64(frameLen*info.NumberOfFrames) / float64(compressed)
	appendValue(ds, tag.New(0x0028, 0x2112), dataelem.DS, fmt.Sprintf("%.2f", ratio))
	appendValue(ds, tag.New(0x0028, 0x2114), dataelem.CS, "ISO_10918_1")
	return nil
}

// rgbToYBR converts interleaved RGB samples to YBR_FULL by the equations of
// PS3.3 C.7.6.3.1.2.
func rgbToYBR(rgb []byte) []byte {
	out := make([]byte, len(rgb))
	for i := 0; i+2 < len(rgb); i += 3 {
		r, g, b := float64(rgb[i]), float64(rgb[i+1]), float64(rgb[i+2])
		out[i] = clamp8(0.299*r + 0.587*g + 0.114*b)
		out[i+1] = clamp8(-0.168736*r - 0.331264*g + 0.5*b + 128)
		out[i+2] = clamp8(0.5*r - 0.418688*g - 0.081312*b + 128)
	}
	return out
}

// encapsulateFrames builds an encapsulated Pixel Data value: the Basic
// Offset Table and one fragment per frame (PS3.5 A.4). Each table entry is
// where its frame's item starts, counted from the first fragment's item tag,
// item headers included.
//
// The sequence delimiter that closes the element is not part of the value:
// the file writer and the network encoder each write it, and one here as
// well made a file dcmtk refused for having two.
func encapsulateFrames(frames [][]byte) []byte {
	size := 8 + 4*len(frames)
	for _, frame := range frames {
		size += 8 + len(frame) + len(frame)%2
	}
	out := make([]byte, 0, size)
	item := func(length int) {
		out = append(out, 0xFE, 0xFF, 0x00, 0xE0)
		out = binary.LittleEndian.AppendUint32(out, uint32(length))
	}

	// A table entry is 32 bits. Where the frames run past that the table is
	// left empty, which is allowed: a wrong entry is worse than a reader
	// finding the frames itself.
	if size-8 > math.MaxUint32 {
		item(0)
	} else {
		item(4 * len(frames))
		offset := 0
		for _, frame := range frames {
			out = binary.LittleEndian.AppendUint32(out, uint32(offset))
			offset += 8 + len(frame) + len(frame)%2
		}
	}
	for _, frame := range frames {
		item(len(frame) + len(frame)%2)
		out = append(out, frame...)
		if len(frame)%2 == 1 {
			out = append(out, 0x00)
		}
	}
	return out
}

// appendValue adds a value to a multi-valued text attribute, creating it
// when the data set has none.
func appendValue(ds *Dataset, t tag.Tag, vr dataelem.VR, value string) {
	if elem, ok := ds.Get(t); ok {
		if existing, err := extractStringValue(elem); err == nil && existing != "" {
			value = existing + `\` + value
		}
	}
	if len(value)%2 == 1 {
		value += " "
	}
	replaceElement(ds, t, vr, []byte(value))
}
