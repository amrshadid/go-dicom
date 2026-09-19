// Package jpeg2000 decodes JPEG 2000 Part 1 codestreams in pure Go.
//
// DICOM carries JPEG 2000 in two transfer syntaxes, 1.2.840.10008.1.2.4.90
// (lossless) and .91 (lossy), and this library bundled no decoder for either:
// an instance in those syntaxes could be stored and sent but not displayed
// (#72). The alternative was CGO against OpenJPEG, which costs the library the
// property it advertises — no hidden native dependency — on every platform it
// builds for.
//
// What is implemented is the subset Part 1 files in medical imaging actually
// use, and anything outside it is refused by name rather than guessed at. A
// decoder that quietly mis-decodes is worse than none: the image looks
// plausible while the numbers are wrong, and in an RT Dose a wrong number is a
// wrong dose.
package jpeg2000

import (
	"encoding/binary"
	"fmt"
)

// Markers delimit the segments of a codestream (PS 15444-1 Annex A).
const (
	markerSOC = 0xFF4F // start of codestream
	markerSIZ = 0xFF51 // image and tile size
	markerCOD = 0xFF52 // coding style default
	markerCOC = 0xFF53 // coding style component
	markerTLM = 0xFF55 // tile-part lengths
	markerPLM = 0xFF57 // packet length, main header
	markerPLT = 0xFF58 // packet length, tile-part header
	markerQCD = 0xFF5C // quantization default
	markerQCC = 0xFF5D // quantization component
	markerRGN = 0xFF5E // region of interest
	markerPOC = 0xFF5F // progression order change
	markerPPM = 0xFF60 // packed packet headers, main header
	markerPPT = 0xFF61 // packed packet headers, tile-part header
	markerCRG = 0xFF63 // component registration
	markerCOM = 0xFF64 // comment
	markerSOT = 0xFF90 // start of tile-part
	markerSOD = 0xFF93 // start of data
	markerEOC = 0xFFD9 // end of codestream
)

// Progression orders (Annex A.6.1). Only the two the corpus uses are decoded.
const (
	ProgressionLRCP = 0 // layer, resolution, component, position
	ProgressionRLCP = 1 // resolution, layer, component, position
)

// Wavelet transforms (Annex A.6.1, SPcod transformation).
const (
	Wavelet97Irreversible = 0
	Wavelet53Reversible   = 1
)

// Quantization styles (Annex A.6.4, Sqcd low five bits).
const (
	QuantNone      = 0 // no quantization: reversible, exponents only
	QuantDerived   = 1 // one value derived for every subband
	QuantExpounded = 2 // a value per subband
)

// Component describes one image component from the SIZ segment.
type Component struct {
	// Signed reports whether samples are two's complement.
	Signed bool
	// Depth is the bit depth, 1 to 38 in the standard and at most 16 here.
	Depth int
	// DX and DY are the sample separations. Anything but 1 is subsampling,
	// which this decoder refuses rather than resample.
	DX, DY int
}

// CodingStyle is a COD segment, or a COC override for one component.
type CodingStyle struct {
	Progression    int
	Layers         int
	MCT            bool // multiple component transform: RCT with 5/3, ICT with 9/7
	Levels         int  // decomposition levels
	CodeBlockWidth int
	CodeBlockHt    int
	BlockStyle     byte // Annex A.6.1 SPcod code-block style; only 0 is decoded
	Wavelet        int
	UsePrecincts   bool
	SOP, EPH       bool
}

// Quantization is a QCD segment, or a QCC override for one component.
type Quantization struct {
	Style     int
	GuardBits int
	// Exponents and Mantissas are per subband, in the order the standard
	// writes them: LL, then HL, LH, HH for each resolution level.
	Exponents []int
	Mantissas []int
}

// TilePart is one tile-part's compressed data: everything between its SOD
// marker and the start of the next segment.
type TilePart struct {
	Tile  int
	Index int // TPsot, the part's place within the tile
	Data  []byte
}

