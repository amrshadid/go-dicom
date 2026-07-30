package compress

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Lossless JPEG (ITU-T T.81 process 14) decoding.
//
// DICOM uses two transfer syntaxes for it: 1.2.840.10008.1.2.4.57 and
// 1.2.840.10008.1.2.4.70, the latter fixed to selection value 1. It is the
// compression older CT and MR archives are full of, and it is not the JPEG the
// standard library decodes: there is no DCT, no quantization and no color
// transform. Each sample is predicted from its neighbors and the difference is
// Huffman coded, which is why a baseline decoder rejects the SOF3 marker rather
// than producing a wrong image.
//
// Only Huffman coding is implemented. SOF11 is the arithmetic-coded variant,
// which no DICOM encoder in circulation produces.

// JPEG marker bytes, the second half of each 0xFF pair.
const (
	markerSOF0 = 0xC0 // baseline DCT, handled by the standard library
	markerSOF1 = 0xC1
	markerSOF3 = 0xC3 // lossless, Huffman — what this decoder is for
	markerDHT  = 0xC4
	markerSOF5 = 0xC5
	markerSOF7 = 0xC7 // lossless, differential — hierarchical, not supported
	markerSOI  = 0xD8
	markerEOI  = 0xD9
	markerSOS  = 0xDA
	markerDNL  = 0xDC
	markerDRI  = 0xDD
	markerRST0 = 0xD0
	markerRST7 = 0xD7
	markerTEM  = 0x01
)

// errNotLosslessJPEG reports a stream this decoder is not meant to handle, as
// opposed to one it should have handled and could not.
var errNotLosslessJPEG = errors.New("jpeglossless: not a lossless JPEG stream")

// JPEGLosslessDecompressor decodes lossless JPEG frames.
//
// It satisfies Decompressor, so it is what GetExternalRegistry returns for
// JPEG_LOSSLESS. Unlike the placeholder it replaces, it needs no C library.
type JPEGLosslessDecompressor struct{}

// NewJPEGLosslessDecompressor returns a decoder for lossless JPEG.
func NewJPEGLosslessDecompressor() *JPEGLosslessDecompressor {
	return &JPEGLosslessDecompressor{}
}

// CanDecompress reports whether data looks like a lossless JPEG stream.
//
// The check is for an SOF3 marker rather than merely for SOI, because every
// JPEG variant starts with SOI and claiming a baseline frame would produce an
// error where the standard library would have produced an image.
func (d *JPEGLosslessDecompressor) CanDecompress(data []byte) bool {
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return false
	}
	for i := 2; i+1 < len(data); {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		switch marker {
		case markerSOF3:
			return true
		case markerSOF0, markerSOF1, markerSOF5, markerSOF7, markerSOS, markerEOI:
			return false
		}
		i += 2
	}
	return false
}

// Decompress decodes one lossless JPEG frame to raw pixel bytes.
//
// The output matches what the rest of this package produces: samples
// pixel-interleaved, and little endian when the precision needs two bytes.
func (d *JPEGLosslessDecompressor) Decompress(data []byte) ([]byte, error) {
	img, err := decodeLosslessJPEG(data)
	if err != nil {
		return nil, err
	}
	return img.pack(), nil
}

// losslessComponent is one color component of the frame.
type losslessComponent struct {
	id       byte
	h, v     int   // sampling factors
	dcTable  int   // Huffman table selector, assigned by the scan header
	samples  []int // decoded values, width*height for this component
	rowWidth int   // width in samples, after sampling factors
	rows     int   // height in samples
}

// losslessImage is a decoded frame.
type losslessImage struct {
	width, height int
	precision     int
	components    []*losslessComponent
}

// pack flattens the components into interleaved little-endian samples.
func (img *losslessImage) pack() []byte {
	n := img.width * img.height
	nc := len(img.components)

	if img.precision <= 8 {
		out := make([]byte, n*nc)
		for c, comp := range img.components {
			for i := 0; i < n; i++ {
				out[i*nc+c] = byte(comp.samples[i])
			}
		}
		return out
	}

	out := make([]byte, n*nc*2)
	for c, comp := range img.components {
		for i := 0; i < n; i++ {
			binary.LittleEndian.PutUint16(out[(i*nc+c)*2:], uint16(comp.samples[i]))
		}
	}
	return out
}

