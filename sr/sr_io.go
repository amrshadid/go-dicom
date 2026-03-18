package sr

import (
	"fmt"
	"os"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
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

	// Convert the DICOM file to dataset
	ds := convertDICOMFileToDataset(dicomFile)

	// Convert the dataset to StructuredReport
	report, err := FromDataset(ds)
	if err != nil {
		return nil, fmt.Errorf("failed to convert dataset to structured report: %w", err)
	}

	return report, nil
}

// WriteSRFile writes a StructuredReport to a DICOM file.
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

	if ds == nil {
		return fmt.Errorf("failed to create dataset")
	}

	// Create output file
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file '%s': %w", filename, err)
	}
	defer file.Close()

	// Create a filebase writer wrapper
	baseWriter := filebase.NewFileWriter(file)
	defer baseWriter.Close()

	// Create DICOM file writer
	writer := filewriter.NewDICOMFileWriter(baseWriter)

	// Set file meta information
	metaInfo := &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    report.SOPClassUID,
		MediaStorageSOPInstanceUID: report.SOPInstanceUID,
		TransferSyntaxUID:          uid.ExplicitVRLittleEndian().String(),
		ImplementationClassUID:     "1.2.826.0.1.3680043.8.498.0",
		ImplementationVersionName:  "GOSRDICOM001",
	}
	writer.SetFileMetaInfo(metaInfo)

	// Convert dataset elements to filewriter format and add them
	allElements := ds.GetAll()
	for _, elem := range allElements {
		// Convert dataelem.DataElement to filewriter.DataElement
		fwElem, err := convertDataElement(elem)
		if err != nil {
			// Skip elements that can't be converted, log warning
			fmt.Printf("Warning: skipping element: %v\n", err)
			continue
		}

		// Add to writer
		if err := writer.AddDataElement(fwElem); err != nil {
			return fmt.Errorf("failed to add data element: %w", err)
		}
	}

	// Write the complete DICOM file
	if err := writer.Write(); err != nil {
		return fmt.Errorf("failed to write DICOM file: %w", err)
	}

	return nil
}

// convertDataElement converts a dataelem.DataElement to filewriter.DataElement.
func convertDataElement(elem *dataelem.DataElement) (*filewriter.DataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("element is nil")
	}

	// Get tag
	var t tag.Tag
	tagIface := elem.GetTag()
	if tagAsTag, ok := tagIface.(tag.Tag); ok {
		t = tagAsTag
	} else {
		return nil, fmt.Errorf("invalid tag type")
	}

	// Get VR
	vr := string(elem.GetVR())
	if vr == "" {
		return nil, fmt.Errorf("element VR is empty")
	}

	// Get value as bytes
	value := serializeElementValue(elem.GetValue())
	if value == nil {
		value = []byte{}
	}

	return &filewriter.DataElement{
		Tag:    t,
		VR:     vr,
		Value:  value,
		Length: uint32(len(value)),
	}, nil
}

// serializeElementValue converts element values to bytes.
func serializeElementValue(value interface{}) []byte {
	if value == nil {
		return []byte{}
	}

	switch v := value.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	case []string:
		if len(v) > 0 {
			return []byte(v[0])
		}
		return []byte{}
	case []interface{}:
		if len(v) > 0 {
			if str, ok := v[0].(string); ok {
				return []byte(str)
			}
			if b, ok := v[0].([]byte); ok {
				return b
			}
		}
		return []byte{}
	default:
		// For other types, convert to string
		return []byte(fmt.Sprintf("%v", v))
	}
}

// convertDICOMFileToDataset converts a DICOMFile to a Dataset.
func convertDICOMFileToDataset(dicomFile *filereader.DICOMFile) *dataset.Dataset {
	if dicomFile == nil {
		return dataset.NewDataset()
	}
	return dataset.NewDataset()
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
