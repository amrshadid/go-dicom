package compress_test

import (
	"bytes"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The encoder and the decoder in this package are written against each other, so
// a round trip through both proves only that they agree. That is exactly the
// failure this project has shipped before: the RLE encoder emitted frames with no
// segment header and its own decompressor accepted them, because it had the
// matching defect.
//
// So there are two kinds of test here. The round trips below establish that the
// encoder is self-consistent and lossless, which is necessary. The dcmtk test at
// the bottom establishes that something with no shared code can read the output,
// which is the part that matters.

// roundTrip encodes and decodes with this package, returning the samples that came
// back.
func roundTrip(t *testing.T, samples []byte, width, height, components, bitsStored int) []byte {
	t.Helper()

	encoded, err := compress.EncodeJPEGLS(samples, width, height, components, bitsStored)
	if err != nil {
		t.Fatalf("EncodeJPEGLS: %v", err)
	}

	// A frame must be a codestream, not a bare entropy segment: a decoder looks
	// for SOI first, and DICOM stores exactly these bytes in a fragment.
	if len(encoded) < 4 || encoded[0] != 0xFF || encoded[1] != 0xD8 {
		t.Fatalf("the output does not begin with SOI: % x", encoded[:min(8, len(encoded))])
	}

	decoded, err := compress.NewJPEGLSDecompressor().Decompress(encoded)
	if err != nil {
		t.Fatalf("decoding what was just encoded: %v", err)
	}
	return decoded
}

func TestJPEGLSRoundTripIsLossless(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		components    int
		bitsStored    int
		samples       func(w, h, c, bytesPerSample int) []byte
	}{
		{
			// A flat image is entirely run mode, which is the half of the coder a
			// gradient never reaches.
			name: "flat 8-bit", width: 16, height: 16, components: 1, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte { return bytes.Repeat([]byte{0x42}, w*h*c*bps) },
		},
		{
			// A gradient is entirely regular mode.
			name: "gradient 8-bit", width: 32, height: 8, components: 1, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte {
				out := make([]byte, w*h*c*bps)
				for i := range out {
					out[i] = byte(i % 251)
				}
				return out
			},
		},
		{
			// Noise defeats prediction, so every escape and limit path gets used.
			name: "noise 8-bit", width: 23, height: 17, components: 1, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte {
				r := rand.New(rand.NewSource(1))
				out := make([]byte, w*h*c*bps)
				for i := range out {
					out[i] = byte(r.Intn(256))
				}
				return out
			},
		},
		{
			// Alternating columns keep the run mode entering and being interrupted.
			name: "stripes 8-bit", width: 20, height: 12, components: 1, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte {
				out := make([]byte, w*h*c*bps)
				for i := range out {
					if (i/bps)%2 == 0 {
						out[i] = 0x10
					} else {
						out[i] = 0xF0
					}
				}
				return out
			},
		},
		{
			// Little endian, as DICOM native pixel data is under the Explicit VR
			// Little Endian syntax this library normalises to.
			name: "flat 16-bit", width: 12, height: 12, components: 1, bitsStored: 16,
			samples: func(w, h, c, bps int) []byte {
				out := make([]byte, w*h*c*bps)
				for i := 0; i < len(out); i += 2 {
					out[i], out[i+1] = 0x34, 0x12
				}
				return out
			},
		},
		{
			name: "noise 16-bit", width: 15, height: 9, components: 1, bitsStored: 16,
			samples: func(w, h, c, bps int) []byte {
				r := rand.New(rand.NewSource(2))
				out := make([]byte, w*h*c*bps)
				for i := 0; i < len(out); i += 2 {
					v := r.Intn(65536)
					out[i], out[i+1] = byte(v), byte(v>>8)
				}
				return out
			},
		},
		{
			// 12 bits is the common medical depth and exercises a maxVal that is
			// not a byte boundary.
			name: "gradient 12-bit", width: 24, height: 10, components: 1, bitsStored: 12,
			samples: func(w, h, c, bps int) []byte {
				out := make([]byte, w*h*c*bps)
				for i := 0; i < len(out); i += 2 {
					v := (i / 2) % 4096
					out[i], out[i+1] = byte(v), byte(v>>8)
				}
				return out
			},
		},
		{
			// Three components, coded as three independent scans.
			name: "rgb 8-bit", width: 10, height: 10, components: 3, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte {
				out := make([]byte, w*h*c*bps)
				for i := 0; i < w*h; i++ {
					out[i*3+0] = byte(i % 256)
					out[i*3+1] = byte((255 - i) % 256)
					out[i*3+2] = 0x80
				}
				return out
			},
		},
		{
			// A single row and a single column: the edge rules in T.87 A.2 are where
			// an encoder and decoder most easily disagree, because there is no line
			// above and no sample to the left.
			name: "single row", width: 17, height: 1, components: 1, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte {
				out := make([]byte, w*h*c*bps)
				for i := range out {
					out[i] = byte(i * 7 % 256)
				}
				return out
			},
		},
		{
			name: "single column", width: 1, height: 17, components: 1, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte {
				out := make([]byte, w*h*c*bps)
				for i := range out {
					out[i] = byte(i * 11 % 256)
				}
				return out
			},
		},
		{
			name: "single pixel", width: 1, height: 1, components: 1, bitsStored: 8,
			samples: func(w, h, c, bps int) []byte { return []byte{0x7F} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bytesPerSample := 1
			if tc.bitsStored > 8 {
				bytesPerSample = 2
			}
			samples := tc.samples(tc.width, tc.height, tc.components, bytesPerSample)

			decoded := roundTrip(t, samples, tc.width, tc.height, tc.components, tc.bitsStored)

			if !bytes.Equal(decoded, samples) {
				// Report the first difference rather than dumping the image: a
				// disagreement is almost always at a specific sample, and which one
				// says which rule diverged.
				for i := range samples {
					if i >= len(decoded) {
						t.Fatalf("decoded %d bytes, want %d", len(decoded), len(samples))
					}
					if decoded[i] != samples[i] {
						t.Fatalf("first difference at byte %d: decoded 0x%02X, want 0x%02X",
							i, decoded[i], samples[i])
					}
				}
				t.Fatalf("decoded %d bytes, want %d", len(decoded), len(samples))
			}
		})
	}
}

