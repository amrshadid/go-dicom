package fileset_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/fileset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

func writtenDICOMDIR(t *testing.T, fs *fileset.FileSet, opts fileset.DICOMDIROptions) *fileset.DICOMDIR {
	t.Helper()
	data, err := fs.WriteDICOMDIR(opts)
	if err != nil {
		t.Fatalf("WriteDICOMDIR: %v", err)
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatalf("the generated file does not read back: %v", err)
	}
	dd, err := fileset.ReadDICOMDIR(df)
	if err != nil {
		t.Fatalf("ReadDICOMDIR: %v", err)
	}
	return dd
}

// TestLeafRecordTypeFollowsSOPClass: every instance was indexed as an IMAGE
// record, so an SR was not an SR DOCUMENT nor a plan an RT PLAN (PS3.3 F.5),
// and none carried the keys of its own type (#215).
func TestLeafRecordTypeFollowsSOPClass(t *testing.T) {
	fs := scannedFileSet(t, "test-SR.dcm", "rtplan.dcm", "rtdose.dcm", "rtstruct.dcm", "CT_small.dcm")
	if len(fs.ListFiles()) != 5 {
		t.Skip("all five fixtures are needed for this")
	}
	dd := writtenDICOMDIR(t, fs, fileset.DICOMDIROptions{})

	for recordType, key := range map[string]tag.Tag{
		fileset.RecordSRDocument:     tag.New(0x0040, 0xA491), // Completion Flag
		fileset.RecordRTPlan:         tag.New(0x300A, 0x0002), // RT Plan Label
		fileset.RecordRTDose:         tag.New(0x3004, 0x000A), // Dose Summation Type
		fileset.RecordRTStructureSet: tag.New(0x3006, 0x0002), // Structure Set Label
		fileset.RecordImage:          tag.New(0x0020, 0x0013), // Instance Number
	} {
		records := dd.RecordsOfType(recordType)
		if len(records) != 1 {
			t.Errorf("%d %s records, want 1", len(records), recordType)
			continue
		}
		if records[0].StringValue(key) == "" {
			t.Errorf("the %s record has no %s", recordType, key)
		}
	}

	// The SR's title is a code, which a record built from byte values alone
	// wrote as an empty sequence.
	if records := dd.RecordsOfType(fileset.RecordSRDocument); len(records) == 1 {
		elem, ok := records[0].Dataset.Get(tag.New(0x0040, 0xA043))
		seq, isSeq := elem.GetValue().(*sequence.Sequence)
		if !ok || !isSeq || seq.Length() != 1 {
			t.Error("the SR DOCUMENT record has no Concept Name Code Sequence item")
		}
	}
}

// TestRecordsCarryTheCharacterSet: a record copies its keys from the instance
// but not the character set they are in, so a Japanese patient name read as
// mojibake (#215).
func TestRecordsCarryTheCharacterSet(t *testing.T) {
	fs := scannedFileSet(t, filepath.Join("..", "charset_files", "chrH31.dcm"))
	dd := writtenDICOMDIR(t, fs, fileset.DICOMDIROptions{})

	patients := dd.RecordsOfType(fileset.RecordPatient)
	if len(patients) != 1 {
		t.Fatalf("%d PATIENT records, want 1", len(patients))
	}
	name := patients[0].StringValue(tag.New(0x0010, 0x0010))
	if !strings.Contains(name, "山田") {
		t.Errorf("the patient's name reads back as %q", name)
	}
	if cs := patients[0].StringValue(tag.New(0x0008, 0x0005)); cs != "ISO_IR 192" {
		t.Errorf("the PATIENT record's Specific Character Set is %q, want ISO_IR 192", cs)
	}
}

// TestNonConformantFileIDIsRefused: PS3.10 8.5 allows a File ID of at most 8
// components of at most 8 characters from A-Z, 0-9 and _, and ct.dcm was
// written into the index as it was (#215).
func TestNonConformantFileIDIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeMinimalDICOM(t, filepath.Join(dir, "ct.dcm"))
	fs, err := fileset.NewFileSet(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ScanDirectory(true); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.WriteDICOMDIR(fileset.DICOMDIROptions{}); err == nil {
		t.Error("a DICOMDIR referencing ct.dcm was written")
	}
	if _, err := fs.WriteDICOMDIR(fileset.DICOMDIROptions{AllowNonConformantFileIDs: true}); err != nil {
		t.Errorf("AllowNonConformantFileIDs did not allow it: %v", err)
	}
}
