package filereader_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// Reading a DICOM file: wrap the file in a byte-order-aware reader, parse it,
// then convert to a Dataset for querying.
//
// This example writes a small file first so that it is self-contained; in
// practice the file already exists.
func Example() {
	path := filepath.Join(os.TempDir(), "example-read.dcm")
	defer os.Remove(path)
	writeSample(path)

	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	dicomFile, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
	if err != nil {
		log.Fatal(err)
	}

	// Non-fatal parse issues are collected rather than printed.
	for _, w := range dicomFile.Warnings {
		fmt.Println("warning:", w)
	}

	fmt.Println(dicomFile.FileMetaInfo.TransferSyntaxUID)

	ds := dicomFile.GetDataset()
	if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
		fmt.Printf("%s\n", trimPad(elem.GetValue().([]byte)))
	}

	// Output:
	// 1.2.840.10008.1.2.1
	// Doe^John
}

// GetDataset materializes the parsed file as a Dataset. Nested sequences
// become child Datasets, so the whole tree is reachable through one API.
func ExampleDICOMFile_GetDataset() {
	path := filepath.Join(os.TempDir(), "example-getdataset.dcm")
	defer os.Remove(path)
	writeSample(path)

	file, _ := os.Open(path)
	defer file.Close()

	dicomFile, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
	if err != nil {
		log.Fatal(err)
	}

	ds := dicomFile.GetDataset()
	fmt.Printf("%d elements\n", ds.Length())

	// Output: 4 elements
}

// writeSample produces a minimal but valid DICOM Part 10 file.
func writeSample(path string) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.2",
		MediaStorageSOPInstanceUID: "1.2.3.4.5.6.7.8.9",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})

	elements := []struct {
		group, element uint16
		vr, value      string
	}{
		{0x0008, 0x0016, "UI", "1.2.840.10008.5.1.4.1.1.2"},
		{0x0008, 0x0018, "UI", "1.2.3.4.5.6.7.8.9"},
		{0x0008, 0x0060, "CS", "CT"},
		{0x0010, 0x0010, "PN", "Doe^John"},
	}
	for _, e := range elements {
		// The writer pads odd-length values to even length as DICOM requires.
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag:    tag.New(e.group, e.element),
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
}

// trimPad removes the NUL or space padding DICOM adds to make values even.
func trimPad(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
