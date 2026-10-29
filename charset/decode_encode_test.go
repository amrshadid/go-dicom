package charset_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/charset"
)

func TestDecodeBytes_Simple(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		encodings []string
		want      string
		wantErr   bool
	}{
		{
			name:      "empty data",
			data:      []byte{},
			encodings: []string{"UTF-8"},
			want:      "",
			wantErr:   false,
		},
		{
			name:      "ASCII text with UTF-8",
			data:      []byte("Hello World"),
			encodings: []string{"UTF-8"},
			want:      "Hello World",
			wantErr:   false,
		},
		{
			name:      "ASCII text with Latin-1",
			data:      []byte("Hello World"),
			encodings: []string{"ISO-8859-1"},
			want:      "Hello World",
			wantErr:   false,
		},
		{
			name:      "UTF-8 encoded string",
			data:      []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0xE4, 0xB8, 0x96, 0xE7, 0x95, 0x8C}, // "Hello 世界"
			encodings: []string{"UTF-8"},
			want:      "Hello 世界",
			wantErr:   false,
		},
		{
			name:      "Latin-1 extended characters",
			data:      []byte{0xE9, 0xE8, 0xE0}, // é è à
			encodings: []string{"ISO-8859-1"},
			want:      "éèà",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charset.DecodeBytes(tt.data, tt.encodings, charset.DefaultTextDelimiters)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DecodeBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeString_Simple(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		encodings []string
		wantErr   bool
	}{
		{
			name:      "empty string",
			value:     "",
			encodings: []string{"UTF-8"},
			wantErr:   false,
		},
		{
			name:      "ASCII text with UTF-8",
			value:     "Hello World",
			encodings: []string{"UTF-8"},
			wantErr:   false,
		},
		{
			name:      "UTF-8 string with multi-byte characters",
			value:     "Hello 世界",
			encodings: []string{"UTF-8"},
			wantErr:   false,
		},
		{
			name:      "Latin-1 extended characters",
			value:     "éèà",
			encodings: []string{"ISO-8859-1"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charset.EncodeString(tt.value, tt.encodings)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncodeString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.value == "" {
				if len(got) != 0 {
					t.Errorf("EncodeString() for empty string should return empty bytes, got %d bytes", len(got))
				}
				return
			}

			// Verify round-trip
			decoded, err := charset.DecodeBytes(got, tt.encodings, charset.DefaultTextDelimiters)
			if err != nil {
				t.Errorf("Round-trip decode error = %v", err)
				return
			}
			if decoded != tt.value {
				t.Errorf("Round-trip failed: got %q, want %q", decoded, tt.value)
			}
		})
	}
}

func TestDecodeEncode_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		encodings []string
	}{
		{
			name:      "English ASCII",
			text:      "Hello World",
			encodings: []string{"UTF-8"},
		},
		{
			name:      "French Latin-1",
			text:      "Français",
			encodings: []string{"ISO-8859-1"},
		},
		{
			name:      "German Latin-1",
			text:      "Äpfel",
			encodings: []string{"ISO-8859-1"},
		},
		{
			name:      "Spanish Latin-1",
			text:      "Español",
			encodings: []string{"ISO-8859-1"},
		},
		{
			name:      "UTF-8 multi-language",
			text:      "Hello 世界 Здравствуй مرحبا",
			encodings: []string{"UTF-8"},
		},
		{
			name:      "Greek",
			text:      "Ελληνικά",
			encodings: []string{"ISO-8859-7"},
		},
		{
			name:      "Cyrillic",
			text:      "Русский",
			encodings: []string{"ISO-8859-5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded, err := charset.EncodeString(tt.text, tt.encodings)
			if err != nil {
				t.Errorf("EncodeString() error = %v", err)
				return
			}

			// Decode
			decoded, err := charset.DecodeBytes(encoded, tt.encodings, charset.DefaultTextDelimiters)
			if err != nil {
				t.Errorf("DecodeBytes() error = %v", err)
				return
			}

			// Compare
			if decoded != tt.text {
				t.Errorf("Round-trip failed: got %q, want %q", decoded, tt.text)
			}
		})
	}
}

