package jpeg2000

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDecodeBlockAgainstOpenJPEG is the known-answer test for tier-1: decode
// real encoder output and require the coefficients to be the ones that went in.
//
// The usual difficulty with testing tier-1 on its own is that a code-block's
// coefficients are wavelet coefficients, and nothing outside the decoder ever
// sees them — OpenJPEG hands back pixels, several stages later. The way around
// it is a codestream with **no decomposition at all**: one resolution level,
// reversible, one component. Then the single LL band is the image itself, the
// quantization is a shift by zero, and the coefficient at (x, y) is the pixel
// less the DC level shift of 128. Any error in the MQ decoder, the pass order,
// the context tables, the sign coding or the bit planes shows up as a wrong
// number here, with no DWT in between to blame.
//
// The three fixtures were produced by OpenJPEG from images this test rebuilds:
//
//	opj_compress -i <image>.pgm -o <image>.j2k -n 1
//
// and each stresses a different part of the coder. Noise makes almost every
// coefficient significant and reaches every bit plane. The gradient is smooth,
// so most of the work is refinement of a few large values. The sparse image is
// eight spikes in a field of zeros, which is the run-length mode of the cleanup
// pass doing nearly all the decoding — 170 bytes for 1024 samples.
func TestDecodeBlockAgainstOpenJPEG(t *testing.T) {
	for _, fixture := range []string{"noise", "gradient", "sparse"} {
		t.Run(fixture, func(t *testing.T) {
			want := referenceImage(fixture, 32)

			raw, err := os.ReadFile(filepath.Join("testdata", fixture+".j2k"))
			if err != nil {
				t.Fatal(err)
			}
			c, err := ParseCodestream(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := c.CodingFor(0).Levels; got != 0 {
				t.Fatalf("the fixture has %d decomposition levels, so its "+
					"coefficients are not the image and this test proves nothing", got)
			}

			got := decodeSingleBand(t, c)
			for i := range want {
				// The DC level shift: an 8-bit unsigned component is coded
				// centered on zero (G.1.2).
				if expect := int32(want[i] - 128); got[i] != expect {
					t.Fatalf("sample %d (%d, %d) decoded as %d, want %d",
						i, i%32, i/32, got[i], expect)
				}
			}
		})
	}
}

// decodeSingleBand decodes every code-block of the one band of a codestream
// with no decomposition, and lays the samples back out in raster order.
func decodeSingleBand(t *testing.T, c *Codestream) []int32 {
	t.Helper()

	tc, err := c.TileComponentFor(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	band := tc.Resolutions[0].Bands[0]
	w, h := band.Width(), band.Height()
	out := make([]int32, w*h)

	data, err := ReadTilePacketsForTest(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Blocks) == 0 {
		t.Fatal("the tile decoded to no code-blocks at all")
	}

	for _, block := range data.Blocks {
		bits, err := MagnitudeBits(c.QuantForTile(0, block.Component), band,
			block.Resolution, c.CodingForTile(0, block.Component).Levels)
		if err != nil {
			t.Fatal(err)
		}
		samples, err := DecodeBlock(block, bits)
		if err != nil {
			t.Fatalf("decoding a %dx%d block: %v",
				block.Block.Width(), block.Block.Height(), err)
		}

		bw := block.Block.Width()
		for y := 0; y < block.Block.Height(); y++ {
			for x := 0; x < bw; x++ {
				row := block.Block.Y0 - band.Y0 + y
				col := block.Block.X0 - band.X0 + x
				out[row*w+col] = samples[y*bw+x]
			}
		}
	}
	return out
}

// referenceImage rebuilds the image a fixture was compressed from. The
// generator is deterministic so the expected values need not be committed
// beside the codestream: if these two ever disagree the test fails loudly
// rather than comparing a decode against itself.
func referenceImage(name string, n int) []int {
	px := make([]int, n*n)
	switch name {
	case "noise":
		seed := uint64(12345)
		for i := range px {
			seed = (seed*1103515245 + 12345) & 0x7FFFFFFF
			px[i] = int((seed >> 16) & 0xFF)
		}
	case "gradient":
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				px[y*n+x] = (x*4 + y*3) & 0xFF
			}
		}
	case "sparse":
		for k := 0; k < 8; k++ {
			px[(k*137)%(n*n)] = 200 + k
		}
	}
	return px
}

