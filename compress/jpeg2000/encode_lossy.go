package jpeg2000

import (
	"math"
	"sort"
)

// Lossy encoding: the irreversible 9/7 wavelet (F.4.8.2), the irreversible
// component transform (G.3) and scalar quantization with a step for every
// subband (E.1), or the reversible 5/3 and the reversible component transform
// unquantized; and a rate met by truncation.
//
// A ratio is met by rate-distortion optimal truncation (J.14, pcrd.go). The
// image is quantized finely, with steps that are one base step divided by
// each subband's synthesis norm, so that an error costs the same wherever it
// is made (J.6 in spirit); every block is coded once, and each coding pass is
// a place its data may be cut. The cuts that meet the target are the ones at
// one slope of error against bytes, in as many quality layers as asked.
//
// Near lossless one layer can do better another way: with the whole of every
// block coded at a step searched until the codestream fits. That is the one
// kept when it decodes nearer the image (EncodeLossy). A PSNR is met by the
// step alone: its error is estimated from the quantization, then the
// codestream decoded and the step made finer until it meets the target.

// LossyOptions are the choices of a lossy encode. Exactly one of Ratio and
// PSNR is set.
type LossyOptions struct {
	// NoComponentTransform codes three components each on its own. The
	// component transform takes them for red, green and blue; components
	// already luminance and chrominance are left as they are.
	NoComponentTransform bool

	// Ratio is the compression ratio to meet: the samples' size, at the
	// bytes a sample of their depth takes, over the codestream's. The
	// codestream is the largest no bigger than that the truncation finds,
	// within half a percent unless the whole image fits in less.
	Ratio float64

	// PSNR is the peak signal-to-noise ratio to meet, in decibels, the peak
	// being the largest sample the depth holds, met as this package decodes
	// the codestream: the step is found from the quantization errors weighted
	// by each subband's synthesis norm, then made finer until the decoded
	// image meets the target. Another conformant decoder differs from this
	// one by at most one in a sample. It is met in one layer, irreversibly.
	PSNR float64

	// Layers is how many quality layers a ratio is met in, one when zero.
	// The last reaches the ratio, and each before it half the size of the
	// next: a decoder that reads the first layers has a coarser picture of
	// the whole image.
	Layers int

	// Reversible codes with the reversible 5/3 wavelet and component
	// transform, and nothing quantized: the codestream is lossless but for
	// the coding passes the ratio cuts, and a decoder of its layers' passes
	// all returns the image exactly when nothing is cut. It takes a Ratio.
	// Three components are then YBR_RCT in a DICOM data set.
	Reversible bool
}

// lossySearchSteps bounds the search for a base step: each halves the
// interval, from a span of 2^24, so the step found is within a part in a
// thousand of the best.
const lossySearchSteps = 34

// maxLayers is the most quality layers a lossy encode writes. A COD can
// declare 65535; a few are what a viewer of a coarser picture first uses.
const maxLayers = 32

