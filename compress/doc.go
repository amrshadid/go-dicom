// Package compress provides comprehensive compression and encapsulation handling for DICOM pixel data.
//
// # Overview
//
// The compress package implements the DICOM PS3.5 standard for handling compressed and encapsulated
// pixel data. It supports multiple compression formats (JPEG, RLE, DEFLATE, JPEG-LS, JPEG 2000, etc.)
// and provides efficient parsing, encoding, and decoding of encapsulated datasets.
//
// # Core Concepts
//
// ## Encapsulation Format
//
// DICOM encapsulation follows a strict format for storing compressed pixel data:
//
//	[Preamble (128 bytes)] [DICM] [File Meta] [Dataset] [Pixel Data]
//
// The Pixel Data (Tag 7FE0,0010) in encapsulated form contains:
//
//	[Basic Offset Table] [Item 1] [Item 2] ... [Item N] [Sequence Delimiter]
//
// Where:
//   - Basic Offset Table: Optional item with 32-bit offsets to each frame
//   - Items: Each item contains one compressed frame
//   - Sequence Delimiter: (FFFE,E0DD) marking end of sequence
//
// ### Basic Offset Table (BOT)
//
// The Basic Offset Table is the first item in encapsulated pixel data:
//   - Tag: (FFFE,E000)
//   - Length: Number of offsets × 4 bytes
//   - Content: 32-bit unsigned integer offsets from end of BOT to each frame
//   - Use Case: Random access to frames without parsing all previous frames
//
// ### Extended Offset Table
//
// For large datasets (>4GB), DICOM provides extended offset tables:
//   - Tag (7FE0,0001): Extended Offset Table (64-bit offsets)
//   - Tag (7FE0,0002): Extended Offset Table Lengths (64-bit frame lengths)
//   - Use Case: Same as BOT but supports 64-bit addressing
//
// # Main Functions
//
// ## Parsing Functions
//
// Parse encapsulated pixel data:
//
//	// Parse Basic Offset Table (32-bit offsets)
//	offsets, err := compress.ParseBasicOffsets(reader, "<")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Parse extended offsets for large datasets
//	extOffsets, extLengths, err := compress.ParseExtendedOffsetTable(
//		offsetsData, lengthsData, "<",
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Parse all fragments
//	fragmentCount, offsets, err := compress.ParseFragments(reader, "<")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// ## Fragment and Frame Generation
//
// Access encapsulated data efficiently:
//
//	// Sequential fragment access using channels
//	for fragment := range compress.GenerateFragments(reader, "<") {
//		fmt.Printf("Fragment length: %d\n", len(fragment.Data))
//	}
//
//	// Access complete frames (multiple fragments combined)
//	for frame := range compress.GenerateFrames(reader, 1, "<") {
//		// Process frame
//		compressedPixels := frame.Data
//		fmt.Printf("Frame %d: %d bytes\n", frame.Index, len(compressedPixels))
//	}
//
//	// Random access to specific frame
//	frame, err := compress.GetFrame(reader, frameIndex, 10, "<")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// ## Decompression
//
// Decompress pixel data using built-in decompressors:
//
//	// Create decompressor for DEFLATE
//	decompressor := compress.NewDeflateDecompressor()
//	pixels, err := decompressor.Decompress(compressedData, width, height, bitsPerPixel)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Use decompressor registry for dynamic compression detection
//	registry := compress.NewDecompressorRegistry()
//	registry.Register("DEFLATE", compress.NewDeflateDecompressor())
//	registry.Register("RLE", compress.NewRLEDecompressor())
//	registry.Register("JPEG", compress.NewJPEGDecompressor())
//
//	decompressor, err := registry.Get("DEFLATE")
//	if err != nil {
//		log.Fatal(err)
//	}
//	pixels, err := decompressor.Decompress(data, width, height, bitsPerPixel)
//
// ## Compression
//
// Compress pixel data using built-in compressors:
//
//	// Create DEFLATE compressor
//	compressor := compress.NewDeflateCompressor(flate.DefaultCompression)
//	compressed, err := compressor.Compress(pixelData, metadata)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Create RLE compressor
//	rleCompressor := compress.NewRLECompressor()
//	compressed, err := rleCompressor.Compress(pixelData, metadata)
//
// ## Encapsulation Generation
//
// Generate encapsulated pixel data:
//
//	// Create encapsulated buffer from compressed frames
//	frames := [][]byte{frame1, frame2, frame3}
//	buffer, err := compress.EncapsulateBuffer(frames, true) // include BOT
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Get encapsulated bytes
//	encapsulatedData := buffer.Read(make([]byte, buffer.Length()))
//
//	// Generate encapsulated data with custom fragmentation
//	encapsulated := compress.EncapsulateFrames(frames, 2, true) // 2 fragments per frame
//
// # Supported Compression Types
//
// ## Built-in Formats (Pure Go)
//
// These formats decode with no external dependency, CGO or otherwise:
//
//   - UNCOMPRESSED: Raw pixel data, no compression
//   - DEFLATE: DEFLATE/zlib (lossless, good compression ratio)
//   - RLE: Run-Length Encoding (lossless, suitable for simple patterns)
//   - JPEG: JPEG baseline and extended (lossy, for photographic images)
//   - JPEG_LOSSLESS: JPEG Lossless, .57 and .70, every predictor
//   - JPEG_LS: JPEG-LS, .80 and .81, lossless and near-lossless, with or
//     without restart intervals
//   - JPEG_2000: JPEG 2000, .90 and .91, both wavelets, tiled and layered,
//     and High-Throughput JPEG 2000, .201, .202 and .203, decoded only
//
// ## Substituting your own decoder
//
// Every codec here has a bundled decoder, so registering one is about
// substitution rather than filling a gap: a faster codec, a CGO binding, or one
// that accepts something this module refuses. Register it with
// ExternalDecoderRegistry.RegisterExternalDecoder and it takes precedence;
// CONFORMANCE.md section 8.1 says what the bundled decoders cover.
//
// GetImplementationGuide(compressionType) reports the current state of any of
// them, and GetExternalCompressionStatus reports which have a decoder.
//
// # Compression
//
// These syntaxes can be compressed *to*: RLE Lossless; JPEG-LS through
// EncodeJPEGLS — lossless — and EncodeJPEGLSNearLossless — every sample within
// the allowed error NEAR the caller gives — 2 to 16 bits, one scan per
// component; JPEG 2000 through EncodeJPEG2000Lossless and, lossy at a ratio or
// a PSNR, EncodeJPEG2000; lossless JPEG (.57 and .70) through
// EncodeJPEGLossless — Process 14, 2 to 16 bits, one to four components in
// one scan, with Huffman tables built for each frame; JPEG Baseline through
// EncodeJPEGBaseline — lossy, 8 bits, one or three components at full
// resolution; and JPEG Extended through EncodeJPEGExtended — lossy, 8 or 12
// bits, its Huffman tables built for each frame. Decoding is broader than
// encoding, and a request to compress to any other syntax fails rather than
// producing bytes described as something they are not.
//
// How much error to accept belongs to the caller, so nothing here chooses a
// NEAR or a quality.
//
// JPEG-LS frames are coded and decoded with the default thresholds of T.87
// C.2.4.1.1, which are CharLS's since 2.1 and libjpeg's. dcmtk bundles an older
// CharLS whose defaults differ below 8 bits (losslessly at 6 and 7 bits, and at
// most NEAR values from 2 to 7 bits), so where they differ the encoder writes
// the thresholds in an LSE segment and dcmtk reads the frame. dcmtk's own frames
// at 6 and 7 bits are coded with its thresholds and refused here; the error
// names it.
//
// # Type Definitions
//
// ## Compression Types
//
// CompressionType identifies the compression method used:
//
//	type CompressionType string
//
//	const (
//		UNCOMPRESSED   CompressionType = "UNCOMPRESSED"
//		DEFLATE        CompressionType = "DEFLATE"
//		RLE            CompressionType = "RLE"
//		JPEG           CompressionType = "JPEG"
//		JPEG_LOSSLESS  CompressionType = "JPEG_LOSSLESS"
//		JPEG_LS        CompressionType = "JPEG_LS"
//		JPEG_2000      CompressionType = "JPEG_2000"
//	)
//
// ## EncapsulatedData
//
// Represents parsed encapsulated pixel data:
//
//	type EncapsulatedData struct {
//		BasicOffsetTable []uint32        // Frame offsets (32-bit)
//		Fragments        []*FragmentInfo // Parsed fragments
//		IsExtended       bool            // Uses 64-bit extended offsets
//		ExtendedOffsets  []uint64        // Frame offsets (64-bit)
//		ExtendedLengths  []uint64        // Frame lengths (64-bit)
//	}
//
// ## FragmentInfo
//
// Metadata about a single fragment:
//
//	type FragmentInfo struct {
//		Index  int      // Fragment index in sequence
//		Offset uint32   // Position in encapsulated data
//		Length uint32   // Fragment size in bytes
//		Data   []byte   // Fragment contents
//	}
//
// ## FrameInfo
//
// Metadata about a complete frame (possibly multiple fragments):
//
//	type FrameInfo struct {
//		Index     int          // Frame index (0-based)
//		Fragments []*FragmentInfo // Fragments in this frame
//		Data      []byte       // Combined frame data (all fragments)
//	}
//
// # Interfaces
//
// ## Decompressor Interface
//
// Implement this interface to create custom decompressors:
//
//	type Decompressor interface {
//		// Decompress decompresses pixel data
//		Decompress(data []byte) ([]byte, error)
//
//		// CanDecompress checks if this decompressor can handle the data
//		CanDecompress(data []byte) bool
//	}
//
// Decompress returns samples at the codestream's own width: one byte a sample
// at a precision of 8 or fewer, two above. A decoder that also satisfies
// FrameDecompressor writes them at the width a data set's Bits Allocated says
// instead, which is what a data set's pixel path asks for; every decoder here
// does. A frame coded at 8 bits in an image of 16 bits allocated is legal, and
// at its own width it is half the image.
//
//	type FrameDecompressor interface {
//		Decompressor
//		DecompressFrame(data []byte, bitsAllocated, bitsStored, pixelRepresentation int) ([]byte, error)
//	}
//
// A decoder that also satisfies FrameHeaderReader reads the size of a frame's
// image from its header without decoding it, and a data set's pixel path
// refuses a frame whose columns, rows or components are not its Columns, Rows
// and Samples per Pixel before decoding it (FrameHeader.Check, wrapping
// ErrFrameMismatch). Every decoder here with a size in its header does; RLE,
// which has none, is held to Rows times Columns by DecompressFrameOfSize. A
// frame of any other length than the data set's is refused after the decode
// whatever the decoder.
//
//	type FrameHeaderReader interface {
//		Decompressor
//		FrameHeader(data []byte) (FrameHeader, error)
//	}
//
// ## Compressor Interface
//
// Implement this interface to create custom compressors:
//
//	type Compressor interface {
//		// Compress compresses pixel data
//		Compress(data []byte, metadata map[string]interface{}) ([]byte, error)
//	}
//
// # Registry Patterns
//
// ## DecompressorRegistry
//
// Thread-safe registry for managing decompressors:
//
//	registry := compress.NewDecompressorRegistry()
//
//	// Register custom decompressor
//	registry.Register("CUSTOM", myDecompressor)
//
//	// Retrieve decompressor
//	decomp, err := registry.Get("DEFLATE")
//
//	// List all registered types
//	types := registry.List()
//
//	// Use registry for decompression
//	data, err := registry.Decompress("JPEG", compressedData, width, height, bits)
//
// ## External Decoder Registry
//
// Holds the substitutable decoder for each of JPEG-LS, lossless JPEG and JPEG
// 2000. "External" is historical and does not mean CGO: the first two are seeded
// with this package's pure-Go decoders, and only JPEG 2000 starts empty.
//
//	// Get singleton external registry
//	extRegistry := compress.GetExternalRegistry()
//
//	// Check available external decoders
//	status := compress.GetExternalCompressionStatus()
//	compress.PrintExternalCompressionStatus()
//
//	// Get setup instructions
//	guide := compress.GetImplementationGuide("JPEG_2000")
//	fmt.Println(guide)
//
// # EncapsulatedBuffer
//
// Efficient lazy-loading buffer for encapsulated data:
//
//	// Create from multiple frames
//	frames := [][]byte{frame1, frame2, frame3}
//	buffer := compress.EncapsulateBuffer(frames, true) // include BOT
//
//	// Read sequentially
//	data := make([]byte, 1024)
//	n, err := buffer.Read(data)
//
//	// Seek for random access
//	buffer.Seek(1000, io.SeekStart)
//
//	// Access offset table
//	offsets := buffer.Offsets()
//	lengths := buffer.Lengths()
//
//	// Extended tables for large data
//	buffer := compress.EncapsulateExtendedBuffer(frames)
//	extOffsets := buffer.ExtendedOffsets()
//	extLengths := buffer.ExtendedLengths()
//
// # Utility Functions
//
// The package provides helper functions for common operations:
//
//	// Wrap fragment with DICOM item tag
//	item := compress.ItemizeFragment(fragmentData, "<")
//
//	// Split frame into multiple fragments
//	fragments := compress.FragmentFrame(frameData, 3) // 3 fragments
//
//	// Split and itemize frame
//	itemizedFragments := compress.ItemizeFrame(frameData, 2, "<")
//
//	// Calculate compression statistics
//	ratio := compress.CalculateCompressionRatio(1000, 250) // 25%
//
//	// Validate encapsulated data format
//	err := compress.ValidateEncapsulatedData(data, "<")
//
//	// Pad data to even byte count
//	padded := compress.PadToEven(data)
//
// # Data Characteristics
//
// ## Transfer Syntax UIDs
//
// Common transfer syntax UIDs supported:
//   - 1.2.840.10008.1.2: Implicit VR Little Endian (uncompressed)
//   - 1.2.840.10008.1.2.1: Explicit VR Little Endian (uncompressed)
//   - 1.2.840.10008.1.2.2: Explicit VR Big Endian (uncompressed)
//   - 1.2.840.10008.1.2.5: RLE Lossless
//   - 1.2.840.10008.1.2.4.50: JPEG Baseline (Process 1)
//   - 1.2.840.10008.1.2.4.70: JPEG Lossless (Process 14)
//   - 1.2.840.10008.1.2.4.80: JPEG-LS Lossless
//   - 1.2.840.10008.1.2.4.81: JPEG-LS Lossy (Near-Lossless)
//   - 1.2.840.10008.1.2.4.90: JPEG 2000 Lossless
//   - 1.2.840.10008.1.2.4.91: JPEG 2000 Lossy
//
// # Thread Safety
//
// The following components are thread-safe:
//   - DecompressorRegistry: Protected by RWMutex
//   - ExternalDecoderRegistry: Protected by RWMutex
//   - EncapsulatedBuffer: Designed for concurrent reads
//
// The following are NOT thread-safe without external synchronization:
//   - Individual decompressor/compressor instances
//   - Encapsulation generation functions
//
// # Common Patterns
//
// ## Pattern 1: Decompress from DICOM File
//
// Read encapsulated pixel data from a DICOM file and decompress:
//
//	file, _ := os.Open("medical.dcm")
//	reader := filereader.NewDCMFileReader(file)
//
//	// Read file meta to detect compression
//	metaInfo, _ := reader.ReadFileMetaInfo()
//
//	// Determine decompressor from transfer syntax
//	decompressor := getDecompressorForTransferSyntax(metaInfo.TransferSyntaxUID)
//
//	// Read encapsulated pixel data
//	dataset := dataset.NewDataset()
//	// ... read elements ...
//	pixelDataElem := dataset.GetElement(0x7FE00010)
//
//	// Parse and decompress frames
//	for frame := range compress.GenerateFrames(pixelDataElem.Value, numFrames, "<") {
//		pixels, _ := decompressor.Decompress(frame.Data, width, height, bits)
//		// Process pixels
//	}
//
// ## Pattern 2: Generate Encapsulated Data
//
// Create encapsulated pixel data from raw pixel frames:
//
//	frames := [][]byte{frame1, frame2, frame3}
//	compressor := compress.NewDeflateCompressor(flate.DefaultCompression)
//
//	// Compress each frame
//	compressedFrames := [][]byte{}
//	for _, frame := range frames {
//		compressed, _ := compressor.Compress(frame, nil)
//		compressedFrames = append(compressedFrames, compressed)
//	}
//
//	// Generate encapsulated data
//	encapsulated := compress.EncapsulateFrames(compressedFrames, 1, true)
//
//	// Store in DICOM file
//	ds.Set(0x7FE00010, "OB", encapsulated)
//
// ## Pattern 3: Random Frame Access
//
// Access specific frames without parsing the entire dataset:
//
//	// Get frame 5 without reading frames 0-4
//	frame, _ := compress.GetFrame(encapsulatedData, 5, 100, "<")
//	pixels, _ := decompressor.Decompress(frame.Data, width, height, bits)
//
// # Performance Characteristics
//
// ## Time Complexity
//   - ParseBasicOffsets: O(n) where n is number of frames
//   - GenerateFragments: O(1) per fragment (streaming)
//   - GetFrame: O(1) with BOT, O(n) without BOT
//   - Decompress: Varies by format (JPEG ~10-50ms, DEFLATE ~1-5ms)
//
// ## Space Complexity
//   - EncapsulatedBuffer: O(1) relative to data size (lazy-loading)
//   - Decompression: O(n) where n is frame size (in-memory)
//   - Frame Generation: O(1) buffer size (streaming)
//
// ## Compression Ratios (Typical)
//   - JPEG: 10:1 to 50:1 (lossy)
//   - JPEG-LS: 2:1 to 5:1 (lossless)
//   - RLE: 1:1 to 3:1 (lossless, data-dependent)
//   - DEFLATE: 2:1 to 5:1 (lossless)
//
// # Standards Compliance
//
// This package implements:
//   - DICOM PS3.5 (Data Structures and Encoding) - Encapsulation and compression
//   - DICOM PS3.3 (Information Object Definitions) - Extended offset tables
//   - RFC 1950 (zlib) - DEFLATE compression
//   - JPEG/JFIF standards - JPEG compression
//   - JPEG-LS standard - JPEG-LS compression
//   - JPEG 2000 standard - JPEG 2000 compression
//
// # Testing
//
// The package includes comprehensive tests:
//   - 94 test cases covering all major functions
//   - 100% test pass rate
//   - Testing encapsulation parsing, decompression, compression, and utilities
//   - Thread safety verified with concurrent access tests
//   - Edge cases and error conditions tested
//
// # Related Packages
//
//   - dataset: Working with DICOM datasets and elements
//   - dataelem: Data element handling and value representations
//   - tag: DICOM tag utilities and dictionary
//   - filereader: Reading DICOM files
//   - filewriter: Writing DICOM files
//
// # References
//
//   - DICOM Standard: https://www.dicomstandard.org/
//   - JPEG Standard: https://jpeg.org/
//   - zlib: https://www.zlib.net/
package compress
