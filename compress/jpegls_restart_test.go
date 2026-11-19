package compress_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// JPEG-LS frames with a restart interval: a DRI segment (T.87 C.2.5) and an
// RSTm marker after every Ri lines.
//
// CharLS 2.2 cannot write restart intervals, but at a marker a JPEG-LS coder
// starts again as at the top of the image, so the data of each interval is
// exactly what CharLS writes for those lines coded as an image of their own.
// testdata/jpegls/restart holds frames put together that way: CharLS 2.2's
// coding of each interval, joined with RST0, RST1, ... behind a DRI and one
// frame header. Built the same way from the T.87 test images, CharLS's own
// restart test files (test8_ilv_none_rm_7.jls and the others of its unit
// tests) come out byte for byte. name.raw is what CharLS decodes the pieces
// to, pixel-interleaved, little endian above 8 bits; libjpeg (through
// pylibjpeg-libjpeg) decodes every frame to the same samples.
func readRestart(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "jpegls", "restart", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// restartFrames names the fixtures: 8-bit color in all three interleave modes,
// 12-bit, 16-bit at NEAR 255 with a restart after every line, 2-bit
// near-lossless and 7-bit, with Ri written in 2, 3 and 4 bytes.
var restartFrames = []string{
	"p8_rgb_ilv0_ri7", "p8_rgb_ilv1_ri7", "p8_rgb_ilv2_near2_ri7",
	"p12_ri5", "p16_near255_ri1", "p2_near1_ri3", "p7_ri10",
}

// TestJPEGLSRestartIntervalsDecode proves a frame with restart markers decodes
// to the samples CharLS and libjpeg decode it to, in each interleave mode, at
// 2 to 16 bits, lossless and near-lossless, and with a 16, 24 or 32-bit Ri.
// Every one of them was refused as corrupt, partway down the image, before.
func TestJPEGLSRestartIntervalsDecode(t *testing.T) {
	for _, name := range restartFrames {
		got, err := compress.NewJPEGLSDecompressor().Decompress(readRestart(t, name+".jls"))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if want := readRestart(t, name+".raw"); !bytes.Equal(got, want) {
			t.Errorf("%s: decoded %d bytes differently from CharLS's %d", name, len(got), len(want))
		}
	}
}

// restartMarkers returns the offset of each RSTm marker in stream after its
// first SOS. Stuffing keeps 0xFF followed by a byte of 0x80 or more out of
// entropy-coded data, so each one found is a marker.
func restartMarkers(stream []byte) []int {
	var at []int
	start := bytes.Index(stream, []byte{0xFF, 0xDA})
	for i := start; i+1 < len(stream); i++ {
		if stream[i] == 0xFF && stream[i+1] >= 0xD0 && stream[i+1] <= 0xD7 {
			at = append(at, i)
		}
	}
	return at
}

// TestJPEGLSRestartFillBytesAreSkipped proves 0xFF fill bytes before a restart
// marker, which T.81 B.1.1.2 allows before any marker, are passed over.
func TestJPEGLSRestartFillBytesAreSkipped(t *testing.T) {
	stream := readRestart(t, "p8_rgb_ilv1_ri7.jls")
	at := restartMarkers(stream)[0]
	filled := append(append(append([]byte{}, stream[:at]...), 0xFF, 0xFF, 0xFF), stream[at:]...)
	got, err := compress.NewJPEGLSDecompressor().Decompress(filled)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, readRestart(t, "p8_rgb_ilv1_ri7.raw")) {
		t.Error("decoded differently with fill bytes before the marker")
	}
}

// TestJPEGLSMalformedRestartIsRefused proves a restart sequence that does not
// match its DRI is refused with an error, never decoded to a wrong image and
// never a panic: a marker missing, out of order or at the wrong line, the data
// ending where one is due, and a DRI of a length T.87 does not define.
func TestJPEGLSMalformedRestartIsRefused(t *testing.T) {
	stream := readRestart(t, "p8_rgb_ilv1_ri7.jls")
	at := restartMarkers(stream)
	if len(at) < 2 {
		t.Fatalf("fixture has %d restart markers", len(at))
	}
	dri := bytes.Index(stream, []byte{0xFF, 0xDD, 0x00})
	splice := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	for _, tc := range []struct {
		name   string
		stream []byte
		want   string
	}{
		{"first marker missing", splice(stream[:at[0]], stream[at[0]+2:]), "a restart marker is missing"},
		{"RST1 first", splice(stream[:at[0]+1], []byte{0xD1}, stream[at[0]+2:]), "not RST0"},
		{"markers swapped", splice(stream[:at[0]+1], []byte{0xD1}, stream[at[0]+2:at[1]+1], []byte{0xD0}, stream[at[1]+2:]), "not RST0"},
		{"ends at a marker", splice(stream[:at[0]], []byte{0xFF}), "RST0 was due"},
		{"ends before a marker", splice(stream[:at[0]], []byte{0xFF, 0xD9}), "not RST0"},
		{"interval of 6 lines declared", splice(stream[:dri+4], []byte{0x00, 0x00, 0x06}, stream[dri+7:]), "a restart marker is missing"},
		{"interval of 8 lines declared", splice(stream[:dri+4], []byte{0x00, 0x00, 0x08}, stream[dri+7:]), "runs into RST0 before the 8 lines"},
		{"no restart declared", splice(stream[:dri+4], []byte{0x00, 0x00, 0x00}, stream[dri+7:]), "runs into RST0 in a scan with no restart interval"},
		{"DRI of 5 bytes", splice(stream[:dri+2], []byte{0x00, 0x07, 0, 0, 0, 0, 7}, stream[dri+7:]), "DRI segment carries 5 bytes"},
		{"DRI of 1 byte", splice(stream[:dri+2], []byte{0x00, 0x03, 7}, stream[dri+7:]), "DRI segment carries 1 bytes"},
	} {
		got, err := compress.NewJPEGLSDecompressor().Decompress(tc.stream)
		if err == nil {
			t.Errorf("%s: decoded %d bytes", tc.name, len(got))
			continue
		}
		if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error does not say %q: %v", tc.name, tc.want, err)
		}
	}
}
