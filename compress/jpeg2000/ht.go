package jpeg2000

import (
	"fmt"
	"math/bits"
)

// The High-Throughput block decoder, ITU-T T.814 | ISO/IEC 15444-15 clause 7.
//
// HTJ2K keeps everything of Part 1 around the code-blocks — the codestream,
// tiles, precincts, packets, the wavelet and quantization — and replaces the
// block coder. Part 1 codes a block a bit plane at a time with an adaptive
// arithmetic coder; the HT coder codes all of a block's bit planes down to one
// plane in a single cleanup pass, and at most one plane below it in a SigProp
// and a MagRef pass. The cleanup pass walks the block in 2x2 quads and reads
// three byte-streams at once: the VLC stream, read backwards from the end of
// the segment, says which samples of a quad are significant and how many bits
// their magnitudes take; the MEL stream, a run-length code, does the same for
// the quads whose neighborhood is entirely insignificant; and the MagSgn
// stream holds the magnitude and sign bits themselves.
//
// What comes out is what Part 1's decoder produces, the magnitude bits of each
// coefficient down to some plane and a sign (7.6), and from there the rest of
// this package does what it does for any code-block.

// BlockStyleHT is bit 6 of the code-block style (Table A.3 of T.814): every
// code-block of the tile-component is an HT code-block. Only the vertically
// causal bit keeps a meaning beside it; the other Part 1 bits are reserved.
const BlockStyleHT = 0x40

// blockStyleHTMixed is bit 7 beside bit 6 (Table A.4): HT and Part 1 code-blocks
// mixed within one tile-component, told apart only by trying the one and
// falling back to the other. It is refused by name; see parseCodingParameters.
const blockStyleHTMixed = 0x80

// melExponents is MEL_E, Table 2 of T.814.
var melExponents = [13]int{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 4, 5}

// vlcDecoded is what a CxtVLC codeword gives (7.3.5), with the length of the
// codeword that gave it.
type vlcDecoded struct {
	rho, uOff, ek, e1 uint8
	length            uint8 // 0 marks a slot no codeword fills
}

// cxtVLCLookup is the Annex C tables turned around for decoding: indexed by
// the table, the context and the next seven bits of the VLC bit-stream (the
// longest codeword), first bit lowest, it gives the entry whose codeword those
// bits begin with. Every slot is filled, since each context's codewords are a
// complete prefix code; TestCxtVLCTablesAreCompletePrefixCodes checks that.
var cxtVLCLookup = func() *[2][8][128]vlcDecoded {
	var lookup [2][8][128]vlcDecoded
	for table, entries := range [2][]cxtVLCEntry{cxtVLCTable0[:], cxtVLCTable1[:]} {
		for _, e := range entries {
			for next := 0; next < 128; next++ {
				if next&(1<<e.length-1) == int(e.codeword) {
					lookup[table][e.context][next] = vlcDecoded{
						rho: e.rho, uOff: e.uOff, ek: e.ek, e1: e.e1, length: e.length}
				}
			}
		}
	}
	return &lookup
}()

// htSegments is an HT code-block's coded bytes as the decoder takes them:
// one HT cleanup segment and, if Z_blk is more than one, the HT refinement
// segment after it (7.1.1, B.3).
type htSegments struct {
	cleanup    []byte
	refinement []byte
	passes     int // Z_blk: 1 cleanup, 2 with SigProp, 3 with MagRef
}

