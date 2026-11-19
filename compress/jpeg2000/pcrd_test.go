package jpeg2000

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// randomBlock is a code-block of coefficients spread like a wavelet band's:
// mostly small, a few large, signed, with their magnitudes before
// quantization a fraction above.
func randomBlock(r *rand.Rand, w, h int) ([]int32, []float64) {
	q := make([]int32, w*h)
	exact := make([]float64, w*h)
	scale := math.Exp2(float64(r.Intn(12)))
	for i := range q {
		x := math.Abs(r.NormFloat64()) * scale
		if r.Intn(10) == 0 {
			x *= 20
		}
		exact[i] = x
		q[i] = int32(x)
		if r.Intn(2) == 0 {
			q[i] = -q[i]
		}
	}
	return q, exact
}

// TestTruncatedBlocksDecodeAsCounted cuts code-blocks at every truncation
// point the encoder records and requires each cut to decode, with this
// package's decoder, to exactly the coefficients the whole data gives for
// the same passes. A point that kept too few bytes would leave the decoder
// guessing at the last symbols of its passes, and a codestream cut there
// would decode differently in every decoder that pads differently. trimCuts
// shortens cuts further, so it is held to the same.
func TestTruncatedBlocksDecodeAsCounted(t *testing.T) {
	r := rand.New(rand.NewSource(526))
	points := 0
	for trial := 0; trial < 300; trial++ {
		w, h := 1+r.Intn(64), 1+r.Intn(64)
		q, exact := randomBlock(r, w, h)
		band := r.Intn(4)
		encoded, cuts, err := encodeBlockTruncations(q, exact, w, h, band, false)
		if err != nil {
			t.Fatal(err)
		}
		if encoded.Passes == 0 {
			continue
		}
		const magnitudeBits = 30
		lb := &layeredBlock{EncodedBlock: encoded, w: w, h: h, bandType: band,
			zeroBitPlanes: magnitudeBits - encoded.BitPlanes, magnitudeBits: magnitudeBits}
		lb.cuts = slices.Clone(cuts)
		trimCuts([]*layeredBlock{lb})
		for k, cut := range append(cuts, lb.cuts...) {
			if k > 0 && k < len(cuts) && cut.bytes < cuts[k-1].bytes {
				t.Fatalf("trial %d: point %d keeps %d bytes, fewer than the point before", trial, k, cut.bytes)
			}
			block := CodeBlockData{
				Block:         CodeBlock{X1: w, Y1: h},
				BandType:      band,
				ZeroBitPlanes: lb.zeroBitPlanes,
				Passes:        cut.passes,
				Data:          encoded.Data,
			}
			if !sameDecode(block, magnitudeBits, cut.bytes) {
				t.Fatalf("trial %d (%dx%d, band %d): %d passes in %d of %d bytes decode differently",
					trial, w, h, band, cut.passes, cut.bytes, len(encoded.Data))
			}
			points++
		}
	}
	t.Logf("%d truncation points checked", points)
}

// TestTruncationGainsAreTheErrorRemoved proves the error the encoder counts
// for each truncation point is the error the decoder's reconstruction has:
// the squared distance, in steps, between the magnitudes before quantization
// and what this package decodes from the block cut there.
func TestTruncationGainsAreTheErrorRemoved(t *testing.T) {
	r := rand.New(rand.NewSource(14))
	for trial := 0; trial < 50; trial++ {
		w, h := 1+r.Intn(32), 1+r.Intn(32)
		q, exact := randomBlock(r, w, h)
		encoded, cuts, err := encodeBlockTruncations(q, exact, w, h, BandHH, false)
		if err != nil {
			t.Fatal(err)
		}
		var total float64
		for _, x := range exact {
			total += x * x
		}
		const magnitudeBits = 30
		for _, cut := range cuts {
			got, err := DecodeBlock(CodeBlockData{
				Block: CodeBlock{X1: w, Y1: h}, BandType: BandHH,
				ZeroBitPlanes: magnitudeBits - encoded.BitPlanes,
				Passes:        cut.passes, Data: encoded.Data[:cut.bytes],
			}, magnitudeBits)
			if err != nil {
				t.Fatal(err)
			}
			var left float64
			for i, v := range got {
				// The decoder's values are in halves, with the sign.
				d := exact[i] - math.Abs(float64(v))/2
				left += d * d
			}
			if want := total - cut.gained; math.Abs(left-want) > 1e-6*math.Max(1, total) {
				t.Fatalf("trial %d, %d passes: the decoded block is %g from the source, the encoder counted %g",
					trial, cut.passes, left, want)
			}
		}
	}
}

// TestConvexHullSlopesFall proves the hull keeps only points whose slopes
// strictly fall, starts from nothing kept, and that no truncation point lies
// above it: a cut anywhere else buys less error for its bytes.
func TestConvexHullSlopesFall(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for trial := 0; trial < 200; trial++ {
		var points []truncationPoint
		bytes, gained := 0, 0.0
		for i := 0; i < 1+r.Intn(40); i++ {
			bytes += r.Intn(20)
			gained += math.Max(0, r.NormFloat64()*100+50)
			points = append(points, truncationPoint{passes: i + 1, bytes: bytes, gained: gained})
		}
		hull := convexHull(points, 1)
		for i := 1; i < len(hull); i++ {
			if hull[i].slope >= hull[i-1].slope {
				t.Fatalf("trial %d: slope %g after %g", trial, hull[i].slope, hull[i-1].slope)
			}
		}
		// Every point is on or under the hull's segments.
		at := func(b int) float64 {
			prevB, prevG := 0, 0.0
			for _, p := range hull {
				if b <= p.bytes {
					if p.bytes == prevB {
						return p.gained
					}
					return prevG + (p.gained-prevG)*float64(b-prevB)/float64(p.bytes-prevB)
				}
				prevB, prevG = p.bytes, p.gained
			}
			return prevG
		}
		for _, p := range points {
			if p.gained > at(p.bytes)+1e-9 {
				t.Fatalf("trial %d: the point of %d bytes and %g lies above the hull (%g)", trial, p.bytes, p.gained, at(p.bytes))
			}
		}
	}
}

