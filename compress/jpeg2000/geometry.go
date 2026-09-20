package jpeg2000

// The geometry of a tile, PS 15444-1 Annex B.
//
// A codestream's compressed bytes mean nothing without the grid they belong to:
// which samples a tile covers, how the wavelet splits them into subbands at
// each resolution, how a band is cut into code-blocks, and which precinct each
// block belongs to. Packet headers name code-blocks by their place in this
// structure, so it has to be built before a single packet can be read.
//
// All coordinates are on the reference grid (B.2): they are absolute, not
// relative to the tile, because the partitions that matter — precincts, code-
// blocks — are anchored at the origin of the grid rather than at the tile.

// Band types, in the order a packet lists them (B.10.2).
const (
	BandLL = 0
	BandHL = 1 // horizontally high-pass, vertically low-pass
	BandLH = 2
	BandHH = 3
)

// CodeBlock is one code-block's place in a band.
type CodeBlock struct {
	X0, Y0, X1, Y1 int
	// Index within the precinct, in raster order, which is the order a packet
	// header walks them in.
	Index int
}

// Width and Height of a code-block in samples.
func (b CodeBlock) Width() int  { return b.X1 - b.X0 }
func (b CodeBlock) Height() int { return b.Y1 - b.Y0 }

// Band is one subband of one resolution.
type Band struct {
	Type           int
	X0, Y0, X1, Y1 int
	// Level is the decomposition level the band came from, counting from the
	// finest: nb in the standard's formulas.
	Level  int
	Blocks []CodeBlock
}

func (b Band) Width() int  { return b.X1 - b.X0 }
func (b Band) Height() int { return b.Y1 - b.Y0 }

// Resolution is one resolution level of a tile-component: the LL band alone at
// index 0, and the three detail bands that refine it at each index after.
type Resolution struct {
	Index          int
	X0, Y0, X1, Y1 int
	Bands          []Band

	// PPx and PPy are the precinct size as powers of two, and Precincts how
	// many the resolution is divided into.
	PPx, PPy               int
	PrecinctsWide          int
	PrecinctsHigh          int
	CodeBlockW, CodeBlockH int // the exponent actually used, after precincts
}

// Precincts is how many precincts this resolution holds.
func (r Resolution) Precincts() int { return r.PrecinctsWide * r.PrecinctsHigh }

// TileComponent is one component of one tile, decomposed into resolutions.
type TileComponent struct {
	Tile           int
	Component      int
	X0, Y0, X1, Y1 int
	Resolutions    []Resolution
}

func (t TileComponent) Width() int  { return t.X1 - t.X0 }
func (t TileComponent) Height() int { return t.Y1 - t.Y0 }

// TileBounds returns the region of the reference grid a tile covers (B.3):
// the tile's own rectangle clipped to the image.
func (c *Codestream) TileBounds(tile int) (x0, y0, x1, y1 int) {
	across := ceilDiv(c.Width-c.TileXOffset, c.TileWidth)
	if across == 0 {
		return 0, 0, 0, 0
	}
	p := tile % across
	q := tile / across

	x0 = maxInt(int(c.TileXOffset)+p*int(c.TileWidth), int(c.XOffset))
	y0 = maxInt(int(c.TileYOffset)+q*int(c.TileHeight), int(c.YOffset))
	x1 = minInt(int(c.TileXOffset)+(p+1)*int(c.TileWidth), int(c.Width))
	y1 = minInt(int(c.TileYOffset)+(q+1)*int(c.TileHeight), int(c.Height))
	return x0, y0, x1, y1
}

