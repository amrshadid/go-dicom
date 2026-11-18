package cli

import (
	"bytes"
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
// and the options that do not go together.
func TestCompressRefusesWhatItDoesNotWrite(t *testing.T) {
	in := compressFixture(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-syntax", "1.2.840.10008.1.2.4.91"}, "JPEG 2000 (lossy)"},
		{[]string{"-syntax", "jpeg2000-lossy"}, "JPEG 2000 (lossy)"},
		{[]string{"-syntax", "1.2.840.10008.1.2.4.57"}, "JPEG Lossless"},
		{[]string{"-syntax", "jpeg-lossless"}, "JPEG Lossless"},
		{[]string{"-syntax", "1.2.840.10008.1.2.4.51"}, "12-bit"},
		{[]string{"-syntax", "jpeg-ls-near"}, "Near-Lossless"},
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
