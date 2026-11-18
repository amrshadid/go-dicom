package jpeg2000

import (
	"math"
	"math/rand"
	"os/exec"
	"testing"
)

// The lossy encoder is held to what a caller is promised: the ratio met, the
// PSNR met, and a codestream OpenJPEG reads to the samples this package's
// decoder reads. Its own decoder agreeing is necessary; OpenJPEG agreeing is
// what shows the codestream is JPEG 2000 and not merely something this
// package accepts.

// smoothImage is a smooth field with texture, the content lossy coding is
// for, centered for a signed image.
func smoothImage(w, h, components, depth int, signed bool) *Image {
	maxVal := float64(int(1)<<uint(depth) - 1)
	r := rand.New(rand.NewSource(int64(w*h + depth)))
	return testImage(w, h, components, depth, signed, func(c, x, y int) int32 {
		v := (math.Sin(float64(x)/11+float64(c)) + math.Cos(float64(y)/7) + 2) / 4 * maxVal
		v += r.NormFloat64() * maxVal / 400
		v = math.Max(0, math.Min(maxVal, math.Round(v)))
		if signed {
			v -= float64(int(1) << uint(depth-1))
		}
		return int32(v)
	})
}

// psnrImages is the PSNR of got against want over every sample.
func psnrImages(want, got *Image) float64 {
	var sum float64
	n := 0
	for c := range want.Components {
		for i, v := range want.Components[c] {
			d := float64(v - got.Components[c][i])
			sum += d * d
			n++
		}
	}
	if sum == 0 {
		return math.Inf(1)
	}
	peak := float64(int(1)<<uint(want.Depths[0]) - 1)
	return 10 * math.Log10(peak*peak/(sum/float64(n)))
}

// lossyDecode parses and decodes a codestream with this package.
func lossyDecode(t *testing.T, data []byte) *Image {
	t.Helper()
	c, err := ParseCodestream(data)
	if err != nil {
		t.Fatalf("ParseCodestream of what EncodeLossy wrote: %v", err)
	}
	if c.Coding.Wavelet != Wavelet97Irreversible || c.Quant.Style != QuantExpounded {
		t.Fatalf("the codestream says wavelet %d, quantization %d; want the 9/7, expounded",
			c.Coding.Wavelet, c.Quant.Style)
	}
	img, err := Decode(c)
	if err != nil {
		t.Fatalf("Decode of what EncodeLossy wrote: %v", err)
	}
	return img
}

// TestAnalyze97IsUndoneBySynthesis proves the forward 9/7 filter is the
// inverse of the decoder's, line by line, at both parities of origin and at
// lengths from one to an odd sixty-three: what synthesis returns is the line,
// to the precision of float32.
func TestAnalyze97IsUndoneBySynthesis(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for _, i0 := range []int{0, 1, 6, 7} {
		for n := 1; n <= 63; n++ {
			line := make([]float32, n)
			for i := range line {
				line[i] = float32(r.Intn(4096) - 2048)
			}
			x := append([]float32(nil), line...)
			analyze97(x, i0, i0+n, nil)
			synthesize1D(x, i0, i0+n, false, &scratch{})
			for i := range x {
				if math.Abs(float64(x[i]-line[i])) > 0.01 {
					t.Fatalf("origin %d, length %d: sample %d came back %g, was %g", i0, n, i, x[i], line[i])
				}
			}
		}
	}
}

// TestEncodeLossyMeetsTheRatio proves the codestream is no larger than the
// ratio allows and not far below it, at 8, 12 and 16 bits, signed and
// unsigned, gray and color, and that a higher ratio loses more.
func TestEncodeLossyMeetsTheRatio(t *testing.T) {
	cases := []struct {
		name               string
		w, h, comps, depth int
		signed             bool
	}{
		{"12-bit", 128, 96, 1, 12, false},
		{"16-bit signed", 97, 61, 1, 16, true},
		{"8-bit RGB", 80, 64, 3, 8, false},
	}
	for _, c := range cases {
		img := smoothImage(c.w, c.h, c.comps, c.depth, c.signed)
		raw := float64(c.w * c.h * c.comps * ((c.depth + 7) / 8))
		previous := math.Inf(1)
		for _, ratio := range []float64{5, 20} {
			data, err := EncodeLossy(img, LossyOptions{Ratio: ratio})
			if err != nil {
				t.Fatalf("%s at %g:1: %v", c.name, ratio, err)
			}
			got := raw / float64(len(data))
			if got < ratio || got > ratio*1.15 {
				t.Errorf("%s: asked for %g:1, got %.2f:1", c.name, ratio, got)
			}
			p := psnrImages(img, lossyDecode(t, data))
			if p >= previous {
				t.Errorf("%s: %g:1 gives %.1f dB, no worse than the lower ratio", c.name, ratio, p)
			}
			previous = p
		}
	}
}

