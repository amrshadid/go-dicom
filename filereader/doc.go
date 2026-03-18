// Package filereader provides comprehensive DICOM file reading support.
//
// This package implements functionality for reading DICOM files from disk with proper
// handling of file structure (preamble, DICM prefix, file meta information, dataset),
// transfer syntax interpretation (explicit/implicit VR, byte order), data element parsing,
// character set decoding, and waveform data handling. Includes validation against DICOM dictionary
// and support for both compressed and uncompressed transfer syntaxes.
//
// # Core Concepts
//
// ## FileMetaInfo
//
// Contains DICOM file meta information (Group 0002) including SOP Class/Instance UIDs,
// transfer syntax, implementation information, and application entity titles.
//
// ## DataElementValue
//
// Represents a single DICOM data element with tag, VR (Value Representation),
// value bytes, and length. Used for both meta information and dataset elements.
//
// ## DCMFileReader
//
// Low-level reader that handles preamble, DICM prefix, file meta information,
// and sequential data element reading. Tracks current file position.
//
// ## DICOMFile
//
// Complete parsed DICOM file structure containing file meta information, all
// data elements, and transfer syntax configuration (explicit/implicit VR, byte order).
//
// ## Transfer Syntax
//
// Determines data encoding:
//   - Explicit VR: Value Representation field present in each element
//   - Implicit VR: VR inferred from DICOM dictionary
//   - Little-Endian: Standard byte order (default)
//   - Big-Endian: Rare byte order for specific encodings
//
// # Basic Usage
//
// ## Reading a Complete DICOM File
//
//	import (
//	    "log"
//	    "os"
//	    "github.com/amrshadid/go-dicom/filereader"
//	    "github.com/amrshadid/go-dicom/filebase"
//	)
//
//	func main() {
//	    // Open DICOM file
//	    file, err := os.Open("patient.dcm")
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    defer file.Close()
//
//	    // Create reader
//	    reader := filebase.NewFileReader(file)
//
//	    // Read entire file
//	    dicomFile, err := filereader.ReadDICOMFile(reader)
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//
//	    // Access file meta information
//	    fmt.Printf("Transfer Syntax: %s\n", dicomFile.FileMetaInfo.TransferSyntaxUID)
//	    fmt.Printf("SOP Class UID: %s\n", dicomFile.FileMetaInfo.MediaStorageSOPClassUID)
//
//	    // Process data elements
//	    for _, elem := range dicomFile.DataElements {
//	        fmt.Printf("Tag: %s, VR: %s, Length: %d\n",
//	            elem.Tag.String(), elem.VR, elem.Length)
//	    }
//	}
//
// ## Low-Level Sequential Reading
//
//	dfr := filereader.NewDCMFileReader(reader)
//
//	// Read file structure
//	if err := dfr.ReadPreamble(); err != nil {
//	    log.Fatal(err)
//	}
//
//	if err := dfr.ReadDICMPrefix(); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Read file meta information
//	metaInfo, err := dfr.ReadFileMetaInfo()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Current position: %d\n", dfr.GetPosition())
//
// # Advanced Usage
//
// ## Validation
//
// Data elements are validated against DICOM dictionary during reading:
//   - VR type matches dictionary definition
//   - Tags marked as retired are detected
//   - Private tags (0x0011-0xFFFF odd groups) are allowed
//   - Multi-VR tags (e.g., "OB or OW") are handled
//
// ## Charset Support
//
// Automatic character set detection and decoding for text elements.
// Supports single-byte, multi-byte, and Asian character sets.
//
// ## Waveform Data
//
// Efficient reading of waveform sequences with sampling information.
// Supports multi-channel and variable sample rates.
//
// ## Transfer Syntax Support
//
// Automatically detects and handles:
//   - Implicit VR Little-Endian (1.2.840.10008.1.2) - most common
//   - Explicit VR Little-Endian (1.2.840.10008.1.2.1) - standard
//   - Explicit VR Big-Endian (1.2.840.10008.1.2.2) - rare
//   - JPEG compression (multiple variants)
//   - JPEG 2000 compression
//   - RLE compression
//   - JPEG-LS compression
//   - DEFLATE compression
//
// # Data Structures
//
// ## FileMetaInfo
//
//	type FileMetaInfo struct {
//	    MediaStorageSOPClassUID         string
//	    MediaStorageSOPInstanceUID      string
//	    TransferSyntaxUID               string
//	    ImplementationClassUID          string
//	    ImplementationVersionName       string
//	    SourceApplicationEntityTitle    string
//	    SendingApplicationEntityTitle   string
//	    ReceivingApplicationEntityTitle string
//	    FileMetaInformationGroupLength  uint32
//	    FileMetaInformationVersion      []byte
//	}
//
// Contains all Group 0002 meta information from DICOM file.
//
// ## DataElementValue
//
//	type DataElementValue struct {
//	    Tag    tag.Tag
//	    VR     string
//	    Value  []byte
//	    Length uint32
//	}
//
// Represents a single DICOM data element with binary value.
//
// ## DCMFileReader
//
//	type DCMFileReader struct {
//	    reader       filebase.Reader
//	    fileMetaInfo *FileMetaInfo
//	    position     int64
//	}
//
// Low-level sequential DICOM reader. Tracks file position for error reporting.
//
// ## DICOMFile
//
//	type DICOMFile struct {
//	    FileMetaInfo   *FileMetaInfo
//	    DataElements   []*DataElementValue
//	    ExplicitVR     bool
//	    IsLittleEndian bool
//	}
//
// Complete parsed DICOM file with all metadata and data elements.
//
// # API Reference
//
// ## Reader Creation
//
// ### NewDCMFileReader
//
//	func NewDCMFileReader(reader filebase.Reader) *DCMFileReader
//
// Creates new low-level DICOM file reader.
//
// **Parameters:**
// - `reader`: filebase.Reader for I/O operations
//
// **Returns:** DCMFileReader pointer
//
// **Example:**
// ```go
// dfr := filereader.NewDCMFileReader(filebaseReader)
// ```
//
// ### ReadDICOMFile
//
//	func ReadDICOMFile(reader filebase.Reader) (*DICOMFile, error)
//
// Reads complete DICOM file including preamble, meta header, and all dataset elements.
//
// **Parameters:**
// - `reader`: filebase.Reader for file reading
//
// **Returns:** DICOMFile pointer and error
//
// **Example:**
// ```go
// file, err := os.Open("patient.dcm")
// reader := filebase.NewFileReader(file)
// dicom, err := filereader.ReadDICOMFile(reader)
// ```
//
// ## File Structure Reading
//
// ### ReadPreamble
//
//	func (dfr *DCMFileReader) ReadPreamble() error
//
// Reads 128-byte DICOM preamble (all zeros).
//
// **Returns:** Error if read fails
//
// ### ReadDICMPrefix
//
//	func (dfr *DCMFileReader) ReadDICMPrefix() error
//
// Reads and validates "DICM" magic string (4 bytes).
//
// **Returns:** Error if read fails or magic string invalid
//
// ### ReadFileMetaInfo
//
//	func (dfr *DCMFileReader) ReadFileMetaInfo() (*FileMetaInfo, error)
//
// Reads DICOM file meta information (Group 0002).
// Parses all meta elements including SOP UIDs and transfer syntax.
//
// **Returns:** FileMetaInfo pointer and error
//
// ### ReadFileMetaInformationGroupLength
//
//	func (dfr *DCMFileReader) ReadFileMetaInformationGroupLength() (uint32, error)
//
// Reads Group Length element (0002,0000) specifying meta header size.
//
// **Returns:** Length value and error
//
// ## Data Element Reading
//
// ### ReadTag
//
//	func (dfr *DCMFileReader) ReadTag() (tag.Tag, error)
//
// Reads 4-byte DICOM tag (group + element).
//
// **Returns:** Tag and error
//
// **Example:**
// ```go
// tg, err := dfr.ReadTag()
// fmt.Printf("Tag: %s\n", tg.String())
// ```
//
// ### ReadDataElement
//
//	func (dfr *DCMFileReader) ReadDataElement(explicitVR bool) (*DataElementValue, error)
//
// Reads single data element with optional VR field.
//
// **Parameters:**
// - `explicitVR`: If true, reads VR field; if false, infers VR from dictionary
//
// **Returns:** DataElementValue pointer and error
//
// **Example:**
// ```go
// elem, err := dfr.ReadDataElement(true)  // Explicit VR
// ```
//
// ## Utility Methods
//
// ### GetPosition
//
//	func (dfr *DCMFileReader) GetPosition() int64
//
// Returns current position in file.
//
// **Returns:** Byte position
//
// **Example:**
// ```go
// pos := dfr.GetPosition()
// fmt.Printf("Read %d bytes so far\n", pos)
// ```
//
// ### GetFileMetaInfo
//
//	func (dfr *DCMFileReader) GetFileMetaInfo() *FileMetaInfo
//
// Returns file meta information previously read.
//
// **Returns:** FileMetaInfo pointer (nil if not read)
//
// # Performance Characteristics
//
// | Operation | Complexity | Description |
// |-----------|-----------|-------------|
// | ReadDICOMFile | O(n) | n = number of data elements |
// | ReadPreamble | O(1) | Fixed 128 bytes |
// | ReadDICMPrefix | O(1) | Fixed 4 bytes |
// | ReadFileMetaInfo | O(m) | m = meta information elements |
// | ReadTag | O(1) | Fixed 4 bytes |
// | ReadDataElement | O(k) | k = element value length |
// | GetPosition | O(1) | Returns counter |
// | ValidateDataElement | O(1) | Dictionary lookup |
//
// # Thread Safety
//
// DCMFileReader is NOT thread-safe. Each goroutine must:
//   - Have its own reader instance
//   - Use external synchronization for shared readers
//   - Not interleave read operations from multiple goroutines
//
// # Error Handling
//
// | Operation | Error Condition |
// |-----------|-----------------|
// | ReadDICOMFile | Invalid preamble, missing DICM, malformed meta info, read failures |
// | ReadPreamble | Read fails, unexpected EOF |
// | ReadDICMPrefix | Invalid magic string (not "DICM") |
// | ReadFileMetaInfo | Malformed meta header, invalid tags, length mismatch |
// | ReadDataElement | Invalid VR, length overflow, read failures |
// | ValidateDataElement | Unknown tag, retired tag, VR mismatch |
//
// # Use Cases
//
// ## Loading Patient Images
//
// Read DICOM medical images from disk and parse all metadata and pixel data.
//
// ## DICOM Format Validation
//
// Verify DICOM file structure and validate against DICOM standard.
//
// ## Metadata Extraction
//
// Extract patient demographics, study information, and image parameters.
//
// ## Batch Processing
//
// Read multiple DICOM files sequentially with proper error handling.
//
// ## Format Conversion
//
// Parse DICOM files as intermediate step in conversion to other formats.
//
// # Limitations
//
// - Pixel data decompression requires external codec packages
// - Some compressed transfer syntaxes may require specialized handling
// - Memory usage scales with file size (entire file loaded into memory)
// - No streaming support for large pixel data arrays
// - Validation is limited to dictionary checks and basic structure
//
// # Related Packages
//
//   - filebase: Low-level file I/O interface
//   - filewriter: Writing DICOM files
//   - fileutil: Utility functions for DICOM file operations
//   - dataset: Dataset structure for higher-level DICOM handling
//   - tag: DICOM tag definitions and dictionary
//   - compress: Codec support for pixel data decompression
//   - charset: Character set encoding/decoding
//
// # DICOM Compliance
//
// Implements DICOM standard (PS3.10) for:
//   - File format and structure
//   - File meta information (Group 0002)
//   - Transfer syntax interpretation
//   - Explicit/implicit VR handling
//   - Byte order (little-endian, big-endian)
//   - Data element encoding
//   - Tag validation and retirement status
//
// See: https://www.dicomstandard.org/
package filereader