// EncodeLossy writes an image as a lossy JPEG 2000 codestream at the rate
// options gives: the irreversible 9/7 wavelet and scalar quantization, or the
// reversible 5/3 cut short.
//
// The image is what Encode takes: every component the image's size, one depth
// from 1 to 16 bits, one signedness. Three components are coded with the
// component transform unless options says not to, which a DICOM data set
// records as Photometric Interpretation YBR_ICT, or YBR_RCT when reversible
// (PS3.5 8.2.4).
func EncodeLossy(img *Image, options LossyOptions) ([]byte, error) {
	if err := checkEncodable(img); err != nil {
		return nil, err
	}
	if (options.Ratio > 0) == (options.PSNR > 0) || options.Ratio < 0 || options.PSNR < 0 {
		return nil, errorf("a lossy encode takes a ratio or a PSNR, one of them and above zero")
	}
	if options.Ratio > 0 && options.Ratio < 1 {
		return nil, errorf("a ratio of %g would make the codestream larger than the samples", options.Ratio)
	}
	if options.Layers < 0 || options.Layers > maxLayers {
		return nil, errorf("%d quality layers, of 1 to %d", options.Layers, maxLayers)
	}
	if options.PSNR > 0 && (options.Reversible || options.Layers > 1) {
		return nil, errorf("a PSNR is met in one irreversible layer; a reversible or layered encode takes a ratio")
	}
	l, err := newLossyEncoder(img, options)
	if err != nil {
		return nil, err
	}

	// The step is searched on a logarithmic scale between bounds no image
	// needs to leave: a step of 2^-8 keeps more than any depth holds, and
	// one of 2^(depth+16) leaves nothing.
	lo, hi := -8.0, float64(img.Depths[0]+16)
	if options.PSNR > 0 {
		peak := float64(int(1)<<uint(img.Depths[0]) - 1)
		target := peak * peak / math.Pow(10, options.PSNR/10)
		for i := 0; i < lossySearchSteps; i++ {
			mid := (lo + hi) / 2
			if l.estimatedMSE(math.Exp2(mid)) <= target {
				lo = mid
			} else {
				hi = mid
			}
		}
		// The estimate leaves out what the decoder's rounding and the
		// filter's not quite orthonormal basis add, a few tenths of a
		// decibel. So the codestream is decoded, here, and the step made
		// finer until what comes back meets the target.
		base := math.Exp2(lo)
		var stream []byte
		for i := 0; i < 4; i++ {
			var err error
			if stream, err = l.encode(base); err != nil {
				return nil, err
			}
			got, err := decodedPSNR(img, stream)
			if err != nil {
				return nil, err
			}
			if got >= options.PSNR {
				break
			}
			base *= math.Pow(10, (got-options.PSNR-0.05)/20)
		}
		return stream, nil
	}

	// The ratio is of the samples as stored, a whole number of bytes each.
	bytesPerSample := (img.Depths[0] + 7) / 8
	target := float64(img.Width*img.Height*len(img.Components)*bytesPerSample) / options.Ratio
	layers := max(1, options.Layers)
	stream, mse, err := l.encodeByTruncation(target, options.Ratio, layers)
	if err != nil {
		return nil, err
	}
	if layers == 1 && !options.Reversible && mse < nearLosslessMSE {
		// Near lossless, the decoder's rounding to whole samples takes away
		// the small errors the truncation's measure still counts, and a
		// finer step over every pass can do better at the same size: on
		// pydicom's SC_jpeg_no_color_transform.dcm at 20:1, 89.6 dB to
		// truncation's 85.2. There, the step is searched too and the
		// codestream that decodes nearer the image is kept.
		if alt, err := l.encodeByStep(target, options.Ratio); err == nil {
			mine, err1 := decodedPSNR(img, stream)
			theirs, err2 := decodedPSNR(img, alt)
			if err1 == nil && err2 == nil && theirs > mine {
				stream = alt
			}
		}
	}
	return stream, nil
}

// nearLosslessMSE is the mean square error per sample, as the truncation
// measures it, below which the decoder's rounding to whole samples decides
// the error more than the coding does.
const nearLosslessMSE = 1.0

// encodeByStep meets a target size by searching the quantization step, with
// every pass of every block coded: the largest step whose codestream fits.
func (l *lossyEncoder) encodeByStep(target, ratio float64) ([]byte, error) {
	lo, hi := -8.0, float64(l.depth+16)
	var best []byte
	for i := 0; i < lossySearchSteps; i++ {
		mid := (lo + hi) / 2
		stream, err := l.encode(math.Exp2(mid))
		if err != nil {
			return nil, err
		}
		if float64(len(stream)) <= target {
			best, hi = stream, mid
		} else {
			lo = mid
		}
		// Within a percent of the target is as close as a ratio is meant.
		if best != nil && float64(len(best)) >= 0.99*target {
			break
		}
	}
	if best == nil {
		// Even a step that leaves nothing of the image does not fit: the
		// headers alone are larger than the ratio allows, as they are for a
		// small image. Writing that would send an empty picture labeled as
		// this one.
		empty, err := l.encode(math.Exp2(float64(l.depth + 16)))
		if err != nil {
			return nil, err
		}
		return nil, errorf("a ratio of %g cannot be met: the image is %.0f bytes, and a codestream "+
			"of it is %d before it holds any of the picture", ratio, target*ratio, len(empty))
	}
	return best, nil
}

