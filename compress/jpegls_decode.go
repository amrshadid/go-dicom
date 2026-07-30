package compress

import (
	"encoding/binary"
	"fmt"
)

// maxJPEGLSTrailingSlack is how many bytes past the end of the entropy-coded
// data a valid frame may appear to need.
//
// It is larger than the equivalent for lossless JPEG because this reader keeps a
// 64-bit window: it fetches up to eight bytes beyond what it has consumed, so a
// frame that ends exactly on its last needed bit still reports an overrun. Two
// more cover the EOI marker the scan runs up against.
const maxJPEGLSTrailingSlack = 10

// jpeglsScan describes one scan's coding options, from its SOS header.
type jpeglsScan struct {
	components []int // indices into the frame's component list
	near       int
	interleave int // 0 none, 1 line, 2 sample
	pointTrans int
}

// decodeJPEGLS parses a JPEG-LS stream and decodes its scans.
func decodeJPEGLS(data []byte) (*jpeglsImage, error) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != markerSOI {
		return nil, fmt.Errorf("%w: missing SOI", errNotJPEGLS)
	}

	var (
		img       *jpeglsImage
		compIDs   []byte
		params    jpeglsParams
		presetT   bool
		haveFrame bool
		scans     int
		pos       = 2
	)

	for pos+1 < len(data) {
		if data[pos] != 0xFF {
			pos++
			continue
		}
		marker := data[pos+1]
		pos += 2

		switch marker {
		case 0xFF:
			pos--
			continue
		case markerSOI, markerEOI, markerTEM:
			continue
		}
		if marker >= markerRST0 && marker <= markerRST7 {
			continue
		}

		if pos+2 > len(data) {
			return nil, fmt.Errorf("jpegls: segment 0x%02X has no length", marker)
		}
		segLen := int(binary.BigEndian.Uint16(data[pos:]))
		if segLen < 2 || pos+segLen > len(data) {
			return nil, fmt.Errorf("jpegls: segment 0x%02X declares %d bytes, %d remain",
				marker, segLen, len(data)-pos)
		}
		body := data[pos+2 : pos+segLen]

		switch marker {
		case markerSOF55:
			var err error
			if img, compIDs, err = parseJPEGLSFrameHeader(body); err != nil {
				return nil, err
			}
			params.maxVal = (1 << img.precision) - 1
			haveFrame = true
			pos += segLen

		case markerSOF0, markerSOF1, markerSOF3, markerSOF5, markerSOF7:
			return nil, fmt.Errorf("%w: frame marker 0x%02X is not JPEG-LS (SOF55)", errNotJPEGLS, marker)

		case markerLSE:
			var err error
			if presetT, err = parseLSE(body, &params); err != nil {
				return nil, err
			}
			pos += segLen

		case markerSOS:
			if !haveFrame {
				return nil, fmt.Errorf("jpegls: scan before any frame header")
			}
			scan, err := parseJPEGLSScanHeader(body, compIDs)
			if err != nil {
				return nil, err
			}

			scanParams := params
			scanParams.near = scan.near
			scanParams.computeDefaults(presetT)

			consumed, err := decodeJPEGLSScan(img, scan, &scanParams, data[pos+segLen:])
			if err != nil {
				return nil, err
			}
			scans++
			pos += segLen + consumed

		default:
			pos += segLen
		}
	}

	if img == nil {
		return nil, fmt.Errorf("%w: no SOF55 frame header", errNotJPEGLS)
	}
	if scans == 0 {
		return nil, fmt.Errorf("jpegls: frame carries no scan")
	}
	return img, nil
}