// htSegmentsOf divides an HT code-block's data into its segments (Annex B).
//
// With no placeholder passes the segments alternate: a cleanup pass alone,
// then a SigProp and a MagRef pass together, which is the set T of B.2 for
// P0 = 0. A block of more than three passes holds either placeholder passes
// or more than one HT set. Neither is decoded here: no codestream available to
// check against uses them, and OpenJPEG refuses both, so each is refused by
// name rather than guessed at.
func htSegmentsOf(block *CodeBlockData) (htSegments, error) {
	if block.Passes > 3 {
		return htSegments{}, &ErrUnsupported{Feature: fmt.Sprintf(
			"an HT code-block of %d coding passes (placeholder passes or more than one HT set)", block.Passes)}
	}
	segments := block.Segments
	if len(segments) == 0 {
		segments = []CodewordSegment{{Passes: block.Passes, Length: len(block.Data)}}
	}
	if segments[0].Passes != 1 || len(segments) > 2 {
		return htSegments{}, errorf("an HT code-block whose %d coding passes are not one cleanup segment and one refinement segment",
			block.Passes)
	}
	var out htSegments
	offset := 0
	for i, s := range segments {
		if s.Length < 0 || offset+s.Length > len(block.Data) {
			return htSegments{}, errorf("HT segment %d claims %d bytes, %d remain", i, s.Length, len(block.Data)-offset)
		}
		data := block.Data[offset : offset+s.Length]
		offset += s.Length
		if i == 0 {
			out.cleanup, out.passes = data, 1
			continue
		}
		// B.3: Z_blk counts the refinement passes only when their segment has
		// bytes; an empty one is as if they were not there.
		if len(data) > 0 {
			out.refinement, out.passes = data, 1+s.Passes
		}
	}
	// 7.1.1: the cleanup segment is 2 to 65534 bytes, the refinement segment
	// under 2047. B.3 requires the first cleanup segment to have more than one.
	if len(out.cleanup) < 2 || len(out.cleanup) >= 65535 {
		return htSegments{}, errorf("an HT cleanup segment of %d bytes", len(out.cleanup))
	}
	if len(out.refinement) >= 2047 {
		return htSegments{}, errorf("an HT refinement segment of %d bytes", len(out.refinement))
	}
	return out, nil
}

// decodeHTBlock decodes one HT code-block into signed coefficients in units of
// half a coefficient, row by row, which is what decodeBlock returns for a
// Part 1 block; magnitudeBits is the band's Mb.
func decodeHTBlock(block *CodeBlockData, magnitudeBits int) ([]int32, error) {
	w, h := block.Block.Width(), block.Block.Height()
	if w <= 0 || h <= 0 {
		return nil, errorf("a code-block of %dx%d samples", w, h)
	}
	if w > 1<<12 || h > 1<<12 || w*h > 1<<22 {
		return nil, errorf("a code-block of %dx%d samples is larger than any legal one", w, h)
	}
	out := make([]int32, w*h)
	if block.Passes == 0 || len(block.Data) == 0 {
		return out, nil // Z_blk = 0: every sample is 0 (7.1.1)
	}
	segments, err := htSegmentsOf(block)
	if err != nil {
		return nil, err
	}

	// 7.6: the cleanup pass gives S_blk + 1 magnitude bits, the most significant
	// of the band's Mb first, so its last bit sits at plane Mb - 1 - S_blk. The
	// refinement passes add the plane below it.
	skipped := block.ZeroBitPlanes
	plane := magnitudeBits - 1 - skipped
	if plane < 0 {
		return nil, errorf("an HT code-block skips %d of its band's %d bit planes", skipped, magnitudeBits)
	}
	// One less than an int32 holds, as for a Part 1 block: the values are in
	// halves, which costs a bit.
	if magnitudeBits > 30 {
		return nil, errorf("an HT code-block in a band of %d bit planes, past what an int32 holds", magnitudeBits)
	}
	if segments.passes > 1 && plane == 0 {
		return nil, errorf("an HT code-block refines the plane below plane 0")
	}

	d := newHTDecoder(w, h, skipped, segments)
	if err := d.cleanup(); err != nil {
		return nil, err
	}
	if segments.passes > 1 {
		if err := d.sigProp(block.Style&BlockStyleVertical != 0); err != nil {
			return nil, err
		}
	}
	if segments.passes > 2 {
		d.magRef()
	}
	d.output(out, plane)
	return out, nil
}

// htDecoder is one HT code-block being decoded. The sample arrays are in
// raster order over the block padded to whole quads (7.2), stride qw*2.
type htDecoder struct {
	w, h   int
	qw, qh int
	stride int
	// skipped is S_blk; a cleanup magnitude is below 2^(skipped+1).
	skipped int
	segs    htSegments

	sig      []uint8  // sigma_n from the cleanup pass
	mu       []uint32 // mu_n
	negative []uint8  // s_n
	exponent []uint8  // E_n, for the predictors of the row of quads below
	// refined is z_n and bit is r_n, from SigProp and MagRef (7.4, 7.5).
	refined []uint8
	bit     []uint8

	vlc    vlcReader
	mel    melDecoder
	magSgn forwardReader
}

