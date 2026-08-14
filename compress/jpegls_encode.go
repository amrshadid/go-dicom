package compress

import (
	"fmt"
)

// JPEG-LS encoding, ITU-T T.87.
//
// This is the mirror of jpegls_decode.go, and the two are deliberately written
// against each other symbol for symbol: the same context quantization, the same
// Golomb parameter, the same bias correction, the same run-mode order table. An
// encoder that disagrees with its decoder about any of those produces a stream
// only itself can read, which this project has shipped once already — the RLE
// encoder emitted frames with no segment header and its own decompressor accepted
// them because it had the matching defect.
//
// So agreement with the decoder here is necessary and not sufficient. The tests
// hand the output to dcmtk's dcmdjpls, which has no shared code with either.
//
// # Scope
//
// Lossless only, NEAR=0. Near-lossless encoding is a different rate decision — how
// much error to accept — and belongs to the caller rather than being chosen here.
// The decoder reads both.
//
// Components are coded non-interleaved (ILV=0), one scan each, which is what
// T.87 B.2.3 defines for separate planes and what keeps the encoder a single loop
// per component rather than three interleaving modes.

// jpeglsEncoder holds the coding state for one scan.
//
// The fields are the decoder's, with the same meanings, because every one of them
// is part of the contract between them.
type jpeglsEncoder struct {
	params *jpeglsParams
	writer *jpeglsBitWriter

	contexts [contextCount]jpeglsContext

	runIndex int

	// prev and cur are one line each with a guard element at either end, so the
	// edge rules in T.87 A.2 fall out of the indexing rather than needing a branch
	// per sample — as in the decoder.
	prev []int32
	cur  []int32
}

// EncodeJPEGLS encodes samples as a lossless JPEG-LS frame.
//
// samples holds the components interleaved per pixel, width*height pixels in
// raster order, each 16-bit sample little endian — the layout DICOM native pixel
// data has under the Explicit VR Little Endian syntax this library normalises to,
// so a caller passes what PixelData already holds.
//
// The result is a complete JPEG-LS codestream: SOI, SOF55, one SOS per component,
// EOI. It is what goes in an encapsulated pixel data fragment.
func EncodeJPEGLS(samples []byte, width, height, components, bitsStored int) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("jpegls: %dx%d is not an image", width, height)
	}
	if components < 1 || components > 255 {
		return nil, fmt.Errorf("jpegls: %d components is outside 1 to 255", components)
	}
	if bitsStored < 2 || bitsStored > 16 {
		// T.87 allows 2 to 16 bits. One-bit data has no Golomb behavior to speak
		// of and the standard excludes it.
		return nil, fmt.Errorf("jpegls: %d bits stored is outside the 2 to 16 T.87 allows", bitsStored)
	}

	bytesPerSample := 1
	if bitsStored > 8 {
		bytesPerSample = 2
	}

	want := width * height * components * bytesPerSample
	if len(samples) != want {
		return nil, fmt.Errorf("jpegls: got %d bytes of samples, want %d for %dx%d, "+
			"%d component(s) at %d bits", len(samples), want, width, height, components, bitsStored)
	}

	params := &jpeglsParams{maxVal: (1 << bitsStored) - 1, near: 0}
	params.computeDefaults(false)

	out := make([]byte, 0, len(samples)/2+64)
	out = appendMarker(out, markerSOI)
	out = appendJPEGLSFrameHeader(out, width, height, components, bitsStored)

	// One scan per component. Each is coded independently, which is what ILV=0
	// means, so a flat channel cannot lend its run state to a busy one.
	for c := 0; c < components; c++ {
		out = appendJPEGLSScanHeader(out, c)

		plane := extractPlane(samples, width, height, components, c, bytesPerSample)

		encoder := newJPEGLSEncoder(params, width)
		encoded, err := encoder.encodePlane(plane, width, height)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded...)
	}

	out = appendMarker(out, markerEOI)
	return out, nil
}

