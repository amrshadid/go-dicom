package jpeg2000

import (
	"math"
	"sort"
)

// Rate control by truncation, PS 15444-1 Annex J.14: post-compression
// rate-distortion optimization, the method of EBCOT.
//
// Every code-block is coded once, finely, and each of its coding passes is a
// place its data may be cut, with the bytes a decoder needs to read that far
// and the squared error those passes remove (encodeBlockTruncations). Of
// those places, the ones worth stopping at lie on the lower convex hull of
// error against bytes: between two hull points the error falls at a slope
// that only falls further along the block. A rate is then met by one slope
// for the whole image: every block is cut at the last hull point whose slope
// is at least that steep, which is where a byte spent in any block buys as
// much error as it would in any other, and the slope is searched for until
// the codestream fits. Quality layers are the same with a steeper slope for
// each earlier layer, so that each layer adds to the cuts of the one before.

// hullPoint is a truncation point on a block's convex hull, with the slope
// of the hull's segment that ends at it: error removed per byte added.
type hullPoint struct {
	truncationPoint
	slope float64
}

// convexHull keeps the truncation points that lie on the lower convex hull
// of error against bytes, with their slopes, which strictly fall from one to
// the next (J.14.1). gained is the error removed, so error is the total less
// it, and a point removes at least as much as any before it.
//
// A point that adds no bytes, or removes no error, or removes it at a slope
// steeper than the point before it allows, is never where an optimal cut
// stops: the segment past it is steeper, so the cut moves on.
func convexHull(points []truncationPoint, weight float64) []hullPoint {
	hull := make([]hullPoint, 0, len(points))
	for _, p := range points {
		for {
			var bytes int
			var gained float64
			if n := len(hull); n > 0 {
				bytes, gained = hull[n-1].bytes, hull[n-1].gained
			}
			dBytes, dGain := p.bytes-bytes, (p.gained-gained)*weight
			if dGain <= 0 {
				break // removes nothing more: never worth stopping at
			}
			if dBytes <= 0 {
				// More error removed for no byte: whatever stopped before it
				// should not have. Drop the earlier point and look again, or
				// with none before it, keep this one whatever the slope.
				if len(hull) == 0 {
					hull = append(hull, hullPoint{truncationPoint: p, slope: math.Inf(1)})
					break
				}
				hull = hull[:len(hull)-1]
				continue
			}
			slope := dGain / float64(dBytes)
			if n := len(hull); n > 0 && slope >= hull[n-1].slope {
				// The segment to it is steeper than the one before: the
				// earlier point is under the line and not on the hull.
				hull = hull[:n-1]
				continue
			}
			hull = append(hull, hullPoint{truncationPoint: p, slope: slope})
			break
		}
	}
	return hull
}

// cutAt is the truncation a slope threshold gives a block: the last hull
// point whose slope is at least lambda, or none.
func cutAt(hull []hullPoint, lambda float64) truncationPoint {
	// The slopes fall, so the points that qualify are a prefix.
	n := sort.Search(len(hull), func(i int) bool { return hull[i].slope < lambda })
	if n == 0 {
		return truncationPoint{}
	}
	return hull[n-1].truncationPoint
}

// slopeRange is the steepest and shallowest slopes of a set of hulls, the
// interval a threshold is searched in.
func slopeRange(hulls [][]hullPoint) (steepest, shallowest float64) {
	steepest, shallowest = 0, math.Inf(1)
	for _, hull := range hulls {
		for _, p := range hull {
			if math.IsInf(p.slope, 1) {
				continue // kept at any threshold
			}
			steepest = math.Max(steepest, p.slope)
			shallowest = math.Min(shallowest, p.slope)
		}
	}
	return steepest, shallowest
}

// layeredBlock is a code-block of a layered codestream: its data, coded once,
// and how far into it each layer reaches.
type layeredBlock struct {
	EncodedBlock
	zeroBitPlanes int
	// cuts[l] is the truncation at the end of layer l. A later layer never
	// reaches less far than an earlier one.
	cuts []truncationPoint

	// The block's shape and band, and the band's magnitude bits, which a
	// decoder needs to read it back (trimCuts).
	w, h, bandType, magnitudeBits int

	// weight turns the block's error in steps into the image's units, and
	// energy is the error with nothing of it kept, in those units.
	weight, energy float64
	// gainedWhole is the error every pass removes, in steps.
	gainedWhole float64
}

// remaining is the mean square error per sample the cuts of the last layer
// leave, as the coding measures it: each block's energy less what its cut
// takes away.
func (c *truncatedCode) remaining() float64 {
	var sum float64
	for _, lb := range c.order {
		sum += lb.energy
		if n := len(lb.cuts); n > 0 {
			sum -= lb.cuts[n-1].gained * lb.weight
		}
	}
	return math.Max(0, sum) / float64(max(1, c.samples))
}

