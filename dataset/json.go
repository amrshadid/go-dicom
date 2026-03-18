package dataset

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// MarshalJSON implements json.Marshaler interface.
func (ds *Dataset) MarshalJSON() ([]byte, error) {
	jsonDS := ds.ToJSON()
	return json.Marshal(jsonDS)
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (ds *Dataset) UnmarshalJSON(data []byte) error {
	var jsonDS JSONDataset
	if err := json.Unmarshal(data, &jsonDS); err != nil {
		return err
	}

	return ds.FromJSON(&jsonDS)
}

// ToJSON converts the dataset to JSON representation.
func (ds *Dataset) ToJSON() *JSONDataset {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	jsonDS := &JSONDataset{
		Elements: make(map[string]JSONElement),
		Metadata: JSONMetadata{
			ElementCount: len(ds.elements),
		},
	}

	// Convert each element
	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		t := tag.FromInt(tagVal)

		jsonElem := ds.elementToJSON(t, elem)
		jsonDS.Elements[t.String()] = jsonElem

		// Update metadata
		if elem.GetVR() == dataelem.SQ {
			jsonDS.Metadata.HasSequences = true
		}
		if t == tag.New(0x7FE0, 0x0010) { // PixelData
			jsonDS.Metadata.HasPixelData = true
		}
	}

	// Add transfer syntax if present
	if ds.ContainsByKeyword("TransferSyntaxUID") {
		jsonDS.Metadata.TransferSyntaxUID = ds.GetStringByKeyword("TransferSyntaxUID")
	}

	// Add character set if present
	if ds.ContainsByKeyword("SpecificCharacterSet") {
		jsonDS.Metadata.CharacterSet = ds.GetStringByKeyword("SpecificCharacterSet")
	}

	return jsonDS
}

// elementToJSON converts a single element to JSON representation.
func (ds *Dataset) elementToJSON(t tag.Tag, elem *dataelem.DataElement) JSONElement {
	vr := elem.GetVR()
	value := elem.GetValue()

	jsonElem := JSONElement{
		Tag:     t.String(),
		VR:      string(vr),
		Keyword: t.GetKeyword(),
		Name:    t.GetName(),
	}

	// Convert value based on VR
	switch vr {
	case dataelem.SQ:
		// Handle sequences
		if seq, ok := value.(*sequence.Sequence); ok {
			items := seq.Items()
			jsonItems := make([]interface{}, len(items))
			for i, item := range items {
				if childDS, ok := item.(*Dataset); ok {
					jsonItems[i] = childDS.ToJSON()
				}
			}
			jsonElem.Value = jsonItems
		}

	case dataelem.OB, dataelem.OW, dataelem.OD, dataelem.OF, dataelem.OL:
		// Binary data - encode as base64
		if b, ok := value.([]byte); ok {
			jsonElem.Value = base64.StdEncoding.EncodeToString(b)
		}

	case dataelem.US, dataelem.SS, dataelem.UL, dataelem.SL, dataelem.FL, dataelem.FD:
		// Numeric data - parse based on VR
		if b, ok := value.([]byte); ok {
			jsonElem.Value = string(b) // Will be parsed appropriately
		}

	default:
		// Text data - convert to string
		if b, ok := value.([]byte); ok {
			jsonElem.Value = trimNullAndSpace(b)
		} else {
			jsonElem.Value = value
		}
	}

	return jsonElem
}

// FromJSON populates the dataset from JSON representation.
func (ds *Dataset) FromJSON(jsonDS *JSONDataset) error {
	ds.Clear()

	for tagStr, jsonElem := range jsonDS.Elements {
		// Parse tag
		t, err := tag.ParseTag(tagStr)
		if err != nil {
			return fmt.Errorf("failed to parse tag %s: %w", tagStr, err)
		}

		// Convert JSON element to DataElement
		elem, err := ds.jsonToElement(t, &jsonElem)
		if err != nil {
			return fmt.Errorf("failed to convert element %s: %w", tagStr, err)
		}

		if err := ds.Add(elem); err != nil {
			return fmt.Errorf("failed to add element %s: %w", tagStr, err)
		}
	}

	return nil
}