// decodedPSNR is the PSNR of img as this package decodes stream.
func decodedPSNR(img *Image, stream []byte) (float64, error) {
	c, err := ParseCodestream(stream)
	if err != nil {
		return 0, err
	}
	got, err := Decode(c)
	if err != nil {
		return 0, err
	}
	var sum float64
	for comp, plane := range img.Components {
		for i, v := range plane {
			d := float64(v - got.Components[comp][i])
			sum += d * d
		}
	}
	if sum == 0 {
		return math.Inf(1), nil
	}
	peak := float64(int(1)<<uint(img.Depths[0]) - 1)
	return 10 * math.Log10(peak*peak*float64(len(img.Components)*img.Width*img.Height)/sum), nil
}

// lossyEncoder holds what every trial of the search shares: the transformed
// coefficients and each subband's norm.
type lossyEncoder struct {
	c      *Codestream
	depth  int
	levels int
	comps  []lossyComponent
	pixels int
	// componentWeights are what an error of one in each coded component
	// costs, squared, summed over the components the decoder rebuilds: one
	// each without a component transform, and with the ICT the energy of its
	// inverse's columns (G.3.2), about three each, since an error in
	// luminance lands in all three of red, green and blue.
	componentWeights []float64
	// reversible codes with the 5/3 filter and the reversible component
	// transform, unquantized, and meets the rate by truncation alone.
	reversible bool
}

type lossyComponent struct {
	tc    TileComponent
	bands map[int]map[int]*subbandGrid
}

func newLossyEncoder(img *Image, options LossyOptions) (*lossyEncoder, error) {
	depth, signed := img.Depths[0], img.Signed[0]
	components := len(img.Components)
	levels := 0
	for levels < encodeLevels && minInt(img.Width, img.Height)>>uint(levels+1) >= 1 {
		levels++
	}
	c := &Codestream{
		Width: uint32(img.Width), Height: uint32(img.Height),
		TileWidth: uint32(img.Width), TileHeight: uint32(img.Height),
		Coding: CodingStyle{
			Progression:    ProgressionLRCP,
			Layers:         1,
			MCT:            components == 3 && !options.NoComponentTransform,
			Levels:         levels,
			CodeBlockWidth: 1 << encodeBlockExponent,
			CodeBlockHt:    1 << encodeBlockExponent,
			Wavelet:        Wavelet97Irreversible,
		},
	}
	if options.Reversible {
		c.Coding.Wavelet = Wavelet53Reversible
	}
	for range img.Components {
		c.Components = append(c.Components, Component{Signed: signed, Depth: depth, DX: 1, DY: 1})
	}

	planes := levelShifted(img)
	l := &lossyEncoder{c: c, depth: depth, levels: levels, pixels: img.Width * img.Height,
		reversible: options.Reversible}
	for range planes {
		l.componentWeights = append(l.componentWeights, 1)
	}
	switch {
	case c.Coding.MCT && options.Reversible:
		// The RCT's inverse (G.2.2): luminance lands whole in all three
		// colors, and each difference whole in one and a quarter off the
		// other two.
		forwardComponentTransform(planes)
		l.componentWeights = []float64{3, 11.0 / 16, 11.0 / 16}
	case c.Coding.MCT:
		forwardIrreversibleComponentTransform(planes)
		l.componentWeights = []float64{
			3,
			0.344136*0.344136 + 1.772*1.772,
			1.402*1.402 + 0.714136*0.714136,
		}
	}
	for comp := range planes {
		tc, err := c.TileComponentFor(0, comp)
		if err != nil {
			return nil, err
		}
		bands, err := forwardDWTFilter(tc, planes[comp], !options.Reversible)
		if err != nil {
			return nil, err
		}
		l.comps = append(l.comps, lossyComponent{tc: tc, bands: bands})
	}
	return l, nil
}

// forwardIrreversibleComponentTransform is the ICT (G.3.1): red, green and
// blue, level-shifted, become luminance and two color differences, the
// matrix inverseComponentTransform in decode.go inverts.
func forwardIrreversibleComponentTransform(planes [][]float32) {
	for i := range planes[0] {
		r, g, b := float64(planes[0][i]), float64(planes[1][i]), float64(planes[2][i])
		planes[0][i] = float32(0.299*r + 0.587*g + 0.114*b)
		planes[1][i] = float32(-0.16875*r - 0.33126*g + 0.5*b)
		planes[2][i] = float32(0.5*r - 0.41869*g - 0.08131*b)
	}
}

