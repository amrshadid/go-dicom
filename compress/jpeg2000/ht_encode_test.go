package jpeg2000

import (
	"encoding/binary"
	"math/bits"
	"testing"
)

// A High-Throughput block encoder and codestream writer, for the tests only.
//
// Encoding HTJ2K is not something this package offers. The tests need HT
// codestreams that use what the third-party files do not — refinement passes,
// several layers, tiles, precincts, every progression order, 16-bit and
// signed samples, the vertically causal style, HT and Part 1 tile-components
// side by side — and nothing on hand writes them: OpenJPEG 2.5.4 decodes HT
// and does not encode it. So the tests write their own, following the
// encoding procedures of T.814 Annex F, and OpenJPEG's decoder is the
// reference both this encoder and the package's decoder answer to. An encoder
// and a decoder written from the same misreading would agree with each other
// and not with OpenJPEG.
//
// The codestreams are transcoded, as T.814 8.8 describes, from Part 1
// codestreams that opj_compress writes: the quantized coefficients of every
// code-block are read back with the Part 1 decoder, coded again as HT
// code-blocks, and written in packets of the same tiles, precincts and
// progression.

// htTestBlock is one code-block coded by the test encoder.
type htTestBlock struct {
	cleanup, refinement []byte
	refinementSplit     int // bytes of the refinement segment in the SigProp pass's layer
	passes              int // 1 to 3
	skipped             int // S_blk, written as the zero bit planes
}

// htRefinement says how many passes the test encoder writes: the cleanup pass
// alone at plane 0, or the cleanup at plane 1 with a SigProp pass, or with a
// SigProp and a MagRef pass, which together reach plane 0 again.
type htRefinement int

const (
	htCleanupOnly htRefinement = iota
	htSigProp
	htSigPropMagRef
)

// encodeHTTestBlock codes a block's quantized coefficients, raster order w by
// h, in a band of mb magnitude bit planes. It returns passes 0 for a block of
// zeros, which a packet then leaves out.
func encodeHTTestBlock(t testing.TB, q []int32, w, h, mb int, refinement htRefinement, vertical bool) htTestBlock {
	t.Helper()
	plane := 0
	if refinement != htCleanupOnly {
		plane = 1
	}
	nonzero := false
	for _, v := range q {
		if v != 0 {
			nonzero = true
		}
		if uint32(abs32(v)) >= 1<<uint(mb) {
			t.Fatalf("a coefficient of %d in a band of %d bit planes", v, mb)
		}
	}
	if !nonzero {
		return htTestBlock{}
	}
	if mb-1-plane < 0 {
		t.Fatalf("a band of %d bit planes cannot hold a cleanup pass at plane %d", mb, plane)
	}
	e := newHTTestEncoder(q, w, h, plane)
	out := htTestBlock{cleanup: e.cleanup(t), passes: 1, skipped: mb - 1 - plane}
	if refinement != htCleanupOnly {
		out.refinement, out.refinementSplit = e.refine(refinement == htSigPropMagRef, vertical)
		out.passes = 2
		if refinement == htSigPropMagRef {
			out.passes = 3
		}
	}
	return out
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// htTestEncoder holds a block padded to whole quads, raster order.
type htTestEncoder struct {
	q            []int32
	w, h, stride int
	qw, qh       int
	plane        int
	mu           []uint32
	sig          []uint8
	exp          []int
}

func newHTTestEncoder(q []int32, w, h, plane int) *htTestEncoder {
	qw, qh := (w+1)/2, (h+1)/2
	e := &htTestEncoder{q: q, w: w, h: h, stride: 2 * qw, qw: qw, qh: qh, plane: plane}
	n := 4 * qw * qh
	e.mu, e.sig, e.exp = make([]uint32, n), make([]uint8, n), make([]int, n)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			mu := uint32(abs32(q[y*w+x])) >> uint(plane)
			i := y*e.stride + x
			e.mu[i] = mu
			if mu > 0 {
				e.sig[i] = 1
				e.exp[i] = bits.Len32(2*mu - 1) // F.1
			}
		}
	}
	return e
}

func (e *htTestEncoder) negative(x, y int) bool { return e.q[y*e.w+x] < 0 }

// msPacker, melPacker and vlcPacker are emitMagSgnBits, emitMELBit and
// emitVLCBits with their termination procedures (F.4).
type msPacker struct {
	buf        []byte
	tmp        byte
	bits, max_ int
}

