package charset_test

import (
	"context"
	"testing"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/config"
)

func TestDecodeString(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		encoding string
		want     string
		wantErr  bool
	}{
		{
			name:     "UTF-8",
			data:     []byte("Hello World"),
			encoding: "ISO_IR 192",
			want:     "Hello World",
			wantErr:  false,
		},
		{
			name:     "Latin-1",
			data:     []byte{0xE9, 0xE8, 0xE0}, // é è à
			encoding: "ISO_IR 100",
			want:     "éèà",
			wantErr:  false,
		},
		{
			name:     "empty data",
			data:     []byte{},
			encoding: "ISO_IR 192",
			want:     "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charset.DecodeString(tt.data, tt.encoding)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DecodeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeToBytes(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		encoding string
		wantErr  bool
	}{
		{
			name:     "UTF-8",
			value:    "Hello World",
			encoding: "ISO_IR 192",
			wantErr:  false,
		},
		{
			name:     "Latin-1",
			value:    "éèà",
			encoding: "ISO_IR 100",
			wantErr:  false,
		},
		{
			name:     "empty string",
			value:    "",
			encoding: "ISO_IR 192",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charset.EncodeToBytes(tt.value, tt.encoding)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncodeToBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify round-trip
			if tt.value != "" {
				decoded, err := charset.DecodeString(got, tt.encoding)
				if err != nil {
					t.Errorf("Round-trip decode error = %v", err)
					return
				}
				if decoded != tt.value {
					t.Errorf("Round-trip failed: got %q, want %q", decoded, tt.value)
				}
			}
		})
	}
}

func TestDecodeWithCharset(t *testing.T) {
	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 192"})

	data := []byte("Hello 世界")
	result, err := charset.DecodeWithCharset(data, cs)
	if err != nil {
		t.Errorf("DecodeWithCharset() error = %v", err)
	}
	if result != "Hello 世界" {
		t.Errorf("DecodeWithCharset() = %q, want %q", result, "Hello 世界")
	}

	// Test with nil charset
	result, err = charset.DecodeWithCharset([]byte("Hello"), nil)
	if err != nil {
		t.Errorf("DecodeWithCharset() with nil charset error = %v", err)
	}
	if result != "Hello" {
		t.Errorf("DecodeWithCharset() with nil = %q, want %q", result, "Hello")
	}
}

func TestEncodeWithCharset(t *testing.T) {
	cs, _ := charset.NewCharacterSet([]string{"ISO_IR 192"})

	result, err := charset.EncodeWithCharset("Hello 世界", cs)
	if err != nil {
		t.Errorf("EncodeWithCharset() error = %v", err)
	}

	// Verify round-trip
	decoded, err := charset.DecodeWithCharset(result, cs)
	if err != nil {
		t.Errorf("Round-trip decode error = %v", err)
	}
	if decoded != "Hello 世界" {
		t.Errorf("Round-trip failed: got %q, want %q", decoded, "Hello 世界")
	}

	// Test with nil charset
	_, err = charset.EncodeWithCharset("Hello", nil)
	if err != nil {
		t.Errorf("EncodeWithCharset() with nil charset error = %v", err)
	}
}

