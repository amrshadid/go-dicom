package filewriter

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/valuerep"
)

// ==================== VR Validation Integration ====================
// The following functionality integrates with the valuerep module for DICOM VR validation.

// ValidationMode represents the validation strictness level for file writing.
type ValidationMode int

const (
	// ValidationNone disables validation completely (fastest, least safe)
	ValidationNone ValidationMode = iota
	// ValidationWarn validates but only logs warnings (default)
	ValidationWarn
	// ValidationStrict validates and returns errors (safest, slowest)
	ValidationStrict
)

// validationMode stores the current validation mode
var validationMode = ValidationWarn

// SetValidationMode sets the global validation mode for file writing.
//
// Example:
//
//	filewriter.SetValidationMode(filewriter.ValidationStrict)
//	// Now all writes will validate strictly
func SetValidationMode(mode ValidationMode) {
	validationMode = mode
}

// GetValidationMode returns the current validation mode.
func GetValidationMode() ValidationMode {
	return validationMode
}

// ValidateElement validates a data element before writing.
//
// This validates:
//   - VR type correctness
//   - Value length constraints
//   - Format compliance (regex patterns)
//
// Returns an error if validation fails.
//
// Example:
//
//	elem := dataelem.NewDataElement(tag.PatientName, dataelem.PN, "Smith^John")
//	if err := ValidateElement(elem); err != nil {
//	    log.Printf("Element validation failed: %v", err)
//	}
func ValidateElement(elem *dataelem.DataElement) error {
	if elem == nil {
		return fmt.Errorf("element is nil")
	}

	return elem.ValidateVR()
}

// ValidateFileMetaInfo validates the file meta information.
//
// This ensures all UIDs and values comply with DICOM VR constraints.
//
// Example:
//
//	metaInfo := &FileMetaInfo{
//	    MediaStorageSOPClassUID: "1.2.840.10008.5.1.4.1.1.2",
//	    TransferSyntaxUID: "1.2.840.10008.1.2.1",
//	}
//	if err := ValidateFileMetaInfo(metaInfo); err != nil {
//	    log.Printf("Meta info validation failed: %v", err)
//	}
func ValidateFileMetaInfo(metaInfo *FileMetaInfo) error {
	if metaInfo == nil {
		return fmt.Errorf("file meta info is nil")
	}

	// Validate MediaStorageSOPClassUID (UI)
	if metaInfo.MediaStorageSOPClassUID != "" {
		if err := valuerep.ValidateUID(metaInfo.MediaStorageSOPClassUID); err != nil {
			return fmt.Errorf("invalid MediaStorageSOPClassUID: %w", err)
		}
	}

	// Validate MediaStorageSOPInstanceUID (UI)
	if metaInfo.MediaStorageSOPInstanceUID != "" {
		if err := valuerep.ValidateUID(metaInfo.MediaStorageSOPInstanceUID); err != nil {
			return fmt.Errorf("invalid MediaStorageSOPInstanceUID: %w", err)
		}
	}

	// Validate TransferSyntaxUID (UI)
	if metaInfo.TransferSyntaxUID != "" {
		if err := valuerep.ValidateUID(metaInfo.TransferSyntaxUID); err != nil {
			return fmt.Errorf("invalid TransferSyntaxUID: %w", err)
		}
	}

	// Validate ImplementationClassUID (UI) if present
	if metaInfo.ImplementationClassUID != "" {
		if err := valuerep.ValidateUID(metaInfo.ImplementationClassUID); err != nil {
			return fmt.Errorf("invalid ImplementationClassUID: %w", err)
		}
	}

	// Validate ImplementationVersionName (SH) if present
	if metaInfo.ImplementationVersionName != "" {
		if err := valuerep.ValidateValue("SH", metaInfo.ImplementationVersionName); err != nil {
			return fmt.Errorf("invalid ImplementationVersionName: %w", err)
		}
	}

	// Validate SourceApplicationEntityTitle (AE) if present
	if metaInfo.SourceApplicationEntityTitle != "" {
		if err := valuerep.ValidateValue("AE", metaInfo.SourceApplicationEntityTitle); err != nil {
			return fmt.Errorf("invalid SourceApplicationEntityTitle: %w", err)
		}
	}

	// Validate SendingApplicationEntityTitle (AE) if present
	if metaInfo.SendingApplicationEntityTitle != "" {
		if err := valuerep.ValidateValue("AE", metaInfo.SendingApplicationEntityTitle); err != nil {
			return fmt.Errorf("invalid SendingApplicationEntityTitle: %w", err)
		}
	}

	// Validate ReceivingApplicationEntityTitle (AE) if present
	if metaInfo.ReceivingApplicationEntityTitle != "" {
		if err := valuerep.ValidateValue("AE", metaInfo.ReceivingApplicationEntityTitle); err != nil {
			return fmt.Errorf("invalid ReceivingApplicationEntityTitle: %w", err)
		}
	}

	return nil
}

