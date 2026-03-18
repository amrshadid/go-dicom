package charset_test

import (
	"context"
	"testing"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDataElementIntegration tests the integration of charset with dataelem module.
func TestDataElementIntegration(t *testing.T) {
	tests := []struct {
		name        string
		encoding    string
		text        string
		vr          dataelem.VR
		expectError bool
	}{
		{
			name:     "Default encoding (ISO-8859-1)",
			encoding: "",
			text:     "Patient Name",
			vr:       dataelem.LO,
		},
		{
			name:     "UTF-8 encoding",
			encoding: "ISO_IR 192",
			text:     "山田^太郎",
			vr:       dataelem.PN,
		},
		{
			name:     "Japanese Shift-JIS",
			encoding: "ISO_IR 13",
			text:     "患者名",
			vr:       dataelem.LO,
		},
		{
			name:     "German with umlaut",
			encoding: "ISO_IR 100",
			text:     "Müller^Hans",
			vr:       dataelem.PN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create character set
			var cs *charset.CharacterSet
			var err error
			if tt.encoding != "" {
				cs, err = charset.NewCharacterSet([]string{tt.encoding})
				if err != nil {
					t.Fatalf("Failed to create character set: %v", err)
				}
			}

			// Create data element
			elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), tt.vr, nil)

			// Test encoding
			if tt.vr == dataelem.PN {
				// PersonName
				pn := &charset.PersonName{Alphabetic: tt.text}
				err = elem.SetPersonNameValue(pn, cs)
			} else {
				// Text
				err = elem.SetTextValue(tt.text, cs)
			}

			if err != nil && !tt.expectError {
				t.Fatalf("Failed to set value: %v", err)
			}
			if err == nil && tt.expectError {
				t.Fatal("Expected error but got none")
			}
			if tt.expectError {
				return
			}

			// Test decoding
			if tt.vr == dataelem.PN {
				decoded, err := elem.GetPersonNameValue(cs)
				if err != nil {
					t.Fatalf("Failed to get PersonName value: %v", err)
				}
				if decoded.Alphabetic != tt.text {
					t.Errorf("Expected %q, got %q", tt.text, decoded.Alphabetic)
				}
			} else {
				decoded, err := elem.GetTextValue(cs)
				if err != nil {
					t.Fatalf("Failed to get text value: %v", err)
				}
				if decoded != tt.text {
					t.Errorf("Expected %q, got %q", tt.text, decoded)
				}
			}
		})
	}
}

// TestDatasetIntegration tests the integration of charset with dataset module.
func TestDatasetIntegration(t *testing.T) {
	// Create dataset
	ds := dataset.NewDataset()

	// Create character set (UTF-8)
	cs, err := charset.NewCharacterSet([]string{"ISO_IR 192"})
	if err != nil {
		t.Fatalf("Failed to create character set: %v", err)
	}

	// Set character set
	if err := ds.SetCharacterSet(cs); err != nil {
		t.Fatalf("Failed to set character set: %v", err)
	}

	// Add text elements
	patientTag := tag.New(0x0010, 0x0010)
	studyTag := tag.New(0x0008, 0x1030)

	// Use SetPersonName for PN VR
	pn := &charset.PersonName{Alphabetic: "山田^太郎"}
	if err := ds.SetPersonName(patientTag, pn); err != nil {
		t.Fatalf("Failed to set patient name: %v", err)
	}

	if err := ds.SetTextValue(studyTag, dataelem.LO, "胸部CT検査"); err != nil {
		t.Fatalf("Failed to set study description: %v", err)
	}

	// Retrieve character set
	retrievedCS := ds.GetCharacterSet()
	if retrievedCS == nil {
		t.Fatal("Failed to retrieve character set")
	}
	if len(retrievedCS.Encodings) != 1 || retrievedCS.Encodings[0] != "UTF-8" {
		t.Errorf("Expected UTF-8 encoding, got %v", retrievedCS.Encodings)
	}

	// Retrieve PersonName
	retrievedPN, err := ds.GetPersonName(patientTag)
	if err != nil {
		t.Fatalf("Failed to get patient name: %v", err)
	}
	if retrievedPN.Alphabetic != "山田^太郎" {
		t.Errorf("Expected '山田^太郎', got %q", retrievedPN.Alphabetic)
	}

	studyDesc, err := ds.GetTextValue(studyTag)
	if err != nil {
		t.Fatalf("Failed to get study description: %v", err)
	}
	if studyDesc != "胸部CT検査" {
		t.Errorf("Expected '胸部CT検査', got %q", studyDesc)
	}

	// Test GetAllTextValues
	allText := ds.GetAllTextValues()
	// Should have:
	// - StudyDescription (LO)
	// - SpecificCharacterSet (CS) - also a text VR
	// Patient name (PN) is excluded
	if len(allText) != 2 {
		t.Errorf("Expected 2 text values (StudyDescription + SpecificCharacterSet), got %d", len(allText))
		for tag, value := range allText {
			t.Logf("  %s: %s", tag.Hex(), value)
		}
	}

	// Test GetAllPersonNames
	allPN := ds.GetAllPersonNames()
	if len(allPN) != 1 {
		t.Errorf("Expected 1 PersonName, got %d", len(allPN))
	}
}

