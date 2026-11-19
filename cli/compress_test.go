package cli

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The data set's coders had no command, so writing a file in another syntax
// meant writing Go or using dcmtk (#390). The committed fixture is an 8-bit
// RGB image stored plane by plane in big endian, which takes every coder.

func runCompress(args ...string) error   { return (&CompressCommand{}).Execute(args) }
func runDecompress(args ...string) error { return (&DecompressCommand{}).Execute(args) }

// compressFixture copies the committed fixture into a temporary directory, so
// a test may change it.
func compressFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "in.dcm")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// pixelValue returns an element's stored bytes, or nil when it is absent.
func pixelValue(ds *dataset.Dataset, t tag.Tag) []byte {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	b, _ := elem.GetValue().([]byte)
	return b
}

// decodedSamples is the image as this library decodes it, pixel by pixel.
func decodedSamples(t *testing.T, ds *dataset.Dataset) []byte {
	t.Helper()
	samples, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return samples
}

// TestCompressWritesEachLosslessSyntax: each lossless target writes its
// syntax in the meta, keeps the instance's UID, and decodes to the samples of
// the source; decompress then writes them native.
func TestCompressWritesEachLosslessSyntax(t *testing.T) {
	in := compressFixture(t)
	_, source := readAnonymized(t, in)
	want := decodedSamples(t, source)
	uid := text(source, tagSOPInstanceUID)

	for name, syntax := range map[string]string{
		"rle":                    "1.2.840.10008.1.2.5",
		"jpeg-ls":                "1.2.840.10008.1.2.4.80",
		"jpeg2000":               "1.2.840.10008.1.2.4.90",
		"1.2.840.10008.1.2.4.80": "1.2.840.10008.1.2.4.80",
		"jpeg-lossless":          "1.2.840.10008.1.2.4.57",
		"jpeg-lossless-sv1":      "1.2.840.10008.1.2.4.70",
		"1.2.840.10008.1.2.4.70": "1.2.840.10008.1.2.4.70",
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.dcm")
			if err := runCompress("-syntax", name, in, out); err != nil {
				t.Fatalf("compress: %v", err)
			}
			meta, ds := readAnonymized(t, out)
			if meta.TransferSyntaxUID != syntax {
				t.Errorf("transfer syntax %s, want %s", meta.TransferSyntaxUID, syntax)
			}
			if text(ds, tagSOPInstanceUID) != uid || meta.MediaStorageSOPInstanceUID != uid {
				t.Error("a lossless compression changed the SOP Instance UID")
			}
			if ds.Contains(tag.New(0x0028, 0x2110)) {
				t.Error("a lossless compression recorded a loss")
			}
			if got := decodedSamples(t, ds); !bytes.Equal(got[:len(want)], want) {
				t.Error("the compressed file does not decode to the source's samples")
			}

			native := filepath.Join(t.TempDir(), "native.dcm")
			if err := runDecompress(out, native); err != nil {
				t.Fatalf("decompress: %v", err)
			}
			meta, ds = readAnonymized(t, native)
			if meta.TransferSyntaxUID != "1.2.840.10008.1.2.1" {
				t.Errorf("decompressed to %s", meta.TransferSyntaxUID)
			}
			if got := pixelValue(ds, tag.New(0x7FE0, 0x0010)); !bytes.Equal(got[:len(want)], want) {
				t.Error("decompress did not write the source's samples")
			}
		})
	}
}

