package compress_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The library decoded JPEG Baseline and could not produce it (#243). These
// decode what the encoder writes with the standard library's decoder, which
// shares no code with it.

// gradient is a width by height image of components samples a pixel, smooth
// with a few edges, which is what a transform coder has to cope with.
func gradient(width, height, components int) []byte {
	out := make([]byte, 0, width*height*components)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			for c := 0; c < components; c++ {
				v := (x*3 + y*2 + c*40) % 256
				if (x/8+y/8)%5 == 0 {
					v = 255 - v
				}
				out = append(out, byte(v))
			}
		}
	}
	return out
}

// decoded returns the samples image/jpeg reads back, in the component order
// they were encoded in.
func decoded(t *testing.T, stream []byte, components int) (samples []byte, width, height int) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("image/jpeg cannot decode the stream: %v", err)
	}
	b := img.Bounds()
	switch src := img.(type) {
	case *image.Gray:
		if components != 1 {
			t.Fatalf("decoded as grayscale, encoded with %d components", components)
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			samples = append(samples, src.Pix[(y-b.Min.Y)*src.Stride:(y-b.Min.Y)*src.Stride+b.Dx()]...)
		}
	case *image.YCbCr:
		if components != 3 {
			t.Fatalf("decoded as color, encoded with %d components", components)
		}
		if src.SubsampleRatio != image.YCbCrSubsampleRatio444 {
			t.Fatalf("chroma is subsampled (%v); the encoder is to write 4:4:4", src.SubsampleRatio)
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				samples = append(samples, src.Y[src.YOffset(x, y)], src.Cb[src.COffset(x, y)], src.Cr[src.COffset(x, y)])
			}
		}
	default:
		t.Fatalf("decoded as %T", img)
	}
	return samples, b.Dx(), b.Dy()
}

func psnr(a, b []byte) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	if sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(sum/float64(len(a))))
}

func maxError(a, b []byte) int {
	worst := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		worst = max(worst, d)
	}
	return worst
}

// TestEncodeJPEGBaselineRoundTrips: another decoder reads the image back at
// its size, close to the original, and closer as quality rises.
func TestEncodeJPEGBaselineRoundTrips(t *testing.T) {
	for _, c := range []struct {
		name                      string
		width, height, components int
	}{
		{"grayscale, whole blocks", 64, 48, 1},
		{"grayscale, ragged edges", 37, 21, 1},
		{"color, whole blocks", 32, 32, 3},
		{"color, ragged edges", 19, 30, 3},
		{"one pixel", 1, 1, 1},
		{"one pixel of color", 1, 1, 3},
		{"a single row", 100, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := gradient(c.width, c.height, c.components)
			last := 0.0
			for _, quality := range []int{25, 75, 100} {
				stream, err := compress.EncodeJPEGBaseline(src, c.width, c.height, c.components, quality)
				if err != nil {
					t.Fatalf("EncodeJPEGBaseline at %d: %v", quality, err)
				}
				got, w, h := decoded(t, stream, c.components)
				if w != c.width || h != c.height {
					t.Fatalf("decoded as %dx%d, encoded %dx%d", w, h, c.width, c.height)
				}
				p := psnr(src, got)
				if p < last-0.5 {
					t.Errorf("quality %d gives %.1f dB, lower than the %.1f dB below it", quality, p, last)
				}
				last = p
				if quality == 100 && maxError(src, got) > 3 {
					t.Errorf("at quality 100 a sample is off by %d", maxError(src, got))
				}
			}
			if last < 40 {
				t.Errorf("quality 100 reaches only %.1f dB", last)
			}
		})
	}
}

// TestEncodeJPEGBaselineIsBaseline: the stream is what the transfer syntax
// names: SOF0, 8-bit precision, starting with SOI and ending with EOI, with
// no restart interval and of even length as a fragment has to be.
func TestEncodeJPEGBaselineIsBaseline(t *testing.T) {
	stream, err := compress.EncodeJPEGBaseline(gradient(24, 16, 3), 24, 16, 3, 90)
	if err != nil {
		t.Fatal(err)
	}
	if len(stream) < 4 || stream[0] != 0xFF || stream[1] != 0xD8 {
		t.Fatal("the stream does not start with SOI")
	}
	end := bytes.LastIndex(stream, []byte{0xFF, 0xD9})
	if end < 0 || len(stream)-end > 3 {
		t.Error("the stream does not end with EOI")
	}
	if len(stream)%2 != 0 {
		t.Errorf("the stream is %d bytes; a fragment is of even length", len(stream))
	}
	sof := bytes.Index(stream, []byte{0xFF, 0xC0})
	if sof < 0 {
		t.Fatal("no SOF0 marker: this is not baseline")
	}
	if precision := stream[sof+4]; precision != 8 {
		t.Errorf("sample precision %d, want 8", precision)
	}
	for _, marker := range []byte{0xC1, 0xC2, 0xC3, 0xDD} {
		if bytes.Contains(stream[:bytes.Index(stream, []byte{0xFF, 0xDA})], []byte{0xFF, marker}) {
			t.Errorf("the header holds marker FF%02X, which baseline does not use here", marker)
		}
	}
}

// TestEncodeJPEGBaselineStuffsFF: a coded FF has to be followed by 00, or a
// decoder takes it for a marker. Noise at top quality produces them.
func TestEncodeJPEGBaselineStuffsFF(t *testing.T) {
	src := make([]byte, 64*64)
	state := uint32(1)
	for i := range src {
		state = state*1664525 + 1013904223
		src[i] = byte(state >> 24)
	}
	stream, err := compress.EncodeJPEGBaseline(src, 64, 64, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	scan := stream[bytes.Index(stream, []byte{0xFF, 0xDA}):]
	if !bytes.Contains(scan[2:len(scan)-3], []byte{0xFF, 0x00}) {
		t.Skip("this noise produced no FF in the entropy-coded data")
	}
	got, _, _ := decoded(t, stream, 1)
	if maxError(src, got) > 6 {
		t.Errorf("noise at quality 100 decodes with a sample off by %d", maxError(src, got))
	}
}

// TestEncodeJPEGBaselineRefusesWhatItCannotCode: baseline is 8 bits and one
// or three components here, and a wrong length would be coded as garbage.
func TestEncodeJPEGBaselineRefusesWhatItCannotCode(t *testing.T) {
	px := gradient(8, 8, 1)
	for name, call := range map[string]func() ([]byte, error){
		"two components": func() ([]byte, error) { return compress.EncodeJPEGBaseline(gradient(8, 8, 2), 8, 8, 2, 90) },
		"short samples":  func() ([]byte, error) { return compress.EncodeJPEGBaseline(px[:60], 8, 8, 1, 90) },
		"no width":       func() ([]byte, error) { return compress.EncodeJPEGBaseline(px, 0, 8, 1, 90) },
		"quality 0":      func() ([]byte, error) { return compress.EncodeJPEGBaseline(px, 8, 8, 1, 0) },
		"quality 101":    func() ([]byte, error) { return compress.EncodeJPEGBaseline(px, 8, 8, 1, 101) },
		"65536 columns":  func() ([]byte, error) { return compress.EncodeJPEGBaseline(px, 65536, 1, 1, 90) },
	} {
		if _, err := call(); err == nil {
			t.Errorf("%s was encoded", name)
		}
	}
}