func newHTDecoder(w, h, skipped int, segs htSegments) *htDecoder {
	qw, qh := (w+1)/2, (h+1)/2
	n := 4 * qw * qh
	return &htDecoder{
		w: w, h: h, qw: qw, qh: qh, stride: 2 * qw,
		skipped:  skipped,
		segs:     segs,
		sig:      make([]uint8, n),
		mu:       make([]uint32, n),
		negative: make([]uint8, n),
		exponent: make([]uint8, n),
	}
}

// at is the index of sample (x, y) in the padded arrays.
func (d *htDecoder) at(x, y int) int { return y*d.stride + x }

// quadSample is the position of sample j of a quad (7.2): top-left,
// bottom-left, top-right, bottom-right.
var quadSample = [4][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}}

// quadState is what the VLC stream says about one quad (7.3.2).
type quadState struct {
	rho, uOff, ek, e1 uint8
	u                 int // the unsigned residual u_q
	bound             int // U_q
}

// cleanup is the HT cleanup pass (7.3): a row of quads at a time, the VLC and
// MEL decoding of every quad-pair, then the MagSgn values of the row, which the
// next row's exponent predictors need.
func (d *htDecoder) cleanup() error {
	data := d.segs.cleanup
	lcup := len(data)
	// 7.1.1: the suffix length is in the last two bytes, which are then read as
	// if their twelve bits that held it were ones (modDcup).
	scup := 16*int(data[lcup-1]) + int(data[lcup-2]&0x0F)
	if scup < 2 || scup > lcup || scup > 4079 {
		return errorf("an HT cleanup segment of %d bytes with a suffix of %d", lcup, scup)
	}
	pcup := lcup - scup
	modified := make([]byte, lcup)
	copy(modified, data)
	modified[lcup-1] = 0xFF
	modified[lcup-2] |= 0x0F

	d.magSgn = forwardReader{data: modified[:pcup], pad: 0xFF}
	d.mel = melDecoder{data: modified, pos: pcup}
	d.vlc = newVLCReader(modified, pcup)

	quads := make([]quadState, d.qw)
	for qy := 0; qy < d.qh; qy++ {
		first := qy == 0
		for qx := 0; qx < d.qw; qx += 2 {
			pair := 1
			if qx+1 < d.qw {
				pair = 2
			}
			// The CxtVLC codeword of each quad, the second's context taking in
			// what the first just said (7.3.4, 7.3.5).
			for k := 0; k < pair; k++ {
				q := &quads[qx+k]
				*q = quadState{}
				if err := d.decodeSigEMB(q, qx+k, qy); err != nil {
					return err
				}
			}
			if err := d.decodeResiduals(quads[qx:qx+pair], first); err != nil {
				return err
			}
		}
		for qx := 0; qx < d.qw; qx++ {
			if err := d.decodeMagSgn(&quads[qx], qx, qy); err != nil {
				return err
			}
		}
	}
	return nil
}

// decodeSigEMB is the procedure of that name (7.3.5): an all-zero-context quad
// is first asked of the MEL stream whether it has anything at all, and a quad
// that does reads its CxtVLC codeword.
func (d *htDecoder) decodeSigEMB(q *quadState, qx, qy int) error {
	context := d.context(qx, qy)
	if context == 0 {
		if d.mel.symbol() == 0 {
			return nil
		}
	}
	table := 1
	if qy == 0 {
		table = 0
	}
	entry := cxtVLCLookup[table][context][d.vlc.peek()]
	if err := d.vlc.consume(int(entry.length)); err != nil {
		return err
	}
	q.rho, q.uOff, q.ek, q.e1 = entry.rho, entry.uOff, entry.ek, entry.e1

	// 7.2: padding samples are never significant.
	for j := 0; j < 4; j++ {
		if q.rho&(1<<j) == 0 {
			continue
		}
		x, y := 2*qx+quadSample[j][0], 2*qy+quadSample[j][1]
		if x >= d.w || y >= d.h {
			return errorf("an HT quad makes a sample outside its code-block significant")
		}
		d.sig[d.at(x, y)] = 1
	}
	return nil
}

