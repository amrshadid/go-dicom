package dataelem

import (
	"context"
	"fmt"

	"github.com/amrshadid/go-dicom/charset"
)

// Note: IsTextVR function is already defined in dataelem.go and checks if a VR is text-based.
// We use that function to determine if a VR should use character set encoding.

// GetTextValue decodes the element's value as text using the specified character set.
//
// This method is used for text VRs (LO, LT, SH, ST, UC, UT) that use the
// Specific Character Set encoding.
//
// Parameters:
//   - cs: The CharacterSet to use for decoding (nil uses default encoding)
//
// Returns:
//   - Decoded text string
//   - Error if the VR is not a text VR or decoding fails
//
// Example:
//
//	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 192"})
//	elem := dataelem.NewDataElement(tag, dataelem.LO, rawBytes)
//	text, err := elem.GetTextValue(cs)
func (de *DataElement) GetTextValue(cs *charset.CharacterSet) (string, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	return de.getTextValueLocked(cs)
}

// getTextValueLocked is the internal implementation (assumes lock is held).
func (de *DataElement) getTextValueLocked(cs *charset.CharacterSet) (string, error) {
	// Check if this is a text VR (excluding PN which has special handling)
	if de.VR == PN {
		return "", fmt.Errorf("use GetPersonNameValue() for PN VR, not GetTextValue()")
	}

	if !IsTextVR(de.VR) {
		return "", fmt.Errorf("VR %s is not a text VR", de.VR)
	}

	// Get raw bytes
	raw, err := de.getValueAsBytes()
	if err != nil {
		return "", err
	}

	if len(raw) == 0 {
		return "", nil
	}

	// Get encodings
	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	// Decode using charset module
	return charset.DecodeBytes(raw, encodings, charset.DefaultTextDelimiters)
}

// GetTextValueWithContext decodes text with context support for validation mode override.
func (de *DataElement) GetTextValueWithContext(ctx context.Context, cs *charset.CharacterSet) (string, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR == PN {
		return "", fmt.Errorf("use GetPersonNameValue() for PN VR, not GetTextValue()")
	}

	if !IsTextVR(de.VR) {
		return "", fmt.Errorf("VR %s is not a text VR", de.VR)
	}

	raw, err := de.getValueAsBytes()
	if err != nil {
		return "", err
	}

	if len(raw) == 0 {
		return "", nil
	}

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	return charset.DecodeBytesWithContext(ctx, raw, encodings, charset.DefaultTextDelimiters)
}

// GetPersonNameValue decodes the element's value as a PersonName using the specified character set.
//
// This method is specifically for PN VR elements. It supports the full DICOM
// PersonName structure with alphabetic, ideographic, and phonetic component groups.
//
// Parameters:
//   - cs: The CharacterSet to use for decoding (nil uses default encoding)
//
// Returns:
//   - Decoded PersonName structure
//   - Error if the VR is not PN or decoding fails
//
// Example:
//
//	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 192"})
//	elem := dataelem.NewDataElement(tag, dataelem.PN, rawBytes)
//	pn, err := elem.GetPersonNameValue(cs)
//	fmt.Printf("Family: %s, Given: %s\n", pn.FamilyName(), pn.GivenName())
func (de *DataElement) GetPersonNameValue(cs *charset.CharacterSet) (*charset.PersonName, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	return de.getPersonNameValueLocked(cs)
}

// getPersonNameValueLocked is the internal implementation (assumes lock is held).
func (de *DataElement) getPersonNameValueLocked(cs *charset.CharacterSet) (*charset.PersonName, error) {
	if de.VR != PN {
		return nil, fmt.Errorf("VR %s is not PN", de.VR)
	}

	// Get raw bytes
	raw, err := de.getValueAsBytes()
	if err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return &charset.PersonName{}, nil
	}

	// Get encodings
	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	// Decode using charset module
	return charset.DecodePersonName(raw, encodings)
}

// GetPersonNameValueWithContext decodes PersonName with context support.
func (de *DataElement) GetPersonNameValueWithContext(ctx context.Context, cs *charset.CharacterSet) (*charset.PersonName, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != PN {
		return nil, fmt.Errorf("VR %s is not PN", de.VR)
	}

	raw, err := de.getValueAsBytes()
	if err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return &charset.PersonName{}, nil
	}

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	return charset.DecodePersonNameWithContext(ctx, raw, encodings)
}

