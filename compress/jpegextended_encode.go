package compress

import (
	"encoding/binary"
	"fmt"
	"math"
)

// A JPEG Extended encoder, ITU-T T.81 Processes 2 and 4: sequential DCT,
// Huffman coded, 8 or 12 bits a sample. It is what DICOM's
// 1.2.840.10008.1.2.4.51 holds, and the lossy JPEG for images deeper than
// 8 bits: CT, MR and CR stored in 12.
//
// It is the baseline encoder with a wider sample. The DCT, the quantization
// and the coding of DC differences and AC run/size pairs are the same; the
// level shift is 2048 rather than 128, a quantization table may need 16-bit
// entries, and the Huffman tables are built for each frame. They have to be:
// at 12 bits a DC difference reaches category 15 and an AC coefficient size
// 14, and the example tables of Annex K stop at 11 and 10.

// EncodeJPEGExtended encodes one frame as a JPEG Extended (SOF1) codestream of
// even length, ready to be a fragment.
//
// samples holds width by height pixels of components samples each, the
// samples of a pixel adjacent: one byte a sample when precision is 8, two,
// little endian, when it is 12. A sample is unsigned and below 2^precision.
// One component is a grayscale image; three are coded as given, each at full
// resolution and with no color transform, so a caller with RGB converts it
// to YBR_FULL first, as for EncodeJPEGBaseline.
//
// quality runs from 1 to 100 and scales the quantization tables of T.81
// Annex K as libjpeg does, at either precision. The coding is lossy at every
// quality, 100 included.
func EncodeJPEGExtended(samples []byte, width, height, components, precision, quality int) ([]byte, error) {
	if precision != 8 && precision != 12 {
		return nil, fmt.Errorf("jpeg extended: a precision of %d bits; Processes 2 and 4 code 8 and 12", precision)
	}
	if components != 1 && components != 3 {
		return nil, fmt.Errorf("jpeg extended: %d components; this encoder codes 1 or 3", components)
	}
	// SOF1 holds each dimension in 16 bits.
	if width < 1 || height < 1 || width > 65535 || height > 65535 {
		return nil, fmt.Errorf("jpeg extended: a frame of %dx%d cannot be coded", width, height)
	}
	if quality < 1 || quality > 100 {
		return nil, fmt.Errorf("jpeg extended: quality %d; it runs from 1 to 100", quality)
	}
	bytesPer := 1
	if precision == 12 {
		bytesPer = 2
	}
	if want := int64(width) * int64(height) * int64(components) * int64(bytesPer); int64(len(samples)) != want {
		return nil, fmt.Errorf("jpeg extended: %d bytes of samples, want %d for %dx%d with %d component(s) at %d bits",
			len(samples), want, width, height, components, precision)
	}
	sample := func(i int) int {
		if bytesPer == 1 {
			return int(samples[i])
		}
		return int(binary.LittleEndian.Uint16(samples[2*i:]))
	}
	maxSample := 1<<uint(precision) - 1
	for i := 0; i < width*height*components; i++ {
		if v := sample(i); v > maxSample {
			return nil, fmt.Errorf("jpeg extended: sample %d at %d is above the %d that %d bits hold",
				v, i, maxSample, precision)
		}
	}

	tables := 1
	if components == 3 {
		tables = 2
	}
	var quantization [2][64]int
	for t := 0; t < tables; t++ {
		quantization[t] = scaledQuantizationTo(baseQuantization[t], quality, extendedQuantizationLimit(precision))
	}

	// First every block is transformed and quantized, and its symbols
	// counted; the tables are built from the counts; then the same blocks are
	// coded with them. One block of each component per minimum coded unit,
	// left to right and top to bottom, a block reaching past an edge
	// repeating the edge.
	blocksWide, blocksHigh := (width+7)/8, (height+7)/8
	coefficients := make([][64]int32, blocksWide*blocksHigh*components)
	var dcCounts, acCounts [2][256]int
	var previousDC [3]int32
	shift := float64(int(1) << uint(precision-1))
	var block [64]float64
	at := 0
	for by := 0; by < blocksHigh; by++ {
		for bx := 0; bx < blocksWide; bx++ {
			for c := 0; c < components; c++ {
				for y := 0; y < 8; y++ {
					sy := min(by*8+y, height-1)
					for x := 0; x < 8; x++ {
						sx := min(bx*8+x, width-1)
						block[y*8+x] = float64(sample((sy*width+sx)*components+c)) - shift
					}
				}
				table := min(c, 1)
				q := &coefficients[at]
				quantizeBlock(&block, &quantization[table], q)
				at++

				size, _ := magnitude(int(q[0] - previousDC[c]))
				dcCounts[table][size]++
				previousDC[c] = q[0]
				countAC(q, &acCounts[table])
			}
		}
	}

	var dcSpecs, acSpecs [2]huffmanSpec
	var dcCodes, acCodes [2][256]huffmanCode
	for t := 0; t < tables; t++ {
		dcSpecs[t], acSpecs[t] = optimalHuffman(&dcCounts[t]), optimalHuffman(&acCounts[t])
		dcCodes[t], acCodes[t] = huffmanCodes(dcSpecs[t]), huffmanCodes(acSpecs[t])
	}

	e := &baselineEncoder{}
	e.marker(markerSOI)
	for t := 0; t < tables; t++ {
		wide := false
		for _, v := range quantization[t] {
			wide = wide || v > 255
		}
		if wide {
			// Pq = 1: sixteen-bit entries, which only an extended frame may
			// have (T.81 B.2.4.1).
			e.segment(markerDQT, 129)
			e.out = append(e.out, 0x10|byte(t))
			for _, pos := range zigzag {
				e.out = append(e.out, byte(quantization[t][pos]>>8), byte(quantization[t][pos]))
			}
		} else {
			e.segment(markerDQT, 65)
			e.out = append(e.out, byte(t))
			for _, pos := range zigzag {
				e.out = append(e.out, byte(quantization[t][pos]))
			}
		}
	}

	e.segment(markerSOF1, 6+3*components)
	e.out = append(e.out, byte(precision), byte(height>>8), byte(height), byte(width>>8), byte(width),
		byte(components))
	for c := 0; c < components; c++ {
		// Every component at 1x1, the first on table 0 and the others on 1.
		e.out = append(e.out, byte(c+1), 0x11, byte(min(c, 1)))
	}

	for t := 0; t < tables; t++ {
		for class, spec := range []huffmanSpec{dcSpecs[t], acSpecs[t]} {
			e.segment(markerDHT, 17+len(spec.symbols))
			e.out = append(e.out, byte(class<<4|t))
			e.out = append(e.out, spec.counts[:]...)
			e.out = append(e.out, spec.symbols...)
		}
	}

	e.segment(markerSOS, 4+2*components)
	e.out = append(e.out, byte(components))
	for c := 0; c < components; c++ {
		table := byte(min(c, 1))
		e.out = append(e.out, byte(c+1), table<<4|table)
	}
	e.out = append(e.out, 0, 63, 0) // the whole spectrum, in one scan

	previousDC = [3]int32{}
	at = 0
	for i := 0; i < blocksWide*blocksHigh; i++ {
		for c := 0; c < components; c++ {
			table := min(c, 1)
			q := &coefficients[at]
			at++
			size, bits := magnitude(int(q[0] - previousDC[c]))
			previousDC[c] = q[0]
			code := dcCodes[table][size]
			e.emit(code.bits, code.length)
			e.emit(bits, size)
			emitAC(e, q, &acCodes[table])
		}
	}

	// The last byte is filled out with ones (T.81 F.1.2.3).
	if e.pending > 0 {
		e.emit(1<<(8-e.pending)-1, 8-e.pending)
	}
	e.marker(markerEOI)
	if len(e.out)%2 == 1 {
		e.out = append(e.out, 0x00)
	}
	return e.out, nil
}

