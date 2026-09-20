package compress

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/amrshadid/go-dicom/compress/jpeg2000"
)

// JPEG 2000 (ISO/IEC 15444-1) decoding, for DICOM's 1.2.840.10008.1.2.4.90
// (lossless) and .91 (lossy).
//
// The codec itself is in compress/jpeg2000. What lives here is the part that
// belongs to DICOM rather than to the standard: packing samples at Bits
// Allocated, and reconciling the codestream's idea of signedness with the data
// set's, which are allowed to disagree and sometimes do.
//
// Both transfer syntaxes decode through the same path. The difference between
// them is which wavelet the encoder chose, and the codestream says so itself —
// the transfer syntax is not consulted, so a file labeled lossy that holds a
// reversible codestream decodes losslessly, which is what it is.

// errEmptyJPEG2000Frame reports a fragment with nothing in it.
var errEmptyJPEG2000Frame = errors.New("jpeg2000: the frame is empty")

// JPEG2000Decompressor decodes JPEG 2000 frames.
//
// It satisfies Decompressor, so it is what GetExternalRegistry returns for
// JPEG_2000. It needs no C library.
type JPEG2000Decompressor struct{}

// NewJPEG2000Decompressor returns a decoder for JPEG 2000.
func NewJPEG2000Decompressor() *JPEG2000Decompressor {
	return &JPEG2000Decompressor{}
}

// CanDecompress reports whether the data looks like a JPEG 2000 frame: either a
// raw codestream, which opens with SOC immediately followed by SIZ, or a JP2
// file, which opens with the signature box.
//
// DICOM asks for the bare codestream (PS3.5 A.4.4), but files carrying the JP2
// wrapper exist and are readable, so both are accepted.
func (d *JPEG2000Decompressor) CanDecompress(data []byte) bool {
	if len(data) >= 4 && data[0] == 0xFF && data[1] == 0x4F &&
		data[2] == 0xFF && data[3] == 0x51 {
		return true
	}
	return len(data) >= 12 &&
		binary.BigEndian.Uint32(data[4:]) == 0x6A502020 && // 'jP  '
		binary.BigEndian.Uint32(data[8:]) == 0x0D0A870A
}

// Decompress decodes one JPEG 2000 frame to raw pixel bytes, laid out the way
// the codestream itself describes: samples at the component's own depth, and
// for several components, interleaved.
//
// A data set's Bits Allocated may be wider than the codestream's depth, and its
// Pixel Representation may disagree with the codestream's; neither is visible
// from here. DecompressFrame takes both and is what the pixel path calls.
func (d *JPEG2000Decompressor) Decompress(data []byte) ([]byte, error) {
	img, err := decodeJPEG2000(data)
	if err != nil {
		return nil, err
	}
	width := 1
	for _, depth := range img.Depths {
		if depth > 8 {
			width = 2
		}
	}
	return packJPEG2000(img, width*8, 0, false), nil
}

// DecompressFrame decodes one frame into the layout a data set describes.
//
// bitsAllocated is the width each sample occupies in the result, which DICOM
// fixes independently of the codestream: a 13-bit image in a data set with Bits
// Allocated 16 is written two bytes to the sample.
//
// pixelRepresentation is where the two standards can contradict each other. A
// codestream states whether its samples are signed, and so does the data set,
// and PS3.5 A.4.4 makes the data set the authority. When the data set says
// signed and the codestream said unsigned, an unsigned sample is read back as
// two's complement of Bits Stored: J2K_pixelrep_mismatch.dcm is a real file
// that does this, and without the conversion its background reads as 6192
// rather than -2000 — a wrong window on a real image, not a rounding error.
func (d *JPEG2000Decompressor) DecompressFrame(data []byte, bitsAllocated, bitsStored,
	pixelRepresentation int) ([]byte, error) {

	img, err := decodeJPEG2000(data)
	if err != nil {
		return nil, err
	}
	if bitsAllocated != 8 && bitsAllocated != 16 && bitsAllocated != 32 {
		return nil, fmt.Errorf("jpeg2000: Bits Allocated is %d, which is not 8, 16 or 32",
			bitsAllocated)
	}
	if bitsStored <= 0 || bitsStored > bitsAllocated {
		bitsStored = bitsAllocated
	}
	return packJPEG2000(img, bitsAllocated, bitsStored, pixelRepresentation == 1), nil
}

// decodeJPEG2000 parses and decodes one frame.
func decodeJPEG2000(data []byte) (*jpeg2000.Image, error) {
	if len(data) == 0 {
		return nil, errEmptyJPEG2000Frame
	}
	codestream, err := jpeg2000.ParseCodestream(data)
	if err != nil {
		return nil, err
	}
	return jpeg2000.Decode(codestream)
}

// packJPEG2000 writes the decoded planes out interleaved, at the given width.
//
// datasetSigned says the data set calls these samples signed. Where the
// codestream disagreed, the sample is reinterpreted rather than rescaled: the
// bits stay as they are and their meaning changes, which is what a mismatch
// between the two headers amounts to.
func packJPEG2000(img *jpeg2000.Image, bitsAllocated, bitsStored int, datasetSigned bool) []byte {
	samples := img.Width * img.Height
	components := len(img.Components)
	bytesPer := bitsAllocated / 8
	out := make([]byte, samples*components*bytesPer)

	at := 0
	for pixel := 0; pixel < samples; pixel++ {
		for comp := 0; comp < components; comp++ {
			v := img.Components[comp][pixel]
			if datasetSigned && !img.Signed[comp] && bitsStored > 0 && bitsStored < 32 {
				if v >= 1<<uint(bitsStored-1) {
					v -= 1 << uint(bitsStored)
				}
			}
			switch bytesPer {
			case 1:
				out[at] = byte(v)
			case 2:
				binary.LittleEndian.PutUint16(out[at:], uint16(v))
			default:
				binary.LittleEndian.PutUint32(out[at:], uint32(v))
			}
			at += bytesPer
		}
	}
	return out
}