// TestCompressLossyOnlyWhenAskedAndAsANewInstance: JPEG Baseline is refused
// without a quality; with one, the file records the loss (PS3.3
// C.7.6.1.1.5), and the instance has a new UID in the data set and the meta,
// which decompress keeps along with the record.
func TestCompressLossyOnlyWhenAskedAndAsANewInstance(t *testing.T) {
	in := compressFixture(t)
	_, source := readAnonymized(t, in)
	out := filepath.Join(t.TempDir(), "jpeg.dcm")
	if err := runCompress("-syntax", "jpeg", in, out); err == nil || !strings.Contains(err.Error(), "-quality") {
		t.Fatalf("jpeg without -quality: %v", err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a refused lossy compression wrote a file")
	}
	if err := runCompress("-syntax", "jpeg", "-quality", "85", in, out); err != nil {
		t.Fatalf("compress: %v", err)
	}
	meta, ds := readAnonymized(t, out)
	if meta.TransferSyntaxUID != "1.2.840.10008.1.2.4.50" {
		t.Errorf("transfer syntax %s", meta.TransferSyntaxUID)
	}
	newUID := text(ds, tagSOPInstanceUID)
	if newUID == text(source, tagSOPInstanceUID) || meta.MediaStorageSOPInstanceUID != newUID {
		t.Errorf("SOP Instance UID %q, meta %q: want a new one in both", newUID, meta.MediaStorageSOPInstanceUID)
	}
	if text(ds, tag.New(0x0028, 0x2110)) != "01" || text(ds, tag.New(0x0028, 0x2114)) != "ISO_10918_1" ||
		text(ds, tag.New(0x0028, 0x2112)) == "" {
		t.Error("the loss is not recorded: Lossy Image Compression, its ratio and method")
	}

	native := filepath.Join(t.TempDir(), "native.dcm")
	if err := runDecompress(out, native); err != nil {
		t.Fatal(err)
	}
	_, ds = readAnonymized(t, native)
	if text(ds, tagSOPInstanceUID) != newUID || text(ds, tag.New(0x0028, 0x2110)) != "01" {
		t.Error("decompress changed the UID or forgot the loss")
	}
}

// TestCompressRefusesWhatItDoesNotWrite names each syntax it does not write,
// refuses each lossy syntax without its parameter, and refuses the options
// that do not go together.
func TestCompressRefusesWhatItDoesNotWrite(t *testing.T) {
	in := compressFixture(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-syntax", "1.2.840.10008.1.2.4.91"}, "-ratio"},
		{[]string{"-syntax", "jpeg2000-lossy"}, "-psnr"},
		{[]string{"-syntax", "jpeg2000-lossy", "-ratio", "10", "-psnr", "40"}, "not both"},
		{[]string{"-syntax", "jpeg2000-lossy", "-ratio", "0.5"}, "1 or more"},
		{[]string{"-syntax", "jpeg2000-lossy", "-quality", "90"}, "-quality is not for JPEG 2000"},
		{[]string{"-syntax", "1.2.840.10008.1.2.4.51"}, "-quality"},
		{[]string{"-syntax", "jpeg-extended", "-quality", "0"}, "-quality"},
		{[]string{"-syntax", "jpeg-ls-near"}, "-near"},
		{[]string{"-syntax", "jpeg-ls-near", "-near", "300"}, "0 to 255"},
		{[]string{"-syntax", "jpeg-ls-near", "-near", "2", "-quality", "90"}, "-quality is not for JPEG-LS"},
		{[]string{"-syntax", "jpeg-lossless", "-near", "2"}, "lossless"},
		{[]string{"-syntax", "jpeg", "-psnr", "40", "-quality", "90"}, "-ratio or -psnr is not for JPEG Baseline"},
		{[]string{"-syntax", "1.2.840.10008.1.2.4.201"}, "does not write"},
		{[]string{"-syntax", "1.2.840.10008.1.2"}, "not a compressed syntax"},
		{[]string{"-syntax", "zip"}, "neither"},
		{[]string{}, "-syntax"},
		{[]string{"-syntax", "rle", "-quality", "50"}, "lossless"},
		{[]string{"-syntax", "jpeg", "-quality", "101"}, "1 to 100"},
	} {
		out := filepath.Join(t.TempDir(), "out.dcm")
		err := runCompress(append(c.args, in, out)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("compress %v: %v, want a refusal naming %q", c.args, err, c.want)
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Errorf("compress %v wrote a file", c.args)
		}
	}
	if err := runDecompress(in, filepath.Join(t.TempDir(), "o.dcm")); err == nil || !strings.Contains(err.Error(), "not compressed") {
		t.Errorf("decompressing a native file: %v", err)
	}
}

// TestCompressInPlace: -in-place replaces the input, and nothing else is left
// in its directory.
func TestCompressInPlace(t *testing.T) {
	in := compressFixture(t)
	if err := runCompress("-syntax", "rle", in, in); err == nil || !strings.Contains(err.Error(), "-in-place") {
		t.Errorf("output == input: %v", err)
	}
	if err := runCompress("-syntax", "rle", "-in-place", in); err != nil {
		t.Fatal(err)
	}
	if meta, _ := readAnonymized(t, in); meta.TransferSyntaxUID != "1.2.840.10008.1.2.5" {
		t.Errorf("-in-place left the input in %s", meta.TransferSyntaxUID)
	}
	if entries, _ := os.ReadDir(filepath.Dir(in)); len(entries) != 1 {
		t.Errorf("%d files in the directory, want the input alone", len(entries))
	}
}

// TestCompressWritesEachLossySyntax: JPEG Extended, JPEG-LS Near-Lossless and
// lossy JPEG 2000 write their syntax, record the loss with their method, give
// the instance a new UID, and decode close to the source: within -near for
// JPEG-LS, above a PSNR for the others, within the ratio for -ratio. JPEG-LS
// Near-Lossless at -near 0 is lossless and keeps the UID.
func TestCompressWritesEachLossySyntax(t *testing.T) {
	in := compressFixture(t)
	_, source := readAnonymized(t, in)
	want := decodedSamples(t, source)
	uid := text(source, tagSOPInstanceUID)

	for _, c := range []struct {
		args     []string
		syntax   string
		method   string
		lossy    bool
		worst    int
		minPSNR  float64
		maxBytes int
	}{
		{[]string{"-syntax", "jpeg-extended", "-quality", "90"}, "1.2.840.10008.1.2.4.51", "ISO_10918_1", true, 255, 30, 0},
		{[]string{"-syntax", "jpeg-ls-near", "-near", "3"}, "1.2.840.10008.1.2.4.81", "ISO_14495_1", true, 3, 0, 0},
		{[]string{"-syntax", "jls-near", "-near", "0"}, "1.2.840.10008.1.2.4.81", "", false, 0, 0, 0},
		{[]string{"-syntax", "jpeg2000-lossy", "-psnr", "40"}, "1.2.840.10008.1.2.4.91", "ISO_15444_1", true, 255, 40, 0},
		{[]string{"-syntax", "j2k-lossy", "-ratio", "8"}, "1.2.840.10008.1.2.4.91", "ISO_15444_1", true, 255, 0, len(want) / 8},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.dcm")
			if err := runCompress(append(c.args, in, out)...); err != nil {
				t.Fatalf("compress: %v", err)
			}
			meta, ds := readAnonymized(t, out)
			if meta.TransferSyntaxUID != c.syntax {
				t.Errorf("transfer syntax %s, want %s", meta.TransferSyntaxUID, c.syntax)
			}
			newUID := text(ds, tagSOPInstanceUID)
			switch {
			case c.lossy && (newUID == uid || meta.MediaStorageSOPInstanceUID != newUID):
				t.Errorf("a lossy compression kept SOP Instance UID %q (meta %q)", newUID, meta.MediaStorageSOPInstanceUID)
			case !c.lossy && newUID != uid:
				t.Error("a lossless compression changed the SOP Instance UID")
			}
			if c.lossy && (text(ds, tag.New(0x0028, 0x2110)) != "01" || text(ds, tag.New(0x0028, 0x2114)) != c.method) {
				t.Errorf("the loss is recorded as %q, %q; want 01, %s",
					text(ds, tag.New(0x0028, 0x2110)), text(ds, tag.New(0x0028, 0x2114)), c.method)
			}
			if !c.lossy && ds.Contains(tag.New(0x0028, 0x2110)) {
				t.Error("a lossless compression recorded a loss")
			}
			if c.maxBytes > 0 {
				// The fragments and the offset table carry a few bytes of
				// headers besides the codestream the ratio is of.
				if n := len(pixelValue(ds, tag.New(0x7FE0, 0x0010))); n > c.maxBytes+32 {
					t.Errorf("%d bytes of pixel data, for at most %d", n, c.maxBytes)
				}
			}
			got := decodedSamples(t, ds)
			if len(got) < len(want) {
				t.Fatalf("%d bytes back for %d", len(got), len(want))
			}
			// JPEG Extended codes RGB as YBR_FULL and decodes to it, so it is
			// held against the YBR of the source.
			reference := want
			if c.syntax == "1.2.840.10008.1.2.4.51" {
				reference = ybrOf(want)
			}
			worst, sum := 0, 0.0
			for i := range reference {
				d := int(got[i]) - int(reference[i])
				worst = max(worst, d, -d)
				sum += float64(d * d)
			}
			if worst > c.worst {
				t.Errorf("a sample came back %d away, for at most %d", worst, c.worst)
			}
			if c.minPSNR > 0 && sum > 0 {
				if p := 10 * math.Log10(255*255/(sum/float64(len(reference)))); p < c.minPSNR {
					t.Errorf("PSNR %.1f dB, for at least %g", p, c.minPSNR)
				}
			}
		})
	}
}

// ybrOf converts interleaved 8-bit RGB to YBR_FULL (PS3.3 C.7.6.3.1.2).
func ybrOf(rgb []byte) []byte {
	out := make([]byte, len(rgb))
	clamp := func(v float64) byte { return byte(math.Max(0, math.Min(255, math.Round(v)))) }
	for i := 0; i+2 < len(rgb); i += 3 {
		r, g, b := float64(rgb[i]), float64(rgb[i+1]), float64(rgb[i+2])
		out[i] = clamp(0.299*r + 0.587*g + 0.114*b)
		out[i+1] = clamp(-0.168736*r - 0.331264*g + 0.5*b + 128)
		out[i+2] = clamp(0.5*r - 0.418688*g - 0.081312*b + 128)
	}
	return out
}
