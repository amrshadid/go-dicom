// Package element provides encoding, decoding, and conversion functionality for DICOM element values.
//
// This package implements value encoding/decoding with proper byte order handling, parsing of
// DICOM-specific value formats (dates, times, person names, decimal strings), and value
// padding according to DICOM standards. It serves as the value processing layer for all
// DICOM data element operations.
//
// # Core Concepts
//
// ## ValueEncoder
//
// Encodes and decodes values to/from bytes with configurable byte order:
//   - Encode methods: EncodeString, EncodeUint16, EncodeUint32, EncodeInt16, EncodeInt32,
//     EncodeFloat32, EncodeFloat64, EncodeMultipleValues
//   - Decode methods: DecodeString, DecodeUint16, DecodeUint32, DecodeInt16, DecodeInt32,
//     DecodeFloat32, DecodeFloat64, DecodeMultipleValues
//   - Handles both little-endian and big-endian byte orders
//
// ## ValueParser
//
// Parses DICOM-specific string formats:
//   - ParseIntegerString (IS): Parses integer strings
//   - ParseDecimalString (DS): Parses decimal/floating-point strings
//   - ParseDate: Validates date format (YYYYMMDD)
//   - ParseTime: Validates time format (HHMMSS or HHMMSSFFFFFF)
//   - ParsePersonName: Parses person name components separated by ^
//
// ## ValuePadder
//
// Handles padding of DICOM values to even length:
//   - GetPadByte: Returns appropriate padding byte for VR (0x20 or 0x00)
//   - Pad: Pads value to even length
//   - Unpad: Removes padding from value
//   - ValueMultiplicity: Calculates number of values (for backslash-separated values)
//
// ## ValueConverter
//
// Combines encoder, parser, and padder for comprehensive value conversion:
//   - ConvertToString: Convert values to string representation
//   - ConvertToBytes: Convert values to binary representation
//   - ValidateLength: Validate value length for VR
//
// ## Value Representation (VR)
//
// DICOM defines 27 Value Representations with different encoding rules:
//   - Text VRs: AE, AS, CS, DA, DS, DT, LO, LT, PN, SH, ST, UC, UI, UR, UT
//   - Binary VRs: OB, OD, OF, OL, OW, UN
//   - Numeric VRs: FL, FD, IS, SL, SS, UL, US
//   - Other: AT, SQ
//
// # Basic Usage
//
// ## Encoding Values
//
//	import (
//	    "log"
//	    "github.com/amrshadid/go-dicom/element"
//	    "github.com/amrshadid/go-dicom/filebase"
//	)
//
//	func encodeValues() {
//	    // Create encoder with byte order
//	    encoder := element.NewValueEncoder(filebase.LittleEndian)
//
//	    // Encode string
//	    strBytes := encoder.EncodeString("Smith")
//	    fmt.Printf("String: %v\n", strBytes)
//
//	    // Encode unsigned integer
//	    u32Bytes := encoder.EncodeUint32(12345)
//	    fmt.Printf("Uint32: %v\n", u32Bytes)
//
//	    // Encode multiple values
//	    multiBytes := encoder.EncodeMultipleValues([]string{"A", "B", "C"})
//	    fmt.Printf("Multiple: %v\n", multiBytes)  // "A\B\C"
//	}
//
// ## Decoding Values
//
//	func decodeValues() {
//	    encoder := element.NewValueEncoder(filebase.LittleEndian)
//
//	    // Decode string
//	    str := encoder.DecodeString([]byte("Smith   "))
//	    fmt.Printf("Decoded: %s\n", str)  // "Smith"
//
//	    // Decode integer
//	    v, _ := encoder.DecodeUint16([]byte{0x34, 0x12})
//	    fmt.Printf("Uint16: 0x%04x\n", v)  // 0x1234
//	}
//
// ## Parsing Specific Formats
//
//	func parseFormats() {
//	    parser := element.NewValueParser()
//
//	    // Parse integer string (IS)
//	    i, _ := parser.ParseIntegerString("  123  ")
//	    fmt.Printf("IS: %d\n", i)  // 123
//
//	    // Parse decimal string (DS)
//	    f, _ := parser.ParseDecimalString("3.14159")
//	    fmt.Printf("DS: %f\n", f)  // 3.141590
//
//	    // Parse date (DA)
//	    date, _ := parser.ParseDate("20231225")
//	    fmt.Printf("Date: %s\n", date)  // "20231225"
//
//	    // Parse person name (PN)
//	    name := parser.ParsePersonName("Smith^John^A^Dr^Jr")
//	    fmt.Printf("Family: %s, Given: %s\n", name["FamilyName"], name["GivenName"])
//	}
//
// ## Value Padding
//
//	func padValues() {
//	    padder := element.NewValuePadder()

