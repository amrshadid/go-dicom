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

// Near-lossless JPEG-LS promises one thing: every decoded sample is within NEAR
// of the sample encoded. These tests hold the encoder to it through this
// package's decoder, which decodes NEAR > 0 streams from other encoders
// (TestJPEGLSNearLosslessStaysWithinBound and the corpus), and through libjpeg,
// which shares no code with either.

// nearSamples returns n samples of bits, little endian above 8 bits: a smooth
// field with noise on it, so that both run mode and regular mode are coded.
func nearSamples(width, height, components, bits int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	maxVal := 1<<uint(bits) - 1
	out := make([]byte, 0, width*height*components*2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			for c := 0; c < components; c++ {
				v := (x*7 + y*3 + c*50) % (maxVal + 1)
				if x%11 > 6 {
					v = (v + r.Intn(9)) % (maxVal + 1) // noisy columns
				}
				if y%13 < 4 {
					v = maxVal / 3 // flat bands, coded in run mode
				}
				if bits <= 8 {
					out = append(out, byte(v))
				} else {
					out = binary.LittleEndian.AppendUint16(out, uint16(v))
				}
			}
		}
	}
	return out
}

// worstError is the largest difference between two sample buffers of the same
// width.
func worstError(a, b []byte, bits int) int {
	worst := 0
	if bits <= 8 {
		for i := range a {
			worst = max(worst, abs(int(a[i])-int(b[i])))
		}
		return worst
	}
	for i := 0; i+1 < len(a); i += 2 {
		worst = max(worst, abs(int(binary.LittleEndian.Uint16(a[i:]))-int(binary.LittleEndian.Uint16(b[i:]))))
	}
	return worst
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestJPEGLSNearLosslessIsWithinNear proves every sample decodes to within NEAR,
// at 2, 8, 12 and 16 bits, gray and color, for NEAR from 1 to 10, at the sizes
// where the edge rules are all there is, and that some error is actually made:
// a stream that decoded exactly would mean NEAR was not applied.
func TestJPEGLSNearLosslessIsWithinNear(t *testing.T) {
	cases := []struct {
		name                       string
		width, height, comps, bits int
	}{
		{"1x1", 1, 1, 1, 8},
		{"one row", 41, 1, 1, 12},
		{"one column", 1, 37, 1, 12},
		{"8-bit", 67, 45, 1, 8},
		{"12-bit", 64, 64, 1, 12},
		{"16-bit", 33, 29, 1, 16},
		{"RGB", 31, 23, 3, 8},
		{"2-bit", 19, 17, 1, 2},
	}
	for i, tc := range cases {
		samples := nearSamples(tc.width, tc.height, tc.comps, tc.bits, int64(i))
		for _, near := range []int{1, 2, 3, 5, 10} {
			if near > (1<<uint(tc.bits)-1)/2 {
				continue
			}
			stream, err := compress.EncodeJPEGLSNearLossless(samples, tc.width, tc.height, tc.comps, tc.bits, near)
			if err != nil {
				t.Fatalf("%s, NEAR %d: %v", tc.name, near, err)
			}
			got, err := compress.NewJPEGLSDecompressor().Decompress(stream)
			if err != nil {
				t.Fatalf("%s, NEAR %d: decoding: %v", tc.name, near, err)
			}
			if len(got) != len(samples) {
				t.Fatalf("%s, NEAR %d: %d bytes back for %d", tc.name, near, len(got), len(samples))
			}
			worst := worstError(got, samples, tc.bits)
			if worst > near {
				t.Errorf("%s, NEAR %d: a sample came back %d away", tc.name, near, worst)
			}
			if worst == 0 && tc.width*tc.height > 100 {
				t.Errorf("%s, NEAR %d: decoded exactly, so NEAR was not applied", tc.name, near)
			}
		}
	}
}

// TestJPEGLSNearZeroIsLossless proves NEAR 0 is the lossless encoder, byte for
// byte, so EncodeJPEGLS keeps the stream it always wrote.
func TestJPEGLSNearZeroIsLossless(t *testing.T) {
	samples := nearSamples(40, 30, 1, 12, 9)
	lossless, err := compress.EncodeJPEGLS(samples, 40, 30, 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	near0, err := compress.EncodeJPEGLSNearLossless(samples, 40, 30, 1, 12, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lossless, near0) {
		t.Error("NEAR 0 wrote a different stream from the lossless encoder")
	}
}

// TestJPEGLSNearLosslessWritesNear proves the scan header carries NEAR, which is
// all a decoder has to go on, and that a larger NEAR codes smaller.
func TestJPEGLSNearLosslessWritesNear(t *testing.T) {
	samples := nearSamples(64, 64, 1, 8, 4)
	previous := 1 << 30
	for _, near := range []int{0, 2, 7} {
		stream, err := compress.EncodeJPEGLSNearLossless(samples, 64, 64, 1, 8, near)
		if err != nil {
			t.Fatal(err)
		}
		sos := bytes.Index(stream, []byte{0xFF, 0xDA})
		// FFDA, length, Ns = 1, one component's two bytes, then NEAR.
		if got := int(stream[sos+7]); got != near {
			t.Errorf("the scan header says NEAR %d; want %d", got, near)
		}
		if len(stream) >= previous {
			t.Errorf("NEAR %d codes %d bytes, no fewer than the NEAR before it", near, len(stream))
		}
		previous = len(stream)
	}
}

// TestJPEGLSNearLosslessRefusesAnErrorOutOfRange proves a NEAR below zero, above
// 255, or above half the largest sample is refused (T.87 C.2.4.1).
func TestJPEGLSNearLosslessRefusesAnErrorOutOfRange(t *testing.T) {
	for _, tc := range []struct{ bits, near int }{{8, -1}, {8, 128}, {16, 256}, {2, 2}} {
		samples := make([]byte, 16*(1+tc.bits/9))
		if _, err := compress.EncodeJPEGLSNearLossless(samples, 4, 4, 1, tc.bits, tc.near); err == nil {
			t.Errorf("NEAR %d at %d bits was encoded", tc.near, tc.bits)
		}
	}
}

// TestJPEGLSNearLosslessIsReadByLibjpeg proves libjpeg, through
// pylibjpeg-libjpeg, decodes what the encoder writes at NEAR > 0 to the same
// samples this package's decoder does, every one within NEAR of the original.
// JPEG-LS reconstruction is integer arithmetic, so two correct decoders agree
// exactly. Skipped, saying so, where pylibjpeg-libjpeg is not installed.
func TestJPEGLSNearLosslessIsReadByLibjpeg(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, so libjpeg cannot be asked to read the output")
	}
	if err := exec.Command(python, "-c", "import libjpeg, numpy").Run(); err != nil {
		t.Skip("pylibjpeg-libjpeg is not installed; install it to verify the encoder against libjpeg")
	}
	dir := t.TempDir()
	var args []string
	for i, tc := range []struct{ w, h, comps, bits, near int }{
		{64, 48, 1, 8, 2}, {50, 40, 1, 12, 3}, {33, 31, 1, 16, 5}, {30, 20, 3, 8, 1}, {1, 1, 1, 8, 4},
	} {
		samples := nearSamples(tc.w, tc.h, tc.comps, tc.bits, int64(i+20))
		stream, err := compress.EncodeJPEGLSNearLossless(samples, tc.w, tc.h, tc.comps, tc.bits, tc.near)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := compress.NewJPEGLSDecompressor().Decompress(stream)
		if err != nil {
			t.Fatal(err)
		}
		stem := filepath.Join(dir, fmt.Sprintf("f%d_%d_%d", i, tc.bits, tc.near))
		for ext, data := range map[string][]byte{".jls": stream, ".raw": samples, ".ours": ours} {
			if err := os.WriteFile(stem+ext, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		args = append(args, stem)
	}
	script := `
import sys, numpy as np
from libjpeg import decode
for stem in sys.argv[1:]:
    bits, near = (int(v) for v in stem.split("_")[-2:])
    dt = np.uint8 if bits <= 8 else "<u2"
    want = np.frombuffer(open(stem + ".raw", "rb").read(), dtype=dt).astype(np.int64)
    ours = np.frombuffer(open(stem + ".ours", "rb").read(), dtype=dt).astype(np.int64)
    got = np.asarray(decode(open(stem + ".jls", "rb").read(), 0)).ravel().astype(np.int64)
    if got.size != want.size:
        sys.exit(f"{stem}: libjpeg decoded {got.size} samples, want {want.size}")
    worst = int(np.abs(got - want).max())
    if worst > near:
        sys.exit(f"{stem}: libjpeg decoded a sample {worst} away, NEAR is {near}")
    if not (got == ours).all():
        sys.exit(f"{stem}: libjpeg and this package decode different samples")
print(f"libjpeg decoded {len(sys.argv) - 1} near-lossless frames within NEAR, as this package does")
`
	out, err := exec.Command(python, append([]string{"-c", script}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("libjpeg disagrees: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
