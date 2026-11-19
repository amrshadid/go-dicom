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
	Tile int
	// Components is the tile's geometry. Its bands do not list their
	// code-blocks, which for a large image with small blocks is millions of
	// entries the packets may never name; Blocks has the ones they did, and
	// TileComponentFor lists them all.
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
//
// Only a block that has been included has one: a precinct can hold millions of
// blocks of which the file includes a few, and the cost of a block is then the
// bits that included it.
type blockState struct {
	zeroBitPlanes int
	lblock        int // bits in the length field, 3 to start (B.10.7.1)
	passes        int
	data          []byte
	// segments is kept only for a style with several codeword segments, and
	// its last entry is the one the next passes join until it is full.
	segments []CodewordSegment
}

// precinctState holds the two tag trees a precinct's packets share and the
// state of its included blocks, for one band of the precinct.
type precinctState struct {
	inclusion *tagTree
	zeroBits  *tagTree
	// indexes says which of the band's code-blocks each of the precinct's
	// blocks is, in the order its packets list them. Nil means the precinct
	// holds the whole band, in the band's order.
	indexes []int
	// w and h are the precinct's code-block grid, the shape of its tag trees.
	w, h int
	// blocks holds the blocks that have been included, by their place in the
	// precinct, and included lists the same places in raster order: each
	// takes a bit in every packet after its first (readBlocks).
	blocks   map[int]*blockState
	included []int
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
	return c.readTilePackets(tile, data)
}

// tileData concatenates every tile's parts in one pass over them. Decode
// wants all the tiles, and looking each one's parts up among all of them
// costs the number of tiles times the number of parts, which a file of a
// megabyte can make 4 billion.
func (c *Codestream) tileData() [][]byte {
	data := make([][]byte, c.NumTiles())
	for _, part := range c.TileParts {
		if part.Tile < len(data) {
			data[part.Tile] = append(data[part.Tile], part.Data...)
		}
	}
	return data
}

