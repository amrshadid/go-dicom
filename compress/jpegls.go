package compress

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// JPEG-LS (ITU-T T.87 / ISO-IEC 14495-1) decoding.
//
// DICOM uses 1.2.840.10008.1.2.4.80 for lossless and .81 for near-lossless. It
// shares nothing with the other JPEGs but the marker framing: no DCT and no
// Huffman tables, but a context model that adapts as it goes, a median edge
// predictor, Golomb-Rice codes whose parameter is derived from the running error
// statistics, and a separate run mode for flat regions.
//
// That adaptation is why the decoder cannot be checked a sample at a time
// against a reference: every decoded value feeds the state that decodes the
// next, so a single wrong bit diverges the rest of the image. Either a frame
// comes out exactly right or it comes out visibly wrong, which makes
// whole-image comparison against a third party the only test worth writing.
//
// Single and multi component, both interleave modes, lossless and near-lossless.

const (
	markerSOF55 = 0xF7 // JPEG-LS frame header
	markerLSE   = 0xF8 // JPEG-LS preset parameters
)

// Context modeling constants, T.87 A.3.
const (
	// contextCount is the 365 regular contexts plus the two used when a run is
	// interrupted.
	contextCount   = 367
	runContextBase = 365
	maxC           = 127
	minC           = -128
	defaultReset   = 64
	basicT1        = 3
	basicT2        = 7
	basicT3        = 21
	// maxJPEGLSSamples bounds the frame a header may declare.
	//
	// 2^25 samples is 8192x4096, comfortably past digital mammography and the
	// largest CR plates, and it is a bound on work as much as on memory: the
	// header is attacker-controlled, and a frame this size claimed by twenty
	// bytes of input would otherwise be allocated and walked before the missing
	// entropy data was noticed.
	maxJPEGLSSamples = 1 << 25
)

// runLengthOrder is J from T.87 table A.2: the exponent of the run length coded
// by each successive run-mode symbol, so long flat regions cost logarithmically.
var runLengthOrder = [32]int{
	0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3,
	4, 4, 5, 5, 6, 6, 7, 7, 8, 9, 10, 11, 12, 13, 14, 15,
}

var errNotJPEGLS = errors.New("jpegls: not a JPEG-LS stream")

// JPEGLSDecompressor decodes JPEG-LS frames, lossless and near-lossless.
//
// It satisfies Decompressor, so it is what GetExternalRegistry returns for
// JPEG_LS. It needs no C library.
type JPEGLSDecompressor struct{}

// NewJPEGLSDecompressor returns a decoder for JPEG-LS.
func NewJPEGLSDecompressor() *JPEGLSDecompressor {
	return &JPEGLSDecompressor{}
}

// CanDecompress reports whether data looks like a JPEG-LS stream.
//
// SOF55 is the discriminator. Checking only for SOI would claim every JPEG
// variant, including the baseline frames the standard library decodes.
func (d *JPEGLSDecompressor) CanDecompress(data []byte) bool {
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return false
	}
	for i := 2; i+1 < len(data); {
		if data[i] != 0xFF {
			i++
			continue
		}
		switch data[i+1] {
		case markerSOF55:
			return true
		case markerSOF0, markerSOF1, markerSOF3, markerSOF5, markerSOF7, markerSOS, markerEOI:
			return false
		}
		i += 2
	}
	return false
}

// Decompress decodes one JPEG-LS frame to raw pixel bytes.
//
// Samples come back pixel-interleaved, little endian when the precision needs
// two bytes, matching the rest of this package.
func (d *JPEGLSDecompressor) Decompress(data []byte) ([]byte, error) {
	img, err := decodeJPEGLS(data)
	if err != nil {
		return nil, err
	}
	return img.pack(), nil
}

// jpeglsImage is a decoded frame. Components are stored as separate planes and
// interleaved on the way out.
type jpeglsImage struct {
	width, height int
	precision     int
	planes        [][]int32
}

func (img *jpeglsImage) pack() []byte {
	n := img.width * img.height
	nc := len(img.planes)

	if img.precision <= 8 {
		out := make([]byte, n*nc)
		for c, plane := range img.planes {
			for i := 0; i < n; i++ {
				out[i*nc+c] = byte(plane[i])
			}
		}
		return out
	}

	out := make([]byte, n*nc*2)
	for c, plane := range img.planes {
		for i := 0; i < n; i++ {
			binary.LittleEndian.PutUint16(out[(i*nc+c)*2:], uint16(plane[i]))
		}
	}
	return out
}

// jpeglsParams holds the coding parameters, either defaults derived from the
// precision or the presets an LSE segment supplied.
type jpeglsParams struct {
	maxVal int
	near   int
	t1     int
	t2     int
	t3     int
	reset  int

	rangeVal int
	qbpp     int
	bpp      int
	limit    int
}