func TestDecodeBytes_WithDelimiters(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		encodings  []string
		delimiters charset.DelimiterSet
		want       string
	}{
		{
			name:       "text with line feed",
			data:       []byte("Line 1\nLine 2"),
			encodings:  []string{"UTF-8"},
			delimiters: charset.DefaultTextDelimiters,
			want:       "Line 1\nLine 2",
		},
		{
			name:       "text with carriage return",
			data:       []byte("Line 1\rLine 2"),
			encodings:  []string{"UTF-8"},
			delimiters: charset.DefaultTextDelimiters,
			want:       "Line 1\rLine 2",
		},
		{
			name:       "text with tabs",
			data:       []byte("Col1\tCol2\tCol3"),
			encodings:  []string{"UTF-8"},
			delimiters: charset.DefaultTextDelimiters,
			want:       "Col1\tCol2\tCol3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charset.DecodeBytes(tt.data, tt.encodings, tt.delimiters)
			if err != nil {
				t.Errorf("DecodeBytes() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("DecodeBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindEscapeSequences(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantCount int
	}{
		{
			name:      "no escape sequences",
			data:      []byte("Hello World"),
			wantCount: 0,
		},
		{
			name:      "single 3-byte escape",
			data:      []byte{0x1B, '(', 'B', 'H', 'e', 'l', 'l', 'o'},
			wantCount: 1,
		},
		{
			name:      "single 4-byte escape",
			data:      []byte{0x1B, '$', ')', 'C', 'H', 'e', 'l', 'l', 'o'},
			wantCount: 1,
		},
		{
			name:      "multiple escape sequences",
			data:      []byte{0x1B, '(', 'B', 'A', 'B', 'C', 0x1B, '$', 'B', 'D', 'E', 'F'},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sequences := charset.FindEscapeSequences(tt.data)
			if len(sequences) != tt.wantCount {
				t.Errorf("FindEscapeSequences() found %d sequences, want %d", len(sequences), tt.wantCount)
			}
		})
	}
}

func TestSplitByEscapeSequences(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantCount int
	}{
		{
			name:      "no escape sequences",
			data:      []byte("Hello World"),
			wantCount: 1,
		},
		{
			name:      "single escape at start",
			data:      []byte{0x1B, '(', 'B', 'H', 'e', 'l', 'l', 'o'},
			wantCount: 1,
		},
		{
			name:      "multiple escapes",
			data:      []byte{0x1B, '(', 'B', 'A', 'B', 'C', 0x1B, '$', 'B', 'D', 'E', 'F'},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fragments := charset.SplitByEscapeSequences(tt.data)
			if len(fragments) != tt.wantCount {
				t.Errorf("SplitByEscapeSequences() got %d fragments, want %d", len(fragments), tt.wantCount)
			}
		})
	}
}

func TestStripEscapeSequence(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		wantDataLen   int
		wantEscapeLen int
	}{
		{
			name:          "no escape sequence",
			data:          []byte("Hello"),
			wantDataLen:   5,
			wantEscapeLen: 0,
		},
		{
			name:          "3-byte escape at start",
			data:          []byte{0x1B, '(', 'B', 'H', 'e', 'l', 'l', 'o'},
			wantDataLen:   5,
			wantEscapeLen: 3,
		},
		{
			name:          "4-byte escape at start",
			data:          []byte{0x1B, '$', ')', 'C', 'H', 'e', 'l', 'l', 'o'},
			wantDataLen:   5,
			wantEscapeLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, escape := charset.StripEscapeSequence(tt.data)
			if len(data) != tt.wantDataLen {
				t.Errorf("StripEscapeSequence() data length = %d, want %d", len(data), tt.wantDataLen)
			}
			if len(escape) != tt.wantEscapeLen {
				t.Errorf("StripEscapeSequence() escape length = %d, want %d", len(escape), tt.wantEscapeLen)
			}
		})
	}
}

