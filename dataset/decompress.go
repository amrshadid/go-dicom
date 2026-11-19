package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// DecompressInPlace replaces encapsulated Pixel Data with its decoded
// samples and makes the data set describe them.
//
// Decoding changes more than the element. The samples come out one pixel
// after another whatever Planar Configuration said, a 4:2:2 JPEG comes out
// with chroma for every pixel, and a JPEG 2000 color transform is undone. So
// alongside the transfer syntax, which becomes Explicit VR Little Endian:
//
//   - Planar Configuration becomes 0 for a color image;
//   - YBR_FULL_422 becomes YBR_FULL, the same color space at full resolution;
//   - YBR_RCT and YBR_ICT become RGB.
//
// Left as they were, a reader takes the native bytes for a layout they are
// not in. Lossy Image Compression and its ratio are not touched: decoding
// does not undo a loss, and the image is still one that was lossily
// compressed.
//
// A data set with native pixel data, or none, is left alone. On an error
// nothing is changed.
func (ds *Dataset) DecompressInPlace() error {
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return nil
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	stored, err := extractBytesValue(elem)
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	if _, encapsulated := ds.pixelCompression(stored, info); !encapsulated {
		return nil
	}
	samples, err := ds.pixelBytesForDecoding(info)
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	if need := info.BytesPerFrame * info.NumberOfFrames; len(samples) < need {
		return fmt.Errorf("decompress: the frames decode to %d bytes and the image needs %d", len(samples), need)
	}

	// DICOM values are of even length.
	if len(samples)%2 == 1 {
		samples = append(samples, 0x00)
	}
	// Native pixel data is OW when a sample is wider than a byte.
	vr := dataelem.OB
	if info.BitsAllocated > 8 {
		vr = dataelem.OW
	}
	elem.SetVR(vr)
	if err := ds.SetValue(pixelData, samples); err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")

	if info.SamplesPerPixel > 1 {
		planar := make([]byte, 2)
		binary.LittleEndian.PutUint16(planar, 0)
		replaceElement(ds, tag.New(0x0028, 0x0006), dataelem.US, planar)
	}
	switch info.PhotometricInterpretation {
	case "YBR_FULL_422":
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte("YBR_FULL"))
	case "YBR_RCT", "YBR_ICT":
		replaceElement(ds, tag.New(0x0028, 0x0004), dataelem.CS, []byte("RGB "))
	}
	return nil
}

// DecodeFrame decodes one compressed frame of this image into its samples:
// codestream is the frame as it is encapsulated, its fragments joined, and
// the data set says the rest, its transfer syntax and the attributes of the
// Image Pixel Module. Pixel Data itself need not be in it, which is what
// lets a frame be decoded from a file read without its bulk data.
//
// The samples come out as DecompressInPlace leaves them, which uses the
// same decoders: pixel after pixel, little endian, chroma for every pixel,
// a JPEG 2000 color transform undone.
func (ds *Dataset) DecodeFrame(codestream []byte) ([]byte, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("decode frame: %w", err)
	}
	compression, encapsulated := ds.pixelCompression(nil, info)
	if !encapsulated {
		return nil, fmt.Errorf("decode frame: transfer syntax %q does not compress frames", ds.TransferSyntaxUID())
	}
	samples, err := decompressPixelFrame(compression, codestream, info)
	if err != nil {
		return nil, fmt.Errorf("decode frame: %w", err)
	}
	return samples, nil
}

// replaceElement sets an attribute, whether or not the data set had it.
func replaceElement(ds *Dataset, t tag.Tag, vr dataelem.VR, value []byte) {
	ds.Remove(t)
	_ = ds.Add(dataelem.NewDataElement(t, vr, value))
}
