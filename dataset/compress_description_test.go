package dataset_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// everyCoder is every Compress...InPlace method, with what an RGB image
// becomes under it: the photometric interpretation of the samples it codes.
var everyCoder = []struct {
	name     string
	compress func(*dataset.Dataset) error
	rgbAs    string
	lossless bool
}{
	{"RLE", (*dataset.Dataset).CompressRLELosslessInPlace, "RGB", true},
	{"JPEG-LS", (*dataset.Dataset).CompressJPEGLSLosslessInPlace, "RGB", true},
	{"JPEG-LS near-lossless", func(ds *dataset.Dataset) error { return ds.CompressJPEGLSNearLosslessInPlace(2) }, "RGB", false},
	{"JPEG 2000", (*dataset.Dataset).CompressJPEG2000LosslessInPlace, "YBR_RCT", true},
	{"JPEG Lossless", (*dataset.Dataset).CompressJPEGLosslessInPlace, "RGB", true},
	{"JPEG Lossless SV1", (*dataset.Dataset).CompressJPEGLosslessSV1InPlace, "RGB", true},
	{"JPEG Baseline", func(ds *dataset.Dataset) error { return ds.CompressJPEGBaselineInPlace(90) }, "YBR_FULL", false},
}

// checkColorDescription fails unless ds says photometric, Planar
// Configuration 0 and three samples per pixel.
func checkColorDescription(t *testing.T, ds *dataset.Dataset, photometric string) {
	t.Helper()
	if got := textValue(t, ds, tagPhotometric); got != photometric {
		t.Errorf("Photometric Interpretation is %q, want %q", got, photometric)
	}
	if elem, ok := ds.Get(tagPlanar); !ok || binary.LittleEndian.Uint16(elem.GetValue().([]byte)) != 0 {
		t.Error("Planar Configuration is not 0")
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil || info.SamplesPerPixel != 3 {
		t.Errorf("Samples per Pixel: %v, %v", info, err)
	}
}

// TestEveryCoderDescribesNative422AsFullChroma proves that every
// Compress...InPlace method describes a native YBR_FULL_422 image as
// YBR_FULL once coded: each codes the samples pixelBytesForDecoding gives,
// whose chroma is expanded to every pixel, and none of these syntaxes allows
// YBR_FULL_422 for samples of full chroma (PS3.5 8.2). JPEG 2000 kept
// YBR_FULL_422, which PS3.5 Table 8.2.4-1 does not allow at all. A lossless
// one decodes to the expanded samples.
func TestEveryCoderDescribesNative422AsFullChroma(t *testing.T) {
	// Two luminance samples share one chroma pair: Y1 Y2 Cb Cr.
	sub := []byte{10, 20, 100, 200, 30, 40, 110, 210, 50, 60, 120, 220, 70, 80, 130, 230}
	full := []byte{10, 100, 200, 20, 100, 200, 30, 110, 210, 40, 110, 210,
		50, 120, 220, 60, 120, 220, 70, 130, 230, 80, 130, 230}
	for _, coder := range everyCoder {
		t.Run(coder.name, func(t *testing.T) {
			ds, _ := nativeImage(4, 2, 3, 1, 8, 8, false, "YBR_FULL_422", 0)
			if err := ds.SetValue(tagPixels, sub); err != nil {
				t.Fatal(err)
			}
			if err := coder.compress(ds); err != nil {
				t.Fatalf("compress: %v", err)
			}
			checkColorDescription(t, ds, "YBR_FULL")
			if !coder.lossless {
				return
			}
			if err := ds.DecompressInPlace(); err != nil {
				t.Fatal(err)
			}
			if got := storedPixels(t, ds); !bytes.Equal(got[:len(full)], full) {
				t.Errorf("decoded to % d, want % d", got[:len(full)], full)
			}
		})
	}
}

// TestEveryCoderDescribesCorpusColor proves the description each method
// gives pydicom's native YBR_FULL_422 and RGB images: YBR_FULL for the first,
// the coder's own color for the second, Planar Configuration 0 and three
// samples for both.
func TestEveryCoderDescribesCorpusColor(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, file := range []string{"SC_ybr_full_422_uncompressed.dcm", "SC_rgb_small_odd.dcm"} {
		for _, coder := range everyCoder {
			t.Run(file+"/"+coder.name, func(t *testing.T) {
				f, err := os.Open(filepath.Join(dir, file))
				if err != nil {
					t.Skipf("%s is not in this corpus", file)
				}
				defer func() { _ = f.Close() }()
				df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
				if err != nil {
					t.Fatal(err)
				}
				ds := df.GetDataset()
				if err := coder.compress(ds); err != nil {
					t.Fatalf("compress: %v", err)
				}
				want := coder.rgbAs
				if file == "SC_ybr_full_422_uncompressed.dcm" {
					want = "YBR_FULL"
				}
				checkColorDescription(t, ds, want)
			})
		}
	}
}
