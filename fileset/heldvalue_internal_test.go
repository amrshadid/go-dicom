package fileset

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestRecordOffsetsHeldAsNumbersReadTheSame proves that recordUint reads the
// offsets and the in-use flag of every record of pydicom's DICOMDIR the same
// when they are held as Go numbers as when read from the file. It asserted
// []byte, so an offset held as a uint32 read as zero: no children, no next
// record (#420).
func TestRecordOffsetsHeldAsNumbersReadTheSame(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	f, err := os.Open(filepath.Join(corpus, "dicomdirtests", "DICOMDIR"))
	if err != nil {
		t.Skipf("DICOMDIR is not present: %v", err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatal(err)
	}
	ds := df.GetDataset()
	elem, ok := ds.Get(tagDirectoryRecordSequence)
	if !ok {
		t.Fatal("no Directory Record Sequence")
	}
	seq, _ := elem.GetValue().(*sequence.Sequence)
	held := heldvalue.AsGoValues(ds)
	heldElem, _ := held.Get(tagDirectoryRecordSequence)
	heldSeq, _ := heldElem.GetValue().(*sequence.Sequence)
	if seq == nil || heldSeq == nil || seq.Length() == 0 {
		t.Fatal("the records were not read")
	}

	nonzero := 0
	for i, item := range seq.Items() {
		record, _ := item.(*dataset.Dataset)
		heldRecord, _ := heldSeq.Items()[i].(*dataset.Dataset)
		for _, tg := range []tag.Tag{tagNextRecordOffset, tagLowerLevelEntityOffset, tagRecordInUseFlag} {
			want, got := recordUint(record, tg), recordUint(heldRecord, tg)
			if want != got {
				t.Errorf("record %d %s: %d from the file, %d held as a Go number", i, tg, want, got)
			}
			if want != 0 {
				nonzero++
			}
		}
	}
	if nonzero == 0 {
		t.Fatal("every value read as zero; nothing was compared")
	}
}
