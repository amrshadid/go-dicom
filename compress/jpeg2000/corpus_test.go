package jpeg2000_test

// The tests that read the pydicom corpus.
//
// They are in the external test package because they have to be: reading a
// DICOM file means filereader, which reaches compress, which now registers this
// codec, so an internal test that opened a corpus file would be an import
// cycle. That is no loss. These tests see only the exported API, which is the
// API a caller has.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress/jpeg2000"
)

// fixtureFrames maps each JPEG 2000 fixture in the corpus to its first frame.
func fixtureFrames(t testing.TB, dir string) map[string][]byte {
	t.Helper()

	out := map[string][]byte{}
	for _, name := range []string{
		"MR_small_jp2klossless.dcm", "693_J2KI.dcm", "J2K_pixelrep_mismatch.dcm",
		"SC_rgb_gdcm_KY.dcm", "GDCMJ2K_TextGBR.dcm", "JPEG2000.dcm",
	} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out[name] = firstFrame(t, path)
	}
	if len(out) == 0 {
		t.Skip("no JPEG 2000 fixtures in this corpus")
	}
	return out
}

// fixtureCodestream parses one fixture's first frame.
func fixtureCodestream(t testing.TB, dir, name string) *jpeg2000.Codestream {
	t.Helper()

	c, err := jpeg2000.ParseCodestream(firstFrame(t, filepath.Join(dir, name)))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return c
}

// TestGeometryOfEveryFixture builds the geometry of every tile and component of
// the corpus files, and checks the same partition invariants on real
// parameters, including the multi-tile one.
func TestGeometryOfEveryFixture(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for name := range fixtureFrames(t, dir) {
		t.Run(name, func(t *testing.T) {
			c := fixtureCodestream(t, dir, name)
			total := 0
			for tile := 0; tile < c.NumTiles(); tile++ {
				for comp := range c.Components {
					tc, err := c.TileComponentFor(tile, comp)
					if err != nil {
						t.Fatalf("tile %d component %d: %v", tile, comp, err)
					}
					samples := 0
					for _, res := range tc.Resolutions {
						for _, band := range res.Bands {
							covered := 0
							for _, block := range band.Blocks {
								covered += block.Width() * block.Height()
							}
							if covered != band.Width()*band.Height() {
								t.Errorf("tile %d component %d resolution %d band %d: blocks cover %d of %d",
									tile, comp, res.Index, band.Type, covered, band.Width()*band.Height())
							}
							samples += band.Width() * band.Height()
						}
					}
					if samples != tc.Width()*tc.Height() {
						t.Errorf("tile %d component %d: bands hold %d samples, the tile has %d",
							tile, comp, samples, tc.Width()*tc.Height())
					}
					total += samples
				}
			}
			// Every sample of the image, once, across all tiles and components.
			if want := int(c.Width-c.XOffset) * int(c.Height-c.YOffset) * len(c.Components); total != want {
				t.Errorf("the tiles hold %d samples, the image has %d", total, want)
			}
		})
	}
}