func (p *msPacker) emit(v uint64, n int) {
	if p.max_ == 0 {
		p.max_ = 8
	}
	for ; n > 0; n-- {
		p.tmp |= byte(v&1) << uint(p.bits)
		v >>= 1
		p.bits++
		if p.bits == p.max_ {
			p.buf = append(p.buf, p.tmp)
			p.max_ = 8
			if p.tmp == 0xFF {
				p.max_ = 7
			}
			p.tmp, p.bits = 0, 0
		}
	}
}

func (p *msPacker) terminate() {
	if p.max_ == 0 {
		p.max_ = 8
	}
	if p.bits > 0 {
		for p.bits < p.max_ {
			p.tmp |= 1 << uint(p.bits)
			p.bits++
		}
		if p.tmp != 0xFF {
			p.buf = append(p.buf, p.tmp)
		}
	} else if p.max_ == 7 {
		p.buf = p.buf[:len(p.buf)-1] // the decoder synthesizes a trailing FF
	}
}

type melPacker struct {
	buf []byte
	tmp byte
	rem int
	// the MEL encoder state (F.3)
	k, run, threshold int
}

func newMELPacker() *melPacker {
	return &melPacker{rem: 8, threshold: 1 << uint(melExponents[0])}
}

func (p *melPacker) emitBit(bit int) {
	p.tmp = 2*p.tmp + byte(bit)
	p.rem--
	if p.rem == 0 {
		p.buf = append(p.buf, p.tmp)
		p.rem = 8
		if p.tmp == 0xFF {
			p.rem = 7
		}
		p.tmp = 0
	}
}

func (p *melPacker) encode(symbol int) {
	if symbol == 0 {
		p.run++
		if p.run >= p.threshold {
			p.emitBit(1)
			p.run = 0
			p.k = min(12, p.k+1)
			p.threshold = 1 << uint(melExponents[p.k])
		}
		return
	}
	p.emitBit(0)
	for e := melExponents[p.k]; e > 0; e-- {
		p.emitBit((p.run >> uint(e-1)) & 1)
	}
	p.run = 0
	p.k = max(0, p.k-1)
	p.threshold = 1 << uint(melExponents[p.k])
}

type vlcPacker struct {
	buf  []byte
	tmp  byte
	bits int
	last byte
}

func newVLCPacker() *vlcPacker {
	return &vlcPacker{buf: []byte{0xFF}, tmp: 15, bits: 4, last: 0xFF}
}

func (p *vlcPacker) emit(cwd uint32, n int) {
	for ; n > 0; n-- {
		p.tmp |= byte(cwd&1) << uint(p.bits)
		cwd >>= 1
		p.bits++
		if p.last > 0x8F && p.tmp == 0x7F {
			p.bits++
		}
		if p.bits == 8 {
			p.buf = append(p.buf, p.tmp)
			p.last = p.tmp
			p.tmp, p.bits = 0, 0
		}
	}
}

// terminateMELAndVLC is termMEL and termMELandVLCPackers (F.3, F.4).
func terminateMELAndVLC(mel *melPacker, vlc *vlcPacker) {
	if mel.run > 0 {
		mel.emitBit(1)
	}
	melTmp := mel.tmp << uint(mel.rem)
	melMask := byte(0xFF << uint(mel.rem))
	vlcMask := byte(0xFF >> uint(8-vlc.bits))
	if mel.rem == 8 {
		melMask = 0
	}
	if vlc.bits == 0 {
		vlcMask = 0
	}
	if melMask|vlcMask == 0 {
		return
	}
	fuse := melTmp | vlc.tmp
	if ((fuse^melTmp)&melMask)|((fuse^vlc.tmp)&vlcMask) == 0 && fuse != 0xFF {
		mel.buf = append(mel.buf, fuse)
	} else {
		mel.buf = append(mel.buf, melTmp)
		vlc.buf = append(vlc.buf, vlc.tmp)
	}
}

// encodeUVLC is encodeUVLC (F.3, Table 3): the prefix as bits in the order
// they are read, the suffix and the extension with their widths.
func encodeUVLC(vlcPrefix func(int), u int) (suffix, suffixBits, ext, extBits int) {
	switch {
	case u == 1:
		vlcPrefix(1)
	case u == 2:
		vlcPrefix(2)
	case u <= 4:
		vlcPrefix(3)
		return u - 3, 1, 0, 0
	case u <= 32:
		vlcPrefix(5)
		return u - 5, 5, 0, 0
	default:
		vlcPrefix(5)
		return 28 + (u-33)%4, 5, (u - 33) / 4, 4
	}
	return 0, 0, 0, 0
}