// ValidateDataElement validates a DataElement (filewriter type) before writing.
//
// Example:
//
//	elem := &DataElement{
//	    Tag: tag.PatientName,
//	    VR: "PN",
//	    Value: []byte("Smith^John"),
//	}
//	if err := ValidateDataElement(elem); err != nil {
//	    log.Printf("Data element validation failed: %v", err)
//	}
func ValidateDataElement(elem *DataElement) error {
	if elem == nil {
		return fmt.Errorf("element is nil")
	}

	// Check if VR is valid
	if !valuerep.IsValidVR(elem.VR) {
		return fmt.Errorf("invalid VR: %s", elem.VR)
	}

	// Validate value against VR
	// Convert []byte to appropriate type based on VR
	metadata, err := valuerep.GetVRMetadata(elem.VR)
	if err != nil {
		return fmt.Errorf("failed to get VR metadata: %w", err)
	}

	// For string VRs, validate as string
	if metadata.IsString {
		return valuerep.ValidateValue(elem.VR, string(elem.Value))
	}

	// For binary VRs, validate as []byte
	return valuerep.ValidateValue(elem.VR, elem.Value)
}

// DCMFileWriterWithValidation wraps DCMFileWriter with validation capabilities.
type DCMFileWriterWithValidation struct {
	*DCMFileWriter
	mode ValidationMode
}

// NewDCMFileWriterWithValidation creates a new DICOM file writer with validation.
//
// Example:
//
//	writer := filewriter.NewDCMFileWriterWithValidation(baseWriter, filewriter.ValidationStrict)
func NewDCMFileWriterWithValidation(writer *DCMFileWriter, mode ValidationMode) *DCMFileWriterWithValidation {
	return &DCMFileWriterWithValidation{
		DCMFileWriter: writer,
		mode:          mode,
	}
}

// SetValidationModeInstance sets the validation mode for this writer instance.
//
// Example:
//
//	writer.SetValidationModeInstance(filewriter.ValidationStrict)
func (dfw *DCMFileWriterWithValidation) SetValidationModeInstance(mode ValidationMode) {
	dfw.mode = mode
}

// WriteFileMetaInfoWithValidation writes file meta information with validation.
//
// Depending on the validation mode:
//   - ValidationNone: No validation
//   - ValidationWarn: Validates and logs warnings
//   - ValidationStrict: Validates and returns errors
//
// Example:
//
//	metaInfo := &FileMetaInfo{...}
//	if err := writer.WriteFileMetaInfoWithValidation(metaInfo); err != nil {
//	    log.Fatal(err)
//	}
func (dfw *DCMFileWriterWithValidation) WriteFileMetaInfoWithValidation(metaInfo *FileMetaInfo) error {
	// Validate based on mode
	if dfw.mode != ValidationNone {
		if err := ValidateFileMetaInfo(metaInfo); err != nil {
			if dfw.mode == ValidationStrict {
				return err
			}
			// ValidationWarn: log warning but continue
			fmt.Printf("Warning: File meta info validation: %v\n", err)
		}
	}

	// Write using base writer
	return dfw.WriteFileMetaInfo(metaInfo)
}

