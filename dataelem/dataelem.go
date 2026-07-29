package dataelem

import (
	"fmt"
	"sync"

	"github.com/amrshadid/go-dicom/tag"
)

// VR represents a DICOM Value Representation.
type VR string

// Common DICOM Value Representations (28 total in DICOM standard).
const (
	AE VR = "AE" // Application Entity
	AS VR = "AS" // Age String
	AT VR = "AT" // Attribute Tag
	CS VR = "CS" // Code String
	DA VR = "DA" // Date
	DS VR = "DS" // Decimal String
	DT VR = "DT" // Date/Time
	FD VR = "FD" // Floating Point Double
	FL VR = "FL" // Floating Point Single
	IS VR = "IS" // Integer String
	LO VR = "LO" // Long String
	LT VR = "LT" // Long Text
	OB VR = "OB" // Other Byte
	OD VR = "OD" // Other Double
	OF VR = "OF" // Other Float
	OL VR = "OL" // Other Long
	OW VR = "OW" // Other Word
	PN VR = "PN" // Person Name
	SH VR = "SH" // Short String
	SL VR = "SL" // Signed Long
	SQ VR = "SQ" // Sequence of Items
	SS VR = "SS" // Signed Short
	ST VR = "ST" // Short Text
	TM VR = "TM" // Time
	UC VR = "UC" // Unlimited Characters
	UI VR = "UI" // Unique Identifier
	UL VR = "UL" // Unsigned Long
	UN VR = "UN" // Unknown
	UR VR = "UR" // Universal Resource Identifier
	US VR = "US" // Unsigned Short
	UT VR = "UT" // Unlimited Text
)

// DataElement represents a DICOM data element.
// A data element consists of a tag, VR, and value(s).
type DataElement struct {
	tag         interface{} // Tag (can be string representation)
	VR          VR
	Value       interface{} // The value(s)
	Keyword     string      // DICOM keyword
	VM          int         // Value Multiplicity (number of values)
	Description string      // Human-readable description
	mu          sync.RWMutex
}

// NewDataElement creates a new DataElement.
func NewDataElement(tag interface{}, vr VR, value interface{}) *DataElement {
	return &DataElement{
		tag:   tag,
		VR:    vr,
		Value: value,
		VM:    1,
	}
}

// NewDataElementWithKeyword creates a new DataElement with keyword and description.
func NewDataElementWithKeyword(tag interface{}, vr VR, value interface{}, keyword, description string) *DataElement {
	return &DataElement{
		tag:         tag,
		VR:          vr,
		Value:       value,
		Keyword:     keyword,
		VM:          1,
		Description: description,
	}
}

// GetTag returns the tag exactly as it was stored, without interpretation.
//
// Prefer Tag for almost every purpose: it returns tag.Tag and reports whether
// the stored value could be read as one. Callers of this have to assert the
// concrete type themselves, and the assertions in this module all discarded the
// element on failure — so an element whose tag arrived in an unexpected form was
// silently dropped from encoding, from written output, and from anything else
// walking a data set.
//
// This remains for the cases that need the original value rather than a tag:
// copying an element without normalizing what it holds, and reporting the
// concrete type in an error.
func (de *DataElement) GetTag() interface{} {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return de.tag
}