// prefixBits is the U-VLC prefix of Table 3 as (codeword, length), first bit
// lowest: "1", "01", "001", "000".
func prefixBits(prefix int) (uint32, int) {
	switch prefix {
	case 1:
		return 1, 1
	case 2:
		return 2, 2
	case 3:
		return 4, 3
	default:
		return 0, 3
	}
}

// vlcEncoding finds the CxtVLC codeword for a quad (F.3): the entry for its
// context, significance and full EMB pattern whose known-bit pattern has the
// most bits set.
func vlcEncoding(t testing.TB, first bool, context int, rho, emb uint8) cxtVLCEntry {
	t.Helper()
	table := cxtVLCTable1[:]
	if first {
		table = cxtVLCTable0[:]
	}
	uOff := uint8(0)
	if emb != 0 {
		uOff = 1
	}
	best, found := cxtVLCEntry{}, false
	for _, e := range table {
		if int(e.context) != context || e.rho != rho || e.uOff != uOff || e.e1 != emb&e.ek {
			continue
		}
		if !found || bits.OnesCount8(e.ek) > bits.OnesCount8(best.ek) {
			best, found = e, true
		}
	}
	if !found {
		t.Fatalf("no CxtVLC codeword for context %d, rho %X, EMB %X", context, rho, emb)
	}
	return best
}

