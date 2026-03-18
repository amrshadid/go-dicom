package element_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/element"
	"github.com/amrshadid/go-dicom/filebase"
)

// TestNewValueEncoder tests creating a value encoder.
func TestNewValueEncoder(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)
	if enc == nil {
		t.Fatal("NewValueEncoder returned nil")
	}
}

// TestEncodeString tests encoding strings.
func TestEncodeString(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)
	result := enc.EncodeString("Hello")
	if !bytes.Equal(result, []byte("Hello")) {
		t.Errorf("EncodeString() = %v, want %v", result, []byte("Hello"))
	}
}

// TestEncodeUint16 tests encoding uint16 values.
func TestEncodeUint16(t *testing.T) {
	tests := []struct {
		name      string
		byteOrder filebase.ByteOrder
		value     uint16
		expected  []byte
	}{
		{"Little Endian", filebase.LittleEndian, 0x1234, []byte{0x34, 0x12}},
		{"Big Endian", filebase.BigEndian, 0x1234, []byte{0x12, 0x34}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := element.NewValueEncoder(tt.byteOrder)
			got := enc.EncodeUint16(tt.value)
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("EncodeUint16() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestEncodeUint32 tests encoding uint32 values.
func TestEncodeUint32(t *testing.T) {
	tests := []struct {
		name      string
		byteOrder filebase.ByteOrder
		value     uint32
		expected  []byte
	}{
		{"Little Endian", filebase.LittleEndian, 0x12345678, []byte{0x78, 0x56, 0x34, 0x12}},
		{"Big Endian", filebase.BigEndian, 0x12345678, []byte{0x12, 0x34, 0x56, 0x78}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := element.NewValueEncoder(tt.byteOrder)
			got := enc.EncodeUint32(tt.value)
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("EncodeUint32() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestEncodeInt16 tests encoding int16 values.
func TestEncodeInt16(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)
	result := enc.EncodeInt16(-1)
	expected := enc.EncodeUint16(65535) // -1 as unsigned
	if !bytes.Equal(result, expected) {
		t.Errorf("EncodeInt16() failed")
	}
}

// TestEncodeMultipleValues tests encoding multiple values.
func TestEncodeMultipleValues(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)
	result := enc.EncodeMultipleValues([]string{"A", "B", "C"})
	expected := []byte("A\\B\\C")
	if !bytes.Equal(result, expected) {
		t.Errorf("EncodeMultipleValues() = %v, want %v", result, expected)
	}
}

// TestDecodeString tests decoding strings.
func TestDecodeString(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)
	result := enc.DecodeString([]byte("Hello   "))
	if result != "Hello" {
		t.Errorf("DecodeString() = %q, want %q", result, "Hello")
	}
}

// TestDecodeUint16 tests decoding uint16 values.
func TestDecodeUint16(t *testing.T) {
	tests := []struct {
		name      string
		byteOrder filebase.ByteOrder
		data      []byte
		expected  uint16
	}{
		{"Little Endian", filebase.LittleEndian, []byte{0x34, 0x12}, 0x1234},
		{"Big Endian", filebase.BigEndian, []byte{0x12, 0x34}, 0x1234},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := element.NewValueEncoder(tt.byteOrder)
			got, err := enc.DecodeUint16(tt.data)
			if err != nil {
				t.Fatalf("DecodeUint16() error = %v", err)
			}
			if got != tt.expected {
				t.Errorf("DecodeUint16() = 0x%04x, want 0x%04x", got, tt.expected)
			}
		})
	}
}

// TestDecodeUint32 tests decoding uint32 values.
func TestDecodeUint32(t *testing.T) {
	tests := []struct {
		name      string
		byteOrder filebase.ByteOrder
		data      []byte
		expected  uint32
	}{
		{"Little Endian", filebase.LittleEndian, []byte{0x78, 0x56, 0x34, 0x12}, 0x12345678},
		{"Big Endian", filebase.BigEndian, []byte{0x12, 0x34, 0x56, 0x78}, 0x12345678},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := element.NewValueEncoder(tt.byteOrder)
			got, err := enc.DecodeUint32(tt.data)
			if err != nil {
				t.Fatalf("DecodeUint32() error = %v", err)
			}
			if got != tt.expected {
				t.Errorf("DecodeUint32() = 0x%08x, want 0x%08x", got, tt.expected)
			}
		})
	}
}

// TestDecodeUint16Short tests decoding with insufficient data.
func TestDecodeUint16Short(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)
	_, err := enc.DecodeUint16([]byte{0x01})
	if err == nil {
		t.Error("DecodeUint16() should error with short data")
	}
}

