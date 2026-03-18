package dataelem_test

import (
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// TestConvertIS tests Integer String conversion.
func TestConvertIS(t *testing.T) {
	testTag := tag.New(0x0028, 0x0010) // Rows

	t.Run("SingleValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.IS, 3, []byte("512"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		val, ok := de.GetValue().(int)
		if !ok {
			t.Fatalf("Value is not int, got %T", de.GetValue())
		}

		if val != 512 {
			t.Errorf("Value = %d, want 512", val)
		}

		if de.GetVM() != 1 {
			t.Errorf("VM = %d, want 1", de.GetVM())
		}
	})

	t.Run("MultiValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.IS, 11, []byte("123\\456\\789"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		vals, ok := de.GetValue().([]int)
		if !ok {
			t.Fatalf("Value is not []int, got %T", de.GetValue())
		}

		if len(vals) != 3 {
			t.Fatalf("Got %d values, want 3", len(vals))
		}

		if vals[0] != 123 || vals[1] != 456 || vals[2] != 789 {
			t.Errorf("Values = %v, want [123 456 789]", vals)
		}

		if de.GetVM() != 3 {
			t.Errorf("VM = %d, want 3", de.GetVM())
		}
	})

	t.Run("EmptyValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.IS, 0, []byte{}, 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		if de.GetValue() != nil {
			t.Errorf("Empty IS should have nil value, got %v", de.GetValue())
		}

		if de.GetVM() != 0 {
			t.Errorf("VM = %d, want 0", de.GetVM())
		}
	})
}

// TestConvertDS tests Decimal String conversion.
func TestConvertDS(t *testing.T) {
	testTag := tag.New(0x0028, 0x0030) // Pixel Spacing

	t.Run("SingleValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.DS, 4, []byte("1.5"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		val, ok := de.GetValue().(float64)
		if !ok {
			t.Fatalf("Value is not float64, got %T", de.GetValue())
		}

		if val != 1.5 {
			t.Errorf("Value = %f, want 1.5", val)
		}
	})

	t.Run("MultiValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.DS, 7, []byte("1.5\\2.5"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		vals, ok := de.GetValue().([]float64)
		if !ok {
			t.Fatalf("Value is not []float64, got %T", de.GetValue())
		}

		if len(vals) != 2 {
			t.Fatalf("Got %d values, want 2", len(vals))
		}

		if vals[0] != 1.5 || vals[1] != 2.5 {
			t.Errorf("Values = %v, want [1.5 2.5]", vals)
		}
	})
}

// TestConvertPN tests PersonName conversion.
func TestConvertPN(t *testing.T) {
	testTag := tag.New(0x0010, 0x0010) // Patient Name

	t.Run("SingleValueAlphabetic", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.PN, 8, []byte("Doe^John"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		pn, ok := de.GetValue().(dataelem.PersonName)
		if !ok {
			t.Fatalf("Value is not PersonName, got %T", de.GetValue())
		}

		if pn.Alphabetic != "Doe^John" {
			t.Errorf("Alphabetic = %s, want Doe^John", pn.Alphabetic)
		}
	})

	t.Run("WithComponents", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.PN, 20, []byte("Doe^John=山田^太郎=Yamada^Taro"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
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

		if pn.Phonetic != "Yamada^Taro" {
			t.Errorf("Phonetic = %s, want Yamada^Taro", pn.Phonetic)
		}
	})

	t.Run("MultiValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.PN, 17, []byte("Doe^John\\Smith^Jane"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		pns, ok := de.GetValue().([]dataelem.PersonName)
		if !ok {
			t.Fatalf("Value is not []PersonName, got %T", de.GetValue())
		}

		if len(pns) != 2 {
			t.Fatalf("Got %d values, want 2", len(pns))
		}

		if pns[0].Alphabetic != "Doe^John" {
			t.Errorf("pns[0].Alphabetic = %s, want Doe^John", pns[0].Alphabetic)
		}

		if pns[1].Alphabetic != "Smith^Jane" {
			t.Errorf("pns[1].Alphabetic = %s, want Smith^Jane", pns[1].Alphabetic)
		}
	})
}

