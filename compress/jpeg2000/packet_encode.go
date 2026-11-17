package jpeg2000

// Packet headers and tag trees as an encoder writes them, PS 15444-1 Annex
// B.10: the other half of packet.go and tagtree.go.
//
// A packet is the contribution of one resolution of one component to one
// layer. Its header says, for each code-block, whether it is there, how many
// of its bit planes are empty, how many coding passes it brings and how many
// bytes; its body is those bytes. This encoder writes one layer and one
// precinct for each resolution, so there is one packet for each resolution of
// each component, and a code-block appears in it whole or not at all.

// headerWriter writes a packet header bit by bit, with the bit stuffing the
// standard requires: after a byte of 0xFF the next carries seven bits, its
// top one zero, so that nothing in a header can be read as a marker
// (B.10.1). It is what bitReader in tagtree.go reads.
type headerWriter struct {
	out  []byte
	buf  byte
	room int // bits still to fill in buf; 0 when no byte is open
}

func (w *headerWriter) writeBit(bit int) {
	if w.room == 0 {
		w.room = 8
		if len(w.out) > 0 && w.out[len(w.out)-1] == 0xFF {
			w.room = 7
		}
		w.buf = 0
	}
	w.room--
	w.buf |= byte(bit&1) << uint(w.room)
	if w.room == 0 {
		w.out = append(w.out, w.buf)
	}
}

// writeBits writes the low n bits of v, most significant first.
func (w *headerWriter) writeBits(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		w.writeBit(int(v>>uint(i)) & 1)
	}
}

// bytes ends the header at a byte boundary and returns it. A header whose
// last byte is 0xFF is followed by a byte of zero: the stuffed bit and seven
// bits of padding, which the reader skips, so that the body's first byte
// cannot complete a marker (B.10.1).
func (w *headerWriter) bytes() []byte {
	if w.room != 0 {
		w.out = append(w.out, w.buf)
		w.room = 0
	}
	if len(w.out) > 0 && w.out[len(w.out)-1] == 0xFF {
		w.out = append(w.out, 0x00)
	}
	return w.out
}

// tagTreeEncoder is the encoder side of a tag tree (B.10.2). It mirrors the
// decoder: walk the path from the root to the leaf, and for each node write
// a 0 for every step below its value and a 1 when the value is reached,
// stopping at the threshold.
//
// It began in the decoder's tests, so the decoder answered to something
// other than itself, and that round trip still tests both.
type tagTreeEncoder struct {
	// nodes[level][index] is the value being coded: a leaf's own at level 0,
	// and the smallest value beneath it at every level above.
	nodes [][]int32
	// low and known are the coding state, kept apart from the values so that
	// walking the tree cannot overwrite what it is coding.
	low   [][]int32
	known [][]bool
	dims  [][2]int
}

// newTagTreeEncoder builds the tree over a w by h grid of leaves with the
// given values, in raster order.
func newTagTreeEncoder(w, h int, values []int32) *tagTreeEncoder {
	e := &tagTreeEncoder{}
	for lw, lh := w, h; ; lw, lh = (lw+1)/2, (lh+1)/2 {
		e.dims = append(e.dims, [2]int{lw, lh})
		e.nodes = append(e.nodes, make([]int32, lw*lh))
		e.low = append(e.low, make([]int32, lw*lh))
		e.known = append(e.known, make([]bool, lw*lh))
		if lw <= 1 && lh <= 1 {
			break
		}
	}
	copy(e.nodes[0], values)
	// Each node above the leaves holds the smallest value beneath it.
	for level := 1; level < len(e.nodes); level++ {
		bw, bh := e.dims[level-1][0], e.dims[level-1][1]
		aw, ah := e.dims[level][0], e.dims[level][1]
		for y := 0; y < ah; y++ {
			for x := 0; x < aw; x++ {
				best := int32(1 << 30)
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						cx, cy := x*2+dx, y*2+dy
						if cx < bw && cy < bh {
							if v := e.nodes[level-1][cy*bw+cx]; v < best {
								best = v
							}
						}
					}
				}
				e.nodes[level][y*aw+x] = best
			}
		}
	}
	return e
}