// TestDecodeMultipleValues tests decoding multiple values.
func TestDecodeMultipleValues(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)
	result := enc.DecodeMultipleValues([]byte("A\\B\\C"))
	expected := []string{"A", "B", "C"}
	if len(result) != len(expected) {
		t.Errorf("DecodeMultipleValues() length = %d, want %d", len(result), len(expected))
	}
}

// TestNewValueParser tests creating a value parser.
func TestNewValueParser(t *testing.T) {
	vp := element.NewValueParser()
	if vp == nil {
		t.Fatal("NewValueParser returned nil")
	}
}

// TestParseIntegerString tests parsing integer strings.
func TestParseIntegerString(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected int64
		wantErr  bool
	}{
		{"Positive", "123", 123, false},
		{"Negative", "-456", -456, false},
		{"Zero", "0", 0, false},
		{"Invalid", "abc", 0, true},
	}

	vp := element.NewValueParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vp.ParseIntegerString(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIntegerString() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseIntegerString() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// TestParseDecimalString tests parsing decimal strings.
func TestParseDecimalString(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected float64
		wantErr  bool
	}{
		{"Decimal", "123.456", 123.456, false},
		{"Integer", "123", 123.0, false},
		{"Negative", "-45.6", -45.6, false},
		{"Invalid", "abc", 0, true},
	}

	vp := element.NewValueParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vp.ParseDecimalString(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDecimalString() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseDecimalString() = %f, want %f", got, tt.expected)
			}
		})
	}
}

// TestParseDate tests parsing dates.
func TestParseDate(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
		wantErr  bool
	}{
		{"Valid", "20231225", "20231225", false},
		{"Short", "202312", "", true},
		{"Non-digits", "2023abcd", "", true},
	}

	vp := element.NewValueParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vp.ParseDate(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseDate() = %s, want %s", got, tt.expected)
			}
		})
	}
}

// TestParsePersonName tests parsing person names.
func TestParsePersonName(t *testing.T) {
	vp := element.NewValueParser()
	result := vp.ParsePersonName("Doe^John^A^Dr^Jr")

	if result["FamilyName"] != "Doe" {
		t.Errorf("FamilyName = %s, want Doe", result["FamilyName"])
	}
	if result["GivenName"] != "John" {
		t.Errorf("GivenName = %s, want John", result["GivenName"])
	}
	if result["MiddleName"] != "A" {
		t.Errorf("MiddleName = %s, want A", result["MiddleName"])
	}
	if result["NamePrefix"] != "Dr" {
		t.Errorf("NamePrefix = %s, want Dr", result["NamePrefix"])
	}
	if result["NameSuffix"] != "Jr" {
		t.Errorf("NameSuffix = %s, want Jr", result["NameSuffix"])
	}
}

// TestNewValuePadder tests creating a value padder.
func TestNewValuePadder(t *testing.T) {
	vp := element.NewValuePadder()
	if vp == nil {
		t.Fatal("NewValuePadder returned nil")
	}
}

// TestGetPadByte tests getting pad byte for different VRs.
func TestGetPadByte(t *testing.T) {
	tests := []struct {
		vr       dataelem.VR
		expected byte
	}{
		{dataelem.AE, 0x20},
		{dataelem.OB, 0x00},
		{dataelem.OW, 0x20},
		{dataelem.PN, 0x20},
	}

	padder := element.NewValuePadder()
	for _, tt := range tests {
		if padder.GetPadByte(tt.vr) != tt.expected {
			t.Errorf("GetPadByte(%s) = 0x%02x, want 0x%02x", tt.vr, padder.GetPadByte(tt.vr), tt.expected)
		}
	}
}

// TestPad tests padding values.
func TestPad(t *testing.T) {
	padder := element.NewValuePadder()
	result := padder.Pad([]byte{0x01}, dataelem.AE)
	if len(result) != 2 {
		t.Errorf("Pad() length = %d, want 2", len(result))
	}
	if result[1] != 0x20 {
		t.Errorf("Pad() second byte = 0x%02x, want 0x20", result[1])
	}
}

// TestUnpad tests removing padding.
func TestUnpad(t *testing.T) {
	padder := element.NewValuePadder()
	result := padder.Unpad([]byte{0x01, 0x20}, dataelem.AE)
	if len(result) != 1 {
		t.Errorf("Unpad() length = %d, want 1", len(result))
	}
}

