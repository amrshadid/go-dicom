// Example: Read and Access DICOM Element Values
//
// This example illustrates how to:
// - Read a DICOM file
// - Access specific element values by keyword (native Dataset API)
// - Access elements by tag when keyword is not available
// - Extract patient information
// - Extract study information
// - Handle different element types
// - Access image metadata
// - Retrieve values with error handling
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// PatientInfo holds extracted patient information
type PatientInfo struct {
	PatientName      string
	PatientID        string
	PatientBirthDate string
	PatientAge       string
	PatientSex       string
	PatientWeight    string
}

// StudyInfo holds extracted study information
type StudyInfo struct {
	StudyDescription string
	StudyDate        string
	StudyTime        string
	StudyInstanceUID string
	Modality         string
}

// ImageInfo holds extracted image information
type ImageInfo struct {
	Rows              int64
	Columns           int64
	SeriesDescription string
	SeriesInstanceUID string
	SamplesPerPixel   int64
	PhotometricInterp string
}

// parseIntValue parses an integer value from a string
func parseIntValue(str string) (int64, error) {
	if str == "" {
		return 0, fmt.Errorf("value is empty")
	}
	return strconv.ParseInt(str, 10, 64)
}

// extractPatientInfo extracts patient demographics from DICOM dataset using native keyword API
func extractPatientInfo(ds *dataset.Dataset) PatientInfo {
	info := PatientInfo{}

	// Use native Dataset keyword methods to access elements
	info.PatientName = ds.GetStringByKeywordWithDefault("PatientName", "")
	info.PatientID = ds.GetStringByKeywordWithDefault("PatientID", "")
	info.PatientBirthDate = ds.GetStringByKeywordWithDefault("PatientBirthDate", "")
	info.PatientAge = ds.GetStringByKeywordWithDefault("PatientAge", "")
	info.PatientSex = ds.GetStringByKeywordWithDefault("PatientSex", "")
	info.PatientWeight = ds.GetStringByKeywordWithDefault("PatientWeight", "")

	return info
}

// extractStudyInfo extracts study information from DICOM dataset using native keyword API
func extractStudyInfo(ds *dataset.Dataset) StudyInfo {
	info := StudyInfo{}

	// Use native Dataset keyword methods to access elements
	info.StudyDescription = ds.GetStringByKeywordWithDefault("StudyDescription", "")
	info.StudyDate = ds.GetStringByKeywordWithDefault("StudyDate", "")
	info.StudyTime = ds.GetStringByKeywordWithDefault("StudyTime", "")
	info.StudyInstanceUID = ds.GetStringByKeywordWithDefault("StudyInstanceUID", "")
	info.Modality = ds.GetStringByKeywordWithDefault("Modality", "")

	return info
}

// extractImageInfo extracts image metadata from DICOM dataset using native keyword API
func extractImageInfo(ds *dataset.Dataset) ImageInfo {
	info := ImageInfo{}

	// Get integer values by keyword
	if rowsStr := ds.GetStringByKeyword("Rows"); rowsStr != "" {
		if val, err := parseIntValue(rowsStr); err == nil {
			info.Rows = val
		}
	}

	if colsStr := ds.GetStringByKeyword("Columns"); colsStr != "" {
		if val, err := parseIntValue(colsStr); err == nil {
			info.Columns = val
		}
	}

	if samplesStr := ds.GetStringByKeyword("SamplesPerPixel"); samplesStr != "" {
		if val, err := parseIntValue(samplesStr); err == nil {
			info.SamplesPerPixel = val
		}
	}

	// Get string values
	info.SeriesDescription = ds.GetStringByKeywordWithDefault("SeriesDescription", "")
	info.SeriesInstanceUID = ds.GetStringByKeywordWithDefault("SeriesInstanceUID", "")
	info.PhotometricInterp = ds.GetStringByKeywordWithDefault("PhotometricInterpretation", "")

	return info
}

