// Package uid provides utilities for DICOM Unique Identifier (UID) management.
//
// This package implements comprehensive support for DICOM UIDs, including creation,
// validation, database lookup, and classification of UIDs into categories such as
// transfer syntaxes, SOP classes, and other DICOM UID types.
//
// # Core Concepts
//
// DICOM Unique Identifiers (UIDs) are globally unique dotted-decimal string identifiers
// that follow the ISO/IEC 8824 standard. UIDs identify:
//
//   - Transfer Syntaxes: How DICOM data is encoded
//   - SOP Classes: Data types and storage objects
//   - Service Classes: Network services
//   - Other DICOM entities
//
// UID Format:
//   - Dotted decimal notation: "1.2.840.10008.1.2"
//   - Each component is a non-negative integer
//   - Must have at least 2 components
//   - No leading zeros in components
//
// # UID Structure
//
// ## UID Type
//
// UID is the main type for working with DICOM UIDs.
//
//	type UID struct {
//		value string
//	}
//
// ## UIDInfo Type
//
// UIDInfo contains metadata about a registered UID.
//
//	type UIDInfo struct {
//		UID         string // The UID value (e.g., "1.2.840.10008.1.2")
//		Name        string // Human-readable name
//		Type        string // Category: TransferSyntax, SOPClass, etc.
//		IsRetired   bool   // Whether this UID is deprecated
//		Description string // Additional information
//	}
//
// # Features
//
//   - **UID Creation**: Create UIDs from strings with validation
//   - **UID Validation**: Verify format compliance
//   - **UID Database**: Lookup registered UIDs and their metadata
//   - **UID Classification**: Query UIDs by type (transfer syntax, SOP class, etc.)
//   - **Transfer Syntax Utilities**: Query compression and losslessness properties
//   - **SOP Class Utilities**: Access common DICOM SOP class UIDs
//
// # Basic Usage
//
// ## Create a UID
//
//	u := uid.New("1.2.840.10008.1.2")
//	fmt.Println(u.String()) // "1.2.840.10008.1.2"
//
// ## Validate a UID
//
//	u := uid.New("1.2.840.10008.1.2")
//	if u.IsValid() {
//		fmt.Println("Valid DICOM UID")
//	}
//
// ## Get UID Information
//
//	info := uid.GetUIDInfo("1.2.840.10008.1.2")
//	fmt.Printf("Name: %s\n", info.Name)   // "Implicit VR Little Endian"
//	fmt.Printf("Type: %s\n", info.Type)   // "TransferSyntax"
//
// ## Query UID Type
//
//	if uid.IsTransferSyntax("1.2.840.10008.1.2") {
//		fmt.Println("This is a transfer syntax")
//	}
//
// # UID Types
//
// ## Transfer Syntaxes
//
// Transfer Syntaxes define how DICOM data is encoded.
//
// **Uncompressed:**
//   - Implicit VR Little Endian: 1.2.840.10008.1.2
//   - Explicit VR Little Endian: 1.2.840.10008.1.2.1
//   - Explicit VR Big Endian: 1.2.840.10008.1.2.2
//
// **Compressed:**
//   - JPEG Baseline: 1.2.840.10008.1.2.4.50
//   - JPEG Lossless: 1.2.840.10008.1.2.4.70
//   - JPEG 2000 Lossless: 1.2.840.10008.1.2.4.90
//   - RLE Lossless: 1.2.840.10008.1.2.5
//
// ## SOP Classes
//
// SOP Classes define the type of information object.
//
// **Common SOP Classes:**
//   - Verification SOP Class: 1.2.840.10008.1.1
//   - CR Image Storage: 1.2.840.10008.5.1.4.1.1.2
//   - CT Image Storage: 1.2.840.10008.5.1.4.1.1.2.1
//   - MR Image Storage: 1.2.840.10008.5.1.4.1.1.4
//   - Ultrasound Image Storage: 1.2.840.10008.5.1.4.1.1.6.4
//
// # API Reference
//
// ## Creating and Querying UIDs
//
// ### New
//
// Creates a new UID from a string.
//
//	u := uid.New("1.2.840.10008.1.2")
//
// ### String
//
// Returns the string representation of a UID.
//
//	str := u.String() // "1.2.840.10008.1.2"
//
// ### IsEmpty
//
// Checks if a UID is empty.
//
//	if !u.IsEmpty() {
//		// Use the UID
//	}
//
// ### IsValid
//
// Validates the UID format (dotted decimal).
//
//	if u.IsValid() {
//		fmt.Println("Valid DICOM UID format")
//	}
//
// ### Equals
//
// Compares two UIDs for equality.
//
//	if u1.Equals(u2) {
//		fmt.Println("Same UID")
//	}
//
// ### Info
//
// Retrieves detailed information about a UID.
//
//	info := u.Info()
//	if info != nil {
//		fmt.Printf("Name: %s\n", info.Name)
//	}
//
// ## Querying the UID Database
//
// ### GetUIDInfo
//
// Retrieves information about a UID by string.
//
//	info := uid.GetUIDInfo("1.2.840.10008.1.2")
//
// ### AllUIDs
//
// Returns all registered UIDs sorted alphabetically.
//
//	allUIDs := uid.AllUIDs()
//	for _, u := range allUIDs {
//		fmt.Println(u.String())
//	}
//
// ### AllUIDInfos
//
// Returns all UID information structures sorted.
//
//	allInfos := uid.AllUIDInfos()
//
// ### GetByName
//
// Finds a UID by its name (case-insensitive).
//
//	u := uid.GetByName("Implicit VR Little Endian")
//
// ### GetByType
//
// Returns all UIDs of a specific type.
//
//	transferSyntaxes := uid.GetByType("TransferSyntax")
//	sopClasses := uid.GetByType("SOPClass")
//
// ## UID Type Queries
//
// ### IsTransferSyntax
//
// Checks if a UID is a transfer syntax.
//
//	if uid.IsTransferSyntax("1.2.840.10008.1.2") {
//		// Process transfer syntax
//	}
//
// ### IsSOPClass
//
// Checks if a UID is a SOP class.
//
//	if uid.IsSOPClass("1.2.840.10008.5.1.4.1.1.2.1") {
//		// Process SOP class
//	}
//
// ## Transfer Syntax Utilities
//
// ### IsCompressed
//
// Checks if a transfer syntax uses compression.
//
//	if uid.IsCompressed(u) {
//		fmt.Println("Data is compressed")
//	}
//
// ### IsLossless
//
// Checks if a transfer syntax is lossless.
//
//	if uid.IsLossless(u) {
//		fmt.Println("Compression is lossless")
//	}
//
// ### CompressedTransferSyntaxes
//
// Returns all compressed transfer syntax UIDs.
//
//	compressed := uid.CompressedTransferSyntaxes()
//
// ### LittleEndianTransferSyntaxes
//
// Returns common little-endian transfer syntaxes.
//
//	syntaxes := uid.LittleEndianTransferSyntaxes()
//
// ### BigEndianTransferSyntax
//
// Returns the big-endian explicit VR transfer syntax UID.
//
//	be := uid.BigEndianTransferSyntax()
//
// ### ImplicitVRLittleEndian
//
// Returns the implicit VR little endian transfer syntax UID.
//
//	implicitVR := uid.ImplicitVRLittleEndian()
//
// ### ExplicitVRLittleEndian
//
// Returns the explicit VR little endian transfer syntax UID.
//
//	explicitVR := uid.ExplicitVRLittleEndian()
//
// ## SOP Class Utilities
//
// ### VerificationSOPClass
//
// Returns the Verification SOP class UID.
//
//	verification := uid.VerificationSOPClass()
//
// ### CTImageStorage
//
// Returns the CT Image Storage SOP class UID.
//
//	ct := uid.CTImageStorage()
//
// ### MRImageStorage
//
// Returns the MR Image Storage SOP class UID.
//
//	mr := uid.MRImageStorage()
//
// # Examples
//
// ## Analyzing Transfer Syntax
//
//	ts := uid.New("1.2.840.10008.1.2.4.70") // JPEG Lossless
//
//	if uid.IsCompressed(ts) {
//		fmt.Println("Compressed transfer syntax")
//	}
//
//	if uid.IsLossless(ts) {
//		fmt.Println("Lossless compression")
//	}
//
//	info := ts.Info()
//	fmt.Printf("Name: %s\n", info.Name)
//
// ## Listing All Transfer Syntaxes
//
//	allTransferSyntaxes := uid.GetByType("TransferSyntax")
//	fmt.Printf("Found %d transfer syntaxes\n", len(allTransferSyntaxes))
//
//	for _, u := range allTransferSyntaxes {
//		info := u.Info()
//		fmt.Printf("- %s: %s\n", u.String(), info.Name)
//	}
//
// ## Working With SOP Classes
//
//	sopClasses := uid.GetByType("SOPClass")
//
//	for _, u := range sopClasses {
//		info := u.Info()
//		fmt.Printf("SOP Class: %s\n", info.Name)
//	}
//
// # Performance Considerations
//
// The UID database is compiled into the package as a map, providing O(1) lookup time
// for UID information queries. All operations are stateless and safe for concurrent use.
//
// # Thread Safety
//
// All functions in this package are thread-safe and can be used concurrently.
// The UID database is immutable and shared across all goroutines.
//
// # DICOM Compliance
//
// The package implements DICOM standards for:
//   - UID format specification (dotted decimal)
//   - Standard DICOM UID definitions
//   - Transfer Syntax classifications
//   - SOP Class definitions
//
// See: https://www.dicomstandard.org/
//
// # Extended UID Database
//
// The current UID database includes:
//   - Transfer Syntax UIDs (Implicit/Explicit VR, JPEG, JPEG 2000, RLE)
//   - Common SOP Class UIDs (Verification, CR, CT, MR, Ultrasound)
//   - Metadata for each UID (name, type, retirement status, description)
//
// Additional UIDs can be added to the database by extending the uids map.
//
// # See Also
//
//   - values package: Value conversion and encoding utilities
//   - dataset package: DICOM dataset structure and handling
//   - tag package: DICOM tag definitions and utilities
package uid
