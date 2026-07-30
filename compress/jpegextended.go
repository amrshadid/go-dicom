package compress

import (
	"errors"
	"fmt"
	"math"
)

// JPEG Extended (1.2.840.10008.1.2.4.51) allows 8-bit and 12-bit sample
// precision. The standard library decodes the 8-bit case — its decoder accepts
// SOF1 frames and rejects only the precision — so what is here is the 12-bit
// one.
//
// Twelve-bit sequential DCT is baseline JPEG with a wider sample: the same
// Huffman-coded DC differences and run/size AC pairs, the same dequantize and
// inverse DCT, differing in the level shift (2048 rather than 128), the clamp
// (0..4095), and in permitting four DC and four AC tables where baseline
// permits two.
//
// Progressive frames are not decoded. They are a different scan structure
// entirely — coefficients arrive across several scans by spectral band and bit
// position — and DICOM does not define a transfer syntax for them.

const markerDQT = 0xDB

// maxSequentialPixels bounds a frame before any allocation, so a header
// claiming an implausible size cannot exhaust memory ahead of the size checks
// the caller applies to the result.
const maxSequentialPixels = 1 << 26

// zigzag maps a coefficient's index in the coded order to its position in the
// 8x8 block. T.81 figure A.6.
var zigzag = [64]int{
	0, 1, 8, 16, 9, 2, 3, 10,
	17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34,
	27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36,
	29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46,
	53, 60, 61, 54, 47, 55, 62, 63,
}

// sequentialComponent is one component of a sequential DCT frame.
type sequentialComponent struct {
	id      byte
	h, v    int // sampling factors
	tq      int // quantization table selector
	dcTable int
	acTable int
	pred    int32 // DC predictor, reset at each restart interval

	// blocksPerLine and blocksPerColumn cover the whole frame, rounded up to
	// the MCU grid, so a partial MCU at the right or bottom edge has somewhere
	// to decode into.
	blocksPerLine   int
	blocksPerColumn int
	samples         []uint16 // blocksPerLine*8 by blocksPerColumn*8
}

// sequentialDecoder holds the tables a stream accumulates before its scan.
type sequentialDecoder struct {
	precision     int
	width, height int
	components    []*sequentialComponent
	quant         [4][64]uint16
	dcTables      [4]*huffTable
	acTables      [4]*huffTable
	restart       int
	hMax, vMax    int
	progressive   bool
}

// decodeSequentialJPEG decodes an 8- or 12-bit sequential DCT frame.
func decodeSequentialJPEG(data []byte) (*losslessImage, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return nil, errNotSequentialJPEG
	}

	d := &sequentialDecoder{}
	pos := 2

	for pos+1 < len(data) {
		if data[pos] != 0xFF {
			pos++
			continue
		}
		marker := data[pos+1]
		pos += 2

		switch marker {
		case 0xFF: // fill byte
			pos--
			continue
		case markerSOI, markerEOI, markerTEM:
			continue
		}
		if marker >= markerRST0 && marker <= markerRST7 {
			continue
		}

		if pos+2 > len(data) {
			return nil, errors.New("jpegextended: a segment header runs past the end of the stream")
		}
		length := int(data[pos])<<8 | int(data[pos+1])
		if length < 2 || pos+length > len(data) {
			return nil, fmt.Errorf("jpegextended: segment %02X declares %d bytes, which does not fit",
				marker, length)
		}
		body := data[pos+2 : pos+length]

		switch marker {
		case markerSOF0, markerSOF1:
			if err := d.parseFrameHeader(body); err != nil {
				return nil, err
			}
		case 0xC2: // SOF2, progressive
			d.progressive = true
			return nil, errNotSequentialJPEG
		case markerDQT:
			if err := d.parseQuantTables(body); err != nil {
				return nil, err
			}
		case markerDHT:
			if err := d.parseHuffmanTables(body); err != nil {
				return nil, err
			}
		case markerDRI:
			if len(body) < 2 {
				return nil, errors.New("jpegextended: the restart interval segment is too short")
			}
			d.restart = int(body[0])<<8 | int(body[1])
		case markerSOS:
			if d.width == 0 {
				return nil, errors.New("jpegextended: the scan arrives before any frame header")
			}
			if err := d.decodeScan(body, data[pos+length:]); err != nil {
				return nil, err
			}
			return d.image()
		}
		pos += length
	}
	return nil, errors.New("jpegextended: the stream ends before its scan")
}

