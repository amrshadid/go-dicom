package jpeg2000

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIrreversibleAgainstOpenJPEG decodes three 9/7 codestreams whose answers
// OpenJPEG wrote, chosen to separate the two things that can go wrong.
//
// The corpus has exactly one irreversible file, and a single failing comparison
// says only that something in the lossy path is wrong. These three say which:
//
//   - g97_n1 has no decomposition at all, so no filter runs. It tests
//     dequantization and rounding alone, and it found that samples must round
//     half to even: rounding half up moved 497 of its 1024 samples by one.
//   - g97_n2 adds a level of the filter, over a gradient.
//   - n97_n2 is the same but over noise, and it is the one that matters.
//     A gradient's detail coefficients are near zero, so it passes even with
//     the high-pass scaling wrong by a factor of two; on noise that error was
//     1006 of 1024 samples wrong, the worst by 102.
//
// Generated with `opj_compress -i <image>.pgm -o <name>.j2k -n <levels> -I -r 1`
// and decoded with `opj_decompress` for the expected samples.
func TestIrreversibleAgainstOpenJPEG(t *testing.T) {
	for _, c := range []struct {
		name   string
		levels int
	}{
		{"g97_n1", 0},
		{"g97_n2", 1},
		{"n97_n2", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", c.name+".j2k"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", c.name+".pgm.raw"))
			if err != nil {
				t.Fatal(err)
			}

			codestream, err := ParseCodestream(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := codestream.CodingFor(0).Wavelet; got != Wavelet97Irreversible {
				t.Fatalf("the fixture uses wavelet %d, not the irreversible one", got)
			}
			if got := codestream.CodingFor(0).Levels; got != c.levels {
				t.Fatalf("the fixture has %d decomposition levels, want %d", got, c.levels)
			}

			img, err := Decode(codestream)
			if err != nil {
				t.Fatal(err)
			}
			if len(img.Components) != 1 || len(img.Components[0]) != len(want) {
				t.Fatalf("decoded %d samples, want %d", len(img.Components[0]), len(want))
			}

			// One sample of the noise image lands the other side of a half from
			// OpenJPEG. The 9/7 is defined in real arithmetic, so that is
			// allowed; being wrong by more than one is not.
			wrong, worst := 0, 0
			for i := range want {
				if diff := abs(int(img.Components[0][i]) - int(want[i])); diff > 1 {
					wrong++
					if diff > worst {
						worst = diff
					}
				}
			}
			if wrong != 0 {
				t.Fatalf("%d of %d samples differ by more than one, worst by %d",
					wrong, len(want), worst)
			}
		})
	}
}

// TestReversibleFilterIsExact round trips the 5/3 filter over signals of every
// length and both origin parities.
//
// The forward transform here is written from the standard's own equations
// (F.4.8.1) and not from the decoder, so this says the two are inverses. It
// matters because a whole class of errors — an index off by one, an extension
// that reflects about the wrong sample, a shift that rounds towards zero
// instead of down — leaves a filter that still looks like a filter and is not
// reversible. Lossless JPEG 2000 rests on this being exact.
func TestReversibleFilterIsExact(t *testing.T) {
	seed := uint64(7)
	next := func() int32 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int32(seed>>33)%2001 - 1000
	}

	for length := 1; length <= 17; length++ {
		for _, i0 := range []int{0, 1, 6, 7, 100, 101} {
			i1 := i0 + length
			original := make([]float32, length)
			for i := range original {
				original[i] = float32(next())
			}

			coefficients := make([]float32, length)
			copy(coefficients, original)
			analyze53(coefficients, i0, i1, nil)

			// A transform that did nothing would round trip too, so make sure
			// there was one: over a random signal of any real length the
			// coefficients cannot be the samples.
			if length > 2 && sameSamples(coefficients, original) {
				t.Fatalf("length %d at origin %d: the forward transform changed nothing, "+
					"so the round trip below proves nothing", length, i0)
			}

			synthesize1D(coefficients, i0, i1, true, &scratch{})

			for i := range original {
				if coefficients[i] != original[i] {
					t.Fatalf("length %d at origin %d: sample %d came back as %v, not %v",
						length, i0, i, coefficients[i], original[i])
				}
			}
		}
	}
}

