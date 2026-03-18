package dataelem

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/tag"
)

// DICOM JSON Model (Part 18, Annex F) Serialization
//
// This file implements JSON serialization/deserialization according to
// the DICOM JSON Model specification. Each data element is represented as:
//
// {
//   "GGGGEEEE": {
//     "vr": "VR",
//     "Value": [...]           // For most VRs
//     // OR
//     "InlineBinary": "base64" // For binary VRs (OB, OD, OF, OL, OW, UN)
//     // OR
//     "BulkDataURI": "uri"     // For bulk data references
//   }
// }

// DicomJSONElement represents a DICOM data element in JSON format.
type DicomJSONElement struct {
	VR           string        `json:"vr"`
	Value        []interface{} `json:"Value,omitempty"`
	InlineBinary string        `json:"InlineBinary,omitempty"`
	BulkDataURI  string        `json:"BulkDataURI,omitempty"`
}

// MarshalJSON implements json.Marshaler for DataElement.
// Converts the data element to DICOM JSON Model format.
func (de *DataElement) MarshalJSON() ([]byte, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Create JSON element
	jsonElem := DicomJSONElement{
		VR: string(de.VR),
	}

	// Handle empty values
	if de.Value == nil {
		return json.Marshal(map[string]interface{}{
			de.getTagString(): jsonElem,
		})
	}

	// Convert value based on VR
	switch de.VR {
	// Binary VRs - use InlineBinary (base64)
	case OB, OD, OF, OL, OW, UN:
		if bytes, ok := de.Value.([]byte); ok {
			jsonElem.InlineBinary = base64.StdEncoding.EncodeToString(bytes)
		}

	// Sequence - special handling
	case SQ:
		if items, ok := de.Value.([]SequenceItem); ok {
			jsonElem.Value = make([]interface{}, len(items))
			for i := range items {
				// Each sequence item is a map of data elements
				itemMap := make(map[string]interface{})
				for _, elem := range items[i].DataElements {
					elemJSON, err := elem.MarshalJSON()
					if err != nil {
						return nil, fmt.Errorf("failed to marshal sequence item element: %w", err)
					}
					// Extract the element from the wrapper map
					var elemData map[string]interface{}
					if err := json.Unmarshal(elemJSON, &elemData); err != nil {
						return nil, err
					}
					// Merge into item map
					for k, v := range elemData {
						itemMap[k] = v
					}
				}
				jsonElem.Value[i] = itemMap
			}
		}

	// Person Name - convert to JSON PN format
	case PN:
		jsonElem.Value = de.convertPNToJSON()

	// Date - convert to DICOM DA format
	case DA:
		jsonElem.Value = de.convertDAToJSON()

	// DateTime - convert to DICOM DT format
	case DT:
		jsonElem.Value = de.convertDTToJSON()

	// Time - convert to DICOM TM format
	case TM:
		jsonElem.Value = de.convertTMToJSON()

	// Attribute Tag - convert to JSON AT format
	case AT:
		jsonElem.Value = de.convertATToJSON()

	// Numeric VRs - convert to JSON number format
	case IS, DS, SS, US, SL, UL, FL, FD:
		jsonElem.Value = de.convertNumericToJSON()

	// Text VRs - convert to JSON string array
	default:
		jsonElem.Value = de.convertTextToJSON()
	}

	// Create final JSON object with tag as key
	result := map[string]interface{}{
		de.getTagString(): jsonElem,
	}

	return json.Marshal(result)
}

