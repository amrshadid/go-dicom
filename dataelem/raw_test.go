package dataelem_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// TestNewRawDataElement tests creating a RawDataElement.
func TestNewRawDataElement(t *testing.T) {
	testTag := tag.New(0x0010, 0x0010) // Patient Name
	vr := dataelem.PN
	length := uint32(16)
	value := []byte("Doe^John")
	valueOffset := int64(1000)

	raw := dataelem.NewRawDataElement(
		testTag,
		vr,
		length,
		value,
		valueOffset,
		false, // not implicit VR
		true,  // little endian
		false, // not undefined length
	)

	if raw == nil {
		t.Fatal("NewRawDataElement returned nil")
	}

	if raw.Tag() != testTag {
		t.Errorf("Tag() = %v, want %v", raw.Tag(), testTag)
	}

	if raw.VR() != vr {
		t.Errorf("VR() = %v, want %v", raw.VR(), vr)
	}

	if raw.Length() != length {
		t.Errorf("Length() = %d, want %d", raw.Length(), length)
	}

	if raw.ValueOffset() != valueOffset {
		t.Errorf("ValueOffset() = %d, want %d", raw.ValueOffset(), valueOffset)
	}

	if raw.IsImplicitVR() != false {
		t.Error("IsImplicitVR() should be false")
	}

	if raw.IsLittleEndian() != true {
		t.Error("IsLittleEndian() should be true")
	}

	if raw.IsUndefinedLength() != false {
		t.Error("IsUndefinedLength() should be false")
	}
}

// TestRawDataElementImmutability tests that RawDataElement is immutable.
func TestRawDataElementImmutability(t *testing.T) {
	testTag := tag.New(0x0010, 0x0010)
	value := []byte("Original")

	raw := dataelem.NewRawDataElement(
		testTag,
		dataelem.PN,
		8,
		value,
		0,
		false,
		true,
		false,
	)

	// Modify original value
	value[0] = 'X'

	// Get value from raw element
	rawValue := raw.Value()

	// Should not be modified
	if rawValue[0] != 'O' {
		t.Error("RawDataElement value was modified (not immutable)")
	}

	// Modify returned value
	rawValue[0] = 'Y'

	// Get value again
	rawValue2 := raw.Value()

	// Should still be original
	if rawValue2[0] != 'O' {
		t.Error("RawDataElement value was modified via returned slice (not immutable)")
	}
}

// TestRawDataElementString tests the String() method.
func TestRawDataElementString(t *testing.T) {
	testTag := tag.New(0x0010, 0x0010)

	raw := dataelem.NewRawDataElement(
		testTag,
		dataelem.PN,
		8,
		[]byte("Doe^John"),
		1000,
		false,
		true,
		false,
	)

	str := raw.String()

	if str == "" {
		t.Error("String() returned empty string")
	}

	// Should contain key information
	if !contains(str, "tag=") {
		t.Error("String() should contain tag information")
	}

	if !contains(str, "VR=") {
		t.Error("String() should contain VR information")
	}
}

// TestRawDataElementEquals tests equality comparison.
func TestRawDataElementEquals(t *testing.T) {
	testTag := tag.New(0x0010, 0x0010)
	value := []byte("Doe^John")

	raw1 := dataelem.NewRawDataElement(testTag, dataelem.PN, 8, value, 1000, false, true, false)
	raw2 := dataelem.NewRawDataElement(testTag, dataelem.PN, 8, value, 1000, false, true, false)

	if !raw1.Equals(raw2) {
		t.Error("Identical RawDataElements should be equal")
	}

	// Different tag
	raw3 := dataelem.NewRawDataElement(tag.New(0x0010, 0x0020), dataelem.PN, 8, value, 1000, false, true, false)
	if raw1.Equals(raw3) {
		t.Error("RawDataElements with different tags should not be equal")
	}

	// Different VR
	raw4 := dataelem.NewRawDataElement(testTag, dataelem.LO, 8, value, 1000, false, true, false)
	if raw1.Equals(raw4) {
		t.Error("RawDataElements with different VRs should not be equal")
	}

	// Different value
	raw5 := dataelem.NewRawDataElement(testTag, dataelem.PN, 8, []byte("Smith^Jane"), 1000, false, true, false)
	if raw1.Equals(raw5) {
		t.Error("RawDataElements with different values should not be equal")
	}

	// Nil comparison
	if raw1.Equals(nil) {
		t.Error("RawDataElement should not equal nil")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
