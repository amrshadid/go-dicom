package charset

import (
	"bytes"
	"context"
	"strings"

	"github.com/amrshadid/go-dicom/config"
)

const (
	// PersonNameComponentSeparator separates components within a group (^)
	PersonNameComponentSeparator byte = 0x5E

	// PersonNameGroupSeparator separates component groups (=)
	PersonNameGroupSeparator byte = 0x3D
)

// PersonName represents a DICOM Person Name (PN VR) with alphabetic, ideographic, and phonetic representations.
type PersonName struct {
	Alphabetic    string
	Ideographic   string
	Phonetic      string
	originalBytes []byte
}

// NewPersonName creates a PersonName from component groups.
func NewPersonName(alphabetic, ideographic, phonetic string) *PersonName {
	return &PersonName{
		Alphabetic:  alphabetic,
		Ideographic: ideographic,
		Phonetic:    phonetic,
	}
}

// String returns a string representation of the PersonName.
// Returns the concatenation of all non-empty groups separated by "=".
func (pn *PersonName) String() string {
	parts := make([]string, 0, 3)

	if pn.Alphabetic != "" {
		parts = append(parts, pn.Alphabetic)
	}

	if pn.Ideographic != "" {
		// Ensure we have the separator from previous group
		if len(parts) == 0 {
			parts = append(parts, "")
		}
		parts = append(parts, pn.Ideographic)
	}

	if pn.Phonetic != "" {
		// Ensure we have separators from previous groups
		for len(parts) < 2 {
			parts = append(parts, "")
		}
		parts = append(parts, pn.Phonetic)
	}

	return strings.Join(parts, "=")
}

// IsEmpty returns true if all components are empty.
func (pn *PersonName) IsEmpty() bool {
	return pn.Alphabetic == "" && pn.Ideographic == "" && pn.Phonetic == ""
}

// FamilyName returns the family name from the alphabetic representation.
func (pn *PersonName) FamilyName() string {
	return pn.getComponent(pn.Alphabetic, 0)
}

// GivenName returns the given name from the alphabetic representation.
func (pn *PersonName) GivenName() string {
	return pn.getComponent(pn.Alphabetic, 1)
}

// MiddleName returns the middle name from the alphabetic representation.
func (pn *PersonName) MiddleName() string {
	return pn.getComponent(pn.Alphabetic, 2)
}

// Prefix returns the name prefix from the alphabetic representation.
func (pn *PersonName) Prefix() string {
	return pn.getComponent(pn.Alphabetic, 3)
}

// Suffix returns the name suffix from the alphabetic representation.
func (pn *PersonName) Suffix() string {
	return pn.getComponent(pn.Alphabetic, 4)
}

// getComponent extracts a component from a group string.
func (pn *PersonName) getComponent(group string, index int) string {
	if group == "" {
		return ""
	}

	components := strings.Split(group, "^")
	if index >= len(components) {
		return ""
	}

	return components[index]
}

// DecodePersonName decodes a DICOM Person Name value from bytes.
func DecodePersonName(value []byte, encodings []string) (*PersonName, error) {
	return DecodePersonNameWithContext(context.Background(), value, encodings)
}