// WriteElementWithValidation writes a data element with validation.
//
// Example:
//
//	elem := &DataElement{...}
//	if err := writer.WriteElementWithValidation(elem); err != nil {
//	    log.Fatal(err)
//	}
func (dfw *DCMFileWriterWithValidation) WriteElementWithValidation(elem *DataElement) error {
	// Validate based on mode
	if dfw.mode != ValidationNone {
		if err := ValidateDataElement(elem); err != nil {
			if dfw.mode == ValidationStrict {
				return fmt.Errorf("element validation failed: %w", err)
			}
			// ValidationWarn: log warning but continue
			fmt.Printf("Warning: Element validation for tag %s: %v\n", elem.Tag.String(), err)
		}
	}

	// Write using base writer (with explicit VR based on writer's setting)
	return dfw.WriteDataElement(elem, false)
}

// ValidateBeforeWrite checks if the writer is ready to write valid DICOM data.
//
// This can be called before starting to write to ensure configuration is correct.
//
// Example:
//
//	if err := writer.ValidateBeforeWrite(); err != nil {
//	    log.Printf("Writer not ready: %v", err)
//	}
func (dfw *DCMFileWriterWithValidation) ValidateBeforeWrite() error {
	// Check basic writer state
	if dfw.DCMFileWriter == nil {
		return fmt.Errorf("base writer is nil")
	}

	// Additional checks can be added here:
	// - Check transfer syntax is set
	// - Verify file is writable
	// - etc.

	return nil
}

// GetValidationStats returns statistics about validation during writing.
type ValidationStats struct {
	TotalElements     int
	ValidatedElements int
	FailedValidations int
	Warnings          int
}

// TrackingValidationWriter wraps a writer and tracks validation statistics.
type TrackingValidationWriter struct {
	*DCMFileWriterWithValidation
	stats ValidationStats
}

// NewTrackingValidationWriter creates a writer that tracks validation statistics.
//
// Example:
//
//	writer := filewriter.NewTrackingValidationWriter(baseWriter, filewriter.ValidationWarn)
//	// ... write elements ...
//	stats := writer.GetStats()
//	fmt.Printf("Validated %d elements, %d warnings\n", stats.ValidatedElements, stats.Warnings)
func NewTrackingValidationWriter(writer *DCMFileWriter, mode ValidationMode) *TrackingValidationWriter {
	return &TrackingValidationWriter{
		DCMFileWriterWithValidation: NewDCMFileWriterWithValidation(writer, mode),
		stats:                       ValidationStats{},
	}
}

// WriteElementTracked writes an element and tracks validation statistics.
//
// Example:
//
//	if err := writer.WriteElementTracked(elem); err != nil {
//	    log.Fatal(err)
//	}
func (tvw *TrackingValidationWriter) WriteElementTracked(elem *DataElement) error {
	tvw.stats.TotalElements++

	if tvw.mode != ValidationNone {
		tvw.stats.ValidatedElements++
		if err := ValidateDataElement(elem); err != nil {
			tvw.stats.FailedValidations++
			if tvw.mode == ValidationStrict {
				return err
			}
			tvw.stats.Warnings++
			fmt.Printf("Warning: Element validation: %v\n", err)
		}
	}

	return tvw.WriteDataElement(elem, false)
}

// GetStats returns the validation statistics.
//
// Example:
//
//	stats := writer.GetStats()
//	fmt.Printf("Total: %d, Valid: %d, Failed: %d, Warnings: %d\n",
//	    stats.TotalElements, stats.ValidatedElements,
//	    stats.FailedValidations, stats.Warnings)
func (tvw *TrackingValidationWriter) GetStats() ValidationStats {
	return tvw.stats
}

// ResetStats resets the validation statistics.
func (tvw *TrackingValidationWriter) ResetStats() {
	tvw.stats = ValidationStats{}
}
