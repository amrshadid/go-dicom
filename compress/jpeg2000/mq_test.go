package jpeg2000

import (
	"math/rand"
	"testing"
)

// mqEncoder is the MQ encoder of Annex C (Figures C.7 to C.12), written here
// because the library never encodes JPEG 2000 — it exists so the decoder can be
// checked against something that is not itself.
//
// What this proves and what it does not: a round trip shows the decoder inverts
// an encoder built from the same annex, which catches a misread flowchart, a
// wrong table row, a renormalization that shifts the wrong register. It cannot
// catch a misunderstanding the two share. That is what stage 5 is for, where
// real code-blocks from pydicom's corpus have to come out as the pixels
// OpenJPEG produces.
type mqEncoder struct {
	out      []byte
	a, c     uint32
	ct       int
	contexts []mqContext
}

func newMQEncoder(contexts int) *mqEncoder {
	return &mqEncoder{a: 0x8000, ct: 12, contexts: make([]mqContext, contexts)}
}

func (e *mqEncoder) setContext(cx int, index, mps uint8) {
	e.contexts[cx] = mqContext{index: index, mps: mps}
}

func (e *mqEncoder) encode(cx, bit int) {
	ctx := &e.contexts[cx]
	entry := &qeTable[ctx.index]
	if bit == int(ctx.mps) {
		// CODEMPS (Figure C.8).
		e.a -= entry.qe
		if e.a&0x8000 == 0 {
			if e.a < entry.qe {
				e.a = entry.qe
			} else {
				e.c += entry.qe
			}
			ctx.index = entry.nmps
			e.renorm()
			return
		}
		e.c += entry.qe
		return
	}
	// CODELPS (Figure C.9).
	e.a -= entry.qe
	if e.a < entry.qe {
		e.c += entry.qe
	} else {
		e.a = entry.qe
	}
	if entry.switchMPS {
		ctx.mps = 1 - ctx.mps
	}
	ctx.index = entry.nlps
	e.renorm()
}

// renorm is RENORME (Figure C.10).
func (e *mqEncoder) renorm() {
	for {
		e.a <<= 1
		e.c <<= 1
		e.ct--
		if e.ct == 0 {
			e.byteOut()
		}
		if e.a&0x8000 != 0 {
			return
		}
	}
}

// byteOut is BYTEOUT (Figure C.11), with the bit stuffing that keeps an 0xFF
// from being followed by a byte a decoder would read as a marker.
func (e *mqEncoder) byteOut() {
	last := byte(0)
	if len(e.out) > 0 {
		last = e.out[len(e.out)-1]
	}
	switch {
	case last == 0xFF:
		e.out = append(e.out, byte(e.c>>20))
		e.c &= 0xFFFFF
		e.ct = 7
	case e.c < 0x8000000:
		e.out = append(e.out, byte(e.c>>19))
		e.c &= 0x7FFFF
		e.ct = 8
	default:
		if len(e.out) > 0 {
			e.out[len(e.out)-1]++
		}
		if len(e.out) > 0 && e.out[len(e.out)-1] == 0xFF {
			e.c &= 0x7FFFFFF
			e.out = append(e.out, byte(e.c>>20))
			e.c &= 0xFFFFF
			e.ct = 7
			return
		}
		e.out = append(e.out, byte(e.c>>19))
		e.c &= 0x7FFFF
		e.ct = 8
	}
}

// flush is FLUSH (Figure C.12): push the last bits out, then drop the trailing
// 0xFF bytes the decoder infers for itself.
func (e *mqEncoder) flush() []byte {
	// SETBITS.
	tempC := e.c + e.a
	e.c |= 0xFFFF
	if e.c >= tempC {
		e.c -= 0x8000
	}
	e.c <<= e.ct
	e.byteOut()
	e.c <<= e.ct
	e.byteOut()

	out := e.out
	for len(out) > 0 && out[len(out)-1] == 0xFF {
		out = out[:len(out)-1]
	}
	return out
}