// SetTextValue sets the element's value from a text string, encoding it with the specified character set.
//
// This method encodes the text and stores it as the element's value.
//
// Parameters:
//   - text: The text string to encode
//   - cs: The CharacterSet to use for encoding (nil uses default encoding)
//
// Returns:
//   - Error if the VR is not a text VR or encoding fails
//
// Example:
//
//	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 192"})
//	elem := dataelem.NewDataElement(tag, dataelem.LO, nil)
//	err := elem.SetTextValue("Patient Name", cs)
func (de *DataElement) SetTextValue(text string, cs *charset.CharacterSet) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	return de.setTextValueLocked(text, cs)
}

// setTextValueLocked is the internal implementation (assumes lock is held).
func (de *DataElement) setTextValueLocked(text string, cs *charset.CharacterSet) error {
	if de.VR == PN {
		return fmt.Errorf("use SetPersonNameValue() for PN VR, not SetTextValue()")
	}

	if !IsTextVR(de.VR) {
		return fmt.Errorf("VR %s is not a text VR", de.VR)
	}

	// Get encodings
	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	// Encode using charset module
	encoded, err := charset.EncodeString(text, encodings)
	if err != nil {
		return err
	}

	de.Value = encoded
	return nil
}

// SetTextValueWithContext encodes text with context support.
func (de *DataElement) SetTextValueWithContext(ctx context.Context, text string, cs *charset.CharacterSet) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	if de.VR == PN {
		return fmt.Errorf("use SetPersonNameValue() for PN VR, not SetTextValue()")
	}

	if !IsTextVR(de.VR) {
		return fmt.Errorf("VR %s is not a text VR", de.VR)
	}

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	encoded, err := charset.EncodeStringWithContext(ctx, text, encodings)
	if err != nil {
		return err
	}

	de.Value = encoded
	return nil
}

// SetPersonNameValue sets the element's value from a PersonName, encoding it with the specified character set.
//
// Parameters:
//   - pn: The PersonName to encode
//   - cs: The CharacterSet to use for encoding (nil uses default encoding)
//
// Returns:
//   - Error if the VR is not PN or encoding fails
//
// Example:
//
//	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 192"})
//	pn := charset.FromComponents("Doe", "John", "", "", "")
//	elem := dataelem.NewDataElement(tag, dataelem.PN, nil)
//	err := elem.SetPersonNameValue(pn, cs)
func (de *DataElement) SetPersonNameValue(pn *charset.PersonName, cs *charset.CharacterSet) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	return de.setPersonNameValueLocked(pn, cs)
}

// setPersonNameValueLocked is the internal implementation (assumes lock is held).
func (de *DataElement) setPersonNameValueLocked(pn *charset.PersonName, cs *charset.CharacterSet) error {
	if de.VR != PN {
		return fmt.Errorf("VR %s is not PN", de.VR)
	}

	if pn == nil {
		de.Value = []byte{}
		return nil
	}

	// Get encodings
	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	// Encode using charset module
	encoded, err := charset.EncodePersonName(pn, encodings)
	if err != nil {
		return err
	}

	de.Value = encoded
	return nil
}

// SetPersonNameValueWithContext encodes PersonName with context support.
func (de *DataElement) SetPersonNameValueWithContext(ctx context.Context, pn *charset.PersonName, cs *charset.CharacterSet) error {
	de.mu.Lock()
	defer de.mu.Unlock()

	if de.VR != PN {
		return fmt.Errorf("VR %s is not PN", de.VR)
	}

	if pn == nil {
		de.Value = []byte{}
		return nil
	}

	encodings := []string{charset.DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	encoded, err := charset.EncodePersonNameWithContext(ctx, pn, encodings)
	if err != nil {
		return err
	}

	de.Value = encoded
	return nil
}

// getValueAsBytes converts the element's value to bytes.
// Handles both []byte and string types.
func (de *DataElement) getValueAsBytes() ([]byte, error) {
	if de.Value == nil {
		return []byte{}, nil
	}

	switch v := de.Value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("value type %T cannot be converted to bytes", de.Value)
	}
}
