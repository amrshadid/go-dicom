package fileset_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/fileset"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
)

// TestAFileSetHeldAsGoValuesWritesTheSameDICOMDIR proves that a file-set
// whose data sets are built in memory with string, []string and number values
// writes the same DICOMDIR, byte for byte, as the same file-set scanned from
// disk.
//
// The record keys, the Specific Character Set and the leaf keys were copied
// only when held as []byte: built in memory, Patient ID, Study Date and the
// rest were written empty, and the character set left out (#420).
func TestAFileSetHeldAsGoValuesWritesTheSameDICOMDIR(t *testing.T) {
	fs := scannedFileSet(t, "CT_small.dcm", "MR_small.dcm", "rtplan.dcm", "test-SR.dcm", "chr_japanese.dcm")
	opts := fileset.DICOMDIROptions{FileSetID: "HELD", SOPInstanceUID: "1.2.826.0.1.3680043.10.511.420.9"}

	fromFiles, err := fs.WriteDICOMDIR(opts)
	if err != nil {
		t.Fatalf("WriteDICOMDIR: %v", err)
	}
	for _, rec := range fs.FileRecords {
		rec.Dataset = heldvalue.AsGoValues(rec.Dataset)
	}
	inMemory, err := fs.WriteDICOMDIR(opts)
	if err != nil {
		t.Fatalf("WriteDICOMDIR, data sets held as Go values: %v", err)
	}
	if !bytes.Equal(fromFiles, inMemory) {
		t.Errorf("the DICOMDIR differs: %d bytes from the files, %d from the same data sets held as Go values",
			len(fromFiles), len(inMemory))
	}
}

// TestARecordHeldAsGoValuesReadsTheSame proves that StringValue answers the
// same for every attribute of every record of a DICOMDIR read from pydicom's
// corpus, and of the same record held as Go values.
func TestARecordHeldAsGoValuesReadsTheSame(t *testing.T) {
	dd := readDICOMDIR(t, filepath.Join(dicomdirDir(t), "DICOMDIR"))
	checked := 0
	dd.Walk(func(rec *fileset.Record) bool {
		held := &fileset.Record{Dataset: heldvalue.AsGoValues(rec.Dataset)}
		for _, elem := range rec.Dataset.GetAll() {
			tg, _ := elem.Tag()
			if want, got := rec.StringValue(tg), held.StringValue(tg); want != got {
				t.Errorf("%s record %s: %q from the file, %q held as Go values", rec.Type, tg, want, got)
			}
			checked++
		}
		return true
	})
	if checked == 0 {
		t.Fatal("no attribute was compared")
	}
}
