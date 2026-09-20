package jpeg2000

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// TestTileGeometryPartitionsTheImage checks the structural facts the packet
// reader depends on. The wavelet rearranges samples, it does not create or lose
// them, so the bands of every resolution together account for exactly the
// tile-component's samples; and the code-blocks of a band cover the band
// exactly once.
//
// These are the invariants a geometry bug breaks, and they hold whatever the
// image size, which is what makes them worth asserting over a range of awkward
// sizes rather than one convenient one.
func TestTileGeometryPartitionsTheImage(t *testing.T) {
	sizes := []struct{ w, h, levels, cb int }{
		{64, 64, 5, 64},   // MR_small_jp2klossless
		{512, 512, 5, 64}, // 693_J2KI, J2K_pixelrep_mismatch
		{100, 100, 5, 64}, // SC_rgb_gdcm_KY: not a power of two
		{128, 128, 5, 32}, // one tile of GDCMJ2K_TextGBR
		{256, 1024, 5, 64},
		{1, 1, 1, 64},    // degenerate
		{3, 7, 3, 16},    // smaller than the code-blocks and not divisible
		{255, 129, 4, 8}, // many code-blocks per band
	}

	for _, size := range sizes {
		c := &Codestream{
			Width: uint32(size.w), Height: uint32(size.h),
			TileWidth: uint32(size.w), TileHeight: uint32(size.h),
			Components:      []Component{{Depth: 16, DX: 1, DY: 1}},
			Coding:          CodingStyle{Levels: size.levels, CodeBlockWidth: size.cb, CodeBlockHt: size.cb, Layers: 1},
			ComponentCoding: map[int]CodingStyle{}, ComponentQuant: map[int]Quantization{},
			TileCoding: map[int]CodingStyle{}, TileQuant: map[int]Quantization{},
			TileComponentCoding: map[TileComponentKey]CodingStyle{},
			TileComponentQuant:  map[TileComponentKey]Quantization{},
		}

		tc, err := c.TileComponentFor(0, 0)
		if err != nil {
			t.Fatalf("%dx%d: %v", size.w, size.h, err)
		}
		if len(tc.Resolutions) != size.levels+1 {
			t.Errorf("%dx%d: %d resolutions, want %d", size.w, size.h, len(tc.Resolutions), size.levels+1)
		}

		samples := 0
		for _, res := range tc.Resolutions {
			for _, band := range res.Bands {
				samples += band.Width() * band.Height()

				// The code-blocks cover the band exactly: no gap, no overlap.
				covered := 0
				for _, block := range band.Blocks {
					if block.Width() <= 0 || block.Height() <= 0 {
						t.Errorf("%dx%d: empty code-block in band %d of resolution %d",
							size.w, size.h, band.Type, res.Index)
					}
					if block.X0 < band.X0 || block.Y0 < band.Y0 || block.X1 > band.X1 || block.Y1 > band.Y1 {
						t.Errorf("%dx%d: code-block %v escapes its band %v",
							size.w, size.h, block, band)
					}
					covered += block.Width() * block.Height()
				}
				if want := band.Width() * band.Height(); covered != want {
					t.Errorf("%dx%d: band %d of resolution %d is %d samples, its code-blocks cover %d",
						size.w, size.h, band.Type, res.Index, want, covered)
				}
			}
		}
		if want := tc.Width() * tc.Height(); samples != want {
			t.Errorf("%dx%d: the bands hold %d samples, the tile-component has %d",
				size.w, size.h, samples, want)
		}
	}
}

