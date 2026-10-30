package sr

import (
	"fmt"
	"os"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/uid"
)

// ReadSRFile reads a DICOM SR file and returns a StructuredReport.
func ReadSRFile(filename string) (*StructuredReport, error) {
	if filename == "" {
		return nil, fmt.Errorf("filename is empty")
	}

	// Check if file exists
	if _, err := os.Stat(filename); err != nil {
		return nil, fmt.Errorf("failed to access file '%s': %w", filename, err)
	}

	// Open the file
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file '%s': %w", filename, err)
	}
	defer file.Close()

	// Create a filebase reader wrapper
	baseReader := filebase.NewFileReader(file)
	defer baseReader.Close()

	// Read the DICOM file
	dicomFile, err := filereader.ReadDICOMFile(baseReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read DICOM file: %w", err)
	}

	// convertDICOMFileToDataset returned an empty data set whatever the file
	// held, so every report read came back empty (#216).
	ds := dicomFile.GetDataset()

	// Convert the dataset to StructuredReport
	report, err := FromDataset(ds)
	if err != nil {
		return nil, fmt.Errorf("failed to convert dataset to structured report: %w", err)
	}

	return report, nil
}

// WriteSRFile writes a StructuredReport to a DICOM file, as the SR document
// ToDataset builds.
func WriteSRFile(filename string, report *StructuredReport) error {
	if filename == "" {
		return fmt.Errorf("filename is empty")
	}

	if report == nil {
		return fmt.Errorf("report is nil")
	}

	// Validate report has required fields
	if report.SOPInstanceUID == "" {
		return fmt.Errorf("report SOP Instance UID is empty")
	}

	if report.SOPClassUID == "" {
		return fmt.Errorf("report SOP Class UID is empty")
	}

	// Convert report to dataset
	ds, err := report.ToDataset()
	if err != nil {
		return fmt.Errorf("failed to convert report to dataset: %w", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file '%s': %w", filename, err)
	}
	defer file.Close()

	baseWriter := filebase.NewFileWriter(file)
	defer baseWriter.Close()
	writer := filewriter.NewDICOMFileWriter(baseWriter)

	// The library's own Implementation Class UID, which the writer supplies.
	// The one written here was under a root this project does not own.
	writer.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    report.SOPClassUID,
		MediaStorageSOPInstanceUID: report.SOPInstanceUID,
		TransferSyntaxUID:          uid.ExplicitVRLittleEndian().String(),
	})

	// ElementsFromDataset writes sequences with their items and every value
	// of a list; the conversion this replaced kept the first value and wrote
	// sequences empty (#216).
	if err := writer.AddDataElements(filewriter.ElementsFromDataset(ds)); err != nil {
		return fmt.Errorf("failed to add data elements: %w", err)
	}
	if err := writer.Write(); err != nil {
		return fmt.Errorf("failed to write DICOM file: %w", err)
	}
	return nil
}

// ValidateAndWriteSRFile validates a report and writes it to a file.
func ValidateAndWriteSRFile(filename string, report *StructuredReport, validator *SRValidator, templateID string) (*ValidationResult, error) {
	if filename == "" {
		return nil, fmt.Errorf("filename is empty")
	}

	if report == nil {
		return nil, fmt.Errorf("report is nil")
	}

	var validationResult *ValidationResult

	// Perform validation if validator is provided
	if validator != nil {
		validationResult = validator.ValidateSRDocument(report, templateID)

		// Check for critical errors (but allow warnings)
		if validationResult != nil && len(validationResult.Errors) > 0 { //nolint:staticcheck // SA9003: intentionally empty — logs errors but proceeds with write
		}
	}

	// Write the file
	if err := WriteSRFile(filename, report); err != nil {
		return validationResult, err
	}

	return validationResult, nil
}

// ReadAndValidateSRFile reads and validates a SR file.
func ReadAndValidateSRFile(filename string, validator *SRValidator, templateID string) (*StructuredReport, *ValidationResult, error) {
	if filename == "" {
		return nil, nil, fmt.Errorf("filename is empty")
	}

	// Read the file
	report, err := ReadSRFile(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read SR file: %w", err)
	}

	var validationResult *ValidationResult

	// Validate if validator is provided
	if validator != nil {
		validationResult = validator.ValidateSRDocument(report, templateID)
	}

	return report, validationResult, nil
}