// parseJPEGLSFrameHeader reads SOF55, which has the same shape as any SOF.
func parseJPEGLSFrameHeader(body []byte) (*jpeglsImage, []byte, error) {
	if len(body) < 6 {
		return nil, nil, fmt.Errorf("jpegls: SOF55 header is %d bytes, want at least 6", len(body))
	}

	precision := int(body[0])
	height := int(binary.BigEndian.Uint16(body[1:]))
	width := int(binary.BigEndian.Uint16(body[3:]))
	numComponents := int(body[5])

	if precision < 2 || precision > 16 {
		return nil, nil, fmt.Errorf("jpegls: precision %d is outside the 2..16 the standard allows", precision)
	}
	if width == 0 || height == 0 {
		return nil, nil, fmt.Errorf("jpegls: frame is %dx%d", width, height)
	}
	if numComponents == 0 {
		return nil, nil, fmt.Errorf("jpegls: frame declares no components")
	}
	if width*height > maxJPEGLSSamples/numComponents {
		return nil, nil, fmt.Errorf("jpegls: frame of %dx%d with %d components is too large to decode",
			width, height, numComponents)
	}
	if len(body) < 6+numComponents*3 {
		return nil, nil, fmt.Errorf("jpegls: SOF55 declares %d components but carries %d bytes",
			numComponents, len(body)-6)
	}

	img := &jpeglsImage{width: width, height: height, precision: precision}
	ids := make([]byte, numComponents)
	for i := 0; i < numComponents; i++ {
		off := 6 + i*3
		ids[i] = body[off]
		h := int(body[off+1] >> 4)
		v := int(body[off+1] & 0x0F)
		if h != 1 || v != 1 {
			return nil, nil, fmt.Errorf("jpegls: component %d has sampling factors %dx%d, "+
				"only 1x1 is supported", i, h, v)
		}
		img.planes = append(img.planes, make([]int32, width*height))
	}
	return img, ids, nil
}

// parseLSE reads a preset parameters segment, returning whether it set the
// gradient thresholds.
func parseLSE(body []byte, params *jpeglsParams) (bool, error) {
	if len(body) < 1 {
		return false, fmt.Errorf("jpegls: empty LSE segment")
	}
	// Only type 1 carries coding parameters; the others are mapping tables this
	// decoder does not need.
	if body[0] != 1 {
		return false, nil
	}
	if len(body) < 11 {
		return false, fmt.Errorf("jpegls: LSE type 1 is %d bytes, want 11", len(body))
	}

	maxVal := int(binary.BigEndian.Uint16(body[1:]))
	t1 := int(binary.BigEndian.Uint16(body[3:]))
	t2 := int(binary.BigEndian.Uint16(body[5:]))
	t3 := int(binary.BigEndian.Uint16(body[7:]))
	reset := int(binary.BigEndian.Uint16(body[9:]))

	if maxVal > 0 {
		params.maxVal = maxVal
	}
	if reset > 0 {
		params.reset = reset
	}
	if t1 == 0 && t2 == 0 && t3 == 0 {
		return false, nil
	}
	if t1 > t2 || t2 > t3 || t3 > params.maxVal {
		return false, fmt.Errorf("jpegls: LSE thresholds %d,%d,%d are not ordered within MAXVAL %d",
			t1, t2, t3, params.maxVal)
	}
	params.t1, params.t2, params.t3 = t1, t2, t3
	return true, nil
}

