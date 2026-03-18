package charset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/charset"
)

func TestDicomToGoEncoding(t *testing.T) {
	tests := []struct {
		name       string
		dicomName  string
		wantGoName string
	}{
		{
			name:       "empty string defaults to ISO-8859-1",
			dicomName:  "",
			wantGoName: "ISO-8859-1",
		},
		{
			name:       "ISO_IR 6 maps to ISO-8859-1",
			dicomName:  "ISO_IR 6",
			wantGoName: "ISO-8859-1",
		},
		{
			name:       "UTF-8",
			dicomName:  "ISO_IR 192",
			wantGoName: "UTF-8",
		},
		{
			name:       "Latin-1",
			dicomName:  "ISO_IR 100",
			wantGoName: "ISO-8859-1",
		},
		{
			name:       "Japanese Kanji",
			dicomName:  "ISO 2022 IR 87",
			wantGoName: "ISO-2022-JP",
		},
		{
			name:       "Korean",
			dicomName:  "ISO 2022 IR 149",
			wantGoName: "EUC-KR",
		},
		{
			name:       "Chinese GB18030",
			dicomName:  "GB18030",
			wantGoName: "GB18030",
		},
		{
			name:       "Greek",
			dicomName:  "ISO_IR 126",
			wantGoName: "ISO-8859-7",
		},
		{
			name:       "Arabic",
			dicomName:  "ISO_IR 127",
			wantGoName: "ISO-8859-6",
		},
		{
			name:       "Hebrew",
			dicomName:  "ISO_IR 138",
			wantGoName: "ISO-8859-8",
		},
		{
			name:       "Cyrillic",
			dicomName:  "ISO_IR 144",
			wantGoName: "ISO-8859-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charset.DicomToGoEncoding(tt.dicomName)
			if got != tt.wantGoName {
				t.Errorf("DicomToGoEncoding(%q) = %q, want %q", tt.dicomName, got, tt.wantGoName)
			}
		})
	}
}

func TestGetEncodingInfo(t *testing.T) {
	tests := []struct {
		name      string
		dicomName string
		wantNil   bool
		checkType charset.EncodingType
	}{
		{
			name:      "UTF-8 is stand-alone",
			dicomName: "ISO_IR 192",
			wantNil:   false,
			checkType: charset.EncodingTypeStandAlone,
		},
		{
			name:      "GB18030 is stand-alone",
			dicomName: "GB18030",
			wantNil:   false,
			checkType: charset.EncodingTypeStandAlone,
		},
		{
			name:      "GBK is stand-alone",
			dicomName: "GBK",
			wantNil:   false,
			checkType: charset.EncodingTypeStandAlone,
		},
		{
			name:      "ISO-8859-1 is single-byte",
			dicomName: "ISO_IR 100",
			wantNil:   false,
			checkType: charset.EncodingTypeSingleByte,
		},
		{
			name:      "ISO-2022-JP is multi-byte",
			dicomName: "ISO 2022 IR 87",
			wantNil:   false,
			checkType: charset.EncodingTypeMultiByte,
		},
		{
			name:      "unknown encoding returns nil",
			dicomName: "UNKNOWN_ENCODING",
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := charset.GetEncodingInfo(tt.dicomName)
			if tt.wantNil {
				if info != nil {
					t.Errorf("GetEncodingInfo(%q) = %v, want nil", tt.dicomName, info)
				}
				return
			}

			if info == nil {
				t.Errorf("GetEncodingInfo(%q) = nil, want non-nil", tt.dicomName)
				return
			}

			if info.Type != tt.checkType {
				t.Errorf("GetEncodingInfo(%q).Type = %v, want %v", tt.dicomName, info.Type, tt.checkType)
			}
		})
	}
}

func TestIsStandAloneEncoding(t *testing.T) {
	tests := []struct {
		name           string
		dicomName      string
		wantStandAlone bool
	}{
		{"UTF-8 is stand-alone", "ISO_IR 192", true},
		{"GB18030 is stand-alone", "GB18030", true},
		{"GBK is stand-alone", "GBK", true},
		{"ISO-8859-1 is not stand-alone", "ISO_IR 100", false},
		{"ISO-2022-JP is not stand-alone", "ISO 2022 IR 87", false},
		{"Korean is not stand-alone", "ISO 2022 IR 149", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charset.IsStandAloneEncoding(tt.dicomName)
			if got != tt.wantStandAlone {
				t.Errorf("IsStandAloneEncoding(%q) = %v, want %v", tt.dicomName, got, tt.wantStandAlone)
			}
		})
	}
}

func TestEscapeSequences(t *testing.T) {
	tests := []struct {
		name           string
		goEncoding     string
		wantEscapeLen  int
		wantTailEscape bool
	}{
		{
			name:           "ISO-2022-JP requires tail escape",
			goEncoding:     "ISO-2022-JP",
			wantEscapeLen:  4, // Returns 4-byte escape from IR 159 (last mapped)
			wantTailEscape: true,
		},
		{
			name:           "UTF-8 has no escape sequence",
			goEncoding:     "UTF-8",
			wantEscapeLen:  0,
			wantTailEscape: false,
		},
		{
			name:           "ISO-8859-1 has escape sequence",
			goEncoding:     "ISO-8859-1",
			wantEscapeLen:  3,
			wantTailEscape: false,
		},
		{
			name:           "EUC-KR has escape sequence",
			goEncoding:     "EUC-KR",
			wantEscapeLen:  4,
			wantTailEscape: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escSeq := charset.GoEncodingToEscape(tt.goEncoding)
			if len(escSeq) != tt.wantEscapeLen {
				t.Errorf("GoEncodingToEscape(%q) length = %d, want %d", tt.goEncoding, len(escSeq), tt.wantEscapeLen)
			}

			requiresTail := charset.RequiresTailEscape(tt.goEncoding)
			if requiresTail != tt.wantTailEscape {
				t.Errorf("RequiresTailEscape(%q) = %v, want %v", tt.goEncoding, requiresTail, tt.wantTailEscape)
			}
		})
	}
}

func TestDelimiterSet(t *testing.T) {
	delims := charset.NewDelimiterSet(
		charset.DelimiterLF,
		charset.DelimiterCR,
		charset.DelimiterTAB,
	)

	tests := []struct {
		name  string
		b     byte
		wants bool
	}{
		{"LF is delimiter", 0x0A, true},
		{"CR is delimiter", 0x0D, true},
		{"TAB is delimiter", 0x09, true},
		{"FF is not delimiter", 0x0C, false},
		{"Caret is not delimiter", 0x5E, false},
		{"Space is not delimiter", 0x20, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := delims.Contains(tt.b)
			if got != tt.wants {
				t.Errorf("Contains(%#x) = %v, want %v", tt.b, got, tt.wants)
			}
		})
	}
}

func TestDefaultDelimiters(t *testing.T) {
	// Test that default text delimiters contain expected values
	if !charset.DefaultTextDelimiters.Contains(0x0A) {
		t.Error("DefaultTextDelimiters should contain LF")
	}
	if !charset.DefaultTextDelimiters.Contains(0x0D) {
		t.Error("DefaultTextDelimiters should contain CR")
	}
	if !charset.DefaultTextDelimiters.Contains(0x09) {
		t.Error("DefaultTextDelimiters should contain TAB")
	}
	if !charset.DefaultTextDelimiters.Contains(0x0C) {
		t.Error("DefaultTextDelimiters should contain FF")
	}

	// Test that PersonName delimiters contain caret
	if !charset.PersonNameDelimiters.Contains(0x5E) {
		t.Error("PersonNameDelimiters should contain caret (^)")
	}
}
