package jpeg2000

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoneIrreversibleSampleIsNotHalved decodes opj_compress's 9/7 codestream
// of a flat 64x40 image of 200 in tiles of 48x40 over six resolutions, whose
// second tile reduces to one sample at an odd coordinate, and requires the
// samples OpenJPEG 2.5.4 and Apple's ImageIO (Kakadu) both decode it to: 200
// in the first tile and 164 in the second.
//
// This decoder halved that sample, as the 5/3 must, and returned 146. In this
// package's normalization of the 9/7, the same as OpenJPEG's, a detail
// coefficient is already half the standard's, so the halving was done twice
// (#549). 164 rather than 200 is OpenJPEG's encoder, which writes the sample
// at half; every decoder agrees on that.
func TestLoneIrreversibleSampleIsNotHalved(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "opj_narrow_edge_tile_irreversible.j2k"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCodestream(data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(c)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			want := int32(200)
			if x >= 48 {
				want = 164
			}
			if got := img.Components[0][y*img.Width+x]; got != want {
				t.Fatalf("sample (%d,%d) is %d; OpenJPEG and ImageIO decode %d", x, y, got, want)
			}
		}
	}
}

// TestIrreversibleTransformIsInverted checks the 9/7 forward transform
// against the inverse the decoder uses, for sizes down to one sample and
// origins of either parity: a lone sample at an odd coordinate, which neither
// scales, comes back as it went in, as every other does, within the float
// arithmetic of the filter.
func TestIrreversibleTransformIsInverted(t *testing.T) {
	seed := uint64(97)
	next := func() float32 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return float32(int32(seed>>33)%256 - 128)
	}
	sizes := [][2]int{{1, 1}, {1, 7}, {7, 1}, {2, 2}, {3, 5}, {17, 13}}
	origins := [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {3, 9}, {101, 100}}
	for _, size := range sizes {
		for _, origin := range origins {
			for levels := 0; levels <= 5; levels++ {
				x0, y0 := origin[0], origin[1]
				tc := tileComponentOf(x0, y0, x0+size[0], y0+size[1], levels)
				original := make([]float32, size[0]*size[1])
				for i := range original {
					original[i] = next()
				}
				bands, err := forwardDWTFilter(tc, append([]float32(nil), original...), true)
				if err != nil {
					t.Fatal(err)
				}
				rebuilt, err := inverseDWT(tc, bands, false)
				if err != nil {
					t.Fatal(err)
				}
				for i := range original {
					if d := rebuilt[i] - original[i]; d > 0.01 || d < -0.01 {
						t.Fatalf("%dx%d at (%d,%d), %d levels: sample %d came back %v, went in %v",
							size[0], size[1], x0, y0, levels, i, rebuilt[i], original[i])
					}
				}
			}
		}
	}
}
