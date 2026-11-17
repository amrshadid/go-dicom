package jpeg2000

import (
	"math/rand"
	"testing"
)

// TestTagTreeDecodesWhatWasEncoded round-trips leaf values through the encoder
// of B.10.2. A tag tree is how a packet says "none of these blocks are here
// yet" in a couple of bits, so the shared lower bound between a parent and its
// children is the whole point — and the thing a wrong path or a missed reset
// breaks.
func TestTagTreeDecodesWhatWasEncoded(t *testing.T) {
	sizes := []struct{ w, h int }{{1, 1}, {2, 2}, {3, 1}, {1, 3}, {4, 4}, {5, 3}, {8, 8}, {7, 9}}
	rng := rand.New(rand.NewSource(7))

	for _, size := range sizes {
		for trial := 0; trial < 20; trial++ {
			n := size.w * size.h
			values := make([]int32, n)
			for i := range values {
				values[i] = int32(rng.Intn(6))
			}

			// A high threshold asks for the exact value of every leaf.
			const threshold = 32
			enc := newTagTreeEncoder(size.w, size.h, values)
			out := &headerWriter{}
			for leaf := 0; leaf < n; leaf++ {
				enc.encode(out, leaf, threshold)
			}
			data := out.bytes()

			dec := newTagTree(size.w, size.h)
			r := newBitReader(data)
			for leaf := 0; leaf < n; leaf++ {
				got, known, err := dec.decode(r, leaf, threshold)
				if err != nil {
					t.Fatalf("%dx%d leaf %d: %v", size.w, size.h, leaf, err)
				}
				if !known {
					t.Fatalf("%dx%d leaf %d: value not resolved below the threshold", size.w, size.h, leaf)
				}
				if got != values[leaf] {
					t.Fatalf("%dx%d leaf %d: decoded %d, want %d", size.w, size.h, leaf, got, values[leaf])
				}
			}
		}
	}
}

// TestTagTreeStopsAtTheThreshold covers the other way a packet header uses a
// tree: asking only whether a block is included in this layer yet. The answer
// is "not yet" without the exact layer ever being coded.
func TestTagTreeStopsAtTheThreshold(t *testing.T) {
	// Four leaves, none included before layer 3.
	values := []int32{3, 5, 4, 7}
	enc := newTagTreeEncoder(2, 2, values)
	out := &headerWriter{}
	for leaf := range values {
		enc.encode(out, leaf, 1) // "is it in layer 0?"
	}
	data := out.bytes()

	dec := newTagTree(2, 2)
	r := newBitReader(data)
	for leaf := range values {
		got, known, err := dec.decode(r, leaf, 1)
		if err != nil {
			t.Fatalf("leaf %d: %v", leaf, err)
		}
		if known {
			t.Errorf("leaf %d reported a final value of %d; none is included in layer 0", leaf, got)
		}
		if got < 1 {
			t.Errorf("leaf %d is only known to be at least %d, want at least 1", leaf, got)
		}
	}
}

// TestBitReaderUnstuffs covers B.10.1: after a byte of 0xFF only seven bits of
// the next byte carry data, so that no marker can appear inside a header.
func TestBitReaderUnstuffs(t *testing.T) {
	// 0xFF then 0x7F: eight bits of ones, then seven more.
	r := newBitReader([]byte{0xFF, 0x7F})
	for i := 0; i < 8; i++ {
		if bit, err := r.readBit(); err != nil || bit != 1 {
			t.Fatalf("bit %d of 0xFF is %d (%v)", i, bit, err)
		}
	}
	// The stuffed bit is skipped: the next seven bits are the low seven of 0x7F.
	for i := 0; i < 7; i++ {
		if bit, err := r.readBit(); err != nil || bit != 1 {
			t.Fatalf("bit %d after the stuffed one is %d (%v)", i, bit, err)
		}
	}
	if _, err := r.readBit(); err == nil {
		t.Error("reading past the end returned no error")
	}

	// 0xFF followed by a byte above 0x7F would be a marker, which cannot occur
	// inside a packet header.
	marker := newBitReader([]byte{0xFF, 0x90})
	for i := 0; i < 8; i++ {
		_, _ = marker.readBit()
	}
	if _, err := marker.readBit(); err == nil {
		t.Error("a marker inside a packet header was accepted")
	}
}

// TestBitReaderAlign: a header ends on a byte boundary, and the stuffed bit
// after a trailing 0xFF belongs to the header, not to the body that follows.
func TestBitReaderAlign(t *testing.T) {
	r := newBitReader([]byte{0xFF, 0x00, 0xAB})
	for i := 0; i < 8; i++ {
		_, _ = r.readBit()
	}
	r.align()
	if got := r.offset(); got != 2 {
		t.Errorf("after a trailing 0xFF the header ends at %d, want 2", got)
	}

	plain := newBitReader([]byte{0x0F, 0xAB})
	_, _ = plain.readBit()
	plain.align()
	if got := plain.offset(); got != 1 {
		t.Errorf("the header ends at %d, want 1", got)
	}
}

// TestBitReaderReadBits covers multi-bit reads, which packet headers use for
// the code-block length fields and the pass counts.
func TestBitReaderReadBits(t *testing.T) {
	r := newBitReader([]byte{0b10110010, 0b01000000})
	for _, want := range []struct {
		n int
		v uint32
	}{{1, 1}, {3, 0b011}, {4, 0b0010}, {2, 0b01}} {
		got, err := r.readBits(want.n)
		if err != nil {
			t.Fatalf("readBits(%d): %v", want.n, err)
		}
		if got != want.v {
			t.Errorf("readBits(%d) = %b, want %b", want.n, got, want.v)
		}
	}
	if _, err := r.readBits(32); err == nil {
		t.Error("reading past the end returned no error")
	}
}

// TestTagTreeResetForgetsEverything: a precinct's trees start afresh in each
// packet sequence, and a tree that remembered the last precinct's values would
// read the next one's header as already-included blocks.
func TestTagTreeResetForgetsEverything(t *testing.T) {
	values := []int32{0, 1, 2, 3}
	enc := newTagTreeEncoder(2, 2, values)
	out := &headerWriter{}
	for leaf := range values {
		enc.encode(out, leaf, 32)
	}
	data := out.bytes()

	tree := newTagTree(2, 2)
	decodeAll := func() []int32 {
		r := newBitReader(data)
		out := make([]int32, len(values))
		for leaf := range values {
			v, known, err := tree.decode(r, leaf, 32)
			if err != nil || !known {
				t.Fatalf("leaf %d: %v (known=%v)", leaf, err, known)
			}
			out[leaf] = v
		}
		return out
	}

	first := decodeAll()
	tree.reset()
	second := decodeAll()
	for i := range first {
		if first[i] != second[i] || first[i] != values[i] {
			t.Fatalf("leaf %d: %d then %d, want %d both times", i, first[i], second[i], values[i])
		}
	}
}
