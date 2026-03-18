package dataelem

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/valuerep"
)

// ValidateWithConfig validates a DataElement against the DICOM dictionary
// using the current config validation mode.
//
// This function integrates with the config module to respect:
// - reading_validation_mode (for elements read from files)
// - writing_validation_mode (for elements being written to files)
// - invalid_keyword_behavior
func (de *DataElement) ValidateWithConfig(isReading bool) error {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Get validation mode from config
	settings := config.Get()
	var mode config.ValidationMode
	if isReading {
		mode = settings.GetReadingValidationMode()
	} else {
		mode = settings.GetWritingValidationMode()
	}

	// Perform validation
	err := de.validateInternal()

	// Handle error based on mode
	if err != nil {
		switch mode {
		case config.RAISE:
			return err
		case config.WARN:
			// Log warning
			config.Logger.Warn("DataElement validation warning", "error", err.Error(), "tag", de.getTagString())
			return nil
		case config.IGNORE:
			return nil
		default:
			return err
		}
	}

	return nil
}

// validateInternal performs the actual validation logic.
// Returns an error if validation fails, nil otherwise.
func (de *DataElement) validateInternal() error {
	// Basic validation already exists in ValidateDictionary()
	// We'll extend it here with additional checks

	// Check if value is empty when it shouldn't be
	if de.Value == nil || de.IsEmpty() { //nolint:staticcheck // SA9003
		// Type checking (Type 1, 1C, 2, 2C, 3) requires IOD-level knowledge
		// that depends on the specific SOP Class and module context. A data
		// element's type is not an intrinsic property of the tag itself but
		// rather depends on which IOD module it appears in. Full Type
		// validation is deferred to a higher-level IOD validation layer.
	}

	// Validate against dictionary
	return de.ValidateAgainstDictionary()
}

// Clear sets the value to the appropriate empty value for this VR.
// This is useful for resetting elements while preserving metadata.
func (de *DataElement) Clear() {
	de.mu.Lock()
	defer de.mu.Unlock()

	de.Value = emptyValueForVR(de.VR)
	de.VM = 0
}

// IsPrivate returns whether this data element's tag is a private tag.
// Private tags have odd group numbers.
func (de *DataElement) IsPrivate() bool {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if t, ok := de.tag.(tag.Tag); ok {
		return t.IsPrivate()
	}
	return false
}

// IsRetired returns whether this data element's tag is marked as retired.
func (de *DataElement) IsRetired() bool {
	return de.IsRetiredTag()
}

// Name returns the dictionary name for this data element's tag.
func (de *DataElement) Name() string {
	return de.GetTagName()
}

// ValidateValueMultiplicity validates that the actual number of values
// matches the expected VM from the dictionary.
func (de *DataElement) ValidateValueMultiplicity() error {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Get dictionary info
	info := de.GetDictionaryInfo()
	if info == nil {
		// No dictionary entry, can't validate
		return nil
	}

	// Parse VM from dictionary
	// VM format examples: "1", "1-n", "2-2n", "3-3n"
	expectedVM := info.VM
	if expectedVM == "" {
		return nil
	}

	actualVM := de.VM

	// For sequences, VM is typically "1"
	if de.VR == SQ {
		return nil
	}

	// Parse VM string and validate against actual VM.
	// VM format examples: "1", "1-n", "2-2n", "3-3n", "1-3", "1-99", "0-n"
	if err := validateVMString(expectedVM, actualVM); err != nil {
		return fmt.Errorf("VM validation failed for %s: %w", de.getTagString(), err)
	}

	return nil
}

