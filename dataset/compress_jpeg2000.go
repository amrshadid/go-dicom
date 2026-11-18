package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// jpeg2000LosslessSyntax is JPEG 2000 Image Compression (Lossless Only).
const jpeg2000LosslessSyntax = "1.2.840.10008.1.2.4.90"

// CompressJPEG2000LosslessInPlace replaces the Pixel Data with lossless JPEG
// 2000 frames and makes the data set describe them.
//
// The coding is exact: decoding returns every sample as it was. The image is
// 8 or 16 bits allocated, of any number of samples per pixel, signed or
// unsigned. Compressed pixel data is decoded first, so an image can be moved
// from another syntax to this one.
//
// What changes besides the element:
//
//   - the transfer syntax becomes 1.2.840.10008.1.2.4.90, with one fragment
//     for each frame and a Basic Offset Table;
//   - an RGB image is coded with the reversible component transform and
//     becomes YBR_RCT, which is how PS3.5 8.2.4 has it described, with Planar
//     Configuration 0. Decoding it returns RGB. An image of three samples
//     that are not red, green and blue is coded component by component and
//     keeps its Photometric Interpretation.
//
// Each frame is coded at Bits Stored, or at Bits Allocated when a sample
// uses the bits above Bits Stored: they are meant to be empty and in old
// files are not always, and a lossless coding returns what it was given.
//
// Lossy Image Compression is not touched: nothing was lost here, and an
// image that was lossily compressed before is still one that was. An image
// already in this syntax is left alone, and on an error nothing is changed.
func (ds *Dataset) CompressJPEG2000LosslessInPlace() error {
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("compress: the data set has no Pixel Data (7FE0,0010)")
	}
	if ds.TransferSyntaxUID() == jpeg2000LosslessSyntax {
		return nil
	}

	// The decoded image, described as decoded, on a copy when decoding is
	// needed so that a refusal below leaves the caller's data set alone.
	source, err := ds.decodedForCompression(elem)
	if err != nil {
		return err
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
	// One depth for the image, so every frame is the same kind of codestream.
	depth := compress.JPEG2000Depth(samples, info.BitsAllocated, info.BitsStored, signed)
	rgb := info.SamplesPerPixel == 3 && info.PhotometricInterpretation == "RGB"

	frames := make([][]byte, info.NumberOfFrames)
	for f := range frames {
		frames[f], err = compress.EncodeJPEG2000Lossless(samples[f*frameLen:(f+1)*frameLen],
			info.Columns, info.Rows, info.SamplesPerPixel, info.BitsAllocated, depth, signed, rgb)
		if err != nil {
			return fmt.Errorf("compress: frame %d: %w", f, err)
		}
	}

	// Everything that can fail has; from here the data set changes. When the
	// image was decoded first, what describes it has to be the decoded
	// image's description: a 4:2:2 JPEG decodes to chroma for every pixel.
	ds.takeDescription(source)
	elem.SetVR(dataelem.OB) // encapsulated pixel data is OB (PS3.5 A.4)
	if err := ds.SetValue(pixelData, encapsulateFrames(frames)); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	ds.SetTransferSyntaxUID(jpeg2000LosslessSyntax)
	if rgb {
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte("YBR_RCT "))
	}
	if info.SamplesPerPixel > 1 {
		// The codestream says how its components are arranged, so Planar
		// Configuration has nothing to say and is 0 (PS3.5 8.2.4).
		planar := make([]byte, 2)
		binary.LittleEndian.PutUint16(planar, 0)
		replaceElement(ds, tag.New(0x0028, 0x0006), dataelem.US, planar)
	}
	return nil
}