// UnmarshalJSON implements json.Unmarshaler for DataElement.
// Parses DICOM JSON Model format into a DataElement.
func (de *DataElement) UnmarshalJSON(data []byte) error {
	// Parse outer object (tag -> element)
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		return err
	}

	// Should have exactly one key (the tag)
	if len(outer) != 1 {
		return fmt.Errorf("expected exactly one tag in JSON, got %d", len(outer))
	}

	// Get the tag and element data
	var tagStr string
	var elemData json.RawMessage
	for k, v := range outer {
		tagStr = k
		elemData = v
	}

	// Parse tag
	parsedTag, err := parseTagString(tagStr)
	if err != nil {
		return fmt.Errorf("invalid tag string %s: %w", tagStr, err)
	}

	// Parse element
	var jsonElem DicomJSONElement
	if err := json.Unmarshal(elemData, &jsonElem); err != nil {
		return err
	}

	// Set basic fields
	de.tag = parsedTag
	de.VR = VR(jsonElem.VR)

	// Populate keyword and description from dictionary
	dict := tag.GlobalDictionary()
	if info := dict.Get(parsedTag); info != nil {
		de.Keyword = info.Keyword
		de.Description = info.Name
	}

	// Convert value based on VR
	if jsonElem.InlineBinary != "" {
		// Binary data
		bytes, err := base64.StdEncoding.DecodeString(jsonElem.InlineBinary)
		if err != nil {
			return fmt.Errorf("failed to decode inline binary: %w", err)
		}
		de.Value = bytes
		de.VM = 1
	} else if jsonElem.BulkDataURI != "" {
		// Bulk data URI (store as string for now)
		de.Value = jsonElem.BulkDataURI
		de.VM = 1
	} else if jsonElem.Value != nil {
		// Regular value
		value, vm, err := de.convertJSONToValue(jsonElem.Value)
		if err != nil {
			return err
		}
		de.Value = value
		de.VM = vm
	} else {
		// Empty value
		de.Value = nil
		de.VM = 0
	}

	return nil
}

// ToJSONDict converts the data element to a DICOM JSON dictionary.
func (de *DataElement) ToJSONDict() (map[string]interface{}, error) {
	jsonBytes, err := de.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// ToJSON converts the data element to a DICOM JSON string.
func (de *DataElement) ToJSON() (string, error) {
	jsonBytes, err := de.MarshalJSON()
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}

// FromJSON creates a DataElement from a DICOM JSON string.
func FromJSON(jsonStr string) (*DataElement, error) {
	de := &DataElement{}
	if err := json.Unmarshal([]byte(jsonStr), de); err != nil {
		return nil, err
	}
	return de, nil
}

// Helper functions for JSON conversion

func (de *DataElement) getTagString() string {
	if t, ok := de.tag.(tag.Tag); ok {
		return fmt.Sprintf("%08X", uint32(t))
	}
	return "00000000"
}

func parseTagString(s string) (tag.Tag, error) {
	// Remove any whitespace
	s = strings.TrimSpace(s)

	// Parse hex string (GGGGEEEE)
	if len(s) != 8 {
		return tag.Tag(0), fmt.Errorf("invalid tag string length: %d (expected 8)", len(s))
	}

	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return tag.Tag(0), fmt.Errorf("failed to parse tag hex: %w", err)
	}

	// Extract group and element
	group := uint16(val >> 16)
	elem := uint16(val & 0xFFFF)

	return tag.New(group, elem), nil
}

func (de *DataElement) convertPNToJSON() []interface{} {
	result := []interface{}{}

	switch v := de.Value.(type) {
	case PersonName:
		pnMap := map[string]string{
			"Alphabetic": v.Alphabetic,
		}
		if v.Ideographic != "" {
			pnMap["Ideographic"] = v.Ideographic
		}
		if v.Phonetic != "" {
			pnMap["Phonetic"] = v.Phonetic
		}
		result = append(result, pnMap)

	case []PersonName:
		for _, pn := range v {
			pnMap := map[string]string{
				"Alphabetic": pn.Alphabetic,
			}
			if pn.Ideographic != "" {
				pnMap["Ideographic"] = pn.Ideographic
			}
			if pn.Phonetic != "" {
				pnMap["Phonetic"] = pn.Phonetic
			}
			result = append(result, pnMap)
		}
	}

	return result
}

func (de *DataElement) convertDAToJSON() []interface{} {
	result := []interface{}{}

	switch v := de.Value.(type) {
	case time.Time:
		result = append(result, formatDA(v))
	case []time.Time:
		for _, t := range v {
			result = append(result, formatDA(t))
		}
	}

	return result
}

func (de *DataElement) convertDTToJSON() []interface{} {
	result := []interface{}{}

	switch v := de.Value.(type) {
	case time.Time:
		result = append(result, formatDT(v))
	case []time.Time:
		for _, t := range v {
			result = append(result, formatDT(t))
		}
	}

	return result
}

func (de *DataElement) convertTMToJSON() []interface{} {
	result := []interface{}{}

	switch v := de.Value.(type) {
	case time.Time:
		result = append(result, formatTM(v))
	case []time.Time:
		for _, t := range v {
			result = append(result, formatTM(t))
		}
	}

	return result
}

func (de *DataElement) convertATToJSON() []interface{} {
	result := []interface{}{}

	switch v := de.Value.(type) {
	case tag.Tag:
		result = append(result, fmt.Sprintf("%08X", uint32(v)))
	case []tag.Tag:
		for _, t := range v {
			result = append(result, fmt.Sprintf("%08X", uint32(t)))
		}
	}

	return result
}

