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
			forward53(coefficients, i0, i1)

			// A transform that did nothing would round trip too, so make sure
			// there was one: over a random signal of any real length the
			// coefficients cannot be the samples.
			if length > 2 && sameSamples(coefficients, original) {
				t.Fatalf("length %d at origin %d: the forward transform changed nothing, "+
					"so the round trip below proves nothing", length, i0)
			}

			synthesize1D(coefficients, i0, i1, true)

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

// forward53 is the analysis half of the 5/3 filter (F.4.8.1), for the round
// trip above. It exists only in this test: the library never encodes.
func forward53(x []float32, i0, i1 int) {
	n := i1 - i0
	if n == 1 {
		if i0%2 != 0 {
			x[0] *= 2
		}
		return
	}

	const pad = 2
	buf := make([]int32, n+2*pad)
	for k := range buf {
		buf[k] = int32(extended(x, i0, i1, i0-pad+k))
	}
	at := func(i int) int { return i - i0 + pad }

	// The odd samples become the detail, then the even ones the average.
	odd := make(map[int]int32, n)
	for i := i0 - 1; i <= i1; i++ {
		if isEven(i) {
			continue
		}
		odd[i] = buf[at(i)] - ((buf[at(i-1)] + buf[at(i+1)]) >> 1)
	}
	for i := i0 - 1; i <= i1; i++ {
		if !isEven(i) {
			continue
		}
		left, right := odd[i-1], odd[i+1]
		if i-1 < i0-1 {
			left = odd[i+1]
		}
		if i+1 > i1 {
			right = odd[i-1]
		}
		buf[at(i)] += (left + right + 2) >> 2
	}
	for i := range odd {
		if i >= i0 && i < i1 {
			buf[at(i)] = odd[i]
		}
	}
	for k := 0; k < n; k++ {
		x[k] = float32(buf[pad+k])
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