func TestGetEscapeSequenceForEncoding(t *testing.T) {
	tests := []struct {
		name          string
		encoding      string
		data          []byte
		wantEscapeLen int
	}{
		{
			name:          "UTF-8 has no escape",
			encoding:      "UTF-8",
			data:          []byte("test"),
			wantEscapeLen: 0,
		},
		{
			name:          "ISO-8859-1 has 3-byte escape",
			encoding:      "ISO-8859-1",
			data:          []byte("test"),
			wantEscapeLen: 3,
		},
		{
			name:          "Shift-JIS Katakana (high byte)",
			encoding:      "Shift_JIS",
			data:          []byte{0x80},
			wantEscapeLen: 3,
		},
		{
			name:          "Shift-JIS Romaji (low byte)",
			encoding:      "Shift_JIS",
			data:          []byte{0x50},
			wantEscapeLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escape := charset.GetEscapeSequenceForEncoding(tt.encoding, tt.data)
			if len(escape) != tt.wantEscapeLen {
				t.Errorf("GetEscapeSequenceForEncoding() length = %d, want %d", len(escape), tt.wantEscapeLen)
			}
		})
	}
}

func TestIsPythonHandledEncoding(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		want     bool
	}{
		{"ISO-2022-JP is handled", "ISO-2022-JP", true},
		// Decoded with GB18030, which does not interpret escapes (#195).
		{"ISO-2022-CN is not handled", "ISO-2022-CN", false},
		{"UTF-8 is not handled", "UTF-8", false},
		{"ISO-8859-1 is not handled", "ISO-8859-1", false},
		{"EUC-KR is not handled", "EUC-KR", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charset.IsPythonHandledEncoding(tt.encoding)
			if got != tt.want {
				t.Errorf("IsPythonHandledEncoding(%q) = %v, want %v", tt.encoding, got, tt.want)
			}
		})
	}
}

func TestGetReturnToASCIIEscape(t *testing.T) {
	escape := charset.GetReturnToASCIIEscape()
	if len(escape) != 3 {
		t.Errorf("GetReturnToASCIIEscape() length = %d, want 3", len(escape))
	}
	if escape[0] != 0x1B || escape[1] != '(' || escape[2] != 'B' {
		t.Errorf("GetReturnToASCIIEscape() = %v, want [0x1B '(' 'B']", escape)
	}
}

// TestEmptyFirstCharacterSetIsTheDefault: a multi-valued (0008,0005) with an
// empty first value names the default repertoire as G0 (PS3.5 6.1.2.5.3), and
// every other set must then be introduced by its escape. The empty value was
// skipped, so "\ISO 2022 IR 149" became EUC-KR alone, and Korean was written
// with no ESC $ ) C for any other reader to switch on (#197).
func TestEmptyFirstCharacterSetIsTheDefault(t *testing.T) {
	encodings, err := charset.ConvertEncodings([]string{"", "ISO 2022 IR 149"})
	if err != nil {
		t.Fatal(err)
	}
	if len(encodings) != 2 || encodings[0] != charset.DefaultEncoding || encodings[1] != "EUC-KR" {
		t.Fatalf("ConvertEncodings gave %q, want [%s EUC-KR]", encodings, charset.DefaultEncoding)
	}

	name := "Hong^Gildong=洪^吉洞=홍^길동"
	encoded, err := charset.EncodeString(name, encodings)
	if err != nil {
		t.Fatal(err)
	}
	escape := []byte{0x1B, '$', ')', 'C'}
	at := bytes.Index(encoded, escape)
	if at < 0 || at != bytes.IndexByte(encoded, '=')+1 {
		t.Fatalf("encoded as % X; the Korean should start with ESC $ ) C after the first '='", encoded)
	}
	if decoded, err := charset.DecodeBytes(encoded, encodings, charset.PersonNameDelimiters); err != nil || decoded != name {
		t.Errorf("decoded back as %q (%v), want %q", decoded, err, name)
	}
}