// TestEncodeLossyMeetsThePSNR proves a PSNR asked for is what this package's
// decoder returns, or up to a decibel better, gray and color.
func TestEncodeLossyMeetsThePSNR(t *testing.T) {
	for _, comps := range []int{1, 3} {
		img := smoothImage(96, 80, comps, 12, false)
		if comps == 3 {
			img = smoothImage(96, 80, comps, 8, false)
		}
		for _, target := range []float64{40, 50} {
			data, err := EncodeLossy(img, LossyOptions{PSNR: target})
			if err != nil {
				t.Fatal(err)
			}
			if p := psnrImages(img, lossyDecode(t, data)); p < target || p > target+1 {
				t.Errorf("%d component(s): asked for %g dB, decoded %.2f dB", comps, target, p)
			}
		}
	}
}

// TestEncodeLossyEdges proves one pixel, a single row and column, and odd
// sizes encode and decode at every depth's extremes.
func TestEncodeLossyEdges(t *testing.T) {
	for _, c := range []struct{ w, h, depth int }{{1, 1, 12}, {1, 37, 8}, {41, 1, 16}, {33, 17, 12}, {2, 2, 1}} {
		img := smoothImage(c.w, c.h, 1, c.depth, false)
		data, err := EncodeLossy(img, LossyOptions{PSNR: 45})
		if err != nil {
			t.Fatalf("%dx%d at %d bits: %v", c.w, c.h, c.depth, err)
		}
		got := lossyDecode(t, data)
		if got.Width != c.w || got.Height != c.h {
			t.Errorf("%dx%d decoded as %dx%d", c.w, c.h, got.Width, got.Height)
		}
	}
}

// TestEncodeLossyRefuses proves a rate that is not one is refused: neither,
// both, a ratio below one; and that a ratio no codestream of the image can
// meet is refused rather than met with one that holds none of the picture,
// as 50:1 of an 8x8 image is.
func TestEncodeLossyRefuses(t *testing.T) {
	img := smoothImage(8, 8, 1, 8, false)
	for _, options := range []LossyOptions{{}, {Ratio: 10, PSNR: 40}, {Ratio: 0.5}, {PSNR: -1}, {Ratio: 50}} {
		if _, err := EncodeLossy(img, options); err == nil {
			t.Errorf("%+v was accepted", options)
		}
	}
}

// TestOpenJPEGDecodesWhatEncodeLossyWrites proves opj_decompress reads the
// lossy codestreams and returns what this package's decoder returns, every
// sample within one: the 9/7 is real arithmetic and two conformant decoders
// may land either side of a half. Skipped where opj_decompress is not on
// PATH.
func TestOpenJPEGDecodesWhatEncodeLossyWrites(t *testing.T) {
	if _, err := exec.LookPath("opj_decompress"); err != nil {
		t.Skip("opj_decompress is not on PATH")
	}
	dir := t.TempDir()
	for _, c := range []struct {
		w, h, comps, depth int
		signed             bool
		options            LossyOptions
	}{
		{128, 96, 1, 12, false, LossyOptions{Ratio: 10}},
		{97, 61, 1, 16, true, LossyOptions{Ratio: 8}},
		{80, 64, 3, 8, false, LossyOptions{PSNR: 40}},
		{33, 17, 1, 8, false, LossyOptions{PSNR: 45}},
	} {
		img := smoothImage(c.w, c.h, c.comps, c.depth, c.signed)
		data, err := EncodeLossy(img, c.options)
		if err != nil {
			t.Fatal(err)
		}
		ours := lossyDecode(t, data)
		theirs := decodeWithOpenJPEG(t, dir, data, c.comps)
		for comp := range ours.Components {
			worst := int32(0)
			for i, v := range ours.Components[comp] {
				d := v - theirs[comp][i]
				if d < 0 {
					d = -d
				}
				worst = max(worst, d)
			}
			if worst > 1 {
				t.Errorf("%dx%dx%d at %d bits: OpenJPEG and this package differ by %d", c.w, c.h, c.comps, c.depth, worst)
			}
		}
	}
}
