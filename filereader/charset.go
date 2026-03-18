package filereader

import (
	"context"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/tag"
)

// SpecificCharacterSetTag is the DICOM tag (0008,0005) that defines the character set encoding.
const SpecificCharacterSetTag = tag.Tag(0x00080005)

// GetCharacterSet extracts the CharacterSet from the DICOM file data elements.
func (df *DICOMFile) GetCharacterSet() *charset.CharacterSet {
	return df.GetCharacterSetWithContext(context.Background())
}

// GetCharacterSetWithContext extracts the CharacterSet with context support.
func (df *DICOMFile) GetCharacterSetWithContext(ctx context.Context) *charset.CharacterSet {
	for _, elem := range df.DataElements {
		if elem.Tag == SpecificCharacterSetTag {
			valueStr := strings.TrimSpace(string(elem.Value))
			if valueStr == "" {
				return nil
			}

			values := parseSpecificCharacterSetValue(valueStr)

			cs, err := charset.NewCharacterSetWithContext(ctx, values)
			if err != nil {
				return nil
			}

			return cs
		}
	}

	return nil
}

// DecodeTextValue decodes a text value from a data element using the file's character set.
// This method is for single-byte text VRs (LO, LT, SH, ST, UC, UT).
// For PersonName (PN) VR, use DecodePersonName instead.
func (df *DICOMFile) DecodeTextValue(elem *DataElementValue) (string, error) {
	return df.DecodeTextValueWithContext(context.Background(), elem)
}

// DecodeTextValueWithContext decodes a text value with context support.
// The context can override the global character set configuration if needed.
// Supported text VRs: LO, LT, SH, ST, UC, UT.
func (df *DICOMFile) DecodeTextValueWithContext(ctx context.Context, elem *DataElementValue) (string, error) {
	if elem == nil {
		return "", fmt.Errorf("element is nil")
	}

	if !isTextVR(elem.VR) {
		return "", fmt.Errorf("VR %s is not a text VR", elem.VR)
	}

	cs := df.GetCharacterSetWithContext(ctx)

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	if elem.VR == "PN" {
		return "", fmt.Errorf("use DecodePersonName() for PN VR, not DecodeTextValue()")
	}

	return charset.DecodeBytesWithContext(ctx, elem.Value, encodings, charset.DefaultTextDelimiters)
}

// DecodePersonName decodes a PersonName value from a data element using the file's character set.
func (df *DICOMFile) DecodePersonName(elem *DataElementValue) (*charset.PersonName, error) {
	return df.DecodePersonNameWithContext(context.Background(), elem)
}

// DecodePersonNameWithContext decodes a PersonName with context support.
func (df *DICOMFile) DecodePersonNameWithContext(ctx context.Context, elem *DataElementValue) (*charset.PersonName, error) {
	if elem == nil {
		return nil, fmt.Errorf("element is nil")
	}

	if elem.VR != "PN" {
		return nil, fmt.Errorf("VR %s is not PN", elem.VR)
	}

	cs := df.GetCharacterSetWithContext(ctx)

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	return charset.DecodePersonNameWithContext(ctx, elem.Value, encodings)
}

// DecodeAllTextValues decodes all text values in the DICOM file.
// Returns a map of tags to decoded text values, excluding PersonName elements.
func (df *DICOMFile) DecodeAllTextValues() map[tag.Tag]string {
	return df.DecodeAllTextValuesWithContext(context.Background())
}

// DecodeAllTextValuesWithContext decodes all text values with context support.
func (df *DICOMFile) DecodeAllTextValuesWithContext(ctx context.Context) map[tag.Tag]string {
	result := make(map[tag.Tag]string)
	cs := df.GetCharacterSetWithContext(ctx)

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	for _, elem := range df.DataElements {
		if !isTextVR(elem.VR) {
			continue
		}

		if elem.VR == "PN" {
			continue
		}

		if text, err := charset.DecodeBytesWithContext(ctx, elem.Value, encodings, charset.DefaultTextDelimiters); err == nil {
			result[elem.Tag] = text
		}
	}

	return result
}

// DecodeAllPersonNames decodes all PersonName values in the DICOM file.
// Returns a map of tags to decoded PersonName structures.
func (df *DICOMFile) DecodeAllPersonNames() map[tag.Tag]*charset.PersonName {
	return df.DecodeAllPersonNamesWithContext(context.Background())
}

// DecodeAllPersonNamesWithContext decodes all PersonName values with context support.
func (df *DICOMFile) DecodeAllPersonNamesWithContext(ctx context.Context) map[tag.Tag]*charset.PersonName {
	result := make(map[tag.Tag]*charset.PersonName)
	cs := df.GetCharacterSetWithContext(ctx)

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	for _, elem := range df.DataElements {
		if elem.VR != "PN" {
			continue
		}

		if pn, err := charset.DecodePersonNameWithContext(ctx, elem.Value, encodings); err == nil {
			result[elem.Tag] = pn
		}
	}

	return result
}

// GetTextValueByTag is a convenience method to get a decoded text value by tag.
func (df *DICOMFile) GetTextValueByTag(t tag.Tag) (string, error) {
	return df.GetTextValueByTagWithContext(context.Background(), t)
}

// GetTextValueByTagWithContext gets a text value by tag with context support.
func (df *DICOMFile) GetTextValueByTagWithContext(ctx context.Context, t tag.Tag) (string, error) {
	for _, elem := range df.DataElements {
		if elem.Tag == t {
			return df.DecodeTextValueWithContext(ctx, elem)
		}
	}

	return "", fmt.Errorf("tag %s not found in file", t.Hex())
}

// GetPersonNameByTag is a convenience method to get a decoded PersonName by tag.
func (df *DICOMFile) GetPersonNameByTag(t tag.Tag) (*charset.PersonName, error) {
	return df.GetPersonNameByTagWithContext(context.Background(), t)
}

// GetPersonNameByTagWithContext gets a PersonName by tag with context support.
func (df *DICOMFile) GetPersonNameByTagWithContext(ctx context.Context, t tag.Tag) (*charset.PersonName, error) {
	for _, elem := range df.DataElements {
		if elem.Tag == t {
			return df.DecodePersonNameWithContext(ctx, elem)
		}
	}

	return nil, fmt.Errorf("tag %s not found in file", t.Hex())
}

// isTextVR returns true if the VR is a text VR that uses Specific Character Set encoding.
// Text VRs include: LO (Long String), LT (Long Text), PN (Person Name), SH (Short String), ST (Short Text), UC (Unlimited Characters), UT (Unlimited Text).
func isTextVR(vr string) bool {
	switch vr {
	case "LO", "LT", "PN", "SH", "ST", "UC", "UT":
		return true
	default:
		return false
	}
}

// parseSpecificCharacterSetValue parses the SpecificCharacterSet tag value.
// The value may contain multiple character set names separated by backslash (e.g., "ISO_IR 192" for UTF-8).
func parseSpecificCharacterSetValue(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := strings.Split(value, "\\")

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		result = append(result, trimmed)
	}

	return result
}
