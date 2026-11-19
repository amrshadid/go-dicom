package compress_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// JPEG-LS below 8 bits, where the default thresholds of T.87 C.2.4.1.1 are
// computed by a branch of their own and where large NEAR values push them past
// MAXVAL into the standard's CLAMP.
//
// testdata/jpegls/lowbits comes from outside this package. source_pP.raw is a
// 41x37 image of P bits (a ramp, noise, a flat band and a checkerboard, so both
// run mode and regular mode are coded). charls_pP_nearN.jls is that image coded
// by CharLS 2.2.0, through its C API, at NEAR N, with no LSE segment; for N > 0
// charls_pP_nearN.raw is CharLS's own decoding of it. dcmtk_pP.jls is the image
// coded losslessly by dcmtk 3.7.0's dcmcjpls (+pc), whose bundled CharLS
// predates the 2.1 fix of the thresholds at 6 and 7 bits.
const lowBitsWidth, lowBitsHeight = 41, 37

func readLowBits(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "jpegls", "lowbits", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// lowBitsCases lists the CharLS-coded fixtures: every precision from 2 to 8, at
// NEAR 0, 1, 2 and the largest NEAR the precision allows.
func lowBitsCases() (cases []struct{ bits, near int }) {
	for bits := 2; bits <= 8; bits++ {
		maxNear := (1<<bits - 1) / 2
		for _, near := range []int{0, 1, 2, maxNear} {
			if near > maxNear {
				continue
			}
			if n := len(cases); n > 0 && cases[n-1].bits == bits && cases[n-1].near >= near {
				continue
			}
			cases = append(cases, struct{ bits, near int }{bits, near})
		}
	}
	return cases
}

// lowBitsExpected is what a correct decoder makes of charls_pP_nearN.jls: the
// source when lossless, CharLS's own decoding otherwise.
func lowBitsExpected(t *testing.T, bits, near int) []byte {
	if near == 0 {
		return readLowBits(t, fmt.Sprintf("source_p%d.raw", bits))
	}
	return readLowBits(t, fmt.Sprintf("charls_p%d_near%d.raw", bits, near))
}

// TestJPEGLSLowPrecisionDecodesAsCharLS proves a frame CharLS 2.2 coded at 2 to
// 8 bits decodes here to exactly the samples CharLS decodes it to, lossless and
// near-lossless, including the NEAR values whose thresholds T.87's CLAMP sets
// to NEAR+1 or T1 rather than to MAXVAL.
func TestJPEGLSLowPrecisionDecodesAsCharLS(t *testing.T) {
	for _, tc := range lowBitsCases() {
		stream := readLowBits(t, fmt.Sprintf("charls_p%d_near%d.jls", tc.bits, tc.near))
		got, err := compress.NewJPEGLSDecompressor().Decompress(stream)
		if err != nil {
			t.Errorf("%d bits, NEAR %d: %v", tc.bits, tc.near, err)
			continue
		}
		if want := lowBitsExpected(t, tc.bits, tc.near); !bytes.Equal(got, want) {
			t.Errorf("%d bits, NEAR %d: decoded differently from CharLS, worst sample %d away",
				tc.bits, tc.near, worstError(got, want, tc.bits))
		}
	}
}

// TestJPEGLSLowPrecisionEncodesAsCharLS proves this package codes the source at
// 2 to 8 bits, lossless and near-lossless, to the very bytes CharLS writes, and
// to a frame that decodes to the samples CharLS's frame decodes to. JPEG-LS
// leaves an encoder no choice once the thresholds are fixed, so equal bytes
// mean equal thresholds and the same coding of every run (T.87 A.7.1.2).
func TestJPEGLSLowPrecisionEncodesAsCharLS(t *testing.T) {
	for _, tc := range lowBitsCases() {
		source := readLowBits(t, fmt.Sprintf("source_p%d.raw", tc.bits))
		stream, err := compress.EncodeJPEGLSNearLossless(source, lowBitsWidth, lowBitsHeight, 1, tc.bits, tc.near)
		if err != nil {
			t.Fatalf("%d bits, NEAR %d: %v", tc.bits, tc.near, err)
		}
		charls := readLowBits(t, fmt.Sprintf("charls_p%d_near%d.jls", tc.bits, tc.near))
		if !bytes.Equal(stream, charls) {
			t.Errorf("%d bits, NEAR %d: %d bytes, CharLS writes a different %d",
				tc.bits, tc.near, len(stream), len(charls))
		}
		got, err := compress.NewJPEGLSDecompressor().Decompress(stream)
		if err != nil {
			t.Fatalf("%d bits, NEAR %d: decoding: %v", tc.bits, tc.near, err)
		}
		if want := lowBitsExpected(t, tc.bits, tc.near); !bytes.Equal(got, want) {
			t.Errorf("%d bits, NEAR %d: reconstructs differently from CharLS, worst sample %d away",
				tc.bits, tc.near, worstError(got, want, tc.bits))
		}
	}
}

// TestJPEGLSLowPrecisionIsReadByLibjpeg proves libjpeg, through
// pylibjpeg-libjpeg, decodes what this package codes at every precision from 2
// to 8 and every NEAR in lowBitsCases to the samples CharLS reconstructs.
// libjpeg shares no code with CharLS or with this package. Skipped, saying so,
// where pylibjpeg-libjpeg is not installed.
func TestJPEGLSLowPrecisionIsReadByLibjpeg(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, so libjpeg cannot be asked to read the output")
	}
	if err := exec.Command(python, "-c", "import libjpeg, numpy").Run(); err != nil {
		t.Skip("pylibjpeg-libjpeg is not installed; install it to verify the encoder against libjpeg")
	}
	dir := t.TempDir()
	var args []string
	for _, tc := range lowBitsCases() {
		source := readLowBits(t, fmt.Sprintf("source_p%d.raw", tc.bits))
		stream, err := compress.EncodeJPEGLSNearLossless(source, lowBitsWidth, lowBitsHeight, 1, tc.bits, tc.near)
		if err != nil {
			t.Fatal(err)
		}
		stem := filepath.Join(dir, fmt.Sprintf("p%d_near%d", tc.bits, tc.near))
		for ext, data := range map[string][]byte{".jls": stream, ".want": lowBitsExpected(t, tc.bits, tc.near)} {
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
    want = np.frombuffer(open(stem + ".want", "rb").read(), dtype=np.uint8).astype(np.int64)
    got = np.asarray(decode(open(stem + ".jls", "rb").read(), 0)).ravel().astype(np.int64)
    if got.size != want.size or not (got == want).all():
        sys.exit(f"{stem}: libjpeg decodes it differently from CharLS")
print(f"libjpeg decoded {len(sys.argv) - 1} frames of 2 to 8 bits to CharLS's samples")
`
	out, err := exec.Command(python, append([]string{"-c", script}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("libjpeg disagrees: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// TestJPEGLSLegacyThresholdFrameIsRefusedByName proves a 6 or 7 bit frame that
// dcmtk coded with its old CharLS is refused, not decoded to a wrong picture,
// and that the error names the cause and the thresholds on each side. The 7-bit
// frame used to decode without an error to an image up to 127 levels off: it
// is a valid stream for other thresholds, so it only shows by ending with 91
// bytes unread.
func TestJPEGLSLegacyThresholdFrameIsRefusedByName(t *testing.T) {
	for _, tc := range []struct {
		bits  int
		t87   string
		older string
	}{
		{6, "2,3,5", "2,3,4"},
		{7, "2,3,10", "2,3,4"},
	} {
		stream := readLowBits(t, fmt.Sprintf("dcmtk_p%d.jls", tc.bits))
		got, err := compress.NewJPEGLSDecompressor().Decompress(stream)
		if err == nil {
			t.Errorf("%d bits: decoded, worst sample %d away from the source", tc.bits,
				worstError(got, readLowBits(t, fmt.Sprintf("source_p%d.raw", tc.bits)), tc.bits))
			continue
		}
		msg := err.Error()
		for _, want := range []string{"CharLS before 2.1", "dcmtk", "C.2.4.1.1", "gives " + tc.t87, "thresholds " + tc.older} {
			if !strings.Contains(msg, want) {
				t.Errorf("%d bits: the error does not say %q: %s", tc.bits, want, msg)
			}
		}
	}
}

// TestJPEGLSScanWithDataLeftOverIsRefused proves a scan whose samples are all
// decoded before its entropy-coded data ends is refused: the encoder and the
// decoder disagreed about something, and the samples are not the ones coded.
// The 8-bit frame is CharLS's, with four bytes inserted before EOI; the error
// does not blame the 6 and 7 bit thresholds, which play no part at 8 bits.
func TestJPEGLSScanWithDataLeftOverIsRefused(t *testing.T) {
	stream := readLowBits(t, "charls_p8_near0.jls")
	padded := append(append(append([]byte{}, stream[:len(stream)-2]...), 0x12, 0x34, 0x56, 0x78), stream[len(stream)-2:]...)
	_, err := compress.NewJPEGLSDecompressor().Decompress(padded)
	if err == nil {
		t.Fatal("a scan with four bytes left over decoded")
	}
	if !strings.Contains(err.Error(), "4 bytes of its entropy-coded data unread") || strings.Contains(err.Error(), "CharLS") {
		t.Errorf("error: %v", err)
	}
}

// withLSE returns stream with an LSE type 1 segment (T.87 C.2.4.1.1) inserted
// after the frame header: MAXVAL, T1, T2, T3 and RESET, zero meaning default.
func withLSE(stream []byte, maxVal, t1, t2, t3, reset uint16) []byte {
	sofEnd := 4 + (int(stream[4])<<8 | int(stream[5]))
	lse := []byte{0xFF, 0xF8, 0x00, 0x0D, 0x01}
	for _, v := range []uint16{maxVal, t1, t2, t3, reset} {
		lse = append(lse, byte(v>>8), byte(v))
	}
	out := append([]byte{}, stream[:sofEnd]...)
	out = append(out, lse...)
	return append(out, stream[sofEnd:]...)
}

// TestJPEGLSPresetParametersDefaultOneByOne proves an LSE segment that gives
// some thresholds and leaves the others at zero has each zero one computed as
// T.87 C.2.4.1.1 says, so a frame coded with the defaults still decodes
// exactly; and that a given threshold outside the range of Table C.1 is
// refused rather than adjusted.
func TestJPEGLSPresetParametersDefaultOneByOne(t *testing.T) {
	source := readLowBits(t, "source_p8.raw")
	for _, tc := range []struct {
		name                      string
		maxVal, t1, t2, t3, reset uint16
	}{
		{"T2 given", 0, 0, 7, 0, 0},
		{"T1 and T3 given", 0, 3, 0, 21, 0},
		{"RESET given", 0, 0, 0, 0, 64},
		{"all given", 255, 3, 7, 21, 64},
	} {
		stream := withLSE(readLowBits(t, "charls_p8_near0.jls"), tc.maxVal, tc.t1, tc.t2, tc.t3, tc.reset)
		got, err := compress.NewJPEGLSDecompressor().Decompress(stream)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !bytes.Equal(got, source) {
			t.Errorf("%s: decoded differently, worst sample %d away", tc.name, worstError(got, source, 8))
		}
	}

	for _, tc := range []struct {
		name                      string
		maxVal, t1, t2, t3, reset uint16
		want                      string
	}{
		{"T3 above MAXVAL", 0, 0, 0, 300, 0, "Table C.1"},
		{"T2 below T1", 0, 9, 5, 0, 0, "Table C.1"},
		{"RESET below 3", 0, 0, 0, 0, 2, "RESET 2"},
	} {
		stream := withLSE(readLowBits(t, "charls_p8_near0.jls"), tc.maxVal, tc.t1, tc.t2, tc.t3, tc.reset)
		if _, err := compress.NewJPEGLSDecompressor().Decompress(stream); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error saying %q", tc.name, err, tc.want)
		}
	}
}