// context is c_q (7.3.5, Formulas 1 and 2): which of the quad's already
// decoded neighbors are significant.
func (d *htDecoder) context(qx, qy int) int {
	x, y := 2*qx, 2*qy
	s := func(x, y int) int {
		if x < 0 || y < 0 || x >= d.stride {
			return 0
		}
		return int(d.sig[d.at(x, y)])
	}
	if qy == 0 {
		// The quad to the left: f and sf its left column, w and sw its right.
		f, sf, w, sw := s(x-2, y), s(x-2, y+1), s(x-1, y), s(x-1, y+1)
		return (f | sf) + 2*w + 4*sw
	}
	// The bottom row of the quads above, and the quad to the left.
	nw, n, ne := s(x-1, y-1), s(x, y-1), s(x+1, y-1)
	nf := 0
	if qx+1 < d.qw {
		nf = s(x+2, y-1)
	}
	w, sw := s(x-1, y), s(x-1, y+1)
	return (nw | n) + 2*(w|sw) + 4*(ne|nf)
}

// decodeResiduals reads the unsigned residuals u_q of a quad-pair (7.3.6) and
// sets each quad's exponent bound U_q (7.3.7).
//
// The bits interleave across the pair: both prefixes, then both suffixes,
// then both extensions. In the first row of quads a pair whose two quads both
// have a residual takes a MEL symbol first, which says whether both residuals
// are above 2, and if not, a second residual after a long first one is a single
// bit.
func (d *htDecoder) decodeResiduals(pair []quadState, first bool) error {
	var prefix, suffix, extension [2]int
	both := len(pair) == 2 && pair[0].uOff == 1 && pair[1].uOff == 1
	bias := 0
	shortSecond := false
	if first && both {
		if d.mel.symbol() == 1 {
			bias = 2 // Formula 4
		}
	}
	for k := range pair {
		if pair[k].uOff == 0 {
			continue
		}
		if k == 1 && first && both && bias == 0 && prefix[0] > 2 {
			bit, err := d.vlc.bit()
			if err != nil {
				return err
			}
			prefix[1] = bit + 1
			shortSecond = true
			continue
		}
		p, err := d.uPrefix()
		if err != nil {
			return err
		}
		prefix[k] = p
	}
	for k := range pair {
		if pair[k].uOff == 0 || (k == 1 && shortSecond) {
			continue
		}
		s, err := d.uSuffix(prefix[k])
		if err != nil {
			return err
		}
		suffix[k] = s
	}
	for k := range pair {
		if pair[k].uOff == 0 || (k == 1 && shortSecond) {
			continue
		}
		if suffix[k] >= 28 {
			e, err := d.vlc.bits(4)
			if err != nil {
				return err
			}
			extension[k] = e
		}
	}
	for k := range pair {
		if pair[k].uOff == 1 {
			pair[k].u = bias + prefix[k] + suffix[k] + 4*extension[k]
			if k == 1 && shortSecond {
				pair[k].u = prefix[1]
			}
		}
	}
	return nil
}

// uPrefix is decodeUPrefix (7.3.6).
func (d *htDecoder) uPrefix() (int, error) {
	for _, value := range [2]int{1, 2} {
		bit, err := d.vlc.bit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			return value, nil
		}
	}
	bit, err := d.vlc.bit()
	if err != nil {
		return 0, err
	}
	if bit == 1 {
		return 3, nil
	}
	return 5, nil
}

// uSuffix is decodeUSuffix (7.3.6): none for a prefix below 3, one bit for 3,
// five for 5.
func (d *htDecoder) uSuffix(prefix int) (int, error) {
	switch {
	case prefix < 3:
		return 0, nil
	case prefix == 3:
		return d.vlc.bit()
	default:
		return d.vlc.bits(5)
	}
}