// Codestream is a parsed main header plus the tile-parts that follow it.
type Codestream struct {
	// The image on the reference grid (Annex A.5.1). Width and Height are the
	// image's own size, XOffset and YOffset its origin on the grid.
	Width, Height   uint32
	XOffset         uint32
	YOffset         uint32
	TileWidth       uint32
	TileHeight      uint32
	TileXOffset     uint32
	TileYOffset     uint32
	Components      []Component
	Coding          CodingStyle
	ComponentCoding map[int]CodingStyle
	Quant           Quantization
	ComponentQuant  map[int]Quantization

	// Per-tile overrides. A tile-part header may restate the coding style or
	// quantization for its own tile, for every component or for one, and the
	// corpus does: SC_rgb_gdcm_KY carries a COC per component per tile.
	TileCoding          map[int]CodingStyle
	TileComponentCoding map[TileComponent]CodingStyle
	TileQuant           map[int]Quantization
	TileComponentQuant  map[TileComponent]Quantization

	TileParts []TilePart
}

// TileComponent names one component of one tile, for the override maps.
type TileComponent struct {
	Tile      int
	Component int
}

// TilesAcross and TilesDown give the tile grid (Annex B.3).
func (c *Codestream) TilesAcross() int {
	return int((c.Width+c.TileXOffset-c.TileXOffset)+c.TileWidth-1-c.TileXOffset) / int(c.TileWidth)
}

// NumTiles is how many tiles the image is divided into.
func (c *Codestream) NumTiles() int {
	across := ceilDiv(c.Width-c.TileXOffset, c.TileWidth)
	down := ceilDiv(c.Height-c.TileYOffset, c.TileHeight)
	return across * down
}

// CodingForTile returns the coding style in force for a component of a tile,
// which is the component override, then the tile's, then the main header's.
func (c *Codestream) CodingForTile(tile, component int) CodingStyle {
	if style, ok := c.TileComponentCoding[TileComponent{Tile: tile, Component: component}]; ok {
		return style
	}
	if style, ok := c.TileCoding[tile]; ok {
		return style
	}
	return c.CodingFor(component)
}

// QuantForTile returns the quantization in force for a component of a tile.
func (c *Codestream) QuantForTile(tile, component int) Quantization {
	if q, ok := c.TileComponentQuant[TileComponent{Tile: tile, Component: component}]; ok {
		return q
	}
	if q, ok := c.TileQuant[tile]; ok {
		return q
	}
	return c.QuantFor(component)
}

// CodingFor returns the coding style in force for a component.
func (c *Codestream) CodingFor(component int) CodingStyle {
	if style, ok := c.ComponentCoding[component]; ok {
		return style
	}
	return c.Coding
}

// QuantFor returns the quantization in force for a component.
func (c *Codestream) QuantFor(component int) Quantization {
	if q, ok := c.ComponentQuant[component]; ok {
		return q
	}
	return c.Quant
}

func ceilDiv(a, b uint32) int {
	if b == 0 {
		return 0
	}
	return int((a + b - 1) / b)
}

// ErrUnsupported reports a codestream feature this decoder does not implement.
// It names the feature: a caller seeing it can tell "this file needs something
// we do not have" from "this file is broken", and can register an external
// decoder for it.
type ErrUnsupported struct {
	Feature string
}

func (e *ErrUnsupported) Error() string {
	return "jpeg2000: unsupported: " + e.Feature
}