// parseJPEGLSScanHeader reads SOS. NEAR sits where a DCT process puts Ss, and
// the interleave mode where it puts Se.
func parseJPEGLSScanHeader(body []byte, compIDs []byte) (*jpeglsScan, error) {
	if len(body) < 1 {
		return nil, fmt.Errorf("jpegls: empty SOS header")
	}
	ns := int(body[0])
	if ns < 1 || ns > len(compIDs) {
		return nil, fmt.Errorf("jpegls: scan names %d components, frame has %d", ns, len(compIDs))
	}
	if len(body) < 1+ns*2+3 {
		return nil, fmt.Errorf("jpegls: SOS header is %d bytes, too short for %d components",
			len(body), ns)
	}

	scan := &jpeglsScan{
		near:       int(body[1+ns*2]),
		interleave: int(body[1+ns*2+1]),
		pointTrans: int(body[1+ns*2+2] & 0x0F),
	}
	if scan.interleave > 2 {
		return nil, fmt.Errorf("jpegls: interleave mode %d is outside the 0..2 the standard defines",
			scan.interleave)
	}
	if scan.interleave != 0 && ns == 1 {
		// One component cannot be interleaved with anything; treat it as a plain
		// scan rather than refusing a file that is merely over-specified.
		scan.interleave = 0
	}
	if scan.interleave == 0 && ns != 1 {
		return nil, fmt.Errorf("jpegls: a non-interleaved scan must name one component, not %d", ns)
	}
	if ns != 1 {
		// Refused rather than decoded. The interleaved paths decode small frames
		// correctly and diverge on larger ones, somewhere in how the context and
		// run state are shared between components — and a decoder that is right
		// until row 11 is worse than one that says it cannot do this, because the
		// output looks like an image either way.
		//
		// Single-component frames, which is what CT, MR, CR and most other
		// modalities produce, are decoded and verified. Register your own decoder
		// with compress.GetExternalRegistry().RegisterExternalDecoder to handle
		// color, or transfer the instance without decoding it.
		return nil, fmt.Errorf("jpegls: %d-component (color) JPEG-LS is not decoded; "+
			"only single-component frames are supported", ns)
	}

	for i := 0; i < ns; i++ {
		id := body[1+i*2]
		idx := -1
		for j, cid := range compIDs {
			if cid == id {
				idx = j
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("jpegls: scan names component %d, which the frame does not declare", id)
		}
		scan.components = append(scan.components, idx)
	}
	return scan, nil
}

// jpeglsDecoder holds the state one scan decodes with.
type jpeglsDecoder struct {
	img    *jpeglsImage
	params *jpeglsParams
	reader *jpeglsBitReader

	contexts [contextCount]jpeglsContext

	// runIndex is the position in the run-length order table, carried between
	// runs so a flat image keeps coding longer runs per symbol.
	//
	// Line-interleaved scans keep one per component in runIndexes and swap the
	// right one in around each line: the components are coded as independent
	// lines that happen to share a scan, so a flat green channel must not lend
	// its run length to a busy red one. Sample-interleaved scans have a single
	// index, because there a run is one run across all components at once.
	runIndex int

	// One previous and current line per component in the scan, each with a
	// guard element at either end so the edge rules in T.87 A.2 fall out of the
	// indexing instead of needing a branch per sample.
	prev [][]int32
	cur  [][]int32
}

// decodeJPEGLSScan decodes one scan and reports the bytes it consumed.
func decodeJPEGLSScan(img *jpeglsImage, scan *jpeglsScan, params *jpeglsParams,
	entropy []byte) (int, error) {

	d := &jpeglsDecoder{
		img:    img,
		params: params,
		reader: &jpeglsBitReader{data: entropy},
	}
	d.resetContexts()

	n := len(scan.components)
	d.prev = make([][]int32, n)
	d.cur = make([][]int32, n)
	for i := range d.prev {
		d.prev[i] = make([]int32, img.width+2)
		d.cur[i] = make([]int32, img.width+2)
	}

	if err := d.decodePlane(scan.components[0]); err != nil {
		return 0, err
	}

	if d.reader.overrun > maxJPEGLSTrailingSlack {
		return 0, fmt.Errorf("jpegls: entropy-coded data ends %d bytes early; the frame is "+
			"truncated and the samples past that point would be invented", d.reader.overrun)
	}
	return d.reader.pos, nil
}

// resetContexts sets the adaptive state T.87 A.8 starts each scan from.
func (d *jpeglsDecoder) resetContexts() {
	// A starts at a value derived from the range so the first Golomb parameter
	// is sane before any statistics exist.
	initA := int32(maxInt(2, (d.params.rangeVal+32)/64))
	for i := range d.contexts {
		d.contexts[i] = jpeglsContext{a: initA, b: 0, c: 0, n: 1, nn: 0}
	}
	d.runIndex = 0
}

// decodePlane decodes a scan carrying a single component.
func (d *jpeglsDecoder) decodePlane(comp int) error {
	width, height := d.img.width, d.img.height
	plane := d.img.planes[comp]

	for y := 0; y < height; y++ {
		d.startLine(0)
		if err := d.decodeRow(0, y, 0, 1); err != nil {
			return err
		}

		// Checked per row rather than once at the end. A header can claim a
		// frame of any size, and without this a few bytes of entropy data would
		// be decoded into millions of samples out of fabricated zero bits before
		// anything noticed — slow enough to be worth doing to a server.
		if d.reader.overrun > maxJPEGLSTrailingSlack {
			return fmt.Errorf("entropy-coded data ran out at row %d of %d; the frame is "+
				"truncated and the samples past that point would be invented", y, height)
		}

		copy(plane[y*width:(y+1)*width], d.cur[0][1:width+1])
		d.prev[0], d.cur[0] = d.cur[0], d.prev[0]
	}
	return nil
}

func (d *jpeglsDecoder) startLine(i int) {
	width := d.img.width
	d.cur[i][0] = d.prev[i][1]
	d.prev[i][width+1] = d.prev[i][width]
}

// decodeRow decodes one row of one component.
func (d *jpeglsDecoder) decodeRow(buf, y, _ int, _ int) error {
	width := d.img.width
	prev, cur := d.prev[buf], d.cur[buf]

	x := 1
	for x <= width {
		ra, rb, rc, rd := cur[x-1], prev[x], prev[x-1], prev[x+1]

		d1, d2, d3 := rd-rb, rb-rc, rc-ra
		if d.isFlat(d1, d2, d3) {
			consumed, err := d.decodeRun(prev, cur, x, width, ra, rb)
			if err != nil {
				return fmt.Errorf("at row %d column %d: %w", y, x-1, err)
			}
			x += consumed
			continue
		}

		value, err := d.decodeRegular(ra, rb, rc, d1, d2, d3)
		if err != nil {
			return fmt.Errorf("at row %d column %d: %w", y, x-1, err)
		}
		cur[x] = value
		x++
	}
	return nil
}

func (d *jpeglsDecoder) isFlat(d1, d2, d3 int32) bool {
	near := int32(d.params.near)
	return abs32(d1) <= near && abs32(d2) <= near && abs32(d3) <= near
}

// decodeRegular decodes one sample in regular mode, T.87 A.4 through A.6.
func (d *jpeglsDecoder) decodeRegular(ra, rb, rc, d1, d2, d3 int32) (int32, error) {
	q1, q2, q3 := d.params.quantize(d1), d.params.quantize(d2), d.params.quantize(d3)

	// The 365 contexts are the 729 sign-bearing combinations folded in half:
	// a context and its negation share statistics, with sign applied to the
	// correction and the error rather than doubling the table.
	q := q1*81 + q2*9 + q3
	sign := int32(1)
	if q < 0 {
		q = -q
		sign = -1
	}
	if q >= runContextBase {
		return 0, fmt.Errorf("jpegls: context %d is outside the %d the standard defines", q, runContextBase)
	}
	ctx := &d.contexts[q]

	px := medianPredict(ra, rb, rc)
	px = clamp32(px+sign*ctx.c, 0, int32(d.params.maxVal))

	k := golombK(ctx.a, ctx.n)
	mErrval, err := d.decodeGolomb(k, d.params.limit, d.params.qbpp)
	if err != nil {
		return 0, err
	}

	// Undo the mapping that folded signed errors onto non-negative integers.
	// Which of the two mappings was used depends on the bias state, so the
	// decoder has to reproduce the same test the encoder made.
	// The bias-driven alternative mapping applies only to lossless coding.
	var errval int32
	if d.params.near == 0 && k == 0 && 2*ctx.b <= -ctx.n {
		if mErrval%2 == 1 {
			errval = (mErrval - 1) / 2
		} else {
			errval = -mErrval/2 - 1
		}
	} else {
		if mErrval%2 == 0 {
			errval = mErrval / 2
		} else {
			errval = -(mErrval + 1) / 2
		}
	}

	d.updateContext(ctx, errval)

	value := px + sign*errval*int32(2*d.params.near+1)
	return d.reconstruct(value), nil
}

// reconstruct folds a value back into range, T.87 A.4.5. A prediction error can
// legitimately carry a sample past the ends, and the encoder relies on the
// decoder wrapping rather than clamping.
func (d *jpeglsDecoder) reconstruct(value int32) int32 {
	maxVal := int32(d.params.maxVal)
	span := int32(d.params.rangeVal * (2*d.params.near + 1))

	// The window that wraps is [-NEAR, MAXVAL+NEAR], not [0, MAXVAL]: a
	// near-lossless reconstruction is allowed to land just outside the range
	// before it is clamped, and folding it early would wrap values that should
	// merely have been clipped. The two are the same test when NEAR is 0, which
	// is why lossless frames decode correctly either way.
	near := int32(d.params.near)
	if value < -near {
		value += span
	} else if value > maxVal+near {
		value -= span
	}
	return clamp32(value, 0, maxVal)
}

// updateContext folds one prediction error into a context's statistics,
// T.87 A.6.1, including the bias correction that keeps the predictor centered.
func (d *jpeglsDecoder) updateContext(ctx *jpeglsContext, errval int32) {
	ctx.b += errval * int32(2*d.params.near+1)
	ctx.a += abs32(errval)

	if ctx.n == int32(d.params.reset) {
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

// medianPredict is the median edge detector, T.87 A.4.2: it predicts the edge
// when the neighbors suggest one and falls back to a planar estimate otherwise.
func medianPredict(ra, rb, rc int32) int32 {
	if rc >= maxInt32(ra, rb) {
		return minInt32(ra, rb)
	}
	if rc <= minInt32(ra, rb) {
		return maxInt32(ra, rb)
	}
	return ra + rb - rc
}

// golombK picks the Golomb parameter from the running statistics, T.87 A.5.1:
// the smallest k for which n<<k reaches a.
func golombK(a, n int32) int {
	k := 0
	for (n << k) < a {
		k++
		if k > 31 {
			break
		}
	}
	return k
}

// decodeGolomb reads one limited-length Golomb-Rice code, T.87 A.5.3.
//
// The limit matters: without it a corrupt or adversarial stream could ask for an
// unbounded unary prefix. Past the limit the value is sent as a plain binary
// number instead, which is also what keeps the worst case bounded for real data.
func (d *jpeglsDecoder) decodeGolomb(k, limit, qbpp int) (int32, error) {
	prefix, err := d.reader.readUnary(limit)
	if err != nil {
		return 0, err
	}

	if prefix < limit-qbpp-1 {
		value := int32(prefix) << k
		if k > 0 {
			value |= d.reader.readBits(k)
		}
		return value, nil
	}
	// Escape: the remainder is qbpp bits, biased by one.
	return d.reader.readBits(qbpp) + 1, nil
}

// decodeRun decodes a run of samples equal to Ra, and the sample that
// interrupted it. Returns how many columns it filled.
func (d *jpeglsDecoder) decodeRun(prev, cur []int32, x, width int, ra, rb int32) (int, error) {
	remaining := width - x + 1

	count, hitEndOfLine, err := d.decodeRunLength(remaining)
	if err != nil {
		return 0, err
	}

	for i := 0; i < count && x+i <= width; i++ {
		cur[x+i] = ra
	}
	if hitEndOfLine || x+count > width {
		return maxInt(count, 1), nil
	}

	// The run ended because a sample differed; decode it in one of the two
	// contexts reserved for interrupted runs.
	value, err := d.decodeRunInterruption(ra, prev[x+count], false)
	if err != nil {
		return 0, err
	}
	cur[x+count] = value
	d.decrementRunIndex()
	return count + 1, nil
}

func (d *jpeglsDecoder) decodeRunLength(remaining int) (count int, hitEndOfLine bool, err error) {
	for d.reader.readBit() == 1 {
		block := 1 << runLengthOrder[d.runIndex]
		if block > remaining-count {
			block = remaining - count
		}
		count += block

		// The index advances only when a whole block fit. A block cut short by
		// the end of the line does not earn a longer code next time.
		if block == 1<<runLengthOrder[d.runIndex] && d.runIndex < 31 {
			d.runIndex++
		}
		if count >= remaining {
			// The line ended inside the run, so no interrupting sample follows.
			return remaining, true, nil
		}
	}

	if j := runLengthOrder[d.runIndex]; j > 0 {
		count += int(d.reader.readBits(j))
	}
	if count > remaining {
		return 0, false, fmt.Errorf("jpegls: run of %d samples overruns the %d left in the line",
			count, remaining)
	}
	return count, false, nil
}

// decodeRunInterruption decodes the sample that ended a run, T.87 A.7.2.
//
// It uses one of two contexts depending on whether the run's value matched the
// sample above, because those two cases have very different error statistics.
func (d *jpeglsDecoder) decodeRunInterruption(ra, rb int32, joint bool) (int32, error) {
	// A sample-interleaved run covered every component at once, so whether the
	// run value matched the sample above was decided for the pixel as a whole.
	// Asking it again per component would pick a context the encoder did not
	// use: those interruptions always take the first one, predicting from Rb.
	riType := 0
	if !joint && abs32(ra-rb) <= int32(d.params.near) {
		riType = 1
	}

	px := rb
	if riType == 1 {
		px = ra
	}

	ctx := &d.contexts[runContextBase+riType]

	// The second context is offset by half its count, so its Golomb parameter
	// reflects that a run ending where the sample above matched is a rarer,
	// larger error than one ending where it did not.
	temp := ctx.a
	if riType == 1 {
		temp += ctx.n >> 1
	}
	k := golombK(temp, ctx.n)

	eMErrval, err := d.decodeGolomb(k, d.params.limit-runLengthOrder[d.runIndex]-1, d.params.qbpp)
	if err != nil {
		return 0, err
	}

	// The encoder wrote 2|Errval| - RItype - map, where map records which of two
	// sign conventions it used. Adding RItype back leaves 2|Errval| - map, whose
	// low bit is map, since 2|Errval| is even. Everything else follows.
	e := eMErrval + int32(riType)
	mapBit := e & 1
	absErr := (e + mapBit) >> 1

	// Which convention was in force depends on the same test the encoder made.
	// Note the shape of it: the negative case is "k is non-zero OR negatives are
	// still in the minority", not the negation of a single condition. Reading it
	// as one decodes most of an image correctly and then diverges, because the
	// two only disagree once the statistics have moved.
	negative := k != 0 || 2*ctx.nn < ctx.n
	var errval int32
	if negative == (mapBit == 1) {
		errval = -absErr
	} else {
		errval = absErr
	}

	// Counted before the orientation is undone: nn tracks the error in the same
	// frame of reference the mapping used.
	if errval < 0 {
		ctx.nn++
	}
	ctx.a += (eMErrval + 1 - int32(riType)) >> 1
	if ctx.n == int32(d.params.reset) {
		ctx.a >>= 1
		ctx.nn >>= 1
		ctx.n >>= 1
	}
	ctx.n++

	// A run interrupted against a smaller neighbor was coded with its error
	// negated, so that both directions share one set of statistics.
	if riType == 0 && ra > rb {
		errval = -errval
	}

	return d.reconstruct(px + errval*int32(2*d.params.near+1)), nil
}

// decrementRunIndex steps back down the run-length order after a run ends.
//
// Once per run, not once per sample: an interleaved run is interrupted by one
// sample of each component, and every one of them derives its Golomb limit from
// the index, so it has to hold still until they are all decoded.
func (d *jpeglsDecoder) decrementRunIndex() {
	if d.runIndex > 0 {
		d.runIndex--
	}
}

func clamp32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