// extendedQuantizationLimit is the largest quantization table entry for a
// precision: what a byte holds at 8 bits, so the table is one a baseline
// decoder also reads, and what libjpeg allows outside baseline at 12.
func extendedQuantizationLimit(precision int) int {
	if precision == 8 {
		return 255
	}
	return 32767
}

// scaledQuantizationTo scales a base table for a quality as libjpeg's
// jpeg_set_quality does, keeping each entry from 1 to limit.
func scaledQuantizationTo(base [64]int, quality, limit int) (table [64]int) {
	scale := 200 - 2*quality
	if quality < 50 {
		scale = 5000 / quality
	}
	for i, v := range base {
		table[i] = min(max((v*scale+50)/100, 1), limit)
	}
	return table
}

// quantizeBlock is the forward DCT of a block of level-shifted samples and
// its quantization, the coefficients left in zigzag order.
func quantizeBlock(samples *[64]float64, quantization *[64]int, out *[64]int32) {
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
	for k, pos := range zigzag {
		out[k] = int32(math.Round(coefficients[pos] / float64(quantization[pos])))
	}
}

// countAC adds a block's AC symbols to counts: each nonzero coefficient with
// the run of zeros before it, sixteen zeros as ZRL, and the end of block.
func countAC(q *[64]int32, counts *[256]int) {
	run := 0
	for k := 1; k < 64; k++ {
		if q[k] == 0 {
			run++
			continue
		}
		for ; run > 15; run -= 16 {
			counts[0xF0]++
		}
		size, _ := magnitude(int(q[k]))
		counts[byte(run)<<4|size]++
		run = 0
	}
	if run > 0 {
		counts[0x00]++
	}
}

// emitAC codes a block's AC coefficients, exactly the symbols countAC
// counted.
func emitAC(e *baselineEncoder, q *[64]int32, codes *[256]huffmanCode) {
	run := 0
	for k := 1; k < 64; k++ {
		if q[k] == 0 {
			run++
			continue
		}
		for ; run > 15; run -= 16 {
			e.emit(codes[0xF0].bits, codes[0xF0].length)
		}
		size, bits := magnitude(int(q[k]))
		code := codes[byte(run)<<4|size]
		e.emit(code.bits, code.length)
		e.emit(bits, size)
		run = 0
	}
	if run > 0 {
		e.emit(codes[0x00].bits, codes[0x00].length)
	}
}