// TestSignificanceContextTable checks the context a significance bit is coded
// in, for every arrangement of the eight neighbors and all four band types —
// 256 configurations each, against Table D.1 written out as its own rules.
//
// The table is the one place where a plausible-looking decoder goes wrong
// quietly: a mis-assigned context does not crash and does not desynchronize
// immediately, it just codes against the wrong probability, and the image comes
// out subtly wrong in the busy parts. Checking the rows directly is cheaper
// than finding that through a pixel comparison.
func TestSignificanceContextTable(t *testing.T) {
	for _, band := range []int{BandLL, BandHL, BandLH, BandHH} {
		for mask := 0; mask < 256; mask++ {
			d := newBlockDecoder(3, 3, band, nil)
			// Bits 0..7 are the neighbors of the center, in reading order.
			offsets := [8][2]int{
				{-1, -1}, {0, -1}, {1, -1},
				{-1, 0}, {1, 0},
				{-1, 1}, {0, 1}, {1, 1},
			}
			var h, v, diag int
			for bit, off := range offsets {
				if mask&(1<<bit) == 0 {
					continue
				}
				// Making the neighbor significant is what writes the center's
				// neighbor bits, so this exercises the path a decode takes
				// rather than setting the bits by hand.
				d.setSignificant(1+off[0], 1+off[1], d.index(1+off[0], 1+off[1]), 0)
				switch {
				case off[0] != 0 && off[1] != 0:
					diag++
				case off[1] == 0:
					h++
				default:
					v++
				}
			}

			want := tableD1(band, h, v, diag)
			if got := d.significanceContext(d.index(1, 1)); got != want {
				t.Fatalf("band %d with h=%d v=%d d=%d: context %d, want %d",
					band, h, v, diag, got, want)
			}
		}
	}
}

// tableD1 is Table D.1 transcribed as ranges, independently of the decoder's
// own branches: each row gives the span of each neighbor count it covers, and
// the first row that matches wins. Written this way a row cannot quietly mean
// "exactly one" where the table says "one or more".
func tableD1(band, h, v, d int) int {
	if band == BandHL {
		h, v = v, h // the HL band's table is the same one transposed
	}
	const more = 99 // the open end of a "or more" range

	type row struct {
		hLo, hHi int
		vLo, vHi int
		dLo, dHi int
		cx       int
	}
	var rows []row
	if band == BandHH {
		// The HH table is read on the diagonals first, with the horizontal and
		// vertical neighbors counted together.
		h, v = h+v, 0
		rows = []row{
			{0, more, 0, 0, 3, more, 8},
			{1, more, 0, 0, 2, 2, 7},
			{0, 0, 0, 0, 2, 2, 6},
			{2, more, 0, 0, 1, 1, 5},
			{1, 1, 0, 0, 1, 1, 4},
			{0, 0, 0, 0, 1, 1, 3},
			{2, more, 0, 0, 0, 0, 2},
			{1, 1, 0, 0, 0, 0, 1},
			{0, 0, 0, 0, 0, 0, 0},
		}
	} else {
		rows = []row{
			{2, 2, 0, more, 0, more, 8},
			{1, 1, 1, more, 0, more, 7},
			{1, 1, 0, 0, 1, more, 6},
			{1, 1, 0, 0, 0, 0, 5},
			{0, 0, 2, 2, 0, more, 4},
			{0, 0, 1, 1, 0, more, 3},
			{0, 0, 0, 0, 2, more, 2},
			{0, 0, 0, 0, 1, 1, 1},
			{0, 0, 0, 0, 0, 0, 0},
		}
	}

	for _, r := range rows {
		if h >= r.hLo && h <= r.hHi && v >= r.vLo && v <= r.vHi &&
			d >= r.dLo && d <= r.dHi {
			return r.cx
		}
	}
	panic("Table D.1 has no row for this arrangement of neighbors")
}