// computeDefaults fills in the thresholds T.87 A.1 derives from MAXVAL and NEAR,
// for a stream that carries no LSE segment. Most do not.
func (p *jpeglsParams) computeDefaults(presetT bool) {
	maxVal, near := p.maxVal, p.near

	if !presetT {
		if maxVal >= 128 {
			factor := (minInt(maxVal, 4095) + 128) / 256
			p.t1 = factor*(basicT1-2) + 2 + 3*near
			p.t2 = factor*(basicT2-3) + 3 + 5*near
			p.t3 = factor*(basicT3-4) + 4 + 7*near
		} else {
			factor := 256 / (maxVal + 1)
			p.t1 = maxInt(2, basicT1/factor+3*near)
			p.t2 = maxInt(3, basicT2/factor+5*near)
			p.t3 = maxInt(4, basicT3/factor+7*near)
		}
		// The thresholds must stay ordered and within range, which the formulas
		// above can violate for unusual precisions.
		p.t1 = clampInt(p.t1, near+1, maxVal)
		p.t2 = clampInt(p.t2, p.t1, maxVal)
		p.t3 = clampInt(p.t3, p.t2, maxVal)
	}
	if p.reset == 0 {
		p.reset = defaultReset
	}

	p.rangeVal = (maxVal+2*near)/(2*near+1) + 1
	p.qbpp = ceilLog2(p.rangeVal)
	p.bpp = maxInt(2, ceilLog2(maxVal+1))
	p.limit = 2 * (p.bpp + maxInt(8, p.bpp))
}

// quantize maps a local gradient to one of nine buckets, T.87 A.3.3.
func (p *jpeglsParams) quantize(d int32) int32 {
	t1, t2, t3 := int32(p.t1), int32(p.t2), int32(p.t3)
	near := int32(p.near)
	switch {
	case d <= -t3:
		return -4
	case d <= -t2:
		return -3
	case d <= -t1:
		return -2
	case d < -near:
		return -1
	case d <= near:
		return 0
	case d < t1:
		return 1
	case d < t2:
		return 2
	case d < t3:
		return 3
	}
	return 4
}

// jpeglsContext is the per-context adaptive state, T.87 A.3.
//
// a accumulates the magnitude of the prediction errors and n counts them, and
// their ratio picks the Golomb parameter. b and c carry the bias correction: b
// accumulates signed error and c is the correction applied to the prediction,
// nudged by one whenever b shows the predictor drifting.
type jpeglsContext struct {
	a int32
	b int32
	c int32
	n int32

	// nn counts negative errors, and exists only for the two contexts used when
	// a run is interrupted. Those contexts do not use b or c: a run ends on a
	// difference, so the question is not whether the predictor drifts but which
	// way the interrupting sample fell, and nn is what the error mapping keys
	// on there.
	nn int32
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func ceilLog2(v int) int {
	n := 0
	for (1 << n) < v {
		n++
	}
	return n
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// jpeglsBitReader reads the entropy-coded segment.
//
// JPEG-LS stuffs differently from the other JPEGs: after a 0xFF byte the next
// byte carries only seven bits, with its top bit forced to zero, so no marker
// can appear by accident. There is no 0xFF 0x00 pair to skip.
type jpeglsBitReader struct {
	data    []byte
	pos     int
	bits    uint64
	n       uint
	overrun int
}

func (r *jpeglsBitReader) fill() {
	for r.n <= 56 {
		if r.pos >= len(r.data) {
			r.overrun++
			r.bits |= 0
			r.n += 8
			continue
		}
		prevWasFF := r.pos > 0 && r.data[r.pos-1] == 0xFF
		c := r.data[r.pos]
		if prevWasFF {
			// Seven data bits follow a 0xFF; the high bit is not part of the
			// stream. A real marker would have the bit set, and the scan is over.
			if c&0x80 != 0 {
				r.overrun++
				r.n += 8
				continue
			}
			r.pos++
			r.bits |= uint64(c&0x7F) << (57 - r.n)
			r.n += 7
			continue
		}
		r.pos++
		r.bits |= uint64(c) << (56 - r.n)
		r.n += 8
	}
}

func (r *jpeglsBitReader) readBit() int32 {
	if r.n == 0 {
		r.fill()
	}
	bit := int32(r.bits >> 63)
	r.bits <<= 1
	r.n--
	return bit
}

func (r *jpeglsBitReader) readBits(count int) int32 {
	var v int32
	for i := 0; i < count; i++ {
		v = v<<1 | r.readBit()
	}
	return v
}

// readUnary reads zeros until a one, bounded so a corrupt stream cannot spin.
func (r *jpeglsBitReader) readUnary(limit int) (int, error) {
	count := 0
	for r.readBit() == 0 {
		count++
		if count > limit {
			return 0, fmt.Errorf("jpegls: unary code exceeds %d bits, the stream is corrupt", limit)
		}
	}
	return count, nil
}
