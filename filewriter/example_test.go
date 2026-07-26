package filewriter_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// Writing a DICOM Part 10 file: set the file meta information, add elements in
// ascending tag order, then Write and Close.
func Example() {
	path := filepath.Join(os.TempDir(), "example-write.dcm")
	defer os.Remove(path)

	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))

	// The meta header identifies the object and states how the data set that
	// follows is encoded.
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.2", // CT Image Storage
		MediaStorageSOPInstanceUID: "1.2.3.4.5.6.7.8.9",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1", // Explicit VR Little Endian
	})

	// SOP Class and SOP Instance UID must also appear in the data set itself.
	elements := []struct {
		tag       tag.Tag
		vr, value string
	}{
		{tag.New(0x0008, 0x0016), "UI", "1.2.840.10008.5.1.4.1.1.2"},
		{tag.New(0x0008, 0x0018), "UI", "1.2.3.4.5.6.7.8.9"},
		{tag.New(0x0008, 0x0060), "CS", "CT"},
		{tag.New(0x0010, 0x0010), "PN", "Doe^John"},
	}
	for _, e := range elements {
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag:    e.tag,
			VR:     e.vr,
			Value:  []byte(e.value),
			Length: uint32(len(e.value)),
		}); err != nil {
			log.Fatal(err)
		}
	}

	if err := w.Write(); err != nil {
		log.Fatal(err)
	}
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}

	info, _ := os.Stat(path)
	fmt.Println(info.Size() > 128) // preamble plus content

	// Output: true
}

// DICOM requires every value to be of even length. The writer pads odd-length
// values with the VR's designated character — NUL for UI, space for the text
// VRs — because a single odd value misaligns every element after it.
func ExampleDICOMFileWriter_AddDataElement() {
	path := filepath.Join(os.TempDir(), "example-padding.dcm")
	defer os.Remove(path)

	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		TransferSyntaxUID: "1.2.840.10008.1.2.1",
	})

	// 25 characters — odd. The caller does not need to pad it.
	value := "1.2.840.10008.5.1.4.1.1.2"
	err = w.AddDataElement(&filewriter.DataElement{
		Tag:    tag.New(0x0008, 0x0016),
		VR:     "UI",
		Value:  []byte(value),
		Length: uint32(len(value)),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("supplied %d bytes, written padded to %d\n", len(value), len(value)+1)

	// Output: supplied 25 bytes, written padded to 26
}
