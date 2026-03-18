// Package valuerep provides utilities for DICOM Value Representation (VR) handling and validation.
//
// This package implements comprehensive support for DICOM Value Representations, including metadata
// management, validation functions, and specialized parsing for complex DICOM types. It handles all
// standard DICOM VRs with their constraints, formats, and requirements.
//
// # Core Concepts
//
// DICOM uses Value Representations (VRs) to define how data is encoded and formatted:
//
//   - String VRs: AE, AS, CS, DA, DT, LO, LT, PN, SH, ST, TM, UI, UC, UR, UT
//   - Numeric String VRs: DS (Decimal String), IS (Integer String)
//   - Binary Numeric VRs: FD (Float64), FL (Float32), SL (Signed Long), SS (Signed Short), UL (Unsigned Long), US (Unsigned Short)
//   - Binary VRs: OB, OD, OF, OL, OW, UN (Other Byte/Word/etc)
//   - Special VRs: AT (Attribute Tag), SQ (Sequence)
//
// # Features
//
//   - **VR Metadata Management**: Complete metadata for all 30+ DICOM Value Representations
//   - **Type Validation**: Verify values match VR type requirements (string, binary, numeric)
//   - **Length Validation**: Check values comply with maximum length constraints
//   - **Format Validation**: Regular expression validation for structured VR formats
//   - **Complete Validation**: Combined type, length, and format validation
//   - **Specialized Parsing**: Parser functions for complex DICOM types (PersonName, Date, Time, etc)
//   - **VR Utilities**: Functions to check VR validity and retrieve VR metadata
//
// # VR Categories
//
// ## String VRs
//
// String VRs represent character data:
//
//   - AE (Application Entity): System name or process ID, max 16 chars
//   - AS (Age String): Age format nnnD/W/M/Y, max 4 chars
//   - CS (Code String): Uppercase alphanumeric and spaces, max 16 chars
//   - DA (Date): Format YYYYMMDD, no length limit
//   - DT (DateTime): Format YYYYMMDDHHMMSS.FFFFFF+HHMM, no length limit
//   - LO (Long String): General text, max 64 chars
//   - LT (Long Text): Paragraph text, max 10240 chars
//   - PN (Person Name): Special format with ^ separators for components
//   - SH (Short String): Text string, max 16 chars
//   - ST (Short Text): Single line text, max 1024 chars
//   - TM (Time): Format HHMMSS.FFFFFF, no length limit
//   - UI (Unique Identifier): OID notation (e.g., 1.2.840.10008), max 64 chars
//   - UC (Unlimited Characters): No length restrictions
//   - UR (Universal Resource Identifier): URL format, no length limit
//   - UT (Unlimited Text): No length restrictions
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
// These represent numeric values in fixed-size binary format:
//
//   - FD (Float Double): 8 bytes, IEEE 754 double precision
//   - FL (Float Single): 4 bytes, IEEE 754 single precision
//   - SL (Signed Long): 4 bytes, two's complement integer
//   - SS (Signed Short): 2 bytes, two's complement integer
//   - UL (Unsigned Long): 4 bytes, unsigned integer
//   - US (Unsigned Short): 2 bytes, unsigned integer
//
// ## Binary VRs
//
// These represent arbitrary binary data or sequences:
//
//   - OB (Other Byte): Arbitrary byte data
//   - OD (Other Double): 8-byte word data (doubles)
//   - OF (Other Float): 4-byte word data (floats)
//   - OL (Other Long): 4-byte word data (longs)
//   - OW (Other Word): 2-byte word data
//   - UN (Unknown): Unknown or private data type
//   - AT (Attribute Tag): 4-byte DICOM tag value
//   - SQ (Sequence): Sequence of items
//
// # Basic Usage
//
// ## Validate a VR Value
//
//	// Check if a value is valid for a VR
//	if err := valuerep.ValidateValue("AE", "MyApplication"); err != nil {
//		log.Fatal(err)
//	}
//
// ## Parse Special DICOM Types
//
//	// Parse a person name with components
//	pn := valuerep.ParsePersonName("Smith^John^M^Dr.^Jr.")
//	fmt.Println(pn.Alphabetic) // "Smith"
//
//	// Parse a date
//	d, err := valuerep.ParseDate("20230615")
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Date: %v\n", d.Value)
//
// ## Get VR Metadata
//
//	// Get metadata for a VR code
//	metadata, err := valuerep.GetVRMetadata("PN")
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("VR: %s, Name: %s, MaxLength: %d\n",
//		metadata.Code, metadata.Name, metadata.MaxLength)
//
// # Validation Hierarchy
//
// The package provides multi-level validation:
//
//  1. ValidateType: Checks if value matches VR type (string, binary, etc)
//  2. ValidateVRLength: Verifies value doesn't exceed max length
//  3. ValidateRegex: Validates format using VR-specific regular expressions
//  4. ValidateValue: Combined validation of all three levels
//
// # Data Types
//
// ## VRMetadata
//
// Metadata structure for a DICOM Value Representation:
//
//	type VRMetadata struct {
//		Code        string // 2-letter VR code (e.g., "AE", "PN")
//		Name        string // Full name (e.g., "Application Entity")
//		MaxLength   int    // Maximum value length (0 = unlimited)
//		Padding     string // Padding character used
//		IsNumeric   bool   // Is a numeric type
//		IsString    bool   // Is a string type
//		IsBinary    bool   // Is binary data
//		IsDateTime  bool   // Is date/time type
//	}
//
// ## VRMetadataMap
//
// A map containing metadata for all 30+ standard DICOM VRs:
//
//	// Access VR metadata directly
//	metadata := valuerep.VRMetadataMap["AE"]
//
// ## Special Type Structs
//
// PersonName: Represents a DICOM PN value with alphabetic, ideographic, and phonetic components
//
//	type PersonName struct {
//		Alphabetic string
//		Ideographic string
//		Phonetic   string
//	}
//
// DecimalString: Represents a DICOM DS value with parsed float
//
//	type DecimalString struct {
//		Value float64
//		Raw   string
//	}
//
// IntegerString: Represents a DICOM IS value with parsed integer
//
//	type IntegerString struct {
//		Value int64
//		Raw   string
//	}
//
// Date: Represents a DICOM DA value as time.Time
//
//	type Date struct {
//		Value time.Time
//		Raw   string
//	}
//
// Time: Represents a DICOM TM value as time.Time
//
//	type Time struct {
//		Value time.Time
//		Raw   string
//	}
//
// UniqueIdentifier: Represents a DICOM UI value
//
//	type UniqueIdentifier struct {
//		Value string
//	}
//
// # Examples
//
// ## Type Validation
//
//	// Valid: string for string VR
//	valid, msg := valuerep.ValidateType("AE", "MyApp")
//	if !valid {
//		log.Fatal(msg)
//	}
//
//	// Invalid: integer for string VR
//	valid, msg = valuerep.ValidateType("AE", 123)
//	if !valid {
//		log.Printf("Error: %s", msg)
//	}
//
// ## Length Validation
//
//	// Check if value exceeds VR max length
//	value := "12345678901234567" // 17 chars
//	valid, msg := valuerep.ValidateVRLength("AE", value) // AE max = 16
//	if !valid {
//		log.Printf("Error: %s", msg)
//	}
//
// ## Complete Date Parsing
//
//	dates := []string{"20230101", "20231225", "20230615"}
//	for _, dateStr := range dates {
//		d, err := valuerep.ParseDate(dateStr)
//		if err != nil {
//			log.Printf("Failed to parse %s: %v", dateStr, err)
//			continue
//		}
//		fmt.Printf("Parsed: %s -> %v\n", dateStr, d.Value)
//	}
//
// ## VR Discovery
//
//	// Get all valid VR codes
//	codes := valuerep.GetAllVRCodes()
//	fmt.Printf("Total VRs: %d\n", len(codes))
//
//	// Check if a code is valid
//	if valuerep.IsValidVR("PN") {
//		fmt.Println("PN is a valid VR")
//	}
//
// # Error Handling
//
// Parsing functions return errors for invalid input:
//
//	// Parsing errors for invalid format
//	d, err := valuerep.ParseDate("2023-01-01") // Wrong format
//	if err != nil {
//		log.Printf("Invalid date format: %v", err)
//	}
//
// Validation functions return validation messages:
//
//	// Validation messages for invalid values
//	valid, msg := valuerep.ValidateValue("IS", "12.34")
//	if !valid {
//		log.Printf("Validation error: %s", msg)
//	}
//
// # DICOM Compliance
//
// The package implements DICOM PS3.5 standard for:
//   - Value Representations and their encoding
//   - Data element value format requirements
//   - Type classification and constraints
//   - Validation rules for each VR type
//
// See: https://www.dicomstandard.org/
//
// # Performance
//
// The package is optimized for typical DICOM processing:
//   - Direct metadata lookup via VRMetadataMap
//   - Efficient regex validation with compiled patterns
//   - Pre-computed metadata for all standard VRs
//   - Minimal allocations for type checking
//
// # See Also
//
//   - values package: Value conversion and encoding utilities
//   - dataset package: Complete DICOM file structure
//   - tag package: DICOM tag definitions and utilities
package valuerep