func (de *DataElement) convertNumericToJSON() []interface{} {
	result := []interface{}{}

	switch v := de.Value.(type) {
	case int:
		result = append(result, v)
	case []int:
		for _, n := range v {
			result = append(result, n)
		}
	case int64:
		result = append(result, v)
	case []int64:
		for _, n := range v {
			result = append(result, n)
		}
	case float64:
		result = append(result, v)
	case []float64:
		for _, f := range v {
			result = append(result, f)
		}
	}

	return result
}

func (de *DataElement) convertTextToJSON() []interface{} {
	result := []interface{}{}

	switch v := de.Value.(type) {
	case string:
		result = append(result, v)
	case []string:
		for _, s := range v {
			result = append(result, s)
		}
	}

	return result
}

func (de *DataElement) convertJSONToValue(jsonValue []interface{}) (interface{}, int, error) {
	if len(jsonValue) == 0 {
		return nil, 0, nil
	}

	switch de.VR {
	// Person Name
	case PN:
		if len(jsonValue) == 1 {
			pn, err := parseJSONPN(jsonValue[0])
			if err != nil {
				return nil, 0, err
			}
			return pn, 1, nil
		}
		pns := make([]PersonName, len(jsonValue))
		for i, v := range jsonValue {
			pn, err := parseJSONPN(v)
			if err != nil {
				return nil, 0, err
			}
			pns[i] = pn
		}
		return pns, len(pns), nil

	// Attribute Tag
	case AT:
		if len(jsonValue) == 1 {
			t, err := parseJSONAT(jsonValue[0])
			if err != nil {
				return nil, 0, err
			}
			return t, 1, nil
		}
		tags := make([]tag.Tag, len(jsonValue))
		for i, v := range jsonValue {
			t, err := parseJSONAT(v)
			if err != nil {
				return nil, 0, err
			}
			tags[i] = t
		}
		return tags, len(tags), nil

	// Numeric types
	case IS, SS, US, SL:
		if len(jsonValue) == 1 {
			return int(jsonValue[0].(float64)), 1, nil
		}
		ints := make([]int, len(jsonValue))
		for i, v := range jsonValue {
			ints[i] = int(v.(float64))
		}
		return ints, len(ints), nil

	case UL:
		if len(jsonValue) == 1 {
			return int64(jsonValue[0].(float64)), 1, nil
		}
		ints := make([]int64, len(jsonValue))
		for i, v := range jsonValue {
			ints[i] = int64(v.(float64))
		}
		return ints, len(ints), nil

	case DS, FL, FD:
		if len(jsonValue) == 1 {
			return jsonValue[0].(float64), 1, nil
		}
		floats := make([]float64, len(jsonValue))
		for i, v := range jsonValue {
			floats[i] = v.(float64)
		}
		return floats, len(floats), nil

	// Text types
	default:
		if len(jsonValue) == 1 {
			return jsonValue[0].(string), 1, nil
		}
		strs := make([]string, len(jsonValue))
		for i, v := range jsonValue {
			strs[i] = v.(string)
		}
		return strs, len(strs), nil
	}
}

func parseJSONPN(v interface{}) (PersonName, error) {
	pnMap, ok := v.(map[string]interface{})
	if !ok {
		return PersonName{}, fmt.Errorf("invalid PN JSON format")
	}

	pn := PersonName{}
	if alpha, ok := pnMap["Alphabetic"].(string); ok {
		pn.Alphabetic = alpha
	}
	if ideo, ok := pnMap["Ideographic"].(string); ok {
		pn.Ideographic = ideo
	}
	if phon, ok := pnMap["Phonetic"].(string); ok {
		pn.Phonetic = phon
	}

	return pn, nil
}

func parseJSONAT(v interface{}) (tag.Tag, error) {
	tagStr, ok := v.(string)
	if !ok {
		return tag.Tag(0), fmt.Errorf("invalid AT JSON format")
	}
	return parseTagString(tagStr)
}

// DICOM date/time formatting functions

func formatDA(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("20060102")
}

func formatDT(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	// Format: YYYYMMDDHHMMSS.FFFFFF+ZZZZ
	// For simplicity, using basic format without fractional seconds and timezone
	return t.Format("20060102150405")
}

func formatTM(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	// Format: HHMMSS.FFFFFF
	return t.Format("150405")
}
