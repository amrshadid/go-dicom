package dataset

import (
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// The two lossless JPEG syntaxes, ITU-T T.81 Process 14 (PS3.5 A.4.1).
const (
	// jpegLosslessSyntax allows any predictor.
	jpegLosslessSyntax = "1.2.840.10008.1.2.4.57"
	// jpegLosslessSV1Syntax is fixed to predictor 1, the sample to the left.
	jpegLosslessSV1Syntax = "1.2.840.10008.1.2.4.70"
)

// CompressJPEGLosslessInPlace replaces the Pixel Data with lossless JPEG
// (Process 14) frames under 1.2.840.10008.1.2.4.57 and makes the data set
// describe them. Each frame is coded with the predictor of the seven that
// codes it smallest, which this syntax, unlike .70, allows.
//
// The coding is exact: decoding returns every sample as it was. See
// CompressJPEGLosslessSV1InPlace for what it accepts and what changes.
func (ds *Dataset) CompressJPEGLosslessInPlace() error {
	return ds.compressJPEGLossless(jpegLosslessSyntax, 0)
}

// CompressJPEGLosslessSV1InPlace replaces the Pixel Data with lossless JPEG
// (Process 14, Selection Value 1) frames under 1.2.840.10008.1.2.4.70 and
// makes the data set describe them. Each sample is predicted from the one to
// its left, which is what this syntax fixes and what the most decoders in
// service read.
//
// The coding is exact: decoding returns every sample as it was. The image is
// 8 or 16 bits allocated, of one to four samples per pixel, signed or
// unsigned. Compressed pixel data is decoded first, so an image can be moved
// from another syntax to this one.
//
// What changes besides the element:
//
//   - the transfer syntax, with one fragment for each frame and a Basic
//     Offset Table;
//   - Planar Configuration becomes 0, since a frame's components are coded
//     interleaved. Color is coded as it is: lossless JPEG has no color
//     transform, so an RGB image stays RGB.
//
// Each frame is coded at Bits Stored, or at Bits Allocated when a sample uses
// the bits above Bits Stored: they are meant to be empty and in old files are
// not always, and a lossless coding returns what it was given. A signed
// sample is coded as its two's complement bits (PS3.5 8.2.1).
//
// Lossy Image Compression is not touched: nothing was lost here, and an image
// that was lossily compressed before is still one that was. An image already
// in the syntax is left alone, and on an error nothing is changed.
func (ds *Dataset) CompressJPEGLosslessSV1InPlace() error {
	return ds.compressJPEGLossless(jpegLosslessSV1Syntax, 1)
}

// compressJPEGLossless is both methods: predictor 0 chooses for each frame.
func (ds *Dataset) compressJPEGLossless(syntax string, predictor int) error {
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("compress: the data set has no Pixel Data (7FE0,0010)")
	}
	if ds.TransferSyntaxUID() == syntax {
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
		return fmt.Errorf("compress: lossless JPEG is written for 8 and 16 bits allocated, and the image is %d",
			info.BitsAllocated)
	}
	if info.Rows < 1 || info.Columns < 1 || info.SamplesPerPixel < 1 || info.NumberOfFrames < 1 {
		return fmt.Errorf("compress: an image of %dx%d with %d samples per pixel in %d frames",
			info.Columns, info.Rows, info.SamplesPerPixel, info.NumberOfFrames)
	}

	// Pixel by pixel and little endian, whatever the element holds.
	samples, err := source.pixelBytesForDecoding(info)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	frameLen := info.Rows * info.Columns * info.SamplesPerPixel * info.BitsAllocated / 8
	if len(samples) < frameLen*info.NumberOfFrames {
		return fmt.Errorf("compress: the image needs %d bytes and the pixel data holds %d",
			frameLen*info.NumberOfFrames, len(samples))
	}
	// Past the last frame is padding, which is not coded.
	samples = samples[:frameLen*info.NumberOfFrames]

	signed := info.PixelRepresentation == 1
	// One precision for the image, so every frame is the same kind of
	// codestream. T.81 has none below 2 bits. And none below 9 for samples
	// of two bytes: a decoder sizes its output by the precision, and a
	// 16-bit image coded at 8 bits came back from this library's own decoder
	// one byte a sample.
	depth := max(compress.JPEG2000Depth(samples, info.BitsAllocated, info.BitsStored, signed), 2)
	if info.BitsAllocated == 16 {
		depth = max(depth, 9)
	}

	frames := make([][]byte, info.NumberOfFrames)
	for f := range frames {
		frames[f], err = compress.EncodeJPEGLossless(samples[f*frameLen:(f+1)*frameLen],
			info.Columns, info.Rows, info.SamplesPerPixel, info.BitsAllocated, depth, signed, predictor)
		if err != nil {
			return fmt.Errorf("compress: frame %d: %w", f, err)
		}
	}

	// Everything that can fail has; from here the data set changes.
	elem.SetVR(dataelem.OB) // encapsulated pixel data is OB (PS3.5 A.4)
	if err := ds.SetValue(pixelData, encapsulateFrames(frames)); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	ds.SetTransferSyntaxUID(syntax)
	ds.describeCoded(source, info, "")
	return nil
}
