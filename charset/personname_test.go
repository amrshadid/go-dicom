package charset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/charset"
)

func TestNewPersonName(t *testing.T) {
	pn := charset.NewPersonName("Doe^John", "山田^太郎", "やまだ^たろう")

	if pn.Alphabetic != "Doe^John" {
		t.Errorf("Alphabetic = %q, want %q", pn.Alphabetic, "Doe^John")
	}
	if pn.Ideographic != "山田^太郎" {
		t.Errorf("Ideographic = %q, want %q", pn.Ideographic, "山田^太郎")
	}
	if pn.Phonetic != "やまだ^たろう" {
		t.Errorf("Phonetic = %q, want %q", pn.Phonetic, "やまだ^たろう")
	}
}

func TestPersonName_String(t *testing.T) {
	tests := []struct {
		name        string
		alphabetic  string
		ideographic string
		phonetic    string
		want        string
	}{
		{
			name:       "alphabetic only",
			alphabetic: "Doe^John",
			want:       "Doe^John",
		},
		{
			name:        "alphabetic and ideographic",
			alphabetic:  "Yamada^Tarou",
			ideographic: "山田^太郎",
			want:        "Yamada^Tarou=山田^太郎",
		},
		{
			name:        "all three components",
			alphabetic:  "Yamada^Tarou",
			ideographic: "山田^太郎",
			phonetic:    "やまだ^たろう",
			want:        "Yamada^Tarou=山田^太郎=やまだ^たろう",
		},
		{
			name:        "ideographic only",
			ideographic: "山田^太郎",
			want:        "=山田^太郎",
		},
		{
			name:     "phonetic only",
			phonetic: "やまだ^たろう",
			want:     "==やまだ^たろう",
		},
		{
			name: "empty",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pn := charset.NewPersonName(tt.alphabetic, tt.ideographic, tt.phonetic)
			got := pn.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPersonName_IsEmpty(t *testing.T) {
	tests := []struct {
		name      string
		pn        *charset.PersonName
		wantEmpty bool
	}{
		{
			name:      "all empty",
			pn:        charset.NewPersonName("", "", ""),
			wantEmpty: true,
		},
		{
			name:      "alphabetic only",
			pn:        charset.NewPersonName("Doe^John", "", ""),
			wantEmpty: false,
		},
		{
			name:      "ideographic only",
			pn:        charset.NewPersonName("", "山田^太郎", ""),
			wantEmpty: false,
		},
		{
			name:      "phonetic only",
			pn:        charset.NewPersonName("", "", "やまだ^たろう"),
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pn.IsEmpty(); got != tt.wantEmpty {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.wantEmpty)
			}
		})
	}
}

func TestPersonName_ComponentAccessors(t *testing.T) {
	pn := charset.NewPersonName("Doe^John^Q^Dr.^Jr.", "", "")

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"FamilyName", pn.FamilyName(), "Doe"},
		{"GivenName", pn.GivenName(), "John"},
		{"MiddleName", pn.MiddleName(), "Q"},
		{"Prefix", pn.Prefix(), "Dr."},
		{"Suffix", pn.Suffix(), "Jr."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestPersonName_PartialComponents(t *testing.T) {
	pn := charset.NewPersonName("Doe^John", "", "")

	if pn.FamilyName() != "Doe" {
		t.Errorf("FamilyName() = %q, want %q", pn.FamilyName(), "Doe")
	}
	if pn.GivenName() != "John" {
		t.Errorf("GivenName() = %q, want %q", pn.GivenName(), "John")
	}
	if pn.MiddleName() != "" {
		t.Errorf("MiddleName() = %q, want empty", pn.MiddleName())
	}
	if pn.Prefix() != "" {
		t.Errorf("Prefix() = %q, want empty", pn.Prefix())
	}
	if pn.Suffix() != "" {
		t.Errorf("Suffix() = %q, want empty", pn.Suffix())
	}
}

func TestDecodePersonName(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		encodings    []string
		wantAlpha    string
		wantIdeo     string
		wantPhonetic string
	}{
		{
			name:      "simple ASCII",
			data:      []byte("Doe^John"),
			encodings: []string{"ISO-8859-1"},
			wantAlpha: "Doe^John",
		},
		{
			name:         "with group separators",
			data:         []byte("Yamada^Tarou=山田^太郎=やまだ^たろう"),
			encodings:    []string{"UTF-8"},
			wantAlpha:    "Yamada^Tarou",
			wantIdeo:     "山田^太郎",
			wantPhonetic: "やまだ^たろう",
		},
		{
			name:      "empty data",
			data:      []byte{},
			encodings: []string{"UTF-8"},
			wantAlpha: "",
		},
		{
			name:      "only alphabetic",
			data:      []byte("Smith^Jane"),
			encodings: []string{"ISO-8859-1"},
			wantAlpha: "Smith^Jane",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pn, err := charset.DecodePersonName(tt.data, tt.encodings)
			if err != nil {
				t.Errorf("DecodePersonName() error = %v", err)
				return
			}

			if pn.Alphabetic != tt.wantAlpha {
				t.Errorf("Alphabetic = %q, want %q", pn.Alphabetic, tt.wantAlpha)
			}
			if pn.Ideographic != tt.wantIdeo {
				t.Errorf("Ideographic = %q, want %q", pn.Ideographic, tt.wantIdeo)
			}
			if pn.Phonetic != tt.wantPhonetic {
				t.Errorf("Phonetic = %q, want %q", pn.Phonetic, tt.wantPhonetic)
			}
		})
	}
}

