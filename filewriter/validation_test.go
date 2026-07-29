package filewriter_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"

	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// TestValidateFileMetaInfoRejectsBadUIDs verifies the meta header is checked
// before it is written.
//
// Every field here is a UID or an AE title, and a file whose meta header is
// malformed is one no other implementation will open — the header is the first
// thing a reader parses and the only thing telling it how to parse the rest.
// Catching it at write time is the difference between a bad file and no file.
func TestValidateFileMetaInfoRejectsBadUIDs(t *testing.T) {
	valid := func() *filewriter.FileMetaInfo {
		return &filewriter.FileMetaInfo{
			MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
			MediaStorageSOPInstanceUID: "1.2.3.4.5",
			TransferSyntaxUID:          "1.2.840.10008.1.2.1",
		}
	}

	if err := filewriter.ValidateFileMetaInfo(valid()); err != nil {
		t.Fatalf("a well-formed meta header was rejected: %v", err)
	}

	if err := filewriter.ValidateFileMetaInfo(nil); err == nil {
		t.Error("a nil meta header was accepted")
	}

	for _, tc := range []struct {
		name   string
		mutate func(*filewriter.FileMetaInfo)
	}{
		{"SOP Class UID with letters", func(m *filewriter.FileMetaInfo) {
			m.MediaStorageSOPClassUID = "1.2.840.abc"
		}},
		{"SOP Instance UID with a trailing dot", func(m *filewriter.FileMetaInfo) {
			m.MediaStorageSOPInstanceUID = "1.2.3.4."
		}},
		{"transfer syntax with spaces", func(m *filewriter.FileMetaInfo) {
			m.TransferSyntaxUID = "1.2.840 10008"
		}},
		{"UID beyond 64 characters", func(m *filewriter.FileMetaInfo) {
			m.MediaStorageSOPInstanceUID = "1." + strings.Repeat("2", 70)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := valid()
			tc.mutate(m)
			if err := filewriter.ValidateFileMetaInfo(m); err == nil {
				t.Error("a malformed meta header was accepted")
			}
		})
	}
}

// TestValidateFileMetaInfoAllowsAbsentOptionalFields verifies the optional
// fields are only checked when present.
//
// Validating an empty optional field would make every minimal file invalid,
// which is worse than not validating at all: the fields exist precisely so a
// writer can omit what it does not know.
func TestValidateFileMetaInfoAllowsAbsentOptionalFields(t *testing.T) {
	m := &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
		// ImplementationClassUID, ImplementationVersionName,
		// SourceApplicationEntityTitle all left empty.
	}
	if err := filewriter.ValidateFileMetaInfo(m); err != nil {
		t.Errorf("absent optional fields were rejected: %v", err)
	}
}

// TestValidateDataElementChecksValueAgainstVR verifies a value is checked
// against the VR it claims.
func TestValidateDataElementChecksValueAgainstVR(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		elem := &filewriter.DataElement{
			Tag: tag.New(0x0008, 0x0060), VR: "CS", Value: []byte("MR"), Length: 2,
		}
		if err := filewriter.ValidateDataElement(elem); err != nil {
			t.Errorf("a well-formed element was rejected: %v", err)
		}
	})

	t.Run("nil element", func(t *testing.T) {
		if err := filewriter.ValidateDataElement(nil); err == nil {
			t.Error("a nil element was accepted")
		}
	})

	t.Run("odd length is padded, not rejected", func(t *testing.T) {
		// PS3.5 §7.1.1 requires every value to occupy an even number of bytes,
		// but the writer pads to satisfy it rather than refusing the value. So
		// validation accepting this is correct; what matters is that the file
		// comes out even, which is checked below rather than assumed.
		elem := &filewriter.DataElement{
			Tag: tag.New(0x0008, 0x0060), VR: "CS", Value: []byte("MRI"), Length: 3,
		}
		if err := filewriter.ValidateDataElement(elem); err != nil {
			t.Errorf("an odd-length value was rejected, though the writer pads it: %v", err)
		}
	})

	t.Run("length disagreeing with the value", func(t *testing.T) {
		elem := &filewriter.DataElement{
			Tag: tag.New(0x0008, 0x0060), VR: "CS", Value: []byte("MR"), Length: 8,
		}
		if err := filewriter.ValidateDataElement(elem); err == nil {
			t.Error("a stated length that does not match the value was accepted")
		}
	})
}

// TestValidationModeIsSettable covers the global mode, since it decides whether
// a validation failure stops a write or is only reported.
func TestValidationModeIsSettable(t *testing.T) {
	original := filewriter.GetValidationMode()
	defer filewriter.SetValidationMode(original)

	for _, mode := range []filewriter.ValidationMode{
		filewriter.ValidationNone,
		filewriter.ValidationWarn,
		filewriter.ValidationStrict,
	} {
		filewriter.SetValidationMode(mode)
		if got := filewriter.GetValidationMode(); got != mode {
			t.Errorf("GetValidationMode = %v after setting %v", got, mode)
		}
	}
}

// TestOddLengthValueIsPaddedOnDisk verifies the writer satisfies PS3.5 §7.1.1
// rather than leaving an odd-length value in the file.
//
// An odd-length value puts the next element's tag one byte out, so a reader
// loses the whole remainder of the file rather than one element. Validation
// permits an odd value precisely because the writer fixes it; that only holds
// if the writer actually does.
func TestOddLengthValueIsPaddedOnDisk(t *testing.T) {
	out := &growBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})

	// Three characters, so the value needs a pad byte.
	if err := w.AddDataElement(&filewriter.DataElement{
		Tag: tag.New(0x0008, 0x0060), VR: "CS", Value: []byte("MRI"), Length: 3,
	}); err != nil {
		t.Fatalf("AddDataElement: %v", err)
	}
	// A second element, so a reader has to find something after the padded one.
	if err := w.AddDataElement(&filewriter.DataElement{
		Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("Doe^John"), Length: 8,
	}); err != nil {
		t.Fatalf("AddDataElement: %v", err)
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The proof that padding worked is that the element after it is found.
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(out.Bytes())))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	if len(df.DataElements) != 2 {
		t.Fatalf("read %d elements, want 2 — an unpadded value would lose the second",
			len(df.DataElements))
	}

	ds := df.GetDataset()
	elem, ok := ds.Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("the element after the padded one was not found")
	}
	if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
		t.Errorf("the element after the padded one = %q, want %q", got, "Doe^John")
	}
}