// extractPlane pulls one component out of interleaved samples.
//
// DICOM native multi-sample data is sample-interleaved by default — RGBRGBRGB —
// so a plane has to be gathered with a stride rather than sliced.
func extractPlane(samples []byte, width, height, components, comp, bytesPerSample int) []int32 {
	plane := make([]int32, width*height)
	stride := components * bytesPerSample
	offset := comp * bytesPerSample

	for i := range plane {
		at := i*stride + offset
		if bytesPerSample == 1 {
			plane[i] = int32(samples[at])
		} else {
			// Little endian, matching what the decoder's pack writes back and what
			// DICOM native pixel data holds under the Explicit VR Little Endian
			// syntax this library normalises everything to. Reading these
			// big-endian byte-swapped every 16-bit sample, which showed up as the
			// first sample of a flat image decoding to 0x34 where 0x12 went in.
			plane[i] = int32(samples[at]) | int32(samples[at+1])<<8
		}
	}
	return plane
}

func newJPEGLSEncoder(params *jpeglsParams, width int) *jpeglsEncoder {
	e := &jpeglsEncoder{
		params: params,
		writer: &jpeglsBitWriter{},
		prev:   make([]int32, width+2),
		cur:    make([]int32, width+2),
	}
	e.resetContexts()
	return e
}

// resetContexts initializes the statistics, T.87 A.2.1 — the same initial A as
// the decoder, or the first Golomb parameter differs and nothing decodes.
func (e *jpeglsEncoder) resetContexts() {
	initA := int32(maxInt(2, (e.params.rangeVal+32)/64))
	for i := range e.contexts {
		e.contexts[i] = jpeglsContext{a: initA, b: 0, c: 0, n: 1, nn: 0}
	}
	e.runIndex = 0
}

// encodePlane codes one component and returns its entropy-coded segment.
//
// The line handling mirrors decodePlane and startLine rather than special-casing
// the first row. The decoder has no first-row branch: prev is zeroed, so the row
// above reads as zeros and Ra comes out as prev[1], which is zero too. Writing an
// encoder that treats row zero specially — predicting from the left, as the
// standard's prose suggests — produces a stream that disagrees with it at the very
// first sample. It did, until this was mirrored instead of reasoned about.
func (e *jpeglsEncoder) encodePlane(plane []int32, width, height int) ([]byte, error) {
	maxVal := int32(e.params.maxVal)

	for y := 0; y < height; y++ {
		row := plane[y*width : (y+1)*width]
		for x := range row {
			if row[x] < 0 || row[x] > maxVal {
				return nil, fmt.Errorf("jpegls: sample %d at row %d column %d is outside 0 to %d",
					row[x], y, x, maxVal)
			}
			e.cur[x+1] = row[x]
		}

		// startLine's two guards, in the same order.
		e.cur[0] = e.prev[1]
		e.prev[width+1] = e.prev[width]

		if err := e.encodeRow(y, width); err != nil {
			return nil, err
		}

		// The line just coded becomes the line above, as the decoder rotates.
		e.prev, e.cur = e.cur, e.prev
	}

	e.writer.flush()
	return e.writer.bytes(), nil
}

// encodeRow codes one line, choosing regular or run mode per sample.
//
// The same loop as decodeRow, including advancing by what a run consumed rather
// than by one.
func (e *jpeglsEncoder) encodeRow(y, width int) error {
	x := 1
	for x <= width {
		ra, rb, rc, rd := e.cur[x-1], e.prev[x], e.prev[x-1], e.prev[x+1]

		d1, d2, d3 := rd-rb, rb-rc, rc-ra
		if e.isFlat(d1, d2, d3) {
			consumed, err := e.encodeRun(x, width, ra)
			if err != nil {
				return fmt.Errorf("at row %d column %d: %w", y, x-1, err)
			}
			x += consumed
			continue
		}

		if err := e.encodeRegular(e.cur[x], ra, rb, rc, d1, d2, d3); err != nil {
			return fmt.Errorf("at row %d column %d: %w", y, x-1, err)
		}
		x++
	}
	return nil
}

// isFlat is the decoder's test, byte for byte: the run-mode entry condition.
func (e *jpeglsEncoder) isFlat(d1, d2, d3 int32) bool {
	near := int32(e.params.near)
	return abs32(d1) <= near && abs32(d2) <= near && abs32(d3) <= near
}