// cleanup codes the HT cleanup pass (F.3) and assembles the segment (F.4).
func (e *htTestEncoder) cleanup(t testing.TB) []byte {
	t.Helper()
	ms, mel, vlc := &msPacker{}, newMELPacker(), newVLCPacker()
	sig := func(x, y int) int {
		if x < 0 || y < 0 || x >= e.stride {
			return 0
		}
		return int(e.sig[y*e.stride+x])
	}
	type quad struct {
		rho, emb, ek uint8
		context      int
		u, bound     int
		entry        cxtVLCEntry
	}
	for qy := 0; qy < e.qh; qy++ {
		first := qy == 0
		quads := make([]quad, e.qw)
		for qx := range quads {
			q := &quads[qx]
			x, y := 2*qx, 2*qy
			emax := 0
			for j, s := range quadSample {
				i := (y+s[1])*e.stride + x + s[0]
				if e.sig[i] != 0 {
					q.rho |= 1 << uint(j)
				}
				emax = max(emax, e.exp[i])
			}
			kappa := 1
			if !first && bits.OnesCount8(q.rho) > 1 {
				ex := max(e.exp[(y-1)*e.stride+x], e.exp[(y-1)*e.stride+x+1])
				if qx > 0 {
					ex = max(ex, e.exp[(y-1)*e.stride+x-1])
				}
				if qx+1 < e.qw {
					ex = max(ex, e.exp[(y-1)*e.stride+x+2])
				}
				kappa = max(1, ex-1)
			}
			if q.rho != 0 {
				q.bound = max(emax, kappa) // F.2
				q.u = q.bound - kappa
				if q.u > 0 {
					for j, s := range quadSample {
						if e.exp[(y+s[1])*e.stride+x+s[0]] == emax {
							q.emb |= 1 << uint(j)
						}
					}
				}
			}
		}
		for qx := 0; qx < e.qw; qx += 2 {
			pair := min(2, e.qw-qx)
			for k := 0; k < pair; k++ {
				q := &quads[qx+k]
				x, y := 2*(qx+k), 2*qy
				// The context after the quad to the left is known (7.3.5).
				if first {
					q.context = (sig(x-2, y) | sig(x-2, y+1)) + 2*sig(x-1, y) + 4*sig(x-1, y+1)
				} else {
					nf := 0
					if qx+k+1 < e.qw {
						nf = sig(x+2, y-1)
					}
					q.context = (sig(x-1, y-1) | sig(x, y-1)) + 2*(sig(x-1, y)|sig(x-1, y+1)) +
						4*(sig(x+1, y-1)|nf)
				}
				if q.context == 0 {
					sym := 0
					if q.rho != 0 {
						sym = 1
					}
					mel.encode(sym)
					if sym == 0 {
						continue
					}
				}
				q.entry = vlcEncoding(t, first, q.context, q.rho, q.emb)
				q.ek = q.entry.ek
				vlc.emit(uint32(q.entry.codeword), int(q.entry.length))
			}
			// The residuals of the pair (7.3.6).
			p := quads[qx : qx+pair]
			both := pair == 2 && p[0].u > 0 && p[1].u > 0
			var suffix, suffixBits, ext, extBits [2]int
			emitPrefix := func(prefix int) {
				c, n := prefixBits(prefix)
				vlc.emit(c, n)
			}
			if first && both {
				if min(p[0].u, p[1].u) > 2 {
					mel.encode(1)
					for k := 0; k < 2; k++ {
						suffix[k], suffixBits[k], ext[k], extBits[k] = encodeUVLC(emitPrefix, p[k].u-2)
					}
				} else {
					mel.encode(0)
					suffix[0], suffixBits[0], ext[0], extBits[0] = encodeUVLC(emitPrefix, p[0].u)
					if p[0].u > 2 {
						vlc.emit(uint32(p[1].u-1), 1)
					} else {
						suffix[1], suffixBits[1], ext[1], extBits[1] = encodeUVLC(emitPrefix, p[1].u)
					}
				}
			} else {
				for k := range p {
					if p[k].u > 0 {
						suffix[k], suffixBits[k], ext[k], extBits[k] = encodeUVLC(emitPrefix, p[k].u)
					}
				}
			}
			for k := range p {
				vlc.emit(uint32(suffix[k]), suffixBits[k])
			}
			for k := range p {
				vlc.emit(uint32(ext[k]), extBits[k])
			}
		}
		// The MagSgn bits of the row (7.3.8).
		for qx := range quads {
			q := &quads[qx]
			for j, s := range quadSample {
				if q.rho&(1<<uint(j)) == 0 {
					continue
				}
				x, y := 2*qx+s[0], 2*qy+s[1]
				mu := uint64(e.mu[y*e.stride+x])
				sign := uint64(0)
				if e.negative(x, y) {
					sign = 1
				}
				v := 2*(mu-1) + sign
				m := q.bound - int(q.ek>>uint(j)&1)
				ms.emit(v, m)
			}
		}
	}
	ms.terminate()
	terminateMELAndVLC(mel, vlc)
	scup := len(mel.buf) + len(vlc.buf)
	out := append([]byte{}, ms.buf...)
	out = append(out, mel.buf...)
	for i := len(vlc.buf) - 1; i >= 0; i-- {
		out = append(out, vlc.buf[i])
	}
	if scup > 4079 || len(out) >= 65535 {
		t.Fatalf("an HT cleanup segment of %d bytes with a suffix of %d", len(out), scup)
	}
	l := len(out)
	out[l-1] = byte(scup >> 4)
	out[l-2] = out[l-2]&0xF0 | byte(scup&0x0F)
	return out
}

// spPacker and mrPacker are emitSPBit and emitMRBit (F.4).
type spPacker struct {
	buf        []byte
	tmp        byte
	bits, max_ int
}

func (p *spPacker) emit(bit int) {
	if p.max_ == 0 {
		p.max_ = 8
	}
	p.tmp |= byte(bit) << uint(p.bits)
	p.bits++
	if p.bits == p.max_ {
		p.buf = append(p.buf, p.tmp)
		p.max_ = 8
		if p.tmp == 0xFF {
			p.max_ = 7
		}
		p.tmp, p.bits = 0, 0
	}
}

type mrPacker struct {
	buf  []byte
	tmp  byte
	bits int
	last byte
}

func (p *mrPacker) emit(bit int) {
	p.tmp |= byte(bit) << uint(p.bits)
	p.bits++
	if p.last > 0x8F && p.tmp == 0x7F {
		p.bits++
	}
	if p.bits == 8 {
		p.buf = append(p.buf, p.tmp)
		p.last = p.tmp
		p.tmp, p.bits = 0, 0
	}
}

