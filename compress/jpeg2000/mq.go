package jpeg2000

// The MQ arithmetic decoder, PS 15444-1 Annex C.
//
// Every bit of every code-block comes through here: tier-1 asks for one binary
// decision at a time, each in a context carrying its own probability state,
// which the coder adapts as it goes. It is the same coder as JBIG2's (ITU-T
// T.88), and the procedure names below are the standard's — INITDEC, DECODE,
// MPS_EXCHANGE, LPS_EXCHANGE, RENORMD, BYTEIN — so the flowcharts can be read
// beside the code.

// qeEntry is one row of the probability table (Table C.2). qe is the estimated
// probability of the less probable symbol, nmps and nlps the next states after
// the more and the less probable symbol, and switchMPS whether the two swap
// places when the less probable one turns up.
type qeEntry struct {
	qe        uint32
	nmps      uint8
	nlps      uint8
	switchMPS bool
}

// qeTable is Table C.2. The last row is the non-adaptive state the uniform
// context sits in: it codes an even chance and never moves.
var qeTable = [47]qeEntry{
	{0x5601, 1, 1, true}, {0x3401, 2, 6, false}, {0x1801, 3, 9, false},
	{0x0AC1, 4, 12, false}, {0x0521, 5, 29, false}, {0x0221, 38, 33, false},
	{0x5601, 7, 6, true}, {0x5401, 8, 14, false}, {0x4801, 9, 14, false},
	{0x3801, 10, 14, false}, {0x3001, 11, 17, false}, {0x2401, 12, 18, false},
	{0x1C01, 13, 20, false}, {0x1601, 29, 21, false}, {0x5601, 15, 14, true},
	{0x5401, 16, 14, false}, {0x5101, 17, 15, false}, {0x4801, 18, 16, false},
	{0x3801, 19, 17, false}, {0x3401, 20, 18, false}, {0x3001, 21, 19, false},
	{0x2801, 22, 19, false}, {0x2401, 23, 20, false}, {0x2201, 24, 21, false},
	{0x1C01, 25, 22, false}, {0x1801, 26, 23, false}, {0x1601, 27, 24, false},
	{0x1401, 28, 25, false}, {0x1201, 29, 26, false}, {0x1101, 30, 27, false},
	{0x0AC1, 31, 28, false}, {0x09C1, 32, 29, false}, {0x08A1, 33, 30, false},
	{0x0521, 34, 31, false}, {0x0441, 35, 32, false}, {0x02A1, 36, 33, false},
	{0x0221, 37, 34, false}, {0x0141, 38, 35, false}, {0x0111, 39, 36, false},
	{0x0085, 40, 37, false}, {0x0049, 41, 38, false}, {0x0025, 42, 39, false},
	{0x0015, 43, 40, false}, {0x0009, 44, 41, false}, {0x0005, 45, 42, false},
	{0x0001, 45, 43, false}, {0x5601, 46, 46, false},
}

// mqContext is one context's state: where it sits in the table, and which
// symbol is the more probable one at the moment.
type mqContext struct {
	index uint8
	mps   uint8
}

// mqDecoder decodes binary decisions from one code-block's bytes.
type mqDecoder struct {
	data []byte
	bp   int    // the byte BYTEIN reads next
	c    uint32 // code register; its top 16 bits are what Qe is compared with
	a    uint32 // interval register
	ct   int    // bits left before another byte is needed

	contexts []mqContext
}

// newMQDecoder starts a decoder over a code-block's bytes with the given number
// of contexts, each in state 0 with MPS 0. Tier-1 then moves the three the
// standard starts elsewhere (Table D.7).
func newMQDecoder(data []byte, contexts int) *mqDecoder {
	d := &mqDecoder{data: data, contexts: make([]mqContext, contexts)}
	d.init()
	return d
}

// setContext puts one context into a starting state.
func (d *mqDecoder) setContext(cx int, index, mps uint8) {
	if cx >= 0 && cx < len(d.contexts) {
		d.contexts[cx] = mqContext{index: index, mps: mps}
	}
}

// byteAt returns a byte of the code-block, or 0xFF past the end.
//
// Running past the data is not an error here. The encoder is allowed to drop
// trailing bytes whose value the decoder can infer, and tier-1 asks for exactly
// as many decisions as a pass needs; 0xFF is what the flowchart feeds when it
// meets a marker. Bounds are checked in one place so no other path can read
// memory it does not own.
func (d *mqDecoder) byteAt(i int) uint32 {
	if i < 0 || i >= len(d.data) {
		return 0xFF
	}
	return uint32(d.data[i])
}

// init is INITDEC (Figure C.20).
func (d *mqDecoder) init() {
	d.bp = 0
	d.c = d.byteAt(0) << 16
	d.byteIn()
	d.c <<= 7
	d.ct -= 7
	d.a = 0x8000
}

// byteIn is BYTEIN (Figure C.19): another byte into the code register, with the
// bit un-stuffing that keeps a 0xFF pair from being read as a marker.
func (d *mqDecoder) byteIn() {
	if d.byteAt(d.bp) == 0xFF {
		if d.byteAt(d.bp+1) > 0x8F {
			// A marker, or the end of the data: 1 bits from here on.
			d.c += 0xFF00
			d.ct = 8
			return
		}
		d.bp++
		d.c += d.byteAt(d.bp) << 9
		d.ct = 7
		return
	}
	d.bp++
	d.c += d.byteAt(d.bp) << 8
	d.ct = 8
}

// decode is DECODE (Figure C.16): one binary decision in context cx.
func (d *mqDecoder) decode(cx int) int {
	ctx := &d.contexts[cx]
	entry := &qeTable[ctx.index]
	qe := entry.qe

	d.a -= qe
	if (d.c >> 16) < qe {
		bit := d.lpsExchange(ctx, entry, qe)
		d.renorm()
		return bit
	}

	d.c -= qe << 16
	if d.a&0x8000 != 0 {
		// The interval is still large enough: no renormalization and no state
		// change, which is the common path and why the coder is quick.
		return int(ctx.mps)
	}
	bit := d.mpsExchange(ctx, entry)
	d.renorm()
	return bit
}

// mpsExchange is MPS_EXCHANGE (Figure C.17).
func (d *mqDecoder) mpsExchange(ctx *mqContext, entry *qeEntry) int {
	if d.a < entry.qe {
		// The two sub-intervals have crossed: the symbol in the larger half is
		// now the less probable one.
		bit := 1 - int(ctx.mps)
		if entry.switchMPS {
			ctx.mps = 1 - ctx.mps
		}
		ctx.index = entry.nlps
		return bit
	}
	ctx.index = entry.nmps
	return int(ctx.mps)
}

// lpsExchange is LPS_EXCHANGE (Figure C.18).
func (d *mqDecoder) lpsExchange(ctx *mqContext, entry *qeEntry, qe uint32) int {
	if d.a < qe {
		d.a = qe
		ctx.index = entry.nmps
		return int(ctx.mps)
	}
	d.a = qe
	bit := 1 - int(ctx.mps)
	if entry.switchMPS {
		ctx.mps = 1 - ctx.mps
	}
	ctx.index = entry.nlps
	return bit
}

// renorm is RENORMD (Figure C.21): shift until the interval is large again,
// taking another byte whenever the code register runs out.
func (d *mqDecoder) renorm() {
	for {
		if d.ct == 0 {
			d.byteIn()
		}
		d.a <<= 1
		d.c <<= 1
		d.ct--
		if d.a&0x8000 != 0 {
			return
		}
	}
}