// encodeRegular codes one sample in regular mode, T.87 A.4 through A.6.
//
// Every step is the decoder's in reverse, in the same order, because the context
// update depends on the error and the error mapping depends on the context.
func (e *jpeglsEncoder) encodeRegular(sample, ra, rb, rc, d1, d2, d3 int32) error {
	q1 := e.params.quantize(d1)
	q2 := e.params.quantize(d2)
	q3 := e.params.quantize(d3)

	q := q1*81 + q2*9 + q3
	sign := int32(1)
	if q < 0 {
		q = -q
		sign = -1
	}
	if q >= runContextBase {
		return fmt.Errorf("jpegls: context %d is outside the %d the standard defines", q, runContextBase)
	}
	ctx := &e.contexts[q]

	px := medianPredict(ra, rb, rc)
	px = clamp32(px+sign*ctx.c, 0, int32(e.params.maxVal))

	errval := sign * (sample - px)

	// The error is reduced modulo the range, so a prediction that lands far from
	// the sample still costs a bounded number of bits. This is on the error, not
	// on the mapped value, and the decoder's reconstruct undoes it by folding the
	// reconstructed sample back into range.
	rangeVal := int32(e.params.rangeVal)
	if errval < 0 {
		errval += rangeVal
	}
	if errval >= (rangeVal+1)/2 {
		errval -= rangeVal
	}

	k := golombK(ctx.a, ctx.n)

	// Fold the signed error onto a non-negative integer. Which mapping applies
	// depends on the bias state, and the decoder reproduces this same test — see
	// decodeRegular.
	var mErrval int32
	if e.params.near == 0 && k == 0 && 2*ctx.b <= -ctx.n {
		if errval >= 0 {
			mErrval = 2*errval + 1
		} else {
			mErrval = -2*errval - 2
		}
	} else {
		if errval >= 0 {
			mErrval = 2 * errval
		} else {
			mErrval = -2*errval - 1
		}
	}

	e.writer.writeGolomb(mErrval, k, e.params.limit, e.params.qbpp)
	e.updateContext(ctx, errval)
	return nil
}

// updateContext is the decoder's, unchanged. Both sides must fold the error into
// the statistics identically or they diverge after the first sample.
func (e *jpeglsEncoder) updateContext(ctx *jpeglsContext, errval int32) {
	ctx.b += errval * int32(2*e.params.near+1)
	ctx.a += abs32(errval)

	if ctx.n == int32(e.params.reset) {
		ctx.a >>= 1
		ctx.b >>= 1
		ctx.n >>= 1
	}
	ctx.n++

	switch {
	case ctx.b <= -ctx.n:
		ctx.b += ctx.n
		if ctx.c > minC {
			ctx.c--
		}
		if ctx.b <= -ctx.n {
			ctx.b = -ctx.n + 1
		}
	case ctx.b > 0:
		ctx.b -= ctx.n
		if ctx.c < maxC {
			ctx.c++
		}
		if ctx.b > 0 {
			ctx.b = 0
		}
	}
}

// encodeRun codes a run of samples equal to Ra plus its interruption, and reports
// how many samples it covered.
//
// The mirror of decodeRunLength and decodeRun. Two details decide whether it
// round-trips, and both were wrong when this was written from the prose:
//
// The terminating zero bit is not conditional. decodeRunLength consumes 1-bits in
// a loop that exits on a 0, so a run that does not reach the end of the line must
// always be followed by one — including a run of length zero, which is a single 0
// bit and nothing else.
//
// The run index advances only when a whole block fit. A block cut short by the end
// of the line does not earn a longer code next time, and the decoder is explicit
// about it.
func (e *jpeglsEncoder) encodeRun(x, width int, ra int32) (int, error) {
	remaining := width - x + 1

	// How far the run actually goes, bounded by the line.
	runLength := 0
	for runLength < remaining && e.cur[x+runLength] == ra {
		runLength++
	}

	count := 0
	for {
		block := 1 << runLengthOrder[e.runIndex]
		if count+block > runLength {
			break
		}

		e.writer.writeBit(1)
		count += block
		if e.runIndex < 31 {
			e.runIndex++
		}

		if count >= remaining {
			// The line ended inside the run: no terminator and no interrupting
			// sample, matching the decoder's early return.
			return maxInt(count, 1), nil
		}
	}

	// The run ended before a whole block would have fit.
	e.writer.writeBit(0)
	if j := runLengthOrder[e.runIndex]; j > 0 {
		e.writer.writeBits(int32(runLength-count), j)
	}
	count = runLength

	if x+count > width {
		return maxInt(count, 1), nil
	}

	// The sample that broke the run.
	if err := e.encodeRunInterruption(e.cur[x+count], ra, e.prev[x+count]); err != nil {
		return 0, err
	}
	e.decrementRunIndex()
	return count + 1, nil
}

