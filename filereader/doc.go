// Package filereader provides comprehensive DICOM file reading support.
//
// This package implements functionality for reading DICOM files from disk with proper
// handling of file structure (preamble, DICM prefix, file meta information, dataset),
// transfer syntax interpretation (explicit/implicit VR, byte order), data element parsing,
// character set decoding, and waveform data handling. Includes validation against DICOM dictionary
// and support for both compressed and uncompressed transfer syntaxes.
//
// A program that reads only attributes can use filereader/attributes instead:
// the same parser and the same element types, without the packages
// GetDataset, FrameRanges and GetWaveforms bring in (compress, encaps,
// pixels, waveforms, dataset). The parser lives in an internal package both
// share; FileMetaInfo, ReadOptions, DataElementValue and the other element
// types here are its types under aliases, so a value from one package is a
// value of the other.
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
// ### ReadDICOMFileWithOptions
//
//	func ReadDICOMFileWithOptions(reader filebase.Reader, options ReadOptions) (*DICOMFile, error)
//
// ReadDICOMFile with a choice of how to read. ReadOptions.SkipBulkData
// leaves Pixel Data, its fragments and the long values of the binary VRs in
// the file: each such element has ValueSkipped set and a nil Value, and its
// Position says where the bytes are. It is for a program that wants what a
// file holds and where, without the memory of the image.
//
//	dicom, err := filereader.ReadDICOMFileWithOptions(reader, filereader.ReadOptions{SkipBulkData: true})
//	for _, e := range dicom.DataElements {
//		fmt.Println(e.Tag, e.Position.ValueOffset, e.Position.End, e.ValueSkipped)
//	}
//
// GetDataset gives a skipped value as nothing. GetDatasetWithValuesInFile
// gives it as a dataelem.ValueInFile, where its bytes are, and filewriter,
// given the file with SetValueSource, copies them from there as it writes:
// a data set can be changed and written back without its pixels in memory,
// byte for byte as if it had been read whole. A deflated file is refused,
// since its values are not in the file.
//
// ReadOptions.Logger is where the read logs. Everything the read does logs
// there: the reader itself, the character set decisions, and the validation
// GetDataset runs as it builds the data set, later, for the file the read
// returned. None of it reaches config.Logger, so two reads at once can log to
// two loggers, or one of them to none. Nil is config.Logger, as before. To
// read silently:
//
//	dicom, err := filereader.ReadDICOMFileWithOptions(reader,
//		filereader.ReadOptions{Logger: slog.New(slog.DiscardHandler)})
//
// The logger travels in a context.Context (config.WithLogger), the way the
// reading validation mode does, through charset's WithContext functions and
// dataset's AddWithContext; no global is changed. A line names tags, VRs,
// lengths and offsets, and never a value's bytes or text, since a file from
// an untrusted source writes what it likes there. What a program should act
// on is in DICOMFile.Warnings as well.
//
// ReadOptions.MaxValueLength is the longest value the read holds in memory.
// A longer one ends the read with a *ValueTooLongError naming its attribute,
// tag, offset and stated length, before any of it is allocated, from a file
// or from a stream whose size is unknown; errors.Is(err, ErrValueTooLong)
// tells it from a file that is merely broken. It bounds element values, File
// Meta elements and encapsulated Pixel Data as a whole. Sequences and items
// are read element by element and are not values, so their lengths are not
// bounded by it. Zero is no maximum, as before.
//
// The two options combine as a server reading what it is sent wants them
// to: SkipBulkData passes over Pixel Data and long binary values, which are
// not held and so not refused, and MaxValueLength holds everything that is
// read, text and short binary values, to the maximum.
//
//	dicom, err := filereader.ReadDICOMFileWithOptions(reader, filereader.ReadOptions{
//		SkipBulkData:   true,
//		MaxValueLength: 1 << 20,
//	})
//	if errors.Is(err, filereader.ErrValueTooLong) {
//		// refused by policy: err names the element and its length
//	}
//
// Whatever the options, a source that cannot seek, such as a socket, has
// each value read as its bytes arrive, so what a read holds follows what was
// sent and not the length a header states. A source whose size is known
// holds every length to the bytes left in it.
//
// ### DICOMFile.FrameRanges
//
//	func (df *DICOMFile) FrameRanges(src io.ReaderAt) ([][]encaps.Range, error)
//	func (df *DICOMFile) FrameFirstBit(i int) (int, error)
//	func (df *DICOMFile) PixelDataElement() *DataElementValue
//
// Where each frame of Pixel Data lies in the file, so one frame is read
// without the rest: a range a frame for native data, and for encapsulated
// data the fragments that hold it, found by the Extended Offset Table, the
// Basic Offset Table, the counts and the end-of-image markers. Float Pixel
// Data and Double Float Pixel Data have frames too; PixelDataElement finds
// whichever of the three an image holds. The frames of a 1-bit image are
// packed with no padding between them, so one that is not a whole number of
// bytes starts inside a byte: its range is the bytes holding its bits, and
// FrameFirstBit the bit of the first byte it starts at.
//
//	file, _ := os.Open(path)
//	dicom, _ := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(file),
//		filereader.ReadOptions{SkipBulkData: true})
//	frames, err := dicom.FrameRanges(file)
//	// frames[i] are the byte ranges of frame i+1, to be read and joined.
//
// ### DICOMFile.FrameReader
//
//	func (df *DICOMFile) FrameReader(src io.ReaderAt) (*FrameReader, error)
//	func (r *FrameReader) ReadFrame(i int) ([]byte, error)
//	func (r *FrameReader) ReadFrameBytes(i int, offset int64, p []byte) error
//
// Reads those ranges: a frame, native samples little endian whatever the
// file's byte order, a 1-bit frame moved to start at bit 0, a compressed
// frame as its fragments joined; or part of a native frame, a band of its
// rows. It is a dataset.FrameReader, so Dataset.RenderFramesFrom draws an
// image from the file a frame or a band at a time.
//
// ### TextEncodings and DecodeText
//
//	func TextEncodings(elements []*DataElementValue, inherited []string) []string
//	func DecodeText(vr string, value []byte, encodings []string) string
//
// The elements of a DICOMFile hold their values as the file has them, text
// in whatever Specific Character Set (0008,0005) declares. TextEncodings
// returns the encodings in force for one data set, given those of the data
// set it is an item of, and DecodeText a value as UTF-8 under them.
// GetDataset applies the same rules and returns text already decoded; these
// are for a program that shows the file's own elements.
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
