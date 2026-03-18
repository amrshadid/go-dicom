// Package filewriter provides comprehensive DICOM file writing support.
//
// This package implements functionality for creating DICOM files with proper
// file meta information, data elements, transfer syntaxes, charset handling,
// waveform data, and validation. It supports both explicit and implicit VR
// encoding with configurable byte order and validation modes.
//
// # Core Concepts
//
// ## FileMetaInfo
//
// Contains DICOM file meta information (Group 0002) including:
//   - Media Storage SOP Class/Instance UIDs
//   - Transfer Syntax UID
//   - Implementation Class/Version
//   - Application Entity Titles
//
// ## DataElement
//
// Represents a DICOM data element with:
//   - Tag: (XXXX,XXXX) format
//   - VR: Value Representation (e.g., "PN", "UI", "IS")
//   - Value: Binary data
//   - Length: Data length
//
// ## DCMFileWriter
//
// Main DICOM file writer handling:
//   - Preamble and DICM prefix writing
//   - File meta information
//   - Data element encoding
//   - Transfer syntax management
//   - Byte order control
//   - Validation modes
//
// ## Transfer Syntaxes
//
// Supports:
//   - Explicit VR Little-Endian (1.2.840.10008.1.2.1) - Default
//   - Implicit VR Little-Endian (1.2.840.10008.1.2)
//   - Big-Endian support (explicit/implicit)
//
// ## Validation Modes
//
// Three validation strictness levels:
//   - ValidationNone: No validation (fastest)
//   - ValidationWarn: Log warnings only (default)
//   - ValidationStrict: Reject invalid data (safest)
//
// # Basic Usage
//
// ## Writing a Simple DICOM File
//
//	writer := filewriter.NewDCMFileWriter(fileWriter)
//
//	// Write file structure
//	writer.WritePreamble()
//	writer.WriteDICMPrefix()
//
//	// Write file meta information
//	metaInfo := &filewriter.FileMetaInfo{
//	    MediaStorageSOPClassUID:  "1.2.840.10008.5.1.4.1.1.2",
//	    MediaStorageSOPInstanceUID: "1.2.3.4.5",
//	    TransferSyntaxUID:        "1.2.840.10008.1.2.1",
//	    ImplementationClassUID:   "1.2.3.4",
//	}
//	writer.WriteFileMetaInfo(metaInfo)
//
//	// Write data elements
//	elem := &filewriter.DataElement{
//	    Tag:    tag.New(0x0010, 0x0010),
//	    VR:     "PN",
//	    Value:  []byte("Doe^John"),
//	    Length: 8,
//	}
//	writer.WriteDataElement(elem)
//
// ## Configuring Transfer Syntax
//
//	writer.SetExplicitVR(true)      // Use explicit VR
//	writer.SetLittleEndian(true)    // Use little-endian byte order
//
// ## Validation
//
//	filewriter.SetValidationMode(filewriter.ValidationStrict)
//	// Now all writes will validate elements strictly
//
// # Advanced Features
//
// ## Charset Handling
//
// Support for multiple character sets:
//   - Single-byte encodings (ASCII, Latin-1, etc.)
//   - Multi-byte encodings (UTF-8, UTF-16)
//   - Japanese (ISO IR 87, 100, 159)
//   - Chinese (ISO IR 149, 192)
//   - Korean (ISO IR 149)
//
// ## Waveform Data
//
// Writing waveform sequences with:
//   - Sampling rate information
//   - Bit depth configuration
//   - Multi-channel support
//   - Efficient binary storage
//
// ## Element Validation
//
// Validates:
//   - VR type correctness
//   - Value length constraints
//   - Format compliance (regex patterns)
//   - Character set compatibility
//   - Waveform data structure
//
// # File Structure
//
// DICOM files follow standard structure:
//
//	[128 bytes Preamble]
//	[4 bytes "DICM"]
//	[File Meta Information Group (0002)]
//	[Data Elements (Groups 0004+)]
//
// # Thread Safety
//
// DCMFileWriter is not thread-safe. Each goroutine must have its own writer
// instance or use external synchronization.
//
// # Performance Characteristics
//
//   - **WritePreamble**: O(1) - Fixed 128 bytes
//   - **WriteDICMPrefix**: O(1) - Fixed 4 bytes
//   - **WriteFileMetaInfo**: O(n) - n elements in meta info
//   - **WriteDataElement**: O(m) - m = element value length
//   - **ValidateElement**: O(1) - Constant-time validation
//
// # Error Handling
//
// Operations return errors for:
//   - Nil file meta info
//   - Invalid data elements
//   - Validation failures (in strict mode)
//   - Write operation failures
//   - Invalid transfer syntax
//   - Unsupported character sets
//   - Malformed waveform data
//
// Example:
//
//	if err := writer.WriteFileMetaInfo(nil); err != nil {
//	    log.Printf("Error: %v", err)  // "file meta info is nil"
//	}
//
// # Use Cases
//
// ## Creating New DICOM Files
//
//	writer := filewriter.NewDCMFileWriter(outputFile)
//	writer.WritePreamble()
//	writer.WriteDICMPrefix()
//	writer.WriteFileMetaInfo(metaInfo)
//	// Add data elements...
//
// ## Converting to Different Transfer Syntax
//
//	writer.SetExplicitVR(false)  // Switch to implicit VR
//	// Re-encode and write all elements
//
// ## Multi-Character Set Documents
//
//	writer.SetCharacterSet("ISO_IR 192")  // UTF-8
//	// Write elements with UTF-8 text
//
// ## Waveform Data Export
//
//	writer.WriteWaveformSequence(waveformSeq)
//	// Efficient binary waveform storage
//
// # DICOM Compliance
//
// Implements DICOM standard (PS3.10) for:
//   - File format and structure
//   - Meta information handling
//   - Transfer syntax encoding
//   - Data element encoding
//   - Explicit/implicit VR handling
//   - Little-endian/big-endian support
//
// See: https://www.dicomstandard.org/
//
// # See Also
//
//   - filebase package: Low-level file I/O
//   - dataelem package: Data element structure
//   - tag package: DICOM tag definitions
//   - valuerep package: Value representation validation
//   - charset package: Character set encoding
//   - waveforms package: Waveform data handling
package filewriter
