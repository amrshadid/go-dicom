package jpeg2000

// The MQ arithmetic encoder, PS 15444-1 Annex C (Figures C.7 to C.12): the
// other half of mq.go, and the same probability table.
//
// Tier-1 hands it one binary decision at a time, each in a context, and it
// narrows an interval by that decision's estimated probability. The
// procedure names are the standard's: ENCODE, CODEMPS, CODELPS, RENORME,
// BYTEOUT, FLUSH.
//
// It began as a helper in the decoder's tests, written so the decoder could
// be checked against something that was not itself. The round trip between
// the two is still what tests both, and what neither can catch, a
// misreading they share, is caught where whole codestreams are held against
// OpenJPEG.

// mqEncoder encodes binary decisions into one code-block's bytes.
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
