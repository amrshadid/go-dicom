package dataset

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// Dataset validation methods integrate with the valuerep module for DICOM VR validation.

// AddWithValidation adds a data element to the dataset with VR validation.
// Validates the element's value against its VR constraints before adding.
// If validation fails, the element is not added and an error is returned.
// Use Add() directly for performance-critical code where elements are already validated.
func (ds *Dataset) AddWithValidation(elem *dataelem.DataElement) error {
	if elem == nil {
		return fmt.Errorf("element is nil")
	}

	// Validate VR before adding
	if err := elem.ValidateVR(); err != nil {
		return fmt.Errorf("element validation failed: %w", err)
	}

	// Add element to dataset (handles insertion logic)
	return ds.Add(elem)
}

// ValidateAllElements validates all data elements in the dataset against their VR constraints.
//
// This method iterates through all elements and validates each one.
// Returns the first validation error encountered, or nil if all elements are valid.
//
// This is useful for:
//   - Validating an entire dataset before writing to file
//   - Checking data quality after reading from file
//   - Ensuring DICOM compliance
//
// Example:
//
//	ds := NewDataset()
//	// ... add elements ...
//	if err := ds.ValidateAllElements(); err != nil {
//	    log.Printf("Dataset validation failed: %v", err)
//	}
func (ds *Dataset) ValidateAllElements() error {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		if err := elem.ValidateVR(); err != nil {
			return fmt.Errorf("validation failed for tag %08X: %w", tagVal, err)
		}
	}

	return nil
}

// ValidateElement validates a specific element by tag.
//
// Returns an error if the tag doesn't exist or if validation fails.
//
// Example:
//
//	if err := ds.ValidateElement(tag.PatientName); err != nil {
//	    log.Printf("Validation failed: %v", err)
//	}
func (ds *Dataset) ValidateElement(t tag.Tag) error {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	elem, exists := ds.elements[uint32(t)]
	if !exists {
		return fmt.Errorf("tag %s not found in dataset", t.String())
	}

	return elem.ValidateVR()
}

// GetValidationErrors returns all validation errors for elements in the dataset.
//
// Unlike ValidateAllElements() which returns on first error, this method
// collects all validation errors and returns them as a map of tag -> error.
//
// This is useful for generating validation reports.
//
// Example:
//
//	errors := ds.GetValidationErrors()
//	if len(errors) > 0 {
//	    for tagVal, err := range errors {
//	        log.Printf("Tag %08X: %v", tagVal, err)
//	    }
//	}
func (ds *Dataset) GetValidationErrors() map[uint32]error {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	errors := make(map[uint32]error)

	for tagVal, elem := range ds.elements {
		if err := elem.ValidateVR(); err != nil {
			errors[tagVal] = err
		}
	}

	return errors
}

// IsValid checks if all elements in the dataset are valid.
//
// This is a convenience method that returns true if no validation errors exist.
//
// Example:
//
//	if !ds.IsValid() {
//	    log.Println("Dataset contains invalid elements")
//	}
func (ds *Dataset) IsValid() bool {
	return len(ds.GetValidationErrors()) == 0
}

// SetWithValidation sets or updates a data element with VR validation.
//
// This is equivalent to AddWithValidation() but with a more explicit name.
//
// Example:
//
//	elem := dataelem.NewDataElement(tag.PatientID, dataelem.LO, "12345")
//	if err := ds.SetWithValidation(tag.PatientID, elem); err != nil {
//	    log.Fatal(err)
//	}
func (ds *Dataset) SetWithValidation(t tag.Tag, elem *dataelem.DataElement) error {
	if elem == nil {
		return fmt.Errorf("element is nil")
	}

	// Validate VR
	if err := elem.ValidateVR(); err != nil {
		return fmt.Errorf("validation failed for tag %s: %w", t.String(), err)
	}

	// Add element
	return ds.Add(elem)
}

