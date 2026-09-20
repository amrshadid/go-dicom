package jpeg2000

// Packet headers, PS 15444-1 Annex B.10.
//
// A tile-part's bytes are a sequence of packets. Each packet belongs to one
// (layer, resolution, component, precinct) and carries, for every code-block in
// that precinct: whether the block appears at all, how many of its leading bit
// planes are zero, how many coding passes it contributes, and how many bytes
// those passes occupy. The bytes themselves follow the header.
//
// Nothing here decodes a coefficient. This is the layer that says which bytes
// belong to which code-block, so tier-1 can be handed one block's data.

// CodeBlockData is everything one code-block contributed across the layers of
// a tile: its place in the band, and the bytes tier-1 will decode.
type CodeBlockData struct {
	Block         CodeBlock
	BandType      int
	Resolution    int
	Component     int
	ZeroBitPlanes int // leading bit planes that are all zero (B.10.5)
	Passes        int
	Data          []byte
}

// TileData is one tile's code-blocks, in the order the geometry lists them.
type TileData struct {
	Tile       int
	Components []TileComponent
	Blocks     []CodeBlockData

	// BytesRead is how much of the tile's data the packets accounted for. It
	// should be all of it: the packet headers are self-describing, so a reader
	// that ends anywhere but the last byte has misread a field somewhere.
	BytesRead int
	TileBytes int
}

// blockState is what a packet header has said about one code-block so far. The
// state persists across layers: a block included in layer 0 is signaled with a
// single bit in layer 1, and its length field grows as its contributions do.
type blockState struct {
	included      bool
	zeroBitPlanes int
	lblock        int // bits in the length field, 3 to start (B.10.7.1)
	passes        int
	data          []byte
}

// precinctState holds the two tag trees a precinct's packets share and the
// per-block state.
type precinctState struct {
	inclusion *tagTree
	zeroBits  *tagTree
	blocks    []blockState
}

// ReadTilePackets reads every packet of a tile and returns the code-blocks with
// their data gathered across layers.
//
// The tile-parts are concatenated first: the standard allows a packet to be
// split across tile-parts, and a decoder that read each part separately would
// stop at the seam.
func (c *Codestream) ReadTilePackets(tile int) (*TileData, error) {
	var data []byte
	for _, part := range c.TileParts {
		if part.Tile == tile {
			data = append(data, part.Data...)
		}
	}
	if len(data) == 0 {
		return nil, errorf("tile %d has no data", tile)
	}

	out := &TileData{Tile: tile}
	states := map[[4]int]*precinctState{} // component, resolution, band, precinct

	maxResolutions := 0
	for comp := range c.Components {
		tc, err := c.TileComponentFor(tile, comp)
		if err != nil {
			return nil, err
		}
		out.Components = append(out.Components, tc)
		if len(tc.Resolutions) > maxResolutions {
			maxResolutions = len(tc.Resolutions)
		}
	}

	style := c.CodingForTile(tile, 0)
	pos := 0
	for _, step := range progressionOrder(style, maxResolutions, len(c.Components), out.Components) {
		if pos >= len(data) {
			// Layers may simply stop: an encoder writes as many as it wrote,
			// and the rest of the progression is empty.
			break
		}
		next, err := c.readPacket(data, pos, tile, step, states, out)
		if err != nil {
			return nil, err
		}
		pos = next
	}

	out.BytesRead = pos
	out.TileBytes = len(data)

	// The blocks in geometry order, which is the order tier-1 wants them.
	for _, key := range sortedKeys(states) {
		state := states[key]
		comp, res, band, _ := key[0], key[1], key[2], key[3]
		tc := out.Components[comp]
		bandInfo := tc.Resolutions[res].Bands[band]
		for i := range state.blocks {
			b := &state.blocks[i]
			if !b.included || len(b.data) == 0 {
				continue
			}
			out.Blocks = append(out.Blocks, CodeBlockData{
				Block:         bandInfo.Blocks[i],
				BandType:      bandInfo.Type,
				Resolution:    res,
				Component:     comp,
				ZeroBitPlanes: b.zeroBitPlanes,
				Passes:        b.passes,
				Data:          b.data,
			})
		}
	}
	return out, nil
}

