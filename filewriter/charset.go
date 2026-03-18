package filewriter

import (
	"context"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/tag"
)

// SpecificCharacterSetTag is the DICOM tag (0008,0005) that defines the character set encoding.
const SpecificCharacterSetTag = tag.Tag(0x00080005)

// DICOMFileWriterWithCharset extends DICOMFileWriter with character set encoding support.
type DICOMFileWriterWithCharset struct {
	*DICOMFileWriter
	characterSet *charset.CharacterSet
}

// NewDICOMFileWriterWithCharset creates a new DICOM file writer with character set support.
//
// Parameters:
//   - dfw: An existing DICOMFileWriter
//   - cs: The CharacterSet to use for encoding text (nil uses default ISO-8859-1)
//
// Example:
//
//	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 192"}) // UTF-8
//	baseWriter := filewriter.NewDICOMFileWriter(filebaseWriter)
//	writer := filewriter.NewDICOMFileWriterWithCharset(baseWriter, cs)
func NewDICOMFileWriterWithCharset(dfw *DICOMFileWriter, cs *charset.CharacterSet) *DICOMFileWriterWithCharset {
	if dfw == nil {
		return nil
	}

	dfwc := &DICOMFileWriterWithCharset{
		DICOMFileWriter: dfw,
		characterSet:    cs,
	}

	// Automatically add SpecificCharacterSet tag if character set is specified
	if cs != nil && !cs.IsDefault {
		dfwc.addSpecificCharacterSetElement()
	}

	return dfwc
}

// SetCharacterSet sets the character set for encoding text values.
//
// This also automatically adds/updates the SpecificCharacterSet (0008,0005) tag.
// If cs is nil, the tag is removed (meaning default ISO-8859-1 encoding).
//
// Example:
//
//	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 100"})
//	writer.SetCharacterSet(cs)
func (dfwc *DICOMFileWriterWithCharset) SetCharacterSet(cs *charset.CharacterSet) {
	dfwc.characterSet = cs

	// Remove existing SpecificCharacterSet tag
	dfwc.removeSpecificCharacterSetElement()

	// Add new SpecificCharacterSet tag if not default
	if cs != nil && !cs.IsDefault {
		dfwc.addSpecificCharacterSetElement()
	}
}

// GetCharacterSet returns the current character set.
func (dfwc *DICOMFileWriterWithCharset) GetCharacterSet() *charset.CharacterSet {
	return dfwc.characterSet
}

// AddTextElement adds a text data element with automatic encoding.
//
// This encodes the text using the writer's CharacterSet and adds it to the dataset.
//
// Parameters:
//   - t: The tag for this element
//   - vr: The Value Representation (must be a text VR: LO, LT, SH, ST, UC, UT)
//   - text: The text string to encode
//
// Returns:
//   - Error if encoding fails or VR is not a text VR
//
// Example:
//
//	err := writer.AddTextElement(tag.PatientName, "PN", "山田^太郎")
//	if err != nil {
//	    log.Fatal(err)
//	}
func (dfwc *DICOMFileWriterWithCharset) AddTextElement(t tag.Tag, vr string, text string) error {
	return dfwc.AddTextElementWithContext(context.Background(), t, vr, text)
}

// AddTextElementWithContext adds a text element with context support.
func (dfwc *DICOMFileWriterWithCharset) AddTextElementWithContext(ctx context.Context, t tag.Tag, vr string, text string) error {
	// Check if this is a text VR
	if !isTextVR(vr) {
		return fmt.Errorf("VR %s is not a text VR", vr)
	}

	// Check if PN (use AddPersonNameElement for PN)
	if vr == "PN" {
		return fmt.Errorf("use AddPersonNameElement() for PN VR, not AddTextElement()")
	}

	// Get encodings
	encodings := []string{charset.DefaultEncoding}
	if dfwc.characterSet != nil {
		encodings = dfwc.characterSet.Encodings
	}

	// Encode the text
	encoded, err := charset.EncodeStringWithContext(ctx, text, encodings)
	if err != nil {
		return fmt.Errorf("failed to encode text: %w", err)
	}

	// Create and add data element
	elem := &DataElement{
		Tag:    t,
		VR:     vr,
		Value:  encoded,
		Length: uint32(len(encoded)),
	}

	return dfwc.AddDataElement(elem)
}

// AddPersonNameElement adds a PersonName data element with automatic encoding.
//
// This encodes the PersonName using the writer's CharacterSet and adds it to the dataset.
//
// Parameters:
//   - t: The tag for this element (typically a PN VR tag)
//   - pn: The PersonName to encode
//
// Returns:
//   - Error if encoding fails
//
// Example:
//
//	pn := charset.FromComponents("Yamada", "Taro", "", "Dr.", "")
//	err := writer.AddPersonNameElement(tag.PatientName, pn)
//	if err != nil {
//	    log.Fatal(err)
//	}
func (dfwc *DICOMFileWriterWithCharset) AddPersonNameElement(t tag.Tag, pn *charset.PersonName) error {
	return dfwc.AddPersonNameElementWithContext(context.Background(), t, pn)
}

