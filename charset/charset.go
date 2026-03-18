package charset

import (
	"context"
	"fmt"
	"strings"
)

// DefaultEncoding is the default character encoding when no Specific Character Set is specified.
const DefaultEncoding = "ISO-8859-1"

// Delimiter represents characters that reset encoding in multi-encoding scenarios.
type Delimiter byte

// Delimiter constants.
const (
	DelimiterLF  Delimiter = 0x0A // Line Feed
	DelimiterCR  Delimiter = 0x0D // Carriage Return
	DelimiterTAB Delimiter = 0x09 // Tab
	DelimiterFF  Delimiter = 0x0C // Form Feed
	DelimiterCAR Delimiter = 0x5E // Caret (^)
)

// DefaultTextDelimiters is the set of delimiters used for general text VRs.
var DefaultTextDelimiters = NewDelimiterSet(
	DelimiterLF,
	DelimiterCR,
	DelimiterTAB,
	DelimiterFF,
)

// PersonNameDelimiters is the set of delimiters used for Person Name (PN) VR.
var PersonNameDelimiters = NewDelimiterSet(
	DelimiterLF,
	DelimiterCR,
	DelimiterTAB,
	DelimiterFF,
	DelimiterCAR,
)

// DelimiterSet is a set of delimiters for fast lookup.
type DelimiterSet map[byte]struct{}

// NewDelimiterSet creates a new delimiter set from the given delimiters.
func NewDelimiterSet(delims ...Delimiter) DelimiterSet {
	set := make(DelimiterSet, len(delims))
	for _, d := range delims {
		set[byte(d)] = struct{}{}
	}
	return set
}

// Contains checks if a byte is in the delimiter set.
func (ds DelimiterSet) Contains(b byte) bool {
	_, exists := ds[b]
	return exists
}

// EncodingType represents the type of character encoding.
type EncodingType int

const (
	// EncodingTypeSingleByte is for single-byte character encodings.
	EncodingTypeSingleByte EncodingType = iota
	// EncodingTypeMultiByte is for multi-byte character encodings.
	EncodingTypeMultiByte
	// EncodingTypeUnicode is for Unicode encodings.
	EncodingTypeUnicode
	// EncodingTypeStandAlone is for stand-alone encodings without code extensions.
	EncodingTypeStandAlone
)

// EncodingInfo contains metadata about a character encoding.
type EncodingInfo struct {
	DicomName          string
	GoEncoding         string
	Type               EncodingType
	Description        string
	EscapeSequence     []byte
	RequiresTailEscape bool
}

// String returns a string representation of the encoding info.
func (ei *EncodingInfo) String() string {
	if ei == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s (%s)", ei.DicomName, ei.GoEncoding)
}

// IsStandAlone returns true if this is a stand-alone encoding that cannot
// be used with code extensions.
func (ei *EncodingInfo) IsStandAlone() bool {
	return ei.Type == EncodingTypeStandAlone
}

// IsMultiByte returns true if this is a multi-byte encoding.
func (ei *EncodingInfo) IsMultiByte() bool {
	return ei.Type == EncodingTypeMultiByte
}

// CharacterSet represents a DICOM character set configuration.
type CharacterSet struct {
	OriginalValues []string
	Encodings      []string
	EncodingInfos  []*EncodingInfo
	IsDefault      bool
}

// NewCharacterSet creates a new CharacterSet from DICOM Specific Character Set values.
func NewCharacterSet(values []string) (*CharacterSet, error) {
	return NewCharacterSetWithContext(context.Background(), values)
}

// NewCharacterSetWithContext creates a new CharacterSet with context support for validation mode override.
func NewCharacterSetWithContext(ctx context.Context, values []string) (*CharacterSet, error) {
	if len(values) == 0 || (len(values) == 1 && strings.TrimSpace(values[0]) == "") {
		return &CharacterSet{
			OriginalValues: []string{""},
			Encodings:      []string{DefaultEncoding},
			EncodingInfos:  []*EncodingInfo{GetEncodingInfo("")},
			IsDefault:      true,
		}, nil
	}

	encodings, err := ConvertEncodingsWithContext(ctx, values)
	if err != nil {
		return nil, err
	}

	infos := make([]*EncodingInfo, len(encodings))
	for i, enc := range encodings {
		infos[i] = GetEncodingInfoByGoName(enc)
	}

	return &CharacterSet{
		OriginalValues: values,
		Encodings:      encodings,
		EncodingInfos:  infos,
		IsDefault:      false,
	}, nil
}

// PrimaryEncoding returns the primary (first) encoding.
func (cs *CharacterSet) PrimaryEncoding() string {
	if len(cs.Encodings) == 0 {
		return DefaultEncoding
	}
	return cs.Encodings[0]
}

// SupportsCodeExtensions returns true if this character set supports
// code extensions (multiple encodings with escape sequences).
func (cs *CharacterSet) SupportsCodeExtensions() bool {
	return len(cs.Encodings) > 1
}

// String returns a string representation of the character set.
func (cs *CharacterSet) String() string {
	if cs.IsDefault {
		return "Default (ISO-IR 6)"
	}
	return fmt.Sprintf("CharacterSet{%v}", cs.OriginalValues)
}
