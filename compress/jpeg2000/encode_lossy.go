package jpeg2000

import (
	"math"
)

// Lossy encoding: the irreversible 9/7 wavelet (F.4.8.2), the irreversible
// component transform (G.3), scalar quantization with a step for every
// subband (E.1), and a choice of step that meets the caller's rate.
//
// The rate is met by the quantization alone. Every coefficient is divided by
// its subband's step and rounded towards zero, the deadzone quantizer of
// E.1.1, and every bit plane of what is left is coded, in one quality layer.
// The steps are one base step divided by each subband's synthesis norm, the
// size an error of one in that subband has in the image (J.6 in spirit), so
// an error costs the same wherever it is made. The base step is then searched
// for: the largest whose codestream fits a target ratio, or whose estimated
// error meets a target PSNR.
//
// That is not the rate-distortion optimal truncation of Annex J.14, which
// codes finely and then drops the coding passes that buy least, and which
// needs pass-by-pass lengths the MQ coder here does not keep. Quantization
// alone lands somewhat below it at the same size, and it writes a
// codestream any decoder reads: one tile, one layer, the shape Encode writes.

// LossyOptions are the choices of a lossy encode. Exactly one of Ratio and
// PSNR is set.
type LossyOptions struct {
	// NoComponentTransform codes three components each on its own. The
	// irreversible component transform takes them for red, green and blue;
	// components already luminance and chrominance are left as they are.
	NoComponentTransform bool

	// Ratio is the compression ratio to meet: the samples' size, at the
	// bytes a sample of their depth takes, over the codestream's. The
	// codestream is the largest that is no bigger than that, as far as a
	// search over the quantization step finds one.
	Ratio float64

	// PSNR is the peak signal-to-noise ratio to meet, in decibels, the peak
	// being the largest sample the depth holds, met as this package decodes
	// the codestream: the step is found from the quantization errors weighted
	// by each subband's synthesis norm, then made finer until the decoded
	// image meets the target. Another conformant decoder differs from this
	// one by at most one in a sample.
	PSNR float64
}

// lossySearchSteps bounds the search for a base step: each halves the
// interval, from a span of 2^24, so the step found is within a part in a
// thousand of the best.
const lossySearchSteps = 34

// EncodeLossy writes an image as a lossy JPEG 2000 codestream, the irreversible
// 9/7 wavelet and scalar quantization at the rate options gives.
//
// The image is what Encode takes: every component the image's size, one depth
// from 1 to 16 bits, one signedness. Three components are coded with the
// irreversible component transform unless options says not to, which a DICOM
// data set records as Photometric Interpretation YBR_ICT (PS3.5 8.2.4).
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
		empty, err := l.encode(math.Exp2(float64(img.Depths[0] + 16)))
		if err != nil {
			return nil, err
		}
		return nil, errorf("a ratio of %g cannot be met: the image is %.0f bytes, and a codestream "+
			"of it is %d before it holds any of the picture", options.Ratio, target*options.Ratio, len(empty))
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
	for range img.Components {
		c.Components = append(c.Components, Component{Signed: signed, Depth: depth, DX: 1, DY: 1})
	}

	planes := levelShifted(img)
	if c.Coding.MCT {
		forwardIrreversibleComponentTransform(planes)
	}
	l := &lossyEncoder{c: c, depth: depth, levels: levels, pixels: img.Width * img.Height}
	for range planes {
		l.componentWeights = append(l.componentWeights, 1)
	}
	if c.Coding.MCT {
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
		bands, err := forwardDWTFilter(tc, planes[comp], true)
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
