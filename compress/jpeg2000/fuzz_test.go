package jpeg2000_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress/jpeg2000"
)

// FuzzDecodeBlockStyles runs tier-1 under every code-block style option with
// arbitrary data, pass counts and codeword segments. FuzzParseCodestream
// reaches these only through a packet header that parses, which constrains
// the segments to what an encoder writes; here a segment can claim any number
// of passes and bytes, and a raw pass can land in a segment opened for MQ.
// Any error is fine; a panic, or coefficients that do not fit the block, is
// not.
func FuzzDecodeBlockStyles(f *testing.F) {
	f.Add([]byte{0x00, 0x11, 0x22, 0xFF, 0x7F, 0xFF, 0x90}, byte(0x3F), uint8(30), uint8(5), uint8(16), uint8(4))
	f.Add([]byte{0xFF, 0xFF, 0xFF}, byte(0x01), uint8(19), uint8(0), uint8(32), uint8(3))
	f.Add([]byte{0x80, 0x00}, byte(0x04), uint8(7), uint8(9), uint8(1), uint8(0))

	f.Fuzz(func(t *testing.T, data []byte, style byte, passes, zero, size, split uint8) {
		w, h := int(size%65)+1, int(size/3%65)+1
		block := jpeg2000.CodeBlockData{
			Block:         jpeg2000.CodeBlock{X0: 0, Y0: 0, X1: w, Y1: h},
			BandType:      int(split % 4),
			ZeroBitPlanes: int(zero % 20),
			Passes:        int(passes % 90),
			Data:          data,
			Style:         style & 0x3F,
		}
		// Up to four segments, cutting the data and the passes anywhere.
		for k := 0; k < int(split%5) && len(data) > 0; k++ {
			block.Segments = append(block.Segments, jpeg2000.CodewordSegment{
				Passes: int(passes)%(k+3) + 1,
				Length: len(data) * (k + 1) / (int(split%5) + 1) / (k + 1),
			})
		}
		values, err := jpeg2000.DecodeBlock(block, 20)
		if err != nil {
			return
		}
		if len(values) != w*h {
			t.Fatalf("a %dx%d block decoded to %d values", w, h, len(values))
		}
		for _, v := range values {
			if v >= 1<<21 || v <= -(1<<21) {
				t.Fatalf("a coefficient of %d from 20 magnitude bits", v)
			}
		}
	})
}

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

	// And whole codestreams: the seeds above all stop in the main header, so
	// without these the fuzzer rarely reaches a tile.
	paths, _ := filepath.Glob("testdata/*.j2k")
	for _, path := range paths {
		if data, err := os.ReadFile(path); err == nil {
			f.Add(data)
		}
	}

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

		// And the whole decode, which is what a caller will actually run: every
		// length, index and allocation from here to the pixels comes out of
		// this file. Any error is fine; a panic is not, and neither is an image
		// whose planes are not the size it claims.
		if img, err := jpeg2000.Decode(c); err == nil {
			if img.Width <= 0 || img.Height <= 0 {
				t.Fatalf("decoded an image of %dx%d", img.Width, img.Height)
			}
			for i, plane := range img.Components {
				if len(plane) != img.Width*img.Height {
					t.Fatalf("component %d has %d samples for a %dx%d image",
						i, len(plane), img.Width, img.Height)
				}
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
