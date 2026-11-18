package compress_test

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The frames in testdata/samplewidth are coded at 8 bits, the
// precision a 16-bit image of 8 bits stored may be coded at. dcmtk's encoders
// made them from 8-bit images (dcmcjpls +pc, dcmcjpls +pc +en, dcmcjpeg +pl +e1,
// +eb, +ee), and each *_expected.raw is dcmtk's own decode of the same
// codestream (dcmdjpls, dcmdjpeg +cn), one byte a sample. The JPEG 2000 frames
// are OpenJPEG's (opj_compress), mono8_j2k_expected.raw the 8-bit image it was
// given. No answer here came from this package.

// sampleWidthCase is one 8-bit frame and the decoder it belongs to.
type sampleWidthCase struct {
	name    string
	stream  string
	decoder compress.FrameDecompressor
	// tolerance is 0 for a lossless frame. A lossy one is compared with
	// libjpeg's decode, and T.83 does not ask two DCT decoders to agree exactly.
	tolerance int
}

func sampleWidthCases() []sampleWidthCase {
	return []sampleWidthCase{
		{"JPEG-LS lossless", "mono8_jpegls.jls", compress.NewJPEGLSDecompressor(), 0},
		{"JPEG-LS near-lossless", "mono8_jpegls_near.jls", compress.NewJPEGLSDecompressor(), 0},
		{"JPEG-LS RGB", "rgb8_jpegls.jls", compress.NewJPEGLSDecompressor(), 0},
		{"lossless JPEG", "mono8_sv1.jpg", compress.NewJPEGLosslessDecompressor(), 0},
		{"lossless JPEG RGB", "rgb8_sv1.jpg", compress.NewJPEGLosslessDecompressor(), 0},
		{"JPEG baseline", "mono8_baseline.jpg", compress.NewJPEGDecompressor(), 1},
		{"JPEG extended", "mono8_extended.jpg", compress.NewJPEGDecompressor(), 1},
		{"JPEG 2000", "mono8_j2k.j2k", compress.NewJPEG2000Decompressor(), 0},
	}
}

func readSampleWidthFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/samplewidth/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// TestFrameDecodesAtBitsAllocated proves that every decoder writes a frame of
// 8 bits or fewer two bytes a sample when the data set allocates 16, and that
// the samples are the ones dcmtk decodes from the same codestream.
//
// Unpacked at the codestream's own width, such a frame was half the size of
// the image, and the pixel path returned it as the whole of it.
func TestFrameDecodesAtBitsAllocated(t *testing.T) {
	for _, c := range sampleWidthCases() {
		t.Run(c.name, func(t *testing.T) {
			stream := readSampleWidthFixture(t, c.stream)
			want := readSampleWidthFixture(t, c.stream[:strings.LastIndex(c.stream, ".")]+"_expected.raw")

			got, err := c.decoder.DecompressFrame(stream, 16, 8, 0)
			if err != nil {
				t.Fatalf("DecompressFrame: %v", err)
			}
			if len(got) != 2*len(want) {
				t.Fatalf("decoded %d bytes; %d samples at 16 bits allocated are %d", len(got), len(want), 2*len(want))
			}
			worst := 0
			for i, w := range want {
				g := binary.LittleEndian.Uint16(got[i*2:])
				d := int(g) - int(w)
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst = d
				}
				if d > c.tolerance {
					t.Fatalf("sample %d is %d, dcmtk decodes %d", i, g, w)
				}
			}
			t.Logf("largest difference from dcmtk: %d", worst)

			// At 8 bits allocated the same frame is one byte a sample, the
			// same values.
			narrow, err := c.decoder.DecompressFrame(stream, 8, 8, 0)
			if err != nil {
				t.Fatalf("DecompressFrame at 8 bits allocated: %v", err)
			}
			if len(narrow) != len(want) {
				t.Fatalf("decoded %d bytes at 8 bits allocated, want %d", len(narrow), len(want))
			}
			for i := range narrow {
				if uint16(narrow[i]) != binary.LittleEndian.Uint16(got[i*2:]) {
					t.Fatalf("sample %d is %d at 8 bits allocated and %d at 16", i, narrow[i],
						binary.LittleEndian.Uint16(got[i*2:]))
				}
			}
		})
	}
}

// TestFrameWiderThanBitsAllocatedIsRefused proves that a frame coded at more
// bits than the data set allocates to a sample is an error from every decoder,
// rather than two bytes a sample (twice the image) or its low bits (a picture
// that looks like the right one).
func TestFrameWiderThanBitsAllocatedIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		decoder compress.FrameDecompressor
	}{
		{"JPEG-LS at 16 bits", "testdata/jpegls/mr_small_lossless.jls", compress.NewJPEGLSDecompressor()},
		{"lossless JPEG at 16 bits", "testdata/jpeglossless/mr_small_sv1.jpgls", compress.NewJPEGLosslessDecompressor()},
		{"JPEG extended at 12 bits", "testdata/jpegextended/mono_extended_12bit.jpg", compress.NewJPEGDecompressor()},
		{"JPEG 2000 at 12 bits", "testdata/samplewidth/mono12_j2k.j2k", compress.NewJPEG2000Decompressor()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stream, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			if _, err := c.decoder.DecompressFrame(stream, 16, 16, 0); err != nil {
				t.Fatalf("at 16 bits allocated the frame should decode: %v", err)
			}
			got, err := c.decoder.DecompressFrame(stream, 8, 8, 0)
			if err == nil {
				t.Fatalf("decoded %d bytes into 8 bits allocated; want a refusal", len(got))
			}
			if !strings.Contains(err.Error(), "more than the 8 bits allocated") {
				t.Errorf("the refusal does not say why: %v", err)
			}
		})
	}
}

// checkFrameWidths is the fuzz targets' check of DecompressFrame against
// Decompress on the same input: it fails where Decompress does, and where
// Decompress succeeds it gives the same samples at 16 bits allocated, two
// bytes each, and at 8 either the same bytes or a refusal.
func checkFrameWidths(t *testing.T, decoder compress.FrameDecompressor, data, out []byte, err error) {
	t.Helper()
	wide, wideErr := decoder.DecompressFrame(data, 16, 16, 0)
	if (err == nil) != (wideErr == nil) {
		t.Fatalf("Decompress err %v, DecompressFrame at 16 bits err %v", err, wideErr)
	}
	if err != nil {
		return
	}
	if len(wide) != len(out) && len(wide) != 2*len(out) {
		t.Fatalf("DecompressFrame at 16 bits gave %d bytes for %d from Decompress", len(wide), len(out))
	}
	narrow, narrowErr := decoder.DecompressFrame(data, 8, 8, 0)
	if narrowErr == nil && len(narrow) != len(out) {
		t.Fatalf("DecompressFrame at 8 bits gave %d bytes for %d from Decompress", len(narrow), len(out))
	}
	if narrowErr != nil && len(wide) == 2*len(out) {
		t.Fatalf("an 8-bit frame was refused at 8 bits allocated: %v", narrowErr)
	}
}
