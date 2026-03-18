// Example: Read a DICOM File and Display Information
//
// This example illustrates how to:
// - Open and read a DICOM file using the filereader
// - Access file meta information
// - Process data elements from the dataset
// - Display file structure and metadata
//
// This example shows low-level DICOM file reading.
// For higher-level operations, see the dataset package.
//

package main

import (
	"fmt"
	"log"
	"os"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// isTextVR returns true if the VR is a text-based representation
func isTextVR(vr string) bool {
	switch vr {
	case "AE", "AS", "CS", "DA", "DS", "DT", "IS", "LO", "LT", "PN", "SH", "ST", "TM", "UI", "UT":
		return true
	default:
		return false
	}
}

// isBinaryVR returns true if the VR is a binary representation
func isBinaryVR(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OW", "UN":
		return true
	default:
		return false
	}
}

func main() {
	// Example: Reading a DICOM file and displaying information

	// Get DICOM file path from command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <dicom-file>\n", os.Args[0])
		os.Exit(1)
	}
	filePath := os.Args[1]

	// Open the DICOM file
	fmt.Printf("Reading DICOM file: %s\n\n", filePath)
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	defer file.Close()

	// Create a filebase reader
	reader := filebase.NewFileReader(file)

	// Read the complete DICOM file
	dicomFile, err := filereader.ReadDICOMFile(reader)
	if err != nil {
		log.Fatalf("Error reading DICOM file: %v", err)
	}

	// Display file meta information
	fmt.Println("=== File Meta Information ===")
	if dicomFile.FileMetaInfo != nil {
		metaInfo := dicomFile.FileMetaInfo
		fmt.Printf("Transfer Syntax UID..: %s\n", metaInfo.TransferSyntaxUID)
		fmt.Printf("SOP Class UID.......: %s\n", metaInfo.MediaStorageSOPClassUID)
		fmt.Printf("SOP Instance UID....: %s\n", metaInfo.MediaStorageSOPInstanceUID)

		if metaInfo.ImplementationClassUID != "" {
			fmt.Printf("Implementation UID..: %s\n", metaInfo.ImplementationClassUID)
		}
		if metaInfo.ImplementationVersionName != "" {
			fmt.Printf("Implementation Name.: %s\n", metaInfo.ImplementationVersionName)
		}
	}

	// Display dataset information
	fmt.Println()
	fmt.Println("=== Dataset Information ===")
	fmt.Printf("Total data elements..: %d\n", len(dicomFile.DataElements))
	fmt.Printf("Explicit VR..........: %v\n", dicomFile.ExplicitVR)
	fmt.Printf("Little Endian........: %v\n", dicomFile.IsLittleEndian)

	// Display first 20 elements
	fmt.Println()
	fmt.Println("=== First 20 Data Elements ===")
	numElements := len(dicomFile.DataElements)
	if numElements > 20 {
		numElements = 20
	}

	for i := 0; i < numElements; i++ {
		elem := dicomFile.DataElements[i]
		if elem != nil {
			// Format value based on VR type
			var valueStr string
			if len(elem.Value) == 0 {
				valueStr = "(empty)"
			} else if isTextVR(elem.VR) {
				// For text VRs, decode as string
				valueStr = string(elem.Value)
				// Truncate long values
				if len(valueStr) > 50 {
					valueStr = valueStr[:50] + "..."
				}
			} else if isBinaryVR(elem.VR) {
				// For binary VRs, show byte count
				valueStr = fmt.Sprintf("<%d bytes>", len(elem.Value))
			} else {
				// For numeric/other VRs, try string conversion
				valueStr = string(elem.Value)
				if len(valueStr) > 50 {
					valueStr = valueStr[:50] + "..."
				}
			}

			fmt.Printf("[%04X,%04X] VR=%s Len=%d Value=%s\n",
				elem.Tag.Group(), elem.Tag.Element(), elem.VR, elem.Length, valueStr)
		}
	}

	if len(dicomFile.DataElements) > 20 {
		fmt.Println()
		fmt.Printf("... and %d more elements\n", len(dicomFile.DataElements)-20)
	}

	fmt.Println()
	fmt.Println("=== Summary ===")
	fmt.Printf("File successfully read with %d elements\n", len(dicomFile.DataElements))
	fmt.Println("Done!")
}