func sameSamples(a, b []float32) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// tileComponentOf builds the geometry of a tile-component covering a
// rectangle of the reference grid, with the given number of decomposition
// levels, as TileComponentFor does for one read from a codestream.
func tileComponentOf(x0, y0, x1, y1, levels int) TileComponent {
	tc := TileComponent{X0: x0, Y0: y0, X1: x1, Y1: y1}
	for r := 0; r <= levels; r++ {
		shift := levels - r
		tc.Resolutions = append(tc.Resolutions, Resolution{
			Index: r,
			X0:    ceilDivPow2(x0, shift), Y0: ceilDivPow2(y0, shift),
			X1: ceilDivPow2(x1, shift), Y1: ceilDivPow2(y1, shift),
			Bands: bandsOf(tc, r, levels),
		})
	}
	return tc
}

// TestForwardTransformIsInvertedExactly checks the forward transform against
// the inverse the decoder uses: random samples, decomposed and rebuilt, come
// back as they were, for sizes down to one sample, origins of either parity
// on each axis, and every number of levels up to more than the size allows.
// The origins matter: the parity of a coordinate decides which samples are
// low-pass, and an image is not always at the origin of the grid.
func TestForwardTransformIsInvertedExactly(t *testing.T) {
	seed := uint64(11)
	next := func() float32 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return float32(int32(seed>>33)%65536 - 32768)
	}
	sizes := [][2]int{{1, 1}, {1, 7}, {7, 1}, {2, 2}, {3, 5}, {8, 8}, {17, 13}, {64, 33}, {65, 64}}
	origins := [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {6, 9}, {101, 100}}
	for _, size := range sizes {
		for _, origin := range origins {
			for levels := 0; levels <= 6; levels++ {
				x0, y0 := origin[0], origin[1]
				tc := tileComponentOf(x0, y0, x0+size[0], y0+size[1], levels)
				original := make([]float32, size[0]*size[1])
				for i := range original {
					original[i] = next()
				}

				bands, err := forwardDWT(tc, original)
				if err != nil {
					t.Fatal(err)
				}
				// Every sample went into a band: the bands hold as many
				// coefficients as the image holds samples.
				held := 0
				for _, resolution := range bands {
					for _, band := range resolution {
						held += len(band.values)
					}
				}
				if held != len(original) {
					t.Fatalf("%dx%d at (%d,%d), %d levels: the bands hold %d coefficients for %d samples",
						size[0], size[1], x0, y0, levels, held, len(original))
				}

				rebuilt, err := inverseDWT(tc, bands, true)
				if err != nil {
					t.Fatalf("%dx%d at (%d,%d), %d levels: %v", size[0], size[1], x0, y0, levels, err)
				}
				if !sameSamples(rebuilt, original) {
					t.Fatalf("%dx%d at (%d,%d), %d levels: the samples did not come back",
						size[0], size[1], x0, y0, levels)
				}
				// A transform that did nothing would round trip too. Where
				// there is a low-pass band of any size, it is not the samples.
				if ll := bands[0][BandLL].values; levels > 0 && len(ll) > 2 && sameSamples(ll, original[:len(ll)]) {
					t.Fatalf("%dx%d at (%d,%d), %d levels: the transform changed nothing",
						size[0], size[1], x0, y0, levels)
				}
			}
		}
	}
}

// TestForwardTransformRefusesWhatItCannotHold checks the two inputs that
// are not a tile-component's samples.
func TestForwardTransformRefusesWhatItCannotHold(t *testing.T) {
	if _, err := forwardDWT(TileComponent{}, nil); err == nil {
		t.Error("a tile-component with no resolutions was decomposed")
	}
	if _, err := forwardDWT(tileComponentOf(0, 0, 4, 4, 1), make([]float32, 15)); err == nil {
		t.Error("15 samples were taken for a 4 by 4 tile-component")
	}
}
