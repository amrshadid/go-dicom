package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// jpeg2000Syntax is JPEG 2000 Image Compression, lossy or not (PS3.5 A.4.4).
const jpeg2000Syntax = "1.2.840.10008.1.2.4.91"

// CompressJPEG2000InPlace replaces the Pixel Data with lossy JPEG 2000 frames
// under 1.2.840.10008.1.2.4.91 and makes the data set describe them: the
// irreversible 9/7 wavelet and scalar quantization, at the compression ratio
// or the PSNR rate gives, exactly one of them.
//
// The image is 8 or 16 bits allocated, of any number of samples per pixel,
// signed or unsigned: JPEG 2000 codes signed samples as signed, so a CT keeps
// its negative values. Compressed pixel data is decoded first. A frame is
// coded at Bits Stored, or at Bits Allocated when a sample uses the bits
// above it.
//
// What changes besides the element:
//
//   - the transfer syntax, with one fragment for each frame and a Basic
//     Offset Table;
//   - an RGB image is coded with the irreversible component transform and
//     becomes YBR_ICT, as PS3.5 8.2.4 has it, with Planar Configuration 0;
//     decoding it returns RGB. Three samples that are not red, green and
//     blue are coded component by component and keep their Photometric
//     Interpretation;
//   - Lossy Image Compression (0028,2110) becomes 01, and this compression's
//     ratio and method, ISO_15444_1, are added to any already recorded
//     (PS3.3 C.7.6.1.1.5).
//
// The SOP Instance UID is not changed: a lossy copy is a different instance,
// and whether this is one is for the caller to say. An image already in this
// syntax is left alone, and on an error nothing is changed.
func (ds *Dataset) CompressJPEG2000InPlace(rate compress.JPEG2000Rate) error {
	if (rate.Ratio > 0) == (rate.PSNR > 0) || rate.Ratio < 0 || rate.PSNR < 0 {
		return fmt.Errorf("compress: lossy JPEG 2000 takes a ratio or a PSNR, one of them and above zero")
	}
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("compress: the data set has no Pixel Data (7FE0,0010)")
	}
	if ds.TransferSyntaxUID() == jpeg2000Syntax {
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
	if info.BitsAllocated != 8 && info.BitsAllocated != 16 {
		return fmt.Errorf("compress: JPEG 2000 is written for 8 and 16 bits allocated, and the image is %d",
			info.BitsAllocated)
	}
	if info.Rows < 1 || info.Columns < 1 || info.SamplesPerPixel < 1 || info.NumberOfFrames < 1 {
		return fmt.Errorf("compress: an image of %dx%d with %d samples per pixel in %d frames",
			info.Columns, info.Rows, info.SamplesPerPixel, info.NumberOfFrames)
	}
	if info.PhotometricInterpretation == "PALETTE COLOR" {
		// An index a little off is another color altogether.
		return fmt.Errorf("compress: a PALETTE COLOR image's samples are indexes, which a lossy coding would change")
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
	samples = samples[:frameLen*info.NumberOfFrames]

	signed := info.PixelRepresentation == 1
	depth := compress.JPEG2000Depth(samples, info.BitsAllocated, info.BitsStored, signed)
	rgb := info.SamplesPerPixel == 3 && info.PhotometricInterpretation == "RGB"

	frames := make([][]byte, info.NumberOfFrames)
	compressed := 0
	for f := range frames {
		frames[f], err = compress.EncodeJPEG2000(samples[f*frameLen:(f+1)*frameLen],
			info.Columns, info.Rows, info.SamplesPerPixel, info.BitsAllocated, depth, signed, rgb, rate)
		if err != nil {
			return fmt.Errorf("compress: frame %d: %w", f, err)
		}
		compressed += len(frames[f]) + len(frames[f])%2
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
	ds.SetTransferSyntaxUID(jpeg2000Syntax)
	switch {
	case rgb:
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte("YBR_ICT "))
	case info.PhotometricInterpretation == "YBR_FULL_422":
		// Native 4:2:2 samples came back from pixelBytesForDecoding with
		// chroma for every pixel, and were coded so.
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte("YBR_FULL"))
	}
	if info.SamplesPerPixel > 1 {
		// The codestream says how its components are arranged (PS3.5 8.2.4).
		planar := make([]byte, 2)
		binary.LittleEndian.PutUint16(planar, 0)
		replaceElement(ds, tag.New(0x0028, 0x0006), dataelem.US, planar)
	}
	replaceElement(ds, tag.New(0x0028, 0x2110), dataelem.CS, []byte("01"))
	ratio := float64(frameLen*info.NumberOfFrames) / float64(max(compressed, 1))
	appendValue(ds, tag.New(0x0028, 0x2112), dataelem.DS, fmt.Sprintf("%.2f", ratio))
	appendValue(ds, tag.New(0x0028, 0x2114), dataelem.CS, "ISO_15444_1")
	return nil
}