// validateVMString parses a DICOM VM specification string and validates an
// actual VM value against it.
//
// Supported VM formats:
//   - "1"      - exactly 1 value
//   - "1-n"    - 1 or more values
//   - "2"      - exactly 2 values
//   - "2-2n"   - even number of values, at least 2
//   - "3-3n"   - multiple of 3, at least 3
//   - "1-3"    - 1 to 3 values
//   - "1-99"   - 1 to 99 values
//   - "0-n"    - any number including 0
func validateVMString(vmSpec string, actualVM int) error {
	vmSpec = strings.TrimSpace(vmSpec)
	if vmSpec == "" {
		return nil
	}

	parts := strings.SplitN(vmSpec, "-", 2)

	if len(parts) == 1 {
		// Exact value, e.g. "1", "2", "3"
		expected, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid VM specification %q: %w", vmSpec, err)
		}
		if actualVM != expected {
			return fmt.Errorf("expected VM=%d, got VM=%d", expected, actualVM)
		}
		return nil
	}

	// Range format: "min-max" where max can be "n" or a number or "<digit>n"
	minStr := parts[0]
	maxStr := parts[1]

	minVM, err := strconv.Atoi(minStr)
	if err != nil {
		return fmt.Errorf("invalid VM specification %q: cannot parse minimum: %w", vmSpec, err)
	}

	if actualVM < minVM {
		return fmt.Errorf("VM=%d is below minimum %d (expected %s)", actualVM, minVM, vmSpec)
	}

	// Parse the max part
	if maxStr == "n" {
		// Unbounded, e.g. "1-n", "0-n"
		return nil
	}

	// Check for multiplier format like "2n", "3n"
	if strings.HasSuffix(maxStr, "n") {
		multiplierStr := strings.TrimSuffix(maxStr, "n")
		multiplier, err := strconv.Atoi(multiplierStr)
		if err != nil || multiplier <= 0 {
			return fmt.Errorf("invalid VM specification %q: cannot parse multiplier", vmSpec)
		}
		// actualVM must be a multiple of the multiplier
		if actualVM%multiplier != 0 {
			return fmt.Errorf("VM=%d is not a multiple of %d (expected %s)", actualVM, multiplier, vmSpec)
		}
		return nil
	}

	// Fixed upper bound, e.g. "1-3", "1-99"
	maxVM, err := strconv.Atoi(maxStr)
	if err != nil {
		return fmt.Errorf("invalid VM specification %q: cannot parse maximum: %w", vmSpec, err)
	}

	if actualVM > maxVM {
		return fmt.Errorf("VM=%d exceeds maximum %d (expected %s)", actualVM, maxVM, vmSpec)
	}

	return nil
}

// emptyValueForVR is already defined in convert.go, but we need to access it
// This is a re-export for clarity in this validation context.
// The actual function is in convert.go

// Integration with config flags

// ShouldUseNoneAsEmptyTextVR checks the config for whether None should be used
// as the empty value for text VRs instead of empty string.
func ShouldUseNoneAsEmptyTextVR() bool {
	settings := config.Get()
	return settings.GetUseNoneAsEmptyTextVR()
}

// ShouldApplyJ2KCorrections checks the config for whether JPEG 2000
// compression corrections should be applied during processing.
func ShouldApplyJ2KCorrections() bool {
	settings := config.Get()
	return settings.GetApplyJ2KCorrections()
}

// ==================== VR Validation Integration ====================
// The following methods integrate with the valuerep module for DICOM VR validation.

// ValidateVR validates the data element's value against its VR constraints.
//
// This method performs comprehensive validation including:
//   - Type validation (string vs binary vs numeric)
//   - Length validation (max length constraints)
//   - Format validation (regex patterns for structured VRs)
//
// Returns an error if the value doesn't comply with the VR requirements.
//
// Example:
//
//	elem := NewDataElement(0x00100010, PN, "VeryLongNameThatExceedsMaximumLength")
//	if err := elem.ValidateVR(); err != nil {
//	    log.Printf("Validation failed: %v", err)
//	}
func (de *DataElement) ValidateVR() error {
	de.mu.RLock()
	defer de.mu.RUnlock()

	return valuerep.ValidateValue(string(de.VR), de.Value)
}

// SetValueWithValidation sets the value of the data element with VR validation.
//
// This method validates the value against the VR constraints before setting it.
// If validation fails, the value is not changed and an error is returned.
//
// Use this method when you want to ensure type safety and DICOM compliance.
// For performance-critical code where values are already validated, use SetValue() instead.
//
// Example:
//
//	elem := NewDataElement(0x00100020, LO, "")
//	if err := elem.SetValueWithValidation("PatientID123"); err != nil {
//	    log.Fatal(err)
//	}
func (de *DataElement) SetValueWithValidation(value interface{}) error {
	// Validate before acquiring write lock
	if err := valuerep.ValidateValue(string(de.VR), value); err != nil {
		return fmt.Errorf("value validation failed for VR %s: %w", de.VR, err)
	}

	de.mu.Lock()
	defer de.mu.Unlock()
	de.Value = value
	return nil
}

// IsValidVR checks if the VR code is a valid DICOM VR.
//
// Example:
//
//	elem := NewDataElement(0x00100010, "PN", "Smith^John")
//	if elem.IsValidVR() {
//	    fmt.Println("Valid VR")
//	}
func (de *DataElement) IsValidVR() bool {
	de.mu.RLock()
	defer de.mu.RUnlock()

	return valuerep.IsValidVR(string(de.VR))
}