// refine codes the SigProp pass and, if asked, the MagRef pass for plane
// e.plane-1 (7.4, 7.5 run the other way), and assembles the refinement
// segment (F.4). It also says how many of its bytes the SigProp pass's
// codeword segment takes when the two passes go in different layers.
func (e *htTestEncoder) refine(magRef bool, vertical bool) ([]byte, int) {
	low := uint(e.plane - 1)
	bit := func(x, y int) int { return int(uint32(abs32(e.q[y*e.w+x]))>>low) & 1 }
	sig := func(x, y int) uint8 {
		if x < 0 || y < 0 || x >= e.w || y >= e.h {
			return 0
		}
		return e.sig[y*e.stride+x]
	}
	newly := make([]uint8, len(e.sig))
	nw := func(x, y int) uint8 {
		if x < 0 || y < 0 || x >= e.w || y >= e.h {
			return 0
		}
		return newly[y*e.stride+x]
	}
	sp := &spPacker{max_: 8}
	for y0 := 0; y0 < e.h; y0 += 4 {
		y1 := min(y0+4, e.h)
		for x0 := 0; x0 < e.w; x0 += 4 {
			x1 := min(x0+4, e.w)
			for x := x0; x < x1; x++ {
				for y := y0; y < y1; y++ {
					if sig(x, y) != 0 {
						continue
					}
					last := y == y0+3
					mbr := sig(x-1, y-1) | sig(x, y-1) | sig(x+1, y-1) | sig(x-1, y) | sig(x+1, y)
					if !vertical || !last {
						mbr |= sig(x-1, y+1) | sig(x, y+1) | sig(x+1, y+1)
					}
					mbr |= nw(x-1, y) | nw(x, y-1) | nw(x-1, y-1)
					if !last {
						mbr |= nw(x-1, y+1)
					}
					if y == y0 {
						mbr |= nw(x+1, y-1)
					}
					if mbr == 0 {
						continue
					}
					b := bit(x, y)
					sp.emit(b)
					newly[y*e.stride+x] = uint8(b)
				}
			}
			for x := x0; x < x1; x++ {
				for y := y0; y < y1; y++ {
					if newly[y*e.stride+x] != 0 {
						s := 0
						if e.negative(x, y) {
							s = 1
						}
						sp.emit(s)
					}
				}
			}
		}
	}
	if !magRef {
		// termSPPacker.
		if sp.tmp != 0 {
			sp.buf = append(sp.buf, sp.tmp)
			sp.max_ = 8
			if sp.tmp == 0xFF {
				sp.max_ = 7
			}
		}
		if sp.max_ == 7 {
			sp.buf = append(sp.buf, 0)
		}
		return sp.buf, len(sp.buf)
	}
	mr := &mrPacker{last: 0xFF}
	for y0 := 0; y0 < e.h; y0 += 4 {
		y1 := min(y0+4, e.h)
		for x := 0; x < e.w; x++ {
			for y := y0; y < y1; y++ {
				if sig(x, y) != 0 {
					mr.emit(bit(x, y))
				}
			}
		}
	}
	// termSPandMRPackers.
	spMask := byte(0xFF >> uint(8-sp.bits))
	if sp.bits == 0 {
		spMask = 0
	}
	spMask |= byte((1 << uint(sp.max_)) & 0x80)
	mrMask := byte(0xFF >> uint(8-mr.bits))
	if mr.bits == 0 {
		mrMask = 0
	}
	if spMask|mrMask != 0 {
		fuse := sp.tmp | mr.tmp
		if ((fuse^sp.tmp)&spMask)|((fuse^mr.tmp)&mrMask) == 0 {
			sp.buf = append(sp.buf, fuse)
		} else {
			sp.buf = append(sp.buf, sp.tmp)
			mr.buf = append(mr.buf, mr.tmp)
		}
	}
	out := append([]byte{}, sp.buf...)
	split := len(out)
	for i := len(mr.buf) - 1; i >= 0; i-- {
		out = append(out, mr.buf[i])
	}
	return out, split
}

// htTranscodeOptions say how a Part 1 codestream becomes an HT one.
type htTranscodeOptions struct {
	refinement htRefinement
	// passLayers puts each pass in a layer of its own: the cleanup in layer
	// 0, SigProp in 1, MagRef in 2, so that a refinement segment is two
	// codeword segments in two packets. Otherwise there is one layer.
	passLayers bool
	vertical   bool
	// partOne lists components left as Part 1 code-blocks, in a COC of
	// their own (the HTDECLARED set of T.814 8.2).
	partOne map[int]bool
}

