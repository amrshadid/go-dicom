# Element

Encoding, decoding, and conversion of DICOM element values with byte order handling, DICOM format parsing (dates, person names, numeric strings), and value padding.

## Quick Start

```go
import (
    "github.com/amrshadid/go-dicom/element"
    "github.com/amrshadid/go-dicom/filebase"
)

encoder := element.NewValueEncoder(filebase.LittleEndian)
bytes := encoder.EncodeUint32(0x12345678)
val, _ := encoder.DecodeUint32(bytes)

parser := element.NewValueParser()
name := parser.ParsePersonName("Smith^John^A^Dr^Jr")
date, _ := parser.ParseDate("20231225")

padder := element.NewValuePadder()
padded := padder.Pad([]byte{0x01}, dataelem.AE) // pads to even length
```

## API Reference

```go
// Encoder
func NewValueEncoder(byteOrder filebase.ByteOrder) *ValueEncoder
func (ve *ValueEncoder) EncodeString(value string) []byte
func (ve *ValueEncoder) EncodeUint16/EncodeUint32/EncodeInt16/EncodeInt32/EncodeFloat32/EncodeFloat64
func (ve *ValueEncoder) DecodeString(data []byte) string
func (ve *ValueEncoder) DecodeUint16/DecodeUint32/DecodeInt16/DecodeInt32/DecodeFloat32/DecodeFloat64
func (ve *ValueEncoder) EncodeMultipleValues(values []string) []byte
func (ve *ValueEncoder) DecodeMultipleValues(data []byte) []string

// Parser
func NewValueParser() *ValueParser
func (vp *ValueParser) ParseIntegerString(value string) (int64, error)
func (vp *ValueParser) ParseDecimalString(value string) (float64, error)
func (vp *ValueParser) ParseDate(value string) (string, error)
func (vp *ValueParser) ParseTime(value string) (string, error)
func (vp *ValueParser) ParsePersonName(value string) map[string]string

// Padder
func NewValuePadder() *ValuePadder
func (vp *ValuePadder) Pad(value []byte, vr dataelem.VR) []byte
func (vp *ValuePadder) Unpad(value []byte, vr dataelem.VR) []byte
func (vp *ValuePadder) GetPadByte(vr dataelem.VR) byte
func (vp *ValuePadder) ValueMultiplicity(value []byte, vr dataelem.VR) int

// Converter (combines all three)
func NewValueConverter(byteOrder filebase.ByteOrder) *ValueConverter
func (vc *ValueConverter) ConvertToString(value interface{}, vr dataelem.VR) (string, error)
func (vc *ValueConverter) ConvertToBytes(value interface{}, vr dataelem.VR) ([]byte, error)

func ValidateLength(value []byte, vr dataelem.VR) error
```

## References

- [DICOM PS3.5](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Value encoding, padding rules, date/time/person name formats
