# Charset

DICOM character set encoding and decoding with support for 30+ international character encodings, ISO 2022 escape sequences, and PersonName component groups.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/charset"

// Decode DICOM text
encodings, _ := charset.ConvertEncodings([]string{"ISO_IR 192"}) // UTF-8
text, _ := charset.DecodeBytes(rawBytes, encodings, charset.DefaultTextDelimiters)

// Encode to DICOM
encoded, _ := charset.EncodeString("Hello", encodings)

// PersonName handling
pn := charset.FromNamedComponents("Yamada^Tarou", "山田^太郎", "やまだ^たろう")
encoded, _ := charset.EncodePersonName(pn, []string{"UTF-8"})
```

## API Reference

```go
// Encoding conversion
func ConvertEncodings(values []string) ([]string, error)
func ValidateEncoding(encoding string) error

// Decode/encode
func DecodeBytes(value []byte, encodings []string, delimiters DelimiterSet) (string, error)
func EncodeString(value string, encodings []string) ([]byte, error)

// PersonName
func DecodePersonName(value []byte, encodings []string) (*PersonName, error)
func EncodePersonName(pn *PersonName, encodings []string) ([]byte, error)
func FromComponents(familyName, givenName, middleName, prefix, suffix string) *PersonName

// Performance optimized
func DecodeBytesWithCache(value []byte, encodings []string, delimiters DelimiterSet) (string, error)
func BatchDecodeBytes(values [][]byte, encodings []string, delimiters DelimiterSet) ([]string, error)
func NewStreamDecoder(encodings []string, delimiters DelimiterSet, chunkSize int) *StreamDecoder

// Helpers
func GetSupportedEncodings() []string
func GetEncodingDescription(encoding string) string

// Types
type CharacterSet struct { ... }
func NewCharacterSet(values []string) (*CharacterSet, error)

type PersonName struct {
    Alphabetic  string
    Ideographic string
    Phonetic    string
}

type Cache struct { ... }
func NewCache(maxSize int) *Cache
func GetDefaultCache() *Cache
func EnableCache(enabled bool)
```

## References

- [DICOM PS3.5 Section 6](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Character Sets and Person Name Component Groups
- ISO/IEC 2022 - Character code structure and extension techniques
