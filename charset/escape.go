package charset

import (
	"bytes"
	"regexp"
)

// EscapeSequence represents an ISO 2022 escape sequence found in text.
type EscapeSequence struct {
	Sequence []byte
	Encoding string
	Position int
}

// Fragment represents a portion of text with a single encoding.
type Fragment struct {
	Data      []byte
	Encoding  string
	HasEscape bool
	Position  int
}

var (
	escapePattern = regexp.MustCompile(`(?:^[^\x1b]+|[\x1b][^\x1b]*)`)
)

// FindEscapeSequences finds all ISO 2022 escape sequences in the data.
func FindEscapeSequences(data []byte) []EscapeSequence {
	var sequences []EscapeSequence
	i := 0

	for i < len(data) {
		if data[i] == ESC {
			// Try to match 3-byte escape sequence
			if i+2 < len(data) {
				seq := data[i : i+3]
				if encoding := EscapeToGoEncoding(seq); encoding != "" {
					sequences = append(sequences, EscapeSequence{
						Sequence: seq,
						Encoding: encoding,
						Position: i,
					})
					i += 3
					continue
				}
			}

			// Try to match 4-byte escape sequence
			if i+3 < len(data) {
				seq := data[i : i+4]
				if encoding := EscapeToGoEncoding(seq); encoding != "" {
					sequences = append(sequences, EscapeSequence{
						Sequence: seq,
						Encoding: encoding,
						Position: i,
					})
					i += 4
					continue
				}
			}

			// Unknown escape sequence, skip it
			i++
		} else {
			i++
		}
	}

	return sequences
}

// SplitByEscapeSequences splits byte data into fragments based on escape sequences.
func SplitByEscapeSequences(data []byte) [][]byte {
	if !bytes.Contains(data, []byte{ESC}) {
		// No escape sequences, return data as single fragment
		return [][]byte{data}
	}

	matches := escapePattern.FindAll(data, -1)
	return matches
}

// ParseFragment parses a fragment and determines its encoding.
func ParseFragment(fragment []byte, baseEncoding string, position int) Fragment {
	if len(fragment) == 0 {
		return Fragment{
			Data:      fragment,
			Encoding:  baseEncoding,
			HasEscape: false,
			Position:  position,
		}
	}

	// Check if fragment starts with an escape sequence
	if fragment[0] == ESC {
		// Try 3-byte escape
		if len(fragment) >= 3 {
			seq := fragment[:3]
			if encoding := EscapeToGoEncoding(seq); encoding != "" {
				return Fragment{
					Data:      fragment,
					Encoding:  encoding,
					HasEscape: true,
					Position:  position,
				}
			}
		}

		// Try 4-byte escape
		if len(fragment) >= 4 {
			seq := fragment[:4]
			if encoding := EscapeToGoEncoding(seq); encoding != "" {
				return Fragment{
					Data:      fragment,
					Encoding:  encoding,
					HasEscape: true,
					Position:  position,
				}
			}
		}

		// Unknown escape sequence, use base encoding
		return Fragment{
			Data:      fragment,
			Encoding:  baseEncoding,
			HasEscape: true,
			Position:  position,
		}
	}

	// No escape sequence, use base encoding
	return Fragment{
		Data:      fragment,
		Encoding:  baseEncoding,
		HasEscape: false,
		Position:  position,
	}
}

// StripEscapeSequence removes the leading escape sequence from data.
func StripEscapeSequence(data []byte) ([]byte, []byte) {
	if len(data) == 0 || data[0] != ESC {
		return data, nil
	}

	// Try 4-byte escape first (longer sequences first)
	if len(data) >= 4 {
		seq := data[:4]
		if EscapeToGoEncoding(seq) != "" {
			return data[4:], seq
		}
	}

	// Try 3-byte escape
	if len(data) >= 3 {
		seq := data[:3]
		if EscapeToGoEncoding(seq) != "" {
			return data[3:], seq
		}
	}

	// Unknown escape sequence, don't strip
	return data, nil
}

// GetEscapeSequenceForEncoding returns the escape sequence for an encoding.
func GetEscapeSequenceForEncoding(encoding string, data []byte) []byte {
	// Special handling for Shift-JIS
	if encoding == "Shift_JIS" {
		// If data starts with byte >= 0x80, use Katakana escape
		if len(data) > 0 && data[0] >= 0x80 {
			return []byte{ESC, ')', 'I'} // Katakana
		}
		return []byte{ESC, '(', 'J'} // Romaji
	}

	// For other encodings, return the standard escape sequence
	return GoEncodingToEscape(encoding)
}

// NeedsEscapeSequence determines if an escape sequence is needed when switching to the given encoding.
func NeedsEscapeSequence(encoding string, isFirst bool, isPythonHandled bool) bool {
	// First encoding doesn't need escape sequence
	if isFirst {
		return false
	}

	// Encodings that handle escape sequences themselves don't need manual prepending
	// (ISO-2022-JP, ISO-2022-CN handle escapes internally)
	if isPythonHandled {
		return false
	}

	// Check if this encoding has an escape sequence
	return len(GoEncodingToEscape(encoding)) > 0
}

// IsPythonHandledEncoding returns true if the encoding's decoder handles escape sequences.
func IsPythonHandledEncoding(encoding string) bool {
	switch encoding {
	case "ISO-2022-JP", "ISO-2022-CN":
		return true
	default:
		return false
	}
}

// GetReturnToASCIIEscape returns the escape sequence to return to ASCII.
// This is needed at the end of text for encodings that require it.
func GetReturnToASCIIEscape() []byte {
	return []byte{ESC, '(', 'B'}
}
