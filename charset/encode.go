package charset

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/errors"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// EncodeString encodes a Unicode string to bytes using the specified encodings.
func EncodeString(value string, encodings []string) ([]byte, error) {
	return EncodeStringWithContext(context.Background(), value, encodings)
}

// EncodeStringWithContext encodes a string with context support for validation mode override.
func EncodeStringWithContext(ctx context.Context, value string, encodings []string) ([]byte, error) {
	if value == "" {
		return []byte{}, nil
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	validationMode := config.WritingValidationModeFromContext(ctx)

	// Try encoding with first encoding
	result, err := encodeSimple(ctx, value, encodings[0], validationMode)
	if err == nil {
		// Success with single encoding
		// Add tail escape if needed
		if RequiresTailEscape(encodings[0]) {
			result = append(result, GetReturnToASCIIEscape()...)
		}
		return result, nil
	}

	// If single encoding failed and we have multiple encodings, try multi-part encoding
	if len(encodings) > 1 {
		result, err = encodeDelimited(ctx, value, encodings, validationMode)
		if err == nil {
			return result, nil
		}
	}

	// All attempts failed
	return handleEncodingError(ctx, value, encodings, err, validationMode)
}

// encodeSimple performs simple encoding without escape sequences.
func encodeSimple(ctx context.Context, value string, encoding string, validationMode config.ValidationMode) ([]byte, error) {
	encoder := getEncoder(encoding)
	if encoder == nil {
		return nil, fmt.Errorf("unknown encoding: %s", encoding)
	}

	result, err := encoder.Bytes([]byte(value))
	if err != nil {
		return nil, err
	}

	return result, nil
}

// codeExtensionDelimiters are where PS3.5 6.1.2.5.3 returns the code element
// designations to their initial state: the PN component separators and the
// value and line separators of text. EncodeString is not told the VR, so it
// resets at all of them; a reset a VR does not call for only repeats an escape.
const codeExtensionDelimiters = "^=\\\r\n\t\f"

// encodeDelimited encodes each run between delimiters from the initial state,
// so a set other than the first is introduced by its escape in every
// component that uses it. Encoding the value as one run put a single escape at
// its start, and a reader resetting at "=" as the standard says read the
// Korean after it as the default set (#197).
func encodeDelimited(ctx context.Context, value string, encodings []string, validationMode config.ValidationMode) ([]byte, error) {
	var result []byte
	for value != "" {
		end := strings.IndexAny(value, codeExtensionDelimiters)
		piece := value
		if end >= 0 {
			piece = value[:end]
		}
		if piece != "" {
			encoded, err := encodeSimple(ctx, piece, encodings[0], validationMode)
			if err != nil {
				if encoded, err = encodeStringParts(ctx, piece, encodings, validationMode); err != nil {
					return nil, err
				}
			}
			result = append(result, encoded...)
		}
		if end < 0 {
			break
		}
		// The delimiters are ASCII in every set DICOM allows here.
		result = append(result, value[end])
		value = value[end+1:]
	}
	return result, nil
}

// encodeStringParts encodes a string using multiple encodings, trying each
// encoding in sequence and automatically inserting escape sequences.
func encodeStringParts(ctx context.Context, value string, encodings []string, validationMode config.ValidationMode) ([]byte, error) {
	var result []byte
	remaining := value
	currentEncoding := encodings[0]

	for len(remaining) > 0 {
		// Find the longest substring that can be encoded with any encoding
		encoded, usedEncoding, consumed := findLongestEncodable(remaining, encodings)
		if consumed == 0 {
			// Can't encode any part - fail
			return nil, fmt.Errorf("cannot encode character at position %d", len(value)-len(remaining))
		}

		// Add escape sequence if switching encodings. Not for an encoder that
		// writes its own: ISO-2022-JP's already begins with ESC $ B, and the
		// table maps both IR 87 and IR 159 to it, so what was prepended could
		// be IR 159's ESC $ ( D in front of IR 87 text (#197).
		if usedEncoding != currentEncoding {
			if !IsPythonHandledEncoding(usedEncoding) {
				result = append(result, GetEscapeSequenceForEncoding(usedEncoding, encoded)...)
			}
			currentEncoding = usedEncoding
		}

		// Add the encoded data
		result = append(result, encoded...)

		// Move to next part. consumed counts runes, as findLongestEncodable
		// does; cutting that many bytes split a UTF-8 character, the next
		// encode failed, and the value fell back to raw UTF-8 (#197).
		remaining = string([]rune(remaining)[consumed:])
	}

	// Add tail escape if the last encoding requires it and has not already.
	if RequiresTailEscape(currentEncoding) && !bytes.HasSuffix(result, GetReturnToASCIIEscape()) {
		result = append(result, GetReturnToASCIIEscape()...)
	}

	return result, nil
}

// findLongestEncodable finds the longest prefix of the string that can be encoded with one of the given encodings.
func findLongestEncodable(s string, encodings []string) (encoded []byte, encoding string, consumed int) {
	maxConsumed := 0
	var maxEncoded []byte
	maxEncoding := ""

	// Try each encoding
	for _, enc := range encodings {
		encoder := getEncoder(enc)
		if encoder == nil {
			continue
		}

		// Try encoding increasingly longer prefixes
		runes := []rune(s)
		for i := 1; i <= len(runes); i++ {
			prefix := string(runes[:i])
			encoded, err := encoder.Bytes([]byte(prefix))
			if err == nil {
				// Successfully encoded this prefix
				if i > maxConsumed {
					maxConsumed = i
					maxEncoded = encoded
					maxEncoding = enc
				}
			} else {
				// Failed, try next encoding
				break
			}
		}
	}

	return maxEncoded, maxEncoding, maxConsumed
}

// handleEncodingError handles encoding errors based on validation mode.
func handleEncodingError(ctx context.Context, value string, encodings []string, err error, validationMode config.ValidationMode) ([]byte, error) {
	switch validationMode {
	case config.RAISE:
		if err != nil {
			return nil, err
		}
		return nil, errors.NewDicomUnicodeDecodeError(encodings[0], []byte(value), "Failed to encode value")
	case config.WARN:
		msg := fmt.Sprintf("Failed to encode value with encodings: %v", encodings)
		logWarning(msg)
		return encodeWithReplacement(value, encodings[0]), nil
	default:
		return encodeWithReplacement(value, encodings[0]), nil
	}
}

// encodeWithReplacement encodes using replacement characters for unencodable characters.
func encodeWithReplacement(value string, encoding string) []byte {
	encoder := getEncoder(encoding)
	if encoder == nil {
		// Fallback to UTF-8
		return []byte(value)
	}

	// Try encoding with replacement
	result, err := encoder.Bytes([]byte(value))
	if err != nil {
		// Final fallback
		return []byte(value)
	}

	return result
}

// getEncoder returns an encoder for the given encoding name.
func getEncoder(encodingName string) *encoding.Encoder {
	var enc encoding.Encoding

	switch encodingName {
	// Unicode
	case "UTF-8":
		enc = unicode.UTF8

	// Western European (ISO-8859-1 through ISO-8859-4)
	case "ISO-8859-1":
		enc = charmap.ISO8859_1
	case "ISO-8859-2":
		enc = charmap.ISO8859_2
	case "ISO-8859-3":
		enc = charmap.ISO8859_3
	case "ISO-8859-4":
		enc = charmap.ISO8859_4

	// Cyrillic
	case "ISO-8859-5":
		enc = charmap.ISO8859_5

	// Arabic
	case "ISO-8859-6":
		enc = charmap.ISO8859_6

	// Greek
	case "ISO-8859-7":
		enc = charmap.ISO8859_7

	// Hebrew
	case "ISO-8859-8":
		enc = charmap.ISO8859_8

	// Turkish
	case "ISO-8859-9":
		enc = charmap.ISO8859_9

	// Thai
	case "ISO-8859-11":
		// Note: Go's charmap doesn't have ISO-8859-11, fallback to UTF-8
		enc = unicode.UTF8

	// Japanese
	case "Shift_JIS":
		enc = japanese.ShiftJIS
	case "ISO-2022-JP":
		enc = japanese.ISO2022JP
	case "EUC-JP":
		enc = japanese.EUCJP

	// Korean
	case "EUC-KR":
		enc = korean.EUCKR

	// Chinese
	case "GB18030":
		enc = simplifiedchinese.GB18030
	case "GBK":
		enc = simplifiedchinese.GBK
	case "ISO-2022-CN":
		// Note: golang.org/x/text doesn't have ISO-2022-CN
		// Use GB18030 as fallback
		enc = simplifiedchinese.GB18030
	case "GB2312":
		enc = simplifiedchinese.HZGB2312

	default:
		return nil
	}

	if enc == nil {
		return nil
	}

	return enc.NewEncoder()
}
