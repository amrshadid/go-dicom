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

// DecodeBytes decodes a byte slice using the specified encodings and delimiters.
func DecodeBytes(value []byte, encodings []string, delimiters DelimiterSet) (string, error) {
	return DecodeBytesWithContext(context.Background(), value, encodings, delimiters)
}

// DecodeBytesWithContext decodes bytes with context support for validation mode override.
func DecodeBytesWithContext(ctx context.Context, value []byte, encodings []string, delimiters DelimiterSet) (string, error) {
	if len(value) == 0 {
		return "", nil
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	validationMode := config.ReadingValidationModeFromContext(ctx)

	// If no escape sequences and single encoding, simple decode
	if !bytes.Contains(value, []byte{ESC}) {
		return decodeSimple(ctx, value, encodings[0], validationMode)
	}

	// Handle multi-byte encodings with escape sequences
	return decodeWithEscapeSequences(ctx, value, encodings, delimiters, validationMode)
}

// decodeSimple performs simple decoding without escape sequence handling.
func decodeSimple(ctx context.Context, value []byte, encoding string, validationMode config.ValidationMode) (string, error) {
	decoder := getDecoder(encoding)
	if decoder == nil {
		return handleDecodingError(ctx, encoding, value, "unknown encoding", validationMode)
	}

	result, err := decoder.Bytes(value)
	if err != nil {
		return handleDecodingError(ctx, encoding, value, err.Error(), validationMode)
	}

	return string(result), nil
}

// decodeWithEscapeSequences decodes data containing ISO 2022 escape sequences.
func decodeWithEscapeSequences(ctx context.Context, value []byte, encodings []string, delimiters DelimiterSet, validationMode config.ValidationMode) (string, error) {
	fragments := SplitByEscapeSequences(value)
	if len(fragments) == 0 {
		return "", nil
	}

	var result strings.Builder
	result.Grow(len(value)) // Pre-allocate approximate space

	for _, fragment := range fragments {
		if len(fragment) == 0 {
			continue
		}

		// Parse the fragment to determine encoding
		parsed := ParseFragment(fragment, encodings[0], 0)

		// Decode the fragment
		decoded, err := decodeFragment(ctx, parsed, encodings, delimiters, validationMode)
		if err != nil {
			return "", err
		}

		result.WriteString(decoded)
	}

	return result.String(), nil
}

// decodeFragment decodes a single fragment with the appropriate encoding.
func decodeFragment(ctx context.Context, fragment Fragment, encodings []string, delimiters DelimiterSet, validationMode config.ValidationMode) (string, error) {
	data := fragment.Data
	encoding := fragment.Encoding

	if fragment.HasEscape && !IsPythonHandledEncoding(encoding) {
		stripped, _ := StripEscapeSequence(data)
		data = stripped
	}

	if len(delimiters) > 0 {
		return decodeWithDelimiters(ctx, data, encoding, encodings[0], delimiters, validationMode)
	}

	return decodeSimple(ctx, data, encoding, validationMode)
}

// decodeWithDelimiters decodes data that may contain delimiters that reset encoding.
func decodeWithDelimiters(ctx context.Context, data []byte, currentEncoding, baseEncoding string, delimiters DelimiterSet, validationMode config.ValidationMode) (string, error) {
	delimPositions := findDelimiters(data, delimiters, currentEncoding)
	if len(delimPositions) == 0 {
		return decodeSimple(ctx, data, currentEncoding, validationMode)
	}

	var result strings.Builder
	result.Grow(len(data))

	start := 0
	for _, pos := range delimPositions {
		if pos > start {
			segment := data[start:pos]
			decoded, err := decodeSimple(ctx, segment, currentEncoding, validationMode)
			if err != nil {
				return "", err
			}
			result.WriteString(decoded)
		}

		result.WriteByte(data[pos])
		currentEncoding = baseEncoding
		start = pos + 1
	}

	if start < len(data) {
		segment := data[start:]
		decoded, err := decodeSimple(ctx, segment, currentEncoding, validationMode)
		if err != nil {
			return "", err
		}
		result.WriteString(decoded)
	}

	return result.String(), nil
}

// findDelimiters returns the positions of all delimiters in the data.
func findDelimiters(data []byte, delimiters DelimiterSet, encoding string) []int {
	positions := make([]int, 0)

	// A delimiter byte only delimits when it stands for itself. In the
	// double-byte sets the second byte of a character occupies the same range as
	// printable ASCII, so a scan that walks one byte at a time finds delimiters
	// inside characters and splits them in half.
	//
	// The Japanese name やまだ^たろう is the case that shows it: ま is 0x24 0x5E,
	// and 0x5E is the ^ that separates Person Name components. Splitting there
	// left the decoder mid-character and it lost the rest of the string.
	// The fragment may still carry the escape sequence that selected its
	// encoding. Those bytes are not text and must not be scanned: skipping them
	// is also what keeps the two-byte stride aligned to character boundaries.
	i := 0
	if stripped, _ := StripEscapeSequence(data); len(stripped) < len(data) {
		i = len(data) - len(stripped)
	}

	// The encoding here is named the way Go names it, which is what the fragment
	// carries — GetEncodingInfo takes the DICOM name and would find nothing.
	step := 1
	if info := GetEncodingInfoByGoName(encoding); info != nil && info.IsMultiByte() {
		step = 2
	}

	for ; i < len(data); i += step {
		if delimiters.Contains(data[i]) {
			positions = append(positions, i)
			// A delimiter returns the stream to the base set, so what follows is
			// single byte again until the next escape sequence says otherwise.
			step = 1
		}
	}
	return positions
}

// handleDecodingError handles decoding errors based on validation mode.
func handleDecodingError(ctx context.Context, encoding string, data []byte, errMsg string, validationMode config.ValidationMode) (string, error) {
	switch validationMode {
	case config.RAISE:
		return "", errors.NewDicomUnicodeDecodeError(encoding, data, errMsg)
	case config.WARN:
		msg := fmt.Sprintf("Failed to decode byte string with encoding '%s': %s", encoding, errMsg)
		logWarning(msg)
		return decodeWithReplacement(encoding, data), nil
	default:
		return decodeWithReplacement(encoding, data), nil
	}
}

// decodeWithReplacement decodes using replacement characters for invalid bytes.
func decodeWithReplacement(encoding string, data []byte) string {
	decoder := getDecoder(encoding)
	if decoder == nil {
		// Fallback to UTF-8 with replacement
		return string(data)
	}

	// Use the decoder's Replacement option if available
	result, _ := decoder.Bytes(data)
	return string(result)
}

// getDecoder returns a decoder for the given encoding name.
func getDecoder(encodingName string) *encoding.Decoder {
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

	return enc.NewDecoder()
}
