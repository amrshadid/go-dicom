package compress_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The encoder is the mirror of this package's decoder, so a round trip through
// both shows only that they agree. The tests here establish that much: every
// predictor, every edge rule, every difference category. That the codestream
// is lossless JPEG and not merely something this decoder accepts is shown by
// TestJPEGLosslessOutputIsReadableByLibjpeg below and by the interop script,
// where dcmtk and pydicom decode the whole corpus.

// losslessNoise returns width*height*components random samples of depth bits,
// little endian when bitsAllocated is 16, two's complement when signed.
func losslessNoise(width, height, components, bitsAllocated, depth int, signed bool, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	n := width * height * components
	out := make([]byte, n*bitsAllocated/8)
	for i := 0; i < n; i++ {
		v := r.Intn(1 << uint(depth))
		if signed {
			v -= 1 << uint(depth-1)
		}
		if bitsAllocated == 8 {
			out[i] = byte(v)
		} else {
			binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
		}
	}
	return out
}

// losslessDecoded decodes a codestream with this package and returns its
// samples, masked to depth bits as the decoder returns them.
func losslessDecoded(t *testing.T, stream []byte) []byte {
	t.Helper()
	if len(stream)%2 != 0 {
		t.Fatalf("the codestream is %d bytes; a fragment is of even length", len(stream))
	}
	out, err := compress.NewJPEGLosslessDecompressor().Decompress(stream)
	if err != nil {
		t.Fatalf("decoding what was just encoded: %v", err)
	}
	return out
}

// maskedSamples is samples as the codestream holds them: each one's low depth
// bits, which for a signed sample drops the sign extension a reader restores.
func maskedSamples(samples []byte, bitsAllocated, depth int) []byte {
	out := append([]byte(nil), samples...)
	if bitsAllocated == 8 {
		for i := range out {
			out[i] &= byte(1<<uint(depth) - 1)
		}
		return out
	}
	for i := 0; i+1 < len(out); i += 2 {
		v := binary.LittleEndian.Uint16(out[i:]) & uint16(1<<uint(depth)-1)
		binary.LittleEndian.PutUint16(out[i:], v)
	}
	return out
}

// TestEncodeJPEGLosslessRoundTrips proves the encoder is lossless through this
// package's decoder for every predictor, both sample widths, both signs, one to
// four components, and the sizes where the edge rules of T.81 H.1.2.1 are all
// there is: one pixel, one row, one column, odd sizes.
func TestEncodeJPEGLosslessRoundTrips(t *testing.T) {
	cases := []struct {
		name                 string
		width, height, comps int
		bitsAllocated, depth int
		signed               bool
	}{
		{"1x1 8-bit", 1, 1, 1, 8, 8, false},
		{"1x1 16-bit", 1, 1, 1, 16, 16, false},
		{"one row", 37, 1, 1, 16, 12, false},
		{"one column", 1, 41, 1, 16, 12, false},
		{"odd 8-bit", 33, 17, 1, 8, 8, false},
		{"odd 12-bit", 31, 29, 1, 16, 12, false},
		{"full 16-bit", 64, 48, 1, 16, 16, false},
		{"signed 16-bit", 29, 23, 1, 16, 16, true},
		{"signed 12 of 16", 29, 23, 1, 16, 12, true},
		{"signed 8-bit", 17, 9, 1, 8, 8, true},
		{"2-bit", 19, 7, 1, 8, 2, false},
		{"RGB", 27, 13, 3, 8, 8, false},
		{"RGB 16-bit", 13, 11, 3, 16, 16, false},
		{"four components", 9, 9, 4, 8, 8, false},
	}
	for i, tc := range cases {
		samples := losslessNoise(tc.width, tc.height, tc.comps, tc.bitsAllocated, tc.depth, tc.signed, int64(i))
		want := maskedSamples(samples, tc.bitsAllocated, tc.depth)
		// The decoder returns one byte a sample at 8 bits of precision or
		// fewer, whatever the image was allocated.
		if tc.depth <= 8 && tc.bitsAllocated == 16 {
			t.Fatalf("%s: a case the decoder returns narrower than it went in", tc.name)
		}
		for predictor := 0; predictor <= 7; predictor++ {
			stream, err := compress.EncodeJPEGLossless(samples, tc.width, tc.height, tc.comps,
				tc.bitsAllocated, tc.depth, tc.signed, predictor)
			if err != nil {
				t.Fatalf("%s, predictor %d: %v", tc.name, predictor, err)
			}
			if got := losslessDecoded(t, stream); !bytes.Equal(got, want) {
				t.Errorf("%s, predictor %d: the samples decoded are not the samples encoded", tc.name, predictor)
			}
		}
	}
}