// TileComponentFor builds the full geometry of one component of one tile.
func (c *Codestream) TileComponentFor(tile, component int) (TileComponent, error) {
	if component < 0 || component >= len(c.Components) {
		return TileComponent{}, errorf("component %d of %d", component, len(c.Components))
	}
	if tile < 0 || tile >= c.NumTiles() {
		return TileComponent{}, errorf("tile %d of %d", tile, c.NumTiles())
	}

	tx0, ty0, tx1, ty1 := c.TileBounds(tile)
	comp := c.Components[component]
	tc := TileComponent{
		Tile:      tile,
		Component: component,
		// B.3: the component's own grid, after any subsampling. This decoder
		// refuses subsampling, so the divisors are 1 and these are the tile's
		// own coordinates.
		X0: ceilDivInt(tx0, comp.DX), Y0: ceilDivInt(ty0, comp.DY),
		X1: ceilDivInt(tx1, comp.DX), Y1: ceilDivInt(ty1, comp.DY),
	}

	style := c.CodingForTile(tile, component)
	levels := style.Levels

	for r := 0; r <= levels; r++ {
		// B.5: the resolution's grid is the component's, scaled down by the
		// decompositions still to be undone.
		shift := levels - r
		res := Resolution{
			Index: r,
			X0:    ceilDivPow2(tc.X0, shift), Y0: ceilDivPow2(tc.Y0, shift),
			X1: ceilDivPow2(tc.X1, shift), Y1: ceilDivPow2(tc.Y1, shift),
			// Default precincts: the whole resolution in one, which is what
			// every fixture uses. User-defined sizes are refused in the parser.
			PPx: 15, PPy: 15,
		}

		// B.7: a code-block cannot be larger than a precinct, and at
		// resolutions above zero the precinct is halved on the way into the
		// bands, so the block exponent is one less.
		xcb, ycb := log2(style.CodeBlockWidth), log2(style.CodeBlockHt)
		if r == 0 {
			res.CodeBlockW = minInt(xcb, res.PPx)
			res.CodeBlockH = minInt(ycb, res.PPy)
		} else {
			res.CodeBlockW = minInt(xcb, res.PPx-1)
			res.CodeBlockH = minInt(ycb, res.PPy-1)
		}

		if res.X1 > res.X0 && res.Y1 > res.Y0 {
			res.PrecinctsWide = ceilDivPow2(res.X1, res.PPx) - floorDivPow2(res.X0, res.PPx)
			res.PrecinctsHigh = ceilDivPow2(res.Y1, res.PPy) - floorDivPow2(res.Y0, res.PPy)
		}

		for _, band := range bandsOf(tc, r, levels) {
			band.Blocks = codeBlocksOf(band, res.CodeBlockW, res.CodeBlockH)
			res.Bands = append(res.Bands, band)
		}
		tc.Resolutions = append(tc.Resolutions, res)
	}
	return tc, nil
}

// bandsOf gives the subbands of one resolution (B.5, Table B.1): the LL band
// alone at resolution 0, and HL, LH, HH at every resolution above it.
func bandsOf(tc TileComponent, r, levels int) []Band {
	if r == 0 {
		nb := levels
		return []Band{{
			Type: BandLL, Level: nb,
			X0: bandCoord(tc.X0, nb, 0), Y0: bandCoord(tc.Y0, nb, 0),
			X1: bandCoord(tc.X1, nb, 0), Y1: bandCoord(tc.Y1, nb, 0),
		}}
	}

	nb := levels - r + 1
	out := make([]Band, 0, 3)
	for _, t := range []struct {
		kind     int
		xob, yob int
	}{{BandHL, 1, 0}, {BandLH, 0, 1}, {BandHH, 1, 1}} {
		out = append(out, Band{
			Type: t.kind, Level: nb,
			X0: bandCoord(tc.X0, nb, t.xob), Y0: bandCoord(tc.Y0, nb, t.yob),
			X1: bandCoord(tc.X1, nb, t.xob), Y1: bandCoord(tc.Y1, nb, t.yob),
		})
	}
	return out
}

// bandCoord is the standard's band coordinate formula (B-15):
// ceil((x - 2^(nb-1) * o) / 2^nb), where o is 0 or 1 for the low or high half.
func bandCoord(x, nb, o int) int {
	if nb == 0 {
		return x
	}
	return ceilDivInt(x-(1<<(nb-1))*o, 1<<nb)
}

// codeBlocksOf cuts a band into code-blocks (B.7). The grid is anchored at the
// origin, not at the band, so the first and last blocks of a band are often
// partial — which is why a block carries its own rectangle rather than a size.
func codeBlocksOf(band Band, xcb, ycb int) []CodeBlock {
	if band.X1 <= band.X0 || band.Y1 <= band.Y0 {
		return nil
	}
	w, h := 1<<xcb, 1<<ycb
	var blocks []CodeBlock
	index := 0
	for y := floorTo(band.Y0, h); y < band.Y1; y += h {
		for x := floorTo(band.X0, w); x < band.X1; x += w {
			blocks = append(blocks, CodeBlock{
				X0: maxInt(x, band.X0), Y0: maxInt(y, band.Y0),
				X1: minInt(x+w, band.X1), Y1: minInt(y+h, band.Y1),
				Index: index,
			})
			index++
		}
	}
	return blocks
}

func ceilDivInt(a, b int) int {
	if b == 0 {
		return 0
	}
	if a >= 0 {
		return (a + b - 1) / b
	}
	// Go truncates toward zero; the standard's ceil is toward positive
	// infinity, and band coordinates can be negative before the tile offset is
	// taken off.
	return -((-a) / b)
}

func ceilDivPow2(a, shift int) int { return ceilDivInt(a, 1<<shift) }

func floorDivPow2(a, shift int) int {
	if a >= 0 {
		return a >> shift
	}
	return -((-a + (1 << shift) - 1) >> shift)
}

// floorTo rounds down to a multiple of size, which is where the partition grid
// line before x sits.
func floorTo(x, size int) int {
	if x >= 0 {
		return x - x%size
	}
	return -(((-x) + size - 1) / size) * size
}

func log2(v int) int {
	n := 0
	for v > 1 {
		v >>= 1
		n++
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