// htTranscode writes the HT codestream of a Part 1 codestream whose
// code-blocks hold every coding pass, as opj_compress writes them without a
// rate. The tiles, precincts, progression and quantization stay as they are.
func htTranscode(t testing.TB, part1 []byte, options htTranscodeOptions) []byte {
	t.Helper()
	c, err := ParseCodestream(part1)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TileCoding)+len(c.TileComponentCoding)+len(c.TileQuant)+len(c.TileComponentQuant) > 0 {
		t.Fatal("htTranscode does not carry tile-part coding or quantization")
	}
	layers := 1
	if options.passLayers {
		layers = int(options.refinement) + 1
	}
	htStyle := byte(BlockStyleHT)
	if options.vertical {
		htStyle |= BlockStyleVertical
	}
	styleOf := func(comp int) byte {
		if options.partOne[comp] {
			return c.CodingFor(comp).BlockStyle
		}
		return htStyle
	}
	maxBits := 0

	tiles := make([][]byte, c.NumTiles())
	for tile := range tiles {
		data, err := c.ReadTilePackets(tile)
		if err != nil {
			t.Fatal(err)
		}
		// Each block's contribution by layer, keyed by where it sits.
		type key struct{ comp, res, band, x0, y0 int }
		type coded struct {
			layers  [][]CodewordSegment // per layer, the segments it carries
			data    []byte
			skipped int
			first   int // the first layer it is in
		}
		blocks := map[key]*coded{}
		for _, block := range data.Blocks {
			tc := data.Components[block.Component]
			res := tc.Resolutions[block.Resolution]
			band := bandOfType(res, block.BandType)
			style := c.CodingForTile(tile, block.Component)
			mb, err := MagnitudeBits(c.QuantForTile(tile, block.Component), band, block.Resolution, style.Levels)
			if err != nil {
				t.Fatal(err)
			}
			maxBits = max(maxBits, mb)
			k := key{block.Component, block.Resolution, block.BandType, block.Block.X0, block.Block.Y0}
			if options.partOne[block.Component] {
				if block.Style != 0 {
					t.Fatal("a Part 1 component to keep must have code-block style 0")
				}
				segs := make([][]CodewordSegment, layers)
				segs[0] = []CodewordSegment{{Passes: block.Passes, Length: len(block.Data)}}
				blocks[k] = &coded{layers: segs, data: block.Data, skipped: block.ZeroBitPlanes}
				continue
			}
			if planes := mb - block.ZeroBitPlanes; block.Passes != 3*planes-2 {
				t.Fatalf("a code-block of %d passes stops before plane 0 (%d planes): not a codestream to transcode",
					block.Passes, planes)
			}
			halves, err := DecodeBlock(block, mb)
			if err != nil {
				t.Fatal(err)
			}
			q := make([]int32, len(halves))
			for i, v := range halves {
				q[i] = v / 2
			}
			ht := encodeHTTestBlock(t, q, block.Block.Width(), block.Block.Height(), mb, options.refinement, options.vertical)
			if ht.passes == 0 {
				continue
			}
			segs := make([][]CodewordSegment, layers)
			cleanup := CodewordSegment{Passes: 1, Length: len(ht.cleanup)}
			refinement := CodewordSegment{Passes: ht.passes - 1, Length: len(ht.refinement)}
			switch {
			case ht.passes == 1:
				segs[0] = []CodewordSegment{cleanup}
			case !options.passLayers:
				segs[0] = []CodewordSegment{cleanup, refinement}
			case ht.passes == 2:
				segs[0], segs[1] = []CodewordSegment{cleanup}, []CodewordSegment{refinement}
			default:
				segs[0] = []CodewordSegment{cleanup}
				segs[1] = []CodewordSegment{{Passes: 1, Length: ht.refinementSplit}}
				segs[2] = []CodewordSegment{{Passes: 1, Length: len(ht.refinement) - ht.refinementSplit}}
			}
			blocks[k] = &coded{layers: segs, data: append(append([]byte{}, ht.cleanup...), ht.refinement...), skipped: ht.skipped}
		}

		precincts, err := c.tilePrecincts(tile, data.Components, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		type bandState struct {
			inclusion, zero *tagTreeEncoder
			blocks          []*coded
			lblock          []int
			included        []bool
			offset          []int
		}
		states := map[[4]int]*bandState{}
		var out []byte
		style := c.CodingForTile(tile, 0)
		style.Layers = layers
		err = forEachPacket(style, c.ProgressionChangesForTile(tile), precincts, 1<<30, func(step packetStep) (bool, error) {
			tc := data.Components[step.component]
			if step.resolution >= len(tc.Resolutions) {
				return true, nil
			}
			res := tc.Resolutions[step.resolution]
			header := &headerWriter{}
			var body []byte
			nonEmpty := false
			var bandsHere []*bandState
			for b, band := range res.Bands {
				sk := [4]int{step.component, step.resolution, b, step.precinct}
				st := states[sk]
				if st == nil {
					indexes, w, h := precinctBlocks(res, band, step.precinct)
					n := w * h
					st = &bandState{blocks: make([]*coded, n), lblock: make([]int, n),
						included: make([]bool, n), offset: make([]int, n)}
					firsts, zeros := make([]int32, n), make([]int32, n)
					for i := 0; i < n; i++ {
						index := i
						if indexes != nil {
							index = indexes[i]
						}
						blk := band.Blocks[index]
						cb := blocks[key{step.component, step.resolution, band.Type, blk.X0, blk.Y0}]
						st.blocks[i] = cb
						st.lblock[i] = initialLblock
						firsts[i] = int32(layers)
						if cb != nil {
							for l := range cb.layers {
								if len(cb.layers[l]) > 0 {
									firsts[i] = int32(l)
									cb.first = l
									break
								}
							}
							zeros[i] = int32(cb.skipped)
						}
					}
					st.inclusion = newTagTreeEncoder(w, h, firsts)
					st.zero = newTagTreeEncoder(w, h, zeros)
					states[sk] = st
				}
				bandsHere = append(bandsHere, st)
				for _, cb := range st.blocks {
					if cb != nil && len(cb.layers[step.layer]) > 0 {
						nonEmpty = true
					}
				}
			}
			if !nonEmpty {
				header.writeBit(0)
				out = append(out, header.bytes()...)
				return true, nil
			}
			header.writeBit(1)
			for _, st := range bandsHere {
				for i, cb := range st.blocks {
					contributes := cb != nil && len(cb.layers[step.layer]) > 0
					if !st.included[i] {
						st.inclusion.encode(header, i, int32(step.layer)+1)
						if !contributes {
							continue
						}
						st.zero.encode(header, i, int32(cb.skipped)+1)
						st.included[i] = true
					} else {
						bit := 0
						if contributes {
							bit = 1
						}
						header.writeBit(bit)
						if !contributes {
							continue
						}
					}
					segs := cb.layers[step.layer]
					passes := 0
					for _, s := range segs {
						passes += s.Passes
					}
					writePassCount(header, passes)
					lblock := st.lblock[i]
					for _, s := range segs {
						for s.Length >= 1<<uint(lblock+log2(s.Passes)) {
							lblock++
						}
					}
					for ; st.lblock[i] < lblock; st.lblock[i]++ {
						header.writeBit(1)
					}
					header.writeBit(0)
					for _, s := range segs {
						header.writeBits(uint32(s.Length), lblock+log2(s.Passes))
						body = append(body, cb.data[st.offset[i]:st.offset[i]+s.Length]...)
						st.offset[i] += s.Length
					}
				}
			}
			out = append(out, header.bytes()...)
			out = append(out, body...)
			return true, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		tiles[tile] = out
	}
	return writeHTTestCodestream(c, tiles, styleOf, layers, maxBits, len(options.partOne) > 0)
}

// bandOfType returns the band of a resolution with the given type.
func bandOfType(res Resolution, bandType int) Band {
	for _, band := range res.Bands {
		if band.Type == bandType {
			return band
		}
	}
	return Band{}
}

// writeHTTestCodestream writes the markers of an HTJ2K codestream (T.814
// Annex A) around the tiles' packets: SIZ with bit 14 of Rsiz, CAP declaring
// Part 15, COD and a COC for each component whose style differs, QCD, QCC and
// POC as the source had them, and one tile-part for each tile.
func writeHTTestCodestream(c *Codestream, tiles [][]byte, styleOf func(int) byte, layers, maxBits int, declared bool) []byte {
	be := binary.BigEndian
	var out []byte
	marker := func(m uint16) { out = be.AppendUint16(out, m) }
	segment := func(m uint16, body []byte) {
		marker(m)
		out = be.AppendUint16(out, uint16(len(body)+2))
		out = append(out, body...)
	}
	marker(markerSOC)

	siz := be.AppendUint16(nil, 0x4000)
	for _, v := range []uint32{c.Width, c.Height, c.XOffset, c.YOffset, c.TileWidth, c.TileHeight, c.TileXOffset, c.TileYOffset} {
		siz = be.AppendUint32(siz, v)
	}
	siz = be.AppendUint16(siz, uint16(len(c.Components)))
	for _, comp := range c.Components {
		ssiz := byte(comp.Depth - 1)
		if comp.Signed {
			ssiz |= 0x80
		}
		siz = append(siz, ssiz, byte(comp.DX), byte(comp.DY))
	}
	segment(markerSIZ, siz)

	// Ccap15 (T.814 Table A.2): HTONLY or HTDECLARED, irreversible if the
	// filter is, and a magnitude bound B = P + 8 that covers every band.
	ccap := uint16(0)
	if declared {
		ccap |= 0x8000
	}
	if c.Coding.Wavelet == Wavelet97Irreversible {
		ccap |= 0x0020
	}
	ccap |= uint16(min(19, max(0, maxBits-8)))
	capBody := be.AppendUint32(nil, 1<<(32-15))
	capBody = be.AppendUint16(capBody, ccap)
	segment(markerCAP, capBody)

	codingParameters := func(style CodingStyle, blockStyle byte) []byte {
		p := []byte{byte(style.Levels), byte(bits.TrailingZeros(uint(style.CodeBlockWidth)) - 2),
			byte(bits.TrailingZeros(uint(style.CodeBlockHt)) - 2), blockStyle, byte(style.Wavelet)}
		if style.UsePrecincts {
			for _, pr := range style.Precincts {
				p = append(p, byte(pr.PPy<<4|pr.PPx))
			}
		}
		return p
	}
	scod := func(style CodingStyle) byte {
		if style.UsePrecincts {
			return 1
		}
		return 0
	}
	mct := byte(0)
	if c.Coding.MCT {
		mct = 1
	}
	cod := []byte{scod(c.Coding), byte(c.Coding.Progression)}
	cod = be.AppendUint16(cod, uint16(layers))
	cod = append(cod, mct)
	cod = append(cod, codingParameters(c.Coding, styleOf(0))...)
	segment(markerCOD, cod)
	for comp := range c.Components {
		style := c.CodingFor(comp)
		_, own := c.ComponentCoding[comp]
		if comp == 0 && !own {
			continue
		}
		if !own && styleOf(comp) == styleOf(0) {
			continue
		}
		coc := []byte{byte(comp), scod(style)}
		coc = append(coc, codingParameters(style, styleOf(comp))...)
		segment(markerCOC, coc)
	}

	quant := func(q Quantization) []byte {
		body := []byte{byte(q.GuardBits<<5 | q.Style)}
		for i, exponent := range q.Exponents {
			if q.Style == QuantNone {
				body = append(body, byte(exponent<<3))
				continue
			}
			body = be.AppendUint16(body, uint16(exponent<<11|q.Mantissas[i]))
		}
		return body
	}
	segment(markerQCD, quant(c.Quant))
	for comp := range c.Components {
		if q, ok := c.ComponentQuant[comp]; ok {
			segment(markerQCC, append([]byte{byte(comp)}, quant(q)...))
		}
	}
	poc := func(changes []ProgressionChange) []byte {
		var body []byte
		for _, ch := range changes {
			body = append(body, byte(ch.ResolutionStart), byte(ch.ComponentStart))
			body = be.AppendUint16(body, uint16(min(ch.LayerEnd, layers)))
			body = append(body, byte(ch.ResolutionEnd), byte(ch.ComponentEnd%256), byte(ch.Progression))
		}
		return body
	}
	if len(c.ProgressionChanges) > 0 {
		segment(markerPOC, poc(c.ProgressionChanges))
	}

	for tile, data := range tiles {
		var header []byte
		if changes, ok := c.TileProgressionChanges[tile]; ok {
			body := poc(changes)
			header = be.AppendUint16(header, markerPOC)
			header = be.AppendUint16(header, uint16(len(body)+2))
			header = append(header, body...)
		}
		sot := be.AppendUint16(nil, uint16(tile))
		sot = be.AppendUint32(sot, uint32(12+len(header)+2+len(data)))
		sot = append(sot, 0, 1)
		segment(markerSOT, sot)
		out = append(out, header...)
		marker(markerSOD)
		out = append(out, data...)
	}
	marker(markerEOC)
	return out
}

// String names a refinement for test labels.
func (r htRefinement) String() string {
	return [...]string{"cleanup", "SigProp", "SigProp+MagRef"}[r]
}
