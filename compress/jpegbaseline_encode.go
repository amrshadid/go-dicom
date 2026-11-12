package compress

import (
	"fmt"
	"math"
)

// A JPEG Baseline (Process 1) encoder: sequential DCT, Huffman coded, 8-bit
// samples (ISO/IEC 10918-1, ITU-T T.81).
//
// The standard library's encoder would have done for grayscale. For color it
// always subsamples chroma 4:2:0, and DICOM has no Photometric Interpretation
// for that in a JPEG Baseline image: YBR_FULL_422 is 4:2:2, and
// YBR_PARTIAL_420 belongs to MPEG. This one writes every component at full
// resolution, which YBR_FULL names exactly (#243).

// The example quantization tables of T.81 Annex K.1, in block order. They
// are what "quality" scales, as libjpeg does, so a quality here means what
// it means elsewhere.
var baseQuantization = [2][64]int{
	{ // luminance (Table K.1)
		16, 11, 10, 16, 24, 40, 51, 61,
		12, 12, 14, 19, 26, 58, 60, 55,
		14, 13, 16, 24, 40, 57, 69, 56,
		14, 17, 22, 29, 51, 87, 80, 62,
		18, 22, 37, 56, 68, 109, 103, 77,
		24, 35, 55, 64, 81, 104, 113, 92,
		49, 64, 78, 87, 103, 121, 120, 101,
		72, 92, 95, 98, 112, 100, 103, 99,
	},
	{ // chrominance (Table K.2)
		17, 18, 24, 47, 99, 99, 99, 99,
		18, 21, 26, 66, 99, 99, 99, 99,
		24, 26, 56, 99, 99, 99, 99, 99,
		47, 66, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
	},
}

// huffmanSpec is a Huffman table as a DHT segment carries it: how many codes
// there are of each length from 1 to 16, and the symbols in code order.
type huffmanSpec struct {
	counts  [16]byte
	symbols []byte
}

