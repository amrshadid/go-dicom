package compress_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The expectations here do not come from this library.
//
// mr_small_sv1.jpgls was produced by dcmtk's dcmcjpeg from pydicom's
// MR_small.dcm, and mr_small_expected.raw is pydicom's reading of that same
// original, uncompressed. So the encoder is one third party and the ground
// truth is another, which is the only arrangement that catches a decoder and a
// test agreeing on a wrong answer — the failure mode every serious defect in
// this project has had.
//
// sc_rgb.jpgls is pydicom's own SC_rgb_jpeg_gdcm.dcm, an 8-bit RGB frame with
// three color components, against pydicom's decode of it.
//
// restart_sv1.jpgls has a restart marker every two rows, which neither dcmtk
// nor the corpus provides. scripts/jpeglossless-restart-fixture.py encodes it
// from T.81 and writes it only once libjpeg decodes it to restart_expected.raw.

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/jpeglossless/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// TestJPEGLosslessMatchesPydicom decodes a 16-bit grayscale frame and compares
// every sample with pydicom's reading of the uncompressed original.
func TestJPEGLosslessMatchesPydicom(t *testing.T) {
	stream := readFixture(t, "mr_small_sv1.jpgls")
	want := readFixture(t, "mr_small_expected.raw")

	decoder := compress.NewJPEGLosslessDecompressor()
	if !decoder.CanDecompress(stream) {
		t.Fatal("CanDecompress rejected a lossless JPEG stream")
	}

	got, err := decoder.Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d (64x64 samples of 16 bits)", len(got), len(want))
	}
	if !bytes.Equal(got, want) {
		for i := 0; i+1 < len(got); i += 2 {
			g := int16(binary.LittleEndian.Uint16(got[i:]))
			w := int16(binary.LittleEndian.Uint16(want[i:]))
			if g != w {
				t.Fatalf("sample %d = %d, pydicom reads %d", i/2, g, w)
			}
		}
		t.Fatal("output differs from pydicom's but no sample did")
	}
}

// TestJPEGLosslessColourComponents covers a frame with three components, where
// the samples of each pixel are interleaved rather than stored as planes.
func TestJPEGLosslessColourComponents(t *testing.T) {
	stream := readFixture(t, "sc_rgb.jpgls")
	want := readFixture(t, "sc_rgb_expected.raw")

	got, err := compress.NewJPEGLosslessDecompressor().Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d (100x100 pixels of 3 samples)", len(got), len(want))
	}
	if !bytes.Equal(got, want) {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("sample %d = %d, pydicom reads %d (pixel %d, component %d)",
					i, got[i], want[i], i/3, i%3)
			}
		}
	}
}

// TestJPEGLosslessRejectsOtherJPEGs guards the boundary with the standard
// library's decoder.
//
// Claiming a baseline frame would turn an image that decodes today into an
// error, so CanDecompress looks for SOF3 rather than merely for SOI, which every
// JPEG variant starts with.
func TestJPEGLosslessRejectsOtherJPEGs(t *testing.T) {
	decoder := compress.NewJPEGLosslessDecompressor()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"baseline SOF0", []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x0B}},
		{"extended SOF1", []byte{0xFF, 0xD8, 0xFF, 0xC1, 0x00, 0x0B}},
		{"differential lossless SOF7", []byte{0xFF, 0xD8, 0xFF, 0xC7, 0x00, 0x0B}},
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

// TestJPEGLosslessRejectsMalformedStreams checks that damaged input produces an
// error rather than a panic or a plausible-looking image.
func TestJPEGLosslessRejectsMalformedStreams(t *testing.T) {
	valid := readFixture(t, "mr_small_sv1.jpgls")
	decoder := compress.NewJPEGLosslessDecompressor()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"header only", valid[:8]},
		{"truncated mid-scan", valid[:len(valid)/2]},
		{"no SOI", valid[2:]},
		{"single byte", valid[:1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A panic here would be the real failure; a wrong-sized result or an
			// error are both acceptable for input that is not a whole frame.
			got, err := decoder.Decompress(tc.data)
			if err == nil && len(got) == 64*64*2 {
				t.Error("a truncated stream decoded to a full frame, which cannot be right")
			}
		})
	}
}

// TestProcess14TransferSyntaxIsRecognized guards the mapping that made the
// decoder unreachable for half its files.
//
// 1.2.840.10008.1.2.4.57 is JPEG Lossless Process 14. The table named
// 1.2.840.10008.1.2.4.71 instead, which the standard does not define at all, so
// a real Process 14 frame matched nothing and fell through to UNCOMPRESSED —
// its compressed bytes handed back as though they were pixels.
func TestProcess14TransferSyntaxIsRecognized(t *testing.T) {
	for _, uid := range []string{
		"1.2.840.10008.1.2.4.57", // Process 14, any selection value
		"1.2.840.10008.1.2.4.70", // Process 14, selection value 1
	} {
		if got := compress.GetCompressionType(uid); got != compress.JPEG_LOSSLESS {
			t.Errorf("GetCompressionType(%s) = %q, want JPEG_LOSSLESS", uid, got)
		}
		if !compress.IsCompressed(uid) {
			t.Errorf("IsCompressed(%s) = false", uid)
		}
	}
}