// A lossless encoder that produced larger output than its input would be
// pointless, and would also suggest the entropy coder is not working — a flat
// image in particular should compress enormously, since it is all run mode.
func TestJPEGLSCompressesFlatDataWell(t *testing.T) {
	const width, height = 64, 64
	samples := bytes.Repeat([]byte{0x55}, width*height)

	encoded, err := compress.EncodeJPEGLS(samples, width, height, 1, 8)
	if err != nil {
		t.Fatalf("EncodeJPEGLS: %v", err)
	}

	ratio := float64(len(encoded)) / float64(len(samples))
	t.Logf("flat 64x64: %d bytes from %d, ratio %.4f", len(encoded), len(samples), ratio)

	// Generous: a flat image is one long run, so this should be a few dozen bytes
	// plus headers. The check is that run mode is being entered at all — a coder
	// stuck in regular mode would produce something near the input size.
	if ratio > 0.10 {
		t.Errorf("a flat image compressed to %.1f%% of its input, which suggests run mode "+
			"is not being used", ratio*100)
	}
}

func TestEncodeJPEGLSRejectsBadInput(t *testing.T) {
	cases := []struct {
		name                                  string
		samples                               []byte
		width, height, components, bitsStored int
	}{
		{"zero width", make([]byte, 4), 0, 4, 1, 8},
		{"zero height", make([]byte, 4), 4, 0, 1, 8},
		{"no components", make([]byte, 4), 2, 2, 0, 8},
		{"one bit", make([]byte, 4), 2, 2, 1, 1},
		{"seventeen bits", make([]byte, 8), 2, 2, 1, 17},
		{"too few samples", make([]byte, 3), 2, 2, 1, 8},
		{"too many samples", make([]byte, 5), 2, 2, 1, 8},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := compress.EncodeJPEGLS(tc.samples, tc.width, tc.height,
				tc.components, tc.bitsStored); err == nil {
				t.Error("accepted input it should have refused")
			}
		})
	}

	// A sample above the declared precision cannot be coded, and quietly clamping
	// it would return an image that is not the one handed in.
	if _, err := compress.EncodeJPEGLS([]byte{0xFF, 0x00, 0x00, 0x00}, 2, 2, 1, 4); err == nil {
		t.Error("a sample of 255 was accepted at 4 bits stored")
	}
}