// TestEncodeLossyLayers proves a ratio asked for in three quality layers is
// met, the codestream says three layers, and this package decodes it to what
// a single layer at the same ratio gives, within a decibel. Where OpenJPEG is
// present, it decodes the first one, two and three layers, each nearer the
// image than the last.
func TestEncodeLossyLayers(t *testing.T) {
	img := smoothImage(160, 120, 1, 12, false)
	raw := float64(160 * 120 * 2)
	one, err := EncodeLossy(img, LossyOptions{Ratio: 10})
	if err != nil {
		t.Fatal(err)
	}
	three, err := EncodeLossy(img, LossyOptions{Ratio: 10, Layers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := raw / float64(len(three)); got < 10 || got > 10*1.02 {
		t.Errorf("three layers at 10:1 came to %.2f:1", got)
	}
	c, err := ParseCodestream(three)
	if err != nil {
		t.Fatal(err)
	}
	if c.Coding.Layers != 3 {
		t.Errorf("the codestream says %d layers", c.Coding.Layers)
	}
	p1, p3 := psnrImages(img, lossyDecode(t, one)), psnrImages(img, lossyDecode(t, three))
	if p3 < p1-1 {
		t.Errorf("three layers decode to %.2f dB, one to %.2f", p3, p1)
	}

	if _, err := exec.LookPath("opj_decompress"); err != nil {
		t.Skip("opj_decompress is not on PATH")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "layers.j2k")
	if err := os.WriteFile(in, three, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := 0.0
	for layers := 1; layers <= 3; layers++ {
		out := filepath.Join(dir, fmt.Sprintf("layers%d.pgx", layers))
		if message, err := exec.Command("opj_decompress", "-i", in, "-o", out, "-l", fmt.Sprint(layers)).CombinedOutput(); err != nil {
			t.Fatalf("opj_decompress -l %d: %v\n%s", layers, err, message)
		}
		if _, err := os.Stat(out); err != nil {
			out = filepath.Join(dir, fmt.Sprintf("layers%d_0.pgx", layers)) // as decodeWithOpenJPEG
		}
		got := &Image{Width: img.Width, Height: img.Height, Components: [][]int32{readPGX(t, out)}, Depths: img.Depths}
		p := psnrImages(img, got)
		if p <= previous {
			t.Errorf("OpenJPEG decodes %d layers to %.2f dB, no nearer than %d (%.2f dB)", layers, p, layers-1, previous)
		}
		previous = p
	}
}

// TestEncodeLossyReversible proves a reversible encode meets its ratio with
// the 5/3 filter and nothing quantized, is the image exactly when the ratio
// leaves room for all of it, and decodes in OpenJPEG to this package's
// samples exactly, the filter being integer: gray, color with the RCT, and
// signed in layers.
func TestEncodeLossyReversible(t *testing.T) {
	for _, c := range []struct {
		comps, depth, layers int
		signed               bool
	}{{1, 12, 1, false}, {3, 8, 1, false}, {1, 16, 3, true}} {
		img := smoothImage(150, 110, c.comps, c.depth, c.signed)
		raw := float64(150 * 110 * c.comps * ((c.depth + 7) / 8))
		data, err := EncodeLossy(img, LossyOptions{Ratio: 12, Reversible: true, Layers: c.layers})
		if err != nil {
			t.Fatal(err)
		}
		if got := raw / float64(len(data)); got < 12 || got > 12*1.02 {
			t.Errorf("%+v: asked for 12:1, got %.2f:1", c, got)
		}
		cs, err := ParseCodestream(data)
		if err != nil {
			t.Fatal(err)
		}
		if cs.Coding.Wavelet != Wavelet53Reversible || cs.Quant.Style != QuantNone || cs.Coding.Layers != c.layers {
			t.Fatalf("%+v: wavelet %d, quantization %d, %d layers", c, cs.Coding.Wavelet, cs.Quant.Style, cs.Coding.Layers)
		}
		ours, err := Decode(cs)
		if err != nil {
			t.Fatal(err)
		}
		if p := psnrImages(img, ours); p < 40 {
			t.Errorf("%+v: %.1f dB at 12:1", c, p)
		}

		whole, err := EncodeLossy(img, LossyOptions{Ratio: 1.01, Reversible: true})
		if err != nil {
			t.Fatal(err)
		}
		cw, err := ParseCodestream(whole)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Decode(cw)
		if err != nil {
			t.Fatal(err)
		}
		if p := psnrImages(img, back); !math.IsInf(p, 1) {
			t.Errorf("%+v: room for the whole image and it decodes to %.1f dB, not exactly", c, p)
		}

		if _, err := exec.LookPath("opj_decompress"); err != nil {
			continue
		}
		theirs := decodeWithOpenJPEG(t, t.TempDir(), data, c.comps)
		for comp := range ours.Components {
			if !slices.Equal(ours.Components[comp], theirs[comp]) {
				t.Errorf("%+v: OpenJPEG decodes component %d differently", c, comp)
			}
		}
	}
}