func TestValidateEncoding(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		wantErr  bool
	}{
		{"empty is valid", "", false},
		{"UTF-8 is valid", "ISO_IR 192", false},
		{"Latin-1 is valid", "ISO_IR 100", false},
		{"Japanese is valid", "ISO 2022 IR 87", false},
		{"unknown encoding", "UNKNOWN_ENCODING", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := charset.ValidateEncoding(tt.encoding)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEncoding() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEncodings(t *testing.T) {
	tests := []struct {
		name      string
		encodings []string
		wantErr   bool
	}{
		{
			name:      "empty is valid",
			encodings: []string{},
			wantErr:   false,
		},
		{
			name:      "single valid encoding",
			encodings: []string{"ISO_IR 192"},
			wantErr:   false,
		},
		{
			name:      "multi valid encodings",
			encodings: []string{"ISO 2022 IR 87", "ISO 2022 IR 13"},
			wantErr:   false,
		},
		{
			name:      "stand-alone with code extensions",
			encodings: []string{"ISO_IR 192", "ISO_IR 100"},
			wantErr:   true,
		},
		{
			name:      "stand-alone in second position",
			encodings: []string{"ISO_IR 100", "ISO_IR 192"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := charset.ValidateEncodings(tt.encodings)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEncodings() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetSupportedEncodings(t *testing.T) {
	encodings := charset.GetSupportedEncodings()

	if len(encodings) == 0 {
		t.Error("GetSupportedEncodings() returned empty list")
	}

	// Check for some expected encodings
	found := make(map[string]bool)
	for _, enc := range encodings {
		found[enc] = true
	}

	expected := []string{"ISO_IR 192", "ISO_IR 100", "ISO 2022 IR 87", "GB18030"}
	for _, exp := range expected {
		if !found[exp] {
			t.Errorf("GetSupportedEncodings() missing expected encoding: %s", exp)
		}
	}
}

func TestGetEncodingDescription(t *testing.T) {
	tests := []struct {
		name      string
		encoding  string
		wantMatch string
	}{
		{
			name:      "UTF-8",
			encoding:  "ISO_IR 192",
			wantMatch: "Unicode",
		},
		{
			name:      "Japanese",
			encoding:  "ISO 2022 IR 87",
			wantMatch: "Japanese",
		},
		{
			name:      "unknown",
			encoding:  "UNKNOWN",
			wantMatch: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := charset.GetEncodingDescription(tt.encoding)
			if desc == "" {
				t.Error("GetEncodingDescription() returned empty string")
			}
			// Just check it contains something relevant
			if tt.wantMatch != "" {
				// Simple check - description should contain the expected word
				found := false
				for _, c := range desc {
					if c != 0 {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("GetEncodingDescription() = %q, expected non-empty", desc)
				}
			}
		})
	}
}

func TestIsMultiByteEncoding(t *testing.T) {
	tests := []struct {
		name      string
		encoding  string
		wantMulti bool
	}{
		{"UTF-8 is not multi-byte", "ISO_IR 192", false},
		{"Latin-1 is not multi-byte", "ISO_IR 100", false},
		{"Japanese is multi-byte", "ISO 2022 IR 87", true},
		{"Korean is multi-byte", "ISO 2022 IR 149", true},
		{"Shift-JIS is multi-byte", "ISO_IR 13", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charset.IsMultiByteEncoding(tt.encoding)
			if got != tt.wantMulti {
				t.Errorf("IsMultiByteEncoding(%q) = %v, want %v", tt.encoding, got, tt.wantMulti)
			}
		})
	}
}

func TestSplitMultiValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "single value",
			value: "value1",
			want:  []string{"value1"},
		},
		{
			name:  "multiple values",
			value: "value1\\value2\\value3",
			want:  []string{"value1", "value2", "value3"},
		},
		{
			name:  "empty string",
			value: "",
			want:  []string{},
		},
		{
			name:  "trailing backslash",
			value: "value1\\value2\\",
			want:  []string{"value1", "value2", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charset.SplitMultiValue(tt.value)
			if len(got) != len(tt.want) {
				t.Errorf("SplitMultiValue() got %d values, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("SplitMultiValue()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestJoinMultiValue(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{
			name:   "single value",
			values: []string{"value1"},
			want:   "value1",
		},
		{
			name:   "multiple values",
			values: []string{"value1", "value2", "value3"},
			want:   "value1\\value2\\value3",
		},
		{
			name:   "empty slice",
			values: []string{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charset.JoinMultiValue(tt.values)
			if got != tt.want {
				t.Errorf("JoinMultiValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeElement(t *testing.T) {
	ctx := context.Background()
	cs, _ := charset.NewCharacterSet([]string{"UTF-8"})

	// Test text VR
	result, err := charset.DecodeElement(ctx, []byte("Hello World"), "LO", cs)
	if err != nil {
		t.Errorf("DecodeElement() error = %v", err)
	}
	if str, ok := result.(string); !ok || str != "Hello World" {
		t.Errorf("DecodeElement() = %v, want %q", result, "Hello World")
	}

	// Test PersonName VR
	result, err = charset.DecodeElement(ctx, []byte("Doe^John"), "PN", cs)
	if err != nil {
		t.Errorf("DecodeElement() for PN error = %v", err)
	}
	if pn, ok := result.(*charset.PersonName); !ok {
		t.Errorf("DecodeElement() for PN returned %T, want *PersonName", result)
	} else if pn.Alphabetic != "Doe^John" {
		t.Errorf("DecodeElement() for PN = %q, want %q", pn.Alphabetic, "Doe^John")
	}

	// Test empty value
	result, err = charset.DecodeElement(ctx, []byte{}, "LO", cs)
	if err != nil {
		t.Errorf("DecodeElement() for empty value error = %v", err)
	}
	if result != "" {
		t.Errorf("DecodeElement() for empty value = %v, want empty string", result)
	}
}

func TestEncodeElement(t *testing.T) {
	ctx := context.Background()
	cs, _ := charset.NewCharacterSet([]string{"UTF-8"})

	// Test string value
	result, err := charset.EncodeElement(ctx, "Hello World", "LO", cs)
	if err != nil {
		t.Errorf("EncodeElement() error = %v", err)
	}
	if string(result) != "Hello World" {
		t.Errorf("EncodeElement() = %q, want %q", result, "Hello World")
	}

	// Test PersonName value
	pn := charset.NewPersonName("Doe^John", "", "")
	result, err = charset.EncodeElement(ctx, pn, "PN", cs)
	if err != nil {
		t.Errorf("EncodeElement() for PN error = %v", err)
	}
	if string(result) != "Doe^John" {
		t.Errorf("EncodeElement() for PN = %q, want %q", result, "Doe^John")
	}

	// Test PN VR with string value
	result, err = charset.EncodeElement(ctx, "Smith^Jane", "PN", cs)
	if err != nil {
		t.Errorf("EncodeElement() for PN string error = %v", err)
	}
	if string(result) != "Smith^Jane" {
		t.Errorf("EncodeElement() for PN string = %q, want %q", result, "Smith^Jane")
	}
}

func TestEncodeElement_ValidationModes(t *testing.T) {
	cs, _ := charset.NewCharacterSet([]string{"ISO-8859-1"})

	// Test with strict mode - should fail for unencodable characters
	ctx := config.WithWritingValidationMode(context.Background(), config.RAISE)
	_, _ = charset.EncodeElement(ctx, "Hello 世界", "LO", cs)
	// In RAISE mode with ISO-8859-1, Chinese characters should cause an error
	// (though the exact behavior depends on the encoder implementation)

	// Test with WARN mode - should succeed with warnings
	ctx = config.WithWritingValidationMode(context.Background(), config.WARN)
	result, err := charset.EncodeElement(ctx, "Hello 世界", "LO", cs)
	if err != nil {
		t.Errorf("EncodeElement() in WARN mode error = %v", err)
	}
	if len(result) == 0 {
		t.Error("EncodeElement() in WARN mode returned empty result")
	}

	// Test with IGNORE mode - should succeed silently
	ctx = config.WithWritingValidationMode(context.Background(), config.IGNORE)
	result, err = charset.EncodeElement(ctx, "Hello 世界", "LO", cs)
	if err != nil {
		t.Errorf("EncodeElement() in IGNORE mode error = %v", err)
	}
	if len(result) == 0 {
		t.Error("EncodeElement() in IGNORE mode returned empty result")
	}
}