// packetStep names one packet's place in the progression.
type packetStep struct {
	layer      int
	resolution int
	component  int
	precinct   int
}

// progressionOrder lists the packets of a tile in the order they appear
// (B.12). Only the two orders the parser accepts are produced.
func progressionOrder(style CodingStyle, resolutions, components int, comps []TileComponent) []packetStep {
	var steps []packetStep
	precincts := func(comp, res int) int {
		if comp >= len(comps) || res >= len(comps[comp].Resolutions) {
			return 0
		}
		return comps[comp].Resolutions[res].Precincts()
	}

	if style.Progression == ProgressionRLCP {
		for r := 0; r < resolutions; r++ {
			for l := 0; l < style.Layers; l++ {
				for c := 0; c < components; c++ {
					for p := 0; p < precincts(c, r); p++ {
						steps = append(steps, packetStep{l, r, c, p})
					}
				}
			}
		}
		return steps
	}

	for l := 0; l < style.Layers; l++ {
		for r := 0; r < resolutions; r++ {
			for c := 0; c < components; c++ {
				for p := 0; p < precincts(c, r); p++ {
					steps = append(steps, packetStep{l, r, c, p})
				}
			}
		}
	}
	return steps
}

// readPacket reads one packet's header and body, returning the offset after it.
func (c *Codestream) readPacket(data []byte, pos, tile int, step packetStep,
	states map[[4]int]*precinctState, out *TileData) (int, error) {

	style := c.CodingForTile(tile, step.component)
	tc := out.Components[step.component]
	if step.resolution >= len(tc.Resolutions) {
		return pos, nil // this component has fewer resolutions than another
	}
	res := tc.Resolutions[step.resolution]

	// An SOP marker may precede any packet (A.8.1). It carries a sequence
	// number this decoder does not need, so it is skipped.
	if style.SOP && pos+6 <= len(data) && be16(data[pos:]) == 0xFF91 {
		pos += 6
	}

	r := newBitReader(data[pos:])
	nonEmpty, err := r.readBit()
	if err != nil {
		return 0, err
	}

	type contribution struct {
		key    [4]int
		block  int
		length int
	}
	var contributions []contribution

	if nonEmpty == 1 {
		for bandIndex, band := range res.Bands {
			if len(band.Blocks) == 0 {
				continue
			}
			key := [4]int{step.component, step.resolution, bandIndex, step.precinct}
			state := states[key]
			if state == nil {
				// The trees are sized by the code-block grid of this precinct.
				w, h := blockGridOf(band)
				state = &precinctState{
					inclusion: newTagTree(w, h),
					zeroBits:  newTagTree(w, h),
					blocks:    make([]blockState, len(band.Blocks)),
				}
				states[key] = state
			}

			for i := range band.Blocks {
				b := &state.blocks[i]
				included, err := readInclusion(r, state, b, i, step.layer)
				if err != nil {
					return 0, err
				}
				if !included {
					continue
				}

				if b.zeroBitPlanes == 0 && !b.included {
					zero, err := readZeroBitPlanes(r, state, i)
					if err != nil {
						return 0, err
					}
					b.zeroBitPlanes = zero
					b.lblock = 3
				}
				b.included = true

				passes, err := readPassCount(r)
				if err != nil {
					return 0, err
				}
				length, err := readSegmentLength(r, b, passes)
				if err != nil {
					return 0, err
				}
				b.passes += passes
				contributions = append(contributions, contribution{key, i, length})
			}
		}
	}

	r.align()
	pos += r.offset()

	// An EPH marker closes the header when the coding style says so (A.8.2).
	if style.EPH && pos+2 <= len(data) && be16(data[pos:]) == 0xFF92 {
		pos += 2
	}

	for _, contribution := range contributions {
		if pos+contribution.length > len(data) {
			return 0, errorf("packet body runs past the tile: %d bytes with %d left",
				contribution.length, len(data)-pos)
		}
		state := states[contribution.key]
		block := &state.blocks[contribution.block]
		block.data = append(block.data, data[pos:pos+contribution.length]...)
		pos += contribution.length
	}
	return pos, nil
}

