package compress_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The JPEG Extended encoder is lossy, so these tests bound the loss: the PSNR
// of what comes back, through this package's decoder and through libjpeg,
// which shares no code with it. The interop check and the PR hold it against
// dcmtk's encoder at the same quality, where the size and PSNR agree.

// smooth12 returns a smooth image of depth bits with a little texture, the
// kind of content the DCT is for, little endian above 8 bits.
func smooth12(width, height, components, depth int) []byte {
	maxVal := float64(int(1)<<uint(depth) - 1)
	out := make([]byte, 0, width*height*components*2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			for c := 0; c < components; c++ {
				v := (math.Sin(float64(x)/9+float64(c)) + math.Cos(float64(y)/7) + 2) / 4 * maxVal
				v += float64((x*31+y*17)%7) - 3
				s := int(math.Max(0, math.Min(maxVal, math.Round(v))))
				if depth <= 8 {
					out = append(out, byte(s))
				} else {
					out = binary.LittleEndian.AppendUint16(out, uint16(s))
				}
			}
		}
	}
	return out
}

// samplePSNR is the PSNR of b against a, both of depth-bit samples.
func samplePSNR(a, b []byte, depth int) float64 {
	var sum float64
	n := 0
	for i := 0; i < len(a); {
		var x, y float64
		if depth <= 8 {
			x, y = float64(a[i]), float64(b[i])
			i++
		} else {
			x, y = float64(binary.LittleEndian.Uint16(a[i:])), float64(binary.LittleEndian.Uint16(b[i:]))
			i += 2
		}
		sum += (x - y) * (x - y)
		n++
	}
	if sum == 0 {
		return math.Inf(1)
	}
	peak := float64(int(1)<<uint(depth) - 1)
	return 10 * math.Log10(peak*peak/(sum/float64(n)))
}

// TestEncodeJPEGExtendedRoundTrips proves the encoder's frames decode here, at
// 8 and 12 bits, gray and color, at sizes that are not a multiple of the block,
// and that the loss falls as the quality rises: above 40 dB at quality 90 for
// smooth content, and lower at 30 than at 90.
func TestEncodeJPEGExtendedRoundTrips(t *testing.T) {
	cases := []struct {
		name                        string
		width, height, comps, depth int
	}{
		{"12-bit", 64, 48, 1, 12},
		{"12-bit, odd size", 37, 23, 1, 12},
		{"12-bit, 1x1", 1, 1, 1, 12},
		{"8-bit", 61, 45, 1, 8},
		{"8-bit color", 40, 33, 3, 8},
	}
	for _, c := range cases {
		samples := smooth12(c.width, c.height, c.comps, c.depth)
		var previous float64
		for _, quality := range []int{30, 90} {
			stream, err := compress.EncodeJPEGExtended(samples, c.width, c.height, c.comps, c.depth, quality)
			if err != nil {
				t.Fatalf("%s at %d: %v", c.name, quality, err)
			}
			if len(stream)%2 != 0 {
				t.Errorf("%s at %d: a codestream of odd length", c.name, quality)
			}
			got, err := compress.NewJPEGDecompressorForPhotometric("YBR_FULL").Decompress(stream)
			if err != nil {
				t.Fatalf("%s at %d: decoding: %v", c.name, quality, err)
			}
			if len(got) != len(samples) {
				t.Fatalf("%s at %d: %d bytes back for %d", c.name, quality, len(got), len(samples))
			}
			p := samplePSNR(samples, got, c.depth)
			if quality == 90 && p < 40 {
				t.Errorf("%s at 90: PSNR %.1f dB", c.name, p)
			}
			if quality == 90 && p < previous && c.width > 8 {
				t.Errorf("%s: PSNR %.1f dB at 90 is below the %.1f dB at 30", c.name, p, previous)
			}
			previous = p
		}
	}
}

// TestEncodeJPEGExtendedWritesSOF1 proves the frame header is SOF1 with the
// precision asked for, and that at a low quality a 12-bit frame's
// quantization tables have 16-bit entries (Pq = 1), which a byte could not
// hold.
func TestEncodeJPEGExtendedWritesSOF1(t *testing.T) {
	stream, err := compress.EncodeJPEGExtended(smooth12(16, 16, 1, 12), 16, 16, 1, 12, 5)
	if err != nil {
		t.Fatal(err)
	}
	sof := bytes.Index(stream, []byte{0xFF, 0xC1})
	if sof < 0 || stream[sof+4] != 12 {
		t.Fatalf("no SOF1 frame of 12 bits")
	}
	if bytes.Contains(stream[:sof], []byte{0xFF, 0xC0}) {
		t.Error("a baseline frame header besides SOF1")
	}
	dqt := bytes.Index(stream, []byte{0xFF, 0xDB})
	if dqt < 0 || stream[dqt+4]>>4 != 1 {
		t.Errorf("the quantization table at quality 5 is not of 16-bit entries")
	}
}

