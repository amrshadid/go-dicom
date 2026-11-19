package jpeg2000

import "math/bits"

// The tier-1 block encoder, PS 15444-1 Annex D: the other half of tier1.go.
//
// It walks a code-block exactly as the decoder does, a bit plane at a time
// and each plane in its three passes, and at every place the decoder asks
// the MQ coder for a decision, this tells the MQ coder what the decision is.
// The context of each decision comes from passState, which the two share, so
// they cannot choose differently.
//
// Every bit plane is coded, down to the last: that is what makes the result
// lossless. A lossy encoder would stop early, and choosing where is most of
// what a lossy encoder is.

// EncodedBlock is one code-block as tier-1 leaves it: what a packet carries
// for it.
type EncodedBlock struct {
	// Data is the block's MQ-coded bytes, one codeword segment.
	Data []byte
	// Passes is how many coding passes Data holds: three for each bit plane
	// but the first, which has the cleanup pass alone.
	Passes int
	// BitPlanes is how many bit planes the block's largest magnitude needs.
	// The planes above them, up to what the band can hold, are the zero bit
	// planes a packet header declares. A block of zeros has none, no passes
	// and no data.
	BitPlanes int
}

// blockEncoder encodes one code-block.
type blockEncoder struct {
	passState
	// magnitudes are the absolute values of the coefficients, row by row,
	// and negative says which were below zero.
	magnitudes []uint32
	negative   []bool

	mq *mqEncoder

	// exact holds each coefficient's magnitude before it was quantized, in
	// units of the step, when the encoder is to measure distortion; nil
	// otherwise. plane is the bit plane being coded, and gained the squared
	// error the passes so far have taken away.
	exact  []float64
	plane  int
	gained float64
	// wholeAtZero says a decoder rebuilds a magnitude read to plane 0 as
	// itself, as the reversible path does, not the middle of its interval.
	wholeAtZero bool
}

// truncationPoint is where a code-block's data may be cut (J.14.1): after a
// coding pass, with the bytes a decoder needs to read every pass up to it,
// and the squared error, in units of the step, that those passes remove from
// the block when it is decoded.
type truncationPoint struct {
	passes int
	bytes  int
	gained float64
}

// truncationSlack is how many bytes past those the MQ coder has written a
// truncation point keeps. The coder holds up to 27 bits it has not written
// out, and a decoder must read them to decide the pass's last symbols the
// way the encoder did; three bytes is the margin OpenJPEG's encoder keeps,
// and TestTruncatedBlocksDecodeAsCounted checks it against this package's
// decoder at every truncation point of every block it codes.
const truncationSlack = 3

// encodeBlockTruncations encodes one code-block, as EncodeBlock does, and
// records a truncation point after every coding pass. exact gives the
// coefficients' magnitudes before quantization, in steps.
func encodeBlockTruncations(coefficients []int32, exact []float64, w, h, bandType int, reversible bool) (EncodedBlock, []truncationPoint, error) {
	return encodeBlock(coefficients, exact, w, h, bandType, reversible)
}

// EncodeBlock encodes the coefficients of one code-block, w by h of them, row
// by row, for a band of the given type.
func EncodeBlock(coefficients []int32, w, h, bandType int) (EncodedBlock, error) {
	block, _, err := encodeBlock(coefficients, nil, w, h, bandType, false)
	return block, err
}

// encodeBlock is EncodeBlock, and with exact set, encodeBlockTruncations.
func encodeBlock(coefficients []int32, exact []float64, w, h, bandType int, reversible bool) (EncodedBlock, []truncationPoint, error) {
	if w <= 0 || h <= 0 || len(coefficients) != w*h || (exact != nil && len(exact) != w*h) {
		return EncodedBlock{}, nil, errorf("%d coefficients for a code-block of %dx%d", len(coefficients), w, h)
	}
	if w > 1<<12 || h > 1<<12 || w*h > 1<<22 {
		return EncodedBlock{}, nil, errorf("a code-block of %dx%d samples is larger than any legal one", w, h)
	}

	e := &blockEncoder{
		passState:   newPassState(w, h, bandType),
		magnitudes:  make([]uint32, w*h),
		negative:    make([]bool, w*h),
		mq:          newMQEncoder(numContexts),
		exact:       exact,
		wholeAtZero: reversible,
	}
	largest := uint32(0)
	for i, c := range coefficients {
		magnitude := uint32(c)
		if c < 0 {
			magnitude = uint32(-int64(c))
			e.negative[i] = true
		}
		e.magnitudes[i] = magnitude
		largest |= magnitude
	}
	planes := bits.Len32(largest)
	if planes == 0 {
		return EncodedBlock{}, nil, nil
	}
	// The decoder keeps its estimates in half units and so reads 30 planes
	// at most. A reversible transform of 16-bit samples stays far below.
	if planes > 30 {
		return EncodedBlock{}, nil, errorf("a coefficient of %d bit planes, past the 30 a code-block is read to", planes)
	}

	for _, start := range initialContexts {
		e.mq.setContext(int(start[0]), start[1], 0)
	}

	// The first plane has the cleanup pass alone: nothing is significant
	// yet for the other two to work on (D.2).
	passes := 0
	var points []truncationPoint
	mark := func() {
		passes++
		if e.exact != nil {
			points = append(points, truncationPoint{passes: passes, bytes: len(e.mq.out) + truncationSlack, gained: e.gained})
		}
	}
	for plane := planes - 1; plane >= 0; plane-- {
		e.plane = plane
		if plane != planes-1 {
			e.significancePass(plane)
			mark()
			e.refinementPass(plane)
			mark()
		}
		e.cleanupPass(plane)
		mark()
	}
	data := e.mq.flush()
	// A point can claim no more than the whole, nor end on an 0xFF that the
	// next byte would have made half of a marker; and the bytes never shrink
	// from one pass to the next.
	for i := range points {
		p := &points[i]
		p.bytes = min(p.bytes, len(data))
		if p.bytes > 0 && p.bytes < len(data) && data[p.bytes-1] == 0xFF {
			p.bytes--
		}
		if i > 0 && p.bytes < points[i-1].bytes {
			p.bytes = points[i-1].bytes
		}
	}
	return EncodedBlock{Data: data, Passes: passes, BitPlanes: planes}, points, nil
}