// readInclusion says whether a code-block contributes to this layer (B.10.4).
// A block that has never appeared is signaled through the inclusion tag tree,
// whose value is the first layer it appears in; one that has appeared before
// takes a single bit.
func readInclusion(r *bitReader, state *precinctState, b *blockState, index, layer int) (bool, error) {
	if b.included {
		bit, err := r.readBit()
		return bit == 1, err
	}
	value, known, err := state.inclusion.decode(r, index, int32(layer)+1)
	if err != nil {
		return false, err
	}
	return known && value <= int32(layer), nil
}

// readZeroBitPlanes reads how many leading bit planes of a newly included block
// are all zero (B.10.5). The tag tree is read until the value is final, which
// is what distinguishes it from the inclusion tree's threshold read.
func readZeroBitPlanes(r *bitReader, state *precinctState, index int) (int, error) {
	for threshold := int32(1); threshold < 74; threshold++ {
		value, known, err := state.zeroBits.decode(r, index, threshold)
		if err != nil {
			return 0, err
		}
		if known {
			return int(value), nil
		}
	}
	return 0, errorf("a code-block claims more than 73 zero bit planes")
}

// readPassCount reads the number of coding passes this contribution carries
// (Table B.4). A 0 ends the code, so one pass costs one bit, and the code grows
// in three steps to 164 — every pass a 37-bit-plane block could have.
func readPassCount(r *bitReader) (int, error) {
	first, err := r.readBit()
	if err != nil {
		return 0, err
	}
	if first == 0 {
		return 1, nil
	}
	second, err := r.readBit()
	if err != nil {
		return 0, err
	}
	if second == 0 {
		return 2, nil
	}

	twoBits, err := r.readBits(2)
	if err != nil {
		return 0, err
	}
	if twoBits != 3 {
		return 3 + int(twoBits), nil
	}

	fiveBits, err := r.readBits(5)
	if err != nil {
		return 0, err
	}
	if fiveBits != 31 {
		return 6 + int(fiveBits), nil
	}

	sevenBits, err := r.readBits(7)
	if err != nil {
		return 0, err
	}
	return 37 + int(sevenBits), nil
}

// readSegmentLength reads how many bytes this contribution occupies (B.10.7).
//
// The field's width is not fixed: it starts at three bits and grows by the
// number of 1 bits that precede it, so a block whose contributions get longer
// does not pay for a wide field in every packet. The width also grows with the
// number of passes, since more passes can carry more bytes.
func readSegmentLength(r *bitReader, b *blockState, passes int) (int, error) {
	for {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		if bit == 0 {
			break
		}
		b.lblock++
		if b.lblock > 32 {
			return 0, errorf("a code-block length field grew past 32 bits")
		}
	}

	bits := b.lblock + log2(passes)
	length, err := r.readBits(bits)
	if err != nil {
		return 0, err
	}
	return int(length), nil
}

// blockGridOf is how many code-blocks a band holds across and down, which is
// the shape of its tag trees.
func blockGridOf(band Band) (int, int) {
	if len(band.Blocks) == 0 {
		return 0, 0
	}
	w := 1
	for _, block := range band.Blocks[1:] {
		if block.Y0 != band.Blocks[0].Y0 {
			break
		}
		w++
	}
	return w, (len(band.Blocks) + w - 1) / w
}

// sortedKeys lists the precinct keys in component, resolution, band, precinct
// order, so the blocks come out in the order the geometry lists them.
func sortedKeys(states map[[4]int]*precinctState) [][4]int {
	keys := make([][4]int, 0, len(states))
	for key := range states {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && less(keys[j], keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func less(a, b [4]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// ReadTilePacketsForTest exposes ReadTilePackets to tests in this package that
// need to drive it with a modified codestream.
func ReadTilePacketsForTest(c *Codestream, tile int) (*TileData, error) {
	return c.ReadTilePackets(tile)
}