//	    // Pad odd-length value
//	    padded := padder.Pad([]byte{0x01}, dataelem.AE)
//	    fmt.Printf("Length: %d\n", len(padded))  // 2
//
//	    // Unpad value
//	    unpadded := padder.Unpad([]byte{0x01, 0x20}, dataelem.AE)
//	    fmt.Printf("Length: %d\n", len(unpadded))  // 1
//
//	    // Get multiplicity
//	    mult := padder.ValueMultiplicity([]byte("A\\B\\C"), dataelem.AE)
//	    fmt.Printf("Count: %d\n", mult)  // 3
//	}
//
// # Advanced Usage
//
// ## Byte Order Handling
//
// Different DICOM transfer syntaxes use different byte orders:
//
//	// Little-endian (Implicit VR, Explicit VR LE)
//	leEncoder := element.NewValueEncoder(filebase.LittleEndian)
//	leBytes := leEncoder.EncodeUint32(0x12345678)
//	// Result: {0x78, 0x56, 0x34, 0x12}
//
//	// Big-endian (Explicit VR BE)
//	beEncoder := element.NewValueEncoder(filebase.BigEndian)
//	beBytes := beEncoder.EncodeUint32(0x12345678)
//	// Result: {0x12, 0x34, 0x56, 0x78}
//
// ## Value Converter for Complete Processing
//
// Combine encoding, parsing, and padding:
//
//	converter := element.NewValueConverter(filebase.LittleEndian)
//
//	// Get component encoders
//	encoder := converter.GetEncoder()
//	parser := converter.GetParser()
//	padder := converter.GetPadder()
//
//	// Convert to string
//	str, _ := converter.ConvertToString([]byte("Hello"), dataelem.LO)
//
//	// Convert to bytes
//	bytes, _ := converter.ConvertToBytes("Hello", dataelem.LO)
//
// ## Validation
//
// Validate value properties before encoding:
//
//	// Check even length (except for binary VRs)
//	if err := element.ValidateLength([]byte("Hello"), dataelem.LO); err != nil {
//	    log.Fatal(err)
//	}
//
// # Data Structures
//
// ## ValueEncoder
//
//	type ValueEncoder struct {
//	    // Unexported field:
//	    // - byteOrder: ByteOrder (LittleEndian or BigEndian)
//	}
//
// Encodes/decodes values with byte order support.
//
// ## ValueParser
//
//	type ValueParser struct {
//	    // No internal state
//	}
//
// Parses DICOM-specific string formats (stateless).
//
// ## ValuePadder
//
//	type ValuePadder struct {
//	    // No internal state
//	}
//
// Handles value padding (stateless).
//
// ## ValueConverter
//
//	type ValueConverter struct {
//	    // Unexported fields:
//	    // - encoder: *ValueEncoder
//	    // - parser: *ValueParser
//	    // - padder: *ValuePadder
//	}
//
// Combines encoder, parser, and padder for complete value processing.
//
// # API Reference
//
// ## ValueEncoder Creation
//
// ### NewValueEncoder
//
//	func NewValueEncoder(byteOrder filebase.ByteOrder) *ValueEncoder
//
// Creates new ValueEncoder with specified byte order.
//
// **Parameters:**
// - `byteOrder`: filebase.LittleEndian or filebase.BigEndian
//
// **Returns:** ValueEncoder pointer
//
// **Example:**
// ```go
// enc := element.NewValueEncoder(filebase.LittleEndian)
// ```
//
// ## Encoding Methods
//
// ### EncodeString
//
//	func (ve *ValueEncoder) EncodeString(value string) []byte
//
// Encodes string to bytes (direct conversion).
//
// ### EncodeUint16
//
//	func (ve *ValueEncoder) EncodeUint16(value uint16) []byte
//
// Encodes uint16 with configured byte order (returns 2 bytes).
//
// ### EncodeUint32
//
//	func (ve *ValueEncoder) EncodeUint32(value uint32) []byte
//
// Encodes uint32 with configured byte order (returns 4 bytes).
//
// ### EncodeInt16
//
//	func (ve *ValueEncoder) EncodeInt16(value int16) []byte
//
// Encodes int16 with configured byte order (returns 2 bytes).
//
// ### EncodeInt32
//
//	func (ve *ValueEncoder) EncodeInt32(value int32) []byte
//
// Encodes int32 with configured byte order (returns 4 bytes).
//
// ### EncodeFloat32
//
//	func (ve *ValueEncoder) EncodeFloat32(value float32) []byte
//
// Encodes float32 with configured byte order (returns 4 bytes).
//
// ### EncodeFloat64
//
//	func (ve *ValueEncoder) EncodeFloat64(value float64) []byte
//
// Encodes float64 with configured byte order (returns 8 bytes).
//
// ### EncodeMultipleValues
//
//	func (ve *ValueEncoder) EncodeMultipleValues(values []string) []byte
//
// Encodes multiple values separated by backslash.
//
// ## Decoding Methods
//
// ### DecodeString
//
//	func (ve *ValueEncoder) DecodeString(data []byte) string
//
// Decodes bytes to string, trimming whitespace.
//
// ### DecodeUint16
//
//	func (ve *ValueEncoder) DecodeUint16(data []byte) (uint16, error)
//
// Decodes 2 bytes to uint16 with configured byte order.
//
// ### DecodeUint32
//
//	func (ve *ValueEncoder) DecodeUint32(data []byte) (uint32, error)
//
// Decodes 4 bytes to uint32 with configured byte order.
//
// ### DecodeInt16
//
//	func (ve *ValueEncoder) DecodeInt16(data []byte) (int16, error)
//
// Decodes 2 bytes to int16 with configured byte order.
//
// ### DecodeInt32
//
//	func (ve *ValueEncoder) DecodeInt32(data []byte) (int32, error)
//
// Decodes 4 bytes to int32 with configured byte order.
//
// ### DecodeFloat32
//
//	func (ve *ValueEncoder) DecodeFloat32(data []byte) (float32, error)
//
// Decodes 4 bytes to float32 with configured byte order.
//
// ### DecodeFloat64
//
//	func (ve *ValueEncoder) DecodeFloat64(data []byte) (float64, error)
//
// Decodes 8 bytes to float64 with configured byte order.
//
// ### DecodeMultipleValues
//
//	func (ve *ValueEncoder) DecodeMultipleValues(data []byte) []string
//
// Decodes backslash-separated values to string slice.
//
// ## ValueParser Methods
//
// ### NewValueParser
//
//	func NewValueParser() *ValueParser
//
// Creates new ValueParser.
//
// ### ParseIntegerString
//
//	func (vp *ValueParser) ParseIntegerString(value string) (int64, error)
//
// Parses IS (Integer String) value (e.g., "  123  ").
//
// ### ParseDecimalString
//
//	func (vp *ValueParser) ParseDecimalString(value string) (float64, error)
//
// Parses DS (Decimal String) value (e.g., "3.14159").
//
// ### ParseDate
//
//	func (vp *ValueParser) ParseDate(value string) (string, error)
//
// Validates and returns DA (Date) in YYYYMMDD format.
//
// ### ParseTime
//
//	func (vp *ValueParser) ParseTime(value string) (string, error)
//
// Validates TM (Time) format (HHMMSS or HHMMSSFFFFFF).
//
// ### ParsePersonName
//
//	func (vp *ValueParser) ParsePersonName(value string) map[string]string
//
// Parses PN (Person Name) with components: FamilyName, GivenName, MiddleName, NamePrefix, NameSuffix.
//
// ## ValuePadder Methods
//
// ### NewValuePadder
//
//	func NewValuePadder() *ValuePadder
//
// Creates new ValuePadder.
//
// ### GetPadByte
//
//	func (vp *ValuePadder) GetPadByte(vr dataelem.VR) byte
//
// Returns padding byte for VR (0x20 for text, 0x00 for binary).
//
// ### Pad
//
//	func (vp *ValuePadder) Pad(value []byte, vr dataelem.VR) []byte
//
// Pads value to even length.
//
// ### Unpad
//
//	func (vp *ValuePadder) Unpad(value []byte, vr dataelem.VR) []byte
//
// Removes padding from value.
//
// ### ValueMultiplicity
//
//	func (vp *ValuePadder) ValueMultiplicity(value []byte, vr dataelem.VR) int
//
// Counts values (for backslash-separated values).
//
// ## ValueConverter Methods
//
// ### NewValueConverter
//
//	func NewValueConverter(byteOrder filebase.ByteOrder) *ValueConverter
//
// Creates ValueConverter with encoder, parser, and padder.
//
// ### GetEncoder/GetParser/GetPadder
//
//	func (vc *ValueConverter) GetEncoder() *ValueEncoder
//	func (vc *ValueConverter) GetParser() *ValueParser
//	func (vc *ValueConverter) GetPadder() *ValuePadder
//
// Returns component converters.
//
// ### ConvertToString
//
//	func (vc *ValueConverter) ConvertToString(value interface{}, vr dataelem.VR) (string, error)
//
// Converts value to string representation.
//
// ### ConvertToBytes
//
//	func (vc *ValueConverter) ConvertToBytes(value interface{}, vr dataelem.VR) ([]byte, error)
//
// Converts value to binary representation.
//
// ### ValidateLength
//
//	func ValidateLength(value []byte, vr dataelem.VR) error
//
// Validates value length for VR (even length except for binary VRs).
//
// # Performance Characteristics
//
// | Operation | Complexity | Description |
// |-----------|-----------|-------------|
// | NewValueEncoder | O(1) | Simple initialization |
// | NewValueParser | O(1) | Simple initialization |
// | NewValuePadder | O(1) | Simple initialization |
// | NewValueConverter | O(1) | Creates 3 components |
// | EncodeString | O(n) | n = string length |
// | EncodeUint16/32 | O(1) | Fixed size |
// | EncodeFloat32/64 | O(1) | Fixed size |
// | DecodeString | O(n) | n = data length |
// | DecodeUint16/32 | O(1) | Fixed size + validation |
// | DecodeFloat32/64 | O(1) | Fixed size + conversion |
// | ParseIntegerString | O(n) | n = string length |
// | ParseDecimalString | O(n) | n = string length |
// | ParseDate | O(1) | Fixed length validation |
// | ParseTime | O(n) | n = string length |
// | ParsePersonName | O(n) | n = string length (split on ^) |
// | Pad | O(1) | Append 1 byte if needed |
// | Unpad | O(k) | k = trailing padding bytes |
// | ValueMultiplicity | O(n) | n = data length (count backslashes) |
// | ValidateLength | O(1) | Length check |
//
// # Padding Rules
//
// DICOM requires values to have even length. Padding rules by VR:
//   - Text VRs (AE, AS, CS, DA, DS, DT, LO, LT, PN, SH, ST, UC, UI, UR, UT): Pad with 0x20 (space)
//   - Binary VRs (OB, OD, OF, OL, OW, UN): Pad with 0x00 (null)
//   - Numeric VRs: Pad with 0x20 (space)
//
// # Byte Order Handling
//
// Multi-byte integer encoding depends on byte order:
//   - LittleEndian (most common): Least significant byte first
//   - BigEndian (Explicit VR BE): Most significant byte first
//
// # Use Cases
//
// ## Value Encoding for DICOM Writing
//
// Encode values when writing DICOM files.
//
// ## Value Decoding for DICOM Reading
//
// Decode values when reading DICOM files.
//
// ## Format Validation
//
// Validate dates, times, and other specific formats.
//
// ## Person Name Parsing
//
// Parse person names into components for display/search.
//
// ## Value Padding Handling
//
// Ensure values meet DICOM even-length requirement.
//
// # Limitations
//
// - No support for fractional values in IS (Integer String)
// - Limited Unicode support (assumes ASCII/UTF-8)
// - No automatic timezone handling for time values
// - Person name parsing limited to first 5 components
//
// # Related Packages
//
// - **filebase**: Byte order handling
// - **dataelem**: Data element and VR definitions
// - **dataset**: Dataset operations using element values
// - **filereader**: Reading DICOM with element value decoding
// - **filewriter**: Writing DICOM with element value encoding
//
// # Best Practices
//
// ## Use ValueConverter for Complete Processing
//
// ValueConverter provides integrated access to all value processing:
//
//	converter := element.NewValueConverter(byteOrder)
//	encoder := converter.GetEncoder()
//	parser := converter.GetParser()
//	padder := converter.GetPadder()
//
// ## Validate Before Encoding
//
// Check value properties before encoding:
//
//	if err := element.ValidateLength(data, vr); err != nil {
//	    log.Fatal(err)
//	}
//
// ## Handle Byte Order Properly
//
// Use correct byte order for transfer syntax:
//
//	// Implicit VR or Explicit VR LE: LittleEndian
//	// Explicit VR BE: BigEndian
//	encoder := element.NewValueEncoder(byteOrder)
//
// # DICOM Compliance
//
// Implements DICOM standard (PS3.5) for:
// - Value encoding/decoding
// - Value Representation (VR) handling
// - Value padding rules
// - Date/Time/Person Name formats
// - Character set handling
// - Backslash-separated multiple values
//
// See: https://www.dicomstandard.org/
package element