// bandStep is a subband's quantization step for a base step: the base over
// the band's synthesis norm, then rounded to what a QCD segment can say, an
// exponent of five bits and a mantissa of eleven (E-3), since the decoder
// multiplies by the step it reads and not by the one that was meant.
func (l *lossyEncoder) bandStep(base float64, band Band) (step float64, exponent, mantissa int) {
	if l.reversible {
		// Nothing is quantized; the exponent is the band's range (E.1.1).
		return 1, l.depth + bandGain(band.Type), 0
	}
	want := base / bandNorm(band, l.levels)
	power := int(math.Floor(math.Log2(want)))
	exponent = l.depth - power
	mantissa = int(math.Round((want/math.Exp2(float64(power)) - 1) * 2048))
	if mantissa >= 2048 {
		mantissa, exponent = 0, exponent-1
	}
	switch {
	case exponent < 0:
		exponent, mantissa = 0, 2047
	case exponent > 31:
		exponent, mantissa = 31, 0
	}
	return math.Ldexp(1+float64(mantissa)/2048, l.depth-exponent), exponent, mantissa
}

// quantize is the deadzone quantizer (E.1.1): the magnitude over the step,
// rounded towards zero, with its sign.
func quantize(coefficient float32, step float64) int32 {
	q := int32(math.Abs(float64(coefficient)) / step)
	if coefficient < 0 {
		return -q
	}
	return q
}

// estimatedMSE is the mean square error per sample a base step would leave:
// each coefficient's error between its value and what a decoder rebuilds
// from its quantized value, the middle of its interval (E.1.1.2), weighted
// by its subband's squared norm.
func (l *lossyEncoder) estimatedMSE(base float64) float64 {
	var sum float64
	for ci, comp := range l.comps {
		for r, res := range comp.tc.Resolutions {
			for _, band := range res.Bands {
				step, _, _ := l.bandStep(base, band)
				norm := bandNorm(band, l.levels)
				var bandSum float64
				for _, v := range comp.bands[r][band.Type].values {
					q := quantize(v, step)
					rebuilt := 0.0
					if q != 0 {
						rebuilt = (math.Abs(float64(q)) + 0.5) * step
					}
					e := math.Abs(float64(v)) - rebuilt
					bandSum += e * e
				}
				sum += bandSum * norm * norm * l.componentWeights[ci]
			}
		}
	}
	return sum / float64(l.pixels*len(l.comps))
}

// encode writes the codestream for a base step.
func (l *lossyEncoder) encode(base float64) ([]byte, error) {
	c := *l.c
	type component struct {
		blocks [][][]EncodedBlock
	}
	coded := make([]component, len(l.comps))
	var exponents, mantissas []int
	guard := encodeGuardBits
	for ci, comp := range l.comps {
		coded[ci].blocks = make([][][]EncodedBlock, len(comp.tc.Resolutions))
		for r, res := range comp.tc.Resolutions {
			coded[ci].blocks[r] = make([][]EncodedBlock, len(res.Bands))
			for b, band := range res.Bands {
				step, exponent, mantissa := l.bandStep(base, band)
				if ci == 0 {
					exponents = append(exponents, exponent)
					mantissas = append(mantissas, mantissa)
				}
				grid := comp.bands[r][band.Type]
				for _, block := range band.Blocks {
					q := make([]int32, 0, block.Width()*block.Height())
					for y := block.Y0; y < block.Y1; y++ {
						for x := block.X0; x < block.X1; x++ {
							q = append(q, quantize(grid.at(x, y), step))
						}
					}
					encoded, err := EncodeBlock(q, block.Width(), block.Height(), band.Type)
					if err != nil {
						return nil, err
					}
					coded[ci].blocks[r][b] = append(coded[ci].blocks[r][b], encoded)
					// A band holds its exponent plus the guard bits, less
					// one, of bit planes (E-2); the guard bits grow to hold
					// what the transform made larger than nominal.
					if need := encoded.BitPlanes - exponent + 1; need > guard {
						guard = need
					}
				}
			}
		}
	}
	if guard > maxGuardBits {
		return nil, errorf("the coefficients need %d guard bits, of the %d a codestream can declare",
			guard, maxGuardBits)
	}
	c.Quant = Quantization{Style: QuantExpounded, GuardBits: guard, Exponents: exponents, Mantissas: mantissas}

	var tile []byte
	for r := 0; r <= l.levels; r++ {
		for ci := range coded {
			res := l.comps[ci].tc.Resolutions[r]
			bands := make([][]packetBlock, len(res.Bands))
			for b := range res.Bands {
				magnitudeBits := guard + exponents[SubbandIndex(r, res.Bands[b].Type)] - 1
				for _, encoded := range coded[ci].blocks[r][b] {
					bands[b] = append(bands[b], packetBlock{
						EncodedBlock:  encoded,
						zeroBitPlanes: magnitudeBits - encoded.BitPlanes,
					})
				}
			}
			tile = append(tile, writePacket(res, bands)...)
		}
	}
	return writeCodestream(&c, tile), nil
}

