package compress

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrFrameMismatch is wrapped by every refusal of a frame whose codestream
// describes a different image from the one its data set does: other columns,
// rows or components, or samples coded wider than Bits Allocated.
//
// PS3.5 8.2 has the Image Pixel Module describe the decompressed image and
// requires its values to be consistent with the compressed data stream. When
// the two disagree one of them is wrong, and nothing in either says which.
var ErrFrameMismatch = errors.New("the frame is not the image the data set describes")

// FrameHeader is what a codestream's own header says of the image in it, read
// without decoding it: T.81's and T.87's frame header (SOF), JPEG 2000's SIZ.
//
// Rows is 0 where the header leaves it to a later marker, as a T.81 frame
// header may with a DNL segment (T.81 B.2.5).
type FrameHeader struct {
	Columns    int
	Rows       int
	Components int
	// Precision is the widest component's depth in bits.
	Precision int
}

// FrameHeaderReader is a Decompressor that can read a frame's header without
// decoding the frame.
//
// A data set knows how large each of its frames is before any codec runs, and
// a codestream can say something else: a 64x64 image whose frame is a
// 4096x4096 JPEG 2000 codestream of a few hundred bytes, or a frame of the
// right length and the wrong shape. Reading the header first lets the pixel
// path refuse such a frame before paying for a decode of the image the
// codestream describes, rather than after. Every decoder of this package that
// has a size in its header satisfies it; RLE, which has none, does not.
type FrameHeaderReader interface {
	Decompressor
	FrameHeader(data []byte) (FrameHeader, error)
}

// Check refuses a frame header that does not describe the image a data set
// does: its Columns, Rows and Samples per Pixel, and samples no wider than
// Bits Allocated. The error wraps ErrFrameMismatch and names both.
//
// A header whose Rows is 0 is not held to rows; whatever decodes it is then
// held to the frame's length instead. A bitsAllocated of 0 or less is not
// compared.
func (h FrameHeader) Check(columns, rows, samplesPerPixel, bitsAllocated int) error {
	if h.Columns != columns || (h.Rows != 0 && h.Rows != rows) || h.Components != samplesPerPixel {
		return fmt.Errorf("%w: its codestream is %dx%d with %d %s, and the data set's Columns, Rows and "+
			"Samples per Pixel are %d, %d and %d", ErrFrameMismatch, h.Columns, h.Rows, h.Components,
			plural(h.Components, "component", "components"), columns, rows, samplesPerPixel)
	}
	if bitsAllocated > 0 && h.Precision > bitsAllocated {
		return fmt.Errorf("%w: it is coded at %d bits, more than the %d bits allocated to a sample",
			ErrFrameMismatch, h.Precision, bitsAllocated)
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// jpegFrameHeader reads the frame header of a T.81 or T.87 stream: the first
// SOF segment, whichever process it names, and the marker that names it.
//
// It walks the stream as the decoders of this package do: fill bytes and bytes
// between segments are skipped, and every segment is stepped over by its length,
// so a byte pair inside an APP or COM payload is not taken for a marker. They
// refuse a second frame header (T.81 B.2: an image that is not hierarchical has
// one), and image/jpeg does too, so the header this finds is the one the image
// is decoded at.
func jpegFrameHeader(data []byte) (FrameHeader, byte, error) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != markerSOI {
		return FrameHeader{}, 0, errors.New("not a JPEG stream: it does not begin with SOI")
	}
	for pos := 2; pos+1 < len(data); {
		if data[pos] != 0xFF {
			pos++
			continue
		}
		marker := data[pos+1]
		pos += 2
		switch {
		case marker == 0xFF:
			pos--
			continue
		case marker == markerSOI || marker == markerEOI || marker == markerTEM ||
			(marker >= markerRST0 && marker <= markerRST7):
			continue
		case marker == markerSOS:
			return FrameHeader{}, 0, errors.New("the scan comes before any frame header")
		}
		if pos+2 > len(data) {
			return FrameHeader{}, 0, fmt.Errorf("segment 0x%02X has no length", marker)
		}
		length := int(binary.BigEndian.Uint16(data[pos:]))
		if length < 2 || pos+length > len(data) {
			return FrameHeader{}, 0, fmt.Errorf("segment 0x%02X declares %d bytes, %d remain",
				marker, length, len(data)-pos)
		}
		if isFrameMarker(marker) {
			body := data[pos+2 : pos+length]
			if len(body) < 6 {
				return FrameHeader{}, 0, fmt.Errorf("frame header 0x%02X is %d bytes, want at least 6",
					marker, len(body))
			}
			return FrameHeader{
				Precision:  int(body[0]),
				Rows:       int(binary.BigEndian.Uint16(body[1:])),
				Columns:    int(binary.BigEndian.Uint16(body[3:])),
				Components: int(body[5]),
			}, marker, nil
		}
		pos += length
	}
	return FrameHeader{}, 0, errors.New("the stream has no frame header")
}

// isFrameMarker reports whether marker is a start of frame: SOF0 to SOF15 of
// T.81 B.1.1.3 less DHT, JPG and DAC, which share the range, and T.87's SOF55.
func isFrameMarker(marker byte) bool {
	if marker == markerSOF55 {
		return true
	}
	return marker >= 0xC0 && marker <= 0xCF && marker != markerDHT && marker != 0xC8 && marker != 0xCC
}