// TestPacketsAccountForEveryTileByte is the check that says the packet reader
// understood the file: read every packet of every tile, and the headers plus
// the code-block segments must come to the tile's bytes, with nothing left over
// and nothing read twice.
//
// It is a strong check because the packet header is self-describing only if it
// is read correctly. One inclusion bit read in the wrong order, one length
// field the wrong width, one tag tree not reset between precincts, and the
// reader lands mid-segment: the next header is nonsense and the totals do not
// match. Getting to the last byte of a 132KB tile means every field in between
// was the width the standard says.
func TestPacketsAccountForEveryTileByte(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for name := range fixtureFrames(t, dir) {
		t.Run(name, func(t *testing.T) {
			c := fixtureCodestream(t, dir, name)

			totalBlocks, totalBytes := 0, 0
			for tile := 0; tile < c.NumTiles(); tile++ {
				var tileBytes int
				for _, part := range c.TileParts {
					if part.Tile == tile {
						tileBytes += len(part.Data)
					}
				}

				data, err := jpeg2000.ReadTilePacketsForTest(c, tile)
				if err != nil {
					t.Fatalf("tile %d: %v", tile, err)
				}

				used := 0
				for _, block := range data.Blocks {
					used += len(block.Data)
					if block.Passes <= 0 {
						t.Errorf("tile %d: a code-block carries %d passes", tile, block.Passes)
					}
					if block.ZeroBitPlanes < 0 || block.ZeroBitPlanes > 37 {
						t.Errorf("tile %d: a code-block claims %d zero bit planes",
							tile, block.ZeroBitPlanes)
					}
					if block.Block.Width() <= 0 || block.Block.Height() <= 0 {
						t.Errorf("tile %d: a code-block has no samples", tile)
					}
				}

				// Everything the tile holds is packets, and every packet was
				// read: the reader must finish on the last byte. Ending early
				// means a field somewhere was read at the wrong width and the
				// reader is adrift in the middle of a segment.
				//
				// A tile may end with padding a few bytes long, so a small
				// remainder is allowed; landing anywhere else is not.
				if data.BytesRead > tileBytes {
					t.Errorf("tile %d: the reader consumed %d bytes of a %d byte tile",
						tile, data.BytesRead, tileBytes)
				}
				if remainder := tileBytes - data.BytesRead; remainder > 2 {
					t.Errorf("tile %d: %d bytes left unread of %d; the reader lost its place",
						tile, remainder, tileBytes)
				}
				if used > tileBytes {
					t.Errorf("tile %d: code-block segments hold %d bytes, the tile has %d",
						tile, used, tileBytes)
				}
				totalBlocks += len(data.Blocks)
				totalBytes += used
			}
			t.Logf("%d code-blocks carrying %d bytes", totalBlocks, totalBytes)
			if totalBlocks == 0 {
				t.Error("no code-blocks were read")
			}
		})
	}
}

// TestPacketReaderRefusesATruncatedTile: a packet body that runs past the tile
// is an error, not a short read. The lengths come from the file.
func TestPacketReaderRefusesATruncatedTile(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	c := fixtureCodestream(t, dir, "MR_small_jp2klossless.dcm")

	for _, keep := range []int{1, 8, 64, 512} {
		if keep >= len(c.TileParts[0].Data) {
			continue
		}
		cut := *c
		cut.TileParts = []jpeg2000.TilePart{{
			Tile:  c.TileParts[0].Tile,
			Index: c.TileParts[0].Index,
			Data:  c.TileParts[0].Data[:keep],
		}}
		// Truncated data may decode into fewer blocks or fail; it must not
		// panic and must not claim bytes that are not there.
		data, err := jpeg2000.ReadTilePacketsForTest(&cut, 0)
		if err != nil {
			continue
		}
		used := 0
		for _, block := range data.Blocks {
			used += len(block.Data)
		}
		if used > keep {
			t.Errorf("with %d bytes the reader claimed %d", keep, used)
		}
	}
}

// TestDecodeEveryCorpusBlock decodes every code-block of every J2K fixture.
//
// It cannot check the values yet — that is stage 5, where the image comes out
// and pydicom says whether it is right. What it checks is that nothing in tier-1
// walks off the end of a real file's data, and that every coefficient fits the
// bit planes its band declared: a magnitude above 2^Mb means a plane index ran
// away, which is how a wrong pass order first shows itself.
func TestDecodeEveryCorpusBlock(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for name := range fixtureFrames(t, dir) {
		t.Run(name, func(t *testing.T) {
			c := fixtureCodestream(t, dir, name)
			blocks, samples := 0, 0

			for tile := 0; tile < c.NumTiles(); tile++ {
				data, err := jpeg2000.ReadTilePacketsForTest(c, tile)
				if err != nil {
					t.Fatalf("tile %d: %v", tile, err)
				}
				for _, block := range data.Blocks {
					style := c.CodingForTile(tile, block.Component)
					tc := data.Components[block.Component]
					band := bandOf(tc.Resolutions[block.Resolution], block.BandType)
					bits, err := jpeg2000.MagnitudeBits(c.QuantForTile(tile, block.Component),
						band, block.Resolution, style.Levels)
					if err != nil {
						t.Fatalf("tile %d: %v", tile, err)
					}

					values, err := jpeg2000.DecodeBlock(block, bits)
					if err != nil {
						t.Fatalf("tile %d: %v", tile, err)
					}
					limit := int32(1) << uint(min(bits, 30))
					for i, v := range values {
						if v >= limit || v <= -limit {
							t.Fatalf("tile %d: coefficient %d is %d, past the %d "+
								"bit planes its band holds", tile, i, v, bits)
						}
					}
					blocks++
					samples += len(values)
				}
			}
			t.Logf("%d code-blocks, %d coefficients", blocks, samples)
		})
	}
}