// decodeMagSgn finishes one quad: its exponent bound from the predictor and
// the residual (7.3.7), then each significant sample's MagSgn value (7.3.8).
func (d *htDecoder) decodeMagSgn(q *quadState, qx, qy int) error {
	if q.rho == 0 {
		return nil
	}
	kappa := 1
	if qy > 0 {
		// Formula 6: the predictor counts only for a quad of more than one
		// significant sample.
		if bits.OnesCount8(q.rho) > 1 {
			x, y := 2*qx, 2*qy-1
			e := int(d.exponent[d.at(x, y)])
			e = max(e, int(d.exponent[d.at(x+1, y)]))
			if qx > 0 {
				e = max(e, int(d.exponent[d.at(x-1, y)]))
			}
			if qx+1 < d.qw {
				e = max(e, int(d.exponent[d.at(x+2, y)]))
			}
			kappa = max(1, e-1)
		}
	}
	q.bound = kappa + q.u
	// The cleanup magnitudes are below 2^(S_blk+1) (7.6), so no exponent bound
	// above S_blk+2 is honest; it also keeps every shift below in range.
	if q.bound > d.skipped+2 || q.bound > 32 {
		return errorf("an HT quad's exponent bound %d is past the %d bit planes its block codes",
			q.bound, d.skipped+1)
	}
	for j := 0; j < 4; j++ {
		if q.rho&(1<<j) == 0 {
			continue
		}
		k := int(q.ek>>j) & 1
		i := uint64(q.e1>>j) & 1
		m := q.bound - k
		raw, err := d.magSgn.bits(m)
		if err != nil {
			return err
		}
		v := raw | i<<uint(m)
		mu := v>>1 + 1
		if mu >= 1<<uint(d.skipped+1) {
			return errorf("an HT sample magnitude of %d in a block coding %d bit planes", mu, d.skipped+1)
		}
		x, y := 2*qx+quadSample[j][0], 2*qy+quadSample[j][1]
		n := d.at(x, y)
		d.mu[n] = uint32(mu)
		d.negative[n] = uint8(v & 1)
		// E_n (7.3.2, Table 1): the bits of 2*mu - 1.
		d.exponent[n] = uint8(bits.Len64(2*mu - 1))
	}
	return nil
}

// sigProp is the HT SigProp pass (7.4): in stripes of four rows, column-groups
// of four columns, a bit for each insignificant sample with a significant
// neighbor, and then a sign for each that the bit made significant.
//
// The neighbors are the cleanup pass's significant samples around the sample,
// and those the pass itself has made significant earlier in its scan. With the
// vertically causal style, a sample on the last row of a stripe does not look
// at the row below it.
func (d *htDecoder) sigProp(vertical bool) error {
	n := len(d.sig)
	d.refined = make([]uint8, n)
	d.bit = make([]uint8, n)
	sp := forwardReader{data: d.segs.refinement}

	sig := func(x, y int) uint8 {
		if x < 0 || y < 0 || x >= d.w || y >= d.h {
			return 0
		}
		return d.sig[d.at(x, y)]
	}
	newly := func(x, y int) uint8 {
		if x < 0 || y < 0 || x >= d.w || y >= d.h {
			return 0
		}
		return d.bit[d.at(x, y)]
	}

	for y0 := 0; y0 < d.h; y0 += 4 {
		y1 := min(y0+4, d.h)
		for x0 := 0; x0 < d.w; x0 += 4 {
			x1 := min(x0+4, d.w)
			for x := x0; x < x1; x++ {
				for y := y0; y < y1; y++ {
					i := d.at(x, y)
					if d.sig[i] != 0 {
						continue
					}
					lastRow := y == y0+3
					below := !vertical || !lastRow
					// The propagation neighborhood, cleanup significance.
					mbr := sig(x-1, y-1) | sig(x, y-1) | sig(x+1, y-1) | sig(x-1, y) | sig(x+1, y)
					if below {
						mbr |= sig(x-1, y+1) | sig(x, y+1) | sig(x+1, y+1)
					}
					// The scan-causal part of it, made significant by this pass:
					// the column to the left within the stripe, the sample
					// above, and the row above the stripe.
					mbr |= newly(x-1, y) | newly(x, y-1) | newly(x-1, y-1)
					if !lastRow {
						mbr |= newly(x-1, y+1)
					}
					if y == y0 {
						mbr |= newly(x+1, y-1)
					}
					if mbr == 0 {
						continue
					}
					b, err := sp.bits(1)
					if err != nil {
						return err
					}
					d.refined[i] = 1
					d.bit[i] = uint8(b)
				}
			}
			for x := x0; x < x1; x++ {
				for y := y0; y < y1; y++ {
					i := d.at(x, y)
					if d.sig[i] != 0 || d.bit[i] == 0 {
						continue
					}
					b, err := sp.bits(1)
					if err != nil {
						return err
					}
					d.negative[i] = uint8(b)
				}
			}
		}
	}
	return nil
}