// truncatedCode is the image coded once at a fine step, for cutting: every
// block with its truncation points and their hull, in the order the geometry
// lists them.
type truncatedCode struct {
	exponents, mantissas []int
	guard                int
	// blocks[comp][resolution][band] are the band's blocks in raster order.
	blocks [][][][]*layeredBlock
	hulls  [][]hullPoint // every block's hull, in the same order, flattened
	order  []*layeredBlock
	// samples is how many samples the image has, over every component.
	samples int
}

// codeForTruncation quantizes at a base step and codes every block, keeping
// each one's truncation points. A point's error is weighted into the image's
// units: the square of the band's step and norm, and the component's weight.
func (l *lossyEncoder) codeForTruncation(base float64) (*truncatedCode, error) {
	code := &truncatedCode{guard: encodeGuardBits, samples: l.pixels * len(l.comps)}
	code.blocks = make([][][][]*layeredBlock, len(l.comps))
	for ci, comp := range l.comps {
		code.blocks[ci] = make([][][]*layeredBlock, len(comp.tc.Resolutions))
		for r, res := range comp.tc.Resolutions {
			code.blocks[ci][r] = make([][]*layeredBlock, len(res.Bands))
			for b, band := range res.Bands {
				step, exponent, mantissa := l.bandStep(base, band)
				if ci == 0 {
					code.exponents = append(code.exponents, exponent)
					code.mantissas = append(code.mantissas, mantissa)
				}
				norm := l.norm(band)
				weight := step * step * norm * norm * l.componentWeights[ci]
				grid := comp.bands[r][band.Type]
				for _, block := range band.Blocks {
					n := block.Width() * block.Height()
					q := make([]int32, 0, n)
					exact := make([]float64, 0, n)
					energy := 0.0
					for y := block.Y0; y < block.Y1; y++ {
						for x := block.X0; x < block.X1; x++ {
							v := grid.at(x, y)
							q = append(q, quantize(v, step))
							exact = append(exact, math.Abs(float64(v))/step)
							energy += exact[len(exact)-1] * exact[len(exact)-1]
						}
					}
					encoded, points, err := encodeBlockTruncations(q, exact, block.Width(), block.Height(), band.Type, l.reversible)
					if err != nil {
						return nil, err
					}
					lb := &layeredBlock{EncodedBlock: encoded, w: block.Width(), h: block.Height(), bandType: band.Type, weight: weight, energy: energy * weight}
					code.blocks[ci][r][b] = append(code.blocks[ci][r][b], lb)
					code.order = append(code.order, lb)
					code.hulls = append(code.hulls, convexHull(points, weight))
					if len(points) > 0 {
						lb.gainedWhole = points[len(points)-1].gained
					}
					if need := encoded.BitPlanes - exponent + 1; need > code.guard {
						code.guard = need
					}
				}
			}
		}
	}
	if code.guard > maxGuardBits {
		return nil, errorf("the coefficients need %d guard bits, of the %d a codestream can declare",
			code.guard, maxGuardBits)
	}
	for ci := range code.blocks {
		for r, res := range l.comps[ci].tc.Resolutions {
			for b := range res.Bands {
				magnitudeBits := code.guard + code.exponents[SubbandIndex(r, res.Bands[b].Type)] - 1
				for _, lb := range code.blocks[ci][r][b] {
					lb.zeroBitPlanes, lb.magnitudeBits = magnitudeBits-lb.BitPlanes, magnitudeBits
				}
			}
		}
	}
	return code, nil
}

