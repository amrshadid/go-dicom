package dataset_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// Redaction needs samples, and the images that most need it are stored
// compressed. Nothing turned a compressed data set into a native one that
// still described itself correctly (#240).

func textAttr(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, _ := elem.GetValue().([]byte)
	return string(bytes.TrimRight(raw, " \x00"))
}

// TestDecompressInPlace: the element holds the samples, the syntax says
// native, and the image reads as it did.
func TestDecompressInPlace(t *testing.T) {
	ds, want := rleImage(t, 2, 1, false)
	before, err := ds.PixelArray()
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatalf("DecompressInPlace: %v", err)
	}
	if got := ds.TransferSyntaxUID(); got != "1.2.840.10008.1.2.1" {
		t.Errorf("transfer syntax %q after decompressing", got)
	}
	elem, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	if raw, _ := elem.GetValue().([]byte); !bytes.Equal(raw, want) {
		t.Errorf("Pixel Data holds %v, want the samples %v", raw, want)
	}
	after, err := ds.PixelArray()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Errorf("the image reads differently after decompressing: %v", err)
	}

	// Native already: nothing to do, and nothing changes.
	if err := ds.DecompressInPlace(); err != nil {
		t.Errorf("a second DecompressInPlace: %v", err)
	}
	if err := dataset.NewDataset().DecompressInPlace(); err != nil {
		t.Errorf("a data set with no pixel data: %v", err)
	}
}

// TestDecompressInPlaceAcrossTheCorpus: for every compressed file pydicom
// decodes, the native data set reads to the same samples as the compressed
// one did. That holds only if Photometric Interpretation and Planar
// Configuration are made to describe what the decoder produced: an RLE color
// image is stored by plane and decoded by pixel, and a 4:2:2 JPEG decodes to
// full-resolution chroma.
func TestDecompressInPlaceAcrossTheCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	checked := 0
	for _, want := range pydicomPixelExpectations {
		if !compress.IsEncapsulated(want.syntax) {
			continue
		}
		t.Run(want.file, func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, want.file))
			if err != nil {
				t.Skipf("%s is not in this corpus", want.file)
			}
			defer func() { _ = f.Close() }()
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("ReadDICOMFile: %v", err)
			}
			ds := df.GetDataset()
			before, err := ds.PixelArrayBySample()
			if err != nil {
				t.Fatalf("decoding the compressed image: %v", err)
			}
			if err := ds.DecompressInPlace(); err != nil {
				t.Fatalf("DecompressInPlace: %v", err)
			}
			after, err := ds.PixelArrayBySample()
			if err != nil {
				t.Fatalf("reading the native image: %v", err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Errorf("the native image differs from the compressed one (%s, planar %s)",
					textAttr(ds, tag.New(0x0028, 0x0004)), textAttr(ds, tag.New(0x0028, 0x0006)))
			}
			if pi := textAttr(ds, tag.New(0x0028, 0x0004)); pi == "YBR_FULL_422" || pi == "YBR_RCT" || pi == "YBR_ICT" {
				t.Errorf("Photometric Interpretation is still %s, which describes the compressed form", pi)
			}
			checked++
		})
	}
	if checked == 0 {
		t.Error("no compressed corpus file was checked")
	}
}