// TestEncodeJPEGLosslessCodesEveryCategory proves the differences of 16 bits,
// which the example tables of T.81 Annex K cannot code, are coded: a column
// alternating between 0 and 65535 and between 0 and 32768 gives differences
// of category 15 and 16 at every sample, and 16 is the one with no bits after
// its code (Table H.2).
func TestEncodeJPEGLosslessCodesEveryCategory(t *testing.T) {
	const width, height = 8, 64
	samples := make([]byte, width*height*2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var v uint16
			switch {
			case x < 4 && y%2 == 1:
				v = 0xFFFF
			case x >= 4 && y%2 == 1:
				v = 0x8000
			}
			binary.LittleEndian.PutUint16(samples[(y*width+x)*2:], v)
		}
	}
	for _, predictor := range []int{1, 2} {
		stream, err := compress.EncodeJPEGLossless(samples, width, height, 1, 16, 16, false, predictor)
		if err != nil {
			t.Fatalf("predictor %d: %v", predictor, err)
		}
		if got := losslessDecoded(t, stream); !bytes.Equal(got, samples) {
			t.Errorf("predictor %d: the extremes of the range did not come back", predictor)
		}
	}
}

// TestEncodeJPEGLosslessWritesProcess14 proves the frame is SOF3 with the
// precision asked for, and the scan names the predictor in Ss with no point
// transform: what a decoder reads to know this is .57 or .70.
func TestEncodeJPEGLosslessWritesProcess14(t *testing.T) {
	samples := losslessNoise(16, 16, 1, 16, 12, false, 7)
	stream, err := compress.EncodeJPEGLossless(samples, 16, 16, 1, 16, 12, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	sof := bytes.Index(stream, []byte{0xFF, 0xC3})
	if sof < 0 {
		t.Fatal("no SOF3 marker")
	}
	if precision := stream[sof+4]; precision != 12 {
		t.Errorf("the frame says %d bits; want 12", precision)
	}
	for _, other := range [][]byte{{0xFF, 0xC0}, {0xFF, 0xC1}} {
		if bytes.Contains(stream[:sof], other) {
			t.Errorf("a DCT frame header % x before SOF3", other)
		}
	}
	sos := bytes.Index(stream, []byte{0xFF, 0xDA})
	if sos < 0 {
		t.Fatal("no SOS marker")
	}
	// FFDA, length, Ns = 1, one component's two bytes, then Ss Se AhAl.
	if ss, se, a := stream[sos+7], stream[sos+8], stream[sos+9]; ss != 1 || se != 0 || a != 0 {
		t.Errorf("the scan says Ss %d, Se %d, Ah/Al %#x; want 1, 0, 0", ss, se, a)
	}
}

// TestEncodeJPEGLosslessChoosesThePredictor proves predictor 0 picks a
// predictor that suits the image: vertical stripes are free predicted from
// above (2) and costly predicted from the left (1), and the stream chosen is
// no larger than the one predictor 1 gives.
func TestEncodeJPEGLosslessChoosesThePredictor(t *testing.T) {
	const width, height = 64, 64
	r := rand.New(rand.NewSource(3))
	column := make([]byte, width)
	for x := range column {
		column[x] = byte(r.Intn(256))
	}
	samples := make([]byte, width*height)
	for y := 0; y < height; y++ {
		copy(samples[y*width:], column)
	}
	chosen, err := compress.EncodeJPEGLossless(samples, width, height, 1, 8, 8, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	sv1, err := compress.EncodeJPEGLossless(samples, width, height, 1, 8, 8, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	sos := bytes.Index(chosen, []byte{0xFF, 0xDA})
	if ss := chosen[sos+7]; ss != 2 {
		t.Errorf("predictor %d was chosen for vertical stripes; want 2, from above", ss)
	}
	if len(chosen) > len(sv1) {
		t.Errorf("the chosen predictor codes %d bytes and predictor 1 codes %d", len(chosen), len(sv1))
	}
	if !bytes.Equal(losslessDecoded(t, chosen), samples) {
		t.Error("the chosen predictor's stream does not decode to the image")
	}
}

// TestEncodeJPEGLosslessRefusesWhatItCannotCode proves each argument outside
// what T.81 Process 14 holds is refused with an error, not coded wrongly: a
// sample wider than the precision, a precision below 2, five components, a
// predictor of 8, and a sample buffer of the wrong length.
func TestEncodeJPEGLosslessRefusesWhatItCannotCode(t *testing.T) {
	wide := make([]byte, 8)
	binary.LittleEndian.PutUint16(wide, 0x1000) // 13 bits, in a 12-bit frame
	cases := []struct {
		name    string
		samples []byte
		w, h, c int
		ba, d   int
		signed  bool
		sv      int
	}{
		{"sample wider than the precision", wide, 2, 2, 1, 16, 12, false, 1},
		{"precision of 1", make([]byte, 4), 2, 2, 1, 8, 1, false, 1},
		{"precision above the allocation", make([]byte, 4), 2, 2, 1, 8, 12, false, 1},
		{"five components", make([]byte, 20), 2, 2, 5, 8, 8, false, 1},
		{"predictor 8", make([]byte, 4), 2, 2, 1, 8, 8, false, 8},
		{"short buffer", make([]byte, 3), 2, 2, 1, 8, 8, false, 1},
		{"32 bits allocated", make([]byte, 16), 2, 2, 1, 32, 16, false, 1},
		{"no width", nil, 0, 2, 1, 8, 8, false, 1},
	}
	for _, tc := range cases {
		if _, err := compress.EncodeJPEGLossless(tc.samples, tc.w, tc.h, tc.c, tc.ba, tc.d, tc.signed, tc.sv); err == nil {
			t.Errorf("%s: encoded without an error", tc.name)
		}
	}
}

// TestJPEGLosslessOutputIsReadableByLibjpeg proves a decoder with no code in
// common with this package, libjpeg through pylibjpeg, reads what the encoder
// writes and returns every sample, for each predictor at 16 bits and for color.
// Skipped, saying so, where pylibjpeg-libjpeg is not installed.
func TestJPEGLosslessOutputIsReadableByLibjpeg(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, so libjpeg cannot be asked to read the output")
	}
	if err := exec.Command(python, "-c", "import libjpeg, numpy").Run(); err != nil {
		t.Skip("pylibjpeg-libjpeg is not installed; install it to verify the encoder against libjpeg")
	}

	dir := t.TempDir()
	type frame struct {
		name                 string
		samples              []byte
		width, height, comps int
		bitsAllocated, depth int
		predictor            int
	}
	var frames []frame
	for predictor := 1; predictor <= 7; predictor++ {
		frames = append(frames, frame{"gray16", losslessNoise(31, 17, 1, 16, 16, false, int64(predictor)),
			31, 17, 1, 16, 16, predictor})
	}
	frames = append(frames,
		frame{"gray12", losslessNoise(33, 19, 1, 16, 12, false, 11), 33, 19, 1, 16, 12, 0},
		frame{"rgb", losslessNoise(21, 15, 3, 8, 8, false, 12), 21, 15, 3, 8, 8, 0})

	script := `
import sys, numpy as np
from libjpeg import decode
for stem in sys.argv[1:]:
    width, height, comps, ba = (int(v) for v in stem.split("_")[-4:])
    want = np.frombuffer(open(stem + ".raw", "rb").read(), dtype=np.uint8 if ba == 8 else "<u2")
    got = np.asarray(decode(open(stem + ".jpg", "rb").read())).ravel()
    if got.size != want.size or not (got.astype(np.int64) == want.astype(np.int64)).all():
        print(f"{stem}: libjpeg decoded something else", file=sys.stderr)
        raise SystemExit(1)
print(f"libjpeg decoded {len(sys.argv) - 1} frames to every sample")
`
	var stems []string
	for i, f := range frames {
		stream, err := compress.EncodeJPEGLossless(f.samples, f.width, f.height, f.comps, f.bitsAllocated,
			f.depth, false, f.predictor)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		stem := filepath.Join(dir, fmt.Sprintf("%s_%d_%d_%d_%d_%d", f.name, i, f.width, f.height,
			f.comps, f.bitsAllocated))
		if err := os.WriteFile(stem+".jpg", stream, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stem+".raw", f.samples, 0o600); err != nil {
			t.Fatal(err)
		}
		stems = append(stems, stem)
	}
	out, err := exec.Command(python, append([]string{"-c", script}, stems...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("libjpeg could not read what this package wrote: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