// writeTruncated writes the codestream of code with one layer for each
// threshold in lambdas, steepest first.
func (l *lossyEncoder) writeTruncated(code *truncatedCode, lambdas []float64) []byte {
	for i, lb := range code.order {
		lb.cuts = lb.cuts[:0]
		for _, lambda := range lambdas {
			lb.cuts = append(lb.cuts, cutAt(code.hulls[i], lambda))
		}
	}
	return l.writeCuts(code, len(lambdas))
}

// writeWhole writes the codestream of code with every pass of every block,
// all in the first of the given layers: a block's last passes can remove too
// little error to lie on its hull, and a coding that fits whole is kept whole.
func (l *lossyEncoder) writeWhole(code *truncatedCode, layers int) []byte {
	for _, lb := range code.order {
		lb.cuts = lb.cuts[:0]
		whole := truncationPoint{passes: lb.Passes, bytes: len(lb.Data), gained: lb.gainedWhole}
		for range layers {
			lb.cuts = append(lb.cuts, whole)
		}
	}
	return l.writeCuts(code, layers)
}

// writeCuts writes the codestream of code as its blocks' cuts stand, in the
// given number of layers.
func (l *lossyEncoder) writeCuts(code *truncatedCode, layers int) []byte {
	c := *l.c
	c.Coding.Layers = layers
	c.Quant = Quantization{Style: l.quantStyle(), GuardBits: code.guard,
		Exponents: code.exponents, Mantissas: code.mantissas}

	// LRCP: each layer whole, resolution by resolution, then component.
	states := make([][][]*layerState, len(l.comps))
	for ci := range states {
		states[ci] = make([][]*layerState, l.levels+1)
		for r := range states[ci] {
			states[ci][r] = make([]*layerState, len(l.comps[ci].tc.Resolutions[r].Bands))
		}
	}
	var tile []byte
	for layer := 0; layer < layers; layer++ {
		for r := 0; r <= l.levels; r++ {
			for ci := range l.comps {
				res := l.comps[ci].tc.Resolutions[r]
				tile = append(tile, writeLayerPacket(res, code.blocks[ci][r], layer, layers, states[ci][r])...)
			}
		}
	}
	return writeCodestream(&c, tile)
}

// fillTruncated spends what the slope left of the target. One slope for every
// block stops where the next hull segment of some block would pass the
// target, and that segment can be large, a bit plane of a block of the
// lowest band, while smaller ones of a shallower slope would fit. So the
// segments past each block's cut in the last layer are taken in order of
// slope, each that fits, and a block whose next does not fit takes no more:
// its later segments only follow from that one. It returns the codestream,
// or nil when nothing more fits.
func (l *lossyEncoder) fillTruncated(code *truncatedCode, lambdas []float64, size int, target float64) []byte {
	last := len(lambdas) - 1
	type step struct {
		block, point int
		slope        float64
	}
	var steps []step
	next := make([]int, len(code.order)) // each block's next hull point
	for i, lb := range code.order {
		hull := code.hulls[i]
		passes := lb.cuts[last].passes
		n := sort.Search(len(hull), func(k int) bool { return hull[k].passes > passes })
		next[i] = n
		for k := n; k < len(hull); k++ {
			steps = append(steps, step{i, k, hull[k].slope})
		}
	}
	sort.SliceStable(steps, func(a, b int) bool { return steps[a].slope > steps[b].slope })

	// The header grows too: a block newly in the layer adds its inclusion,
	// zero planes, passes and length, a few bytes, and one already there a
	// byte at most for its longer fields. The estimate is checked by writing.
	extra := func(i int) int {
		if code.order[i].cuts[last].passes == 0 {
			return 4
		}
		return 1
	}
	spent := float64(size)
	var taken []step
	closed := make([]bool, len(code.order))
	for _, s := range steps {
		if closed[s.block] || s.point != next[s.block] {
			continue
		}
		lb := code.order[s.block]
		p := code.hulls[s.block][s.point].truncationPoint
		cost := float64(p.bytes - lb.cuts[last].bytes + extra(s.block))
		if spent+cost > target {
			closed[s.block] = true
			continue
		}
		spent += cost
		lb.cuts[last] = p
		next[s.block]++
		taken = append(taken, s)
	}
	if len(taken) == 0 {
		return nil
	}
	// Written with the cuts as they now stand; if the estimate was short,
	// give back the last segments taken until it fits.
	for len(taken) > 0 {
		stream := l.writeCuts(code, len(lambdas))
		if float64(len(stream)) <= target {
			return stream
		}
		s := taken[len(taken)-1]
		taken = taken[:len(taken)-1]
		lb := code.order[s.block]
		if s.point > 0 {
			lb.cuts[last] = code.hulls[s.block][s.point-1].truncationPoint
		} else {
			lb.cuts[last] = truncationPoint{}
		}
		if last > 0 && lb.cuts[last].passes < lb.cuts[last-1].passes {
			lb.cuts[last] = lb.cuts[last-1]
		}
	}
	return nil
}

