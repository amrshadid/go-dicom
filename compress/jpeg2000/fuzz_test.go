package jpeg2000_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/compress/jpeg2000"
)

// FuzzParseCodestream runs the parser against arbitrary bytes. Every length in
// a codestream comes from the file, and this file came from a peer: a segment
// length, a component count, a tile index or a Psot can each point past the end
// or drive an allocation. The parser may return any error it likes; it may not
// panic and it may not hang.
func FuzzParseCodestream(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0xFF, 0x4F})                                     // SOC alone
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x29})             // SOC, a SIZ that stops
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x90, 0x00, 0x0A, 0, 0, 0xFF}) // a tile-part that stops
	f.Add([]byte{0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20}) // a JP2 signature box

	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := jpeg2000.ParseCodestream(data)
		if err != nil {
			return
		}
		// A codestream that parsed must describe something coherent, since the
		// decoder that follows will size its buffers from these numbers.
		if c.Width == 0 || c.Height == 0 {
			t.Fatalf("parsed a codestream of %dx%d", c.Width, c.Height)
		}
		if len(c.Components) == 0 {
			t.Fatal("parsed a codestream with no components")
		}
		if c.NumTiles() <= 0 {
			t.Fatalf("parsed a codestream with %d tiles", c.NumTiles())
		}
		for _, comp := range c.Components {
			if comp.Depth < 1 || comp.Depth > 16 {
				t.Fatalf("component depth %d", comp.Depth)
			}
		}

		// And the packets, which is where the lengths a file controls turn into
		// slices: a segment length, a pass count, a tag tree value. Any error is
		// fine; a panic or a claim on bytes that are not there is not.
		for tile := 0; tile < c.NumTiles() && tile < 4; tile++ {
			data, err := jpeg2000.ReadTilePacketsForTest(c, tile)
			if err != nil {
				continue
			}
			used := 0
			for _, block := range data.Blocks {
				used += len(block.Data)
			}
			if used > data.TileBytes {
				t.Fatalf("tile %d: code-blocks claim %d bytes of a %d byte tile",
					tile, used, data.TileBytes)
			}

			// And tier-1, which turns those bytes into coefficients. Its loop
			// bounds are a pass count and a bit plane, both from the header, and
			// both able to send a shift or an index out of range.
			for _, block := range data.Blocks {
				res := data.Components[block.Component].Resolutions[block.Resolution]
				var band jpeg2000.Band
				for _, candidate := range res.Bands {
					if candidate.Type == block.BandType {
						band = candidate
					}
				}
				bits, err := jpeg2000.MagnitudeBits(c.QuantForTile(tile, block.Component),
					band, block.Resolution, c.CodingForTile(tile, block.Component).Levels)
				if err != nil {
					continue
				}
				samples, err := jpeg2000.DecodeBlock(block, bits)
				if err != nil {
					continue
				}
				if want := block.Block.Width() * block.Block.Height(); len(samples) != want {
					t.Fatalf("a %dx%d block decoded to %d samples",
						block.Block.Width(), block.Block.Height(), len(samples))
				}
			}
		}
	})
}
