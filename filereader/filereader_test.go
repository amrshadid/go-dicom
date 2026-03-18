package filereader_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// TestFileMetaInfo tests the FileMetaInfo struct.
func TestFileMetaInfo(t *testing.T) {
	meta := &filereader.FileMetaInfo{
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

// TestDataElementValue tests the DataElementValue struct.
func TestDataElementValue(t *testing.T) {
	elem := &filereader.DataElementValue{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Doe^John"),
		Length: 8,
	}

	if elem.Tag != tag.New(0x0010, 0x0010) {
		t.Error("DataElementValue.Tag not set correctly")
	}

	if elem.VR != "PN" {
		t.Error("DataElementValue.VR not set correctly")
	}

	if string(elem.Value) != "Doe^John" {
		t.Error("DataElementValue.Value not set correctly")
	}
}

// TestNewDCMFileReader tests creating a new DCM file reader.
func TestNewDCMFileReader(t *testing.T) {
	buf := &bytes.Buffer{}
	mockReader := filebase.NewFileReader(&readWriteSeeker{Buffer: buf})
	dfr := filereader.NewDCMFileReader(mockReader)

	if dfr == nil {
		t.Fatal("NewDCMFileReader returned nil")
	}

	if dfr.GetPosition() != 0 {
		t.Errorf("Initial position = %d, want 0", dfr.GetPosition())
	}
}

// TestGetPosition tests getting the current file position.
func TestGetPosition(t *testing.T) {
	buf := &bytes.Buffer{}
	mockReader := filebase.NewFileReader(&readWriteSeeker{Buffer: buf})
	dfr := filereader.NewDCMFileReader(mockReader)

	if dfr.GetPosition() != 0 {
		t.Errorf("Initial position = %d, want 0", dfr.GetPosition())
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
