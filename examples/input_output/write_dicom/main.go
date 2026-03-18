// Example: Create and Write a DICOM File
//
// This example illustrates how to:
// - Create a new DICOM dataset using the dataset and dataelem packages
// - Add data elements with proper tags and VRs
// - Write the dataset to a DICOM file using the filewriter package
// - Set file meta information (transfer syntax, SOP UIDs)
//
// Usage:
//
//	go run . output.dcm

package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

func main() {
	// Get output file path from command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <output-dicom-file>\n", os.Args[0])
		os.Exit(1)
	}
	outputPath := os.Args[1]

	fmt.Println("=== Create and Write a DICOM File ===")

	// Build data elements for the dataset.
	// Each element needs a tag, VR, value (as []byte), and length.
	now := time.Now()

	elements := []*filewriter.DataElement{
		// Patient information
		newTextElement(0x0010, 0x0010, "PN", "Doe^John"),      // PatientName
		newTextElement(0x0010, 0x0020, "LO", "PATIENT-12345"), // PatientID
		newTextElement(0x0010, 0x0030, "DA", "19800101"),      // PatientBirthDate
		newTextElement(0x0010, 0x0040, "CS", "M"),             // PatientSex

		// Study information
		newTextElement(0x0008, 0x0020, "DA", now.Format("20060102")),      // StudyDate
		newTextElement(0x0008, 0x0030, "TM", now.Format("150405")),        // StudyTime
		newTextElement(0x0008, 0x0060, "CS", "MR"),                        // Modality
		newTextElement(0x0008, 0x0016, "UI", "1.2.840.10008.5.1.4.1.1.4"), // SOPClassUID (MR Image Storage)
		newTextElement(0x0008, 0x0018, "UI", "1.2.3.4.5.6.7.8.9"),         // SOPInstanceUID
		newTextElement(0x0020, 0x000D, "UI", "1.2.3.4.5.6.7.8"),           // StudyInstanceUID
		newTextElement(0x0020, 0x000E, "UI", "1.2.3.4.5.6.7.9"),           // SeriesInstanceUID
		newTextElement(0x0020, 0x0010, "SH", "STUDY001"),                  // StudyID
	}

	// Create the output file
	file, err := os.Create(outputPath)
	if err != nil {
		log.Fatalf("Error creating output file: %v", err)
	}
	defer file.Close()

	// Create the high-level DICOM file writer.
	// This handles preamble, DICM prefix, and file meta information automatically.
	writer := filebase.NewFileWriter(file)
	dcmFileWriter := filewriter.NewDICOMFileWriter(writer)

	// Set file meta information (group 0002 elements).
	// The writer automatically determines encoding from the transfer syntax.
	metaInfo := &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4", // MR Image Storage
		MediaStorageSOPInstanceUID: "1.2.3.4.5.6.7.8.9",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1", // Explicit VR Little Endian
	}
	dcmFileWriter.SetFileMetaInfo(metaInfo)

	// Add data elements to the writer
	if err := dcmFileWriter.AddDataElements(elements); err != nil {
		log.Fatalf("Error adding data elements: %v", err)
	}

	// Write the complete DICOM file (preamble + DICM + meta info + dataset)
	if err := dcmFileWriter.Write(); err != nil {
		log.Fatalf("Error writing DICOM file: %v", err)
	}

	// Close the writer to flush any buffered data
	if err := dcmFileWriter.Close(); err != nil {
		log.Fatalf("Error closing writer: %v", err)
	}

	fmt.Printf("DICOM file written to: %s\n", outputPath)
	fmt.Printf("Elements written: %d\n", len(elements))

	// Print what was written
	fmt.Println()
	fmt.Println("Dataset contents:")
	for _, elem := range elements {
		fmt.Printf("  (%04X,%04X) %s = %s\n",
			elem.Tag.Group(), elem.Tag.Element(), elem.VR, string(elem.Value))
	}

	fmt.Println()
	fmt.Println("Done!")

	// Suppress unused import warning for dataelem (used conceptually in comments)
	_ = dataelem.PN
}

// newTextElement creates a filewriter.DataElement from a text value.
func newTextElement(group, element uint16, vr, value string) *filewriter.DataElement {
	b := []byte(value)
	return &filewriter.DataElement{
		Tag:    tag.New(group, element),
		VR:     vr,
		Value:  b,
		Length: uint32(len(b)),
	}
}