// jsonToElement converts a JSON element to a DataElement.
func (ds *Dataset) jsonToElement(t tag.Tag, jsonElem *JSONElement) (*dataelem.DataElement, error) {
	vr := dataelem.VR(jsonElem.VR)

	// Handle sequences
	if vr == dataelem.SQ {
		seq := sequence.New()

		if items, ok := jsonElem.Value.([]interface{}); ok {
			for _, item := range items {
				// Convert item to dataset
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				// Re-marshal and unmarshal to get proper JSONDataset
				itemBytes, err := json.Marshal(itemMap)
				if err != nil {
					continue
				}

				var itemJSON JSONDataset
				if err := json.Unmarshal(itemBytes, &itemJSON); err != nil {
					continue
				}

				childDS := NewDataset()
				if err := childDS.FromJSON(&itemJSON); err != nil {
					continue
				}

				_ = seq.Append(childDS)
			}
		}

		return dataelem.NewDataElement(t, vr, seq), nil
	}

	// Handle binary data (base64 encoded)
	if isBinaryVR(vr) {
		if str, ok := jsonElem.Value.(string); ok {
			decoded, err := base64.StdEncoding.DecodeString(str)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64: %w", err)
			}
			return dataelem.NewDataElement(t, vr, decoded), nil
		}
	}

	// Handle text/numeric data
	var value []byte
	switch v := jsonElem.Value.(type) {
	case string:
		value = []byte(v)
	case []byte:
		value = v
	default:
		value = []byte(fmt.Sprintf("%v", v))
	}

	return dataelem.NewDataElement(t, vr, value), nil
}

// ToJSONString converts the dataset to a JSON string.
func (ds *Dataset) ToJSONString() (string, error) {
	bytes, err := ds.MarshalJSON()
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ToJSONPretty converts the dataset to a pretty-printed JSON string.
func (ds *Dataset) ToJSONPretty() (string, error) {
	jsonDS := ds.ToJSON()
	bytes, err := json.MarshalIndent(jsonDS, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// FromJSONString populates the dataset from a JSON string.
func (ds *Dataset) FromJSONString(jsonStr string) error {
	return ds.UnmarshalJSON([]byte(jsonStr))
}

// SaveJSON writes the dataset to a JSON file.
func (ds *Dataset) SaveJSON(filepath string) error {
	_, err := ds.ToJSONPretty()
	if err != nil {
		return err
	}

	// Would use os.WriteFile here, but keeping it simple
	return fmt.Errorf("SaveJSON not fully implemented - use ToJSONPretty() and write manually")
}

// LoadJSON reads a dataset from a JSON file.
func (ds *Dataset) LoadJSON(filepath string) error {
	// Would use os.ReadFile here
	return fmt.Errorf("LoadJSON not fully implemented - use FromJSONString() with manual file read")
}

// isBinaryVR checks if a VR represents binary data.
func isBinaryVR(vr dataelem.VR) bool {
	switch vr {
	case dataelem.OB, dataelem.OW, dataelem.OD, dataelem.OF, dataelem.OL, dataelem.UN:
		return true
	default:
		return false
	}
}

// JSONExportOptions contains options for JSON export.
type JSONExportOptions struct {
	IncludePrivateTags bool // Include private tags
	IncludePixelData   bool // Include pixel data (can be large)
	IncludeSequences   bool // Include sequences
	IncludeMetadata    bool // Include metadata object
	PrettyPrint        bool // Pretty-print the JSON
}

// DefaultJSONExportOptions returns sensible default JSON export options.
func DefaultJSONExportOptions() JSONExportOptions {
	return JSONExportOptions{
		IncludePrivateTags: false,
		IncludePixelData:   false,
		IncludeSequences:   true,
		IncludeMetadata:    true,
		PrettyPrint:        false,
	}
}

// ToJSONWithOptions converts the dataset to JSON with custom options.
func (ds *Dataset) ToJSONWithOptions(opts JSONExportOptions) ([]byte, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	jsonDS := &JSONDataset{
		Elements: make(map[string]JSONElement),
	}

	if opts.IncludeMetadata {
		jsonDS.Metadata = JSONMetadata{
			ElementCount: len(ds.elements),
		}
	}

	// Convert each element with filtering
	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		t := tag.FromInt(tagVal)

		// Filter private tags
		if !opts.IncludePrivateTags && t.IsPrivate() {
			continue
		}

		// Filter pixel data
		if !opts.IncludePixelData && t == tag.New(0x7FE0, 0x0010) {
			continue
		}

		// Filter sequences
		if !opts.IncludeSequences && elem.GetVR() == dataelem.SQ {
			continue
		}

		jsonElem := ds.elementToJSON(t, elem)
		jsonDS.Elements[t.String()] = jsonElem

		// Update metadata
		if opts.IncludeMetadata {
			if elem.GetVR() == dataelem.SQ {
				jsonDS.Metadata.HasSequences = true
			}
			if t == tag.New(0x7FE0, 0x0010) {
				jsonDS.Metadata.HasPixelData = true
			}
		}
	}

	// Marshal
	if opts.PrettyPrint {
		return json.MarshalIndent(jsonDS, "", "  ")
	}
	return json.Marshal(jsonDS)
}

// CloneFromJSON creates a new dataset from JSON bytes.
func CloneFromJSON(jsonBytes []byte) (*Dataset, error) {
	ds := NewDataset()
	if err := ds.UnmarshalJSON(jsonBytes); err != nil {
		return nil, err
	}
	return ds, nil
}
