package jpeg2000

import (
	"math"
	"runtime"
	"sync"
)

// Decoding a codestream to samples, PS 15444-1 Annexes E and G.
//
// This is where the stages meet. Tier-2 said which bytes belong to which
// code-block, tier-1 turned those bytes into coefficients, and Annex F put the
// subbands back together; what is left is the arithmetic that surrounds them.
// Dequantization (E.1) turns a quantized integer into the value it stands for.
// The component transform (G.2, G.3) undoes the decorrelation an encoder
// applies across color channels. The DC level shift (G.1.2) moves samples
// back from the signed range they were coded in to the unsigned one a picture
// is stored in.

// Image is a decoded codestream.
type Image struct {
	Width, Height int
	// Components holds one plane per component, in raster order, each already
	// level shifted and clamped to the component's declared depth. A plane is
	// Width by Height samples unless its component is subsampled.
	Components [][]int32
	Depths     []int
	Signed     []bool

	// ComponentWidths and ComponentHeights give each plane's size when a
	// component is subsampled (B.2): the samples it has on the reference
	// grid, ceil(Xsiz/XRsiz) - ceil(XOsiz/XRsiz) across. Nil means every
	// plane is the image's size, which is all an encoder here takes.
	ComponentWidths, ComponentHeights []int
}

// ComponentSize is the width and height of a component's plane.
func (img *Image) ComponentSize(c int) (width, height int) {
	if c < len(img.ComponentWidths) && c < len(img.ComponentHeights) {
		return img.ComponentWidths[c], img.ComponentHeights[c]
	}
	return img.Width, img.Height
}

// Subsampled reports whether any component's plane is smaller than the image.
func (img *Image) Subsampled() bool {
	for c := range img.Components {
		if w, h := img.ComponentSize(c); w != img.Width || h != img.Height {
			return true
		}
	}
	return false
}

// componentBounds is where a component's samples lie on its own grid (B.2):
// the image's rectangle divided by its separation, rounded up at both ends.
func (c *Codestream) componentBounds(comp int) (x0, y0, x1, y1 int) {
	dx, dy := c.Components[comp].DX, c.Components[comp].DY
	return ceilDivInt(int(c.XOffset), dx), ceilDivInt(int(c.YOffset), dy),
		ceilDivInt(int(c.Width), dx), ceilDivInt(int(c.Height), dy)
}

// Decode decodes a whole codestream to samples.
//
// Every tile is decoded independently and written into the image at the place
// its coordinates give, which is what makes a tiled file work without any
// special handling: a tile knows where it belongs.
func Decode(c *Codestream) (*Image, error) {
	if c == nil {
		return nil, errorf("no codestream")
	}
	if err := checkImageSize(c); err != nil {
		return nil, err
	}
	width := int(c.Width) - int(c.XOffset)
	height := int(c.Height) - int(c.YOffset)

	img := &Image{Width: width, Height: height}
	subsampled := false
	for _, comp := range c.Components {
		subsampled = subsampled || comp.DX != 1 || comp.DY != 1
	}
	for i, comp := range c.Components {
		w, h := width, height
		if subsampled {
			x0, y0, x1, y1 := c.componentBounds(i)
			w, h = x1-x0, y1-y0
			img.ComponentWidths = append(img.ComponentWidths, w)
			img.ComponentHeights = append(img.ComponentHeights, h)
		}
		img.Components = append(img.Components, make([]int32, w*h))
		img.Depths = append(img.Depths, comp.Depth)
		img.Signed = append(img.Signed, comp.Signed)
	}

	for tile := 0; tile < c.NumTiles(); tile++ {
		if err := c.decodeTile(tile, img); err != nil {
			return nil, err
		}
	}
	return img, nil
}