// ParseCodestream reads a JPEG 2000 codestream, or the codestream inside a JP2
// container, and returns its header parameters and tile-parts.
//
// Every length is treated as peer-controlled: it is checked against the bytes
// that remain before it is used, so a truncated or hostile file is an error
// rather than a panic or an allocation.
func ParseCodestream(data []byte) (*Codestream, error) {
	if box, err := codestreamFromJP2(data); err != nil {
		return nil, err
	} else if box != nil {
		data = box
	}

	if len(data) < 4 || be16(data) != markerSOC {
		return nil, fmt.Errorf("jpeg2000: not a codestream: it does not begin with SOC")
	}

	c := &Codestream{
		ComponentCoding:     map[int]CodingStyle{},
		ComponentQuant:      map[int]Quantization{},
		TileCoding:          map[int]CodingStyle{},
		TileComponentCoding: map[TileComponent]CodingStyle{},
		TileQuant:           map[int]Quantization{},
		TileComponentQuant:  map[TileComponent]Quantization{},
	}
	pos := 2
	seenSIZ, seenCOD, seenQCD := false, false, false

	for pos+1 < len(data) {
		marker := be16(data[pos:])
		pos += 2

		switch marker {
		case markerEOC:
			return c, validate(c, seenSIZ, seenCOD, seenQCD)

		case markerSOD:
			return nil, fmt.Errorf("jpeg2000: SOD outside a tile-part at offset %d", pos-2)

		case markerSOT:
			next, err := parseTilePart(c, data, pos)
			if err != nil {
				return nil, err
			}
			pos = next
			continue
		}

		// Every other marker in the main header carries a length that counts
		// itself, so a zero or oversized one would leave the parser in place or
		// past the end.
		if pos+2 > len(data) {
			return nil, fmt.Errorf("jpeg2000: truncated segment length for marker %04X", marker)
		}
		length := int(be16(data[pos:]))
		if length < 2 || pos+length > len(data) {
			return nil, fmt.Errorf("jpeg2000: marker %04X declares %d bytes, %d remain",
				marker, length, len(data)-pos)
		}
		segment := data[pos+2 : pos+length]

		var err error
		switch marker {
		case markerSIZ:
			err = parseSIZ(c, segment)
			seenSIZ = true
		case markerCOD:
			c.Coding, err = parseCOD(segment)
			seenCOD = true
		case markerCOC:
			err = parseCOC(c, segment)
		case markerQCD:
			c.Quant, err = parseQuantization(segment, 0)
			seenQCD = true
		case markerQCC:
			err = parseQCC(c, segment)
		case markerCOM, markerTLM, markerPLM, markerPLT, markerCRG:
			// Comments and length indexes say nothing about how to decode;
			// skipping them is what the standard expects of a reader that does
			// not use them.
		case markerPPM, markerPPT:
			err = &ErrUnsupported{Feature: "packed packet headers (PPM/PPT)"}
		case markerPOC:
			err = &ErrUnsupported{Feature: "progression order change (POC)"}
		case markerRGN:
			err = &ErrUnsupported{Feature: "region of interest (RGN)"}
		default:
			err = fmt.Errorf("jpeg2000: unexpected marker %04X at offset %d", marker, pos-2)
		}
		if err != nil {
			return nil, err
		}
		pos += length
	}

	return c, validate(c, seenSIZ, seenCOD, seenQCD)
}

// codestreamFromJP2 returns the contiguous codestream inside a JP2 container,
// or nil when the data is a bare codestream. DICOM allows either.
func codestreamFromJP2(data []byte) ([]byte, error) {
	const jp2Signature = 0x6A502020 // "jP  "
	if len(data) < 12 || be32(data[4:]) != jp2Signature {
		return nil, nil
	}
	for pos := 0; pos+8 <= len(data); {
		length := int(be32(data[pos:]))
		boxType := be32(data[pos+4:])
		body := pos + 8
		switch {
		case length == 1:
			return nil, &ErrUnsupported{Feature: "JP2 box with a 64-bit length"}
		case length == 0:
			length = len(data) - pos // to the end of the file
		case length < 8 || pos+length > len(data):
			return nil, fmt.Errorf("jpeg2000: JP2 box declares %d bytes, %d remain",
				length, len(data)-pos)
		}
		if boxType == 0x6A703263 { // "jp2c", the contiguous codestream
			return data[body : pos+length], nil
		}
		pos += length
	}
	return nil, fmt.Errorf("jpeg2000: JP2 container holds no codestream box")
}