func TestEncodePersonName(t *testing.T) {
	tests := []struct {
		name      string
		pn        *charset.PersonName
		encodings []string
		wantErr   bool
	}{
		{
			name:      "simple ASCII",
			pn:        charset.NewPersonName("Doe^John", "", ""),
			encodings: []string{"ISO-8859-1"},
			wantErr:   false,
		},
		{
			name:      "all three components UTF-8",
			pn:        charset.NewPersonName("Yamada^Tarou", "山田^太郎", "やまだ^たろう"),
			encodings: []string{"UTF-8"},
			wantErr:   false,
		},
		{
			name:      "empty PersonName",
			pn:        charset.NewPersonName("", "", ""),
			encodings: []string{"UTF-8"},
			wantErr:   false,
		},
		{
			name:      "nil PersonName",
			pn:        nil,
			encodings: []string{"UTF-8"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := charset.EncodePersonName(tt.pn, tt.encodings)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncodePersonName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.pn == nil || tt.pn.IsEmpty() {
				if len(encoded) != 0 {
					t.Errorf("EncodePersonName() for empty/nil should return empty bytes, got %d bytes", len(encoded))
				}
				return
			}

			// Verify round-trip
			decoded, err := charset.DecodePersonName(encoded, tt.encodings)
			if err != nil {
				t.Errorf("Round-trip decode error = %v", err)
				return
			}

			if decoded.Alphabetic != tt.pn.Alphabetic {
				t.Errorf("Round-trip Alphabetic = %q, want %q", decoded.Alphabetic, tt.pn.Alphabetic)
			}
			if decoded.Ideographic != tt.pn.Ideographic {
				t.Errorf("Round-trip Ideographic = %q, want %q", decoded.Ideographic, tt.pn.Ideographic)
			}
			if decoded.Phonetic != tt.pn.Phonetic {
				t.Errorf("Round-trip Phonetic = %q, want %q", decoded.Phonetic, tt.pn.Phonetic)
			}
		})
	}
}

func TestFromComponents(t *testing.T) {
	tests := []struct {
		name       string
		familyName string
		givenName  string
		middleName string
		prefix     string
		suffix     string
		want       string
	}{
		{
			name:       "full name",
			familyName: "Doe",
			givenName:  "John",
			middleName: "Q",
			prefix:     "Dr.",
			suffix:     "Jr.",
			want:       "Doe^John^Q^Dr.^Jr.",
		},
		{
			name:       "family and given only",
			familyName: "Smith",
			givenName:  "Jane",
			want:       "Smith^Jane",
		},
		{
			name:       "family only",
			familyName: "Doe",
			want:       "Doe",
		},
		{
			name: "empty",
			want: "",
		},
		{
			name:       "with middle, no prefix/suffix",
			familyName: "Doe",
			givenName:  "John",
			middleName: "Q",
			want:       "Doe^John^Q",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pn := charset.FromComponents(tt.familyName, tt.givenName, tt.middleName, tt.prefix, tt.suffix)
			if pn.Alphabetic != tt.want {
				t.Errorf("FromComponents() = %q, want %q", pn.Alphabetic, tt.want)
			}
		})
	}
}

func TestFromNamedComponents(t *testing.T) {
	alphabetic := "Yamada^Tarou"
	ideographic := "山田^太郎"
	phonetic := "やまだ^たろう"

	pn := charset.FromNamedComponents(alphabetic, ideographic, phonetic)

	if pn.Alphabetic != alphabetic {
		t.Errorf("Alphabetic = %q, want %q", pn.Alphabetic, alphabetic)
	}
	if pn.Ideographic != ideographic {
		t.Errorf("Ideographic = %q, want %q", pn.Ideographic, ideographic)
	}
	if pn.Phonetic != phonetic {
		t.Errorf("Phonetic = %q, want %q", pn.Phonetic, phonetic)
	}
}

func TestPersonName_MultiEncoding(t *testing.T) {
	// Test with multi-encoding (alphabetic in ASCII, ideographic in UTF-8)
	pn := charset.NewPersonName("Yamada^Tarou", "山田^太郎", "やまだ^たろう")
	encodings := []string{"ISO-8859-1", "UTF-8", "UTF-8"}

	encoded, err := charset.EncodePersonName(pn, encodings)
	if err != nil {
		t.Fatalf("EncodePersonName() error = %v", err)
	}

	decoded, err := charset.DecodePersonName(encoded, encodings)
	if err != nil {
		t.Fatalf("DecodePersonName() error = %v", err)
	}

	// Alphabetic should be preserved (ASCII compatible)
	if decoded.Alphabetic != pn.Alphabetic {
		t.Errorf("Alphabetic = %q, want %q", decoded.Alphabetic, pn.Alphabetic)
	}

	// Ideographic should be preserved (UTF-8)
	if decoded.Ideographic != pn.Ideographic {
		t.Errorf("Ideographic = %q, want %q", decoded.Ideographic, pn.Ideographic)
	}

	// Phonetic should be preserved (UTF-8)
	if decoded.Phonetic != pn.Phonetic {
		t.Errorf("Phonetic = %q, want %q", decoded.Phonetic, pn.Phonetic)
	}
}