// AddPersonNameElementWithContext adds a PersonName element with context support.
func (dfwc *DICOMFileWriterWithCharset) AddPersonNameElementWithContext(ctx context.Context, t tag.Tag, pn *charset.PersonName) error {
	// Get encodings
	encodings := []string{charset.DefaultEncoding}
	if dfwc.characterSet != nil {
		encodings = dfwc.characterSet.Encodings
	}

	// Encode the PersonName
	encoded, err := charset.EncodePersonNameWithContext(ctx, pn, encodings)
	if err != nil {
		return fmt.Errorf("failed to encode PersonName: %w", err)
	}

	// Create and add data element
	elem := &DataElement{
		Tag:    t,
		VR:     "PN",
		Value:  encoded,
		Length: uint32(len(encoded)),
	}

	return dfwc.AddDataElement(elem)
}

// AddTextElements adds multiple text data elements with automatic encoding.
//
// This is a convenience method for adding multiple text elements at once.
//
// Parameters:
//   - elements: Map of tag -> (vr, text) pairs
//
// Returns:
//   - Error if any encoding fails
//
// Example:
//
//	elements := map[tag.Tag]struct{ VR, Text string }{
//	    tag.PatientName: {"PN", "Yamada^Taro"},
//	    tag.StudyDescription: {"LO", "CT Chest"},
//	}
//	err := writer.AddTextElements(elements)
func (dfwc *DICOMFileWriterWithCharset) AddTextElements(elements map[tag.Tag]struct{ VR, Text string }) error {
	return dfwc.AddTextElementsWithContext(context.Background(), elements)
}

// AddTextElementsWithContext adds multiple text elements with context support.
func (dfwc *DICOMFileWriterWithCharset) AddTextElementsWithContext(ctx context.Context, elements map[tag.Tag]struct{ VR, Text string }) error {
	for t, elem := range elements {
		if elem.VR == "PN" {
			return fmt.Errorf("cannot add PN VR through AddTextElements, use AddPersonNameElement")
		}

		if err := dfwc.AddTextElementWithContext(ctx, t, elem.VR, elem.Text); err != nil {
			return fmt.Errorf("failed to add element %s: %w", t.Hex(), err)
		}
	}

	return nil
}

// EncodeTextValue is a convenience method to encode text without adding it to the dataset.
//
// This is useful when you need the encoded bytes for other purposes.
//
// Parameters:
//   - text: The text string to encode
//
// Returns:
//   - Encoded byte slice
//   - Error if encoding fails
//
// Example:
//
//	encoded, err := writer.EncodeTextValue("Patient Name")
func (dfwc *DICOMFileWriterWithCharset) EncodeTextValue(text string) ([]byte, error) {
	return dfwc.EncodeTextValueWithContext(context.Background(), text)
}

// EncodeTextValueWithContext encodes text with context support.
func (dfwc *DICOMFileWriterWithCharset) EncodeTextValueWithContext(ctx context.Context, text string) ([]byte, error) {
	encodings := []string{charset.DefaultEncoding}
	if dfwc.characterSet != nil {
		encodings = dfwc.characterSet.Encodings
	}

	return charset.EncodeStringWithContext(ctx, text, encodings)
}

// EncodePersonNameValue is a convenience method to encode a PersonName without adding it to the dataset.
//
// Parameters:
//   - pn: The PersonName to encode
//
// Returns:
//   - Encoded byte slice
//   - Error if encoding fails
//
// Example:
//
//	pn := charset.FromComponents("Yamada", "Taro", "", "", "")
//	encoded, err := writer.EncodePersonNameValue(pn)
func (dfwc *DICOMFileWriterWithCharset) EncodePersonNameValue(pn *charset.PersonName) ([]byte, error) {
	return dfwc.EncodePersonNameValueWithContext(context.Background(), pn)
}

// EncodePersonNameValueWithContext encodes a PersonName with context support.
func (dfwc *DICOMFileWriterWithCharset) EncodePersonNameValueWithContext(ctx context.Context, pn *charset.PersonName) ([]byte, error) {
	encodings := []string{charset.DefaultEncoding}
	if dfwc.characterSet != nil {
		encodings = dfwc.characterSet.Encodings
	}

	return charset.EncodePersonNameWithContext(ctx, pn, encodings)
}

// Helper methods

// addSpecificCharacterSetElement adds the SpecificCharacterSet (0008,0005) tag.
func (dfwc *DICOMFileWriterWithCharset) addSpecificCharacterSetElement() {
	if dfwc.characterSet == nil {
		return
	}

	// Create value string from original values
	valueStr := strings.Join(dfwc.characterSet.OriginalValues, "\\")
	valueBytes := []byte(valueStr)

	// Create data element
	elem := &DataElement{
		Tag:    SpecificCharacterSetTag,
		VR:     "CS",
		Value:  valueBytes,
		Length: uint32(len(valueBytes)),
	}

	// Check if it already exists and remove it
	dfwc.removeSpecificCharacterSetElement()

	// Add at the beginning (SpecificCharacterSet should come early)
	dfwc.dataElements = append([]*DataElement{elem}, dfwc.dataElements...)
}

// removeSpecificCharacterSetElement removes the SpecificCharacterSet tag if it exists.
func (dfwc *DICOMFileWriterWithCharset) removeSpecificCharacterSetElement() {
	for i, elem := range dfwc.dataElements {
		if elem.Tag == SpecificCharacterSetTag {
			// Remove this element
			dfwc.dataElements = append(dfwc.dataElements[:i], dfwc.dataElements[i+1:]...)
			return
		}
	}
}

// isTextVR returns true if the VR is a text VR that uses Specific Character Set encoding.
func isTextVR(vr string) bool {
	switch vr {
	case "LO", "LT", "PN", "SH", "ST", "UC", "UT":
		return true
	default:
		return false
	}
}