// The dcmtk cross-check lives in scripts/interop-test.sh rather than here.
//
// It needs a Part 10 file with encapsulated pixel data to hand to dcmdjpls, and
// the script already has the dcmtk plumbing, the temporary directories and the
// pydicom ground truth the other codec checks use. Hand-rolling an encapsulated
// writer inside a unit test would mostly be testing the test.
//
// What that check establishes is the part these tests cannot: dcmtk shares no code
// with this package, so its decoding the output is evidence the stream is JPEG-LS
// rather than evidence that two functions here agree with each other.

// The check that counts: CharLS reads what this package wrote.
//
// pylibjpeg wraps CharLS, the reference JPEG-LS implementation, and shares no code
// with go-dicom. A round trip through this package's own decoder proves the two
// agree; this proves the bytes are JPEG-LS.
//
// Skipped when pylibjpeg is not installed, so the suite still runs on a machine
// without it — but the skip says what is not being checked rather than passing
// quietly.
func TestJPEGLSOutputIsReadableByCharLS(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, so CharLS cannot be asked to read the output")
	}
	if err := exec.Command(python, "-c", "import pylibjpeg, numpy").Run(); err != nil {
		t.Skip("pylibjpeg is not installed; install it to verify the encoder against CharLS")
	}

	// A pattern with a diagonal, so a decoder that transposed or shifted the image
	// produces an obvious mismatch rather than a plausible one.
	const width, height = 32, 24
	samples := make([]byte, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			switch {
			case x == y:
				samples[y*width+x] = 0xFF
			case x < width/2:
				samples[y*width+x] = byte(0x20 + y)
			default:
				samples[y*width+x] = byte(0xC0 - x)
			}
		}
	}

	encoded, err := compress.EncodeJPEGLS(samples, width, height, 1, 8)
	if err != nil {
		t.Fatalf("EncodeJPEGLS: %v", err)
	}

	dir := t.TempDir()
	framePath := filepath.Join(dir, "frame.jls")
	expectedPath := filepath.Join(dir, "expected.raw")
	if err := os.WriteFile(framePath, encoded, 0o600); err != nil {
		t.Fatalf("writing the frame: %v", err)
	}
	if err := os.WriteFile(expectedPath, samples, 0o600); err != nil {
		t.Fatalf("writing the expected samples: %v", err)
	}

	script := `
import sys, numpy as np
from pylibjpeg import decode

frame = open(sys.argv[1], "rb").read()
expected = np.frombuffer(open(sys.argv[2], "rb").read(), dtype=np.uint8)

decoded = np.asarray(decode(frame)).ravel().astype(np.uint8)
if decoded.size != expected.size:
    print(f"CharLS decoded {decoded.size} samples, want {expected.size}", file=sys.stderr)
    raise SystemExit(1)
if not (decoded == expected).all():
    first = int(np.nonzero(decoded != expected)[0][0])
    print(f"CharLS and go-dicom disagree first at sample {first}: "
          f"{decoded[first]} vs {expected[first]}", file=sys.stderr)
    raise SystemExit(1)
print(f"CharLS decoded {decoded.size} samples identically")
`

	cmd := exec.Command(python, "-c", script, framePath, expectedPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CharLS could not read the frame this package produced: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
