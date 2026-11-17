package jpeg2000

import (
	"encoding/binary"
)

// Encoding samples to a codestream, lossless: the other half of decode.go.
//
// The stages are the decoder's in reverse. The DC level shift (G.1.2) moves
// unsigned samples to the signed range the transform works in. For three
// components the reversible component transform (G.2.1) decorrelates them.
// The forward 5/3 transform (Annex F) splits each component into subbands,
// tier-1 (Annex D) codes each code-block of each subband, and tier-2 (Annex
// B) wraps the blocks in packets. Nothing is quantized and every bit plane
// is coded, so a decoder returns the samples that went in.
//
// The codestream is the simplest shape that is one: one tile, one quality
// layer, one precinct for each resolution, code-blocks of 64 by 64, packets
// in layer-resolution-component-position order. That is what every decoder
// reads, and what DICOM asks of transfer syntax 1.2.840.10008.1.2.4.90.

// The shape of what Encode writes.
const (
	// encodeLevels is how many times an image is decomposed when it is large
	// enough: five, giving six resolutions, which is OpenJPEG's default too.
	encodeLevels = 5
	// encodeBlockExponent is the code-block size as a power of two: 64.
	encodeBlockExponent = 6
	// encodeGuardBits is how many guard bits a band has before any is added
	// for coefficients that outgrow their nominal range (E.1).
	encodeGuardBits = 2
	// maxGuardBits is the most the three bits of Sqcd can say.
	maxGuardBits = 7
)

// EncodeOptions are the choices Encode leaves to its caller.
type EncodeOptions struct {
	// NoComponentTransform codes three components each on its own. The
	// reversible component transform takes them for red, green and blue,
	// which is what makes it worth doing; components that are something
	// else, luminance and chrominance already, are better left as they are.
	NoComponentTransform bool
}

// Encode writes an image as a lossless JPEG 2000 codestream.
//
// Every component has the image's size, and they share one depth, from 1 to
// 16 bits, and one signedness: that is every DICOM image. Three components
// are coded with the reversible component transform, which a DICOM data set
// records as Photometric Interpretation YBR_RCT (PS3.5 8.2.4). A sample
// outside what its depth holds is an error: it could not come back.
func Encode(img *Image) ([]byte, error) {
	return EncodeWithOptions(img, EncodeOptions{})
}

// EncodeWithOptions is Encode with the choices of EncodeOptions.
func EncodeWithOptions(img *Image, options EncodeOptions) ([]byte, error) {
	if err := checkEncodable(img); err != nil {
		return nil, err
	}
	depth, signed := img.Depths[0], img.Signed[0]
	components := len(img.Components)

	levels := 0
	for levels < encodeLevels && minInt(img.Width, img.Height)>>uint(levels+1) >= 1 {
		levels++
	}

	// The header's own description of the image, which is also what gives
	// the geometry: the encoder cuts the image up with the functions the
	// decoder will use to put it back.
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
			Wavelet:        Wavelet53Reversible,
		},
	}
	for range img.Components {
		c.Components = append(c.Components, Component{Signed: signed, Depth: depth, DX: 1, DY: 1})
	}

	planes := levelShifted(img)
	if c.Coding.MCT {
		forwardComponentTransform(planes)
	}

	// Tier-1 for every block of every component, before any header is
	// written: how many bit planes the blocks need decides the guard bits.
	type component struct {
		tc     TileComponent
		blocks [][][]EncodedBlock // by resolution, band, block
	}
	coded := make([]component, components)
	overflow := 0
	for comp := range planes {
		tc, err := c.TileComponentFor(0, comp)
		if err != nil {
			return nil, err
		}
		bands, err := forwardDWT(tc, planes[comp])
		if err != nil {
			return nil, err
		}
		coded[comp].tc = tc
		coded[comp].blocks = make([][][]EncodedBlock, len(tc.Resolutions))
		for r, res := range tc.Resolutions {
			coded[comp].blocks[r] = make([][]EncodedBlock, len(res.Bands))
			for b, band := range res.Bands {
				grid := bands[r][band.Type]
				nominal := nominalMagnitudeBits(depth, band.Type)
				for _, block := range band.Blocks {
					encoded, err := EncodeBlock(blockCoefficients(grid, block), block.Width(), block.Height(), band.Type)
					if err != nil {
						return nil, err
					}
					coded[comp].blocks[r][b] = append(coded[comp].blocks[r][b], encoded)
					if over := encoded.BitPlanes - nominal; over > overflow {
						overflow = over
					}
				}
			}
		}
	}

	// A band holds its exponent plus the guard bits, less one, of bit planes
	// (E-2). The transforms can carry a coefficient past the nominal range,
	// and guard bits are what the standard provides for that.
	guard := encodeGuardBits + overflow
	if guard > maxGuardBits {
		return nil, errorf("the coefficients need %d guard bits, of the %d a codestream can declare",
			guard, maxGuardBits)
	}
	for r := 0; r <= levels; r++ {
		for _, band := range coded[0].tc.Resolutions[r].Bands {
			c.Quant.Exponents = append(c.Quant.Exponents, depth+bandGain(band.Type))
		}
	}
	c.Quant.Style, c.Quant.GuardBits = QuantNone, guard

	var tile []byte
	for r := 0; r <= levels; r++ {
		for comp := range coded {
			res := coded[comp].tc.Resolutions[r]
			bands := make([][]packetBlock, len(res.Bands))
			for b, band := range res.Bands {
				magnitudeBits := depth + bandGain(band.Type) + guard - 1
				for _, encoded := range coded[comp].blocks[r][b] {
					bands[b] = append(bands[b], packetBlock{
						EncodedBlock:  encoded,
						zeroBitPlanes: magnitudeBits - encoded.BitPlanes,
					})
				}
			}
			tile = append(tile, writePacket(res, bands)...)
		}
	}
	return writeCodestream(c, tile), nil
}

