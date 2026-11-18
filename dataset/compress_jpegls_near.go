package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// jpeglsNearLosslessSyntax is JPEG-LS Lossless and Near-Lossless (PS3.5 A.4.4).
const jpeglsNearLosslessSyntax = "1.2.840.10008.1.2.4.81"

// CompressJPEGLSNearLosslessInPlace replaces the Pixel Data with JPEG-LS
// frames under 1.2.840.10008.1.2.4.81 in which every sample decodes to within
// near of its value, and makes the data set describe them. near is the
// allowed error NEAR of ITU-T T.87; 0 codes the image losslessly.
//
// The image is 8 or 16 bits allocated, of any number of samples per pixel.
// A signed image is coded only at near 0: JPEG-LS codes samples as unsigned
// numbers, and an error allowed on the two's complement bits would carry a
// sample at the top of the signed range to the bottom, an error of the whole
// range. dcmtk refuses the same images for the same reason. Compressed pixel
// data is decoded first.
//
// What changes besides the element:
//
//   - the transfer syntax, with one fragment for each frame and a Basic
//     Offset Table;
//   - Planar Configuration becomes 0. Color is coded as it is, component by
//     component, so an RGB image stays RGB;
//   - when near is above 0, Lossy Image Compression (0028,2110) becomes 01,
//     and this compression's ratio and method, ISO_14495_1, are added to any
//     already recorded (PS3.3 C.7.6.1.1.5). At near 0 nothing was lost and
//     they are not touched.
//
// The SOP Instance UID is not changed: a lossy copy is a different instance
// (PS3.3 C.7.6.1.1.5), and whether this is one is for the caller to say. An
// image already in this syntax is left alone, whatever NEAR it was coded
// with, and on an error nothing is changed.
func (ds *Dataset) CompressJPEGLSNearLosslessInPlace(near int) error {
	if near < 0 || near > 255 {
		return fmt.Errorf("compress: NEAR %d; it runs from 0 to 255", near)
	}
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("compress: the data set has no Pixel Data (7FE0,0010)")
	}
	if ds.TransferSyntaxUID() == jpeglsNearLosslessSyntax {
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
		return fmt.Errorf("compress: JPEG-LS is written for 8 and 16 bits allocated, and the image is %d",
			info.BitsAllocated)
	}
	if info.Rows < 1 || info.Columns < 1 || info.SamplesPerPixel < 1 || info.NumberOfFrames < 1 {
		return fmt.Errorf("compress: an image of %dx%d with %d samples per pixel in %d frames",
			info.Columns, info.Rows, info.SamplesPerPixel, info.NumberOfFrames)
	}
	signed := info.PixelRepresentation == 1
	if signed && near > 0 {
		return fmt.Errorf("compress: a signed image is coded as JPEG-LS only losslessly, at NEAR 0: " +
			"an error on its two's complement bits wraps the top of the range to the bottom")
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

	// One precision for the image: Bits Stored, or Bits Allocated when a
	// sample uses the bits above. Not below 2, which T.87 does not have, nor
	// below 9 for samples of two bytes, which the encoder reads as one byte
	// each at 8 bits or fewer (#364) and a decoder returns one byte each
	// (#362).
	depth := max(compress.JPEG2000Depth(samples, info.BitsAllocated, info.BitsStored, signed), 2)
	if info.BitsAllocated == 16 {
		depth = max(depth, 9)
	}
	if signed {
		// A signed sample is coded as its two's complement bits, the low
		// depth of them (PS3.5 8.2.3); the reader extends the sign again.
		samples = lowBits(samples, info.BitsAllocated, depth)
	}
	if limit := (1<<uint(depth) - 1) / 2; near > limit {
		return fmt.Errorf("compress: NEAR %d is more than half the largest sample of %d bits", near, depth)
	}

	frames := make([][]byte, info.NumberOfFrames)
	compressed := 0
	for f := range frames {
		frames[f], err = compress.EncodeJPEGLSNearLossless(samples[f*frameLen:(f+1)*frameLen],
			info.Columns, info.Rows, info.SamplesPerPixel, depth, near)
		if err != nil {
			return fmt.Errorf("compress: frame %d: %w", f, err)
		}
		// A fragment is of even length.
		if len(frames[f])%2 == 1 {
			frames[f] = append(frames[f], 0x00)
		}
		compressed += len(frames[f])
	}

	// Everything that can fail has; from here the data set changes.
	elem.SetVR(dataelem.OB) // encapsulated pixel data is OB (PS3.5 A.4)
	if err := ds.SetValue(pixelData, encapsulateFrames(frames)); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	ds.SetTransferSyntaxUID(jpeglsNearLosslessSyntax)
	ds.describeCoded(source, info, "")
	if near > 0 {
		replaceElement(ds, tag.New(0x0028, 0x2110), dataelem.CS, []byte("01"))
		ratio := float64(frameLen*info.NumberOfFrames) / float64(compressed)
		appendValue(ds, tag.New(0x0028, 0x2112), dataelem.DS, fmt.Sprintf("%.2f", ratio))
		appendValue(ds, tag.New(0x0028, 0x2114), dataelem.CS, "ISO_14495_1")
	}
	return nil
}

// lowBits keeps the low depth bits of each sample, little endian samples of
// bitsAllocated bits, and clears the rest.
func lowBits(samples []byte, bitsAllocated, depth int) []byte {
	out := make([]byte, len(samples))
	if bitsAllocated == 8 {
		mask := byte(1<<uint(depth) - 1)
		for i, v := range samples {
			out[i] = v & mask
		}
		return out
	}
	mask := uint16(1<<uint(depth) - 1)
	for i := 0; i+1 < len(samples); i += 2 {
		binary.LittleEndian.PutUint16(out[i:], binary.LittleEndian.Uint16(samples[i:])&mask)
	}
	return out
}