// TestValueMultiplicity tests calculating value multiplicity.
func TestValueMultiplicity(t *testing.T) {
	tests := []struct {
		value    []byte
		vr       dataelem.VR
		expected int
	}{
		{[]byte("A\\B\\C"), dataelem.AE, 3},
		{[]byte("SingleValue"), dataelem.AE, 1},
		{[]byte{}, dataelem.AE, 0},
	}

	padder := element.NewValuePadder()
	for _, tt := range tests {
		got := padder.ValueMultiplicity(tt.value, tt.vr)
		if got != tt.expected {
			t.Errorf("ValueMultiplicity() = %d, want %d", got, tt.expected)
		}
	}
}

// TestNewValueConverter tests creating a value converter.
func TestNewValueConverter(t *testing.T) {
	vc := element.NewValueConverter(filebase.LittleEndian)
	if vc == nil {
		t.Fatal("NewValueConverter returned nil")
	}

	if vc.GetEncoder() == nil {
		t.Error("GetEncoder() returned nil")
	}
	if vc.GetParser() == nil {
		t.Error("GetParser() returned nil")
	}
	if vc.GetPadder() == nil {
		t.Error("GetPadder() returned nil")
	}
}

// TestConvertToString tests converting values to strings.
func TestConvertToString(t *testing.T) {
	vc := element.NewValueConverter(filebase.LittleEndian)
	result, err := vc.ConvertToString([]byte("Hello"), dataelem.LO)
	if err != nil {
		t.Fatalf("ConvertToString() error = %v", err)
	}
	if result != "Hello" {
		t.Errorf("ConvertToString() = %q, want %q", result, "Hello")
	}

	// String input
	result, err = vc.ConvertToString("Direct", dataelem.LO)
	if err != nil || result != "Direct" {
		t.Errorf("ConvertToString() with string failed")
	}

	// Integer input
	result, err = vc.ConvertToString(123, dataelem.IS)
	if err != nil || result != "123" {
		t.Errorf("ConvertToString() with int failed")
	}
}

// TestConvertToBytes tests converting values to bytes.
func TestConvertToBytes(t *testing.T) {
	vc := element.NewValueConverter(filebase.LittleEndian)
	result, err := vc.ConvertToBytes("Hello", dataelem.LO)
	if err != nil {
		t.Fatalf("ConvertToBytes() error = %v", err)
	}
	if !bytes.Equal(result, []byte("Hello")) {
		t.Errorf("ConvertToBytes() failed")
	}

	// Bytes input
	result, err = vc.ConvertToBytes([]byte("Direct"), dataelem.OB)
	if err != nil || !bytes.Equal(result, []byte("Direct")) {
		t.Errorf("ConvertToBytes() with bytes failed")
	}

	// Integer input
	result, err = vc.ConvertToBytes(int32(100), dataelem.IS)
	if err != nil || len(result) == 0 {
		t.Errorf("ConvertToBytes() with int32 failed")
	}
}

// TestValidateLength tests validating value lengths.
func TestValidateLength(t *testing.T) {
	tests := []struct {
		name    string
		value   []byte
		vr      dataelem.VR
		wantErr bool
	}{
		{"Even length LO", []byte("Hell"), dataelem.LO, false},
		{"Odd length LO", []byte("Hello"), dataelem.LO, true},
		{"Even length OB", []byte("Hell"), dataelem.OB, false},
		{"Odd length OB", []byte("Hello"), dataelem.OB, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := element.ValidateLength(tt.value, tt.vr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLength() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestEncodeDecodeRoundTrip tests encoding then decoding.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)

	// Uint32
	original := uint32(0x12345678)
	encoded := enc.EncodeUint32(original)
	decoded, err := enc.DecodeUint32(encoded)
	if err != nil || decoded != original {
		t.Errorf("Uint32 roundtrip failed: %d != %d", decoded, original)
	}

	// Float32
	encFloat := element.NewValueEncoder(filebase.LittleEndian)
	origFloat := float32(3.14159)
	encBytes := encFloat.EncodeFloat32(origFloat)
	decFloat, err := encFloat.DecodeFloat32(encBytes)
	if err != nil || decFloat != origFloat {
		t.Errorf("Float32 roundtrip failed")
	}
}

// TestFloatEncoding tests float encoding.
func TestFloatEncoding(t *testing.T) {
	enc := element.NewValueEncoder(filebase.LittleEndian)

	// Encode float32
	result := enc.EncodeFloat32(1.5)
	if len(result) != 4 {
		t.Errorf("EncodeFloat32() length = %d, want 4", len(result))
	}

	// Encode float64
	result = enc.EncodeFloat64(3.14159)
	if len(result) != 8 {
		t.Errorf("EncodeFloat64() length = %d, want 8", len(result))
	}
}