// reconstruction is what a decoder rebuilds a magnitude of q as, in steps,
// when it has read the bits of q down to plane p: the middle of the interval
// those bits leave, or nothing while q has no bit above p.
func reconstruction(q uint32, p int) float64 {
	top := q >> uint(p)
	if top == 0 {
		return 0
	}
	return (float64(top) + 0.5) * float64(uint32(1)<<uint(p))
}

// gain counts what coding the bit of coefficient i in the current plane
// takes off the block's squared error: its reconstruction moves from what
// the planes above gave to what this one gives.
func (e *blockEncoder) gain(i int) {
	if e.exact == nil {
		return
	}
	x, q := e.exact[i], e.magnitudes[i]
	before := x - reconstruction(q, e.plane+1)
	after := x - reconstruction(q, e.plane)
	if e.wholeAtZero && e.plane == 0 {
		after = x - float64(q)
	}
	e.gained += before*before - after*after
}

// bit is the coefficient's bit in a plane.
func (e *blockEncoder) bit(x, y, plane int) int {
	return int(e.magnitudes[y*e.w+x]>>uint(plane)) & 1
}

// becomeSignificant records a coefficient's first one bit and codes its
// sign, as the decoder's setSignificant and decodeSign read them.
func (e *blockEncoder) becomeSignificant(x, y, i int) {
	e.markSignificant(i)
	// The context is chosen from the neighbors' signs before this one's is
	// recorded, which is the order the decoder has them in.
	cx, xorBit := e.signContext(i)
	sign := 0
	if e.negative[y*e.w+x] {
		sign = 1
	}
	e.mq.encode(cx, sign^xorBit)
	if sign == 1 {
		e.flags[i] |= flagNegative
	}
	e.gain(y*e.w + x)
}

// significancePass is D.3.1, coding what the decoder's reads.
func (e *blockEncoder) significancePass(plane int) {
	for y0 := 0; y0 < e.h; y0 += 4 {
		for x := 0; x < e.w; x++ {
			i := e.index(x, y0)
			for y := y0; y < y0+4 && y < e.h; y, i = y+1, i+e.stride {
				f := e.flags[i]
				if f&flagSignificant != 0 || f&flagNeighbors == 0 {
					continue
				}
				bit := e.bit(x, y, plane)
				e.mq.encode(e.significanceContext(i), bit)
				if bit == 1 {
					e.becomeSignificant(x, y, i)
				}
				e.flags[i] |= flagVisited
			}
		}
	}
}

// refinementPass is D.3.2.
func (e *blockEncoder) refinementPass(plane int) {
	for y0 := 0; y0 < e.h; y0 += 4 {
		for x := 0; x < e.w; x++ {
			i := e.index(x, y0)
			for y := y0; y < y0+4 && y < e.h; y, i = y+1, i+e.stride {
				f := e.flags[i]
				if f&flagSignificant == 0 || f&flagVisited != 0 {
					continue
				}
				cx := cxRefineMore
				if f&flagRefined == 0 {
					if f&flagNeighbors == 0 {
						cx = cxRefineNone
					} else {
						cx = cxRefineNear
					}
				}
				e.mq.encode(cx, e.bit(x, y, plane))
				e.flags[i] |= flagRefined
				e.gain(y*e.w + x)
			}
		}
	}
}

// cleanupPass is D.3.4, with its run-length mode: a column of four that is
// insignificant with no significant neighbor is coded as one decision when
// all four stay zero in this plane, and otherwise as where the first one is.
func (e *blockEncoder) cleanupPass(plane int) {
	for y0 := 0; y0 < e.h; y0 += 4 {
		for x := 0; x < e.w; x++ {
			y := y0
			for y < y0+4 && y < e.h {
				i := e.index(x, y)

				if y == y0 && y0+4 <= e.h && e.columnIsClean(i) {
					first := 4
					for k := 0; k < 4; k++ {
						if e.bit(x, y0+k, plane) == 1 {
							first = k
							break
						}
					}
					if first == 4 {
						e.mq.encode(cxRunLen, 0)
						e.clearVisited(i, 4)
						y = y0 + 4
						continue
					}
					e.mq.encode(cxRunLen, 1)
					e.mq.encode(cxUniform, first>>1)
					e.mq.encode(cxUniform, first&1)
					e.clearVisited(i, first)
					y = y0 + first
					i += first * e.stride
					e.becomeSignificant(x, y, i)
					e.flags[i] &^= flagVisited
					y++
					continue
				}

				if e.flags[i]&(flagSignificant|flagVisited) == 0 {
					bit := e.bit(x, y, plane)
					e.mq.encode(e.significanceContext(i), bit)
					if bit == 1 {
						e.becomeSignificant(x, y, i)
					}
				}
				e.flags[i] &^= flagVisited
				y++
			}
		}
	}
}