// errNotSequentialJPEG reports a stream this decoder is not meant to handle.
var errNotSequentialJPEG = errors.New("jpegextended: not a sequential DCT JPEG stream")

// parseFrameHeader reads SOF0 or SOF1 and sizes the component planes.
func (d *sequentialDecoder) parseFrameHeader(body []byte) error {
	if len(body) < 6 {
		return errors.New("jpegextended: the frame header is too short")
	}
	d.precision = int(body[0])
	d.height = int(body[1])<<8 | int(body[2])
	d.width = int(body[3])<<8 | int(body[4])
	count := int(body[5])

	if d.precision != 8 && d.precision != 12 {
		return fmt.Errorf("jpegextended: sample precision %d is not 8 or 12", d.precision)
	}
	if d.width <= 0 || d.height <= 0 {
		return fmt.Errorf("jpegextended: the frame is %dx%d", d.width, d.height)
	}
	if count < 1 || count > 4 {
		return fmt.Errorf("jpegextended: the frame declares %d components", count)
	}
	if d.width*d.height > maxSequentialPixels {
		return fmt.Errorf("jpegextended: a %dx%d frame is beyond the %d pixel limit",
			d.width, d.height, maxSequentialPixels)
	}
	if len(body) < 6+count*3 {
		return errors.New("jpegextended: the frame header is shorter than its component list")
	}

	d.components = nil
	for i := 0; i < count; i++ {
		o := 6 + i*3
		c := &sequentialComponent{
			id: body[o],
			h:  int(body[o+1] >> 4),
			v:  int(body[o+1] & 0x0F),
			tq: int(body[o+2]),
		}
		if c.h < 1 || c.h > 4 || c.v < 1 || c.v > 4 {
			return fmt.Errorf("jpegextended: component %d has sampling factors %dx%d", c.id, c.h, c.v)
		}
		if c.tq > 3 {
			return fmt.Errorf("jpegextended: component %d selects quantization table %d", c.id, c.tq)
		}
		d.components = append(d.components, c)
		d.hMax = max(d.hMax, c.h)
		d.vMax = max(d.vMax, c.v)
	}

	// Each component is decoded on its own block grid, sized to the MCU grid so
	// a partial MCU at an edge still has blocks to write into.
	mcusPerLine := ceilDiv(d.width, 8*d.hMax)
	mcusPerColumn := ceilDiv(d.height, 8*d.vMax)
	for _, c := range d.components {
		c.blocksPerLine = mcusPerLine * c.h
		c.blocksPerColumn = mcusPerColumn * c.v
		n := c.blocksPerLine * 8 * c.blocksPerColumn * 8
		if n <= 0 || n > maxSequentialPixels*4 {
			return fmt.Errorf("jpegextended: component %d needs %d samples", c.id, n)
		}
		c.samples = make([]uint16, n)
	}
	return nil
}

// parseQuantTables reads a DQT segment, which may hold several tables.
func (d *sequentialDecoder) parseQuantTables(body []byte) error {
	for len(body) > 0 {
		pq, tq := int(body[0]>>4), int(body[0]&0x0F)
		if tq > 3 {
			return fmt.Errorf("jpegextended: quantization table %d is outside 0..3", tq)
		}
		if pq != 0 && pq != 1 {
			return fmt.Errorf("jpegextended: quantization table %d has precision code %d", tq, pq)
		}
		body = body[1:]

		width := 1 + pq // 16-bit entries when pq is 1, which 12-bit frames use
		if len(body) < 64*width {
			return fmt.Errorf("jpegextended: quantization table %d is truncated", tq)
		}
		for i := 0; i < 64; i++ {
			var v uint16
			if pq == 0 {
				v = uint16(body[i])
			} else {
				v = uint16(body[i*2])<<8 | uint16(body[i*2+1])
			}
			if v == 0 {
				return fmt.Errorf("jpegextended: quantization table %d has a zero at %d", tq, i)
			}
			d.quant[tq][zigzag[i]] = v
		}
		body = body[64*width:]
	}
	return nil
}