// TestRoundTripEncoding tests encoding and decoding with multiple character sets.
func TestRoundTripEncoding(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		texts    []string
	}{
		{
			name:     "UTF-8 Japanese",
			encoding: "ISO_IR 192",
			texts:    []string{"山田^太郎", "佐藤^花子", "鈴木^一郎"},
		},
		{
			name:     "Latin-1 German",
			encoding: "ISO_IR 100",
			texts:    []string{"Müller", "Schröder", "Größe"},
		},
		{
			name:     "Latin-1 French",
			encoding: "ISO_IR 100",
			texts:    []string{"Français", "Médecin", "Hôpital"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, err := charset.NewCharacterSet([]string{tt.encoding})
			if err != nil {
				t.Fatalf("Failed to create character set: %v", err)
			}

			for _, originalText := range tt.texts {
				// Encode
				encoded, err := charset.EncodeString(originalText, cs.Encodings)
				if err != nil {
					t.Fatalf("Failed to encode %q: %v", originalText, err)
				}

				// Decode
				decoded, err := charset.DecodeBytes(encoded, cs.Encodings, charset.DefaultTextDelimiters)
				if err != nil {
					t.Fatalf("Failed to decode: %v", err)
				}

				if decoded != originalText {
					t.Errorf("Round-trip failed: expected %q, got %q", originalText, decoded)
				}
			}
		})
	}
}

// TestPersonNameIntegration tests PersonName encoding/decoding integration.
func TestPersonNameIntegration(t *testing.T) {
	tests := []struct {
		name       string
		encoding   string
		familyName string
		givenName  string
		middleName string
		prefix     string
		suffix     string
	}{
		{
			name:       "English name",
			encoding:   "",
			familyName: "Doe",
			givenName:  "John",
			middleName: "Michael",
			prefix:     "Dr.",
			suffix:     "Jr.",
		},
		{
			name:       "Japanese name (UTF-8)",
			encoding:   "ISO_IR 192",
			familyName: "山田",
			givenName:  "太郎",
		},
		{
			name:       "German name with umlaut",
			encoding:   "ISO_IR 100",
			familyName: "Müller",
			givenName:  "Hans",
			prefix:     "Prof.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create character set
			var cs *charset.CharacterSet
			var err error
			if tt.encoding != "" {
				cs, err = charset.NewCharacterSet([]string{tt.encoding})
				if err != nil {
					t.Fatalf("Failed to create character set: %v", err)
				}
			}

			// Create PersonName
			pn := charset.FromComponents(tt.familyName, tt.givenName, tt.middleName, tt.prefix, tt.suffix)

			// Encode
			var encodings []string
			if cs != nil {
				encodings = cs.Encodings
			} else {
				encodings = []string{charset.DefaultEncoding}
			}

			encoded, err := charset.EncodePersonName(pn, encodings)
			if err != nil {
				t.Fatalf("Failed to encode PersonName: %v", err)
			}

			// Decode
			decoded, err := charset.DecodePersonName(encoded, encodings)
			if err != nil {
				t.Fatalf("Failed to decode PersonName: %v", err)
			}

			// Verify components
			if decoded.FamilyName() != tt.familyName {
				t.Errorf("Family name: expected %q, got %q", tt.familyName, decoded.FamilyName())
			}
			if decoded.GivenName() != tt.givenName {
				t.Errorf("Given name: expected %q, got %q", tt.givenName, decoded.GivenName())
			}
			if decoded.MiddleName() != tt.middleName {
				t.Errorf("Middle name: expected %q, got %q", tt.middleName, decoded.MiddleName())
			}
			if decoded.Prefix() != tt.prefix {
				t.Errorf("Prefix: expected %q, got %q", tt.prefix, decoded.Prefix())
			}
			if decoded.Suffix() != tt.suffix {
				t.Errorf("Suffix: expected %q, got %q", tt.suffix, decoded.Suffix())
			}
		})
	}
}