// TestBlocksConsumeExactlyTheirBytes is the structural check on tier-1: decode
// every code-block of every fixture and require the passes to land on the last
// byte of the block's data, neither short nor over.
//
// This is a sharper instrument than it looks. A code-block's data is one MQ
// stream with no internal framing: nothing marks where a pass ends, so the only
// thing that keeps the decoder in step is making exactly the decisions the
// encoder made, in exactly the same order and the same contexts. Get one
// context wrong and the arithmetic coder still returns bits — plausible ones —
// but the number of decisions drifts, and the stream is consumed early or late.
//
// It caught a real error. The sign context in D.3.2 is the same for every
// subband, but I had transposed it for HL the way the significance context is
// transposed. Every HL block then read short: 976 bytes consumed as 843 in
// MR_small, and the image would have come out wrong in every vertical edge.
// The known-answer test above could not see it — that fixture has only an LL
// band, where no transposition applies either way.
func TestBlocksConsumeExactlyTheirBytes(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for name := range fixtureFrames(t, dir) {
		t.Run(name, func(t *testing.T) {
			c := fixtureCodestream(t, dir, name)
			checked := 0

			for tile := 0; tile < c.NumTiles(); tile++ {
				data, err := jpeg2000.ReadTilePacketsForTest(c, tile)
				if err != nil {
					t.Fatalf("tile %d: %v", tile, err)
				}
				for _, block := range data.Blocks {
					bits := magnitudeBitsFor(t, c, data, tile, block)
					read, err := jpeg2000.DecodeBlockBytesReadForTest(block, bits)
					if err != nil {
						t.Fatalf("tile %d: %v", tile, err)
					}
					if read != len(block.Data) {
						t.Fatalf("tile %d: a %dx%d block of band %d at resolution %d "+
							"consumed %d of its %d bytes",
							tile, block.Block.Width(), block.Block.Height(),
							block.BandType, block.Resolution, read, len(block.Data))
					}
					checked++
				}
			}
			t.Logf("%d code-blocks, every one landing on its last byte", checked)
		})
	}
}

// BenchmarkDecodeBlocks measures tier-1 on a whole frame's code-blocks, which
// is where a decode spends most of its time: every coefficient of every band
// passes through the MQ coder one binary decision at a time.
func BenchmarkDecodeBlocks(b *testing.B) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		b.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for _, name := range []string{"MR_small_jp2klossless.dcm", "J2K_pixelrep_mismatch.dcm"} {
		b.Run(name, func(b *testing.B) {
			c := fixtureCodestream(b, dir, name)
			data, err := jpeg2000.ReadTilePacketsForTest(c, 0)
			if err != nil {
				b.Fatal(err)
			}

			type job struct {
				block jpeg2000.CodeBlockData
				bits  int
			}
			jobs := make([]job, 0, len(data.Blocks))
			samples := 0
			for _, block := range data.Blocks {
				res := data.Components[block.Component].Resolutions[block.Resolution]
				bits, err := jpeg2000.MagnitudeBits(c.QuantForTile(0, block.Component),
					bandOf(res, block.BandType), block.Resolution,
					c.CodingForTile(0, block.Component).Levels)
				if err != nil {
					b.Fatal(err)
				}
				jobs = append(jobs, job{block, bits})
				samples += block.Block.Width() * block.Block.Height()
			}

			b.SetBytes(int64(samples))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, j := range jobs {
					if _, err := jpeg2000.DecodeBlock(j.block, j.bits); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// magnitudeBitsFor is the Mb of the band a decoded block belongs to.
func magnitudeBitsFor(t testing.TB, c *jpeg2000.Codestream, data *jpeg2000.TileData, tile int, block jpeg2000.CodeBlockData) int {
	t.Helper()
	res := data.Components[block.Component].Resolutions[block.Resolution]
	bits, err := jpeg2000.MagnitudeBits(c.QuantForTile(tile, block.Component), bandOf(res, block.BandType),
		block.Resolution, c.CodingForTile(tile, block.Component).Levels)
	if err != nil {
		t.Fatal(err)
	}
	return bits
}

// bandOf picks a resolution's band by type. jpeg2000.Resolution 0 holds only LL.
func bandOf(res jpeg2000.Resolution, bandType int) jpeg2000.Band {
	for _, band := range res.Bands {
		if band.Type == bandType {
			return band
		}
	}
	return jpeg2000.Band{}
}
