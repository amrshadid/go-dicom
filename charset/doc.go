// Package charset provides DICOM character set encoding and decoding support.
//
// This package implements character set handling according to DICOM PS3.5 specifications,
// supporting 30+ single-byte and multi-byte character sets including Japanese, Chinese,
// Korean, Arabic, Hebrew, Greek, and other international encodings.
//
// # Character Set Support
//
// Supported encodings:
//   - Western European: Latin-1 to Latin-4, Greek, Turkish
//   - Middle Eastern: Arabic, Hebrew
//   - Asian: Japanese (Shift-JIS, ISO-2022-JP), Chinese (GB18030, GBK, UTF-8), Korean (EUC-KR), Thai
//   - Cyrillic: Russian
//   - Default: ISO_IR 6 (ASCII/ISO-8859)
//
// # Key Features
//
//   - Automatic escape sequence handling for multi-byte character sets
//   - Support for code extensions (multi-valued Specific Character Set)
//   - Delimiter-based encoding reset (CR, LF, TAB, FF)
//   - Stand-alone encoding validation (UTF-8, GBK, GB18030)
//   - Configurable error handling (ignore, warn, raise)
//   - Performance optimization (caching, buffer pooling)
//   - Person Name support (alphabetic, ideographic, phonetic representations)
//
// # Quick Start
//
// Decoding DICOM text:
//
//	encodings, err := ConvertEncodings([]string{"ISO 2022 IR 87"})
//	if err != nil {
//	    return err
//	}
//	text, err := DecodeBytes(rawBytes, encodings, DefaultTextDelimiters)
//
// Encoding to DICOM:
//
//	encodings, _ := ConvertEncodings([]string{"ISO_IR 192"}) // UTF-8
//	encoded, err := EncodeString("Hello 世界", encodings)
//
// Decoding Person Names:
//
//	pn, err := DecodePersonName(rawBytes, encodings)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Family: %s, Given: %s\n", pn.FamilyName(), pn.GivenName())
//
// # DICOM Specific Character Set (0008,0005)
//
// The Specific Character Set tag (0008,0005) determines which character encoding
// is used for text values. If absent, the default encoding ISO_IR 6 (ASCII) is used.
//
// For multi-valued character sets (code extensions), the first value is the base
// character set, and additional values specify allowed escape sequences for
// switching to other character sets within the same text value.
//
// # Escape Sequences
//
// Multi-byte character sets use ISO 2022 escape sequences to switch between
// character sets. For example:
//
//	ESC ( B     - Switch to ASCII (ISO-IR 6)
//	ESC $ B     - Switch to JIS X 0208 (Japanese Kanji)
//	ESC $ ) C   - Switch to EUC-KR (Korean)
//
// The package automatically handles insertion and detection of escape sequences
// during encoding and decoding operations.
//
// # Thread Safety
//
// All functions in this package are safe for concurrent use. The package uses
// immutable data structures and does not maintain mutable global state beyond
// static encoding mappings.
//
// # Integration with Config Module
//
// The charset package integrates with the config module to respect validation
// modes:
//
//   - config.IGNORE: Invalid encodings use default, no errors
//   - config.WARN: Invalid encodings use default with warnings
//   - config.RAISE: Invalid encodings return errors
//
// # References
//
//   - DICOM PS3.5 Section 6.1: Character Sets and Person Name Component Groups
//   - DICOM PS3.5 Table C.12-2 to C.12-5: Defined Terms for Specific Character Set
//   - ISO/IEC 2022: Character code structure and extension techniques
package charset
