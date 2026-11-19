package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// The two lossless syntaxes written here besides JPEG 2000.
const (
	rleLosslessSyntax    = "1.2.840.10008.1.2.5"
	jpeglsLosslessSyntax = "1.2.840.10008.1.2.4.80"
)

// CompressRLELosslessInPlace replaces the Pixel Data with RLE Lossless frames
// (PS3.5 Annex G) and makes the data set describe them.
//
// The coding is exact. The image is any whole number of bytes per sample, of
// as many samples per pixel as fit the fifteen segments an RLE header holds:
// three samples of 32 bits, or one of 64 at most. Compressed pixel data is
// decoded first, so an image can be moved from another syntax to this one.
//
// What changes besides the element: the transfer syntax becomes RLE
// Lossless, with one fragment per frame and a Basic Offset Table; a color
// image has Planar Configuration 0, which is how the decoders of this
// library, pydicom and dcmtk hand RLE back, and a native YBR_FULL_422 image,
// whose chroma is expanded for every pixel before coding, becomes YBR_FULL.
//
// Nothing was lost, so Lossy Image Compression and the SOP Instance UID are
// not touched. An image already in this syntax is left alone, and on an error
// nothing is changed.
func (ds *Dataset) CompressRLELosslessInPlace() error {
	return ds.compressFramesInPlace(rleLosslessSyntax, "RLE Lossless",
		func(info *PixelDataInfo, _ []byte) (frameCoder, error) {
			if info.BitsAllocated < 8 || info.BitsAllocated%8 != 0 {
				return nil, fmt.Errorf("RLE codes whole bytes, and the image is %d bits allocated", info.BitsAllocated)
			}
			rle := compress.NewRLECompressor()
			return func(frame []byte) ([]byte, error) {
				return rle.CompressFrame(frame, info.SamplesPerPixel, info.BitsAllocated)
			}, nil
		})
}

// CompressJPEGLSLosslessInPlace replaces the Pixel Data with JPEG-LS Lossless
// frames (ISO/IEC 14495-1, NEAR 0) and makes the data set describe them.
//
// The coding is exact. The image is 8 or 16 bits allocated, of any number of
// samples per pixel, each coded as a scan of its own. Compressed pixel data is
// decoded first.
//
// The codestream's precision is Bits Stored when every sample fits in it, and
// Bits Allocated when one does not: the bits above Bits Stored are meant to
// be empty, and in old files, or in a signed image whose samples are sign
// extended, they are not, and a lossless coding returns what it was given. A
// 16-bit image is coded at no less than 9 bits, so that a decoder returns two
// bytes a sample, as the data set describes.
//
// What changes besides the element is what CompressRLELosslessInPlace
// changes: the transfer syntax, one fragment per frame with a Basic Offset
// Table, Planar Configuration 0 for color, and YBR_FULL for a native
// YBR_FULL_422 image. Lossy Image Compression and the SOP Instance UID are
// not touched. An image already in this syntax is left alone, and on an
// error nothing is changed.
func (ds *Dataset) CompressJPEGLSLosslessInPlace() error {
	return ds.compressFramesInPlace(jpeglsLosslessSyntax, "JPEG-LS Lossless",
		func(info *PixelDataInfo, samples []byte) (frameCoder, error) {
			if info.BitsAllocated != 8 && info.BitsAllocated != 16 {
				return nil, fmt.Errorf("JPEG-LS is written for 8 and 16 bits allocated, and the image is %d",
					info.BitsAllocated)
			}
			// Unsigned: JPEG-LS codes P-bit unsigned samples, so a stored bit
			// pattern is what has to fit, whatever Pixel Representation says.
			precision := compress.JPEG2000Depth(samples, info.BitsAllocated, info.BitsStored, false)
			if info.BitsAllocated == 16 && precision < 9 {
				precision = 9
			}
			if precision < 2 {
				precision = 2 // T.87 starts at 2 bits
			}
			return func(frame []byte) ([]byte, error) {
				return compress.EncodeJPEGLS(frame, info.Columns, info.Rows, info.SamplesPerPixel, precision)
			}, nil
		})
}

// frameCoder codes one frame of pixel-interleaved, little endian samples.
type frameCoder func(frame []byte) ([]byte, error)