// Tag returns the element's tag.
//
// The second result reports whether the stored value could be understood as a
// tag. It is false only for an element built with something that is not a tag
// at all, which the interface{} parameter on NewDataElement permits; checking it
// is the difference between reporting such an element and quietly skipping it.
//
// A string is accepted because tags have been passed that way, in the
// "(0008,0060)" and "00080060" forms the dictionary uses.
func (de *DataElement) Tag() (tag.Tag, bool) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	switch v := de.tag.(type) {
	case tag.Tag:
		return v, true
	case uint32:
		return tag.Tag(v), true
	case string:
		parsed, err := tag.ParseTag(v)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// MustTag returns the element's tag, or zero if it is not a tag.
//
// For callers that have already established the element is well-formed, or for
// which a zero tag is an acceptable sentinel. Prefer Tag where the distinction
// matters.
func (de *DataElement) MustTag() tag.Tag {
	t, _ := de.Tag()
	return t
}

// SetTag sets the tag of the data element.
func (de *DataElement) SetTag(tag interface{}) {
	de.mu.Lock()
	defer de.mu.Unlock()
	de.tag = tag
}

// GetVR returns the VR of the data element.
func (de *DataElement) GetVR() VR {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return de.VR
}

// SetVR sets the VR of the data element.
func (de *DataElement) SetVR(vr VR) {
	de.mu.Lock()
	defer de.mu.Unlock()
	de.VR = vr
}

// GetValue returns the value of the data element.
func (de *DataElement) GetValue() interface{} {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return de.Value
}

// SetValue sets the value of the data element.
func (de *DataElement) SetValue(value interface{}) {
	de.mu.Lock()
	defer de.mu.Unlock()
	de.Value = value
}

// GetKeyword returns the keyword of the data element.
func (de *DataElement) GetKeyword() string {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return de.Keyword
}

// SetKeyword sets the keyword of the data element.
func (de *DataElement) SetKeyword(keyword string) {
	de.mu.Lock()
	defer de.mu.Unlock()
	de.Keyword = keyword
}

// GetVM returns the value multiplicity of the data element.
func (de *DataElement) GetVM() int {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return de.VM
}

// SetVM sets the value multiplicity of the data element.
func (de *DataElement) SetVM(vm int) {
	de.mu.Lock()
	defer de.mu.Unlock()
	de.VM = vm
}

// GetDescription returns the description of the data element.
func (de *DataElement) GetDescription() string {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return de.Description
}

// SetDescription sets the description of the data element.
func (de *DataElement) SetDescription(description string) {
	de.mu.Lock()
	defer de.mu.Unlock()
	de.Description = description
}

// String returns a string representation of the data element.
func (de *DataElement) String() string {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.Keyword != "" {
		return fmt.Sprintf("DataElement{Tag: %v, VR: %s, Keyword: %s, VM: %d}", de.tag, de.VR, de.Keyword, de.VM)
	}
	return fmt.Sprintf("DataElement{Tag: %v, VR: %s, VM: %d}", de.tag, de.VR, de.VM)
}

// IsMultiValue returns true if the data element has multiple values (VM > 1).
func (de *DataElement) IsMultiValue() bool {
	de.mu.RLock()
	defer de.mu.RUnlock()
	return de.VM > 1
}

// IsEmpty returns true if the value is nil or empty.
func (de *DataElement) IsEmpty() bool {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.Value == nil {
		return true
	}

	// Check for empty strings
	if str, ok := de.Value.(string); ok {
		return str == ""
	}

	return false
}

// GetValueLength returns the length of the value in bytes (approximate for most types).
func (de *DataElement) GetValueLength() int {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.Value == nil {
		return 0
	}

	switch v := de.Value.(type) {
	case string:
		return len(v)
	case []byte:
		return len(v)
	case int, int32, int64:
		return 4
	case float32, float64:
		return 8
	case bool:
		return 1
	default:
		return 0
	}
}

// Clone creates a shallow copy of the data element.
func (de *DataElement) Clone() *DataElement {
	de.mu.RLock()
	defer de.mu.RUnlock()

	return &DataElement{
		tag:         de.tag,
		VR:          de.VR,
		Value:       de.Value,
		Keyword:     de.Keyword,
		VM:          de.VM,
		Description: de.Description,
	}
}

// Equals checks if two data elements are equal (by tag and value).
func (de *DataElement) Equals(other *DataElement) bool {
	if other == nil {
		return false
	}

	de.mu.RLock()
	defer de.mu.RUnlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	return de.tag == other.tag && de.Value == other.Value
}

// VRInfo contains metadata about a Value Representation.
type VRInfo struct {
	VR         VR
	Name       string
	ByteLength int  // -1 for variable length
	PadValue   byte // Padding character
	IsText     bool // Whether it's text-based
	IsNumeric  bool // Whether it represents numeric values
	SupportsVM bool // Whether it supports value multiplicity
	IsStandard bool // Whether it's a standard VR
}

// GetVRInfo returns metadata about a Value Representation.
func GetVRInfo(vr VR) *VRInfo {
	vrInfoMap := map[VR]*VRInfo{
		AE: {VR: AE, Name: "Application Entity", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		AS: {VR: AS, Name: "Age String", ByteLength: 4, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		AT: {VR: AT, Name: "Attribute Tag", ByteLength: 4, PadValue: 0, IsText: false, IsNumeric: false, SupportsVM: false, IsStandard: true},
		CS: {VR: CS, Name: "Code String", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: true, IsStandard: true},
		DA: {VR: DA, Name: "Date", ByteLength: 8, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: true, IsStandard: true},
		DS: {VR: DS, Name: "Decimal String", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: true, SupportsVM: true, IsStandard: true},
		DT: {VR: DT, Name: "Date/Time", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: true, IsStandard: true},
		FD: {VR: FD, Name: "Floating Point Double", ByteLength: 8, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: true, IsStandard: true},
		FL: {VR: FL, Name: "Floating Point Single", ByteLength: 4, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: true, IsStandard: true},
		IS: {VR: IS, Name: "Integer String", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: true, SupportsVM: true, IsStandard: true},
		LO: {VR: LO, Name: "Long String", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		LT: {VR: LT, Name: "Long Text", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		OB: {VR: OB, Name: "Other Byte", ByteLength: -1, PadValue: 0, IsText: false, IsNumeric: false, SupportsVM: false, IsStandard: true},
		OD: {VR: OD, Name: "Other Double", ByteLength: -1, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: false, IsStandard: true},
		OF: {VR: OF, Name: "Other Float", ByteLength: -1, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: false, IsStandard: true},
		OL: {VR: OL, Name: "Other Long", ByteLength: -1, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: false, IsStandard: true},
		OW: {VR: OW, Name: "Other Word", ByteLength: -1, PadValue: 0, IsText: false, IsNumeric: false, SupportsVM: false, IsStandard: true},
		PN: {VR: PN, Name: "Person Name", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: true, IsStandard: true},
		SH: {VR: SH, Name: "Short String", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		SL: {VR: SL, Name: "Signed Long", ByteLength: 4, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: true, IsStandard: true},
		SQ: {VR: SQ, Name: "Sequence of Items", ByteLength: -1, PadValue: 0, IsText: false, IsNumeric: false, SupportsVM: false, IsStandard: true},
		SS: {VR: SS, Name: "Signed Short", ByteLength: 2, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: true, IsStandard: true},
		ST: {VR: ST, Name: "Short Text", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		TM: {VR: TM, Name: "Time", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: true, IsStandard: true},
		UC: {VR: UC, Name: "Unlimited Characters", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		UI: {VR: UI, Name: "Unique Identifier", ByteLength: -1, PadValue: '\x00', IsText: true, IsNumeric: false, SupportsVM: true, IsStandard: true},
		UL: {VR: UL, Name: "Unsigned Long", ByteLength: 4, PadValue: 0, IsText: false, IsNumeric: true, SupportsVM: true, IsStandard: true},
		UN: {VR: UN, Name: "Unknown", ByteLength: -1, PadValue: 0, IsText: false, IsNumeric: false, SupportsVM: false, IsStandard: true},
		UR: {VR: UR, Name: "Universal Resource Identifier", ByteLength: -1, PadValue: '\x00', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
		UT: {VR: UT, Name: "Unlimited Text", ByteLength: -1, PadValue: ' ', IsText: true, IsNumeric: false, SupportsVM: false, IsStandard: true},
	}

	if info, ok := vrInfoMap[vr]; ok {
		return info
	}
	return nil
}

// IsValidVR checks if a VR string is valid.
func IsValidVR(vr VR) bool {
	return GetVRInfo(vr) != nil
}

// IsTextVR checks if a VR is text-based.
func IsTextVR(vr VR) bool {
	info := GetVRInfo(vr)
	return info != nil && info.IsText
}

// IsNumericVR checks if a VR represents numeric values.
func IsNumericVR(vr VR) bool {
	info := GetVRInfo(vr)
	return info != nil && info.IsNumeric
}

// AllVRs returns all standard DICOM Value Representations.
func AllVRs() []VR {
	return []VR{
		AE, AS, AT, CS, DA, DS, DT, FD, FL, IS,
		LO, LT, OB, OD, OF, OL, OW, PN, SH, SL,
		SQ, SS, ST, TM, UC, UI, UL, UN, UR, UT,
	}
}

// Group represents a DICOM group (upper 16 bits of tag).
type Group uint16

// Element represents a DICOM element (lower 16 bits of tag).
type Element uint16

// SequenceItem represents an item in a sequence data element.
type SequenceItem struct {
	DataElements []*DataElement
	mu           sync.RWMutex
}

// NewSequenceItem creates a new sequence item.
func NewSequenceItem() *SequenceItem {
	return &SequenceItem{
		DataElements: make([]*DataElement, 0),
	}
}

// AddDataElement adds a data element to the sequence item.
func (si *SequenceItem) AddDataElement(de *DataElement) error {
	if de == nil {
		return fmt.Errorf("cannot add nil data element")
	}

	si.mu.Lock()
	defer si.mu.Unlock()

	si.DataElements = append(si.DataElements, de)
	return nil
}

// GetDataElements returns all data elements in the sequence item.
func (si *SequenceItem) GetDataElements() []*DataElement {
	si.mu.RLock()
	defer si.mu.RUnlock()

	result := make([]*DataElement, len(si.DataElements))
	copy(result, si.DataElements)
	return result
}

// Count returns the number of data elements in the sequence item.
func (si *SequenceItem) Count() int {
	si.mu.RLock()
	defer si.mu.RUnlock()

	return len(si.DataElements)
}

// ValidateAgainstDictionary validates a data element against the DICOM dictionary.
// Returns nil if valid, or an error describing the validation issue.
// Note: Validation is informational; some issues may not be fatal.
func (de *DataElement) ValidateAgainstDictionary() error {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Get the tag
	var t tag.Tag
	if tagAsTag, ok := de.tag.(tag.Tag); ok {
		t = tagAsTag
	} else {
		// Can't validate if tag is not a tag.Tag
		return nil
	}

	// Get dictionary info
	dict := tag.GlobalDictionary()
	info := dict.Get(t)

	// Check if tag exists
	if info == nil {
		// Private tags are allowed without dictionary entry
		if t.IsPrivate() {
			return nil
		}
		return fmt.Errorf("unknown standard tag: %s", t.String())
	}

	// Warn about retired tags
	if info.Retired {
		return fmt.Errorf("tag %s (%s) is retired", t.String(), info.Name)
	}

	// Validate VR if present
	if de.VR != "" && info.VR != "" {
		if string(de.VR) != info.VR && !isValidVRVariant(string(de.VR), info.VR) {
			return fmt.Errorf("VR mismatch for %s: expected %s, got %s",
				t.String(), info.VR, de.VR)
		}
	}

	return nil
}

// GetDictionaryInfo returns the DICOM dictionary metadata for this element's tag.
// Returns nil if the tag is not in the dictionary.
func (de *DataElement) GetDictionaryInfo() *tag.TagInfo {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Get the tag
	var t tag.Tag
	if tagAsTag, ok := de.tag.(tag.Tag); ok {
		t = tagAsTag
	} else {
		return nil
	}

	// Get dictionary info
	dict := tag.GlobalDictionary()
	return dict.Get(t)
}

// GetTagName returns the human-readable tag name from the dictionary.
func (de *DataElement) GetTagName() string {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Get the tag
	var t tag.Tag
	if tagAsTag, ok := de.tag.(tag.Tag); ok {
		t = tagAsTag
	} else {
		return ""
	}

	return t.GetName()
}

// GetVRFromDictionary returns the expected VR for this tag from the dictionary.
func (de *DataElement) GetVRFromDictionary() string {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Get the tag
	var t tag.Tag
	if tagAsTag, ok := de.tag.(tag.Tag); ok {
		t = tagAsTag
	} else {
		return ""
	}

	return t.GetVR()
}

// IsRetiredTag returns whether this element's tag is marked as retired in the dictionary.
func (de *DataElement) IsRetiredTag() bool {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Get the tag
	var t tag.Tag
	if tagAsTag, ok := de.tag.(tag.Tag); ok {
		t = tagAsTag
	} else {
		return false
	}

	return t.IsRetired()
}

// isValidVRVariant checks if a VR is a valid variant of the expected VR.
// Handles cases like "OB or OW" where either is acceptable.
func isValidVRVariant(actual, expected string) bool {
	if expected == "OB or OW" {
		return actual == "OB" || actual == "OW"
	}
	if expected == "OB or OD" {
		return actual == "OB" || actual == "OD"
	}
	if expected == "US or SS" {
		return actual == "US" || actual == "SS"
	}
	if expected == "OW or OB" {
		return actual == "OW" || actual == "OB"
	}
	return false
}