// DecodePersonNameWithContext decodes a Person Name with context support.
func DecodePersonNameWithContext(ctx context.Context, value []byte, encodings []string) (*PersonName, error) {
	if len(value) == 0 {
		return &PersonName{}, nil
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	// Split by group separator (=)
	groups := bytes.Split(value, []byte{PersonNameGroupSeparator})

	pn := &PersonName{
		originalBytes: make([]byte, len(value)),
	}
	copy(pn.originalBytes, value)

	validationMode := config.ReadingValidationModeFromContext(ctx)

	// Decode alphabetic group (first encoding)
	if len(groups) > 0 && len(groups[0]) > 0 {
		decoded, err := DecodeBytes(groups[0], []string{encodings[0]}, PersonNameDelimiters)
		if err != nil {
			return nil, err
		}
		pn.Alphabetic = decoded
	}

	// Decode ideographic group (second encoding, or first if not available)
	if len(groups) > 1 && len(groups[1]) > 0 {
		encoding := encodings[0]
		if len(encodings) > 1 {
			encoding = encodings[1]
		}
		decoded, err := DecodeBytesWithContext(ctx, groups[1], []string{encoding}, PersonNameDelimiters)
		if err != nil && validationMode == config.RAISE {
			return nil, err
		}
		if err == nil {
			pn.Ideographic = decoded
		}
	}

	// Decode phonetic group (third encoding, or first if not available)
	if len(groups) > 2 && len(groups[2]) > 0 {
		encoding := encodings[0]
		if len(encodings) > 2 {
			encoding = encodings[2]
		} else if len(encodings) > 1 {
			encoding = encodings[1]
		}
		decoded, err := DecodeBytesWithContext(ctx, groups[2], []string{encoding}, PersonNameDelimiters)
		if err != nil && validationMode == config.RAISE {
			return nil, err
		}
		if err == nil {
			pn.Phonetic = decoded
		}
	}

	return pn, nil
}

// EncodePersonName encodes a PersonName to DICOM byte representation.
func EncodePersonName(pn *PersonName, encodings []string) ([]byte, error) {
	return EncodePersonNameWithContext(context.Background(), pn, encodings)
}

// EncodePersonNameWithContext encodes a Person Name with context support.
func EncodePersonNameWithContext(ctx context.Context, pn *PersonName, encodings []string) ([]byte, error) {
	if pn == nil || pn.IsEmpty() {
		return []byte{}, nil
	}

	// If we have original bytes and encodings match, return original for lossless round-trip
	if len(pn.originalBytes) > 0 {
		return pn.originalBytes, nil
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	var parts [][]byte

	// Encode alphabetic group (first encoding)
	if pn.Alphabetic != "" {
		encoded, err := EncodeStringWithContext(ctx, pn.Alphabetic, []string{encodings[0]})
		if err != nil {
			return nil, err
		}
		parts = append(parts, encoded)
	}

	// Encode ideographic group (second encoding, or first if not available)
	if pn.Ideographic != "" {
		// Ensure we have a separator from previous group
		if len(parts) == 0 {
			parts = append(parts, []byte{})
		}

		encoding := encodings[0]
		if len(encodings) > 1 {
			encoding = encodings[1]
		}

		encoded, err := EncodeStringWithContext(ctx, pn.Ideographic, []string{encoding})
		if err != nil {
			return nil, err
		}
		parts = append(parts, encoded)
	}

	// Encode phonetic group (third encoding, or first if not available)
	if pn.Phonetic != "" {
		// Ensure we have separators from previous groups
		for len(parts) < 2 {
			parts = append(parts, []byte{})
		}

		encoding := encodings[0]
		if len(encodings) > 2 {
			encoding = encodings[2]
		} else if len(encodings) > 1 {
			encoding = encodings[1]
		}

		encoded, err := EncodeStringWithContext(ctx, pn.Phonetic, []string{encoding})
		if err != nil {
			return nil, err
		}
		parts = append(parts, encoded)
	}

	// Join with group separator
	return bytes.Join(parts, []byte{PersonNameGroupSeparator}), nil
}

// FromComponents creates a PersonName from individual name components.
func FromComponents(familyName, givenName, middleName, prefix, suffix string) *PersonName {
	components := []string{familyName, givenName, middleName, prefix, suffix}

	// Trim trailing empty components
	lastNonEmpty := -1
	for i := len(components) - 1; i >= 0; i-- {
		if components[i] != "" {
			lastNonEmpty = i
			break
		}
	}

	if lastNonEmpty < 0 {
		return &PersonName{}
	}

	alphabetic := strings.Join(components[:lastNonEmpty+1], "^")
	return &PersonName{
		Alphabetic: alphabetic,
	}
}

// FromNamedComponents creates a PersonName with all three component groups.
func FromNamedComponents(alphabetic, ideographic, phonetic string) *PersonName {
	return &PersonName{
		Alphabetic:  alphabetic,
		Ideographic: ideographic,
		Phonetic:    phonetic,
	}
}