// encodeRunInterruption codes the sample that ended a run, T.87 A.7.2.
//
// The mirror of decodeRunInterruption, including the two things its comments warn
// about: which of the two reserved contexts applies, and the sign convention whose
// second half reads as 2*nn >= n rather than <.
func (e *jpeglsEncoder) encodeRunInterruption(sample, ra, rb int32) error {
	riType := 0
	if abs32(ra-rb) <= int32(e.params.near) {
		riType = 1
	}

	px := rb
	if riType == 1 {
		px = ra
	}

	ctx := &e.contexts[runContextBase+riType]

	temp := ctx.a
	if riType == 1 {
		temp += ctx.n >> 1
	}
	k := golombK(temp, ctx.n)

	errval := sample - px

	// A run interrupted against a smaller neighbor is coded with its error
	// negated so both directions share one set of statistics. The decoder undoes
	// this after the mapping, so the encoder applies it before.
	if riType == 0 && ra > rb {
		errval = -errval
	}

	// Reduced modulo the range, as in regular mode. Without this a 16-bit sample
	// predicted from zero gives an error near 65536, whose mapped value needs 17
	// bits, and the escape path writes qbpp — 16 — so the top bit was silently
	// dropped. It showed up as every 16-bit sample decoding correct in its low
	// byte and wrong by 0x80 in its high one.
	//
	// The decoder does not undo this here; its reconstruct folds the sample back
	// into range afterwards, which is the same mechanism regular mode relies on.
	rangeVal := int32(e.params.rangeVal)
	if errval < 0 {
		errval += rangeVal
	}
	if errval >= (rangeVal+1)/2 {
		errval -= rangeVal
	}

	// The mapping the decoder inverts: it recovers absErr and a map bit, and picks
	// the sign from whether "negative" agrees with that bit.
	negative := k != 0 || 2*ctx.nn >= ctx.n

	absErr := errval
	if absErr < 0 {
		absErr = -absErr
	}

	var mapBit int32
	if errval < 0 {
		// The decoder reads a negative when negative == (mapBit == 1).
		if negative {
			mapBit = 1
		}
	} else if !negative {
		mapBit = 1
	}

	// absErr = (e + mapBit) >> 1 with mapBit = e & 1, so e follows from both.
	eValue := 2 * absErr
	if mapBit == 1 {
		eValue--
	}
	eMErrval := eValue - int32(riType)

	e.writer.writeGolomb(eMErrval, k,
		e.params.limit-runLengthOrder[e.runIndex]-1, e.params.qbpp)

	// Counted in the same frame of reference the mapping used, before the
	// orientation is undone — as the decoder does.
	if errval < 0 {
		ctx.nn++
	}
	ctx.a += (eMErrval + 1 - int32(riType)) >> 1
	if ctx.n == int32(e.params.reset) {
		ctx.a >>= 1
		ctx.nn >>= 1
		ctx.n >>= 1
	}
	ctx.n++

	return nil
}

// decrementRunIndex is the decoder's, T.87 A.7.1.2.
func (e *jpeglsEncoder) decrementRunIndex() {
	if e.runIndex > 0 {
		e.runIndex--
	}
}

// jpeglsBitWriter writes the entropy-coded segment, stuffing as T.87 requires.
//
// The mirror of jpeglsBitReader: after a 0xFF byte only seven bits go in the next,
// with the top bit left zero, so no marker can appear by accident. There is no
// 0xFF 0x00 pair, which is where JPEG-LS differs from the other JPEGs and where an
// encoder written from the wrong standard produces a stream that decodes as
// garbage after the first 0xFF.
type jpeglsBitWriter struct {
	out  []byte
	bits uint32
	n    uint
}