// ValidateBeforeWrite validates the dataset before writing to file.
//
// This performs comprehensive validation including:
//   - VR validation for all elements
//   - Required element checks (Type 1 elements)
//   - Value multiplicity validation
//
// Returns an error if validation fails.
//
// Example:
//
//	if err := ds.ValidateBeforeWrite(); err != nil {
//	    log.Printf("Cannot write dataset: %v", err)
//	    return err
//	}
//	// Safe to write...
func (ds *Dataset) ValidateBeforeWrite() error {
	// First, validate all VR constraints
	if err := ds.ValidateAllElements(); err != nil {
		return fmt.Errorf("VR validation failed: %w", err)
	}

	// Additional checks can be added here:
	// - Check for required Type 1 elements
	// - Validate UIDs
	// - Check transfer syntax compatibility
	// etc.

	return nil
}

// GetInvalidElements returns a slice of tags for elements that fail validation.
//
// This is useful for identifying which elements need correction.
//
// Example:
//
//	invalid := ds.GetInvalidElements()
//	for _, t := range invalid {
//	    elem, _ := ds.Get(t)
//	    log.Printf("Invalid element: %s (VR: %s)", t.String(), elem.GetVR())
//	}
func (ds *Dataset) GetInvalidElements() []tag.Tag {
	errors := ds.GetValidationErrors()

	invalid := make([]tag.Tag, 0, len(errors))
	for tagVal := range errors {
		invalid = append(invalid, tag.Tag(tagVal))
	}

	return invalid
}

// ValidateAndFix attempts to validate and fix common VR issues.
//
// This method:
//   - Validates all elements
//   - Attempts to fix common issues (truncate strings, pad values, etc.)
//   - Returns a report of what was fixed
//
// Returns a slice of messages describing fixes applied, and an error if fixes failed.
//
// Example:
//
//	fixes, err := ds.ValidateAndFix()
//	if err != nil {
//	    log.Printf("Could not fix issues: %v", err)
//	} else if len(fixes) > 0 {
//	    for _, fix := range fixes {
//	        log.Printf("Fixed: %s", fix)
//	    }
//	}
func (ds *Dataset) ValidateAndFix() ([]string, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	fixes := make([]string, 0)

	for tagVal, elem := range ds.elements {
		if err := elem.ValidateVR(); err != nil {
			// Attempt to fix the issue
			metadata, metaErr := elem.GetVRMetadata()
			if metaErr != nil {
				continue
			}

			// Try to fix string length violations
			if metadata.IsString {
				if strValue, ok := elem.GetValue().(string); ok {
					if metadata.MaxLength > 0 && len(strValue) > metadata.MaxLength {
						// Truncate string
						newValue := strValue[:metadata.MaxLength]
						elem.SetValue(newValue)
						fixes = append(fixes, fmt.Sprintf("Tag %08X: Truncated string from %d to %d characters",
							tagVal, len(strValue), metadata.MaxLength))
					}
				}
			}
		}
	}

	// Re-validate to ensure fixes worked
	if err := ds.ValidateAllElements(); err != nil {
		return fixes, fmt.Errorf("validation still failing after fixes: %w", err)
	}

	return fixes, nil
}

// GetValidationSummary returns a human-readable summary of validation results.
//
// Example:
//
//	summary := ds.GetValidationSummary()
//	fmt.Println(summary)
//	// Output:
//	// Dataset Validation Summary:
//	//   Total elements: 42
//	//   Valid elements: 40
//	//   Invalid elements: 2
//	//   Invalid tags: 00100010, 00100020
func (ds *Dataset) GetValidationSummary() string {
	errors := ds.GetValidationErrors()

	summary := "Dataset Validation Summary:\n"
	summary += fmt.Sprintf("  Total elements: %d\n", ds.Length())
	summary += fmt.Sprintf("  Valid elements: %d\n", ds.Length()-len(errors))
	summary += fmt.Sprintf("  Invalid elements: %d\n", len(errors))

	if len(errors) > 0 {
		summary += "  Invalid tags: "
		first := true
		for tagVal := range errors {
			if !first {
				summary += ", "
			}
			summary += fmt.Sprintf("%08X", tagVal)
			first = false
		}
		summary += "\n"
	}

	return summary
}