func parseSIZ(c *Codestream, s []byte) error {
	if len(s) < 36 {
		return fmt.Errorf("jpeg2000: SIZ is %d bytes, want at least 36", len(s))
	}
	// s[0:2] is Rsiz, the capability set; Part 1 readers ignore it.
	c.Width = be32(s[2:])
	c.Height = be32(s[6:])
	c.XOffset = be32(s[10:])
	c.YOffset = be32(s[14:])
	c.TileWidth = be32(s[18:])
	c.TileHeight = be32(s[22:])
	c.TileXOffset = be32(s[26:])
	c.TileYOffset = be32(s[30:])
	count := int(be16(s[34:]))

	if c.Width <= c.XOffset || c.Height <= c.YOffset {
		return fmt.Errorf("jpeg2000: SIZ image is %dx%d with origin (%d,%d)",
			c.Width, c.Height, c.XOffset, c.YOffset)
	}
	if c.TileWidth == 0 || c.TileHeight == 0 {
		return fmt.Errorf("jpeg2000: SIZ declares a zero-sized tile")
	}
	if count == 0 || len(s) < 36+count*3 {
		return fmt.Errorf("jpeg2000: SIZ declares %d components, segment holds %d bytes",
			count, len(s))
	}

	c.Components = make([]Component, count)
	for i := 0; i < count; i++ {
		ssiz := s[36+i*3]
		comp := Component{
			Signed: ssiz&0x80 != 0,
			Depth:  int(ssiz&0x7F) + 1,
			DX:     int(s[37+i*3]),
			DY:     int(s[38+i*3]),
		}
		if comp.Depth > 16 {
			return &ErrUnsupported{Feature: fmt.Sprintf("%d-bit components", comp.Depth)}
		}
		if comp.DX != 1 || comp.DY != 1 {
			return &ErrUnsupported{Feature: "component subsampling"}
		}
		c.Components[i] = comp
	}
	return nil
}

func parseCOD(s []byte) (CodingStyle, error) {
	if len(s) < 10 {
		return CodingStyle{}, fmt.Errorf("jpeg2000: COD is %d bytes, want at least 10", len(s))
	}
	scod := s[0]
	style := CodingStyle{
		UsePrecincts: scod&0x01 != 0,
		SOP:          scod&0x02 != 0,
		EPH:          scod&0x04 != 0,
		Progression:  int(s[1]),
		Layers:       int(be16(s[2:])),
		MCT:          s[4] != 0,
	}
	if err := parseCodingParameters(&style, s[5:]); err != nil {
		return CodingStyle{}, err
	}
	if style.Layers == 0 {
		return CodingStyle{}, fmt.Errorf("jpeg2000: COD declares zero layers")
	}
	if style.Progression != ProgressionLRCP && style.Progression != ProgressionRLCP {
		return CodingStyle{}, &ErrUnsupported{
			Feature: fmt.Sprintf("progression order %d", style.Progression)}
	}
	return style, nil
}

func parseCOC(c *Codestream, s []byte) error {
	if len(c.Components) == 0 {
		return fmt.Errorf("jpeg2000: COC before SIZ")
	}
	index, rest, err := componentIndex(c, s)
	if err != nil {
		return err
	}
	if len(rest) < 6 {
		return fmt.Errorf("jpeg2000: COC is too short")
	}
	// A COC inherits everything in SGcod — progression, layers, MCT — from the
	// COD it overrides, and carries only the SPcoc parameters.
	style := c.Coding
	style.UsePrecincts = rest[0]&0x01 != 0
	if err := parseCodingParameters(&style, rest[1:]); err != nil {
		return err
	}
	c.ComponentCoding[index] = style
	return nil
}

// parseTileCOC records a coding style that applies to one component of one tile.
func parseTileCOC(c *Codestream, tile int, s []byte) error {
	index, rest, err := componentIndex(c, s)
	if err != nil {
		return err
	}
	if len(rest) < 6 {
		return fmt.Errorf("jpeg2000: COC in tile %d is too short", tile)
	}
	style := c.CodingFor(index)
	if tileStyle, ok := c.TileCoding[tile]; ok {
		style = tileStyle
	}
	style.UsePrecincts = rest[0]&0x01 != 0
	if err := parseCodingParameters(&style, rest[1:]); err != nil {
		return err
	}
	c.TileComponentCoding[TileComponent{Tile: tile, Component: index}] = style
	return nil
}

// parseTileQCC records quantization for one component of one tile.
func parseTileQCC(c *Codestream, tile int, s []byte) error {
	index, rest, err := componentIndex(c, s)
	if err != nil {
		return err
	}
	q, err := parseQuantization(rest, 0)
	if err != nil {
		return err
	}
	c.TileComponentQuant[TileComponent{Tile: tile, Component: index}] = q
	return nil
}

