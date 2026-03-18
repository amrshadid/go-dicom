// Package values provides utilities for DICOM value conversion and handling.
//
// This package implements comprehensive conversion and encoding of DICOM data elements
// across different Value Representations (VRs) and endianness formats. It handles the
// complete cycle of DICOM data processing: raw bytes to Go types and back.
//
// # Core Concepts
//
// DICOM uses Value Representations (VRs) to define how data is encoded:
//   - String VRs: AE, AS, CS, DA, DT, LO, LT, PN, SH, ST, TM, UI, UC, UR, UT
//   - Numeric String VRs: DS (Decimal String), IS (Integer String)
//   - Binary Numeric VRs: FD (Float64), FL (Float32), SL (Signed Long), SS (Signed Short), UL (Unsigned Long), US (Unsigned Short)
//   - Binary VRs: OB, OD, OF, OL, OW, UN (Other Byte/Word/etc)
//   - Special VRs: AT (Attribute Tag), SQ (Sequence)
//
// # Features
//
//   - **VR-based Conversion**: Convert raw bytes to appropriate Go types using VR info
//   - **Endianness Support**: Handle both little-endian and big-endian byte order
//   - **String Encoding**: Parse text-based DICOM values with proper trimming
//   - **Numeric Parsing**: Convert numeric strings to int64 and float64
//   - **Binary Numeric Conversion**: Decode fixed-size binary numeric values
//   - **Multi-Value Support**: Parse backslash-separated multi-valued strings
//   - **Bidirectional Conversion**: Encode Go values back to DICOM bytes
//   - **String Utilities**: Sanitization, padding, and validation functions
//   - **Tag Conversion**: Parse DICOM tag bytes into group and element numbers
//
// # Basic Usage
//
// ## Convert Raw DICOM Bytes
//
//	// Convert DS (Decimal String) value
//	raw := []byte("123.45")
//	value, err := values.ConvertValue("DS", raw, true) // true = little-endian
//	if err != nil {
//		log.Fatal(err)
//	}
//	f := value.(float64) // 123.45
//
// ## Convert Binary Numeric Values
//
//	// Convert FD (Float64) in little-endian
//	buf := new(bytes.Buffer)
//	binary.Write(buf, binary.LittleEndian, 123.45)
//	value, err := values.ConvertValue("FD", buf.Bytes(), true)
//	if err != nil {
//		log.Fatal(err)
//	}
//	f := value.(float64) // 123.45
//
// ## Parse Multi-Valued Strings
//
//	// Parse backslash-separated values
//	multiStr := "123\\456\\789"
//	numbers := values.MultiStringInt(multiStr)
//	// Result: []int64{123, 456, 789}
//
// ## Encode Go Values to DICOM
//
//	// Encode a string value
//	encoded, err := values.EncodeValue("PN", "Smith^John", true)
//	if err != nil {
//		log.Fatal(err)
//	}
//	// encoded == []byte("Smith^John")
//
//	// Encode a numeric value
//	encoded, err := values.EncodeValue("IS", int64(789), true)
//	if err != nil {
//		log.Fatal(err)
//	}
//	// encoded == []byte("789")
//
// ## Validate DICOM Values
//
//	// Validate numeric strings
//	err := values.ValidateNumericString("DS", "123.45")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # VR Categories
//
// ## String VRs
//
// String VRs represent character data with varying constraints:
//
//   - AE (Application Entity): 16 chars max
//   - AS (Age String): 4 chars format: nnnD, nnnW, nnnM, nnnY
//   - CS (Code String): 16 chars max, uppercase
//   - DA (Date): YYYYMMDD format
//   - DT (Date Time): YYYYMMDDHHMMSS.FFFFFF+HHMM format
//   - LO (Long String): 64 chars max
//   - LT (Long Text): 10240 chars max
//   - PN (Person Name): Special format with ^ and = separators
//   - SH (Short String): 16 chars max
//   - ST (Short Text): 1024 chars max
//   - TM (Time): HHMMSS.FFFFFF format
//   - UI (Unique Identifier): OID notation
//   - UC (Unlimited Characters): No length limit
//   - UR (Universal Resource Identifier): URL format
//   - UT (Unlimited Text): No length limit
//
// ## Numeric String VRs
//
// These represent numeric values encoded as text strings:
//
//   - DS (Decimal String): Floating-point as string (e.g., "123.45")
//   - IS (Integer String): Integer as string (e.g., "789")
//
// ## Binary Numeric VRs
//
// These represent numeric values in binary format with fixed sizes:
//
//   - FD (Float Double): 8 bytes, IEEE 754 double precision
//   - FL (Float Single): 4 bytes, IEEE 754 single precision
//   - SL (Signed Long): 4 bytes, two's complement
//   - SS (Signed Short): 2 bytes, two's complement
//   - UL (Unsigned Long): 4 bytes, unsigned
//   - US (Unsigned Short): 2 bytes, unsigned
//
// ## Binary VRs
//
// These represent arbitrary binary data or sequences:
//
//   - OB (Other Byte): Arbitrary byte data
//   - OD (Other Double): 8-byte words (doubles)
//   - OF (Other Float): 4-byte words (floats)
//   - OL (Other Long): 4-byte words (longs)
//   - OW (Other Word): 2-byte words
//   - UN (Unknown): Unknown data type
//   - AT (Attribute Tag): 4-byte DICOM tag
//   - SQ (Sequence): Sequence of items
//
// # Endianness
//
// DICOM supports both byte orders:
//
//   - Little-endian (true): LSB first (most common)
//   - Big-endian (false): MSB first
//
// The endianness parameter in conversion functions specifies the byte order of the input data.
//
// # Types
//
//   - RawDataElement: Raw DICOM data before conversion
//   - ParsedValue: Converted DICOM value with metadata
//
// # Multi-Value Strings
//
// DICOM uses backslash (\) as separator for multi-valued elements:
//
//	"value1\\value2\\value3"
//
// The package provides specialized parsers for multi-valued strings:
//
//   - MultiString(): Generic parser with optional converter
//   - MultiStringInt(): Parse integers separated by backslash
//   - MultiStringFloat(): Parse floats separated by backslash
//
// # Error Handling
//
// Conversion functions return errors for:
//   - Invalid byte lengths (too short for numeric types)
//   - Parse failures (invalid numeric format)
//   - Type assertion failures
//   - Unsupported VR types
//
// # Examples
//
// ## Complete Conversion Workflow
//
//	// Read raw DICOM bytes
//	raw := []byte{...}
//	vr := "US" // Unsigned Short
//	isLittleEndian := true
//
//	// Convert to Go type
//	value, err := values.ConvertValue(vr, raw, isLittleEndian)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Use the converted value
//	us := value.(uint64)
//	fmt.Printf("Value: %d\n", us)
//
//	// Encode back to DICOM
//	encoded, err := values.EncodeValue(vr, us, isLittleEndian)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// ## Tag Conversion
//
//	// Convert 4 raw bytes to DICOM tag
//	rawTag := []byte{0x10, 0x00, 0x10, 0x00} // (0010, 0010) in little-endian
//	group, element, err := values.ConvertTag(rawTag, true)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Tag: (%04X, %04X)\n", group, element) // (0010, 0010)
//
// # Performance
//
// The package is optimized for typical DICOM processing:
//   - Minimal allocations for string conversions
//   - Direct binary.Read/Write for numeric types
//   - Pre-allocated slices for multi-value parsing
//   - Efficient string trimming and validation
//
// # DICOM Standards
//
// The package follows DICOM PS3.5 standard for value representations and encoding.
// See: https://www.dicomstandard.org/
//
// # See Also
//
// - DICOM Standard: https://www.dicomstandard.org/
// - Package dataset: Structured DICOM file handling
// - Package tag: DICOM tag definitions and utilities
package values
