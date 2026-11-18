package compress

import (
	"encoding/binary"
	"fmt"
)

// Lossless JPEG encoding, ITU-T T.81 Process 14 (Annex H): the mirror of
// jpeglossless_decode.go.
//
// Each sample is predicted from the samples left of it, above it and above
// left, and the difference is Huffman coded the way a DCT image codes its DC
// differences. There is no transform and nothing is quantized, so what a
// decoder returns is what went in.
//
// The tables are built for each frame (huffman_build.go). The example tables
// of T.81 Annex K hold difference categories up to 11, and a 16-bit image
// produces up to 16; a table missing a category makes the frame impossible
// to code. dcmtk's encoder builds its tables the same way.
//
// # Scope
//
// One scan, every component in it, each at full resolution, with a table of
// its own. No point transform, which would make the coding lossy, and no
// restart intervals, which a frame in one fragment has no use for.

// jpegLosslessMaxComponents is the most components one scan may hold (T.81
// B.2.3), and so the most this encoder codes. DICOM images have one or three.
const jpegLosslessMaxComponents = 4

// EncodeJPEGLossless encodes one frame as a lossless JPEG (Process 14)
// codestream of even length, ready to be a fragment: transfer syntax
// 1.2.840.10008.1.2.4.70 when predictor is 1, and 1.2.840.10008.1.2.4.57
// for any predictor.
//
// samples holds the components interleaved by pixel, width*height pixels in
// raster order, each sample one byte when bitsAllocated is 8 and two, little
// endian, when it is 16: the layout native pixel data has. depth is the
// sample precision written in the frame header, 2 to 16 bits, and signed says
// whether a sample is two's complement. A signed sample is coded as its low
// depth bits, which is how PS3.5 8.2.1 has signed data held in a JPEG
// codestream; a reader sign-extends it again from Bits Stored.
//
// predictor is the selection value of T.81 Table H.1, 1 to 7, or 0 to have
// the encoder try each and keep the one that codes this frame in the fewest
// bits.
//
// A sample that does not fit depth is an error: narrowing it would not be
// lossless, and the caller knows whether the bits above depth mean anything.
func EncodeJPEGLossless(samples []byte, width, height, components, bitsAllocated, depth int,
	signed bool, predictor int) ([]byte, error) {
	if bitsAllocated != 8 && bitsAllocated != 16 {
		return nil, fmt.Errorf("jpeg lossless: Bits Allocated is %d; samples of 8 and 16 bits are encoded",
			bitsAllocated)
	}
	// T.81 B.2.2: a lossless frame's precision is 2 to 16 bits.
	if depth < 2 || depth > bitsAllocated {
		return nil, fmt.Errorf("jpeg lossless: a precision of %d bits in samples of %d; it runs from 2",
			depth, bitsAllocated)
	}
	if components < 1 || components > jpegLosslessMaxComponents {
		return nil, fmt.Errorf("jpeg lossless: %d components; this encoder codes 1 to %d, in one scan",
			components, jpegLosslessMaxComponents)
	}
	// SOF3 holds each dimension in 16 bits.
	if width < 1 || height < 1 || width > 65535 || height > 65535 {
		return nil, fmt.Errorf("jpeg lossless: a frame of %dx%d cannot be coded", width, height)
	}
	if predictor < 0 || predictor > 7 {
		return nil, fmt.Errorf("jpeg lossless: predictor %d; the selection values run from 1 to 7, "+
			"and 0 chooses", predictor)
	}
	bytesPer := bitsAllocated / 8
	if want := int64(width) * int64(height) * int64(components) * int64(bytesPer); int64(len(samples)) != want {
		return nil, fmt.Errorf("jpeg lossless: %d bytes of samples, want %d for %dx%d with %d component(s) at %d bits",
			len(samples), want, width, height, components, bitsAllocated)
	}

	planes, err := losslessPlanes(samples, width, height, components, bytesPer, depth, signed)
	if err != nil {
		return nil, err
	}

	if predictor == 0 {
		predictor = cheapestPredictor(planes, width, height, depth)
	}

	// The tables first, from the differences this predictor gives, then the
	// same differences coded with them. Both passes predict the same way, so
	// each symbol written has a code.
	var frequencies [jpegLosslessMaxComponents][256]int
	for c, plane := range planes {
		countDifferences(plane, width, height, depth, predictor, &frequencies[c])
	}
	specs := make([]huffmanSpec, components)
	codes := make([][256]huffmanCode, components)
	for c := range planes {
		specs[c] = optimalHuffman(&frequencies[c])
		codes[c] = huffmanCodes(specs[c])
	}

	e := &baselineEncoder{}
	e.marker(0xD8) // SOI

	e.segment(markerSOF3, 6+3*components)
	e.out = append(e.out, byte(depth), byte(height>>8), byte(height), byte(width>>8), byte(width), byte(components))
	for c := 0; c < components; c++ {
		// 1x1 sampling; the quantization table selector means nothing in a
		// lossless frame and is zero (T.81 B.2.2).
		e.out = append(e.out, byte(c+1), 0x11, 0)
	}

	for c, spec := range specs {
		e.segment(markerDHT, 17+len(spec.symbols))
		e.out = append(e.out, byte(c)) // class 0, the DC class lossless coding uses, destination c
		e.out = append(e.out, spec.counts[:]...)
		e.out = append(e.out, spec.symbols...)
	}

	e.segment(markerSOS, 4+2*components)
	e.out = append(e.out, byte(components))
	for c := 0; c < components; c++ {
		e.out = append(e.out, byte(c+1), byte(c)<<4)
	}
	// Ss carries the predictor, Se is zero, and Ah and Al, the point
	// transform, are zero (T.81 H.2.3).
	e.out = append(e.out, byte(predictor), 0, 0)

	// One sample of each component per pixel, in raster order: with 1x1
	// sampling that is what an interleaved minimum coded unit is (H.2).
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			for c, plane := range planes {
				d := losslessDifference(plane, x, y, width, depth, predictor)
				category, bits := losslessCategory(d)
				code := codes[c][category]
				e.emit(code.bits, code.length)
				if category > 0 && category < 16 {
					e.emit(bits, category)
				}
			}
		}
	}

	// The last byte is filled out with ones (T.81 F.1.2.3).
	if e.pending > 0 {
		e.emit(1<<(8-e.pending)-1, 8-e.pending)
	}
	e.marker(markerEOI)
	// A fragment is of even length, and a zero after EOI is how that is done.
	if len(e.out)%2 == 1 {
		e.out = append(e.out, 0x00)
	}
	return e.out, nil
}