// parseHuffmanTables reads a DHT segment into the DC and AC table sets.
func (d *sequentialDecoder) parseHuffmanTables(body []byte) error {
	for len(body) >= 17 {
		class, id := int(body[0]>>4), int(body[0]&0x0F)
		if class > 1 {
			return fmt.Errorf("jpegextended: Huffman table class %d is neither DC nor AC", class)
		}
		if id > 3 {
			return fmt.Errorf("jpegextended: Huffman table %d is outside 0..3", id)
		}

		counts := body[1:17]
		total := 0
		for _, n := range counts {
			total += int(n)
		}
		if total > 256 || len(body) < 17+total {
			return fmt.Errorf("jpegextended: Huffman table %d declares %d values", id, total)
		}

		table, err := buildHuffTable(counts, body[17:17+total])
		if err != nil {
			return err
		}
		if class == 0 {
			d.dcTables[id] = table
		} else {
			d.acTables[id] = table
		}
		body = body[17+total:]
	}
	return nil
}

// decodeScan decodes the entropy-coded data following an SOS header.
func (d *sequentialDecoder) decodeScan(header, entropy []byte) error {
	if len(header) < 1 {
		return errors.New("jpegextended: the scan header is empty")
	}
	count := int(header[0])
	if count < 1 || count > len(d.components) {
		return fmt.Errorf("jpegextended: the scan names %d components", count)
	}
	if len(header) < 1+count*2+3 {
		return errors.New("jpegextended: the scan header is shorter than its component list")
	}

	scan := make([]*sequentialComponent, 0, count)
	for i := 0; i < count; i++ {
		id, tables := header[1+i*2], header[2+i*2]
		var comp *sequentialComponent
		for _, c := range d.components {
			if c.id == id {
				comp = c
				break
			}
		}
		if comp == nil {
			return fmt.Errorf("jpegextended: the scan names component %d, which the frame does not declare", id)
		}
		comp.dcTable = int(tables >> 4)
		comp.acTable = int(tables & 0x0F)
		if comp.dcTable > 3 || comp.acTable > 3 {
			return fmt.Errorf("jpegextended: component %d selects Huffman tables %d and %d",
				id, comp.dcTable, comp.acTable)
		}
		comp.pred = 0
		scan = append(scan, comp)
	}

	// Sequential frames code the whole spectrum in one scan. A stream saying
	// otherwise is progressive or damaged, and decoding it as sequential would
	// return an image built from coefficients that were never there.
	o := 1 + count*2
	ss, se, a := int(header[o]), int(header[o+1]), header[o+2]
	if ss != 0 || se != 63 || a != 0 {
		return fmt.Errorf("jpegextended: the scan covers coefficients %d..%d at bit position %d/%d; "+
			"a sequential frame codes 0..63 in one scan", ss, se, a>>4, a&0x0F)
	}

	r := &bitReader{data: entropy}

	// A single-component scan walks that component's own block grid rather than
	// the MCU grid, so a subsampled component is not padded out to the MCU.
	if len(scan) == 1 {
		c := scan[0]
		across := ceilDiv(d.width*c.h, 8*d.hMax)
		down := ceilDiv(d.height*c.v, 8*d.vMax)
		return d.walk(r, across*down, func(n int) error {
			return d.decodeBlock(r, c, n/across, n%across)
		})
	}

	mcusPerLine := ceilDiv(d.width, 8*d.hMax)
	mcusPerColumn := ceilDiv(d.height, 8*d.vMax)
	return d.walk(r, mcusPerLine*mcusPerColumn, func(n int) error {
		mcuRow, mcuCol := n/mcusPerLine, n%mcusPerLine
		for _, c := range scan {
			for v := 0; v < c.v; v++ {
				for h := 0; h < c.h; h++ {
					if err := d.decodeBlock(r, c, mcuRow*c.v+v, mcuCol*c.h+h); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// walk runs one decode step per unit, honoring the restart interval.
func (d *sequentialDecoder) walk(r *bitReader, units int, step func(int) error) error {
	interval := d.restart
	if interval <= 0 {
		interval = units
	}
	for n := 0; n < units; n++ {
		if n > 0 && n%interval == 0 {
			r.reset()
			for _, c := range d.components {
				c.pred = 0
			}
		}
		if err := step(n); err != nil {
			return err
		}
		// Entropy data exhausted well before the frame is decoded means the
		// stream is truncated; the remaining blocks would be built from
		// fabricated zero bits.
		if r.overrun > maxTrailingSlack && n < units-1 {
			return fmt.Errorf("jpegextended: the entropy-coded data ends after %d of %d units", n+1, units)
		}
	}
	return nil
}

// decodeBlock decodes one 8x8 block into the component's sample plane.
func (d *sequentialDecoder) decodeBlock(r *bitReader, c *sequentialComponent, row, col int) error {
	if row >= c.blocksPerColumn || col >= c.blocksPerLine {
		return nil // a block past the edge of the padded grid
	}
	dc, ac := d.dcTables[c.dcTable], d.acTables[c.acTable]
	if dc == nil || ac == nil {
		return fmt.Errorf("jpegextended: component %d uses Huffman tables that were never defined", c.id)
	}
	q := &d.quant[c.tq]

	var block [64]int32

	// DC: a Huffman-coded category, then that many bits of magnitude, taken as
	// a difference from the previous block of the same component.
	t, err := r.decodeHuff(dc)
	if err != nil {
		return err
	}
	if t > 15 {
		return fmt.Errorf("jpegextended: DC category %d is outside 0..15", t)
	}
	diff := int32(0)
	if t > 0 {
		diff = extend(r.readBits(int(t)), int(t))
	}
	c.pred += diff
	block[0] = c.pred * int32(q[0])

	// AC: run/size pairs until a block-end symbol or the 63rd coefficient.
	for k := 1; k < 64; {
		rs, err := r.decodeHuff(ac)
		if err != nil {
			return err
		}
		run, size := int(rs>>4), int(rs&0x0F)
		if size == 0 {
			if run != 15 {
				break // end of block
			}
			k += 16 // a run of sixteen zeros
			continue
		}
		k += run
		if k > 63 {
			return fmt.Errorf("jpegextended: a coefficient run reaches %d, past the end of the block", k)
		}
		z := zigzag[k]
		block[z] = extend(r.readBits(size), size) * int32(q[z])
		k++
	}

	idct8x8(&block)

	// Level shift and clamp. The DCT is taken on samples centered on zero, so
	// the midpoint goes back on here.
	shift := int32(1) << (d.precision - 1)
	maxVal := int32(1)<<d.precision - 1
	stride := c.blocksPerLine * 8
	for y := 0; y < 8; y++ {
		dst := (row*8+y)*stride + col*8
		for x := 0; x < 8; x++ {
			v := block[y*8+x] + shift
			if v < 0 {
				v = 0
			} else if v > maxVal {
				v = maxVal
			}
			c.samples[dst+x] = uint16(v)
		}
	}
	return nil
}

// image assembles the component planes into the frame, upsampling any component
// stored at a lower resolution.
func (d *sequentialDecoder) image() (*losslessImage, error) {
	img := &losslessImage{width: d.width, height: d.height, precision: d.precision}
	for _, c := range d.components {
		out := &losslessComponent{
			id: c.id, h: c.h, v: c.v,
			rowWidth: d.width, rows: d.height,
			samples: make([]int, d.width*d.height),
		}
		stride := c.blocksPerLine * 8
		// Nearest-neighbor, which is what the sample-replication upsampling in
		// T.81 A.2.1 describes. Anything smoother would invent values.
		for y := 0; y < d.height; y++ {
			sy := y * c.v / d.vMax
			for x := 0; x < d.width; x++ {
				sx := x * c.h / d.hMax
				out.samples[y*d.width+x] = int(c.samples[sy*stride+sx])
			}
		}
		img.components = append(img.components, out)
	}
	return img, nil
}

// idct8x8 replaces a block of dequantized coefficients with samples.
//
// This is the separable inverse DCT of T.81 A.3.3, done in floating point on
// rows then columns. T.83 defines conformance as an accuracy bound rather than
// exact output, so this need not match any particular implementation bit for
// bit — and against libjpeg's it does not, by at most one count.
func idct8x8(block *[64]int32) {
	var tmp [64]float64

	for i := 0; i < 8; i++ {
		row := block[i*8 : i*8+8]
		// A row of nothing but its DC term is flat, which is most rows of most
		// images and worth not running the full transform for.
		if row[1] == 0 && row[2] == 0 && row[3] == 0 && row[4] == 0 &&
			row[5] == 0 && row[6] == 0 && row[7] == 0 {
			v := float64(row[0]) * idctScale[0]
			for x := 0; x < 8; x++ {
				tmp[i*8+x] = v
			}
			continue
		}
		for x := 0; x < 8; x++ {
			var sum float64
			for u := 0; u < 8; u++ {
				if row[u] == 0 {
					continue
				}
				sum += idctScale[u] * float64(row[u]) * idctCos[u][x]
			}
			tmp[i*8+x] = sum
		}
	}

	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			var sum float64
			for v := 0; v < 8; v++ {
				c := tmp[v*8+x]
				if c == 0 {
					continue
				}
				sum += idctScale[v] * c * idctCos[v][y]
			}
			// Round to nearest, halves away from zero.
			if sum >= 0 {
				block[y*8+x] = int32(sum + 0.5)
			} else {
				block[y*8+x] = -int32(-sum + 0.5)
			}
		}
	}
}

// idctScale holds C(u)/2 from T.81 A.3.3: 1/(2*sqrt(2)) at u=0 and 1/2 elsewhere.
var idctScale = func() [8]float64 {
	var s [8]float64
	s[0] = 0.35355339059327373 // 1 / (2 * sqrt(2))
	for u := 1; u < 8; u++ {
		s[u] = 0.5
	}
	return s
}()

// idctCos holds cos((2x+1)u*pi/16).
var idctCos = func() [8][8]float64 {
	var t [8][8]float64
	for u := 0; u < 8; u++ {
		for x := 0; x < 8; x++ {
			t[u][x] = cosTable[(2*x+1)*u%32]
		}
	}
	return t
}()

// cosTable holds cos(k*pi/16) for k in 0..31, so the products above are looked
// up rather than recomputed per coefficient.
var cosTable = func() [32]float64 {
	var t [32]float64
	for k := 0; k < 32; k++ {
		t[k] = math.Cos(float64(k) * math.Pi / 16)
	}
	return t
}()

// ceilDiv divides rounding up, for sizes that must cover a partial unit.
func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

// sequentialJPEGPrecision returns the sample precision of a sequential DCT
// frame, or 0 if the stream is not one. It reads only the frame header, so a
// caller can route a stream before committing to decoding it.
func sequentialJPEGPrecision(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return 0
	}
	for i := 2; i+9 < len(data); {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		switch marker {
		case markerSOF0, markerSOF1:
			return int(data[i+4])
		case markerSOS, markerEOI:
			return 0
		}
		if marker >= 0xC2 && marker <= 0xCF && marker != markerDHT {
			return 0 // some other frame type: progressive, lossless, arithmetic
		}
		// Skip the segment rather than scanning through it, so a byte pair
		// inside a payload cannot be mistaken for a frame header.
		if marker == 0xFF || marker == markerSOI || marker == markerTEM ||
			(marker >= markerRST0 && marker <= markerRST7) {
			i += 2
			continue
		}
		length := int(data[i+2])<<8 | int(data[i+3])
		if length < 2 {
			return 0
		}
		i += 2 + length
	}
	return 0
}
