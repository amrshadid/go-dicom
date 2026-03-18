package charset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/charset"
)

// TestIsValidGoEncoding_AllSupportedEncodings tests that all supported encodings are recognized as valid.
func TestIsValidGoEncoding_AllSupportedEncodings(t *testing.T) {
	// Get all supported encodings
	supportedEncodings := charset.GetSupportedEncodings()

	if len(supportedEncodings) == 0 {
		t.Fatal("Expected at least some supported encodings")
	}

	t.Logf("Testing %d supported encodings", len(supportedEncodings))

	// For each supported DICOM encoding, convert to Go encoding and verify it's valid
	for _, dicomEncoding := range supportedEncodings {
		// Convert DICOM name to Go encoding
		goEncoding := charset.DicomToGoEncoding(dicomEncoding)
		if goEncoding == "" {
			t.Errorf("DICOM encoding %q has no Go encoding mapping", dicomEncoding)
			continue
		}

		// Get encoding info
		info := charset.GetEncodingInfoByGoName(goEncoding)
		if info == nil {
			t.Errorf("No encoding info found for Go encoding %q (from DICOM %q)", goEncoding, dicomEncoding)
			continue
		}

		t.Logf("✓ %s -> %s (%s)", dicomEncoding, goEncoding, info.Description)
	}
}

// TestIsValidGoEncoding_CommonEncodings tests common encoding names.
func TestIsValidGoEncoding_CommonEncodings(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		want     bool
	}{
		{
			name:     "UTF-8",
			encoding: "UTF-8",
			want:     true,
		},
		{
			name:     "ISO-8859-1",
			encoding: "ISO-8859-1",
			want:     true,
		},
		{
			name:     "ISO-8859-2",
			encoding: "ISO-8859-2",
			want:     true,
		},
		{
			name:     "ISO-8859-5 (Cyrillic)",
			encoding: "ISO-8859-5",
			want:     true,
		},
		{
			name:     "ISO-8859-6 (Arabic)",
			encoding: "ISO-8859-6",
			want:     true,
		},
		{
			name:     "ISO-8859-7 (Greek)",
			encoding: "ISO-8859-7",
			want:     true,
		},
		{
			name:     "ISO-8859-8 (Hebrew)",
			encoding: "ISO-8859-8",
			want:     true,
		},
		{
			name:     "Shift_JIS",
			encoding: "Shift_JIS",
			want:     true,
		},
		{
			name:     "EUC-JP",
			encoding: "EUC-JP",
			want:     true,
		},
		{
			name:     "ISO-2022-JP",
			encoding: "ISO-2022-JP",
			want:     true,
		},
		{
			name:     "EUC-KR",
			encoding: "EUC-KR",
			want:     true,
		},
		{
			name:     "GB18030",
			encoding: "GB18030",
			want:     true,
		},
		{
			name:     "GBK",
			encoding: "GBK",
			want:     true,
		},
		{
			name:     "Invalid encoding",
			encoding: "INVALID-ENCODING",
			want:     false,
		},
		{
			name:     "Empty string (valid, means default)",
			encoding: "",
			want:     true, // Empty is valid - means default encoding
		},
		{
			name:     "Random string",
			encoding: "foobar123",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need to test via ValidateEncoding since isValidGoEncoding is not exported
			err := charset.ValidateEncoding(tt.encoding)
			got := (err == nil)

			if got != tt.want {
				t.Errorf("ValidateEncoding(%q) valid=%v, want valid=%v, error=%v",
					tt.encoding, got, tt.want, err)
			}
		})
	}
}

// TestValidateEncoding_AllGoEncodings tests that ValidateEncoding works with all Go encoding names.
func TestValidateEncoding_AllGoEncodings(t *testing.T) {
	goEncodings := []string{
		"UTF-8",
		"ISO-8859-1",
		"ISO-8859-2",
		"ISO-8859-3",
		"ISO-8859-4",
		"ISO-8859-5",
		"ISO-8859-6",
		"ISO-8859-7",
		"ISO-8859-8",
		"ISO-8859-9",
		"Shift_JIS",
		"EUC-JP",
		"ISO-2022-JP",
		"EUC-KR",
		"GB18030",
		"GBK",
	}

	for _, encoding := range goEncodings {
		t.Run(encoding, func(t *testing.T) {
			err := charset.ValidateEncoding(encoding)
			if err != nil {
				t.Errorf("ValidateEncoding(%q) failed: %v", encoding, err)
			}
		})
	}
}

// TestValidateEncoding_MixedDICOMAndGo tests that both DICOM and Go encoding names work.
func TestValidateEncoding_MixedDICOMAndGo(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		wantErr  bool
	}{
		// DICOM names
		{name: "DICOM UTF-8", encoding: "ISO_IR 192", wantErr: false},
		{name: "DICOM Latin-1", encoding: "ISO_IR 100", wantErr: false},
		{name: "DICOM Japanese", encoding: "ISO_IR 13", wantErr: false},

		// Go names
		{name: "Go UTF-8", encoding: "UTF-8", wantErr: false},
		{name: "Go ISO-8859-1", encoding: "ISO-8859-1", wantErr: false},
		{name: "Go Shift_JIS", encoding: "Shift_JIS", wantErr: false},

		// Invalid
		{name: "Invalid", encoding: "NOT_A_REAL_ENCODING", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := charset.ValidateEncoding(tt.encoding)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEncoding(%q) error=%v, wantErr=%v",
					tt.encoding, err, tt.wantErr)
			}
		})
	}
}

// TestEncodingRoundTrip tests that all encodings can encode and decode successfully.
func TestEncodingRoundTrip(t *testing.T) {
	// Test with simple ASCII text (should work with all encodings)
	testText := "Hello World 123"

	supportedEncodings := charset.GetSupportedEncodings()

	for _, dicomEncoding := range supportedEncodings {
		t.Run(dicomEncoding, func(t *testing.T) {
			// Convert to character set
			cs, err := charset.NewCharacterSet([]string{dicomEncoding})
			if err != nil {
				t.Fatalf("Failed to create character set: %v", err)
			}

			// Encode
			encoded, err := charset.EncodeString(testText, cs.Encodings)
			if err != nil {
				t.Fatalf("Failed to encode: %v", err)
			}

			// Decode
			decoded, err := charset.DecodeBytes(encoded, cs.Encodings, charset.DefaultTextDelimiters)
			if err != nil {
				t.Fatalf("Failed to decode: %v", err)
			}

			// Verify
			if decoded != testText {
				t.Errorf("Round-trip failed: got %q, want %q", decoded, testText)
			}
		})
	}
}
