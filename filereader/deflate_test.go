package filereader_test

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDeflatedTransferSyntax verifies a deflated data set is inflated and
// parsed. The reader previously did not inflate at all, so such a file yielded
// no elements.
func TestDeflatedTransferSyntax(t *testing.T) {
	plain := explicitDataset(binary.LittleEndian)

	df := readFile(t, buildFile("1.2.840.10008.1.2.1.99", plain, true))

	if len(df.DataElements) != 5 {
		t.Fatalf("parsed %d elements from a deflated file, want 5", len(df.DataElements))
	}

	elem, ok := df.GetDataset().Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("PatientName missing from the deflated data set")
	}
	if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
		t.Errorf("PatientName = %q, want %q", got, "Doe^John")
	}
}

// TestDeflatedRejectsDecompressionBomb verifies a deflated data set that
// expands past the limit is refused rather than allocated.
func TestDeflatedRejectsDecompressionBomb(t *testing.T) {
	// Compress far more than the limit; highly repetitive input compresses to a
	// tiny fraction of its output, which is what a bomb exploits.
	var z bytes.Buffer
	w, _ := flate.NewWriter(&z, flate.BestCompression)
	chunk := bytes.Repeat([]byte{0}, 1<<20)
	for written := int64(0); written < filereader.MaxInflatedDatasetSize+(1<<20); written += int64(len(chunk)) {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	_ = w.Close()

	raw := buildFile("1.2.840.10008.1.2.1.99", nil, false)
	raw = append(raw, z.Bytes()...)

	_, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
	if err == nil {
		t.Fatal("expected an error for a data set expanding past the limit, got nil")
	}
}
