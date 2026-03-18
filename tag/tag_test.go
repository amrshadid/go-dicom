package tag_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

func TestTagCreation(t *testing.T) {
	tests := []struct {
		name    string
		group   uint16
		element uint16
		want    uint32
	}{
		{
			name:    "PatientName",
			group:   0x0010,
			element: 0x0010,
			want:    0x00100010,
		},
		{
			name:    "PatientID",
			group:   0x0010,
			element: 0x0020,
			want:    0x00100020,
		},
		{
			name:    "StudyDate",
			group:   0x0008,
			element: 0x0020,
			want:    0x00080020,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := tag.New(tt.group, tt.element)
			if uint32(tg) != tt.want {
				t.Errorf("New(%04X, %04X) = %08X, want %08X", tt.group, tt.element, uint32(tg), tt.want)
			}
		})
	}
}

func TestTagGroup(t *testing.T) {
	tg := tag.New(0x0010, 0x0020)
	if tg.Group() != 0x0010 {
		t.Errorf("Group() = %04X, want 0010", tg.Group())
	}
}

func TestTagElement(t *testing.T) {
	tg := tag.New(0x0010, 0x0020)
	if tg.Element() != 0x0020 {
		t.Errorf("Element() = %04X, want 0020", tg.Element())
	}
}

func TestTagIsPrivate(t *testing.T) {
	tests := []struct {
		name      string
		group     uint16
		element   uint16
		wantPriv  bool
		wantCreat bool
	}{
		{
			name:      "standard tag",
			group:     0x0010,
			element:   0x0010,
			wantPriv:  false,
			wantCreat: false,
		},
		{
			name:      "private tag",
			group:     0x0011,
			element:   0x0010,
			wantPriv:  true,
			wantCreat: true, // 0x0010 is in the range 0x0010-0x00FF
		},
		{
			name:      "private creator tag",
			group:     0x0011,
			element:   0x0010,
			wantPriv:  true,
			wantCreat: true,
		},
		{
			name:      "private creator tag at 0x00FF",
			group:     0x0011,
			element:   0x00FF,
			wantPriv:  true,
			wantCreat: true,
		},
		{
			name:      "private data tag",
			group:     0x0011,
			element:   0x1001,
			wantPriv:  true,
			wantCreat: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := tag.New(tt.group, tt.element)

			if tg.IsPrivate() != tt.wantPriv {
				t.Errorf("IsPrivate() = %v, want %v", tg.IsPrivate(), tt.wantPriv)
			}

			if tg.IsPrivateCreator() != tt.wantCreat {
				t.Errorf("IsPrivateCreator() = %v, want %v", tg.IsPrivateCreator(), tt.wantCreat)
			}
		})
	}
}

func TestTagString(t *testing.T) {
	tests := []struct {
		group   uint16
		element uint16
		want    string
	}{
		{0x0010, 0x0010, "(0010,0010)"},
		{0x0008, 0x0020, "(0008,0020)"},
		{0xFFFF, 0xFFFF, "(FFFF,FFFF)"},
		{0x0000, 0x0000, "(0000,0000)"},
	}

	for _, tt := range tests {
		tg := tag.New(tt.group, tt.element)
		if tg.String() != tt.want {
			t.Errorf("String() = %s, want %s", tg.String(), tt.want)
		}
	}
}

func TestTagHex(t *testing.T) {
	tests := []struct {
		group   uint16
		element uint16
		want    string
	}{
		{0x0010, 0x0010, "00100010"},
		{0x0008, 0x0020, "00080020"},
		{0xFFFF, 0xFFFF, "FFFFFFFF"},
	}

	for _, tt := range tests {
		tg := tag.New(tt.group, tt.element)
		if tg.Hex() != tt.want {
			t.Errorf("Hex() = %s, want %s", tg.Hex(), tt.want)
		}
	}
}

func TestTagFromBytes(t *testing.T) {
	tests := []struct {
		name         string
		bytes        []byte
		littleEndian bool
		want         tag.Tag
		wantErr      bool
	}{
		{
			name:         "little endian PatientName",
			bytes:        []byte{0x10, 0x00, 0x10, 0x00},
			littleEndian: true,
			want:         tag.New(0x0010, 0x0010),
			wantErr:      false,
		},
		{
			name:         "big endian PatientName",
			bytes:        []byte{0x00, 0x10, 0x00, 0x10},
			littleEndian: false,
			want:         tag.New(0x0010, 0x0010),
			wantErr:      false,
		},
		{
			name:         "insufficient bytes",
			bytes:        []byte{0x10, 0x00},
			littleEndian: true,
			want:         0,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg, err := tag.FromBytes(tt.bytes, tt.littleEndian)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromBytes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tg != tt.want {
				t.Errorf("FromBytes() = %08X, want %08X", uint32(tg), uint32(tt.want))
			}
		})
	}
}