// checkImageSize refuses an image too large to decode. The parser applies it
// as well as Decode, because the packet reader sizes its precincts from the
// same dimensions.
func checkImageSize(c *Codestream) error {
	width := int(c.Width) - int(c.XOffset)
	height := int(c.Height) - int(c.YOffset)
	if width <= 0 || height <= 0 {
		return errorf("an image of %dx%d samples", width, height)
	}
	// The product in 64 bits: 65536 squared is 0 in a 32-bit int (#258).
	if width > 1<<16 || height > 1<<16 || int64(width)*int64(height) > 1<<28 {
		return errorf("an image of %dx%d samples is larger than this decoder accepts",
			width, height)
	}
	return nil
}

// decodeTile decodes one tile and writes its samples into the image.
func (c *Codestream) decodeTile(tile int, img *Image) error {
	data, err := c.ReadTilePackets(tile)
	if err != nil {
		return err
	}

	planes := make([][]float32, len(c.Components))
	for comp := range c.Components {
		tc := data.Components[comp]
		style := c.CodingForTile(tile, comp)
		quant := c.QuantForTile(tile, comp)
		reversible := style.Wavelet == Wavelet53Reversible

		bands, err := c.subbandsOf(tile, comp, tc, quant, style, data.Blocks)
		if err != nil {
			return err
		}
		samples, err := inverseDWT(tc, bands, reversible)
		if err != nil {
			return err
		}
		planes[comp] = samples
	}

	style := c.CodingForTile(tile, 0)
	if style.MCT {
		if err := inverseComponentTransform(planes, style.Wavelet == Wavelet53Reversible); err != nil {
			return err
		}
	}

	// And into the image, with the level shift each component's own depth and
	// signedness decide.
	for comp, plane := range planes {
		tc := data.Components[comp]
		shift := int32(0)
		if !c.Components[comp].Signed {
			shift = 1 << uint(c.Components[comp].Depth-1)
		}
		lo, hi := sampleRange(c.Components[comp])

		// The tile-component is on the component's own grid, and so is its
		// plane, which starts where the image does on that grid (B.2).
		ox, oy, _, _ := c.componentBounds(comp)
		pw, ph := img.ComponentSize(comp)
		for y := tc.Y0; y < tc.Y1; y++ {
			row := y - oy
			if row < 0 || row >= ph {
				continue
			}
			for x := tc.X0; x < tc.X1; x++ {
				col := x - ox
				if col < 0 || col >= pw {
					continue
				}
				v := int32(roundSample(plane[(y-tc.Y0)*tc.Width()+(x-tc.X0)])) + shift
				img.Components[comp][row*pw+col] = clampInt32(v, lo, hi)
			}
		}
	}
	return nil
}

// subbandsOf decodes every code-block of one tile-component and lays the
// dequantized coefficients out band by band.
func (c *Codestream) subbandsOf(tile, comp int, tc TileComponent, quant Quantization,
	style CodingStyle, blocks []CodeBlockData) (map[int]map[int]*subbandGrid, error) {

	bands := map[int]map[int]*subbandGrid{}
	for r, res := range tc.Resolutions {
		bands[r] = map[int]*subbandGrid{}
		for _, band := range res.Bands {
			w, h := band.Width(), band.Height()
			bands[r][band.Type] = &subbandGrid{
				x0: band.X0, y0: band.Y0, x1: band.X1, y1: band.Y1,
				values: make([]float32, maxInt(0, w*h)),
			}
		}
	}

	// Pointers, not copies: a tile can hold thousands of small blocks.
	var mine []*CodeBlockData
	for i := range blocks {
		if blocks[i].Component == comp {
			mine = append(mine, &blocks[i])
		}
	}

	// Code-blocks are coded independently (B.7), so they decode in parallel.
	// Each writes its own rectangle of its band, which the decoder computes
	// from the header rather than taking from the file, and the errors are
	// read back in block order, so the result does not depend on scheduling.
	errs := make([]error, len(mine))
	next := make(chan int)
	roiShift := c.ROIShiftForTile(tile, comp)
	var wg sync.WaitGroup
	for w := 0; w < min(runtime.GOMAXPROCS(0), len(mine)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				errs[i] = c.decodeBlockInto(bands, tc, quant, style, comp, roiShift, mine[i])
			}
		}()
	}
	for i := range mine {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return bands, nil
}