// readTilePackets is ReadTilePackets given the tile's data.
func (c *Codestream) readTilePackets(tile int, data []byte) (*TileData, error) {
	if len(data) == 0 {
		return nil, errorf("tile %d has no data", tile)
	}

	out := &TileData{Tile: tile}
	states := map[[4]int]*precinctState{} // component, resolution, band, precinct

	// Every precinct of every resolution has a packet in each layer, and a
	// packet is at least one byte (B.9, B.10.3), so a tile holds at least as
	// many bytes as it has precincts. This is where the header is first held
	// to the data: 244 bytes declared 185 million samples in 33 resolutions
	// over a tile of one byte (#526). A resolution with no samples has no
	// precinct and is counted as one anyway, since it costs its geometry like
	// any other. The count is kept as the components are laid out, so a header
	// that declares thousands of components and decomposition levels over a
	// few bytes is refused before their geometry is built, not after.
	//
	// It is a count and not a check that every packet is there: opj_compress
	// writes, under some progression order changes, a tile that leaves
	// packets out, and OpenJPEG decodes it from what is there.
	units := 0
	for comp := range c.Components {
		tc, err := c.tileComponent(tile, comp, false)
		if err != nil {
			return nil, err
		}
		for _, res := range tc.Resolutions {
			units += maxInt(1, res.Precincts())
		}
		if units > len(data) {
			return nil, errorf("tile %d declares more precincts than its %d bytes can hold a packet for",
				tile, len(data))
		}
		out.Components = append(out.Components, tc)
	}

	precincts := c.tilePrecincts(tile, out.Components)
	changes := c.ProgressionChangesForTile(tile)
	style := c.CodingForTile(tile, 0)
	work := &packetWork{budget: packetWorkBudget(len(data))}
	pos := 0
	err := forEachPacket(style, changes, precincts, len(data), func(step packetStep) (bool, error) {
		if pos >= len(data) {
			// Layers may simply stop: an encoder writes as many as it wrote,
			// and the rest of the progression is empty. This is also what
			// bounds the walk: every packet read takes at least a byte.
			return false, nil
		}
		next, err := c.readPacket(data, pos, tile, step, states, out, work)
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
		resInfo := tc.Resolutions[res]
		blockStyle := c.CodingForTile(tile, comp).BlockStyle
		for _, i := range state.included {
			b := state.blocks[i]
			if len(b.data) == 0 {
				continue
			}
			out.Blocks = append(out.Blocks, CodeBlockData{
				Block:         codeBlockAt(bandInfo, resInfo.CodeBlockW, resInfo.CodeBlockH, state.bandBlock(i)),
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
// ReadTilePackets has held the count of precincts to the tile's bytes before
// this list is made.
func (c *Codestream) tilePrecincts(tile int, comps []TileComponent) []tilePrecinct {
	total, resolutions := 0, 0
	for _, tc := range comps {
		for _, res := range tc.Resolutions {
			total += res.Precincts()
		}
		resolutions = maxInt(resolutions, len(tc.Resolutions))
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
	return precincts
}

// packetWork counts the steps readBlocks takes through a tile's packet
// headers, against a budget the tile's size sets.
//
// Each step reads a bit, or steps over code-blocks a tag tree node has
// answered for, and such a node is under one whose value cost a bit. So the
// steps follow the bits, and the budget is a guard on that reasoning rather
// than a limit a file meets: the most any file in the corpus or the OpenJPEG
// matrix takes is under 3 steps a byte, and black images of 16384 by 16384
// that opj_compress wrote with 16x16 blocks in ten layers, or 4096 by 4096
// with 4x4 blocks, take one step a byte. Visiting every block of every
// precinct in every packet, as the reader did before, took 14 million steps
// for the first of those.
type packetWork struct {
	spent, budget int
}

// packetWorkBudget is the steps a tile of tileBytes may take.
func packetWorkBudget(tileBytes int) int {
	return 1<<10 + 64*tileBytes
}

// spend charges n steps, and refuses once the budget is gone.
func (w *packetWork) spend(n, tile int) error {
	if w.spent += n; w.spent > w.budget {
		return errorf("tile %d takes more than %d steps to read its packet headers, too many for its size",
			tile, w.budget)
	}
	return nil
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
	states map[[4]int]*precinctState, out *TileData, work *packetWork) (int, error) {

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
					indexes:   indexes,
					w:         w,
					h:         h,
					blocks:    map[int]*blockState{},
				}
				states[key] = state
			}

			// readBlock reads the rest of what the header says about block i,
			// once it is known to contribute to this layer.
			readBlock := func(i int) error {
				b := state.blocks[i]
				if b == nil {
					zero, err := readZeroBitPlanes(r, state, i)
					if err != nil {
						return err
					}
					b = &blockState{zeroBitPlanes: zero, lblock: 3}
					state.blocks[i] = b
				}
				passes, err := readPassCount(r)
				if err != nil {
					return err
				}
				if style.BlockStyle&(BlockStyleBypass|BlockStyleTermAll|BlockStyleHT) == 0 {
					length, err := readSegmentLength(r, b, passes)
					if err != nil {
						return err
					}
					b.passes += passes
					contributions = append(contributions, contribution{key, i, length})
					return nil
				}
				lengths, err := readSegmentLengths(r, b, passes, style.BlockStyle)
				if err != nil {
					return err
				}
				b.passes += passes
				for _, length := range lengths {
					contributions = append(contributions, contribution{key, i, length})
				}
				return nil
			}
			// A band with no code-blocks in this precinct has nothing in its
			// packets (B.10).
			if err := state.readBlocks(r, step.layer, tile, work, readBlock); err != nil {
				return 0, err
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
		block := states[contribution.key].blocks[contribution.block]
		block.data = append(block.data, data[pos:pos+contribution.length]...)
		pos += contribution.length
	}
	return pos, nil
}

// readBlocks reads, for each code-block of the precinct's band in raster
// order, whether it contributes to this layer (B.10.4), and calls readBlock
// for each one that does.
//
// A block that has appeared before takes a single bit. One that has not is
// signaled through the inclusion tag tree, whose value is the first layer it
// appears in, and when a node of the tree is at the layer's threshold every
// block under it is answered "not yet" without a bit (decodeLeaf). Those are
// stepped over rather than asked one by one: across the node's columns in
// this row, and when a whole row was answered so, down to the first row the
// nodes do not cover. The bits read are the same, in the same order, as
// asking every block; what changes is that the work is the bits and the
// blocks that have appeared, not the size of the precinct. A precinct of
// default size over 4x4 code-blocks is millions of blocks, and before this a
// header of a byte had each one visited, in every layer.
func (p *precinctState) readBlocks(r *bitReader, layer, tile int, work *packetWork,
	readBlock func(i int) error) error {

	w, h := p.w, p.h
	if w <= 0 || h <= 0 {
		return nil
	}
	threshold := int32(layer) + 1
	before := p.included // blocks that appeared in an earlier layer, in raster order
	next := 0            // the first of them not yet passed
	var newly []int

	for y := 0; y < h; {
		skipTo := h // the first row the nodes stepped over in this row do not cover
		stepped := true
		for x := 0; x < w; {
			if err := work.spend(1, tile); err != nil {
				return err
			}
			i := y*w + x
			if next < len(before) && before[next] == i {
				bit, err := r.readBit()
				if err != nil {
					return err
				}
				if bit == 1 {
					if err := readBlock(i); err != nil {
						return err
					}
				}
				next++
				x++
				stepped = false
				continue
			}
			value, known, stop, err := p.inclusion.decodeLeaf(r, i, threshold)
			if err != nil {
				return err
			}
			if known && value <= int32(layer) {
				if err := readBlock(i); err != nil {
					return err
				}
				newly = append(newly, i)
				x++
				stepped = false
				continue
			}
			if stop <= 0 {
				// Settled at the leaf itself: it says nothing about the next.
				x++
				stepped = false
				continue
			}
			// Every block under the node at level stop is "not yet": step to
			// the node's right edge. No block that appeared before is under
			// it. Such a block's leaf and every node above it are known, at
			// values no higher than the layer it appeared in, and a known node
			// is never the one a read stops at.
			x = min((x>>stop+1)<<stop, w)
			skipTo = min(skipTo, (y>>stop+1)<<stop)
		}
		if !stepped {
			y++
			continue
		}
		// The whole row was stepped over, so the rows below it are too, as far
		// as the nodes reach.
		y = max(skipTo, y+1)
	}

	if len(newly) > 0 {
		p.included = mergeSorted(before, newly)
	}
	return nil
}

// mergeSorted merges two ascending lists into a new one.
func mergeSorted(a, b []int) []int {
	out := make([]int, 0, len(a)+len(b))
	for len(a) > 0 && len(b) > 0 {
		if a[0] < b[0] {
			out, a = append(out, a[0]), a[1:]
		} else {
			out, b = append(out, b[0]), b[1:]
		}
	}
	return append(append(out, a...), b...)
}

// readZeroBitPlanes reads how many leading bit planes of a newly included block
// are all zero (B.10.5). The tag tree is read until the value is final, which
// is what distinguishes it from the inclusion tree's threshold read.
//
// The tree is made when the precinct's first block is included, since a
// precinct none of whose blocks appear never reads it.
func readZeroBitPlanes(r *bitReader, state *precinctState, index int) (int, error) {
	if state.zeroBits == nil {
		state.zeroBits = newTagTree(state.w, state.h)
	}
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