// truncationSearchSteps bounds the search for each layer's slope; each
// halves an interval of log slopes.
const truncationSearchSteps = 40

// encodeByTruncation meets a target size by cutting the blocks of a fine
// coding at one slope (J.14), in the given number of quality layers: the
// last reaches the target and each before it half the size of the next. It
// returns the codestream and the mean square error per sample the cuts
// leave, as the coding measures it.
func (l *lossyEncoder) encodeByTruncation(target, ratio float64, layers int) ([]byte, float64, error) {
	// A step of one sample over the band's norm leaves an error far below
	// what any ratio worth asking for allows. If even the whole of such a
	// coding fits, a finer one is made, so the target is used; reversibly,
	// nothing is quantized and the whole is the image itself.
	base := 1.0
	var code *truncatedCode
	for {
		var err error
		if code, err = l.codeForTruncation(base); err != nil {
			return nil, 0, err
		}
		whole := l.writeWhole(code, layers)
		if float64(len(whole)) > target {
			break
		}
		if l.reversible || base < 1.0/64 {
			return whole, code.remaining(), nil
		}
		base /= 4
	}

	// Nothing kept at all: if that does not fit either, the headers alone
	// pass the target, as they do for a small image at a high ratio.
	// Writing that would send an empty picture labeled as this one.
	none := make([]float64, layers)
	for i := range none {
		none[i] = math.Inf(1)
	}
	if empty := l.writeTruncated(code, none); float64(len(empty)) > target {
		return nil, 0, errorf("a ratio of %g cannot be met: the image is %.0f bytes, and a codestream "+
			"of it is %d before it holds any of the picture", ratio, target*ratio, len(empty))
	}

	steepest, shallowest := slopeRange(code.hulls)
	lambdas := make([]float64, 0, layers)
	var best []byte
	for layer := 0; layer < layers; layer++ {
		want := target / math.Exp2(float64(layers-1-layer))
		// The slope is searched on a log scale, from one steeper than any
		// block's first point, where nothing is kept, down to the shallowest
		// of any, or the layer before's, whichever is steeper.
		hi := math.Log2(steepest) + 1
		lo := math.Log2(shallowest) - 1
		if layer > 0 {
			hi = math.Log2(lambdas[layer-1])
		}
		var fit []byte
		fitLambda := math.Exp2(hi)
		for i := 0; i < truncationSearchSteps && lo < hi; i++ {
			mid := (lo + hi) / 2
			stream := l.writeTruncated(code, append(lambdas, math.Exp2(mid)))
			if float64(len(stream)) <= want {
				fit, fitLambda, hi = stream, math.Exp2(mid), mid
				if float64(len(stream)) >= 0.995*want {
					break
				}
			} else {
				lo = mid
			}
		}
		if fit == nil {
			fit = l.writeTruncated(code, append(lambdas, fitLambda))
		}
		lambdas = append(lambdas, fitLambda)
		best = fit
	}

	// The cuts as the last layer's slope left them, then the rest of the
	// target spent, the bytes no decoder reads given back, and spent again.
	l.writeTruncated(code, lambdas)
	for round := 0; round < 3; round++ {
		if filled := l.fillTruncated(code, lambdas, len(best), target); filled != nil {
			best = filled
		}
		if !trimCuts(code.order) {
			break
		}
		best = l.writeCuts(code, len(lambdas))
	}
	return best, code.remaining(), nil
}

// norm is bandNorm for the filter the encoder uses.
func (l *lossyEncoder) norm(band Band) float64 {
	if !l.reversible {
		return bandNorm(band, l.levels)
	}
	if l.levels == 0 {
		return 1
	}
	low, high := reversibleNorms[band.Level][0], reversibleNorms[band.Level][1]
	switch band.Type {
	case BandHL, BandLH:
		return low * high
	case BandHH:
		return high * high
	}
	return low * low
}