func main() {
	fmt.Println("=== Read and Access DICOM Element Values ===")

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

	// Convert DataElementValue slice to native Dataset for keyword access
	ds := dataset.NewDataset()
	for _, elem := range dicomFile.DataElements {
		if elem != nil {
			t := tag.New(elem.Tag.Group(), elem.Tag.Element())
			err := ds.SetValue(t, elem.Value)
			if err != nil {
				log.Printf("Warning: could not set element %04X,%04X: %v", elem.Tag.Group(), elem.Tag.Element(), err)
			}
		}
	}

	// Extract and display patient information
	fmt.Println("=== Method 1: Direct Keyword Access ===")

	fmt.Println("Patient Information (Using Keywords):")
	fmt.Println("─────────────────────────────────────────")

	// Access elements using native Dataset keyword API
	fmt.Printf("Patient Name (PatientName):     %s\n", ds.GetStringByKeywordWithDefault("PatientName", "(not set)"))
	fmt.Printf("Patient ID (PatientID):         %s\n", ds.GetStringByKeywordWithDefault("PatientID", "(not set)"))
	fmt.Printf("Birth Date (PatientBirthDate):  %s\n", ds.GetStringByKeywordWithDefault("PatientBirthDate", "(not set)"))
	fmt.Printf("Sex (PatientSex):               %s\n", ds.GetStringByKeywordWithDefault("PatientSex", "(not set)"))
	fmt.Printf("Age (PatientAge):               %s\n", ds.GetStringByKeywordWithDefault("PatientAge", "(not set)"))
	fmt.Printf("Weight (PatientWeight):         %s kg\n", ds.GetStringByKeywordWithDefault("PatientWeight", "(not set)"))

	// Extract and display study information
	fmt.Println()
	fmt.Println("Study Information (Using Keywords):")
	fmt.Println("─────────────────────────────────────────")

	fmt.Printf("Modality (Modality):                   %s\n", ds.GetStringByKeywordWithDefault("Modality", "(not set)"))
	fmt.Printf("Study Description (StudyDescription): %s\n", ds.GetStringByKeywordWithDefault("StudyDescription", "(not set)"))
	fmt.Printf("Series Description (SeriesDescription): %s\n", ds.GetStringByKeywordWithDefault("SeriesDescription", "(not set)"))
	fmt.Printf("Study Date (StudyDate):               %s\n", ds.GetStringByKeywordWithDefault("StudyDate", "(not set)"))

	// Extract and display image information
	fmt.Println()
	fmt.Println("Image Information (Using Keywords):")
	fmt.Println("─────────────────────────────────────────")

	if rowsStr := ds.GetStringByKeyword("Rows"); rowsStr != "" {
		if rows, err := parseIntValue(rowsStr); err == nil {
			fmt.Printf("Rows (Rows):                 %d\n", rows)
		}
	} else {
		fmt.Printf("Rows (Rows):                 (not set)\n")
	}

	if colsStr := ds.GetStringByKeyword("Columns"); colsStr != "" {
		if cols, err := parseIntValue(colsStr); err == nil {
			fmt.Printf("Columns (Columns):           %d\n", cols)
		}
	} else {
		fmt.Printf("Columns (Columns):           (not set)\n")
	}

	if samplesStr := ds.GetStringByKeyword("SamplesPerPixel"); samplesStr != "" {
		if samples, err := parseIntValue(samplesStr); err == nil {
			fmt.Printf("Samples per Pixel (SamplesPerPixel): %d\n", samples)
		}
	} else {
		fmt.Printf("Samples per Pixel (SamplesPerPixel): (not set)\n")
	}

	fmt.Printf("Photometric (PhotometricInterpretation): %s\n", ds.GetStringByKeywordWithDefault("PhotometricInterpretation", "(not set)"))

	// Method 2: Use helper functions for bulk extraction
	fmt.Println()
	fmt.Println("=== Method 2: Bulk Extraction with Helper Functions ===")

	patientInfo := extractPatientInfo(ds)
	studyInfo := extractStudyInfo(ds)
	imageInfo := extractImageInfo(ds)

	fmt.Println("Extracted Patient Information:")
	if patientInfo.PatientName != "" {
		fmt.Printf("  Name: %s\n", patientInfo.PatientName)
	}
	if patientInfo.PatientID != "" {
		fmt.Printf("  ID: %s\n", patientInfo.PatientID)
	}
	if patientInfo.PatientBirthDate != "" {
		fmt.Printf("  Birth Date: %s\n", patientInfo.PatientBirthDate)
	}
	if patientInfo.PatientAge != "" {
		fmt.Printf("  Age: %s\n", patientInfo.PatientAge)
	}
	if patientInfo.PatientSex != "" {
		fmt.Printf("  Sex: %s\n", patientInfo.PatientSex)
	}
	if patientInfo.PatientWeight != "" {
		fmt.Printf("  Weight: %s kg\n", patientInfo.PatientWeight)
	}

	fmt.Println()
	fmt.Println("Extracted Study Information:")
	if studyInfo.StudyDescription != "" {
		fmt.Printf("  Description: %s\n", studyInfo.StudyDescription)
	}
	if studyInfo.StudyDate != "" {
		fmt.Printf("  Date: %s\n", studyInfo.StudyDate)
	}
	if studyInfo.Modality != "" {
		fmt.Printf("  Modality: %s\n", studyInfo.Modality)
	}

	fmt.Println()
	fmt.Println("Extracted Image Information:")
	if imageInfo.Rows > 0 || imageInfo.Columns > 0 {
		fmt.Printf("  Dimensions: %d x %d\n", imageInfo.Rows, imageInfo.Columns)
	}
	if imageInfo.SeriesDescription != "" {
		fmt.Printf("  Series: %s\n", imageInfo.SeriesDescription)
	}
	if imageInfo.PhotometricInterp != "" {
		fmt.Printf("  Photometric: %s\n", imageInfo.PhotometricInterp)
	}

	// Demonstrate error handling with keyword API
	fmt.Println()
	fmt.Println("=== Handling Missing Elements ===")

	// Check if optional elements exist using HasKeyword
	if ds.HasKeyword("ReferencedImageSequence") {
		value := ds.GetValueByKeyword("ReferencedImageSequence")
		fmt.Printf("Referenced Image Sequence found: %d bytes\n", len(value))
	} else {
		fmt.Println("Referenced Image Sequence not found (optional element)")
	}

	// Try to find Performing Physician Name (optional)
	if ds.HasKeyword("PerformingPhysicianName") {
		perfPhys := ds.GetStringByKeyword("PerformingPhysicianName")
		fmt.Printf("Performing Physician: %s\n", perfPhys)
	} else {
		fmt.Println("Performing Physician not present in this file")
	}

	fmt.Println()
	fmt.Println("=== Summary ===")
	fmt.Printf("Total elements in file: %d\n", len(dicomFile.DataElements))
	fmt.Println()
	fmt.Println("✓ Element value access example complete!")
	fmt.Println()
	fmt.Println("Key Takeaways - Using Native Dataset Keyword API:")
	fmt.Println("  1. Use GetStringByKeyword(keyword) to get text values by name")
	fmt.Println("  2. Use GetValueByKeyword(keyword) to get raw byte values")
	fmt.Println("  3. Use GetStringByKeywordWithDefault() to provide fallback values")
	fmt.Println("  4. Use HasKeyword() or ContainsByKeyword() to check existence")
	fmt.Println("  5. Use RemoveByKeyword() to remove elements for anonymization")
	fmt.Println("  6. Use SetStringByKeyword() to modify values (see modify_dicom example)")
	fmt.Println()
	fmt.Println("Common DICOM Keywords:")
	fmt.Println("  Patient Info: PatientName, PatientID, PatientBirthDate, PatientAge, PatientSex, PatientWeight")
	fmt.Println("  Study Info: StudyDescription, StudyDate, StudyTime, StudyInstanceUID, Modality")
	fmt.Println("  Image Info: Rows, Columns, SamplesPerPixel, PhotometricInterpretation, SeriesDescription")
}