// encode codes one leaf up to a threshold: enough for a reader to learn the
// leaf's value when it is below the threshold, and that it is not when it
// is not.
func (e *tagTreeEncoder) encode(out *headerWriter, leaf int, threshold int32) {
	w := e.dims[0][0]
	x, y := leaf%w, leaf/w

	var lower int32
	for level := len(e.nodes) - 1; level >= 0; level-- {
		index := (y>>level)*e.dims[level][0] + (x >> level)
		if e.low[level][index] < lower {
			e.low[level][index] = lower
		}
		value := e.nodes[level][index]
		for !e.known[level][index] && e.low[level][index] < threshold {
			if e.low[level][index] == value {
				out.writeBit(1)
				e.known[level][index] = true
			} else {
				out.writeBit(0)
				e.low[level][index]++
			}
		}
		lower = e.low[level][index]
		if !e.known[level][index] {
			return
		}
	}
}

// writePassCount writes how many coding passes a contribution carries, in
// the code of Table B.4, which readPassCount reads. The code reaches 164,
// and a block of the 30 bit planes EncodeBlock allows has 88.
func writePassCount(w *headerWriter, passes int) {
	switch {
	case passes == 1:
		w.writeBit(0)
	case passes == 2:
		w.writeBits(0b10, 2)
	case passes <= 5:
		w.writeBits(0b11, 2)
		w.writeBits(uint32(passes-3), 2)
	case passes <= 36:
		w.writeBits(0b1111, 4)
		w.writeBits(uint32(passes-6), 5)
	default:
		w.writeBits(0b111111111, 9)
		w.writeBits(uint32(passes-37), 7)
	}
}

// initialLblock is the width a code-block's length field starts at (B.10.7.1).
const initialLblock = 3

// writeSegmentLength writes how many bytes a contribution occupies
// (B.10.7.1), which readSegmentLength reads. The field is Lblock bits wide,
// plus the logarithm of the number of passes; Lblock starts at three and is
// raised, a 1 bit for each step, until the length fits.
func writeSegmentLength(w *headerWriter, length, passes int) {
	lblock := initialLblock
	for length >= 1<<uint(lblock+log2(passes)) {
		w.writeBit(1)
		lblock++
	}
	w.writeBit(0)
	w.writeBits(uint32(length), lblock+log2(passes))
}

// packetBlock is one code-block as a packet carries it: the encoded block,
// and how many bit planes above it the band could hold and it does not use.
type packetBlock struct {
	EncodedBlock
	zeroBitPlanes int
}

// writePacket writes the one packet of a resolution of a component: its
// header and then its body. bands holds, for each band of the resolution in
// the order the geometry lists them, the band's code-blocks in raster order.
//
// A resolution with no code-block that has anything in it is an empty
// packet, which is a single 0 bit (B.10.3).
func writePacket(res Resolution, bands [][]packetBlock) []byte {
	header := &headerWriter{}
	var body []byte

	empty := true
	for _, blocks := range bands {
		for _, block := range blocks {
			if block.Passes > 0 {
				empty = false
			}
		}
	}
	if empty {
		header.writeBit(0)
		return header.bytes()
	}
	header.writeBit(1)

	for bandIndex, band := range res.Bands {
		blocks := bands[bandIndex]
		if len(band.Blocks) == 0 {
			continue
		}
		// With one layer, a block is in layer 0 or in none. The inclusion
		// tree holds the first layer each appears in, so one that never
		// does is given layer 1, which a reader asking about layer 0 learns
		// only as "not yet".
		w, h := blockGridOf(band)
		included := make([]int32, len(blocks))
		zeroPlanes := make([]int32, len(blocks))
		for i, block := range blocks {
			if block.Passes == 0 {
				included[i] = 1
			}
			zeroPlanes[i] = int32(block.zeroBitPlanes)
		}
		inclusion := newTagTreeEncoder(w, h, included)
		zeroBits := newTagTreeEncoder(w, h, zeroPlanes)

		for i, block := range blocks {
			inclusion.encode(header, i, 1)
			if block.Passes == 0 {
				continue
			}
			// The zero bit planes in full: thresholds raised until the
			// value is final, as readZeroBitPlanes reads them.
			zeroBits.encode(header, i, int32(block.zeroBitPlanes)+1)
			writePassCount(header, block.Passes)
			writeSegmentLength(header, len(block.Data), block.Passes)
			body = append(body, block.Data...)
		}
	}
	return append(header.bytes(), body...)
}