// TestJPEGLosslessDecoderIsRegistered checks the decoder is reachable through
// the registry, since that is the only route the dataset layer takes.
func TestJPEGLosslessDecoderIsRegistered(t *testing.T) {
	decoder, err := compress.GetExternalRegistry().GetExternalDecoder(compress.JPEG_LOSSLESS)
	if err != nil {
		t.Fatalf("no JPEG_LOSSLESS decoder is registered: %v", err)
	}
	if _, err := decoder.Decompress(readFixture(t, "mr_small_sv1.jpgls")); err != nil {
		t.Errorf("the registered decoder failed on a valid frame: %v", err)
	}
}

// TestJPEGLosslessRestartIntervals decodes frames with a restart marker every
// two rows. At each restart the prediction resets to its default and the first
// line is predicted as a first line (T.81 H.1.2.1). Only the reset at the start
// of the scan was applied, so every interval after the first came out offset;
// selection value 7, which looks up, also shows the first-line rule (#174).
func TestJPEGLosslessRestartIntervals(t *testing.T) {
	want := readFixture(t, "restart_expected.raw")
	for _, name := range []string{"restart_sv1.jpgls", "restart_sv7.jpgls"} {
		t.Run(name, func(t *testing.T) {
			got, err := compress.NewJPEGLosslessDecompressor().Decompress(readFixture(t, name))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !bytes.Equal(got, want) {
				for i := range want {
					if i >= len(got) || got[i] != want[i] {
						t.Fatalf("first difference at row %d column %d of %d bytes", i/16, i%16, len(want))
					}
				}
				t.Fatalf("decoded %d bytes, libjpeg %d", len(got), len(want))
			}
		})
	}
}

// TestJPEGLosslessRefusesARestartIntervalOffARow: a lossless restart interval
// is a whole number of rows (T.81 H.2), and the first-line rule has no meaning
// for one that starts mid-row.
func TestJPEGLosslessRefusesARestartIntervalOffARow(t *testing.T) {
	data := readFixture(t, "restart_sv1.jpgls")
	dri := bytes.Index(data, []byte{0xFF, 0xDD, 0x00, 0x04})
	binary.BigEndian.PutUint16(data[dri+4:], 24) // one and a half rows of 16
	_, err := compress.NewJPEGLosslessDecompressor().Decompress(data)
	if err == nil || !strings.Contains(err.Error(), "restart interval") {
		t.Fatalf("error %v, want one naming the restart interval", err)
	}
}

// TestJPEGLosslessRefusesAScanThatRunsOut: a scan whose entropy-coded data ends
// before its samples do is truncated, and what the decoder would produce past
// that point is invented. A scan ends at a marker, EOI at the latest, and zeros
// fed at a marker were not counted against it, so a 4 KB file declaring a
// 5792x5792 frame decoded to 67 MB of samples without an error (#175).
func TestJPEGLosslessRefusesAScanThatRunsOut(t *testing.T) {
	valid := readFixture(t, "mr_small_sv1.jpgls")
	restart := readFixture(t, "restart_sv1.jpgls")
	eoi := []byte{0xFF, 0xD9}

	enlarged := bytes.Clone(valid)
	sof := bytes.Index(enlarged, []byte{0xFF, 0xC3})
	binary.BigEndian.PutUint16(enlarged[sof+5:], 1024) // Y
	binary.BigEndian.PutUint16(enlarged[sof+7:], 1024) // X

	// The third interval of the restart fixture, cut to half its length.
	rst1 := bytes.Index(restart, []byte{0xFF, 0xD1})
	rst2 := bytes.Index(restart, []byte{0xFF, 0xD2})
	shortInterval := append(bytes.Clone(restart[:rst1+2+(rst2-rst1-2)/2]), restart[rst2:]...)

	for name, data := range map[string][]byte{
		"frame larger than its data":       enlarged,
		"cut mid-scan, then EOI":           append(bytes.Clone(valid[:len(valid)/2]), eoi...),
		"one restart interval cut in half": shortInterval,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := compress.NewJPEGLosslessDecompressor().Decompress(data)
			if err == nil {
				t.Fatalf("decoded %d bytes from a scan that ran out", len(got))
			}
		})
	}
}

// TestJPEGLosslessRefusesAComponentNoScanCovers: a frame declaring two
// components with a scan for one has no samples for the other, and returning
// zeros for it would be as invented as samples decoded past the end.
func TestJPEGLosslessRefusesAComponentNoScanCovers(t *testing.T) {
	valid := readFixture(t, "mr_small_sv1.jpgls")
	sof := bytes.Index(valid, []byte{0xFF, 0xC3})
	length := int(binary.BigEndian.Uint16(valid[sof+2:]))

	// Rewrite the SOF3 with a second component, id 2, that no scan names.
	body := append(bytes.Clone(valid[sof+4:sof+2+length]), 2, 0x11, 0)
	body[5] = 2
	var data []byte
	data = append(data, valid[:sof]...)
	data = append(data, 0xFF, 0xC3, byte((len(body)+2)>>8), byte(len(body)+2))
	data = append(data, body...)
	data = append(data, valid[sof+2+length:]...)

	if got, err := compress.NewJPEGLosslessDecompressor().Decompress(data); err == nil {
		t.Fatalf("decoded %d bytes for a frame with a component no scan covers", len(got))
	}
}
