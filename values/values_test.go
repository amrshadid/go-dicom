package values_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/values"
)

func TestConvertString(t *testing.T) {
	tests := []struct {
		vr       string
		raw      []byte
		expected string
	}{
		{"AE", []byte("Entity"), "Entity"},
		{"CS", []byte("CODE"), "CODE"},
		{"LO", []byte("Long String   "), "Long String"},
		{"PN", []byte("Smith^John"), "Smith^John"},
	}

	for _, tt := range tests {
		result, err := values.ConvertValue(tt.vr, tt.raw, true)
		if err != nil {
			t.Errorf("ConvertValue(%s): unexpected error: %v", tt.vr, err)
		}
		if str, ok := result.(string); !ok || str != tt.expected {
			t.Errorf("ConvertValue(%s): expected %s, got %v", tt.vr, tt.expected, result)
		}
	}
}

func TestConvertDecimalString(t *testing.T) {
	tests := []struct {
		raw      []byte
		expected float64
	}{
		{[]byte("123.45"), 123.45},
		{[]byte("  -123.45  "), -123.45},
		{[]byte("+1.23e-4"), 0.000123},
	}

	for _, tt := range tests {
		result, _ := values.ConvertValue("DS", tt.raw, true)
		if f, ok := result.(float64); !ok || f != tt.expected {
			t.Errorf("ConvertValue(DS, %v): expected %f, got %v", tt.raw, tt.expected, result)
		}
	}
}

func TestConvertIntegerString(t *testing.T) {
	tests := []struct {
		raw      []byte
		expected int64
	}{
		{[]byte("123"), 123},
		{[]byte("  -456  "), -456},
		{[]byte("+789"), 789},
	}

	for _, tt := range tests {
		result, _ := values.ConvertValue("IS", tt.raw, true)
		if i, ok := result.(int64); !ok || i != tt.expected {
			t.Errorf("ConvertValue(IS, %v): expected %d, got %v", tt.raw, tt.expected, result)
		}
	}
}

func TestConvertFloatingPointDouble(t *testing.T) {
	value := 123.45
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, value)

	result, err := values.ConvertValue("FD", buf.Bytes(), true)
	if err != nil {
		t.Fatalf("ConvertValue(FD): unexpected error: %v", err)
	}

	if f, ok := result.(float64); !ok || f != value {
		t.Errorf("ConvertValue(FD): expected %f, got %v", value, result)
	}
}

func TestConvertFloatingPointSingle(t *testing.T) {
	value := float32(123.45)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, value)

	result, err := values.ConvertValue("FL", buf.Bytes(), true)
	if err != nil {
		t.Fatalf("ConvertValue(FL): unexpected error: %v", err)
	}

	if f, ok := result.(float64); !ok || f != float64(value) {
		t.Errorf("ConvertValue(FL): expected %f, got %v", value, result)
	}
}

func TestConvertSignedLong(t *testing.T) {
	value := int32(-123456)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, value)

	result, err := values.ConvertValue("SL", buf.Bytes(), true)
	if err != nil {
		t.Fatalf("ConvertValue(SL): unexpected error: %v", err)
	}

	if i, ok := result.(int64); !ok || i != int64(value) {
		t.Errorf("ConvertValue(SL): expected %d, got %v", value, result)
	}
}

func TestConvertUnsignedShort(t *testing.T) {
	value := uint16(12345)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, value)

	result, err := values.ConvertValue("US", buf.Bytes(), true)
	if err != nil {
		t.Fatalf("ConvertValue(US): unexpected error: %v", err)
	}

	if u, ok := result.(uint64); !ok || u != uint64(value) {
		t.Errorf("ConvertValue(US): expected %d, got %v", value, result)
	}
}

func TestConvertBinary(t *testing.T) {
	raw := []byte{0x00, 0x01, 0x02, 0x03}

	tests := []string{"OB", "OW", "UN"}
	for _, vr := range tests {
		result, err := values.ConvertValue(vr, raw, true)
		if err != nil {
			t.Errorf("ConvertValue(%s): unexpected error: %v", vr, err)
		}
		if !bytes.Equal(result.([]byte), raw) {
			t.Errorf("ConvertValue(%s): expected %v, got %v", vr, raw, result)
		}
	}
}

func TestMultiString(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"Single", 1},
		{"First\\Second", 2},
		{"A\\B\\C", 3},
	}

	for _, tt := range tests {
		result := values.MultiString(tt.input, nil)
		if len(result) != tt.expected {
			t.Errorf("MultiString(%s): expected %d values, got %d", tt.input, tt.expected, len(result))
		}
	}
}

func TestMultiStringInt(t *testing.T) {
	tests := []struct {
		input    string
		expected []int64
	}{
		{"", []int64{}},
		{"123", []int64{123}},
		{"123\\456\\789", []int64{123, 456, 789}},
		{"123\\invalid\\456", []int64{123, 456}},
	}

	for _, tt := range tests {
		result := values.MultiStringInt(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("MultiStringInt(%s): expected %d values, got %d", tt.input, len(tt.expected), len(result))
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("MultiStringInt(%s)[%d]: expected %d, got %d", tt.input, i, tt.expected[i], v)
			}
		}
	}
}

func TestMultiStringFloat(t *testing.T) {
	tests := []struct {
		input    string
		expected []float64
	}{
		{"", []float64{}},
		{"123.45", []float64{123.45}},
		{"123.45\\678.90\\-12.34", []float64{123.45, 678.90, -12.34}},
	}

	for _, tt := range tests {
		result := values.MultiStringFloat(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("MultiStringFloat(%s): expected %d values, got %d", tt.input, len(tt.expected), len(result))
		}
	}
}