// quantStyle is how the codestream's QCD describes the steps: one for each
// subband, or, reversible, none at all.
func (l *lossyEncoder) quantStyle() int {
	if l.reversible {
		return QuantNone
	}
	return QuantExpounded
}

// reversibleNorms are the norms of the 5/3 synthesis at each level, low and
// high: measureSynthesisNorm's, for the 5/3 filter in real arithmetic. The
// integer filter rounds, but by less than one in a coefficient, and an
// error's size in the image is the linear filter's.
var reversibleNorms = func() (norms [encodeLevels + 1][2]float64) {
	for level := 1; level <= encodeLevels; level++ {
		norms[level][0] = measureReversibleNorm(level, false)
		norms[level][1] = measureReversibleNorm(level, true)
	}
	return norms
}()

// measureReversibleNorm is the L2 norm of one line synthesized by the 5/3
// filter (F.3.8.1), without its rounding, from an impulse in the low-pass or
// high-pass band of the given level.
func measureReversibleNorm(level int, high bool) float64 {
	n := 64 << uint(level)
	band := make([]float64, n>>uint(level))
	band[len(band)/2] = 1
	for l := level; l >= 1; l-- {
		line := make([]float64, n>>uint(l-1))
		for i, v := range band {
			if l == level && high {
				line[2*i+1] = v
			} else {
				line[2*i] = v
			}
		}
		at := func(i int) float64 { // symmetric extension; the impulse is far from either end
			if i < 0 {
				i = -i
			}
			if i >= len(line) {
				i = 2*(len(line)-1) - i
			}
			return line[i]
		}
		for i := 0; i < len(line); i += 2 {
			line[i] -= (at(i-1) + at(i+1)) / 4
		}
		for i := 1; i < len(line); i += 2 {
			line[i] += (at(i-1) + at(i+1)) / 2
		}
		band = line
	}
	var sum float64
	for _, v := range band {
		sum += v * v
	}
	return math.Sqrt(sum)
}

// bandNorm is the size in the image of a coefficient of one in a subband: the
// L2 norm of the 9/7 synthesis of an impulse there. The filter is separable,
// so it is the product of a norm along each axis, low-pass or high-pass at
// the band's decomposition level.
func bandNorm(band Band, levels int) float64 {
	if levels == 0 {
		return 1
	}
	low, high := synthesisNorm(band.Level, false), synthesisNorm(band.Level, true)
	switch band.Type {
	case BandHL, BandLH:
		return low * high
	case BandHH:
		return high * high
	}
	return low * low
}

// synthesisNorms holds synthesisNorm for every level an encode reaches, low
// and high, computed once: they are the same for every image.
var synthesisNorms = func() (norms [encodeLevels + 1][2]float64) {
	for level := 1; level <= encodeLevels; level++ {
		norms[level][0] = measureSynthesisNorm(level, false)
		norms[level][1] = measureSynthesisNorm(level, true)
	}
	return norms
}()

// synthesisNorm is the norm measureSynthesisNorm gives, from the table.
func synthesisNorm(level int, high bool) float64 {
	if level < 1 || level > encodeLevels {
		return measureSynthesisNorm(level, high)
	}
	if high {
		return synthesisNorms[level][1]
	}
	return synthesisNorms[level][0]
}

// measureSynthesisNorm is the L2 norm of one line synthesized, by this
// package's own inverse filter, from an impulse in the low-pass or high-pass
// band of the given decomposition level. Measured rather than taken from a
// table, so it is the norm of the filter the decoder runs, normalization and
// all.
func measureSynthesisNorm(level int, high bool) float64 {
	if level < 1 {
		return 1
	}
	// Long enough that the impulse's response, which widens about twofold a
	// level, stays clear of the ends at the deepest level.
	n := 64 << uint(level)
	band := make([]float32, n>>uint(level))
	band[len(band)/2] = 1
	for l := level; l >= 1; l-- {
		line := make([]float32, n>>uint(l-1))
		for i, v := range band {
			if l == level && high {
				line[2*i+1] = v
			} else {
				line[2*i] = v
			}
		}
		synthesize1D(line, 0, len(line), false, &scratch{})
		band = line
	}
	var sum float64
	for _, v := range band {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum)
}