// checkEncodable refuses what Encode cannot write as it is.
func checkEncodable(img *Image) error {
	if img == nil {
		return errorf("no image")
	}
	if img.Width <= 0 || img.Height <= 0 {
		return errorf("an image of %dx%d samples", img.Width, img.Height)
	}
	if img.Width > 1<<16 || img.Height > 1<<16 || int64(img.Width)*int64(img.Height) > 1<<28 {
		return errorf("an image of %dx%d samples is larger than this encoder accepts", img.Width, img.Height)
	}
	n := len(img.Components)
	if n == 0 || n > 16384 || len(img.Depths) != n || len(img.Signed) != n {
		return errorf("an image of %d components, %d depths and %d signs", n, len(img.Depths), len(img.Signed))
	}
	depth, signed := img.Depths[0], img.Signed[0]
	if depth < 1 || depth > 16 {
		return errorf("components of %d bits; 1 to 16 are encoded", depth)
	}
	lo, hi := sampleRange(Component{Signed: signed, Depth: depth})
	for i, plane := range img.Components {
		if img.Depths[i] != depth || img.Signed[i] != signed {
			return errorf("component %d is %d bits and the first is %d: they are encoded alike or not at all",
				i, img.Depths[i], depth)
		}
		if len(plane) != img.Width*img.Height {
			return errorf("component %d holds %d samples for an image of %dx%d", i, len(plane), img.Width, img.Height)
		}
		for _, v := range plane {
			if v < lo || v > hi {
				return errorf("component %d holds %d, outside the %d to %d its %d bits hold", i, v, lo, hi, depth)
			}
		}
	}
	return nil
}

// levelShifted returns the components as the transform takes them: unsigned
// samples moved down by half their range, so that they are centered on zero
// as signed ones already are (G.1.2).
func levelShifted(img *Image) [][]float32 {
	shift := int32(0)
	if !img.Signed[0] {
		shift = 1 << uint(img.Depths[0]-1)
	}
	planes := make([][]float32, len(img.Components))
	for i, plane := range img.Components {
		planes[i] = make([]float32, len(plane))
		for k, v := range plane {
			planes[i][k] = float32(v - shift)
		}
	}
	return planes
}