// magRef is the HT MagRef pass (7.5): one more bit of every sample the cleanup
// pass made significant, in the same stripe scan.
func (d *htDecoder) magRef() {
	mr := backwardReader{data: d.segs.refinement, pos: len(d.segs.refinement) - 1, last: 0xFF}
	for y0 := 0; y0 < d.h; y0 += 4 {
		y1 := min(y0+4, d.h)
		for x := 0; x < d.w; x++ {
			for y := y0; y < y1; y++ {
				i := d.at(x, y)
				if d.sig[i] == 0 {
					continue
				}
				d.refined[i] = 1
				d.bit[i] = uint8(mr.bit())
			}
		}
	}
}

// output writes the coefficients (7.6) in units of half a coefficient, as
// decodeBlock does for a Part 1 block: each nonzero magnitude at the middle of
// the interval its last decoded plane leaves open, which a reversible decode
// then drops when it halves the value.
func (d *htDecoder) output(out []int32, plane int) {
	for y := 0; y < d.h; y++ {
		for x := 0; x < d.w; x++ {
			i := d.at(x, y)
			magnitude, low := int64(d.mu[i]), plane
			if d.refined != nil && d.refined[i] != 0 {
				magnitude, low = 2*magnitude+int64(d.bit[i]), plane-1
			}
			if magnitude == 0 {
				continue
			}
			v := int32((2*magnitude + 1) << uint(low))
			if d.negative[i] != 0 {
				v = -v
			}
			out[y*d.w+x] = v
		}
	}
}

// forwardReader is the bit-stream of the MagSgn (7.1.2) and SigProp (7.1.5)
// byte-streams: bytes read forwards, bits least significant first, and a byte
// after an 0xFF giving only its seven low bits, its top bit being a stuffed 0.
// Past the end it reads the pad byte, which the MagSgn stream has once, as
// 0xFF, and the SigProp stream as often as asked, as 0.
type forwardReader struct {
	data []byte
	pos  int
	pad  byte

	acc     uint64 // bits not yet read, the next lowest
	count   int
	last    byte
	padUsed bool
}

// bits reads n bits, n up to 32, the first lowest.
func (r *forwardReader) bits(n int) (uint64, error) {
	for r.count < n {
		width := 8
		if r.last == 0xFF {
			width = 7
		}
		var b byte
		switch {
		case r.pos < len(r.data):
			b = r.data[r.pos]
			r.pos++
			// The bit a byte after 0xFF gives up must be the 0 the encoder
			// stuffed there (7.1.2, 7.1.5).
			if width == 7 && b&0x80 != 0 {
				return 0, errorf("an HT byte after 0xFF has its stuffed bit set")
			}
		case r.pad == 0xFF && r.padUsed:
			// 7.1.2: one byte past the MagSgn byte-stream is synthesized and
			// no more; a conforming stream never needs a second.
			return 0, errorf("an HT MagSgn stream runs past its bytes")
		default:
			b = r.pad
			r.padUsed = true
		}
		r.last = b
		r.acc |= uint64(b) & (1<<uint(width) - 1) << uint(r.count)
		r.count += width
	}
	v := r.acc & (1<<uint(n) - 1)
	r.acc >>= uint(n)
	r.count -= n
	return v, nil
}

// melDecoder is the MEL bit-stream (7.1.3) and its symbol decoder (7.3.3):
// bytes forwards from Pcup, bits most significant first, seven after an 0xFF,
// and 0xFF bytes past the end of the segment.
type melDecoder struct {
	data []byte
	pos  int

	tmp      byte
	bitsLeft int
	k, run   int
	one      bool
}