// The typical Huffman tables of T.81 Annex K.3. A baseline decoder needs no
// more than the tables in the stream, so nothing depends on these being the
// best for an image, only on their holding every symbol an 8-bit image can
// produce, which they do.
var baselineHuffman = [4]huffmanSpec{
	{ // DC, luminance (Table K.3)
		[16]byte{0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	{ // AC, luminance (Table K.5)
		[16]byte{0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 0x7d},
		[]byte{
			0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12,
			0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
			0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08,
			0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
			0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16,
			0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
			0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39,
			0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
			0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59,
			0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
			0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79,
			0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
			0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
			0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
			0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6,
			0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
			0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4,
			0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
			0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea,
			0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
	{ // DC, chrominance (Table K.4)
		[16]byte{0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	{ // AC, chrominance (Table K.6)
		[16]byte{0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 0x77},
		[]byte{
			0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21,
			0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71,
			0x13, 0x22, 0x32, 0x81, 0x08, 0x14, 0x42, 0x91,
			0xa1, 0xb1, 0xc1, 0x09, 0x23, 0x33, 0x52, 0xf0,
			0x15, 0x62, 0x72, 0xd1, 0x0a, 0x16, 0x24, 0x34,
			0xe1, 0x25, 0xf1, 0x17, 0x18, 0x19, 0x1a, 0x26,
			0x27, 0x28, 0x29, 0x2a, 0x35, 0x36, 0x37, 0x38,
			0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
			0x49, 0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
			0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
			0x69, 0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x79, 0x7a, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
			0x88, 0x89, 0x8a, 0x92, 0x93, 0x94, 0x95, 0x96,
			0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5,
			0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4,
			0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3,
			0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2,
			0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda,
			0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9,
			0xea, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
}

// huffmanCode is the code for one symbol.
type huffmanCode struct {
	bits   uint32
	length uint8
}

// baselineCodes holds the codes of baselineHuffman, indexed by symbol, built
// the way T.81 Annex C derives them from the counts.
var baselineCodes = func() (tables [4][256]huffmanCode) {
	for t, spec := range baselineHuffman {
		code, next := uint32(0), 0
		for length := 1; length <= 16; length++ {
			for i := 0; i < int(spec.counts[length-1]); i++ {
				tables[t][spec.symbols[next]] = huffmanCode{bits: code, length: uint8(length)}
				code++
				next++
			}
			code <<= 1
		}
	}
	return tables
}()

// dctCosine[x][u] is cos((2x+1)uπ/16), scaled by the DCT's normalization.
var dctCosine = func() (table [8][8]float64) {
	for x := 0; x < 8; x++ {
		for u := 0; u < 8; u++ {
			scale := 0.5
			if u == 0 {
				scale = 0.5 / math.Sqrt2
			}
			table[x][u] = scale * math.Cos(float64(2*x+1)*float64(u)*math.Pi/16)
		}
	}
	return table
}()

// EncodeJPEGBaseline encodes one frame as a JPEG Baseline (Process 1)
// codestream of even length, ready to be a fragment.
//
// samples holds width by height pixels of components samples each, one byte
// a sample, the samples of a pixel adjacent. One component is a grayscale
// image. Three are coded as given, each at full resolution, with no color
// transform: a decoder takes three components for Y, Cb and Cr, so that is
// what the caller passes, and the data set says YBR_FULL.
//
// quality runs from 1 to 100 and scales the quantization tables of T.81
// Annex K as libjpeg does. The coding is lossy at every quality, 100
// included.
func EncodeJPEGBaseline(samples []byte, width, height, components, quality int) ([]byte, error) {
	if components != 1 && components != 3 {
		return nil, fmt.Errorf("jpeg baseline: %d components; this encoder codes 1 or 3", components)
	}
	// SOF0 holds each dimension in 16 bits.
	if width < 1 || height < 1 || width > 65535 || height > 65535 {
		return nil, fmt.Errorf("jpeg baseline: a frame of %dx%d cannot be coded", width, height)
	}
	if quality < 1 || quality > 100 {
		return nil, fmt.Errorf("jpeg baseline: quality %d; it runs from 1 to 100", quality)
	}
	if need := width * height * components; len(samples) < need {
		return nil, fmt.Errorf("jpeg baseline: %d samples for a %dx%d frame of %d components, which needs %d",
			len(samples), width, height, components, need)
	}

	e := &baselineEncoder{}
	tables := 1
	if components == 3 {
		tables = 2
	}
	for t := 0; t < tables; t++ {
		e.quantization[t] = scaledQuantization(baseQuantization[t], quality)
	}

	e.marker(0xD8) // SOI
	for t := 0; t < tables; t++ {
		e.segment(0xDB, 65)
		e.out = append(e.out, byte(t)) // 8-bit entries, table t
		for _, at := range zigzag {
			e.out = append(e.out, byte(e.quantization[t][at]))
		}
	}

	e.segment(0xC0, 6+3*components) // SOF0
	e.out = append(e.out, 8, byte(height>>8), byte(height), byte(width>>8), byte(width), byte(components))
	for c := 0; c < components; c++ {
		// Every component at 1x1: no subsampling. The first uses the
		// luminance quantization table and the others the chrominance one.
		e.out = append(e.out, byte(c+1), 0x11, byte(min(c, 1)))
	}

	for t := 0; t < 2*tables; t++ {
		spec := baselineHuffman[t]
		e.segment(0xC4, 17+len(spec.symbols))
		// The class in the high nibble, 0 for DC and 1 for AC, and the
		// destination in the low one.
		e.out = append(e.out, byte(t%2<<4|t/2))
		e.out = append(e.out, spec.counts[:]...)
		e.out = append(e.out, spec.symbols...)
	}

	e.segment(0xDA, 4+2*components) // SOS
	e.out = append(e.out, byte(components))
	for c := 0; c < components; c++ {
		table := byte(min(c, 1))
		e.out = append(e.out, byte(c+1), table<<4|table)
	}
	e.out = append(e.out, 0, 63, 0) // the whole spectrum, in one scan

	// One block of each component per minimum coded unit, left to right and
	// top to bottom. A block that reaches past an edge repeats the edge.
	var previousDC [3]int
	var block [64]float64
	for by := 0; by < height; by += 8 {
		for bx := 0; bx < width; bx += 8 {
			for c := 0; c < components; c++ {
				for y := 0; y < 8; y++ {
					sy := min(by+y, height-1)
					for x := 0; x < 8; x++ {
						sx := min(bx+x, width-1)
						block[y*8+x] = float64(samples[(sy*width+sx)*components+c]) - 128
					}
				}
				table := min(c, 1)
				previousDC[c] = e.block(&block, table, previousDC[c])
			}
		}
	}

	// The last byte is filled out with ones (T.81 F.1.2.3).
	if e.pending > 0 {
		e.emit(1<<(8-e.pending)-1, uint8(8-e.pending))
	}
	e.marker(0xD9) // EOI
	// A fragment is of even length, and a zero after EOI is how that is done.
	if len(e.out)%2 == 1 {
		e.out = append(e.out, 0x00)
	}
	return e.out, nil
}

// scaledQuantization scales a base table for a quality, as libjpeg's
// jpeg_set_quality does, keeping every entry within a byte.
func scaledQuantization(base [64]int, quality int) (table [64]int) {
	scale := 200 - 2*quality
	if quality < 50 {
		scale = 5000 / quality
	}
	for i, v := range base {
		table[i] = min(max((v*scale+50)/100, 1), 255)
	}
	return table
}

type baselineEncoder struct {
	out          []byte
	quantization [2][64]int

	// The bits waiting to make up a byte, in the low end of acc.
	acc     uint32
	pending uint8
}

func (e *baselineEncoder) marker(m byte) {
	e.out = append(e.out, 0xFF, m)
}

// segment starts a marker segment whose payload is n bytes.
func (e *baselineEncoder) segment(m byte, n int) {
	e.out = append(e.out, 0xFF, m, byte((n+2)>>8), byte(n+2))
}

// emit writes the low length bits of bits, most significant first. A byte of
// FF in the coded data is followed by a zero, so that a decoder does not
// take it for a marker (T.81 B.1.1.5).
func (e *baselineEncoder) emit(bits uint32, length uint8) {
	e.acc = e.acc<<length | bits&(1<<length-1)
	e.pending += length
	for e.pending >= 8 {
		b := byte(e.acc >> (e.pending - 8))
		e.out = append(e.out, b)
		if b == 0xFF {
			e.out = append(e.out, 0x00)
		}
		e.pending -= 8
	}
}

func (e *baselineEncoder) emitCode(table int, symbol byte) {
	code := baselineCodes[table][symbol]
	e.emit(code.bits, code.length)
}

// magnitude returns the number of bits needed for v's size and the bits that
// say which value of that size it is: v itself when positive, and v - 1 in
// that many bits when negative (T.81 F.1.2.1).
func magnitude(v int) (size uint8, bits uint32) {
	a := v
	if a < 0 {
		a = -a
		v--
	}
	for a > 0 {
		size++
		a >>= 1
	}
	return size, uint32(v) & (1<<size - 1)
}

// block transforms, quantizes and codes one 8 by 8 block of level-shifted
// samples, and returns its DC value for the next block to be coded against.
func (e *baselineEncoder) block(samples *[64]float64, table, previousDC int) int {
	// The forward DCT, rows then columns.
	var rows, coefficients [64]float64
	for y := 0; y < 8; y++ {
		for u := 0; u < 8; u++ {
			var sum float64
			for x := 0; x < 8; x++ {
				sum += samples[y*8+x] * dctCosine[x][u]
			}
			rows[y*8+u] = sum
		}
	}
	for u := 0; u < 8; u++ {
		for v := 0; v < 8; v++ {
			var sum float64
			for y := 0; y < 8; y++ {
				sum += rows[y*8+u] * dctCosine[y][v]
			}
			coefficients[v*8+u] = sum
		}
	}

	var quantized [64]int
	for k, at := range zigzag {
		quantized[k] = int(math.Round(coefficients[at] / float64(e.quantization[table][at])))
	}

	// DC: the difference from the previous block of this component.
	dc, ac := 2*table, 2*table+1
	size, bits := magnitude(quantized[0] - previousDC)
	e.emitCode(dc, size)
	e.emit(bits, size)

	// AC: each nonzero coefficient with the run of zeros before it.
	run := 0
	for k := 1; k < 64; k++ {
		if quantized[k] == 0 {
			run++
			continue
		}
		for ; run > 15; run -= 16 {
			e.emitCode(ac, 0xF0) // sixteen zeros
		}
		size, bits := magnitude(quantized[k])
		e.emitCode(ac, byte(run)<<4|size)
		e.emit(bits, size)
		run = 0
	}
	if run > 0 {
		e.emitCode(ac, 0x00) // end of block
	}
	return quantized[0]
}