// forwardComponentTransform is the reversible component transform (G.2.1):
// three components, taken as red, green and blue, become one of luminance
// and two of difference from green. It is integer and exact, and
// inverseComponentTransform in decode.go undoes it.
func forwardComponentTransform(planes [][]float32) {
	for i := range planes[0] {
		r, g, b := int32(planes[0][i]), int32(planes[1][i]), int32(planes[2][i])
		planes[0][i] = float32(floorDiv4Int(r + 2*g + b))
		planes[1][i] = float32(b - g)
		planes[2][i] = float32(r - g)
	}
}

// bandGain is how many bits a band's coefficients gain over the samples in
// the reversible transform (Table E.1): one for each direction it is
// high-pass in.
func bandGain(bandType int) int {
	switch bandType {
	case BandHL, BandLH:
		return 1
	case BandHH:
		return 2
	}
	return 0
}

// nominalMagnitudeBits is how many bit planes a band holds with the guard
// bits Encode starts from (E-2).
func nominalMagnitudeBits(depth, bandType int) int {
	return depth + bandGain(bandType) + encodeGuardBits - 1
}

// blockCoefficients takes one code-block's coefficients out of its band.
func blockCoefficients(grid *subbandGrid, block CodeBlock) []int32 {
	out := make([]int32, block.Width()*block.Height())
	i := 0
	for y := block.Y0; y < block.Y1; y++ {
		for x := block.X0; x < block.X1; x++ {
			out[i] = int32(grid.at(x, y))
			i++
		}
	}
	return out
}

// writeCodestream writes the markers of Annex A around one tile's packets:
// the main header (SOC, SIZ, COD, QCD), one tile-part (SOT, SOD and the
// data), and EOC. It writes what ParseCodestream reads back into c.
func writeCodestream(c *Codestream, tile []byte) []byte {
	be := binary.BigEndian
	var out []byte
	marker := func(m uint16) { out = be.AppendUint16(out, m) }
	// A segment's length counts its own two bytes and not the marker's.
	segment := func(m uint16, body []byte) {
		marker(m)
		out = be.AppendUint16(out, uint16(len(body)+2))
		out = append(out, body...)
	}

	marker(markerSOC)

	// SIZ (A.5.1): the image and the one tile that covers it, both at the
	// origin of the reference grid, and each component's depth.
	siz := be.AppendUint16(nil, 0) // Rsiz: no capability beyond Part 1
	for _, v := range []uint32{c.Width, c.Height, 0, 0, c.TileWidth, c.TileHeight, 0, 0} {
		siz = be.AppendUint32(siz, v)
	}
	siz = be.AppendUint16(siz, uint16(len(c.Components)))
	for _, comp := range c.Components {
		ssiz := byte(comp.Depth - 1)
		if comp.Signed {
			ssiz |= 0x80
		}
		siz = append(siz, ssiz, 1, 1)
	}
	segment(markerSIZ, siz)

	// COD (A.6.1): no precincts, SOP or EPH; the progression, one layer,
	// the component transform; then the levels, the code-block size as its
	// exponent less two, style 0, and the reversible filter.
	mct := byte(0)
	if c.Coding.MCT {
		mct = 1
	}
	cod := []byte{0, byte(c.Coding.Progression)}
	cod = be.AppendUint16(cod, uint16(c.Coding.Layers))
	cod = append(cod, mct, byte(c.Coding.Levels),
		encodeBlockExponent-2, encodeBlockExponent-2, 0, Wavelet53Reversible)
	segment(markerCOD, cod)

	// QCD (A.6.4): no quantization, the guard bits, and an exponent for
	// each subband in its top five bits.
	qcd := []byte{byte(c.Quant.GuardBits<<5 | QuantNone)}
	for _, exponent := range c.Quant.Exponents {
		qcd = append(qcd, byte(exponent<<3))
	}
	segment(markerQCD, qcd)

	// SOT (A.4.2): tile 0, the length of this tile-part from the SOT marker
	// to the end of its data, part 0 of 1.
	const sotLength, sodLength = 12, 2
	sot := be.AppendUint16(nil, 0)
	sot = be.AppendUint32(sot, uint32(sotLength+sodLength+len(tile)))
	sot = append(sot, 0, 1)
	segment(markerSOT, sot)
	marker(markerSOD)
	out = append(out, tile...)

	marker(markerEOC)
	return out
}