func TestConvertTag(t *testing.T) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(0x0010))
	binary.Write(buf, binary.LittleEndian, uint16(0x0010))

	group, element, err := values.ConvertTag(buf.Bytes(), true)
	if err != nil {
		t.Fatalf("ConvertTag: unexpected error: %v", err)
	}

	if group != 0x0010 || element != 0x0010 {
		t.Errorf("ConvertTag: expected (0x0010, 0x0010), got (0x%04x, 0x%04x)", group, element)
	}
}

func TestEncodeStringValue(t *testing.T) {
	tests := []struct {
		vr    string
		value string
	}{
		{"AE", "ApplicationEntity"},
		{"PN", "Smith^John"},
		{"LO", "LongString"},
	}

	for _, tt := range tests {
		result, err := values.EncodeValue(tt.vr, tt.value, true)
		if err != nil {
			t.Errorf("EncodeValue(%s): unexpected error: %v", tt.vr, err)
		}
		if string(result) != tt.value {
			t.Errorf("EncodeValue(%s): expected %s, got %s", tt.vr, tt.value, string(result))
		}
	}
}

func TestEncodeNumericValue(t *testing.T) {
	tests := []struct {
		vr    string
		value interface{}
	}{
		{"DS", 123.45},
		{"IS", int64(789)},
		{"FD", 123.456789},
		{"FL", 12.34},
	}

	for _, tt := range tests {
		result, err := values.EncodeValue(tt.vr, tt.value, true)
		if err != nil {
			t.Errorf("EncodeValue(%s): unexpected error: %v", tt.vr, err)
		}
		if len(result) == 0 {
			t.Errorf("EncodeValue(%s): got empty result", tt.vr)
		}
	}
}

func TestEncodeBinaryValue(t *testing.T) {
	raw := []byte{0x00, 0x01, 0x02, 0x03}

	result, err := values.EncodeValue("OB", raw, true)
	if err != nil {
		t.Fatalf("EncodeValue(OB): unexpected error: %v", err)
	}

	if !bytes.Equal(result, raw) {
		t.Errorf("EncodeValue(OB): expected %v, got %v", raw, result)
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello", "Hello"},
		{"Hello   ", "Hello"},
		{"Hello\x00World", "HelloWorld"},
		{"\x00\x00Hello\x00\x00", "Hello"},
	}

	for _, tt := range tests {
		result := values.SanitizeString(tt.input)
		if result != tt.expected {
			t.Errorf("SanitizeString(%q): expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}

func TestPadString(t *testing.T) {
	tests := []struct {
		input    string
		length   int
		expected string
	}{
		{"Hello", 10, "Hello     "},
		{"Hello", 5, "Hello"},
		{"Hello", 3, "Hel"},
	}

	for _, tt := range tests {
		result := values.PadString(tt.input, tt.length)
		if result != tt.expected {
			t.Errorf("PadString(%q, %d): expected %q, got %q", tt.input, tt.length, tt.expected, result)
		}
	}
}

func TestValidateNumericString(t *testing.T) {
	tests := []struct {
		vr        string
		value     string
		shouldErr bool
	}{
		{"DS", "123.45", false},
		{"DS", "-123.45", false},
		{"DS", "not_a_number", true},
		{"IS", "123", false},
		{"IS", "-456", false},
		{"IS", "12.34", true},
	}

	for _, tt := range tests {
		err := values.ValidateNumericString(tt.vr, tt.value)
		if (err != nil) != tt.shouldErr {
			t.Errorf("ValidateNumericString(%s, %s): expected error=%v, got %v", tt.vr, tt.value, tt.shouldErr, err)
		}
	}
}

func TestEmptyValue(t *testing.T) {
	result, err := values.ConvertValue("AE", []byte{}, true)
	if err != nil {
		t.Fatalf("ConvertValue with empty value: unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("ConvertValue with empty value: expected nil, got %v", result)
	}
}

func TestBigEndianConversion(t *testing.T) {
	value := uint16(12345)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, value)

	result, err := values.ConvertValue("US", buf.Bytes(), false)
	if err != nil {
		t.Fatalf("ConvertValue(US, BigEndian): unexpected error: %v", err)
	}

	if u, ok := result.(uint64); !ok || u != uint64(value) {
		t.Errorf("ConvertValue(US, BigEndian): expected %d, got %v", value, result)
	}
}

func TestConvertValueErrors(t *testing.T) {
	tests := []struct {
		vr    string
		raw   []byte
		valid bool
	}{
		{"FD", []byte{1, 2}, false},
		{"FL", []byte{1, 2}, false},
		{"US", []byte{1}, false},
	}

	for _, tt := range tests {
		_, err := values.ConvertValue(tt.vr, tt.raw, true)
		if (err != nil) != !tt.valid {
			t.Errorf("ConvertValue(%s): expected error=%v, got %v", tt.vr, !tt.valid, err)
		}
	}
}

func TestRawDataElement(t *testing.T) {
	vr := "PN"
	raw := values.RawDataElement{
		Tag:   "0010,0010",
		VR:    &vr,
		Value: []byte("Smith^John"),
	}

	if raw.Tag != "0010,0010" {
		t.Errorf("RawDataElement: expected tag 0010,0010, got %s", raw.Tag)
	}
	if *raw.VR != "PN" {
		t.Errorf("RawDataElement: expected VR PN, got %s", *raw.VR)
	}
}
