package filewriter_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// TestFileMetaInfo tests the FileMetaInfo struct.
func TestFileMetaInfo(t *testing.T) {
	meta := &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.2",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2",
	}

	if meta.MediaStorageSOPClassUID != "1.2.840.10008.5.1.4.1.1.2" {
		t.Error("FileMetaInfo.MediaStorageSOPClassUID not set correctly")
	}

	if meta.TransferSyntaxUID != "1.2.840.10008.1.2" {
		t.Error("FileMetaInfo.TransferSyntaxUID not set correctly")
	}
}

// TestDataElement tests the DataElement struct.
func TestDataElement(t *testing.T) {
	elem := &filewriter.DataElement{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Doe^John"),
		Length: 8,
	}

	if elem.Tag != tag.New(0x0010, 0x0010) {
		t.Error("DataElement.Tag not set correctly")
	}

	if elem.VR != "PN" {
		t.Error("DataElement.VR not set correctly")
	}

	if string(elem.Value) != "Doe^John" {
		t.Error("DataElement.Value not set correctly")
	}
}

// TestNewDCMFileWriter tests creating a new DCM file writer.
func TestNewDCMFileWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	if dfw == nil {
		t.Fatal("NewDCMFileWriter returned nil")
	}

	if dfw.GetPosition() != 0 {
		t.Errorf("Initial position = %d, want 0", dfw.GetPosition())
	}
}

// TestSetExplicitVR tests setting explicit VR mode.
func TestSetExplicitVR(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	// Just verify methods don't panic
	dfw.SetExplicitVR(false)
	dfw.SetExplicitVR(true)
}

// TestSetLittleEndian tests setting byte order.
func TestSetLittleEndian(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	// Just verify methods don't panic
	dfw.SetLittleEndian(false)
	dfw.SetLittleEndian(true)
}

// TestWritePreamble tests writing the 128-byte preamble.
func TestWritePreamble(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	err := dfw.WritePreamble()
	if err != nil {
		t.Fatalf("WritePreamble() error = %v", err)
	}

	if dfw.GetPosition() != 128 {
		t.Errorf("Position after preamble = %d, want 128", dfw.GetPosition())
	}
}

// TestWriteDICMPrefix tests writing the "DICM" magic string.
func TestWriteDICMPrefix(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	dfw.WritePreamble()
	err := dfw.WriteDICMPrefix()
	if err != nil {
		t.Fatalf("WriteDICMPrefix() error = %v", err)
	}

	if dfw.GetPosition() != 132 {
		t.Errorf("Position after preamble + DICM = %d, want 132", dfw.GetPosition())
	}
}

// TestWriteTag tests writing a DICOM tag.
func TestWriteTag(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	testTag := tag.New(0x0010, 0x0010)
	err := dfw.WriteTag(testTag)
	if err != nil {
		t.Fatalf("WriteTag() error = %v", err)
	}

	if dfw.GetPosition() != 4 {
		t.Errorf("Position after tag = %d, want 4", dfw.GetPosition())
	}
}

// TestWriteDataElement tests writing a data element.
func TestWriteDataElement(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	elem := &filewriter.DataElement{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Smith^John"),
		Length: 11,
	}

	err := dfw.WriteDataElement(elem, true)
	if err != nil {
		t.Fatalf("WriteDataElement() error = %v", err)
	}

	if dfw.GetPosition() == 0 {
		t.Error("Position should have increased after writing element")
	}
}

// TestWriteDataElements tests writing multiple data elements.
func TestWriteDataElements(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	elements := []*filewriter.DataElement{
		{
			Tag:    tag.New(0x0010, 0x0010),
			VR:     "PN",
			Value:  []byte("Smith^John"),
			Length: 11,
		},
		{
			Tag:    tag.New(0x0010, 0x0020),
			VR:     "LO",
			Value:  []byte("12345"),
			Length: 5,
		},
	}

	err := dfw.WriteDataElements(elements)
	if err != nil {
		t.Fatalf("WriteDataElements() error = %v", err)
	}

	if dfw.GetPosition() == 0 {
		t.Error("Position should have increased after writing elements")
	}
}

// TestGetPosition tests getting the current file position.
func TestGetPosition(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	dfw := filewriter.NewDCMFileWriter(mockWriter)

	if dfw.GetPosition() != 0 {
		t.Errorf("Initial position = %d, want 0", dfw.GetPosition())
	}

	dfw.WritePreamble()
	dfw.WriteDICMPrefix()

	if dfw.GetPosition() != 132 {
		t.Errorf("Position after preamble + DICM = %d, want 132", dfw.GetPosition())
	}
}

// readWriteSeeker is a test helper for mock IO operations.
type readWriteSeeker struct {
	*bytes.Buffer
	position int64
}

func (rws *readWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		rws.position = offset
	case io.SeekCurrent:
		rws.position += offset
	case io.SeekEnd:
		rws.position = int64(rws.Len()) + offset
	}
	return rws.position, nil
}
