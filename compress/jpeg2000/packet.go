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

import (
	"fmt"
	"slices"
)

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

	// Style is the code-block style the block was coded with (Table A.19),
	// the BlockStyle bits.
	Style byte
	// Segments divides Data into the codeword segments the encoder
	// terminated (D.4.1), in order. It is set only when the style makes
	// more than one: with bypass or termination on each pass. Empty means
	// all of Data is one segment holding every pass.
	Segments []CodewordSegment
}

// CodewordSegment is one terminated run of a code-block's coding passes: how
// many passes it holds and how many bytes of the block's data they occupy.
type CodewordSegment struct {
	Passes int
	Length int
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
	// segments is kept only for a style with several codeword segments, and
	// its last entry is the one the next passes join until it is full.
	segments []CodewordSegment
}

// precinctState holds the two tag trees a precinct's packets share and the
// per-block state, for one band of the precinct.
type precinctState struct {
	inclusion *tagTree
	zeroBits  *tagTree
	// indexes says which of the band's code-blocks each entry of blocks is:
	// the precinct's own blocks, in the order its packets list them. Nil
	// means the precinct holds the whole band, in the band's order.
	indexes []int
	blocks  []blockState
}

// bandBlock is which of the band's code-blocks entry i of the precinct is.
func (p *precinctState) bandBlock(i int) int {
	if p.indexes == nil {
		return i
	}
	return p.indexes[i]
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

	for comp := range c.Components {
		tc, err := c.TileComponentFor(tile, comp)
		if err != nil {
			return nil, err
		}
		out.Components = append(out.Components, tc)
	}

	precincts, err := c.tilePrecincts(tile, out.Components, len(data))
	if err != nil {
		return nil, err
	}
	style := c.CodingForTile(tile, 0)
	pos := 0
	err = forEachPacket(style, c.ProgressionChangesForTile(tile), precincts, len(data), func(step packetStep) (bool, error) {
		if pos >= len(data) {
			// Layers may simply stop: an encoder writes as many as it wrote,
			// and the rest of the progression is empty. This is also what
			// bounds the walk: every packet read takes at least a byte.
			return false, nil
		}
		next, err := c.readPacket(data, pos, tile, step, states, out)
		if err != nil {
			return false, err
		}
		pos = next
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	out.BytesRead = pos
	out.TileBytes = len(data)

	// The blocks in geometry order, which is the order tier-1 wants them.
	for _, key := range sortedKeys(states) {
		state := states[key]
		comp, res, band, _ := key[0], key[1], key[2], key[3]
		tc := out.Components[comp]
		bandInfo := tc.Resolutions[res].Bands[band]
		blockStyle := c.CodingForTile(tile, comp).BlockStyle
		for i := range state.blocks {
			b := &state.blocks[i]
			if !b.included || len(b.data) == 0 {
				continue
			}
			out.Blocks = append(out.Blocks, CodeBlockData{
				Block:         bandInfo.Blocks[state.bandBlock(i)],
				BandType:      bandInfo.Type,
				Resolution:    res,
				Component:     comp,
				ZeroBitPlanes: b.zeroBitPlanes,
				Passes:        b.passes,
				Data:          b.data,
				Style:         blockStyle,
				Segments:      b.segments,
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

// tilePrecinct is one precinct of one resolution of one component, and the
// point of the reference grid where the position-driven progressions meet it.
type tilePrecinct struct {
	component, resolution, precinct int
	x, y                            int64
	index                           int // its place in tilePrecincts' list
}

// tilePrecincts lists every precinct of a tile, resolution by resolution, and
// within a resolution component by component in raster order: the order LRCP
// and RLCP visit them in.
//
// The position-driven orders (B.12.1.3 to B.12.1.5) step across the tile's
// reference grid and take a component's precinct at each point that is a
// multiple of the precinct's size there, 2^(PPx + NL - r) times the
// component's subsampling, or at the tile's own edge when the first precinct
// starts before it. That point is where the precinct's left and top edges are
// on the reference grid, moved in to the tile's edge for the first one, which
// is what x and y hold; sorting by them is the standard's loop without
// visiting every point of the grid to find the few that start a precinct.
//
// A complete codestream holds at least one packet of at least one byte for
// every precinct, so a tile that declares far more precincts than it has bytes
// is refused here, before a list of them is allocated. The slack is for a
// codestream truncated after its first layers, which is legal.
func (c *Codestream) tilePrecincts(tile int, comps []TileComponent, tileBytes int) ([]tilePrecinct, error) {
	total, resolutions := int64(0), 0
	for _, tc := range comps {
		for _, res := range tc.Resolutions {
			total += int64(res.PrecinctsWide) * int64(res.PrecinctsHigh)
		}
		resolutions = maxInt(resolutions, len(tc.Resolutions))
	}
	if limit := int64(tileBytes) + 1<<16; total > limit {
		return nil, errorf("tile %d declares %d precincts in %d bytes", tile, total, tileBytes)
	}

	tx0, ty0, _, _ := c.TileBounds(tile)
	precincts := make([]tilePrecinct, 0, total)
	for r := 0; r < resolutions; r++ {
		for comp, tc := range comps {
			if r >= len(tc.Resolutions) {
				continue // this component has fewer resolutions than another
			}
			res := tc.Resolutions[r]
			// The precinct's size on the reference grid, as a shift and the
			// component's subsampling.
			shift := res.PPx + len(tc.Resolutions) - 1 - r
			shiftY := res.PPy + len(tc.Resolutions) - 1 - r
			dx, dy := int64(c.Components[comp].DX), int64(c.Components[comp].DY)
			firstX, firstY := floorDivPow2(res.X0, res.PPx), floorDivPow2(res.Y0, res.PPy)
			for k := 0; k < res.Precincts(); k++ {
				x := int64(firstX+k%res.PrecinctsWide) << shift * dx
				y := int64(firstY+k/res.PrecinctsWide) << shiftY * dy
				precincts = append(precincts, tilePrecinct{
					component: comp, resolution: r, precinct: k,
					x: max(x, int64(tx0)), y: max(y, int64(ty0)),
					index: len(precincts),
				})
			}
		}
	}
	return precincts, nil
}

// forEachPacket calls visit for each packet of a tile in the order the coding
// style's progression writes them (B.12.1), or the order its progression
// order changes give (B.12.2), until visit returns false or an error.
// precincts is tilePrecincts' list, and tileBytes the tile's length.
func forEachPacket(style CodingStyle, changes []ProgressionChange, precincts []tilePrecinct,
	tileBytes int, visit func(packetStep) (bool, error)) error {

	step := func(p tilePrecinct, layer int) packetStep {
		return packetStep{layer: layer, resolution: p.resolution, component: p.component, precinct: p.precinct}
	}
	if len(changes) == 0 {
		_, err := walkVolume(style.Progression, style.Layers, precincts, func(p tilePrecinct, l int) (bool, error) {
			return visit(step(p, l))
		})
		return err
	}

	// Each volume is a range of resolutions, components and layers in its own
	// order, and a packet an earlier volume carried is not carried again.
	// Every volume walks a precinct's layers upwards from 0, so the layers a
	// precinct has given are always 0 to some n, and one count per precinct
	// says which packets are done.
	//
	// Skipping is not free, and a POC can name thousands of volumes that
	// repeat each other without consuming a byte: the work of skipping and of
	// picking out each volume's precincts is bounded by the tile's size, far
	// above what any real progression needs.
	done := make([]int, len(precincts))
	work, budget := 0, 64*(tileBytes+len(precincts))+1<<20
	for _, change := range changes {
		if work += len(precincts); work > budget {
			return errorf("a tile's progression order changes name more than %d precincts", budget)
		}
		var volume []tilePrecinct
		for _, p := range precincts {
			if p.resolution >= change.ResolutionStart && p.resolution < change.ResolutionEnd &&
				p.component >= change.ComponentStart && p.component < change.ComponentEnd {
				volume = append(volume, p)
			}
		}
		more, err := walkVolume(change.Progression, min(change.LayerEnd, style.Layers), volume,
			func(p tilePrecinct, l int) (bool, error) {
				if l < done[p.index] {
					if work++; work > budget {
						return false, errorf("a tile's progression order changes revisit more than %d packets", budget)
					}
					return true, nil
				}
				done[p.index] = l + 1
				return visit(step(p, l))
			})
		if !more || err != nil {
			return err
		}
	}
	return nil
}

// walkVolume calls emit for each packet of a volume of precincts in one
// progression order (B.12.1), with layers 0 to layers-1, until emit returns
// false or an error. It reports whether emit asked to go on.
//
// Each order is the list in some order, with the layer loop where the order
// puts it: outermost for LRCP, second for RLCP, innermost for the three
// position-driven ones, whose precincts are sorted by where on the grid the
// standard's loops meet them.
func walkVolume(progression, layers int, precincts []tilePrecinct, emit func(tilePrecinct, int) (bool, error)) (bool, error) {
	switch progression {
	case ProgressionLRCP:
		for l := 0; l < layers; l++ {
			for _, p := range precincts {
				if more, err := emit(p, l); !more || err != nil {
					return more, err
				}
			}
		}
		return true, nil

	case ProgressionRLCP:
		// The list is in resolution order, so each resolution is a run of it.
		for start := 0; start < len(precincts); {
			end := start
			for end < len(precincts) && precincts[end].resolution == precincts[start].resolution {
				end++
			}
			for l := 0; l < layers; l++ {
				for _, p := range precincts[start:end] {
					if more, err := emit(p, l); !more || err != nil {
						return more, err
					}
				}
			}
			start = end
		}
		return true, nil
	}

	order := slices.Clone(precincts)
	var key func(p tilePrecinct) [4]int64
	switch progression {
	case ProgressionRPCL:
		key = func(p tilePrecinct) [4]int64 { return [4]int64{int64(p.resolution), p.y, p.x, int64(p.component)} }
	case ProgressionPCRL:
		key = func(p tilePrecinct) [4]int64 { return [4]int64{p.y, p.x, int64(p.component), int64(p.resolution)} }
	case ProgressionCPRL:
		key = func(p tilePrecinct) [4]int64 { return [4]int64{int64(p.component), p.y, p.x, int64(p.resolution)} }
	default:
		return false, &ErrUnsupported{Feature: fmt.Sprintf("progression order %d", progression)}
	}
	slices.SortStableFunc(order, func(a, b tilePrecinct) int {
		ka, kb := key(a), key(b)
		return slices.Compare(ka[:], kb[:])
	})
	for _, p := range order {
		for l := 0; l < layers; l++ {
			if more, err := emit(p, l); !more || err != nil {
				return more, err
			}
		}
	}
	return true, nil
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
			key := [4]int{step.component, step.resolution, bandIndex, step.precinct}
			state := states[key]
			if state == nil {
				// The trees are sized by the code-block grid of this precinct,
				// which is the whole band only with the default precincts.
				indexes, w, h := precinctBlocks(res, band, step.precinct)
				state = &precinctState{
					inclusion: newTagTree(w, h),
					zeroBits:  newTagTree(w, h),
					indexes:   indexes,
					blocks:    make([]blockState, w*h),
				}
				states[key] = state
			}

			// A band with no code-blocks in this precinct has nothing in its
			// packets (B.10).
			for i := range state.blocks {
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
				if style.BlockStyle&(BlockStyleBypass|BlockStyleTermAll|BlockStyleHT) == 0 {
					length, err := readSegmentLength(r, b, passes)
					if err != nil {
						return 0, err
					}
					b.passes += passes
					contributions = append(contributions, contribution{key, i, length})
					continue
				}
				lengths, err := readSegmentLengths(r, b, passes, style.BlockStyle)
				if err != nil {
					return 0, err
				}
				b.passes += passes
				for _, length := range lengths {
					contributions = append(contributions, contribution{key, i, length})
				}
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

// readSegmentLengths reads the lengths of a contribution whose passes may fall
// in several codeword segments, one length for each (B.10.7.2), and records
// the segments on the block.
//
// The field's width grows with the 1 bits before it, as in readSegmentLength,
// but once for the contribution; each segment's length then takes that many
// bits plus the floor of log2 of the passes it holds. Which passes share a
// segment follows from the style alone. Termination on each pass makes every
// pass its own segment. Bypass makes the first ten one MQ-coded segment (the
// four most significant bit planes, D.6), and then alternates a raw segment of
// a significance and a refinement pass with an MQ-coded one of a cleanup pass.
func readSegmentLengths(r *bitReader, b *blockState, passes int, blockStyle byte) ([]int, error) {
	for {
		bit, err := r.readBit()
		if err != nil {
			return nil, err
		}
		if bit == 0 {
			break
		}
		b.lblock++
		if b.lblock > 32 {
			return nil, errorf("a code-block length field grew past 32 bits")
		}
	}

	var lengths []int
	for passes > 0 {
		last := len(b.segments) - 1
		if last < 0 || b.segments[last].Passes == segmentCapacity(blockStyle, last) {
			b.segments = append(b.segments, CodewordSegment{})
			last++
		}
		n := min(segmentCapacity(blockStyle, last)-b.segments[last].Passes, passes)
		length, err := r.readBits(b.lblock + log2(n))
		if err != nil {
			return nil, err
		}
		b.segments[last].Passes += n
		b.segments[last].Length += int(length)
		lengths = append(lengths, int(length))
		passes -= n
	}
	return lengths, nil
}

// segmentCapacity is how many passes a block's codeword segment number index
// holds, under a style with bypass or termination on each pass.
//
// HT code-blocks (T.814 B.2, B.3) have segments of their own: an HT cleanup
// pass alone, then an HT refinement segment of a SigProp and a MagRef pass,
// and so on, which is the bypass pattern with a first segment of one pass.
// That is the set T for no placeholder passes; a block with placeholder
// passes is refused when it is decoded (htSegmentsOf).
func segmentCapacity(blockStyle byte, index int) int {
	switch {
	case blockStyle&BlockStyleHT != 0:
		if index%2 == 1 {
			return 2
		}
		return 1
	case blockStyle&BlockStyleTermAll != 0:
		return 1
	case index == 0:
		return 10
	case index%2 == 1:
		return 2 // a raw significance and refinement pass
	default:
		return 1 // an MQ-coded cleanup pass
	}
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
// order, so the blocks come out in the same order whatever order the map
// gives them in. A tile with user-defined precincts can have thousands, so
// this is a real sort and not the insertion sort a handful of keys allowed.
func sortedKeys(states map[[4]int]*precinctState) [][4]int {
	keys := make([][4]int, 0, len(states))
	for key := range states {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b [4]int) int { return slices.Compare(a[:], b[:]) })
	return keys
}

// ReadTilePacketsForTest exposes ReadTilePackets to tests in this package that
// need to drive it with a modified codestream.
func ReadTilePacketsForTest(c *Codestream, tile int) (*TileData, error) {
	return c.ReadTilePackets(tile)
}