// TestTileGeometryOfTheSimplestFixture spells out the arithmetic for one file,
// so a reader can check it against Annex B by hand rather than trusting the
// invariants above. MR_small_jp2klossless is 64x64 with five decomposition
// levels and 64x64 code-blocks.
func TestTileGeometryOfTheSimplestFixture(t *testing.T) {
	c := &Codestream{
		Width: 64, Height: 64, TileWidth: 64, TileHeight: 64,
		Components:      []Component{{Depth: 16, Signed: true, DX: 1, DY: 1}},
		Coding:          CodingStyle{Levels: 5, CodeBlockWidth: 64, CodeBlockHt: 64, Layers: 1},
		ComponentCoding: map[int]CodingStyle{}, ComponentQuant: map[int]Quantization{},
		TileCoding: map[int]CodingStyle{}, TileQuant: map[int]Quantization{},
		TileComponentCoding: map[TileComponentKey]CodingStyle{},
		TileComponentQuant:  map[TileComponentKey]Quantization{},
	}
	tc, err := c.TileComponentFor(0, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Resolution 0 is the LL band after five decompositions: 64 >> 5 = 2.
	// Each resolution above doubles, and its three bands are the size of the
	// resolution below it.
	wantBands := []struct {
		resolution int
		bands      int
		size       int
	}{
		{0, 1, 2}, {1, 3, 2}, {2, 3, 4}, {3, 3, 8}, {4, 3, 16}, {5, 3, 32},
	}
	for _, want := range wantBands {
		res := tc.Resolutions[want.resolution]
		if len(res.Bands) != want.bands {
			t.Errorf("resolution %d has %d bands, want %d", want.resolution, len(res.Bands), want.bands)
			continue
		}
		for _, band := range res.Bands {
			if band.Width() != want.size || band.Height() != want.size {
				t.Errorf("resolution %d band %d is %dx%d, want %dx%d",
					want.resolution, band.Type, band.Width(), band.Height(), want.size, want.size)
			}
			// Every band here is smaller than one 64x64 code-block.
			if len(band.Blocks) != 1 {
				t.Errorf("resolution %d band %d has %d code-blocks, want 1",
					want.resolution, band.Type, len(band.Blocks))
			}
		}
		// One precinct per resolution at the default size of 2^15.
		if res.Precincts() != 1 {
			t.Errorf("resolution %d has %d precincts, want 1", want.resolution, res.Precincts())
		}
	}
}

// TestTileBoundsCoverTheImageExactly: tiles partition the image, so their areas
// sum to it and none overlaps. GDCMJ2K_TextGBR is 400x400 in 128x128 tiles,
// which leaves a 16-pixel strip at two edges — the case an off-by-one hides in.
func TestTileBoundsCoverTheImageExactly(t *testing.T) {
	c := &Codestream{
		Width: 400, Height: 400, TileWidth: 128, TileHeight: 128,
		Components: []Component{{Depth: 8, DX: 1, DY: 1}},
	}
	if got := c.NumTiles(); got != 16 {
		t.Fatalf("%d tiles, want 16", got)
	}

	area := 0
	seen := map[[2]int]bool{}
	for tile := 0; tile < c.NumTiles(); tile++ {
		x0, y0, x1, y1 := c.TileBounds(tile)
		if x1 <= x0 || y1 <= y0 {
			t.Fatalf("tile %d is empty: %d,%d to %d,%d", tile, x0, y0, x1, y1)
		}
		area += (x1 - x0) * (y1 - y0)
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if seen[[2]int{x, y}] {
					t.Fatalf("tile %d covers %d,%d twice", tile, x, y)
				}
				seen[[2]int{x, y}] = true
			}
		}
	}
	if area != 400*400 {
		t.Errorf("the tiles cover %d samples, the image has %d", area, 400*400)
	}
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

// fixtureFrames maps each JPEG 2000 fixture in the corpus to its first frame.
func fixtureFrames(t testing.TB, dir string) map[string][]byte {
	t.Helper()

	names := []string{
		"MR_small_jp2klossless.dcm", "693_J2KI.dcm", "J2K_pixelrep_mismatch.dcm",
		"SC_rgb_gdcm_KY.dcm", "GDCMJ2K_TextGBR.dcm", "JPEG2000.dcm",
	}
	out := map[string][]byte{}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		elem, ok := df.GetDataset().Get(tag.New(0x7FE0, 0x0010))
		if !ok {
			continue
		}
		pixels, _ := elem.GetValue().([]byte)
		frames, errs := compress.GenerateFrames(bytes.NewReader(pixels), 1, "<")
		select {
		case frame := <-frames:
			out[name] = frame
		case err := <-errs:
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(out) == 0 {
		t.Skip("no JPEG 2000 fixtures in this corpus")
	}
	return out
}

// fixtureCodestream parses one fixture's first frame.
func fixtureCodestream(t testing.TB, dir, name string) *Codestream {
	t.Helper()

	frame := fixtureFrames(t, dir)[name]
	c, err := ParseCodestream(frame)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return c
}