func TestTagToBytes(t *testing.T) {
	tests := []struct {
		name         string
		tag          tag.Tag
		littleEndian bool
		want         []byte
	}{
		{
			name:         "little endian PatientName",
			tag:          tag.New(0x0010, 0x0010),
			littleEndian: true,
			want:         []byte{0x10, 0x00, 0x10, 0x00},
		},
		{
			name:         "big endian PatientName",
			tag:          tag.New(0x0010, 0x0010),
			littleEndian: false,
			want:         []byte{0x00, 0x10, 0x00, 0x10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tag.ToBytes(tt.littleEndian)
			if len(got) != len(tt.want) {
				t.Errorf("ToBytes() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ToBytes()[%d] = %02X, want %02X", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseTag(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    tag.Tag
		wantErr bool
	}{
		{
			name:    "parenthesis format",
			input:   "(0010,0010)",
			want:    tag.New(0x0010, 0x0010),
			wantErr: false,
		},
		{
			name:    "parenthesis format with spaces",
			input:   "( 0010 , 0010 )",
			want:    tag.New(0x0010, 0x0010),
			wantErr: false,
		},
		{
			name:    "hex literal 0x format",
			input:   "0x00100010",
			want:    tag.New(0x0010, 0x0010),
			wantErr: false,
		},
		{
			name:    "hex literal 0X format",
			input:   "0X00100010",
			want:    tag.New(0x0010, 0x0010),
			wantErr: false,
		},
		{
			name:    "plain hex",
			input:   "00100010",
			want:    tag.New(0x0010, 0x0010),
			wantErr: false,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			want:    0,
			wantErr: true,
		},
		{
			name:    "incomplete parenthesis format",
			input:   "(0010,0010",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tag.ParseTag(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTag() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("ParseTag() = %08X, want %08X", uint32(got), uint32(tt.want))
			}
		})
	}
}

func TestTagEquals(t *testing.T) {
	tag1 := tag.New(0x0010, 0x0010)
	tag2 := tag.New(0x0010, 0x0010)
	tag3 := tag.New(0x0010, 0x0020)

	if !tag1.Equals(tag2) {
		t.Errorf("tag1.Equals(tag2) = false, want true")
	}

	if tag1.Equals(tag3) {
		t.Errorf("tag1.Equals(tag3) = true, want false")
	}
}

func TestTagLess(t *testing.T) {
	tests := []struct {
		tag1 tag.Tag
		tag2 tag.Tag
		want bool
	}{
		{tag.New(0x0010, 0x0010), tag.New(0x0010, 0x0020), true},
		{tag.New(0x0010, 0x0020), tag.New(0x0010, 0x0010), false},
		{tag.New(0x0008, 0x0020), tag.New(0x0010, 0x0010), true},
		{tag.New(0x0010, 0x0010), tag.New(0x0010, 0x0010), false},
	}

	for i, tt := range tests {
		if tt.tag1.Less(tt.tag2) != tt.want {
			t.Errorf("test %d: tag1.Less(tag2) = %v, want %v", i, tt.tag1.Less(tt.tag2), tt.want)
		}
	}
}

func TestSpecialTags(t *testing.T) {
	if !tag.ItemTag.IsSpecial() {
		t.Error("ItemTag should be special")
	}
	if !tag.ItemDelimiterTag.IsSpecial() {
		t.Error("ItemDelimiterTag should be special")
	}
	if !tag.SequenceDelimiterTag.IsSpecial() {
		t.Error("SequenceDelimiterTag should be special")
	}

	regularTag := tag.New(0x0010, 0x0010)
	if regularTag.IsSpecial() {
		t.Error("regular tag should not be special")
	}
}

func TestTagUint32(t *testing.T) {
	tg := tag.New(0x0010, 0x0010)
	if tg.Uint32() != uint32(0x00100010) {
		t.Errorf("Uint32() = %08X, want 00100010", tg.Uint32())
	}
}

func TestCompareGroups(t *testing.T) {
	tests := []struct {
		name string
		t1   tag.Tag
		t2   tag.Tag
		want int
	}{
		{
			name: "t1 group < t2 group",
			t1:   tag.New(0x0008, 0x0010),
			t2:   tag.New(0x0010, 0x0010),
			want: -1,
		},
		{
			name: "t1 group > t2 group",
			t1:   tag.New(0x0010, 0x0010),
			t2:   tag.New(0x0008, 0x0010),
			want: 1,
		},
		{
			name: "t1 group == t2 group",
			t1:   tag.New(0x0010, 0x0010),
			t2:   tag.New(0x0010, 0x0020),
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tag.CompareGroups(tt.t1, tt.t2)
			if got != tt.want {
				t.Errorf("CompareGroups() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCompareElements(t *testing.T) {
	tests := []struct {
		name string
		t1   tag.Tag
		t2   tag.Tag
		want int
	}{
		{
			name: "t1 element < t2 element",
			t1:   tag.New(0x0010, 0x0010),
			t2:   tag.New(0x0010, 0x0020),
			want: -1,
		},
		{
			name: "t1 element > t2 element",
			t1:   tag.New(0x0010, 0x0020),
			t2:   tag.New(0x0010, 0x0010),
			want: 1,
		},
		{
			name: "t1 element == t2 element",
			t1:   tag.New(0x0010, 0x0010),
			t2:   tag.New(0x0010, 0x0010),
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tag.CompareElements(tt.t1, tt.t2)
			if got != tt.want {
				t.Errorf("CompareElements() = %d, want %d", got, tt.want)
			}
		})
	}
}