func (w *jpeglsBitWriter) writeBit(bit int32) {
	w.bits = w.bits<<1 | uint32(bit&1)
	w.n++
	w.emitFullBytes()
}

func (w *jpeglsBitWriter) writeBits(value int32, count int) {
	for i := count - 1; i >= 0; i-- {
		w.writeBit((value >> uint(i)) & 1)
	}
}

// emitFullBytes flushes whole bytes, taking seven bits at a time when the previous
// byte was 0xFF.
func (w *jpeglsBitWriter) emitFullBytes() {
	for {
		width := uint(8)
		if len(w.out) > 0 && w.out[len(w.out)-1] == 0xFF {
			width = 7
		}
		if w.n < width {
			return
		}

		shift := w.n - width
		b := byte((w.bits >> shift) & ((1 << width) - 1))
		w.n = shift
		w.bits &= (1 << shift) - 1

		// A seven-bit byte carries its data in the low bits with the top bit
		// zero, which is exactly what the reader takes back out.
		w.out = append(w.out, b)
	}
}

// flush pads the final partial byte with zero bits.
//
// Zeros rather than ones: a trailing run of ones could form the first half of a
// marker, and the decoder reads a set high bit after 0xFF as the end of the scan.
func (w *jpeglsBitWriter) flush() {
	if w.n == 0 {
		return
	}

	width := uint(8)
	if len(w.out) > 0 && w.out[len(w.out)-1] == 0xFF {
		width = 7
	}
	for w.n < width {
		w.bits <<= 1
		w.n++
	}
	w.emitFullBytes()
}

func (w *jpeglsBitWriter) bytes() []byte { return w.out }

// writeGolomb writes one mapped error, T.87 A.5.3.
//
// Below the limit it is a unary quotient then k remainder bits. At or above it,
// the escape: limit-qbpp-1 zeros, a one, then the value in qbpp bits — which
// bounds the cost of an outlier instead of emitting a unary code the length of the
// range.
func (w *jpeglsBitWriter) writeGolomb(value int32, k, limit, qbpp int) {
	high := value >> uint(k)

	if int(high) < limit-qbpp-1 {
		for i := int32(0); i < high; i++ {
			w.writeBit(0)
		}
		w.writeBit(1)
		if k > 0 {
			w.writeBits(value, k)
		}
		return
	}

	for i := 0; i < limit-qbpp-1; i++ {
		w.writeBit(0)
	}
	w.writeBit(1)
	w.writeBits(value-1, qbpp)
}

// appendMarker appends a two-byte marker.
func appendMarker(out []byte, marker byte) []byte {
	return append(out, 0xFF, marker)
}

// appendJPEGLSFrameHeader appends the SOF55 segment, T.87 C.2.
func appendJPEGLSFrameHeader(out []byte, width, height, components, bitsStored int) []byte {
	out = appendMarker(out, markerSOF55)

	length := 8 + 3*components
	out = append(out, byte(length>>8), byte(length))
	out = append(out, byte(bitsStored))
	out = append(out, byte(height>>8), byte(height))
	out = append(out, byte(width>>8), byte(width))
	out = append(out, byte(components))

	for c := 0; c < components; c++ {
		// Component identifier, then sampling factors of 1:1 — no subsampling,
		// which is the only sensible choice for lossless — then a quantization
		// table selector JPEG-LS does not use.
		out = append(out, byte(c+1), 0x11, 0x00)
	}
	return out
}

// appendJPEGLSScanHeader appends an SOS segment for one component, T.87 C.3.
func appendJPEGLSScanHeader(out []byte, comp int) []byte {
	out = appendMarker(out, markerSOS)

	// Length 8: the two length bytes, the component count, one component's two
	// bytes, then NEAR, ILV and the point transform.
	out = append(out, 0x00, 0x08)
	out = append(out, 0x01)               // one component in this scan
	out = append(out, byte(comp+1), 0x00) // its identifier, no mapping table
	out = append(out, 0x00)               // NEAR: lossless
	out = append(out, 0x00)               // ILV: non-interleaved
	out = append(out, 0x00)               // point transform
	return out
}
