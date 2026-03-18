// Package tag provides DICOM tag handling and dictionary lookup functionality.
//
// DICOM tags are 32-bit identifiers consisting of a 16-bit group number
// (upper 16 bits) and a 16-bit element number (lower 16 bits).
// Tags are typically represented in the format (GGGG,EEEE) where GGGG is
// the group in hexadecimal and EEEE is the element in hexadecimal.
//
// # Core Types
//
// Tag: Represents a DICOM tag as a 32-bit unsigned integer. Provides methods
// for tag manipulation, comparison, and dictionary lookup.
//
// TagInfo: Contains metadata about a tag including its name, Value Representation
// (VR), Value Multiplicity (VM), keyword, and retirement status.
//
// # Tag Dictionary
//
// The GlobalDictionary() function provides access to the DICOM standard dictionary
// which contains information about all standard DICOM tags. Tags can be looked up
// by tag value and provide access to their metadata.
//
// # Tag Creation and Parsing
//
// Tags can be created in several ways:
//   - New(group, element) - Create from group and element numbers
//   - FromInt(val) - Create from a 32-bit integer
//   - FromBytes(data, littleEndian) - Create from 4 bytes
//   - ParseTag(s) - Parse from string formats like "(0010,0020)" or "00100020"
//
// # Tag Categories
//
// Standard Tags: Tags defined in the DICOM standard dictionary.
// Private Tags: Tags with odd group numbers, used for vendor-specific extensions.
// Special Tags: Item (FFFE,0000), Item Delimiter (FFFE,E00D), Sequence Delimiter (FFFE,E0DD).
//
// # Private Tags
//
// Private tags allow vendors to extend DICOM for specific applications.
// Private creator tags (element 0x0010-0x00FF) identify the private creator
// and are referenced by higher-numbered elements in the same group.
//
// # Dictionary Lookup
//
// The Tag type provides methods for dictionary lookups:
//   - GetInfo() - Get complete TagInfo
//   - GetName() - Get human-readable tag name
//   - GetVR() - Get Value Representation
//   - GetVM() - Get Value Multiplicity
//   - GetKeyword() - Get keyword identifier
//   - IsRetired() - Check retirement status
//   - Exists() - Check if tag is in dictionary
//
// # Byte Conversion
//
// Tags can be converted to/from bytes in both little-endian and big-endian formats:
//   - ToBytes(littleEndian) - Convert tag to 4 bytes
//   - FromBytes(data, littleEndian) - Create tag from 4 bytes
//
// This is useful for reading/writing DICOM files which typically use explicit VR
// with little-endian byte order.
//
// # String Representation
//
// Tags provide multiple string representations:
//   - String() - Standard (GGGG,EEEE) format
//   - Hex() - Hex format without separators (GGGGEEEE)
//   - ParseTag() - Parse from various string formats
//
// # Performance Notes
//
// The tag dictionary is loaded once and cached globally, making lookups O(1)
// for standard tags. Private tags can be added at runtime as needed.
package tag
