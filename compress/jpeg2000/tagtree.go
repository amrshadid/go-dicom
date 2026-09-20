package jpeg2000

// Tag trees, PS 15444-1 Annex B.10.2.
//
// A packet header has two things to say about every code-block in a precinct:
// in which layer it first appears, and how many of its most significant bit
// planes are all zero. Both are coded as tag trees — quadtrees over the
// code-block grid where each node holds a value at least as large as its
// parent's, so the parent's value is a lower bound that has already been sent.
//
// That structure is why a packet for a precinct of 64 blocks can say "none of
// these are here yet" in a few bits: the root answers for all of them.
type tagTree struct {
	// levels holds the node values from the leaves (level 0) up to the root.
	// Each level is a grid half the size of the one below it, rounded up.
	levels []tagTreeLevel
}

type tagTreeLevel struct {
	w, h  int
	value []int32
	known []bool // whether the node's value is final rather than a lower bound
}

// newTagTree builds a tree over a w by h grid of leaves.
func newTagTree(w, h int) *tagTree {
	t := &tagTree{}
	for w > 0 && h > 0 {
		t.levels = append(t.levels, tagTreeLevel{
			w: w, h: h,
			value: make([]int32, w*h),
			known: make([]bool, w*h),
		})
		if w == 1 && h == 1 {
			break
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	return t
}

// reset returns the tree to its starting state, which a new precinct needs.
func (t *tagTree) reset() {
	for _, level := range t.levels {
		for i := range level.value {
			level.value[i] = 0
			level.known[i] = false
		}
	}
}

// decode reads the value of one leaf, stopping as soon as it is known to be at
// least threshold — which is all a packet header needs when it is only asking
// "is this block included in this layer yet".
//
// It returns the value and whether it is final. A value that reached the
// threshold without being confirmed is a lower bound: the answer is "not yet,
// and not before threshold".
func (t *tagTree) decode(r *bitReader, leaf int, threshold int32) (int32, bool, error) {
	if len(t.levels) == 0 {
		return 0, false, errorf("tag tree has no levels")
	}
	leaves := t.levels[0]
	if leaf < 0 || leaf >= len(leaves.value) {
		return 0, false, errorf("tag tree leaf %d of %d", leaf, len(leaves.value))
	}

	// The path from the root down to the leaf, as an index per level.
	x, y := leaf%leaves.w, leaf/leaves.w
	path := make([]int, len(t.levels))
	for level := range t.levels {
		path[level] = (y>>level)*t.levels[level].w + (x >> level)
	}

	var lower int32
	for level := len(t.levels) - 1; level >= 0; level-- {
		node := &t.levels[level]
		index := path[level]
		if node.value[index] < lower {
			// A child starts from its parent's value: everything below the
			// parent has already been ruled out.
			node.value[index] = lower
		}
		for !node.known[index] && node.value[index] < threshold {
			bit, err := r.readBit()
			if err != nil {
				return 0, false, err
			}
			if bit == 1 {
				node.known[index] = true
			} else {
				node.value[index]++
			}
		}
		lower = node.value[index]
		if !node.known[index] {
			// The parent is only known to be at least threshold, so nothing
			// below it can be decided yet either.
			return lower, false, nil
		}
	}
	return lower, true, nil
}

// bitReader reads a packet header bit by bit, with the bit un-stuffing the
// standard requires: after a byte of 0xFF only seven bits of the next byte
// carry data, so that no 0xFF 0x90-or-above pair — a marker — can appear
// inside a header (B.10.1).
type bitReader struct {
	data []byte
	pos  int
	buf  uint32
	bits int
	last byte
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (r *bitReader) readBit() (int, error) {
	if r.bits == 0 {
		if r.pos >= len(r.data) {
			return 0, errorf("packet header ended mid-value")
		}
		b := r.data[r.pos]
		r.pos++
		if r.last == 0xFF {
			// The stuffed bit: seven data bits in this byte, and its top bit
			// must be zero or the pair would read as a marker.
			if b > 0x7F {
				return 0, errorf("packet header holds the marker %02X%02X", r.last, b)
			}
			r.buf = uint32(b)
			r.bits = 7
		} else {
			r.buf = uint32(b)
			r.bits = 8
		}
		r.last = b
	}
	r.bits--
	return int(r.buf>>uint(r.bits)) & 1, nil
}

// readBits reads n bits, most significant first.
func (r *bitReader) readBits(n int) (uint32, error) {
	var v uint32
	for i := 0; i < n; i++ {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | uint32(bit)
	}
	return v, nil
}

// align ends the header at a byte boundary, skipping the stuffed bit when the
// last byte read was 0xFF (B.10.1).
func (r *bitReader) align() {
	r.bits = 0
	if r.last == 0xFF {
		if r.pos < len(r.data) {
			r.pos++
		}
		r.last = 0
	}
}

// offset is how many bytes of the header have been consumed, which is where
// the packet's body begins.
func (r *bitReader) offset() int { return r.pos }