// parseCodingParameters reads SPcod/SPcoc: levels, code-block size, style and
// wavelet, which COD and COC share.
func parseCodingParameters(style *CodingStyle, s []byte) error {
	if len(s) < 5 {
		return fmt.Errorf("jpeg2000: coding parameters are %d bytes, want 5", len(s))
	}
	style.Levels = int(s[0])
	style.CodeBlockWidth = 1 << (int(s[1]&0x0F) + 2)
	style.CodeBlockHt = 1 << (int(s[2]&0x0F) + 2)
	style.BlockStyle = s[3]
	style.Wavelet = int(s[4])

	switch {
	case style.Levels > 32:
		return fmt.Errorf("jpeg2000: %d decomposition levels", style.Levels)
	case style.CodeBlockWidth*style.CodeBlockHt > 4096:
		return fmt.Errorf("jpeg2000: code-block %dx%d exceeds 4096 samples",
			style.CodeBlockWidth, style.CodeBlockHt)
	case style.BlockStyle != 0:
		// Selective arithmetic bypass, termination on each pass, vertically
		// causal context, predictable termination, segmentation symbols. Each
		// changes how tier-1 reads a block, and none appears in the corpus.
		return &ErrUnsupported{
			Feature: fmt.Sprintf("code-block style 0x%02X", style.BlockStyle)}
	case style.Wavelet != Wavelet53Reversible && style.Wavelet != Wavelet97Irreversible:
		return &ErrUnsupported{Feature: fmt.Sprintf("wavelet transform %d", style.Wavelet)}
	case style.UsePrecincts:
		return &ErrUnsupported{Feature: "user-defined precinct sizes"}
	}
	return nil
}

func parseQCC(c *Codestream, s []byte) error {
	if len(c.Components) == 0 {
		return fmt.Errorf("jpeg2000: QCC before SIZ")
	}
	index, rest, err := componentIndex(c, s)
	if err != nil {
		return err
	}
	q, err := parseQuantization(rest, 0)
	if err != nil {
		return err
	}
	c.ComponentQuant[index] = q
	return nil
}

// parseQuantization reads a QCD or the tail of a QCC (Annex A.6.4).
func parseQuantization(s []byte, _ int) (Quantization, error) {
	if len(s) < 1 {
		return Quantization{}, fmt.Errorf("jpeg2000: quantization segment is empty")
	}
	q := Quantization{
		Style:     int(s[0] & 0x1F),
		GuardBits: int(s[0] >> 5),
	}
	values := s[1:]

	switch q.Style {
	case QuantNone:
		// One byte per subband: the exponent in the top five bits.
		for _, b := range values {
			q.Exponents = append(q.Exponents, int(b>>3))
			q.Mantissas = append(q.Mantissas, 0)
		}
	case QuantDerived, QuantExpounded:
		// Two bytes per subband: five bits of exponent, eleven of mantissa.
		for i := 0; i+1 < len(values); i += 2 {
			v := be16(values[i:])
			q.Exponents = append(q.Exponents, int(v>>11))
			q.Mantissas = append(q.Mantissas, int(v&0x07FF))
		}
	default:
		return Quantization{}, &ErrUnsupported{
			Feature: fmt.Sprintf("quantization style %d", q.Style)}
	}
	if len(q.Exponents) == 0 {
		return Quantization{}, fmt.Errorf("jpeg2000: quantization segment carries no values")
	}
	return q, nil
}

// componentIndex reads the component number that prefixes a COC or QCC, which
// is one byte when there are fewer than 257 components and two above that.
func componentIndex(c *Codestream, s []byte) (int, []byte, error) {
	if len(c.Components) < 257 {
		if len(s) < 1 {
			return 0, nil, fmt.Errorf("jpeg2000: component index is missing")
		}
		return int(s[0]), s[1:], nil
	}
	if len(s) < 2 {
		return 0, nil, fmt.Errorf("jpeg2000: component index is missing")
	}
	return int(be16(s)), s[2:], nil
}