// losslessPlanes separates interleaved samples into one plane a component,
// each sample as the unsigned depth-bit value the codestream holds.
func losslessPlanes(samples []byte, width, height, components, bytesPer, depth int,
	signed bool) ([][]int32, error) {
	lo, hi := int32(0), int32(1)<<uint(depth)-1
	if signed {
		lo, hi = -(int32(1) << uint(depth-1)), int32(1)<<uint(depth-1)-1
	}
	mask := int32(1)<<uint(depth) - 1

	planes := make([][]int32, components)
	for c := range planes {
		planes[c] = make([]int32, width*height)
	}
	at := 0
	for pixel := 0; pixel < width*height; pixel++ {
		for c := 0; c < components; c++ {
			var v int32
			switch {
			case bytesPer == 1 && signed:
				v = int32(int8(samples[at]))
			case bytesPer == 1:
				v = int32(samples[at])
			case signed:
				v = int32(int16(binary.LittleEndian.Uint16(samples[at:])))
			default:
				v = int32(binary.LittleEndian.Uint16(samples[at:]))
			}
			if v < lo || v > hi {
				return nil, fmt.Errorf("jpeg lossless: sample %d of pixel %d is outside the %d to %d "+
					"a precision of %d bits holds", v, pixel, lo, hi, depth)
			}
			planes[c][pixel] = v & mask
			at += bytesPer
		}
	}
	return planes, nil
}

// losslessDifference is the difference coded for one sample: the sample less
// its prediction, modulo 2^16, as a value from -32768 to 32767.
//
// The prediction is the decoder's predictSample, case for case: the first
// sample of the scan takes 2^(P-1), the rest of the first line looks left,
// and the first sample of each later line looks up (T.81 H.1.2.1).
func losslessDifference(plane []int32, x, y, width, depth, predictor int) int32 {
	var prediction int32
	switch {
	case x == 0 && y == 0:
		prediction = 1 << uint(depth-1)
	case y == 0:
		prediction = plane[x-1]
	case x == 0:
		prediction = plane[(y-1)*width]
	default:
		ra, rb, rc := plane[y*width+x-1], plane[(y-1)*width+x], plane[(y-1)*width+x-1]
		switch predictor {
		case 1:
			prediction = ra
		case 2:
			prediction = rb
		case 3:
			prediction = rc
		case 4:
			prediction = ra + rb - rc
		case 5:
			prediction = ra + (rb-rc)>>1
		case 6:
			prediction = rb + (ra-rc)>>1
		case 7:
			prediction = (ra + rb) / 2
		}
	}
	// H.1.2.1: the difference is taken modulo 2^16 whatever the precision,
	// which is what bounds it to 16 bits when the prediction is far off.
	d := (plane[y*width+x] - prediction) & 0xFFFF
	if d >= 0x8000 {
		d -= 0x10000
	}
	return d
}

// losslessCategory returns a difference's category, SSSS of T.81 Table H.2,
// and the bits that follow its code. -32768 is the same difference as 32768
// modulo 2^16, which is category 16 and has no bits after it.
func losslessCategory(d int32) (uint8, uint32) {
	if d == -32768 {
		return 16, 0
	}
	return magnitude(int(d))
}

// countDifferences adds up how often each category occurs in a plane coded
// with predictor.
func countDifferences(plane []int32, width, height, depth, predictor int, frequencies *[256]int) {
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			category, _ := losslessCategory(losslessDifference(plane, x, y, width, depth, predictor))
			frequencies[category]++
		}
	}
}

// cheapestPredictor returns the selection value, 1 to 7, that codes the
// planes in the fewest bits: each code's length in a table built for it,
// and the category's worth of bits after it.
//
// It is what makes .57 worth asking for over .70. Which predictor suits an
// image depends on it: an image with strong vertical structure is cheaper
// predicted from above, and a smooth one from a blend. Counting is cheaper
// than coding seven times, and the count is exact.
func cheapestPredictor(planes [][]int32, width, height, depth int) int {
	best, bestCost := 1, -1
	for predictor := 1; predictor <= 7; predictor++ {
		cost := 0
		for _, plane := range planes {
			var frequencies [256]int
			countDifferences(plane, width, height, depth, predictor, &frequencies)
			codes := huffmanCodes(optimalHuffman(&frequencies))
			cost += huffmanCost(&frequencies, &codes)
			for category := 1; category < 16; category++ {
				cost += frequencies[category] * category
			}
		}
		if bestCost < 0 || cost < bestCost {
			best, bestCost = predictor, cost
		}
	}
	return best
}