// TestSignContextTable checks the sign contexts and the XOR bit against
// Table D.3 written out in full: nine rows, each a pair of neighbor votes.
//
// It also checks the thing that went wrong once: the table is the same for
// every subband. Unlike the significance context, the sign context is not
// transposed in the HL band, so all four band types must answer alike for the
// same arrangement of neighbors.
func TestSignContextTable(t *testing.T) {
	want := map[[2]int][2]int{
		{1, 1}: {13, 0}, {1, 0}: {12, 0}, {1, -1}: {11, 0},
		{0, 1}: {10, 0}, {0, 0}: {9, 0}, {0, -1}: {10, 1},
		{-1, 1}: {11, 1}, {-1, 0}: {12, 1}, {-1, -1}: {13, 1},
	}

	for _, band := range []int{BandLL, BandHL, BandLH, BandHH} {
		for pair, expect := range want {
			d := newBlockDecoder(3, 3, band, nil)
			setVote(d, 0, 1, pair[0]) // a horizontal neighbor
			setVote(d, 1, 0, pair[1]) // a vertical one

			cx, xor := d.signContext(d.index(1, 1))
			if cx != expect[0] || xor != expect[1] {
				t.Fatalf("band %d, h=%d v=%d: context %d xor %d, want %d and %d",
					band, pair[0], pair[1], cx, xor, expect[0], expect[1])
			}
		}
	}
}

// setVote makes a neighbor significant and positive or negative, or leaves it
// insignificant for a vote of zero.
func setVote(d *blockDecoder, x, y, vote int) {
	if vote == 0 {
		return
	}
	i := d.index(x, y)
	d.flags[i] |= flagSignificant
	if vote < 0 {
		d.flags[i] |= flagNegative
	}
}

// TestDecodeBlockRefusesImpossibleBlocks checks the bounds around a decode.
// The sizes come from a packet header, which is peer-controlled.
func TestDecodeBlockRefusesImpossibleBlocks(t *testing.T) {
	for _, c := range []struct {
		name  string
		block CodeBlockData
		bits  int
	}{
		{"no samples", CodeBlockData{Block: CodeBlock{X1: 0, Y1: 4}}, 9},
		{"enormous", CodeBlockData{Block: CodeBlock{X1: 1 << 13, Y1: 1 << 13}}, 9},
		{"past an int32", CodeBlockData{Block: CodeBlock{X1: 4, Y1: 4}}, 40},
		{
			"more passes than bit planes",
			// Two planes hold four passes: a cleanup, then three.
			CodeBlockData{Block: CodeBlock{X1: 4, Y1: 4}, ZeroBitPlanes: 7, Passes: 5},
			9,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeBlock(c.block, c.bits); err == nil {
				t.Fatal("decoded a block that cannot exist")
			}
		})
	}
}

// TestDecodeBlockOfAllZeroPlanes is the block whose every plane the header
// declared zero: no passes are possible and every coefficient is zero. It is
// worth its own case because the plane index goes negative, and a decoder that
// did not check would index a shift of -1.
func TestDecodeBlockOfAllZeroPlanes(t *testing.T) {
	samples, err := DecodeBlock(CodeBlockData{
		Block:         CodeBlock{X1: 4, Y1: 4},
		ZeroBitPlanes: 9,
		Passes:        1,
	}, 9)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range samples {
		if s != 0 {
			t.Fatalf("sample %d is %d in a block with no coded planes", i, s)
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
				data, err := ReadTilePacketsForTest(c, tile)
				if err != nil {
					t.Fatalf("tile %d: %v", tile, err)
				}
				for _, block := range data.Blocks {
					style := c.CodingForTile(tile, block.Component)
					tc := data.Components[block.Component]
					band := bandOf(tc.Resolutions[block.Resolution], block.BandType)
					bits, err := MagnitudeBits(c.QuantForTile(tile, block.Component),
						band, block.Resolution, style.Levels)
					if err != nil {
						t.Fatalf("tile %d: %v", tile, err)
					}

					values, err := DecodeBlock(block, bits)
					if err != nil {
						t.Fatalf("tile %d: %v", tile, err)
					}
					limit := int32(1) << uint(minInt(bits, 30))
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

// bandOf picks a resolution's band by type. Resolution 0 holds only LL.
func bandOf(res Resolution, bandType int) Band {
	for _, band := range res.Bands {
		if band.Type == bandType {
			return band
		}
	}
	return Band{}
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
				data, err := ReadTilePacketsForTest(c, tile)
				if err != nil {
					t.Fatalf("tile %d: %v", tile, err)
				}
				for _, block := range data.Blocks {
					bits := magnitudeBitsFor(t, c, data, tile, block)
					d, err := decodeBlock(block, bits)
					if err != nil {
						t.Fatalf("tile %d: %v", tile, err)
					}
					if d.mq.bp != len(block.Data) {
						t.Fatalf("tile %d: a %dx%d block of band %d at resolution %d "+
							"consumed %d of its %d bytes",
							tile, block.Block.Width(), block.Block.Height(),
							block.BandType, block.Resolution, d.mq.bp, len(block.Data))
					}
					checked++
				}
			}
			t.Logf("%d code-blocks, every one landing on its last byte", checked)
		})
	}
}

