package compress_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The expectations here come from outside this library.
//
// mr_small_lossless.jls is pydicom's own MR_small_jpeg_ls_lossless.dcm, and the
// other streams were produced by dcmtk's dcmcjpls from uncompressed fixtures.
// Every expected pixel file is pydicom's reading of the corresponding
// *uncompressed* original, so neither the encoder nor the answer came from the
// code under test.
//
// That matters more here than for most decoders. JPEG-LS adapts as it goes:
// every decoded sample feeds the statistics that decode the next, so a decoder
// with a subtly wrong update rule produces a correct image for hundreds of
// samples and then diverges. Testing against frames this project encoded would
// simply have reproduced the same mistake on both sides.

func readJPEGLSFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/jpegls/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// samplesOf reinterprets packed little-endian bytes as 16-bit values.
func samplesOf(b []byte) []int {
	out := make([]int, len(b)/2)
	for i := range out {
		out[i] = int(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}

// TestJPEGLSLosslessMatchesOriginal decodes pydicom's own JPEG-LS fixture.
func TestJPEGLSLosslessMatchesOriginal(t *testing.T) {
	stream := readJPEGLSFixture(t, "mr_small_lossless.jls")
	want := readJPEGLSFixture(t, "mr_small_expected.raw")

	decoder := compress.NewJPEGLSDecompressor()
	if !decoder.CanDecompress(stream) {
		t.Fatal("CanDecompress rejected a JPEG-LS stream")
	}

	got, err := decoder.Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if !bytes.Equal(got, want) {
		g, w := samplesOf(got), samplesOf(want)
		if len(g) != len(w) {
			t.Fatalf("decoded %d samples, want %d", len(g), len(w))
		}
		for i := range g {
			if g[i] != w[i] {
				t.Fatalf("sample %d = %d, the uncompressed original has %d", i, g[i], w[i])
			}
		}
	}
}

// TestJPEGLSRunMode covers an image built from large flat blocks.
//
// It is here because the medical fixtures barely exercise run mode at all —
// instrumenting the CT and MR frames showed each entering it exactly once, so
// they were passing while saying almost nothing about the run coder. This image
// is mostly runs, and it caught the read-ahead accounting that made a valid
// frame look truncated.
func TestJPEGLSRunMode(t *testing.T) {
	stream := readJPEGLSFixture(t, "flat_lossless.jls")
	want := readJPEGLSFixture(t, "flat_expected.raw")

	got, err := compress.NewJPEGLSDecompressor().Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if !bytes.Equal(got, want) {
		g, w := samplesOf(got), samplesOf(want)
		for i := range g {
			if i >= len(w) || g[i] != w[i] {
				t.Fatalf("sample %d differs: got %d", i, g[i])
			}
		}
		t.Fatal("output differs from the original")
	}
}

// TestJPEGLSNearLosslessStaysWithinBound checks the guarantee near-lossless
// coding actually makes: no sample is off by more than NEAR.
//
// Exact equality is the wrong assertion — the mode is lossy by construction —
// but the bound is not advisory, and a decoder with the reconstruction window
// slightly wrong exceeds it. Here NEAR is 2.
func TestJPEGLSNearLosslessStaysWithinBound(t *testing.T) {
	const near = 2

	stream := readJPEGLSFixture(t, "ct_nearlossless.jls")
	want := samplesOf(readJPEGLSFixture(t, "ct_expected.raw"))

	raw, err := compress.NewJPEGLSDecompressor().Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	got := samplesOf(raw)
	if len(got) != len(want) {
		t.Fatalf("decoded %d samples, want %d", len(got), len(want))
	}

	worst, worstAt := 0, -1
	for i := range got {
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst, worstAt = d, i
		}
	}
	if worst > near {
		t.Fatalf("sample %d is off by %d, more than the NEAR=%d the encoding promises",
			worstAt, worst, near)
	}
	if worst == 0 {
		t.Errorf("a near-lossless frame decoded exactly, which suggests it was not "+
			"the near-lossless path being exercised (worst difference %d)", worst)
	}
}

// TestJPEGLSRefusesColor checks that a multi-component frame is refused rather
// than decoded wrongly.
//
// The interleaved paths decoded small frames correctly and diverged on larger
// ones. A decoder that is right until row 11 is worse than one that declines:
// the output looks like an image either way, and only a comparison against the
// original tells them apart.
func TestJPEGLSRefusesColor(t *testing.T) {
	stream := readJPEGLSFixture(t, "rgb_line_interleaved.jls")

	decoder := compress.NewJPEGLSDecompressor()
	if !decoder.CanDecompress(stream) {
		t.Error("CanDecompress rejected a color JPEG-LS stream; it is still JPEG-LS")
	}

	got, err := decoder.Decompress(stream)
	if err == nil {
		t.Fatalf("a color frame decoded to %d bytes; it should have been refused", len(got))
	}
	if !strings.Contains(err.Error(), "color") && !strings.Contains(err.Error(), "component") {
		t.Errorf("the error does not say why color was refused: %v", err)
	}
}

// TestJPEGLSRejectsOtherJPEGs guards the boundary with the other decoders.
func TestJPEGLSRejectsOtherJPEGs(t *testing.T) {
	decoder := compress.NewJPEGLSDecompressor()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"baseline SOF0", []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x0B}},
		{"lossless SOF3", []byte{0xFF, 0xD8, 0xFF, 0xC3, 0x00, 0x0B}},
		{"not a JPEG", []byte{0x00, 0x01, 0x02, 0x03}},
		{"empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if decoder.CanDecompress(tc.data) {
				t.Errorf("CanDecompress accepted %s", tc.name)
			}
		})
	}
}

// TestJPEGLSRejectsTruncatedStreams checks a damaged frame errors rather than
// silently completing with invented samples.
func TestJPEGLSRejectsTruncatedStreams(t *testing.T) {
	valid := readJPEGLSFixture(t, "mr_small_lossless.jls")
	decoder := compress.NewJPEGLSDecompressor()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"header only", valid[:10]},
		{"half a scan", valid[:len(valid)/2]},
		{"no SOI", valid[2:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decoder.Decompress(tc.data)
			if err == nil && len(got) == 64*64*2 {
				t.Error("a truncated stream decoded to a full frame, which cannot be right")
			}
		})
	}
}

// TestJPEGLSTransferSyntaxesAreRecognized covers both UIDs reaching the decoder.
func TestJPEGLSTransferSyntaxesAreRecognized(t *testing.T) {
	for _, uid := range []string{
		"1.2.840.10008.1.2.4.80", // lossless
		"1.2.840.10008.1.2.4.81", // near-lossless
	} {
		if got := compress.GetCompressionType(uid); got != compress.JPEG_LS {
			t.Errorf("GetCompressionType(%s) = %q, want JPEG_LS", uid, got)
		}
	}
}

// TestJPEGLSDecoderIsRegistered checks the decoder is reachable through the
// registry, since that is the only route the dataset layer takes.
func TestJPEGLSDecoderIsRegistered(t *testing.T) {
	decoder, err := compress.GetExternalRegistry().GetExternalDecoder(compress.JPEG_LS)
	if err != nil {
		t.Fatalf("no JPEG_LS decoder is registered: %v", err)
	}
	if _, err := decoder.Decompress(readJPEGLSFixture(t, "mr_small_lossless.jls")); err != nil {
		t.Errorf("the registered decoder failed on a valid frame: %v", err)
	}
}