// TestEncodeJPEGExtendedRefuses proves a precision other than 8 or 12, a
// sample above the precision, two components, a quality of 0 and a buffer of
// the wrong length are refused.
func TestEncodeJPEGExtendedRefuses(t *testing.T) {
	over := make([]byte, 8)
	binary.LittleEndian.PutUint16(over, 4096)
	for _, c := range []struct {
		name                     string
		samples                  []byte
		w, h, comps, depth, qual int
	}{
		{"16-bit precision", make([]byte, 8), 2, 2, 1, 16, 90},
		{"sample above 12 bits", over, 2, 2, 1, 12, 90},
		{"two components", make([]byte, 8), 2, 2, 2, 8, 90},
		{"quality 0", make([]byte, 4), 2, 2, 1, 8, 0},
		{"short buffer", make([]byte, 3), 2, 2, 1, 8, 90},
	} {
		if _, err := compress.EncodeJPEGExtended(c.samples, c.w, c.h, c.comps, c.depth, c.qual); err == nil {
			t.Errorf("%s: encoded", c.name)
		}
	}
}

// TestJPEGExtendedOutputIsReadByLibjpeg proves libjpeg, through
// pylibjpeg-libjpeg, decodes the encoder's 12-bit and 8-bit frames to an image
// within a few levels of this package's decoder, and above 40 dB of the
// original at quality 90. The IDCTs differ in rounding, so not exactly.
// Skipped, saying so, where pylibjpeg-libjpeg is not installed.
func TestJPEGExtendedOutputIsReadByLibjpeg(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, so libjpeg cannot be asked to read the output")
	}
	if err := exec.Command(python, "-c", "import libjpeg, numpy").Run(); err != nil {
		t.Skip("pylibjpeg-libjpeg is not installed; install it to verify the encoder against libjpeg")
	}
	dir := t.TempDir()
	var args []string
	for i, c := range []struct{ w, h, comps, depth int }{{64, 48, 1, 12}, {37, 29, 1, 12}, {50, 30, 3, 8}} {
		samples := smooth12(c.w, c.h, c.comps, c.depth)
		stream, err := compress.EncodeJPEGExtended(samples, c.w, c.h, c.comps, c.depth, 90)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := compress.NewJPEGDecompressorForPhotometric("YBR_FULL").Decompress(stream)
		if err != nil {
			t.Fatal(err)
		}
		stem := filepath.Join(dir, fmt.Sprintf("f%d_%d", i, c.depth))
		for ext, data := range map[string][]byte{".jpg": stream, ".raw": samples, ".ours": ours} {
			if err := os.WriteFile(stem+ext, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		args = append(args, stem)
	}
	script := `
import math, sys, numpy as np
from libjpeg import decode
for stem in sys.argv[1:]:
    depth = int(stem.split("_")[-1])
    dt = np.uint8 if depth <= 8 else "<u2"
    want = np.frombuffer(open(stem + ".raw", "rb").read(), dtype=dt).astype(np.float64)
    ours = np.frombuffer(open(stem + ".ours", "rb").read(), dtype=dt).astype(np.float64)
    got = np.asarray(decode(open(stem + ".jpg", "rb").read(), 0)).ravel().astype(np.float64)
    if got.size != want.size:
        sys.exit(f"{stem}: libjpeg decoded {got.size} samples, want {want.size}")
    peak = (1 << depth) - 1
    psnr = 10 * math.log10(peak * peak / max(np.mean((got - want) ** 2), 1e-9))
    apart = np.abs(got - ours).max()
    if psnr < 40 or apart > 2:
        sys.exit(f"{stem}: libjpeg PSNR {psnr:.1f} dB, {apart:.0f} from this package's decoder")
    print(f"{stem.split('/')[-1]}: libjpeg {psnr:.1f} dB, within {apart:.0f} of this package")
`
	out, err := exec.Command(python, append([]string{"-c", script}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}
