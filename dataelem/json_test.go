package dataelem_test

import (
	"encoding/json"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDataElementToJSON tests JSON serialization.
func TestDataElementToJSON(t *testing.T) {
	t.Run("IntegerString", func(t *testing.T) {
		testTag := tag.New(0x0028, 0x0010) // Rows
		de := dataelem.NewDataElement(testTag, dataelem.IS, 512)

		jsonDict, err := de.ToJSONDict()
		if err != nil {
			t.Fatalf("ToJSONDict failed: %v", err)
		}

		// Check structure
		elem, ok := jsonDict["00280010"]
		if !ok {
			t.Fatal("Tag 00280010 not found in JSON")
		}

		elemMap, ok := elem.(map[string]interface{})
		if !ok {
			t.Fatalf("Element is not a map, got %T", elem)
		}

		// Check VR
		if elemMap["vr"] != "IS" {
			t.Errorf("VR = %v, want IS", elemMap["vr"])
		}

		// Check Value
		valueArray, ok := elemMap["Value"].([]interface{})
		if !ok {
			t.Fatalf("Value is not an array, got %T", elemMap["Value"])
		}

		if len(valueArray) != 1 {
			t.Fatalf("Value array length = %d, want 1", len(valueArray))
		}

		if valueArray[0] != float64(512) {
			t.Errorf("Value[0] = %v, want 512", valueArray[0])
		}
	})

	t.Run("PersonName", func(t *testing.T) {
		testTag := tag.New(0x0010, 0x0010) // Patient Name
		pn := dataelem.PersonName{
			Alphabetic:  "Doe^John",
			Ideographic: "山田^太郎",
			Phonetic:    "Yamada^Taro",
		}
		de := dataelem.NewDataElement(testTag, dataelem.PN, pn)

		jsonDict, err := de.ToJSONDict()
		if err != nil {
			t.Fatalf("ToJSONDict failed: %v", err)
		}

		elem := jsonDict["00100010"].(map[string]interface{})
		valueArray := elem["Value"].([]interface{})
		pnMap := valueArray[0].(map[string]interface{})

		if pnMap["Alphabetic"] != "Doe^John" {
			t.Errorf("Alphabetic = %v, want Doe^John", pnMap["Alphabetic"])
		}

		if pnMap["Ideographic"] != "山田^太郎" {
			t.Errorf("Ideographic = %v, want 山田^太郎", pnMap["Ideographic"])
		}

		if pnMap["Phonetic"] != "Yamada^Taro" {
			t.Errorf("Phonetic = %v, want Yamada^Taro", pnMap["Phonetic"])
		}
	})

	t.Run("MultiValueText", func(t *testing.T) {
		testTag := tag.New(0x0008, 0x0008) // Image Type
		de := dataelem.NewDataElement(testTag, dataelem.CS, []string{"ORIGINAL", "PRIMARY", "AXIAL"})

		jsonDict, err := de.ToJSONDict()
		if err != nil {
			t.Fatalf("ToJSONDict failed: %v", err)
		}

		elem := jsonDict["00080008"].(map[string]interface{})
		valueArray := elem["Value"].([]interface{})

		if len(valueArray) != 3 {
			t.Fatalf("Value array length = %d, want 3", len(valueArray))
		}

		if valueArray[0] != "ORIGINAL" || valueArray[1] != "PRIMARY" || valueArray[2] != "AXIAL" {
			t.Errorf("Value = %v, want [ORIGINAL PRIMARY AXIAL]", valueArray)
		}
	})

	t.Run("BinaryData", func(t *testing.T) {
		testTag := tag.New(0x7FE0, 0x0010) // Pixel Data
		binaryData := []byte{0x01, 0x02, 0x03, 0x04}
		de := dataelem.NewDataElement(testTag, dataelem.OB, binaryData)

		jsonDict, err := de.ToJSONDict()
		if err != nil {
			t.Fatalf("ToJSONDict failed: %v", err)
		}

		elem := jsonDict["7FE00010"].(map[string]interface{})

		// Binary data should use InlineBinary (base64)
		inlineBinary, ok := elem["InlineBinary"]
		if !ok {
			t.Fatal("InlineBinary field not found")
		}

		if inlineBinary == "" {
			t.Error("InlineBinary should not be empty")
		}
	})
}

// TestDataElementFromJSON tests JSON deserialization.
func TestDataElementFromJSON(t *testing.T) {
	t.Run("IntegerString", func(t *testing.T) {
		jsonStr := `{"00280010": {"vr": "IS", "Value": [512]}}`

		de, err := dataelem.FromJSON(jsonStr)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		if de.GetVR() != dataelem.IS {
			t.Errorf("VR = %v, want IS", de.GetVR())
		}

		val, ok := de.GetValue().(int)
		if !ok {
			t.Fatalf("Value is not int, got %T", de.GetValue())
		}

		if val != 512 {
			t.Errorf("Value = %d, want 512", val)
		}
	})

	t.Run("PersonName", func(t *testing.T) {
		jsonStr := `{"00100010": {"vr": "PN", "Value": [{"Alphabetic": "Doe^John", "Ideographic": "山田^太郎", "Phonetic": "Yamada^Taro"}]}}`

		de, err := dataelem.FromJSON(jsonStr)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		pn, ok := de.GetValue().(dataelem.PersonName)
		if !ok {
			t.Fatalf("Value is not PersonName, got %T", de.GetValue())
		}

		if pn.Alphabetic != "Doe^John" {
			t.Errorf("Alphabetic = %s, want Doe^John", pn.Alphabetic)
		}

		if pn.Ideographic != "山田^太郎" {
			t.Errorf("Ideographic = %s, want 山田^太郎", pn.Ideographic)
		}
	})

	t.Run("MultiValueText", func(t *testing.T) {
		jsonStr := `{"00080008": {"vr": "CS", "Value": ["ORIGINAL", "PRIMARY", "AXIAL"]}}`

		de, err := dataelem.FromJSON(jsonStr)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		vals, ok := de.GetValue().([]string)
		if !ok {
			t.Fatalf("Value is not []string, got %T", de.GetValue())
		}

		if len(vals) != 3 {
			t.Fatalf("Got %d values, want 3", len(vals))
		}

		if vals[0] != "ORIGINAL" || vals[1] != "PRIMARY" || vals[2] != "AXIAL" {
			t.Errorf("Values = %v, want [ORIGINAL PRIMARY AXIAL]", vals)
		}
	})

	t.Run("BinaryData", func(t *testing.T) {
		// Base64 for [0x01, 0x02, 0x03, 0x04]
		jsonStr := `{"7FE00010": {"vr": "OB", "InlineBinary": "AQIDBA=="}}`

		de, err := dataelem.FromJSON(jsonStr)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		bytes, ok := de.GetValue().([]byte)
		if !ok {
			t.Fatalf("Value is not []byte, got %T", de.GetValue())
		}

		expected := []byte{0x01, 0x02, 0x03, 0x04}
		if len(bytes) != len(expected) {
			t.Fatalf("Got %d bytes, want %d", len(bytes), len(expected))
		}

		for i, b := range bytes {
			if b != expected[i] {
				t.Errorf("byte[%d] = 0x%02X, want 0x%02X", i, b, expected[i])
			}
		}
	})
}

// TestJSONRoundTrip tests serialization and deserialization roundtrip.
func TestJSONRoundTrip(t *testing.T) {
	t.Run("IntegerString", func(t *testing.T) {
		original := dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.IS, 512)

		// Serialize
		jsonStr, err := original.ToJSON()
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}

		// Deserialize
		restored, err := dataelem.FromJSON(jsonStr)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		// Compare
		if restored.GetVR() != original.GetVR() {
			t.Errorf("VR mismatch: got %v, want %v", restored.GetVR(), original.GetVR())
		}

		if restored.GetValue() != original.GetValue() {
			t.Errorf("Value mismatch: got %v, want %v", restored.GetValue(), original.GetValue())
		}
	})

	t.Run("MultiValueFloat", func(t *testing.T) {
		original := dataelem.NewDataElement(tag.New(0x0028, 0x0030), dataelem.DS, []float64{1.5, 2.5})

		jsonStr, err := original.ToJSON()
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}

		restored, err := dataelem.FromJSON(jsonStr)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		origVals := original.GetValue().([]float64)
		restVals, ok := restored.GetValue().([]float64)
		if !ok {
			t.Fatalf("Restored value is not []float64, got %T", restored.GetValue())
		}

		if len(restVals) != len(origVals) {
			t.Fatalf("Value count mismatch: got %d, want %d", len(restVals), len(origVals))
		}

		for i := range origVals {
			if restVals[i] != origVals[i] {
				t.Errorf("Value[%d] mismatch: got %f, want %f", i, restVals[i], origVals[i])
			}
		}
	})
}

// TestMarshalJSON tests the MarshalJSON interface implementation.
func TestMarshalJSON(t *testing.T) {
	de := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, dataelem.PersonName{
		Alphabetic: "Doe^John",
	})

	jsonBytes, err := json.Marshal(de)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Parse to verify structure
	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if _, ok := result["00100010"]; !ok {
		t.Error("Expected tag 00100010 in JSON output")
	}
}

// TestUnmarshalJSON tests the UnmarshalJSON interface implementation.
func TestUnmarshalJSON(t *testing.T) {
	jsonBytes := []byte(`{"00100010": {"vr": "PN", "Value": [{"Alphabetic": "Doe^John"}]}}`)

	var de dataelem.DataElement
	if err := json.Unmarshal(jsonBytes, &de); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if de.GetVR() != dataelem.PN {
		t.Errorf("VR = %v, want PN", de.GetVR())
	}

	pn, ok := de.GetValue().(dataelem.PersonName)
	if !ok {
		t.Fatalf("Value is not PersonName, got %T", de.GetValue())
	}

	if pn.Alphabetic != "Doe^John" {
		t.Errorf("Alphabetic = %s, want Doe^John", pn.Alphabetic)
	}
}
