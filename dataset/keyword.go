package dataset

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// GetByKeyword retrieves a data element by DICOM keyword.
// Keywords are programmatic identifiers like "PatientName", "StudyDate", etc.
// Returns (nil, false) if the keyword is not found in the dictionary or not present in the dataset.
func (ds *Dataset) GetByKeyword(keyword string) (*dataelem.DataElement, bool) {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		// Keyword not found in dictionary
		return nil, false
	}
	return ds.Get(t)
}

// GetValueByKeyword retrieves the raw value bytes for a DICOM keyword.
// Returns nil if the keyword is not found or not present in the dataset.
func (ds *Dataset) GetValueByKeyword(keyword string) []byte {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		return nil
	}
	return ds.GetValue(t)
}

// SetValueByKeyword sets the value for a DICOM keyword.
// Creates a new element if it doesn't exist.
// Returns an error if the keyword is not found in the dictionary.
//
// Example:
//
//	err := ds.SetValueByKeyword("PatientName", []byte("John Doe"))
func (ds *Dataset) SetValueByKeyword(keyword string, value []byte) error {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		return fmt.Errorf("keyword %q not found in DICOM dictionary", keyword)
	}
	return ds.SetValue(t, value)
}

// ContainsByKeyword checks if an element with the given keyword exists in the dataset.
//
// Example:
//
//	if ds.ContainsByKeyword("PatientName") {
//	    // Patient name is present
//	}
func (ds *Dataset) ContainsByKeyword(keyword string) bool {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		return false
	}
	return ds.Contains(t)
}

// RemoveByKeyword removes an element by DICOM keyword.
// Returns false if the keyword is not found in the dictionary or not present in the dataset.
//
// Example:
//
//	removed := ds.RemoveByKeyword("PatientName")
func (ds *Dataset) RemoveByKeyword(keyword string) bool {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		return false
	}
	return ds.Remove(t)
}

// UpdateElementByKeyword updates only the value of an existing element by keyword.
// Returns an error if the keyword is not found or the element doesn't exist.
//
// Example:
//
//	err := ds.UpdateElementByKeyword("PatientName", []byte("Jane Doe"))
func (ds *Dataset) UpdateElementByKeyword(keyword string, value []byte) error {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		return fmt.Errorf("keyword %q not found in DICOM dictionary", keyword)
	}
	return ds.UpdateElement(t, value)
}

// AddByKeyword adds a data element using a keyword, VR, and value.
// This is a convenience method that looks up the tag from the keyword.
// Returns an error if the keyword is not found in the dictionary.
//
// Example:
//
//	err := ds.AddByKeyword("PatientName", dataelem.PN, []byte("John Doe"))
func (ds *Dataset) AddByKeyword(keyword string, vr dataelem.VR, value interface{}) error {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		return fmt.Errorf("keyword %q not found in DICOM dictionary", keyword)
	}

	elem := dataelem.NewDataElement(t, vr, value)
	return ds.Add(elem)
}

// GetKeywordInfo returns information about a keyword from the DICOM dictionary.
// Returns nil if the keyword is not found.
//
// Example:
//
//	info := ds.GetKeywordInfo("PatientName")
//	if info != nil {
//	    fmt.Printf("VR: %s, VM: %s, Name: %s\n", info.VR, info.VM, info.Name)
//	}
func (ds *Dataset) GetKeywordInfo(keyword string) *tag.TagInfo {
	t := tag.GlobalDictionary().GetByKeyword(keyword)
	if t == 0 {
		return nil
	}
	return tag.GlobalDictionary().Get(t)
}

// GetElementsByKeywords retrieves multiple elements by their keywords.
// Skips keywords that are not found in the dictionary or not present in the dataset.
//
// Example:
//
//	elems := ds.GetElementsByKeywords("PatientName", "PatientID", "StudyDate")
func (ds *Dataset) GetElementsByKeywords(keywords ...string) []*dataelem.DataElement {
	var tags []tag.Tag
	for _, keyword := range keywords {
		t := tag.GlobalDictionary().GetByKeyword(keyword)
		if t != 0 {
			tags = append(tags, t)
		}
	}
	return ds.GetElements(tags...)
}

// RemoveElementsByKeywords removes multiple elements by their keywords.
// Returns the number of elements actually removed.
//
// Example:
//
//	count := ds.RemoveElementsByKeywords("PatientName", "PatientID", "StudyDate")
func (ds *Dataset) RemoveElementsByKeywords(keywords ...string) int {
	var tags []tag.Tag
	for _, keyword := range keywords {
		t := tag.GlobalDictionary().GetByKeyword(keyword)
		if t != 0 {
			tags = append(tags, t)
		}
	}
	return ds.RemoveElements(tags...)
}

// GetStringByKeyword retrieves a string value by keyword.
// Trims trailing null bytes and whitespace.
// Returns empty string if keyword not found or not present.
//
// Example:
//
//	patientName := ds.GetStringByKeyword("PatientName")
func (ds *Dataset) GetStringByKeyword(keyword string) string {
	value := ds.GetValueByKeyword(keyword)
	if value == nil {
		return ""
	}
	// Trim null bytes and whitespace
	return trimNullAndSpace(value)
}

// SetStringByKeyword sets a string value by keyword.
// Converts the string to bytes and calls SetValueByKeyword.
//
// Example:
//
//	err := ds.SetStringByKeyword("PatientName", "John Doe")
func (ds *Dataset) SetStringByKeyword(keyword string, value string) error {
	return ds.SetValueByKeyword(keyword, []byte(value))
}

// HasKeyword is an alias for ContainsByKeyword for convenience.
func (ds *Dataset) HasKeyword(keyword string) bool {
	return ds.ContainsByKeyword(keyword)
}

// KeywordToTag converts a DICOM keyword to a tag.
// Returns Tag(0) if the keyword is not found.
//
// Example:
//
//	t := ds.KeywordToTag("PatientName") // Returns tag.Tag(0x00100010)
func (ds *Dataset) KeywordToTag(keyword string) tag.Tag {
	return tag.GlobalDictionary().GetByKeyword(keyword)
}

// TagToKeyword converts a tag to its DICOM keyword.
// Returns empty string if the tag is not in the dictionary.
//
// Example:
//
//	keyword := ds.TagToKeyword(tag.New(0x0010, 0x0010)) // Returns "PatientName"
func (ds *Dataset) TagToKeyword(t tag.Tag) string {
	return tag.GlobalDictionary().GetKeyword(t)
}

// GetByKeywordWithDefault retrieves a value by keyword with a default value.
// Returns the default if the keyword is not found or not present.
//
// Example:
//
//	name := ds.GetByKeywordWithDefault("PatientName", []byte("Unknown"))
func (ds *Dataset) GetByKeywordWithDefault(keyword string, defaultValue []byte) []byte {
	value := ds.GetValueByKeyword(keyword)
	if value == nil {
		return defaultValue
	}
	return value
}

// GetStringByKeywordWithDefault retrieves a string value by keyword with a default.
//
// Example:
//
//	name := ds.GetStringByKeywordWithDefault("PatientName", "Unknown")
func (ds *Dataset) GetStringByKeywordWithDefault(keyword string, defaultValue string) string {
	str := ds.GetStringByKeyword(keyword)
	if str == "" {
		return defaultValue
	}
	return str
}

// Helper function to trim null bytes and whitespace
func trimNullAndSpace(b []byte) string {
	// Trim trailing nulls and spaces together
	for len(b) > 0 && (b[len(b)-1] == 0 || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return string(b)
}
