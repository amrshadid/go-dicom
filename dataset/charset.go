package dataset

import (
	"context"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// SpecificCharacterSetTag is the DICOM tag (0008,0005).
const SpecificCharacterSetTag = tag.Tag(0x00080005)

// GetCharacterSet retrieves the CharacterSet from SpecificCharacterSet tag.
// Returns nil if not present (defaults to ISO-8859-1).
func (ds *Dataset) GetCharacterSet() *charset.CharacterSet {
	return ds.GetCharacterSetWithContext(context.Background())
}

// GetCharacterSetWithContext retrieves the CharacterSet with context support.
func (ds *Dataset) GetCharacterSetWithContext(ctx context.Context) *charset.CharacterSet {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	elem, exists := ds.elements[uint32(SpecificCharacterSetTag)]
	if !exists {
		return nil // Use default encoding
	}

	// Get raw value
	value := elem.GetValue()
	if value == nil {
		return nil
	}

	var valueStr string
	switch v := value.(type) {
	case []byte:
		valueStr = strings.TrimSpace(string(v))
	case string:
		valueStr = strings.TrimSpace(v)
	default:
		return nil
	}

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

// SetCharacterSet sets the SpecificCharacterSet tag.
// If cs is nil, removes the tag (defaults to ISO-8859-1).
func (ds *Dataset) SetCharacterSet(cs *charset.CharacterSet) error {
	return ds.SetCharacterSetWithContext(context.Background(), cs)
}

// SetCharacterSetWithContext sets the CharacterSet with context support.
func (ds *Dataset) SetCharacterSetWithContext(ctx context.Context, cs *charset.CharacterSet) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	tagVal := uint32(SpecificCharacterSetTag)

	if cs == nil {
		delete(ds.elements, tagVal)
		for i, v := range ds.order {
			if v == tagVal {
				ds.order = append(ds.order[:i], ds.order[i+1:]...)
				break
			}
		}
		return nil
	}

	valueStr := strings.Join(cs.OriginalValues, "\\")
	valueBytes := []byte(valueStr)

	elem, exists := ds.elements[tagVal]
	if exists {
		elem.SetValue(valueBytes)
	} else {
		newElem := dataelem.NewDataElement(SpecificCharacterSetTag, dataelem.CS, valueBytes)
		ds.elements[tagVal] = newElem
		ds.order = append(ds.order, tagVal)
	}

	return nil
}

// GetTextValue retrieves and decodes a text value using character set encoding.
func (ds *Dataset) GetTextValue(t tag.Tag) (string, error) {
	return ds.GetTextValueWithContext(context.Background(), t)
}

// GetTextValueWithContext retrieves and decodes a text value with context support.
func (ds *Dataset) GetTextValueWithContext(ctx context.Context, t tag.Tag) (string, error) {
	elem, exists := ds.Get(t)
	if !exists {
		return "", fmt.Errorf("tag %s not found in dataset", t.Hex())
	}

	cs := ds.GetCharacterSetWithContext(ctx)
	return elem.GetTextValueWithContext(ctx, cs)
}

// GetPersonName retrieves and decodes a PersonName value using character set encoding.
func (ds *Dataset) GetPersonName(t tag.Tag) (*charset.PersonName, error) {
	return ds.GetPersonNameWithContext(context.Background(), t)
}

// GetPersonNameWithContext retrieves and decodes a PersonName with context support.
func (ds *Dataset) GetPersonNameWithContext(ctx context.Context, t tag.Tag) (*charset.PersonName, error) {
	elem, exists := ds.Get(t)
	if !exists {
		return nil, fmt.Errorf("tag %s not found in dataset", t.Hex())
	}

	cs := ds.GetCharacterSetWithContext(ctx)
	return elem.GetPersonNameValueWithContext(ctx, cs)
}

// SetTextValue encodes and sets a text value using character set encoding.
func (ds *Dataset) SetTextValue(t tag.Tag, vr dataelem.VR, text string) error {
	return ds.SetTextValueWithContext(context.Background(), t, vr, text)
}

// SetTextValueWithContext encodes and sets a text value with context support.
func (ds *Dataset) SetTextValueWithContext(ctx context.Context, t tag.Tag, vr dataelem.VR, text string) error {
	cs := ds.GetCharacterSetWithContext(ctx)

	elem, exists := ds.Get(t)
	if !exists {
		elem = dataelem.NewDataElement(t, vr, nil)
	}

	if err := elem.SetTextValueWithContext(ctx, text, cs); err != nil {
		return err
	}

	return ds.Add(elem)
}

// SetPersonName encodes and sets a PersonName value using character set encoding.
func (ds *Dataset) SetPersonName(t tag.Tag, pn *charset.PersonName) error {
	return ds.SetPersonNameWithContext(context.Background(), t, pn)
}

// SetPersonNameWithContext encodes and sets a PersonName with context support.
func (ds *Dataset) SetPersonNameWithContext(ctx context.Context, t tag.Tag, pn *charset.PersonName) error {
	cs := ds.GetCharacterSetWithContext(ctx)

	elem, exists := ds.Get(t)
	if !exists {
		elem = dataelem.NewDataElement(t, dataelem.PN, nil)
	}

	if err := elem.SetPersonNameValueWithContext(ctx, pn, cs); err != nil {
		return err
	}

	return ds.Add(elem)
}

// GetAllTextValues retrieves and decodes all text VR elements.
func (ds *Dataset) GetAllTextValues() map[tag.Tag]string {
	return ds.GetAllTextValuesWithContext(context.Background())
}

// GetAllTextValuesWithContext retrieves all text values with context support.
func (ds *Dataset) GetAllTextValuesWithContext(ctx context.Context) map[tag.Tag]string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make(map[tag.Tag]string)
	cs := ds.GetCharacterSetWithContext(ctx)

	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		t := tag.FromInt(tagVal)

		if !dataelem.IsTextVR(elem.GetVR()) {
			continue
		}

		if elem.GetVR() == dataelem.PN {
			continue
		}

		if text, err := elem.GetTextValueWithContext(ctx, cs); err == nil {
			result[t] = text
		}
	}

	return result
}

// GetAllPersonNames retrieves and decodes all PN VR elements.
func (ds *Dataset) GetAllPersonNames() map[tag.Tag]*charset.PersonName {
	return ds.GetAllPersonNamesWithContext(context.Background())
}

// GetAllPersonNamesWithContext retrieves all PersonName values with context support.
func (ds *Dataset) GetAllPersonNamesWithContext(ctx context.Context) map[tag.Tag]*charset.PersonName {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make(map[tag.Tag]*charset.PersonName)
	cs := ds.GetCharacterSetWithContext(ctx)

	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]

		if elem.GetVR() != dataelem.PN {
			continue
		}

		t := tag.FromInt(tagVal)

		if pn, err := elem.GetPersonNameValueWithContext(ctx, cs); err == nil {
			result[t] = pn
		}
	}

	return result
}

// parseSpecificCharacterSetValue parses the SpecificCharacterSet tag value.
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