// TestTextNeedingACodeExtensionIsEncoded: Latin-1 with Greek or Cyrillic as a
// code extension. The encoder counted the encodable prefix in runes and cut
// the remainder in bytes, splitting a UTF-8 character, so the encoding failed
// and the value came back as raw UTF-8 with no error (#197).
func TestTextNeedingACodeExtensionIsEncoded(t *testing.T) {
	for _, tc := range []struct {
		value     string
		encodings []string
		escape    []byte
	}{
		{"Muller Διονυσιος", []string{"ISO-8859-1", "ISO-8859-7"}, []byte{0x1B, '-', 'F'}},
		{"Ivanov Люк", []string{"ISO-8859-1", "ISO-8859-5"}, []byte{0x1B, '-', 'L'}},
	} {
		encoded, err := charset.EncodeString(tc.value, tc.encodings)
		if err != nil {
			t.Fatalf("%q: %v", tc.value, err)
		}
		if !bytes.Contains(encoded, tc.escape) {
			t.Errorf("%q encoded as % X, want the extension introduced by % X", tc.value, encoded, tc.escape)
		}
		if decoded, err := charset.DecodeBytes(encoded, tc.encodings, charset.DefaultTextDelimiters); err != nil || decoded != tc.value {
			t.Errorf("%q decoded back as %q (%v)", tc.value, decoded, err)
		}
	}
}

// TestThaiIsDecoded: ISO_IR 166 is TIS 620-2533 (PS3.3 C.12.1.1.2). The
// decoder had no table for it and read the bytes as UTF-8, so every Thai
// character became U+FFFD, and the data set then declared ISO_IR 192 over the
// replacement characters (#195). pydicom reads these bytes as ไทย^สมชาย.
func TestThaiIsDecoded(t *testing.T) {
	tis620 := []byte{0xE4, 0xB7, 0xC2, '^', 0xCA, 0xC1, 0xAA, 0xD2, 0xC2}
	for _, declared := range [][]string{{"ISO_IR 166"}, {"ISO 2022 IR 166"}} {
		encodings, err := charset.ConvertEncodings(declared)
		if err != nil {
			t.Fatalf("%v: %v", declared, err)
		}
		got, err := charset.DecodeBytes(tis620, encodings, charset.PersonNameDelimiters)
		if err != nil {
			t.Fatalf("%v: %v", declared, err)
		}
		if got != "ไทย^สมชาย" {
			t.Errorf("%v: decoded as %q, want %q", declared, got, "ไทย^สมชาย")
		}
	}

	encoded, err := charset.EncodeString("ไทย^สมชาย", []string{"ISO-8859-11"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(tis620) {
		t.Errorf("encoded as % X, want % X", encoded, tis620)
	}
}

// TestGB2312EscapeIsNotText: under ISO 2022 IR 58 the GB2312 characters are
// introduced by ESC $ ) A (PS3.5 Annex K), a code extension control rather
// than part of the value. The escape was left for a decoder that was supposed
// to interpret it and does not, so it stayed inside the decoded text (#195).
func TestGB2312EscapeIsNotText(t *testing.T) {
	escape := []byte{0x1B, '$', ')', 'A'}
	var value []byte
	value = append(value, "Zhang^XiaoDong="...)
	value = append(value, escape...)
	value = append(value, 0xD5, 0xC5, '^')
	value = append(value, escape...)
	value = append(value, 0xD0, 0xA1, 0xB6, 0xAB)

	encodings, err := charset.ConvertEncodings([]string{"", "ISO 2022 IR 58"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := charset.DecodeBytes(value, encodings, charset.PersonNameDelimiters)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Zhang^XiaoDong=张^小东"; got != want {
		t.Errorf("decoded as %q, want %q", got, want)
	}
}