// decodeBlockInto decodes one code-block and writes its dequantized
// coefficients into the band it belongs to. roiShift is the component's
// region-of-interest shift, 0 when it has none.
func (c *Codestream) decodeBlockInto(bands map[int]map[int]*subbandGrid, tc TileComponent,
	quant Quantization, style CodingStyle, comp, roiShift int, block *CodeBlockData) error {

	if block.Resolution >= len(tc.Resolutions) {
		return errorf("a code-block at resolution %d of %d", block.Resolution, len(tc.Resolutions))
	}
	res := tc.Resolutions[block.Resolution]
	band := Band{}
	for _, candidate := range res.Bands {
		if candidate.Type == block.BandType {
			band = candidate
		}
	}

	magnitudeBits, err := MagnitudeBits(quant, band, block.Resolution, style.Levels)
	if err != nil {
		return err
	}
	if block.Style&BlockStyleHT != 0 && roiShift > 0 {
		// T.814 allows it (A.5, 8.4) and nothing here can check it: no
		// codestream available has one, and OpenJPEG refuses it.
		return &ErrUnsupported{Feature: "a region of interest in HT code-blocks"}
	}
	coefficients, err := DecodeBlock(*block, magnitudeBits+roiShift)
	if err != nil {
		return err
	}
	if roiShift > 0 {
		descaleRegionOfInterest(coefficients, roiShift)
	}
	step, err := stepSize(quant, band, block.Resolution, style.Levels, c.Components[comp].Depth)
	if err != nil {
		return err
	}

	grid := bands[block.Resolution][block.BandType]
	if grid == nil {
		return errorf("a code-block in a band the resolution does not have")
	}
	reversible := style.Wavelet == Wavelet53Reversible
	bw := block.Block.Width()
	for y := 0; y < block.Block.Height(); y++ {
		row := block.Block.Y0 + y
		if row < grid.y0 || row >= grid.y1 {
			continue
		}
		for x := 0; x < bw; x++ {
			col := block.Block.X0 + x
			if col < grid.x0 || col >= grid.x1 {
				continue
			}
			// The coefficients arrive in halves; the step carries the factor
			// of two that turns them back into whole ones.
			grid.values[(row-grid.y0)*grid.width()+(col-grid.x0)] =
				scaleCoefficient(coefficients[y*bw+x], step, reversible)
		}
	}
	return nil
}

// descaleRegionOfInterest undoes the Maxshift scaling (Annex H.1): the
// encoder raised the region's coefficients by s bit planes, past every
// coefficient of the background, so any coefficient of magnitude 2^s or more
// is in the region and is scaled back down, and the rest are background and
// stay as they are. The band's bit planes were counted s higher to read them
// (decodeBlockInto), which is the other half of the method.
//
// The values are in halves (DecodeBlock), so the threshold is 2^(s+1) of
// them; shifting a value in halves right by s keeps it in halves.
func descaleRegionOfInterest(values []int32, shift int) {
	if shift >= 31 {
		// Nothing a block can hold reaches the threshold: all background.
		return
	}
	threshold := int32(1) << uint(shift+1)
	for i, v := range values {
		switch {
		case v >= threshold:
			values[i] = v >> uint(shift)
		case v <= -threshold:
			values[i] = -((-v) >> uint(shift))
		}
	}
}