// TestContextIntegration tests context-aware methods.
func TestContextIntegration(t *testing.T) {
	ctx := context.Background()

	// Create dataset
	ds := dataset.NewDataset()

	// Create character set
	cs, err := charset.NewCharacterSetWithContext(ctx, []string{"ISO_IR 192"})
	if err != nil {
		t.Fatalf("Failed to create character set: %v", err)
	}

	// Set with context
	if err := ds.SetCharacterSetWithContext(ctx, cs); err != nil {
		t.Fatalf("Failed to set character set: %v", err)
	}

	// Add PersonName with context
	patientTag := tag.New(0x0010, 0x0010)
	pn := &charset.PersonName{Alphabetic: "山田^太郎"}
	if err := ds.SetPersonNameWithContext(ctx, patientTag, pn); err != nil {
		t.Fatalf("Failed to set patient name: %v", err)
	}

	// Get with context
	retrievedPN, err := ds.GetPersonNameWithContext(ctx, patientTag)
	if err != nil {
		t.Fatalf("Failed to get patient name: %v", err)
	}

	if retrievedPN.Alphabetic != "山田^太郎" {
		t.Errorf("Expected '山田^太郎', got %q", retrievedPN.Alphabetic)
	}
}

// TestMultiValuedCharacterSet tests multi-valued character sets (code extensions).
func TestMultiValuedCharacterSet(t *testing.T) {
	// Create multi-valued character set (ISO 2022 IR 87 = Japanese with escape sequences)
	cs, err := charset.NewCharacterSet([]string{"", "ISO 2022 IR 87"})
	if err != nil {
		t.Fatalf("Failed to create character set: %v", err)
	}

	// Create dataset
	ds := dataset.NewDataset()
	if err := ds.SetCharacterSet(cs); err != nil {
		t.Fatalf("Failed to set character set: %v", err)
	}

	// Add text (this should handle escape sequences automatically)
	testTag := tag.New(0x0008, 0x1030)
	testText := "Study Description with Japanese"

	if err := ds.SetTextValue(testTag, dataelem.LO, testText); err != nil {
		t.Fatalf("Failed to set text value: %v", err)
	}

	// Retrieve and verify
	retrieved, err := ds.GetTextValue(testTag)
	if err != nil {
		t.Fatalf("Failed to get text value: %v", err)
	}

	if retrieved != testText {
		t.Errorf("Expected %q, got %q", testText, retrieved)
	}
}

// TestEmptyAndNilValues tests handling of empty and nil values.
func TestEmptyAndNilValues(t *testing.T) {
	ds := dataset.NewDataset()

	// Test nil character set
	cs := ds.GetCharacterSet()
	if cs != nil {
		t.Error("Expected nil character set for new dataset")
	}

	// Test empty text value
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.LO, nil)
	if err := elem.SetTextValue("", nil); err != nil {
		t.Fatalf("Failed to set empty text value: %v", err)
	}

	text, err := elem.GetTextValue(nil)
	if err != nil {
		t.Fatalf("Failed to get empty text value: %v", err)
	}
	if text != "" {
		t.Errorf("Expected empty string, got %q", text)
	}

	// Test nil PersonName
	pnElem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil)
	if err := pnElem.SetPersonNameValue(nil, nil); err != nil {
		t.Fatalf("Failed to set nil PersonName: %v", err)
	}

	pn, err := pnElem.GetPersonNameValue(nil)
	if err != nil {
		t.Fatalf("Failed to get nil PersonName: %v", err)
	}
	if !pn.IsEmpty() {
		t.Error("Expected empty PersonName")
	}
}

// TestErrorHandling tests error handling in integration scenarios.
func TestErrorHandling(t *testing.T) {
	ds := dataset.NewDataset()

	// Test getting non-existent tag
	_, err := ds.GetTextValue(tag.New(0xFFFF, 0xFFFF))
	if err == nil {
		t.Error("Expected error for non-existent tag")
	}

	// Test wrong VR for GetTextValue
	pnTag := tag.New(0x0010, 0x0010)
	pnElem := dataelem.NewDataElement(pnTag, dataelem.PN, []byte("Test"))
	if err := ds.Add(pnElem); err != nil {
		t.Fatalf("Failed to add element: %v", err)
	}

	_, err = pnElem.GetTextValue(nil)
	if err == nil {
		t.Error("Expected error when using GetTextValue on PN VR")
	}

	// Test wrong VR for GetPersonNameValue
	loElem := dataelem.NewDataElement(tag.New(0x0008, 0x0008), dataelem.LO, []byte("Test"))
	_, err = loElem.GetPersonNameValue(nil)
	if err == nil {
		t.Error("Expected error when using GetPersonNameValue on non-PN VR")
	}
}