// parseTilePart reads one SOT segment and the data after its SOD, returning the
// offset of the marker that follows.
func parseTilePart(c *Codestream, data []byte, pos int) (int, error) {
	if pos+10 > len(data) {
		return 0, fmt.Errorf("jpeg2000: truncated SOT segment")
	}
	sotLength := int(be16(data[pos:]))
	if sotLength != 10 {
		return 0, fmt.Errorf("jpeg2000: SOT is %d bytes, want 10", sotLength)
	}
	tile := int(be16(data[pos+2:]))
	psot := int(be32(data[pos+4:]))
	partIndex := int(data[pos+8])

	start := pos - 2 // where the SOT marker itself began
	end := len(data)
	if psot != 0 {
		// Psot counts from the SOT marker to the end of the tile-part. Zero
		// means "the rest of the codestream", which the standard allows for the
		// last tile-part. The smallest honest tile-part is the SOT segment (12
		// bytes with its marker) and an SOD marker; anything less would place
		// the end of the part before its own header, which a fuzzed Psot of 1
		// did.
		const smallestTilePart = 2 + 10 + 2
		if psot < smallestTilePart {
			return 0, fmt.Errorf("jpeg2000: tile-part %d declares %d bytes, too few to hold its header",
				tile, psot)
		}
		if start+psot > len(data) {
			return 0, fmt.Errorf("jpeg2000: tile-part %d declares %d bytes, %d remain",
				tile, psot, len(data)-start)
		}
		end = start + psot
	}

	// Between the SOT segment and SOD there may be tile-part header segments.
	cursor := pos + sotLength
	for cursor+1 < end {
		marker := be16(data[cursor:])
		if marker == markerSOD {
			cursor += 2
			break
		}
		if cursor+4 > end {
			return 0, fmt.Errorf("jpeg2000: truncated tile-part header in tile %d", tile)
		}
		length := int(be16(data[cursor+2:]))
		if length < 2 || cursor+2+length > end {
			return 0, fmt.Errorf("jpeg2000: tile-part header segment %04X declares %d bytes",
				marker, length)
		}
		segment := data[cursor+4 : cursor+2+length]

		var err error
		switch marker {
		case markerCOD:
			// A tile-part may restate the coding style for its own tile, and
			// the tile's components inherit it.
			var style CodingStyle
			if style, err = parseCOD(segment); err == nil {
				c.TileCoding[tile] = style
			}
		case markerCOC:
			err = parseTileCOC(c, tile, segment)
		case markerQCD:
			var q Quantization
			if q, err = parseQuantization(segment, 0); err == nil {
				c.TileQuant[tile] = q
			}
		case markerQCC:
			err = parseTileQCC(c, tile, segment)
		case markerPPT:
			err = &ErrUnsupported{Feature: "packed packet headers (PPT)"}
		case markerCOM, markerPLT:
			// Nothing to decode.
		default:
			err = fmt.Errorf("jpeg2000: unexpected marker %04X in tile-part %d", marker, tile)
		}
		if err != nil {
			return 0, err
		}
		cursor += 2 + length
	}

	if tile >= maxTiles {
		return 0, fmt.Errorf("jpeg2000: tile index %d", tile)
	}
	// The header scan above stops at SOD or at the end of the part. If it ran
	// past the end, the part declared less than its own header occupies.
	if cursor > end {
		return 0, fmt.Errorf("jpeg2000: tile-part %d ends before its header does", tile)
	}
	c.TileParts = append(c.TileParts, TilePart{
		Tile:  tile,
		Index: partIndex,
		Data:  data[cursor:end],
	})
	return end, nil
}

// maxTiles bounds the tile index a file may name, so a corrupt index cannot
// drive an allocation. 65535 is the standard's own limit.
const maxTiles = 65535

// validate checks that the header carried what a decoder needs.
func validate(c *Codestream, seenSIZ, seenCOD, seenQCD bool) error {
	switch {
	case !seenSIZ:
		return fmt.Errorf("jpeg2000: codestream has no SIZ segment")
	case !seenCOD:
		return fmt.Errorf("jpeg2000: codestream has no COD segment")
	case !seenQCD:
		return fmt.Errorf("jpeg2000: codestream has no QCD segment")
	case len(c.TileParts) == 0:
		return fmt.Errorf("jpeg2000: codestream has no tile-parts")
	}
	if c.Coding.MCT && len(c.Components) < 3 {
		return fmt.Errorf("jpeg2000: the multiple component transform needs three components, not %d",
			len(c.Components))
	}
	return nil
}

func be16(b []byte) int {
	return int(binary.BigEndian.Uint16(b))
}

func be32(b []byte) uint32 {
	return binary.BigEndian.Uint32(b)
}