// scaleCoefficient turns one code-block value, which is in halves, into the
// coefficient the wavelet filter wants.
//
// The reversible path must divide exactly and round towards zero, because
// lossless depends on it: the half is below the last bit a lossless decode
// reaches, and dropping it is what makes the result the encoder's own integer.
// The lossy path keeps the half through the multiplication, where it is worth
// a real part of a quantization step.
func scaleCoefficient(value int32, step float32, reversible bool) float32 {
	if reversible {
		return float32(value / 2)
	}
	return float32(value) * step / 2
}

// stepSize is the quantization step a subband's coefficients are multiplied by
// (E-3). A codestream that is not quantized at all has a step of one: the
// coefficient tier-1 produced is already the value.
//
// The nominal range Rb is the component's depth alone. Table E.1's subband
// gains belong to the reversible filter, which is never quantized; the
// irreversible filter carries its normalization in its own coefficients, so
// adding a gain here would scale every detail band by two or four.
func stepSize(q Quantization, band Band, resolution, levels, depth int) (float32, error) {
	if q.Style == QuantNone {
		return 1, nil
	}

	index := SubbandIndex(resolution, band.Type)
	var exponent, mantissa int
	switch q.Style {
	case QuantDerived:
		if len(q.Exponents) == 0 || len(q.Mantissas) == 0 {
			return 0, errorf("a derived quantization with no values")
		}
		exponent = q.Exponents[0] + band.Level - levels
		mantissa = q.Mantissas[0]
	default:
		if index < 0 || index >= len(q.Exponents) || index >= len(q.Mantissas) {
			return 0, errorf("subband %d has no quantization step, of %d given",
				index, len(q.Exponents))
		}
		exponent = q.Exponents[index]
		mantissa = q.Mantissas[index]
	}

	power := depth - exponent
	if power < -31 || power > 31 {
		return 0, errorf("a quantization step of 2^%d", power)
	}
	return float32(math.Ldexp(1+float64(mantissa)/2048, power)), nil
}

// inverseComponentTransform undoes the decorrelation across three components
// (G.2, G.3). The reversible transform is integer and exact; the irreversible
// one is the familiar YCbCr matrix.
func inverseComponentTransform(planes [][]float32, reversible bool) error {
	if len(planes) < 3 {
		return errorf("a component transform needs three components, not %d", len(planes))
	}
	n := len(planes[0])
	if len(planes[1]) != n || len(planes[2]) != n {
		return errorf("the three transformed components are different sizes")
	}

	for i := 0; i < n; i++ {
		y0, y1, y2 := planes[0][i], planes[1][i], planes[2][i]
		if reversible {
			// RCT (G.2.2). The green channel is recovered first, because the
			// other two were coded as differences from it.
			g := int32(y0) - floorDiv4Int(int32(y1)+int32(y2))
			planes[0][i] = float32(int32(y2) + g)
			planes[1][i] = float32(g)
			planes[2][i] = float32(int32(y1) + g)
			continue
		}
		// ICT (G.3.2).
		planes[0][i] = y0 + 1.402*y2
		planes[1][i] = y0 - 0.344136*y1 - 0.714136*y2
		planes[2][i] = y0 + 1.772*y1
	}
	return nil
}

// sampleRange is what a component's samples may be after the level shift.
// Clamping is part of the standard (G.1.2), not a safety net: an irreversible
// transform can overshoot, and a lossy file is allowed to.
func sampleRange(comp Component) (int32, int32) {
	if comp.Signed {
		return -(1 << uint(comp.Depth-1)), 1<<uint(comp.Depth-1) - 1
	}
	return 0, 1<<uint(comp.Depth) - 1
}

func clampInt32(v, lo, hi int32) int32 {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// roundSample rounds a reconstructed sample to the nearest integer, with ties
// going to the even one. A reversible decode produces whole numbers already, so
// this only matters for the irreversible path — but there it matters a lot:
// rounding halves upwards instead moved 497 of a 1024 sample test image by one.
func roundSample(v float32) float64 {
	return math.RoundToEven(float64(v))
}

func floorDiv4Int(x int32) int32 { return x >> 2 }