// TestConvertAT tests Attribute Tag conversion.
func TestConvertAT(t *testing.T) {
	testTag := tag.New(0x0020, 0x9165) // Dimension Index Pointer

	t.Run("SingleTag", func(t *testing.T) {
		// Create raw bytes for tag (0x0028, 0x0010) - little endian
		value := make([]byte, 4)
		binary.LittleEndian.PutUint16(value[0:2], 0x0028)
		binary.LittleEndian.PutUint16(value[2:4], 0x0010)

		raw := dataelem.NewRawDataElement(testTag, dataelem.AT, 4, value, 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		tagVal, ok := de.GetValue().(tag.Tag)
		if !ok {
			t.Fatalf("Value is not tag.Tag, got %T", de.GetValue())
		}

		expectedTag := tag.New(0x0028, 0x0010)
		if tagVal != expectedTag {
			t.Errorf("Tag = %v, want %v", tagVal, expectedTag)
		}
	})

	t.Run("MultipleTagsLittleEndian", func(t *testing.T) {
		// Create raw bytes for two tags - little endian
		value := make([]byte, 8)
		binary.LittleEndian.PutUint16(value[0:2], 0x0028)
		binary.LittleEndian.PutUint16(value[2:4], 0x0010)
		binary.LittleEndian.PutUint16(value[4:6], 0x0028)
		binary.LittleEndian.PutUint16(value[6:8], 0x0011)

		raw := dataelem.NewRawDataElement(testTag, dataelem.AT, 8, value, 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		tags, ok := de.GetValue().([]tag.Tag)
		if !ok {
			t.Fatalf("Value is not []tag.Tag, got %T", de.GetValue())
		}

		if len(tags) != 2 {
			t.Fatalf("Got %d tags, want 2", len(tags))
		}

		expected1 := tag.New(0x0028, 0x0010)
		expected2 := tag.New(0x0028, 0x0011)

		if tags[0] != expected1 {
			t.Errorf("tags[0] = %v, want %v", tags[0], expected1)
		}

		if tags[1] != expected2 {
			t.Errorf("tags[1] = %v, want %v", tags[1], expected2)
		}
	})

	t.Run("BigEndian", func(t *testing.T) {
		// Create raw bytes for tag (0x0028, 0x0010) - big endian
		value := make([]byte, 4)
		binary.BigEndian.PutUint16(value[0:2], 0x0028)
		binary.BigEndian.PutUint16(value[2:4], 0x0010)

		raw := dataelem.NewRawDataElement(testTag, dataelem.AT, 4, value, 0, false, false, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		tagVal, ok := de.GetValue().(tag.Tag)
		if !ok {
			t.Fatalf("Value is not tag.Tag, got %T", de.GetValue())
		}

		expectedTag := tag.New(0x0028, 0x0010)
		if tagVal != expectedTag {
			t.Errorf("Tag = %v, want %v", tagVal, expectedTag)
		}
	})
}

// TestConvertTextVR tests text VR conversion with backslash splitting.
func TestConvertTextVR(t *testing.T) {
	testTag := tag.New(0x0008, 0x0016) // SOP Class UID

	t.Run("SingleValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.UI, 26, []byte("1.2.840.10008.5.1.4.1.1.2"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		val, ok := de.GetValue().(string)
		if !ok {
			t.Fatalf("Value is not string, got %T", de.GetValue())
		}

		if val != "1.2.840.10008.5.1.4.1.1.2" {
			t.Errorf("Value = %s, want 1.2.840.10008.5.1.4.1.1.2", val)
		}
	})

	t.Run("MultiValue", func(t *testing.T) {
		raw := dataelem.NewRawDataElement(testTag, dataelem.CS, 11, []byte("AXIAL\\SAGITTAL\\CORONAL"), 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		vals, ok := de.GetValue().([]string)
		if !ok {
			t.Fatalf("Value is not []string, got %T", de.GetValue())
		}

		if len(vals) != 3 {
			t.Fatalf("Got %d values, want 3", len(vals))
		}

		if vals[0] != "AXIAL" || vals[1] != "SAGITTAL" || vals[2] != "CORONAL" {
			t.Errorf("Values = %v, want [AXIAL SAGITTAL CORONAL]", vals)
		}
	})
}

// TestConvertBinaryNumeric tests binary numeric VR conversion.
func TestConvertBinaryNumeric(t *testing.T) {
	testTag := tag.New(0x0028, 0x0100) // Bits Allocated

	t.Run("US_SingleValue", func(t *testing.T) {
		value := make([]byte, 2)
		binary.LittleEndian.PutUint16(value, 16)

		raw := dataelem.NewRawDataElement(testTag, dataelem.US, 2, value, 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		val, ok := de.GetValue().(int)
		if !ok {
			t.Fatalf("Value is not int, got %T", de.GetValue())
		}

		if val != 16 {
			t.Errorf("Value = %d, want 16", val)
		}
	})

	t.Run("FD_SingleValue", func(t *testing.T) {
		value := make([]byte, 8)
		binary.LittleEndian.PutUint64(value, 0x3FF8000000000000) // 1.5 in IEEE 754

		raw := dataelem.NewRawDataElement(testTag, dataelem.FD, 8, value, 0, false, true, false)
		de, err := dataelem.ConvertRawDataElement(raw)
		if err != nil {
			t.Fatalf("ConvertRawDataElement failed: %v", err)
		}

		val, ok := de.GetValue().(float64)
		if !ok {
			t.Fatalf("Value is not float64, got %T", de.GetValue())
		}

		if val != 1.5 {
			t.Errorf("Value = %f, want 1.5", val)
		}
	})
}

// TestConvertBinaryVR tests binary VR handling.
func TestConvertBinaryVR(t *testing.T) {
	testTag := tag.New(0x7FE0, 0x0010) // Pixel Data

	raw := dataelem.NewRawDataElement(testTag, dataelem.OB, 4, []byte{0x01, 0x02, 0x03, 0x04}, 0, false, true, false)
	de, err := dataelem.ConvertRawDataElement(raw)
	if err != nil {
		t.Fatalf("ConvertRawDataElement failed: %v", err)
	}

	bytes, ok := de.GetValue().([]byte)
	if !ok {
		t.Fatalf("Value is not []byte, got %T", de.GetValue())
	}

	if len(bytes) != 4 {
		t.Fatalf("Got %d bytes, want 4", len(bytes))
	}

	for i, expected := range []byte{0x01, 0x02, 0x03, 0x04} {
		if bytes[i] != expected {
			t.Errorf("bytes[%d] = 0x%02X, want 0x%02X", i, bytes[i], expected)
		}
	}
}
