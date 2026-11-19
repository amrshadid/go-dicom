package compress_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// TestFrameHeaderOfEachCodec proves that every decoder with a size in its
// header reads it without decoding: the dcmtk and OpenJPEG codestreams of
// testdata/samplewidth, of a 41x37 gray image, a 19x21 color one and an 11x9
// one at 12 bits, report the columns, rows, components and precision they were
// coded at.
func TestFrameHeaderOfEachCodec(t *testing.T) {
	gray := compress.FrameHeader{Columns: 41, Rows: 37, Components: 1, Precision: 8}
	color := compress.FrameHeader{Columns: 19, Rows: 21, Components: 3, Precision: 8}
	cases := []struct {
		decoder compress.FrameHeaderReader
		stream  string
		want    compress.FrameHeader
	}{
		{compress.NewJPEGLSDecompressor(), "mono8_jpegls.jls", gray},
		{compress.NewJPEGLSDecompressor(), "mono8_jpegls_near.jls", gray},
		{compress.NewJPEGLSDecompressor(), "rgb8_jpegls.jls", color},
		{compress.NewJPEGLosslessDecompressor(), "mono8_sv1.jpg", gray},
		{compress.NewJPEGLosslessDecompressor(), "rgb8_sv1.jpg", color},
		{compress.NewJPEGDecompressor(), "mono8_baseline.jpg", gray},
		{compress.NewJPEGDecompressor(), "mono8_extended.jpg", gray},
		{compress.NewJPEG2000Decompressor(), "mono8_j2k.j2k", gray},
		{compress.NewJPEG2000Decompressor(), "mono12_j2k.j2k",
			compress.FrameHeader{Columns: 11, Rows: 9, Components: 1, Precision: 12}},
	}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join("testdata", "samplewidth", c.stream))
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.decoder.FrameHeader(data)
		if err != nil || got != c.want {
			t.Errorf("%s: %+v, %v; want %+v", c.stream, got, err, c.want)
		}
	}
}

// TestFrameHeaderIsTheOneDecoded proves that the header read before decoding
// is the one the image is decoded at. A marker's bytes inside another
// segment's payload are not a frame header, and a second frame header, which
// would size the image again after the first was held to the data set's, is
// refused by every decoder (T.81 B.2: an image that is not hierarchical has
// one).
func TestFrameHeaderIsTheOneDecoded(t *testing.T) {
	cases := []struct {
		decoder compress.FrameHeaderReader
		file    string
		refusal string
	}{
		{compress.NewJPEGLSDecompressor(), "jpegls/mr_small_lossless.jls", "a second frame header"},
		{compress.NewJPEGLosslessDecompressor(), "jpeglossless/mr_small_sv1.jpgls", "a second frame header"},
		{compress.NewJPEGDecompressor(), "jpegextended/mono_extended_12bit.jpg", "a second frame header"},
		{compress.NewJPEGDecompressor(), "samplewidth/mono8_baseline.jpg", "multiple SOF markers"},
	}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		want, err := c.decoder.FrameHeader(data)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		sof := frameHeaderAt(t, data)
		length := int(data[sof+2])<<8 | int(data[sof+3])
		header := data[sof : sof+2+length]

		// An APP1 segment ahead of the frame holding a frame header of 16x16
		// as its payload.
		decoy := []byte{0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x10, 0x00, 0x10, 0x01, 0x01, 0x11, 0x00}
		app := append([]byte{0xFF, 0xE1, 0x00, byte(len(decoy) + 2)}, decoy...)
		withApp := append(append(append([]byte{}, data[:2]...), app...), data[2:]...)
		if got, err := c.decoder.FrameHeader(withApp); err != nil || got != want {
			t.Errorf("%s with a frame header inside an APP1 payload: %+v, %v; want %+v", c.file, got, err, want)
		}

		// The frame header twice, the second of a 16x16 image.
		second := append([]byte{}, header...)
		second[5], second[6], second[7], second[8] = 0, 16, 0, 16
		twice := append(append(append([]byte{}, data[:sof+len(header)]...), second...), data[sof+len(header):]...)
		if got, err := c.decoder.FrameHeader(twice); err != nil || got != want {
			t.Errorf("%s with two frame headers: FrameHeader gave %+v, %v; want the first, %+v", c.file, got, err, want)
		}
		if _, err := c.decoder.Decompress(twice); err == nil || !strings.Contains(err.Error(), c.refusal) {
			t.Errorf("%s with two frame headers decoded: %v; want %q", c.file, err, c.refusal)
		}
	}
}