// TestMQDecoderInvertsTheEncoder round-trips decisions through the encoder of
// Annex C and back. The contexts move as the coder adapts, so a wrong next-state
// or a missed switch shows up as a decision that comes back different.
func TestMQDecoderInvertsTheEncoder(t *testing.T) {
	const contexts = 19 // what tier-1 uses (Table D.1)

	cases := map[string]func(i int) (cx, bit int){
		"all zeros in one context":    func(int) (int, int) { return 0, 0 },
		"all ones in one context":     func(int) (int, int) { return 0, 1 },
		"alternating":                 func(i int) (int, int) { return 0, i % 2 },
		"skewed, one in sixteen":      func(i int) (int, int) { return 3, map[bool]int{true: 1, false: 0}[i%16 == 0] },
		"spread across every context": func(i int) (int, int) { return i % contexts, (i / contexts) % 2 },
		"runs of each symbol":         func(i int) (int, int) { return 5, (i / 37) % 2 },
	}

	for name, pattern := range cases {
		t.Run(name, func(t *testing.T) {
			const n = 4000
			want := make([]int, n)
			cxs := make([]int, n)
			enc := newMQEncoder(contexts)
			for i := 0; i < n; i++ {
				cxs[i], want[i] = pattern(i)
				enc.encode(cxs[i], want[i])
			}
			data := enc.flush()

			dec := newMQDecoder(data, contexts)
			for i := 0; i < n; i++ {
				if got := dec.decode(cxs[i]); got != want[i] {
					t.Fatalf("decision %d in context %d came back %d, want %d (%d bytes coded)",
						i, cxs[i], got, want[i], len(data))
				}
			}
		})
	}
}

// TestMQDecoderInvertsTheEncoderOnRandomInput covers what a fixed pattern does
// not: sequences that move the contexts through the table unevenly.
func TestMQDecoderInvertsTheEncoderOnRandomInput(t *testing.T) {
	const contexts = 19
	rng := rand.New(rand.NewSource(1))

	for trial := 0; trial < 50; trial++ {
		n := 1 + rng.Intn(3000)
		// A different bias each trial: a nearly-certain symbol drives the
		// context to the far end of the table, an even one keeps it near 0.
		bias := rng.Float64()
		cxs, want := make([]int, n), make([]int, n)
		enc := newMQEncoder(contexts)
		// Start some contexts where Table D.7 starts them.
		enc.setContext(18, 46, 0)
		enc.setContext(17, 3, 0)
		enc.setContext(0, 4, 0)
		for i := 0; i < n; i++ {
			cxs[i] = rng.Intn(contexts)
			if rng.Float64() < bias {
				want[i] = 1
			}
			enc.encode(cxs[i], want[i])
		}
		data := enc.flush()

		dec := newMQDecoder(data, contexts)
		dec.setContext(18, 46, 0)
		dec.setContext(17, 3, 0)
		dec.setContext(0, 4, 0)
		for i := 0; i < n; i++ {
			if got := dec.decode(cxs[i]); got != want[i] {
				t.Fatalf("trial %d: decision %d in context %d came back %d, want %d",
					trial, i, cxs[i], got, want[i])
			}
		}
	}
}

// TestQeTableIsWellFormed checks the table's own structure: every next-state
// points inside the table, the uniform state is fixed, and the probabilities
// never rise as the state advances on the more probable symbol.
func TestQeTableIsWellFormed(t *testing.T) {
	for i, entry := range qeTable {
		if int(entry.nmps) >= len(qeTable) || int(entry.nlps) >= len(qeTable) {
			t.Errorf("row %d points outside the table: nmps=%d nlps=%d", i, entry.nmps, entry.nlps)
		}
		if entry.qe == 0 || entry.qe > 0x5601 {
			t.Errorf("row %d has Qe %#04x, outside the range the coder allows", i, entry.qe)
		}
	}
	// Row 46 is the non-adaptive uniform state: it stays where it is whichever
	// symbol turns up, which is what makes it code an even chance.
	if uniform := qeTable[46]; uniform.nmps != 46 || uniform.nlps != 46 || uniform.switchMPS {
		t.Errorf("row 46 is %+v, want a state that never moves", uniform)
	}
	// Only three rows swap the symbols, and they are the three the standard
	// marks: 0, 6 and 14.
	var switches []int
	for i, entry := range qeTable {
		if entry.switchMPS {
			switches = append(switches, i)
		}
	}
	if len(switches) != 3 || switches[0] != 0 || switches[1] != 6 || switches[2] != 14 {
		t.Errorf("rows with SWITCH set are %v, want [0 6 14]", switches)
	}
}

// TestMQDecoderPastTheEnd: a pass may ask for more decisions than the data
// holds, because the encoder drops bytes a decoder can infer. It must keep
// answering rather than read past the slice.
func TestMQDecoderPastTheEnd(t *testing.T) {
	for _, data := range [][]byte{{}, {0x00}, {0xFF}, {0xFF, 0xFF}, {0x84, 0xC7, 0x3B, 0xFF, 0xFF}} {
		dec := newMQDecoder(data, 19)
		for i := 0; i < 5000; i++ {
			if bit := dec.decode(i % 19); bit != 0 && bit != 1 {
				t.Fatalf("decode returned %d", bit)
			}
		}
	}
}