// trimCuts gives back the bytes a truncation point kept beyond what a
// decoder reads. A point keeps truncationSlack bytes past those the MQ
// coder had written, which is safe and wasteful: three bytes in every block
// cut short, a tenth of the budget of a small image. The bytes a decoder
// actually reads to decode a block's first passes are known by decoding
// them, so each cut is shortened to those, and kept only when the shortened
// data decodes to the same coefficients as the whole. It reports whether
// any cut changed.
func trimCuts(blocks []*layeredBlock) bool {
	changed := false
	for _, lb := range blocks {
		prev, prevPasses := 0, 0
		for l := range lb.cuts {
			cut := &lb.cuts[l]
			// A layer that adds no pass adds no byte either: a packet can
			// say a block has nothing in a layer, and not that it has bytes
			// and no passes.
			if l > 0 && cut.passes == prevPasses {
				if cut.bytes != prev {
					cut.bytes = prev
					changed = true
				}
				continue
			}
			prevPasses = cut.passes
			if cut.passes == 0 || cut.bytes >= len(lb.Data) {
				prev = cut.bytes
				continue
			}
			block := CodeBlockData{
				Block:         CodeBlock{X1: lb.w, Y1: lb.h},
				BandType:      lb.bandType,
				ZeroBitPlanes: lb.zeroBitPlanes,
				Passes:        cut.passes,
				Data:          lb.Data,
			}
			// Shorter by a byte at a time while the passes still decode the
			// same: the decoder reads bytes ahead of the symbols it decides,
			// and the last of them often do not change any decision.
			for short := cut.bytes - 1; short > prev && short >= cut.bytes-truncationSlack-2; short-- {
				if !sameDecode(block, lb.magnitudeBits, short) {
					break
				}
				cut.bytes = short
				changed = true
			}
			prev = cut.bytes
		}
	}
	return changed
}

// sameDecode reports whether block decodes to the same coefficients from its
// first n bytes as from all of them.
func sameDecode(block CodeBlockData, magnitudeBits, n int) bool {
	whole, err := DecodeBlock(block, magnitudeBits)
	if err != nil {
		return false
	}
	block.Data = block.Data[:n]
	cut, err := DecodeBlock(block, magnitudeBits)
	if err != nil || len(cut) != len(whole) {
		return false
	}
	for i := range whole {
		if whole[i] != cut[i] {
			return false
		}
	}
	return true
}

// firstLayer is the first layer the block contributes to, or layers when it
// contributes to none.
func (b *layeredBlock) firstLayer(layers int) int {
	for l := 0; l < layers; l++ {
		if b.cuts[l].passes > 0 {
			return l
		}
	}
	return layers
}

// layerState is what a packet header has said about the blocks of one band
// so far: the encoder's half of the decoder's precinctState.
type layerState struct {
	inclusion, zeroBits *tagTreeEncoder
	included            []bool
	lblock              []int
}

// writeLayerPacket writes the packet of one layer of one resolution of one
// component: for each block, whether it contributes, and if so its passes and
// bytes from where the layer before stopped to where this one does (B.10).
// states holds each band's header state across layers, made on first use.
func writeLayerPacket(res Resolution, bands [][]*layeredBlock, layer, layers int, states []*layerState) []byte {
	header := &headerWriter{}
	var body []byte

	contributes := func(b *layeredBlock) bool {
		prev := 0
		if layer > 0 {
			prev = b.cuts[layer-1].passes
		}
		return b.cuts[layer].passes > prev
	}
	empty := true
	for _, blocks := range bands {
		for _, b := range blocks {
			if contributes(b) {
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
		st := states[bandIndex]
		if st == nil {
			w, h := blockGridOf(band)
			first := make([]int32, len(blocks))
			zero := make([]int32, len(blocks))
			for i, b := range blocks {
				first[i] = int32(b.firstLayer(layers))
				zero[i] = int32(b.zeroBitPlanes)
			}
			st = &layerState{
				inclusion: newTagTreeEncoder(w, h, first),
				zeroBits:  newTagTreeEncoder(w, h, zero),
				included:  make([]bool, len(blocks)),
				lblock:    make([]int, len(blocks)),
			}
			states[bandIndex] = st
		}
		for i, b := range blocks {
			if !st.included[i] {
				// The inclusion tree's value is the first layer the block is
				// in; asked about this layer, a reader learns whether it is.
				st.inclusion.encode(header, i, int32(layer)+1)
				if !contributes(b) {
					continue
				}
				st.zeroBits.encode(header, i, int32(b.zeroBitPlanes)+1)
				st.included[i] = true
				st.lblock[i] = initialLblock
			} else {
				if !contributes(b) {
					header.writeBit(0)
					continue
				}
				header.writeBit(1)
			}
			var prev truncationPoint
			if layer > 0 {
				prev = b.cuts[layer-1]
			}
			passes := b.cuts[layer].passes - prev.passes
			length := b.cuts[layer].bytes - prev.bytes
			writePassCount(header, passes)
			writeLayerSegmentLength(header, length, passes, &st.lblock[i])
			body = append(body, b.Data[prev.bytes:b.cuts[layer].bytes]...)
		}
	}
	return append(header.bytes(), body...)
}

// writeLayerSegmentLength is writeSegmentLength for a block that contributes
// to several layers: Lblock carries over from one contribution to the next,
// and only grows (B.10.7.1).
func writeLayerSegmentLength(w *headerWriter, length, passes int, lblock *int) {
	for length >= 1<<uint(*lblock+log2(passes)) {
		w.writeBit(1)
		*lblock++
	}
	w.writeBit(0)
	w.writeBits(uint32(length), *lblock+log2(passes))
}