func (m *melDecoder) importBit() int {
	if m.bitsLeft == 0 {
		m.bitsLeft = 8
		if m.tmp == 0xFF {
			m.bitsLeft = 7
		}
		if m.pos < len(m.data) {
			m.tmp = m.data[m.pos]
			m.pos++
		} else {
			m.tmp = 0xFF
		}
	}
	m.bitsLeft--
	return int(m.tmp>>uint(m.bitsLeft)) & 1
}

// symbol is decodeMELSym (7.3.3): a run of zeros, adapting its length to the
// runs seen so far, then a one.
func (m *melDecoder) symbol() int {
	if m.run == 0 && !m.one {
		e := melExponents[m.k]
		if m.importBit() == 1 {
			m.run = 1 << uint(e)
			m.k = min(12, m.k+1)
		} else {
			m.run = 0
			for ; e > 0; e-- {
				m.run = 2*m.run + m.importBit()
			}
			m.k = max(0, m.k-1)
			m.one = true
		}
	}
	if m.run > 0 {
		m.run--
		return 0
	}
	m.one = false
	return 1
}

// vlcReader is the VLC bit-stream (7.1.4): bytes backwards from the end of the
// cleanup segment, bits least significant first, starting with the top four
// bits of the second last byte (the rest held Scup), and a byte whose seven
// low bits are all ones after a byte above 0x8F giving only those seven.
//
// It buffers ahead so that a codeword can be looked up from the next seven
// bits at once; what it may not do is consume a bit from before Pcup.
type vlcReader struct {
	data  []byte
	pos   int // the next byte to read, going down
	floor int // Pcup
	last  byte

	acc   uint64
	count int
}

func newVLCReader(modified []byte, pcup int) vlcReader {
	lcup := len(modified)
	last := modified[lcup-2]
	tmp := last >> 4
	width := 4
	if tmp&7 == 7 {
		width = 3
	}
	return vlcReader{
		data: modified, pos: lcup - 3, floor: pcup, last: last,
		acc: uint64(tmp) & (1<<uint(width) - 1), count: width,
	}
}

// fill reads bytes until at least n bits are buffered or the stream ends.
func (r *vlcReader) fill(n int) {
	for r.count < n && r.pos >= r.floor {
		b := r.data[r.pos]
		r.pos--
		width := 8
		if r.last > 0x8F && b&0x7F == 0x7F {
			width = 7
		}
		r.last = b
		r.acc |= uint64(b) & (1<<uint(width) - 1) << uint(r.count)
		r.count += width
	}
}

// peek returns the next seven bits, zeros where the stream has ended; a
// codeword that needs those is caught by consume.
func (r *vlcReader) peek() int {
	r.fill(7)
	return int(r.acc & 0x7F)
}

func (r *vlcReader) consume(n int) error {
	r.fill(n)
	if r.count < n {
		return errorf("an HT VLC stream runs past the start of its bytes")
	}
	r.acc >>= uint(n)
	r.count -= n
	return nil
}

func (r *vlcReader) bit() (int, error) {
	v, err := r.bits(1)
	return v, err
}

// bits reads n bits, n up to 8, the first lowest.
func (r *vlcReader) bits(n int) (int, error) {
	r.fill(n)
	if r.count < n {
		return 0, errorf("an HT VLC stream runs past the start of its bytes")
	}
	v := int(r.acc & (1<<uint(n) - 1))
	r.acc >>= uint(n)
	r.count -= n
	return v, nil
}

// backwardReader is the MagRef bit-stream (7.1.6): the VLC stream's rules,
// read from the end of the refinement segment, and 0 bytes before its start.
type backwardReader struct {
	data []byte
	pos  int
	last byte

	tmp      byte
	bitsLeft int
}

func (r *backwardReader) bit() int {
	if r.bitsLeft == 0 {
		if r.pos >= 0 {
			r.tmp = r.data[r.pos]
			r.pos--
		} else {
			r.tmp = 0
		}
		r.bitsLeft = 8
		if r.last > 0x8F && r.tmp&0x7F == 0x7F {
			r.bitsLeft = 7
		}
		r.last = r.tmp
	}
	b := int(r.tmp & 1)
	r.tmp >>= 1
	r.bitsLeft--
	return b
}
