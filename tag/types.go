package tag

import (
	"fmt"
	"strconv"
	"strings"
)

// Tag represents a DICOM tag as a 32-bit unsigned integer.
// Upper 16 bits are the group number, lower 16 bits are the element number.
type Tag uint32

// New creates a Tag from group and element numbers.
func New(group, element uint16) Tag {
	return Tag((uint32(group) << 16) | uint32(element))
}

// FromInt creates a Tag from a 32-bit integer.
func FromInt(val uint32) Tag {
	return Tag(val)
}

// FromBytes creates a Tag from 4 bytes in little-endian or big-endian format.
func FromBytes(data []byte, littleEndian bool) (Tag, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("insufficient bytes for tag: need 4, got %d", len(data))
	}

	var val uint32
	if littleEndian {
		val = uint32(data[0]) | (uint32(data[1]) << 8) | (uint32(data[2]) << 16) | (uint32(data[3]) << 24)
	} else {
		val = (uint32(data[0]) << 24) | (uint32(data[1]) << 16) | (uint32(data[2]) << 8) | uint32(data[3])
	}
	return Tag(val), nil
}

// ToBytes converts a Tag to 4 bytes in the specified endianness.
func (t Tag) ToBytes(littleEndian bool) []byte {
	result := make([]byte, 4)
	val := uint32(t)

	if littleEndian {
		result[0] = byte(val)
		result[1] = byte(val >> 8)
		result[2] = byte(val >> 16)
		result[3] = byte(val >> 24)
	} else {
		result[0] = byte(val >> 24)
		result[1] = byte(val >> 16)
		result[2] = byte(val >> 8)
		result[3] = byte(val)
	}

	return result
}

// Group returns the group number of the tag (upper 16 bits).
func (t Tag) Group() uint16 {
	return uint16((uint32(t) >> 16) & 0xFFFF)
}

// Element returns the element number of the tag (lower 16 bits).
func (t Tag) Element() uint16 {
	return uint16(uint32(t) & 0xFFFF)
}

// IsPrivate returns true if the tag is a private tag (odd group number).
func (t Tag) IsPrivate() bool {
	return (t.Group() & 0x0001) == 0x0001
}

// IsPrivateCreator returns true if this is a private creator tag.
// Private creator tags have element numbers in the range 0x0010-0x00FF.
func (t Tag) IsPrivateCreator() bool {
	if !t.IsPrivate() {
		return false
	}
	elem := t.Element()
	return elem >= 0x0010 && elem <= 0x00FF
}

// PrivateCreator returns the private creator string if this is a private tag.
// For private tags, the private creator is determined by which 0x00-0x0F range
// the element falls within.
func (t Tag) PrivateCreator() string {
	if !t.IsPrivate() {
		return ""
	}
	elem := t.Element()
	if elem < 0x0100 {
		// Creator tag references
		return ""
	}
	// Private group element offset is elem >> 8
	return fmt.Sprintf("%d", (elem >> 8))
}

// String returns the tag in (GGGG,EEEE) format.
func (t Tag) String() string {
	return fmt.Sprintf("(%04X,%04X)", t.Group(), t.Element())
}

// Hex returns the tag in hexadecimal format without parentheses/comma.
func (t Tag) Hex() string {
	return fmt.Sprintf("%08X", uint32(t))
}

// Uint32 returns the tag as an uint32 value.
func (t Tag) Uint32() uint32 {
	return uint32(t)
}

// Equals checks if two tags are equal.
func (t Tag) Equals(other Tag) bool {
	return t == other
}

// Less checks if this tag is less than another (for sorting).
func (t Tag) Less(other Tag) bool {
	return t < other
}

// IsSpecial returns true if the tag is one of the special DICOM tags.
func (t Tag) IsSpecial() bool {
	return t == ItemTag || t == ItemDelimiterTag || t == SequenceDelimiterTag
}

// Special DICOM tags
const (
	ItemTag              Tag = 0xFFFE0000
	ItemDelimiterTag     Tag = 0xFFFEE00D
	SequenceDelimiterTag Tag = 0xFFFEE0DD
)

// ParseTag parses a tag from various string formats.
// Supported formats:
//   - "(GGGG,EEEE)" hexadecimal format
//   - "0xGGGGEEEE" hex literal
//   - "GGGGEEEE" hex without any separators
func ParseTag(s string) (Tag, error) {
	s = strings.TrimSpace(s)

	// Try (GGGG,EEEE) format
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && strings.Contains(s, ",") {
		inner := s[1 : len(s)-1]
		parts := strings.Split(inner, ",")
		if len(parts) != 2 {
			return 0, fmt.Errorf("invalid tag format: %s", s)
		}

		group, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 16, 16)
		if err != nil {
			return 0, fmt.Errorf("invalid group number in tag: %s", s)
		}

		element, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 16, 16)
		if err != nil {
			return 0, fmt.Errorf("invalid element number in tag: %s", s)
		}

		return New(uint16(group), uint16(element)), nil
	}

	// Try 0xGGGGEEEE format
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		val, err := strconv.ParseUint(s, 0, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid hex tag format: %s", s)
		}
		return Tag(val), nil
	}

	// Try GGGGEEEE format (8 hex digits)
	if len(s) == 8 {
		val, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid hex tag format: %s", s)
		}
		return Tag(val), nil
	}

	return 0, fmt.Errorf("invalid tag format: %s (expected (GGGG,EEEE) or 0xGGGGEEEE or GGGGEEEE)", s)
}

// CompareGroups compares the groups of two tags.
func CompareGroups(t1, t2 Tag) int {
	g1 := t1.Group()
	g2 := t2.Group()

	if g1 < g2 {
		return -1
	} else if g1 > g2 {
		return 1
	}
	return 0
}

// CompareElements compares the elements of two tags (assuming same group).
func CompareElements(t1, t2 Tag) int {
	e1 := t1.Element()
	e2 := t2.Element()

	if e1 < e2 {
		return -1
	} else if e1 > e2 {
		return 1
	}
	return 0
}