// huffTable is a decoding table built from a DHT segment.
//
// Codes are canonical: all codes of a given length are consecutive, so a code
// is identified by its length and its offset within that length. That makes
// decoding a walk down the lengths comparing against maxcode, rather than a
// lookup in a table of every possible code.
type huffTable struct {
	mincode [17]int32 // smallest code of each length, -1 when none
	maxcode [17]int32 // largest code of each length, -1 when none
	valptr  [17]int   // index into values of the first code of each length
	values  []byte
}

func buildHuffTable(counts []byte, values []byte) (*huffTable, error) {
	t := &huffTable{values: values}

	var code int32
	k := 0
	for length := 1; length <= 16; length++ {
		n := int(counts[length-1])
		if n == 0 {
			t.maxcode[length] = -1
			t.mincode[length] = -1
			continue
		}
		t.valptr[length] = k
		t.mincode[length] = code
		code += int32(n)
		k += n
		t.maxcode[length] = code - 1
		code <<= 1

		if k > len(values) {
			return nil, fmt.Errorf("jpeglossless: Huffman table declares %d codes but carries %d values",
				k, len(values))
		}
	}
	return t, nil
}

// bitReader reads the entropy-coded segment a bit at a time.
//
// Inside that segment a literal 0xFF byte is stored as 0xFF 0x00, so a real
// marker can be found by scanning for 0xFF followed by anything else.
//
// Reading past the end yields zero bits, because the last few samples of a valid
// frame can legitimately need bits the encoder did not have to write. But it
// records that it happened, and the decoder refuses a frame that ran off the
// end by more than that: zero bits decode to plausible samples, so a stream cut
// in half would otherwise produce a full-size image whose second half is
// invented — the failure that cannot be seen by looking at the result.
type bitReader struct {
	data []byte
	pos  int
	bits uint32
	n    uint
	// marker holds a marker met while filling, so the scan loop can see a
	// restart or EOI without the bit reader having to interpret it.
	marker byte
	// overrun counts bytes fabricated past the end of the data.
	overrun int
}

func (b *bitReader) fill() {
	for b.n <= 24 {
		if b.pos >= len(b.data) {
			b.overrun++
			b.bits |= 0 << (24 - b.n)
			b.n += 8
			continue
		}
		c := b.data[b.pos]
		b.pos++
		if c == 0xFF {
			if b.pos < len(b.data) {
				next := b.data[b.pos]
				if next == 0x00 {
					b.pos++ // stuffed byte: the 0xFF is data
				} else {
					// A marker. Leave it for the scan loop and feed zeros.
					b.marker = next
					b.pos--
					b.bits |= 0 << (24 - b.n)
					b.n += 8
					continue
				}
			}
		}
		b.bits |= uint32(c) << (24 - b.n)
		b.n += 8
	}
}

func (b *bitReader) readBit() int32 {
	if b.n == 0 {
		b.fill()
	}
	bit := int32(b.bits >> 31)
	b.bits <<= 1
	b.n--
	return bit
}

func (b *bitReader) readBits(count int) int32 {
	var v int32
	for i := 0; i < count; i++ {
		v = v<<1 | b.readBit()
	}
	return v
}

// reset discards buffered bits and skips a restart marker, as required at each
// restart interval.
func (b *bitReader) reset() {
	b.bits = 0
	b.n = 0
	b.marker = 0
	// Skip to just past the next RSTn.
	for b.pos+1 < len(b.data) {
		if b.data[b.pos] == 0xFF {
			m := b.data[b.pos+1]
			if m >= markerRST0 && m <= markerRST7 {
				b.pos += 2
				return
			}
		}
		b.pos++
	}
	b.pos = len(b.data)
}

// decodeHuff reads one Huffman-coded symbol.
func (b *bitReader) decodeHuff(t *huffTable) (byte, error) {
	code := b.readBit()
	for length := 1; length <= 16; length++ {
		if t.maxcode[length] >= 0 && code <= t.maxcode[length] {
			idx := t.valptr[length] + int(code-t.mincode[length])
			if idx < 0 || idx >= len(t.values) {
				return 0, fmt.Errorf("jpeglossless: Huffman code of length %d is outside the table", length)
			}
			return t.values[idx], nil
		}
		code = code<<1 | b.readBit()
	}
	return 0, errors.New("jpeglossless: no Huffman code matched in 16 bits")
}

// extend converts a magnitude and its category into a signed difference,
// per T.81 figure F.12. The high half of each category is positive, the low
// half negative.
func extend(v int32, category int) int32 {
	if category == 0 {
		return 0
	}
	if v < 1<<(category-1) {
		return v - (1 << category) + 1
	}
	return v
}