// compressFramesInPlace is the work the lossless coders share: decode a
// compressed source, take its samples interleaved by pixel, code each frame,
// and make the data set describe the result. plan sees the image and all of
// its samples before any frame is coded, and refuses what its coder cannot
// write.
func (ds *Dataset) compressFramesInPlace(syntax, name string,
	plan func(info *PixelDataInfo, samples []byte) (frameCoder, error)) error {
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return fmt.Errorf("compress: the data set has no Pixel Data (7FE0,0010)")
	}
	if ds.TransferSyntaxUID() == syntax {
		return nil
	}

	source, err := ds.decodedForCompression(elem)
	if err != nil {
		return err
	}
	info, err := source.GetPixelDataInfo()
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	if info.Rows < 1 || info.Columns < 1 || info.SamplesPerPixel < 1 || info.NumberOfFrames < 1 {
		return fmt.Errorf("compress: an image of %dx%d with %d samples per pixel in %d frames",
			info.Columns, info.Rows, info.SamplesPerPixel, info.NumberOfFrames)
	}
	if info.BitsAllocated < 8 || info.BitsAllocated%8 != 0 {
		return fmt.Errorf("compress: %s is not written for %d bits allocated", name, info.BitsAllocated)
	}

	// Pixel by pixel and little endian, whatever the element holds: Planar
	// Configuration 1 and native 4:2:2 are rearranged here, where giving the
	// coders the stored bytes would code a layout they do not expect.
	samples, err := source.pixelBytesForDecoding(info)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	frameLen := info.Rows * info.Columns * info.SamplesPerPixel * info.BitsAllocated / 8
	if len(samples) < frameLen*info.NumberOfFrames {
		return fmt.Errorf("compress: the image needs %d bytes and the pixel data holds %d",
			frameLen*info.NumberOfFrames, len(samples))
	}
	// Past the last frame is padding, which is not coded: a frame that decodes
	// longer than the image is refused by pydicom.
	samples = samples[:frameLen*info.NumberOfFrames]

	code, err := plan(info, samples)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	frames := make([][]byte, info.NumberOfFrames)
	for f := range frames {
		if frames[f], err = code(samples[f*frameLen : (f+1)*frameLen]); err != nil {
			return fmt.Errorf("compress: %s frame %d: %w", name, f, err)
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

// decodedForCompression returns the image to code: ds itself when its pixel
// data is native, and a decoded copy when it is encapsulated, so that a
// refusal later leaves the caller's data set alone.
func (ds *Dataset) decodedForCompression(elem *dataelem.DataElement) (*Dataset, error) {
	stored, err := extractBytesValue(elem)
	if err != nil {
		return ds, nil
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return ds, nil
	}
	if _, encapsulated := ds.pixelCompression(stored, info); !encapsulated {
		return ds, nil
	}
	decoded := ds.Clone()
	if err := decoded.DecompressInPlace(); err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	return decoded, nil
}

// describeCoded makes ds describe the frames just coded from source, the
// image info describes. It is how every Compress...InPlace method describes
// its result, so that what one method gets right the others do too: the
// JPEG 2000 method once had its own copy, which left a native 4:2:2 image
// described as YBR_FULL_422 (#374).
//
// The samples coded are the ones pixelBytesForDecoding gives, and the
// description follows them:
//
//   - when the image was decoded first, its decoded description: a 4:2:2
//     JPEG decodes to chroma for every pixel, and a JPEG 2000 color transform
//     is undone;
//   - a native YBR_FULL_422 image had its chroma expanded to every pixel,
//     so it is YBR_FULL, which is what PS3.5 8.2 allows for full chroma in
//     every syntax written here, and JPEG 2000 allows nothing else
//     (Table 8.2.4-1);
//   - color is coded pixel by pixel, so Planar Configuration is 0 (PS3.5
//     8.2: the codestream says how its components are arranged).
//
// photometric, when not empty, is a color transform the coder applied, which
// names the samples instead: YBR_RCT for JPEG 2000's reversible transform of
// RGB, YBR_FULL for JPEG Baseline's conversion.
func (ds *Dataset) describeCoded(source *Dataset, info *PixelDataInfo, photometric string) {
	if source != ds {
		for _, t := range []tag.Tag{tag.New(0x0028, 0x0004), tag.New(0x0028, 0x0006)} {
			if described, ok := source.Get(t); ok {
				ds.Remove(t)
				_ = ds.Add(described)
			}
		}
	}
	if photometric == "" && info.PhotometricInterpretation == "YBR_FULL_422" {
		photometric = "YBR_FULL"
	}
	if photometric != "" {
		if len(photometric)%2 == 1 {
			photometric += " " // CS is padded to an even length with a space
		}
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte(photometric))
	}
	if info.SamplesPerPixel > 1 {
		planar := make([]byte, 2)
		binary.LittleEndian.PutUint16(planar, 0)
		replaceElement(ds, tag.New(0x0028, 0x0006), dataelem.US, planar)
	}
}