// GetVRMetadata returns metadata for the data element's VR.
//
// Returns VR metadata including max length, padding character, and type flags.
//
// Example:
//
//	elem := NewDataElement(0x00100010, PN, "Smith^John")
//	metadata, err := elem.GetVRMetadata()
//	if err == nil {
//	    fmt.Printf("VR: %s, MaxLength: %d\n", metadata.Code, metadata.MaxLength)
//	}
func (de *DataElement) GetVRMetadata() (valuerep.VRMetadata, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	return valuerep.GetVRMetadata(string(de.VR))
}

// ParsePersonName parses a Person Name (PN) value into components.
//
// Returns nil if the VR is not PN or the value is not a string.
//
// Example:
//
//	elem := NewDataElement(0x00100010, PN, "Smith^John^M^Dr.^Jr.")
//	pn := elem.ParsePersonName()
//	if pn != nil {
//	    fmt.Printf("Last Name: %s, First Name: %s\n", pn.Alphabetic, pn.Ideographic)
//	}
func (de *DataElement) ParsePersonName() *valuerep.PersonName {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != PN {
		return nil
	}

	strValue, ok := de.Value.(string)
	if !ok {
		return nil
	}

	return valuerep.ParsePersonName(strValue)
}

// ParseDate parses a Date (DA) value into time.Time.
//
// Returns nil and error if the VR is not DA or parsing fails.
//
// Example:
//
//	elem := NewDataElement(0x00080020, DA, "20230615")
//	date, err := elem.ParseDate()
//	if err == nil {
//	    fmt.Printf("Date: %v\n", date.Value)
//	}
func (de *DataElement) ParseDate() (*valuerep.Date, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != DA {
		return nil, fmt.Errorf("VR is not DA (Date), got %s", de.VR)
	}

	strValue, ok := de.Value.(string)
	if !ok {
		return nil, fmt.Errorf("value is not a string")
	}

	return valuerep.ParseDate(strValue)
}

// ParseTime parses a Time (TM) value into time.Time.
//
// Returns nil and error if the VR is not TM or parsing fails.
//
// Example:
//
//	elem := NewDataElement(0x00080030, TM, "143000")
//	time, err := elem.ParseTime()
//	if err == nil {
//	    fmt.Printf("Time: %v\n", time.Value)
//	}
func (de *DataElement) ParseTime() (*valuerep.Time, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != TM {
		return nil, fmt.Errorf("VR is not TM (Time), got %s", de.VR)
	}

	strValue, ok := de.Value.(string)
	if !ok {
		return nil, fmt.Errorf("value is not a string")
	}

	return valuerep.ParseTime(strValue)
}

// ParseDecimalString parses a Decimal String (DS) value into float64.
//
// Returns nil and error if the VR is not DS or parsing fails.
//
// Example:
//
//	elem := NewDataElement(0x00181041, DS, "123.45")
//	ds, err := elem.ParseDecimalString()
//	if err == nil {
//	    fmt.Printf("Value: %.2f\n", ds.Value)
//	}
func (de *DataElement) ParseDecimalString() (*valuerep.DecimalString, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != DS {
		return nil, fmt.Errorf("VR is not DS (Decimal String), got %s", de.VR)
	}

	strValue, ok := de.Value.(string)
	if !ok {
		return nil, fmt.Errorf("value is not a string")
	}

	return valuerep.ParseDecimalString(strValue)
}

// ParseIntegerString parses an Integer String (IS) value into int64.
//
// Returns nil and error if the VR is not IS or parsing fails.
//
// Example:
//
//	elem := NewDataElement(0x00200013, IS, "42")
//	is, err := elem.ParseIntegerString()
//	if err == nil {
//	    fmt.Printf("Value: %d\n", is.Value)
//	}
func (de *DataElement) ParseIntegerString() (*valuerep.IntegerString, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != IS {
		return nil, fmt.Errorf("VR is not IS (Integer String), got %s", de.VR)
	}

	strValue, ok := de.Value.(string)
	if !ok {
		return nil, fmt.Errorf("value is not a string")
	}

	return valuerep.ParseIntegerString(strValue)
}

// ValidateUID validates a Unique Identifier (UI) value format.
//
// Returns error if the VR is not UI or the UID format is invalid.
//
// Example:
//
//	elem := NewDataElement(0x0020000D, UI, "1.2.840.10008.1.2.1")
//	if err := elem.ValidateUID(); err != nil {
//	    log.Printf("Invalid UID: %v", err)
//	}
func (de *DataElement) ValidateUID() error {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != UI {
		return fmt.Errorf("VR is not UI (Unique Identifier), got %s", de.VR)
	}

	strValue, ok := de.Value.(string)
	if !ok {
		return fmt.Errorf("value is not a string")
	}

	return valuerep.ValidateUID(strValue)
}