// frameHeaderAt is the offset of the first SOF marker in a stream that has no
// segment before it holding one in its payload.
func frameHeaderAt(t *testing.T, data []byte) int {
	t.Helper()
	for i := 2; i+1 < len(data); i++ {
		if data[i] == 0xFF && (data[i+1] == 0xC0 || data[i+1] == 0xC1 || data[i+1] == 0xC3 || data[i+1] == 0xF7) {
			return i
		}
	}
	t.Fatal("no frame header")
	return 0
}

// TestFrameHeaderCheck proves what Check compares: columns, rows and
// components exactly, rows only where the header states them (a T.81 frame
// header may leave them to a DNL segment), and a precision no wider than Bits
// Allocated, with every refusal wrapping ErrFrameMismatch and naming both
// sides.
func TestFrameHeaderCheck(t *testing.T) {
	h := compress.FrameHeader{Columns: 41, Rows: 37, Components: 1, Precision: 12}
	if err := h.Check(41, 37, 1, 16); err != nil {
		t.Errorf("the image itself: %v", err)
	}
	for _, c := range []struct {
		columns, rows, samples, bits int
		says                         string
	}{
		{37, 41, 1, 16, "its codestream is 41x37 with 1 component, and the data set's Columns, Rows and Samples per Pixel are 37, 41 and 1"},
		{41, 37, 3, 16, "are 41, 37 and 3"},
		{41, 36, 1, 16, "are 41, 36 and 1"},
		{41, 37, 1, 8, "it is coded at 12 bits, more than the 8 bits allocated to a sample"},
	} {
		err := h.Check(c.columns, c.rows, c.samples, c.bits)
		if !errors.Is(err, compress.ErrFrameMismatch) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("Check(%d, %d, %d, %d): %v; want %q", c.columns, c.rows, c.samples, c.bits, err, c.says)
		}
	}
	dnl := compress.FrameHeader{Columns: 41, Rows: 0, Components: 1, Precision: 8}
	if err := dnl.Check(41, 37, 1, 8); err != nil {
		t.Errorf("a header whose rows are left to DNL: %v", err)
	}
}

// TestRLEFrameOfSize proves that DecompressFrameOfSize holds each segment to
// the number of pixels it is given, and that DecompressFrame, which is not
// given one, still takes the frame's own.
func TestRLEFrameOfSize(t *testing.T) {
	samples := bytes.Repeat([]byte{7, 9}, 50)
	frame, err := compress.NewRLECompressor().CompressFrame(samples, 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	d := compress.NewRLEDecompressor()
	if got, err := d.DecompressFrameOfSize(frame, 1, 8, 100); err != nil || !bytes.Equal(got, samples) {
		t.Errorf("100 pixels: %v", err)
	}
	if got, err := d.DecompressFrame(frame, 1, 8); err != nil || !bytes.Equal(got, samples) {
		t.Errorf("no size given: %v", err)
	}
	for _, pixels := range []int{98, 101} {
		if _, err := d.DecompressFrameOfSize(frame, 1, 8, pixels); !errors.Is(err, compress.ErrFrameMismatch) {
			t.Errorf("held to %d pixels: %v; want the frame refused", pixels, err)
		}
	}
}

// checkFrameHeaderAgrees fails a fuzz case whose frame decoded to another
// size than its header, read without decoding, says it is: the header is what
// a data set's image is compared with before the decode, so the two must not
// part. A header whose rows are left to DNL is held to its columns alone.
func checkFrameHeaderAgrees(t *testing.T, decoder compress.FrameHeaderReader, data, out []byte, err error) {
	t.Helper()
	if err != nil {
		return
	}
	h, headerErr := decoder.FrameHeader(data)
	if headerErr != nil {
		t.Fatalf("the frame decoded and its header was refused: %v", headerErr)
	}
	width := 1
	if h.Precision > 8 {
		width = 2
	}
	components := h.Components
	if _, ok := decoder.(*compress.JPEGDecompressor); ok && components == 4 {
		components = 3 // a CMYK frame is handed back as RGB
	}
	pixel := components * width
	if pixel == 0 || len(out)%pixel != 0 || (h.Rows != 0 && len(out) != h.Columns*h.Rows*pixel) ||
		(h.Rows == 0 && (len(out)/pixel)%h.Columns != 0) {
		t.Fatalf("the frame decoded to %d bytes and its header says %+v", len(out), h)
	}
}
