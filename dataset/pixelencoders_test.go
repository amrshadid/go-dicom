package dataset_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// TestPixelEncodersAreTheCoders proves the table states what each coder is:
// the syntaxes the Compress…InPlace methods write, each once, lossy where it
// may lose detail, with the parameter it takes. A native syntax, and one this
// library reads but does not write, has no encoder.
func TestPixelEncodersAreTheCoders(t *testing.T) {
	want := map[string]struct {
		lossy bool
		param dataset.EncoderParam
	}{
		compress.RLELossless:             {false, dataset.EncoderParamNone},
		compress.JPEGLSLossless:          {false, dataset.EncoderParamNone},
		compress.JPEGLSNearLossless:      {true, dataset.EncoderParamJPEGLSNear},
		compress.JPEG2000Lossless:        {false, dataset.EncoderParamNone},
		compress.JPEG2000Lossy:           {true, dataset.EncoderParamJPEG2000Rate},
		compress.JPEGLosslessNonHierarch: {false, dataset.EncoderParamNone},
		compress.JPEGLossless:            {false, dataset.EncoderParamNone},
		compress.JPEGBaseline:            {true, dataset.EncoderParamJPEGQuality},
		compress.JPEGExtended:            {true, dataset.EncoderParamJPEGQuality},
	}
	encoders := dataset.PixelEncoders()
	if len(encoders) != len(want) {
		t.Errorf("%d encoders, want %d", len(encoders), len(want))
	}
	for _, e := range encoders {
		w, ok := want[e.UID]
		if !ok {
			t.Errorf("%s (%s) is not a syntax the coders write", e.UID, e.Name)
			continue
		}
		if e.Lossy != w.lossy || e.Param != w.param || e.Name == "" {
			t.Errorf("%s: lossy %v, param %v, name %q; want lossy %v, param %v", e.UID, e.Lossy, e.Param, e.Name, w.lossy, w.param)
		}
		found, ok := dataset.PixelEncoderFor(e.UID)
		if !ok || found.UID != e.UID {
			t.Errorf("PixelEncoderFor(%s) = %v, %v", e.UID, found.UID, ok)
		}
		delete(want, e.UID)
	}
	for _, syntax := range []string{"1.2.840.10008.1.2", "1.2.840.10008.1.2.1", "1.2.840.10008.1.2.1.99",
		"1.2.840.10008.1.2.2", "1.2.840.10008.1.2.4.93", "1.2.840.10008.1.2.4.201", ""} {
		if e, ok := dataset.PixelEncoderFor(syntax); ok {
			t.Errorf("PixelEncoderFor(%q) = %s, want none", syntax, e.Name)
		}
	}
	if err := (dataset.PixelEncoder{}).EncodeInPlace(dataset.NewDataset(), dataset.EncoderParams{}); err == nil {
		t.Error("an encoder with no coder encoded")
	}
	near, _ := dataset.PixelEncoderFor(compress.JPEGLSNearLossless)
	if near.Loses(dataset.EncoderParams{}) || !near.Loses(dataset.EncoderParams{JPEGLSNear: 1}) {
		t.Error("JPEG-LS Near-Lossless loses only at a NEAR above 0")
	}
	encoders[0].UID = "changed"
	if dataset.PixelEncoders()[0].UID == "changed" {
		t.Error("PixelEncoders handed out the table itself")
	}
}

// TestPixelEncoderWritesWhatItsMethodWrites proves an encoder is its
// method: a corpus image coded through the table and through the method,
// with the same parameter, comes out byte for byte the same.
func TestPixelEncoderWritesWhatItsMethodWrites(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	params := dataset.EncoderParams{JPEGQuality: 80, JPEGLSNear: 2, JPEG2000Rate: compress.JPEG2000Rate{Ratio: 10}}
	methods := map[string]func(*dataset.Dataset) error{
		compress.RLELossless:             (*dataset.Dataset).CompressRLELosslessInPlace,
		compress.JPEGLSLossless:          (*dataset.Dataset).CompressJPEGLSLosslessInPlace,
		compress.JPEG2000Lossless:        (*dataset.Dataset).CompressJPEG2000LosslessInPlace,
		compress.JPEGLosslessNonHierarch: (*dataset.Dataset).CompressJPEGLosslessInPlace,
		compress.JPEGLossless:            (*dataset.Dataset).CompressJPEGLosslessSV1InPlace,
		compress.JPEGLSNearLossless: func(ds *dataset.Dataset) error {
			return ds.CompressJPEGLSNearLosslessInPlace(params.JPEGLSNear)
		},
		compress.JPEG2000Lossy: func(ds *dataset.Dataset) error { return ds.CompressJPEG2000InPlace(params.JPEG2000Rate) },
		compress.JPEGBaseline:  func(ds *dataset.Dataset) error { return ds.CompressJPEGBaselineInPlace(params.JPEGQuality) },
		compress.JPEGExtended:  func(ds *dataset.Dataset) error { return ds.CompressJPEGExtendedInPlace(params.JPEGQuality) },
	}
	read := func(t *testing.T, name string) *dataset.Dataset {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
		if err != nil {
			t.Fatal(err)
		}
		return df.GetDataset()
	}
	pixelData := tag.New(0x7FE0, 0x0010)
	for _, name := range []string{"SC_rgb_small_odd.dcm", "MR_small.dcm"} {
		for _, e := range dataset.PixelEncoders() {
			t.Run(name+"/"+e.Name, func(t *testing.T) {
				viaTable, viaMethod := read(t, name), read(t, name)
				errTable, errMethod := e.EncodeInPlace(viaTable, params), methods[e.UID](viaMethod)
				if (errTable == nil) != (errMethod == nil) {
					t.Fatalf("through the table: %v; through the method: %v", errTable, errMethod)
				}
				if errTable != nil {
					return
				}
				a, _ := viaTable.GetEncapsulatedFrame(0)
				b, _ := viaMethod.GetEncapsulatedFrame(0)
				if len(a) == 0 || !bytes.Equal(a, b) || viaTable.TransferSyntaxUID() != e.UID {
					t.Errorf("through the table %d bytes in %s, through the method %d bytes", len(a),
						viaTable.TransferSyntaxUID(), len(b))
				}
				if _, ok := viaTable.Get(pixelData); !ok {
					t.Error("no Pixel Data")
				}
			})
		}
	}
}
