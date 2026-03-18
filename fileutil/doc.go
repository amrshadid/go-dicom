// Package fileutil provides comprehensive file and data utilities for DICOM processing.
//
// This package implements low-level file I/O utilities, byte order detection, data conversion,
// caching mechanisms, and codec integration for DICOM file reading and writing. It supports
// multiple transfer syntaxes, pixel data decompression, and efficient buffer management.
//
// # Core Concepts
//
// ## Byte Order Detection
//
// Detects byte order (little-endian, big-endian) from transfer syntax UIDs or DICOM file structure.
// Essential for correctly interpreting binary data in DICOM files.
//
// ## Data Conversion
//
// Utilities for converting between different data types (uint16, uint32, bytes) with proper
// byte order handling. Supports both little-endian and big-endian byte orders.
//
// ## Padding Utilities
//
// DICOM requires even-length values. Provides functions to pad values to even length and
// remove padding. Supports different padding bytes (space, null).
//
// ## Bounded Reading
//
// Enforces reading limits to prevent buffer overflows. ReaderWithBoundary ensures that
// reads do not exceed a specified byte boundary, essential for structured DICOM parsing.
//
// ## Caching System
//
// Multiple cache types for performance optimization:
//   - FileMetaCache: General-purpose metadata caching with LRU-like eviction
//   - PixelDataCache: Specialized cache for decompressed pixel data
//   - Tracks hits/misses for statistics
//
// ## Tag Indexing
//
// TagIndex creates fast lookup structures mapping DICOM tags to their file positions.
// Enables efficient random access to elements within DICOM files.
//
// ## Codec Integration
//
// Manages decompression of various pixel data encodings (DEFLATE, RLE, JPEG).
// Supports custom codec registration and deferred pixel data loading.
//
// # Basic Usage
//
// ## Detecting Byte Order
//
//	uid := uid.New("1.2.840.10008.1.2.1")  // Explicit VR Little Endian
//	byteOrder, err := fileutil.DetectFromTransferSyntax(uid)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Padding Values
//
//	value := []byte("Test")  // Length 4 (even)
//	padded := fileutil.PadValueSpace(value)  // Still 4 bytes (already even)
//
//	value = []byte("A")  // Length 1 (odd)
//	padded = fileutil.PadValueNull(value)  // Now 2 bytes: [A, 0x00]
//
// ## Converting Data Types
//
//	// Convert uint16 to bytes with little-endian byte order
//	value := uint16(0x1234)
//	bytes := fileutil.Uint16ToBytes(value, filebase.LittleEndian)
//	// Result: [0x34, 0x12]
//
//	// Convert bytes back to uint16
//	value = fileutil.BytesToUint16(bytes, filebase.LittleEndian)
//	// Result: 0x1234
//
// ## Using Buffer Pool
//
//	pool := fileutil.NewByteBufferPool(10, 4096)  // 10 buffers, 4096 bytes each
//	buf := pool.Get()  // Get buffer from pool or allocate new
//	// Use buffer...
//	pool.Put(buf)  // Return to pool for reuse
//
// ## Bounded Reading
//
//	reader := filebase.NewFileReader(file)
//	bounded := fileutil.NewReaderWithBoundary(reader, 1024)  // Limit to 1024 bytes
//	if err := bounded.ReadBytes(data); err != nil {
//	    log.Fatal(err)
//	}
//
// # Advanced Usage
//
// ## Tag Indexing
//
//	index := fileutil.NewTagIndex()
//	for _, pos := range positions {
//	    index.Add(pos)
//	}
//	// Later, fast lookup
//	pos, found := index.Get(tag.New(0x0010, 0x0010))
//
// ## Codec Integration
//
//	ci := fileutil.NewCodecIntegration()
//
//	// Check compression support
//	supported := ci.IsCompressionSupported(compress.DEFLATE)
//	if !supported {
//	    log.Fatal("DEFLATE not supported")
//	}
//
//	// Decompress pixel data
//	decompressed, err := ci.Decompress(compressedData, compress.DEFLATE)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Deferred Pixel Data Loading
//
//	ci := fileutil.NewCodecIntegration()
//	reader := fileutil.NewDeferredPixelDataReader(
//	    "dicom.dcm",
//	    compress.UNCOMPRESSED,
//	    1024,      // Offset in file
//	    65536,     // Length
//	    ci,
//	)
//
//	// Data is not loaded yet
//	if !reader.IsLoaded() {
//	    data, err := reader.Get()  // Loads on first access
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// ## File Metadata Caching
//
//	cache := fileutil.NewFileMetaCache(100)  // 100-item cache
//
//	cache.Set("patient_name", "Doe^John")
//	if val, ok := cache.Get("patient_name"); ok {
//	    fmt.Println(val)
//	}
//
//	hits, misses := cache.GetStats()
//	fmt.Printf("Cache hits: %d, misses: %d\n", hits, misses)
//
// # Data Structures
//
// ## ByteOrderDetector
//
// Detects byte order from DICOM files. Examines transfer syntax UIDs to determine
// whether data should be interpreted as little-endian or big-endian.
//
// ## ByteBufferPool
//
// Manages a pool of reusable byte buffers to reduce allocation overhead.
// Pre-allocates buffers of specified size and reuses them across operations.
//
// ## FileMetaCache
//
// General-purpose metadata cache with LRU-like eviction. Thread-safe with separate
// read and write locks. Tracks hit/miss statistics.
//
// ## TagIndex
//
// Maps DICOM tags to their file positions. Uses uint32 keys for fast lookups.
// Maintains both map-based indexing and ordered list of positions.
//
// ## ReaderWithBoundary
//
// Wraps a filebase.Reader to enforce a byte boundary limit. Prevents reading beyond
// the specified number of bytes. Tracks bytes read and remaining.
//
// ## CodecIntegration
//
// Manages codec registration and pixel data decompression. Maintains a decompressor
// registry for various compression types. Includes caching for decompressed data.
//
// ## DeferredPixelDataReader
//
// Lazy-loads pixel data from file on first access. Caches decompressed data.
// Reduces memory usage for large DICOM files.
//
// ## PixelDataCache
//
// Specialized cache for pixel data. LRU-like eviction when full. Tracks size.
//
// # API Reference
//
// ## Byte Order Detection
//
// ### DetectFromTransferSyntax
//
//	func DetectFromTransferSyntax(ts uid.UID) (filebase.ByteOrder, error)
//
// Determines byte order from transfer syntax UID. Returns LittleEndian for most syntaxes,
// BigEndian only for explicit VR big-endian.
//
// ### DetectFromPreamble
//
//	func DetectFromPreamble(reader filebase.Reader) (bool, error)
//
// Checks if file starts with 128-byte preamble followed by "DICM" magic string.
// Verifies DICOM file format compliance.
//
// ## Padding Functions
//
// ### PadValue
//
//	func PadValue(value []byte, padByte byte) []byte
//
// Pads value to even length if needed using specified padding byte. Returns unchanged
// if already even length.
//
// ### PadValueSpace / PadValueNull
//
//	func PadValueSpace(value []byte) []byte
//	func PadValueNull(value []byte) []byte
//
// Convenience functions that pad with space (0x20) or null (0x00) respectively.
//
// ### UnpadValue
//
//	func UnpadValue(value []byte, padByte byte) []byte
//
// Removes trailing occurrences of specified padding byte.
//
// ## Alignment
//
// ### AlignToEvenBoundary
//
//	func AlignToEvenBoundary(position int64) int64
//
// Returns next even byte position. If already even, returns unchanged.
//
// ## Data Conversion
//
// ### Uint16ToBytes / Uint32ToBytes
//
//	func Uint16ToBytes(value uint16, byteOrder filebase.ByteOrder) []byte
//	func Uint32ToBytes(value uint32, byteOrder filebase.ByteOrder) []byte
//
// Converts unsigned integers to bytes with specified byte order.
//
// ### BytesToUint16 / BytesToUint32
//
//	func BytesToUint16(data []byte, byteOrder filebase.ByteOrder) uint16
//	func BytesToUint32(data []byte, byteOrder filebase.ByteOrder) uint32
//
// Converts bytes to unsigned integers with specified byte order. Returns 0 if insufficient data.
//
// ### ReadBytesWithByteOrder / WriteBytesWithByteOrder
//
//	func ReadBytesWithByteOrder(data []byte, byteOrder filebase.ByteOrder) []byte
//	func WriteBytesWithByteOrder(value []byte, byteOrder filebase.ByteOrder) []byte
//
// Applies byte order conversion to byte arrays. For big-endian, reverses byte order.
//
// ## Buffer Management
//
// ### NewByteBufferPool
//
//	func NewByteBufferPool(poolSize, bufferSize int) *ByteBufferPool
//
// Creates buffer pool with specified number of buffers and buffer size.
//
// ## Caching
//
// ### FileMetaCache
//
//	func NewFileMetaCache(maxSize int) *FileMetaCache
//	func (fmc *FileMetaCache) Get(key string) (interface{}, bool)
//	func (fmc *FileMetaCache) Set(key string, value interface{})
//	func (fmc *FileMetaCache) Clear()
//	func (fmc *FileMetaCache) GetStats() (hits, misses int64)
//
// General-purpose metadata cache with statistics tracking.
//
// ## Tag Indexing
//
// ### TagIndex
//
//	func NewTagIndex() *TagIndex
//	func (ti *TagIndex) Add(position *filebase.Position)
//	func (ti *TagIndex) Get(t tag.Tag) (*filebase.Position, bool)
//	func (ti *TagIndex) GetAll() []*filebase.Position
//	func (ti *TagIndex) Contains(t tag.Tag) bool
//	func (ti *TagIndex) Count() int
//	func (ti *TagIndex) Clear()
//
// Maps tags to file positions for efficient element location.
//
// ## Bounded Reading
//
// ### ReaderWithBoundary
//
//	func NewReaderWithBoundary(reader filebase.Reader, boundary int64) *ReaderWithBoundary
//	func (rwb *ReaderWithBoundary) ReadBytes(b []byte) error
//	func (rwb *ReaderWithBoundary) ReadByte() (byte, error)
//	func (rwb *ReaderWithBoundary) ReadUint16() (uint16, error)
//	func (rwb *ReaderWithBoundary) ReadUint32() (uint32, error)
//	func (rwb *ReaderWithBoundary) Seek(offset int64, whence int) (int64, error)
//	func (rwb *ReaderWithBoundary) Tell() (int64, error)
//	func (rwb *ReaderWithBoundary) GetBytesRead() int64
//	func (rwb *ReaderWithBoundary) GetBytesRemaining() int64
//
// Enforces reading limits on wrapped reader.
//
// ## Codec Integration
//
// ### CodecIntegration
//
//	func NewCodecIntegration() *CodecIntegration
//	func (ci *CodecIntegration) Decompress(data []byte, compressionType compress.CompressionType) ([]byte, error)
//	func (ci *CodecIntegration) IsCompressionSupported(compressionType compress.CompressionType) bool
//	func (ci *CodecIntegration) GetSupportedCompressions() []compress.CompressionType
//	func (ci *CodecIntegration) GetCompressionInfo(compressionType compress.CompressionType) compress.CompressionInfo
//	func (ci *CodecIntegration) RegisterCustomCodec(compressionType compress.CompressionType, decompressor compress.Decompressor) error
//	func (ci *CodecIntegration) DecompressSegmentedPixelData(segments [][]byte, compressionType compress.CompressionType) ([]byte, error)
//	func (ci *CodecIntegration) ValidatePixelData(data []byte, compressionType compress.CompressionType, expectedLength int) error
//	func (ci *CodecIntegration) CalculateCompressionStatistics(originalData, compressedData []byte, compressionType compress.CompressionType) CompressionStatistics
//	func (ci *CodecIntegration) GetTransferSyntaxSupport(uid string) TransferSyntaxSupport
//
// Manages codec selection and pixel data decompression.
//
// ### DeferredPixelDataReader
//
//	func NewDeferredPixelDataReader(filePath string, compressionType compress.CompressionType, offset, length int64, integration *CodecIntegration) *DeferredPixelDataReader
//	func (dpdr *DeferredPixelDataReader) Load() error
//	func (dpdr *DeferredPixelDataReader) Get() ([]byte, error)
//	func (dpdr *DeferredPixelDataReader) IsLoaded() bool
//
// Lazy-loads pixel data from file with optional caching and decompression.
//
// # Performance Characteristics
//
// | Operation | Complexity | Description |
// |-----------|-----------|-------------|
// | DetectFromTransferSyntax | O(1) | String lookup in map |
// | DetectFromPreamble | O(1) | Seek and read fixed bytes |
// | PadValue/UnpadValue | O(n) | n = value length |
// | Uint16ToBytes/BytesToUint16 | O(1) | Constant-time conversion |
// | Uint32ToBytes/BytesToUint32 | O(1) | Constant-time conversion |
// | ByteBufferPool.Get/Put | O(1) | Channel operation (amortized) |
// | FileMetaCache.Get/Set | O(1) | Hash map operation |
// | TagIndex.Add/Get | O(1) | Hash map operation |
// | ReaderWithBoundary.ReadBytes | O(n) | n = bytes to read |
// | CodecIntegration.Decompress | O(n) | n = data size, codec-dependent |
// | CalculateGroupLength | O(m) | m = number of elements |
//
// # Thread Safety
//
// Most types are thread-safe through sync.RWMutex:
//   - FileMetaCache: Safe for concurrent reads/writes
//   - TagIndex: Safe for concurrent reads/writes
//   - ReaderWithBoundary: Safe for concurrent operations
//   - CodecIntegration: Safe for codec operations
//   - ByteBufferPool: Channel-based, inherently thread-safe
//
// # Supported Compression Types
//
// Supported pixel data compressions:
//   - UNCOMPRESSED: No compression
//   - DEFLATE: RFC 1951 deflate compression
//   - RLE: Run-Length Encoding (lossless)
//   - JPEG: JPEG lossy compression
//
// # Error Handling
//
// | Operation | Error Condition |
// |-----------|-----------------|
// | DetectFromTransferSyntax | Unknown transfer syntax UID |
// | DetectFromPreamble | Seek/read failure, short data |
// | BytesToUint16/Uint32 | Insufficient data (returns 0) |
// | Decompress | Unsupported compression type |
// | ReaderWithBoundary.ReadBytes | Exceed boundary limit |
// | ValidatePixelData | Empty data, length mismatch, decompression failure |
// | CalculateGroupLength | Elements with mismatched group |
//
// # Use Cases
//
// ## Byte Order Detection in DICOM Parser
//
// Determine how to interpret DICOM element data based on transfer syntax.
//
// ## Efficient Pixel Data Handling
//
// Load large pixel data lazily and cache decompressed results to minimize memory usage.
//
// ## Random Access to Elements
//
// Build tag index for fast lookup of specific elements without parsing entire file.
//
// ## Data Validation
//
// Use bounded readers to prevent buffer overflows. Validate pixel data integrity.
//
// ## Format Conversion
//
// Convert data types with proper byte order for cross-platform compatibility.
//
// # Related Packages
//
//   - filebase: Low-level file I/O interface
//   - compress: Codec implementations and decompression registry
//   - tag: DICOM tag definitions and operations
//   - uid: DICOM UID definitions and utilities
//
// # DICOM Compliance
//
// Implements DICOM standard (PS3.10) for:
//   - File format detection (preamble, magic string)
//   - Byte order handling (little-endian, big-endian)
//   - Padding requirements (even-length values)
//   - Transfer syntax interpretation
//   - Pixel data decompression support
//
// See: https://www.dicomstandard.org/
package fileutil