// magnitudeBitsFor is the Mb of the band a decoded block belongs to.
func magnitudeBitsFor(t testing.TB, c *Codestream, data *TileData, tile int, block CodeBlockData) int {
	t.Helper()
	res := data.Components[block.Component].Resolutions[block.Resolution]
	bits, err := MagnitudeBits(c.QuantForTile(tile, block.Component), bandOf(res, block.BandType),
		block.Resolution, c.CodingForTile(tile, block.Component).Levels)
	if err != nil {
		t.Fatal(err)
	}
	return bits
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
			data, err := ReadTilePacketsForTest(c, 0)
			if err != nil {
				b.Fatal(err)
			}

			type job struct {
				block CodeBlockData
				bits  int
			}
			jobs := make([]job, 0, len(data.Blocks))
			samples := 0
			for _, block := range data.Blocks {
				res := data.Components[block.Component].Resolutions[block.Resolution]
				bits, err := MagnitudeBits(c.QuantForTile(0, block.Component),
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
					if _, err := DecodeBlock(j.block, j.bits); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// TestMagnitudeBits checks Mb for both ways a codestream states quantization.
// The expounded style lists an exponent per subband; the derived style lists
// one and each band works its own out from how many decompositions separate it
// from the image (E-5). No corpus fixture uses the derived style, so without
// this its formula would go untested until a file in the field used it.
func TestMagnitudeBits(t *testing.T) {
	expounded := Quantization{
		Style: QuantExpounded, GuardBits: 2,
		Exponents: []int{16, 17, 17, 18, 8, 9, 10},
	}
	derived := Quantization{Style: QuantDerived, GuardBits: 2, Exponents: []int{16}}

	for _, c := range []struct {
		name       string
		quant      Quantization
		band       Band
		resolution int
		levels     int
		want       int
	}{
		{"expounded LL", expounded, Band{Type: BandLL, Level: 5}, 0, 5, 17},
		{"expounded HL of the first level", expounded, Band{Type: BandHL, Level: 5}, 1, 5, 18},
		{"expounded HH of the first level", expounded, Band{Type: BandHH, Level: 5}, 1, 5, 19},
		{"expounded HL of the second", expounded, Band{Type: BandHL, Level: 4}, 2, 5, 9},
		// Derived: the LL band keeps the signaled exponent, and each finer
		// resolution loses one.
		{"derived LL", derived, Band{Type: BandLL, Level: 5}, 0, 5, 17},
		{"derived at resolution 1", derived, Band{Type: BandHL, Level: 5}, 1, 5, 17},
		{"derived at resolution 5", derived, Band{Type: BandHH, Level: 1}, 5, 5, 13},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := MagnitudeBits(c.quant, c.band, c.resolution, c.levels)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("Mb is %d, want %d", got, c.want)
			}
		})
	}

	// A quantization that names fewer subbands than the codestream has is a
	// refusal, not a zero exponent: a missing value would shift a whole band.
	short := Quantization{Style: QuantExpounded, GuardBits: 2, Exponents: []int{16}}
	if _, err := MagnitudeBits(short, Band{Type: BandHH, Level: 1}, 3, 5); err == nil {
		t.Fatal("accepted a subband with no quantization exponent")
	}
}
