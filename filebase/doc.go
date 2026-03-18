// Package filebase provides low-level file I/O abstractions for DICOM file operations.
//
// This package implements core interfaces and concrete implementations for reading and writing
// binary data with proper byte order (endianness) handling. It provides the foundational I/O
// abstractions used throughout the go-dicom library for handling DICOM files with different
// transfer syntaxes and byte orders.
//
// # Core Concepts
//
// ## ByteOrder
//
// Specifies the byte order (endianness) for multi-byte integer operations:
//   - LittleEndian (0): Standard byte order, least significant byte first (Intel x86)
//   - BigEndian (1): Network byte order, most significant byte first (big-endian architectures)
//
// Most DICOM files use little-endian byte order, but explicit VR big-endian is defined
// in the DICOM standard for specific transfer syntaxes.
//
// ## Reader Interface
//
// Defines methods for reading binary data from a file or stream with configurable byte order:
//   - Read: Read exact number of bytes into buffer
//   - ReadByte: Read single byte
//   - ReadBytes: Read multiple bytes into pre-allocated buffer
//   - ReadUint16/ReadUint32: Read integers with automatic byte order conversion
//   - Seek: Change file position
//   - Tell: Get current position
//   - Close: Release resources
//   - GetByteOrder/SetByteOrder: Manage endianness configuration
//
// ## Writer Interface
//
// Defines methods for writing binary data to a file or stream with configurable byte order:
//   - Write: Write exact number of bytes from buffer
//   - WriteByte: Write single byte
//   - WriteBytes: Write multiple bytes
//   - WriteUint16/WriteUint32: Write integers with automatic byte order conversion
//   - Seek: Change file position
//   - Tell: Get current position
//   - Close: Release resources
//   - Flush: Ensure all data written to underlying storage
//   - GetByteOrder/SetByteOrder: Manage endianness configuration
//
// ## FileReader
//
// Concrete implementation of Reader interface. Manages reading from an io.ReadSeeker
// with configurable byte order. Uses sync.RWMutex for thread-safe position tracking.
//
// ## FileWriter
//
// Concrete implementation of Writer interface. Manages writing to an io.ReadWriteSeeker
// with configurable byte order. Uses sync.RWMutex for thread-safe position tracking.
//
// ## BufferPool
//
// Memory pool for reusable byte slices. Reduces allocation overhead in applications
// that perform repeated read/write operations by pooling and reusing buffers.
//
// ## Position
//
// Struct for tracking file position information during DICOM parsing. Stores offset,
// tag, VR (Value Representation), and element length for error reporting.
//
// # Basic Usage
//
// ## Reading Binary Data
//
//	import (
//	    "log"
//	    "os"
//	    "github.com/amrshadid/go-dicom/filebase"
//	)
//
//	func main() {
//	    // Open file
//	    file, err := os.Open("data.bin")
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    defer file.Close()
//
//	    // Create reader with little-endian byte order
//	    reader := filebase.NewFileReader(file)
//	    defer reader.Close()
//
//	    // Read single byte
//	    b, err := reader.ReadByte()
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    fmt.Printf("Byte: 0x%02x\n", b)
//
//	    // Read 16-bit unsigned integer
//	    v16, err := reader.ReadUint16()
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    fmt.Printf("Uint16: 0x%04x\n", v16)
//	}
//
// ## Writing Binary Data
//
//	import (
//	    "bytes"
//	    "log"
//	    "github.com/amrshadid/go-dicom/filebase"
//	)
//
//	func main() {
//	    buf := &bytes.Buffer{}
//	    rws := &readWriteSeeker{Buffer: buf}
//
//	    // Create writer
//	    writer := filebase.NewFileWriter(rws)
//	    defer writer.Close()
//
//	    // Write single byte
//	    err := writer.WriteByte(0x42)
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//
//	    // Write 32-bit unsigned integer
//	    err = writer.WriteUint32(0x12345678)
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// # Advanced Usage
//
// ## Byte Order Handling
//
// Switch byte order during reading to handle different DICOM transfer syntaxes:
//
//	reader := filebase.NewFileReader(file)
//
//	// Read as little-endian
//	reader.SetByteOrder(filebase.LittleEndian)
//	v16, _ := reader.ReadUint16()
//	fmt.Printf("LE: 0x%04x\n", v16)
//
//	// Seek back and read as big-endian
//	reader.Seek(0, 0)  // io.SeekStart
//	reader.SetByteOrder(filebase.BigEndian)
//	v16, _ = reader.ReadUint16()
//	fmt.Printf("BE: 0x%04x\n", v16)
//
// ## Position Tracking
//
// Track file position for error reporting and recovery:
//
//	reader := filebase.NewFileReader(file)
//
//	startPos, _ := reader.Tell()
//	fmt.Printf("Starting at: %d\n", startPos)
//
//	// Read some data
//	reader.ReadBytes(make([]byte, 10))
//
//	currentPos, _ := reader.Tell()
//	fmt.Printf("After read: %d bytes consumed\n", currentPos-startPos)
//
// ## Buffer Pooling
//
// Use buffer pool to reduce allocation overhead:
//
//	pool := filebase.NewBufferPool(1024)
//	defer pool.Close()
//
//	// Get buffer from pool
//	buf := pool.Get()
//	defer pool.Put(buf)
//
//	// Use buffer...
//	copy(buf, data)
//
// ## Thread-Safe Reading
//
// Multiple goroutines can safely read different parts of file:
//
//	reader := filebase.NewFileReader(file)
//
//	go func() {
//	    reader.Seek(0, 0)
//	    reader.ReadBytes(make([]byte, 100))
//	}()
//
//	go func() {
//	    reader.Seek(1000, 0)
//	    reader.ReadBytes(make([]byte, 100))
//	}()
//
// # Data Structures
//
// ## ByteOrder
//
//	type ByteOrder uint8
//
//	const (
//	    LittleEndian ByteOrder = iota  // 0
//	    BigEndian                      // 1
//	)
//
// Enum for byte order specification with String() method for representation.
//
// ## Reader Interface
//
//	type Reader interface {
//	    Read(p []byte) (n int, err error)
//	    ReadByte() (byte, error)
//	    ReadBytes(p []byte) error
//	    ReadUint16() (uint16, error)
//	    ReadUint32() (uint32, error)
//	    Seek(offset int64, whence int) (int64, error)
//	    Tell() (int64, error)
//	    Close() error
//	    GetByteOrder() ByteOrder
//	    SetByteOrder(bo ByteOrder)
//	}
//
// Interface for reading binary data with byte order support.
//
// ## Writer Interface
//
//	type Writer interface {
//	    Write(p []byte) (n int, err error)
//	    WriteByte(b byte) error
//	    WriteBytes(p []byte) error
//	    WriteUint16(v uint16) error
//	    WriteUint32(v uint32) error
//	    Seek(offset int64, whence int) (int64, error)
//	    Tell() (int64, error)
//	    Close() error
//	    Flush() error
//	    GetByteOrder() ByteOrder
//	    SetByteOrder(bo ByteOrder)
//	}
//
// Interface for writing binary data with byte order support.
//
// ## FileReader
//
//	type FileReader struct {
//	    // Unexported fields
//	}
//
// Implements Reader interface. Manages position tracking and byte order conversion
// for reading operations.
//
// ## FileWriter
//
//	type FileWriter struct {
//	    // Unexported fields
//	}
//
// Implements Writer interface. Manages position tracking and byte order conversion
// for writing operations.
//
// ## BufferPool
//
//	type BufferPool struct {
//	    // Unexported fields
//	}
//
// Manages pool of reusable byte slices. Call Get() to retrieve a buffer,
// use it, and call Put() to return it.
//
// ## Position
//
//	type Position struct {
//	    Offset int64  // Byte offset in file
//	    Tag    uint32 // DICOM tag (for context)
//	    VR     string // Value Representation (for context)
//	    Length uint32 // Element length (for context)
//	}
//
// Tracks file position during DICOM parsing for detailed error reporting.
//
// # API Reference
//
// ## Reader/Writer Creation
//
// ### NewFileReader
//
//	func NewFileReader(r io.ReadSeeker) *FileReader
//
// Creates new FileReader for reading binary data from io.ReadSeeker.
//
// **Parameters:**
// - `r`: io.ReadSeeker for read operations
//
// **Returns:** FileReader pointer
//
// **Example:**
// ```go
// file, _ := os.Open("data.bin")
// reader := filebase.NewFileReader(file)
// ```
//
// ### NewFileWriter
//
//	func NewFileWriter(w io.ReadWriteSeeker) *FileWriter
//
// Creates new FileWriter for writing binary data to io.ReadWriteSeeker.
//
// **Parameters:**
// - `w`: io.ReadWriteSeeker for write operations
//
// **Returns:** FileWriter pointer
//
// **Example:**
// ```go
// buf := &bytes.Buffer{}
// rws := &readWriteSeeker{Buffer: buf}
// writer := filebase.NewFileWriter(rws)
// ```
//
// ### NewBufferPool
//
//	func NewBufferPool(size int) *BufferPool
//
// Creates new buffer pool with specified buffer size.
//
// **Parameters:**
// - `size`: Size of each buffer in pool
//
// **Returns:** BufferPool pointer
//
// **Example:**
// ```go
// pool := filebase.NewBufferPool(4096)
// buf := pool.Get()
// defer pool.Put(buf)
// ```
//
// ## Read Operations
//
// ### Read
//
//	func (fr *FileReader) Read(p []byte) (int, error)
//
// Reads up to len(p) bytes. Returns number of bytes read and error.
//
// **Parameters:**
// - `p`: Byte slice to read into
//
// **Returns:** Number of bytes read and error
//
// ### ReadByte
//
//	func (fr *FileReader) ReadByte() (byte, error)
//
// Reads single byte from file.
//
// **Returns:** Byte value and error
//
// **Example:**
// ```go
// b, err := reader.ReadByte()
//
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ```
//
// ### ReadBytes
//
//	func (fr *FileReader) ReadBytes(p []byte) error
//
// Reads exactly len(p) bytes into provided buffer. Returns error if insufficient data.
//
// **Parameters:**
// - `p`: Pre-allocated byte slice
//
// **Returns:** Error if read fails
//
// **Example:**
// ```go
// buf := make([]byte, 4)
// err := reader.ReadBytes(buf)
// ```
//
// ### ReadUint16
//
//	func (fr *FileReader) ReadUint16() (uint16, error)
//
// Reads 16-bit unsigned integer with configured byte order.
//
// **Returns:** uint16 value and error
//
// **Example:**
// ```go
// v, err := reader.ReadUint16()
// fmt.Printf("0x%04x\n", v)
// ```
//
// ### ReadUint32
//
//	func (fr *FileReader) ReadUint32() (uint32, error)
//
// Reads 32-bit unsigned integer with configured byte order.
//
// **Returns:** uint32 value and error
//
// **Example:**
// ```go
// v, err := reader.ReadUint32()
// fmt.Printf("0x%08x\n", v)
// ```
//
// ## Write Operations
//
// ### Write
//
//	func (fw *FileWriter) Write(p []byte) (int, error)
//
// Writes up to len(p) bytes. Returns number of bytes written and error.
//
// **Parameters:**
// - `p`: Byte slice to write
//
// **Returns:** Number of bytes written and error
//
// ### WriteByte
//
//	func (fw *FileWriter) WriteByte(b byte) error
//
// Writes single byte to file.
//
// **Parameters:**
// - `b`: Byte to write
//
// **Returns:** Error if write fails
//
// ### WriteBytes
//
//	func (fw *FileWriter) WriteBytes(p []byte) error
//
// Writes exactly len(p) bytes from provided buffer.
//
// **Parameters:**
// - `p`: Byte slice to write
//
// **Returns:** Error if write fails
//
// ### WriteUint16
//
//	func (fw *FileWriter) WriteUint16(v uint16) error
//
// Writes 16-bit unsigned integer with configured byte order.
//
// **Parameters:**
// - `v`: uint16 value to write
//
// **Returns:** Error if write fails
//
// ### WriteUint32
//
//	func (fw *FileWriter) WriteUint32(v uint32) error
//
// Writes 32-bit unsigned integer with configured byte order.
//
// **Parameters:**
// - `v`: uint32 value to write
//
// **Returns:** Error if write fails
//
// ## Position Management
//
// ### Seek
//
//	func (r *FileReader) Seek(offset int64, whence int) (int64, error)
//	func (w *FileWriter) Seek(offset int64, whence int) (int64, error)
//
// Changes file position. Whence: 0=SeekStart, 1=SeekCurrent, 2=SeekEnd.
//
// **Parameters:**
// - `offset`: Offset to seek to
// - `whence`: Reference point (io.SeekStart, io.SeekCurrent, io.SeekEnd)
//
// **Returns:** New position and error
//
// **Example:**
// ```go
// reader.Seek(128, io.SeekStart)  // Skip 128-byte preamble
// ```
//
// ### Tell
//
//	func (r *FileReader) Tell() (int64, error)
//	func (w *FileWriter) Tell() (int64, error)
//
// Returns current file position.
//
// **Returns:** Current position and error
//
// **Example:**
// ```go
// pos, _ := reader.Tell()
// fmt.Printf("Current position: %d\n", pos)
// ```
//
// ## Byte Order Management
//
// ### GetByteOrder
//
//	func (r *FileReader) GetByteOrder() ByteOrder
//	func (w *FileWriter) GetByteOrder() ByteOrder
//
// Returns current byte order setting.
//
// **Returns:** Current ByteOrder
//
// ### SetByteOrder
//
//	func (r *FileReader) SetByteOrder(bo ByteOrder)
//	func (w *FileWriter) SetByteOrder(bo ByteOrder)
//
// Sets byte order for subsequent read/write operations.
//
// **Parameters:**
// - `bo`: ByteOrder to use (LittleEndian or BigEndian)
//
// **Example:**
// ```go
// reader.SetByteOrder(filebase.BigEndian)
// ```
//
// ## Resource Management
//
// ### Close
//
//	func (r *FileReader) Close() error
//	func (w *FileWriter) Close() error
//
// Closes reader/writer and releases resources.
//
// **Returns:** Error if close fails
//
// ### Flush
//
//	func (w *FileWriter) Flush() error
//
// Flushes any pending writes to underlying storage.
//
// **Returns:** Error if flush fails
//
// # Performance Characteristics
//
// | Operation | Complexity | Description |
// |-----------|-----------|-------------|
// | NewFileReader | O(1) | Simple pointer initialization |
// | NewFileWriter | O(1) | Simple pointer initialization |
// | Read/ReadByte | O(k) | k = number of bytes read |
// | ReadBytes | O(n) | n = number of bytes in buffer |
// | ReadUint16 | O(1) | Fixed 2 bytes + conversion |
// | ReadUint32 | O(1) | Fixed 4 bytes + conversion |
// | Write/WriteByte | O(k) | k = number of bytes written |
// | WriteBytes | O(n) | n = number of bytes in buffer |
// | WriteUint16 | O(1) | Fixed 2 bytes + conversion |
// | WriteUint32 | O(1) | Fixed 4 bytes + conversion |
// | Seek | O(1) | Updates position counter |
// | Tell | O(1) | Returns position counter |
// | SetByteOrder | O(1) | Updates byte order field |
// | GetByteOrder | O(1) | Returns byte order field |
// | BufferPool.Get | O(1) | Reuses existing buffer |
// | BufferPool.Put | O(1) | Returns buffer to pool |
//
// # Thread Safety
//
// FileReader and FileWriter are thread-safe:
//
// - **Position Tracking**: sync.RWMutex protects position and byte order
// - **Read Operations**: Multiple goroutines can read concurrently with RLock
// - **Write Operations**: Write operations use exclusive Lock for atomicity
// - **Seeking**: Safe for concurrent access with proper synchronization
// - **Byte Order**: Each reader/writer has independent byte order configuration
//
// Important: BufferPool is thread-safe (uses sync.Pool internally).
//
// # Error Handling
//
// | Operation | Error Condition |
// |-----------|--------------------|
// | Read/ReadByte/ReadBytes | EOF (io.EOF), I/O errors |
// | ReadUint16/ReadUint32 | Insufficient bytes, I/O errors |
// | Write/WriteByte/WriteBytes | I/O errors, disk full |
// | WriteUint16/WriteUint32 | I/O errors, disk full |
// | Seek | Invalid offset, I/O errors |
// | Close | Underlying close failure |
// | Flush | Underlying flush failure |
//
// # Use Cases
//
// ## DICOM File Reading
//
// Read DICOM files with proper byte order handling and position tracking.
//
// ## Binary Format Parsing
//
// Parse any binary file format with automatic multi-byte integer conversion.
//
// ## Transfer Syntax Handling
//
// Support different byte orders by switching ByteOrder configuration.
//
// ## Memory-Efficient Operations
//
// Use BufferPool for repeated read/write operations to reduce allocations.
//
// ## Error Reporting
//
// Track file position for detailed error messages during parsing.
//
// # Limitations
//
// - No support for reading/writing 64-bit integers directly (use ReadBytes/WriteBytes)
// - Position type is for annotation only, not enforced by Reader/Writer
// - BufferPool reuses buffers of fixed size (no dynamic resizing)
// - No built-in caching or buffering (use io.BufReader if needed)
//
// # Related Packages
//
// - **filereader**: Reading complete DICOM files using Reader interface
// - **filewriter**: Writing DICOM files using Writer interface
// - **fileutil**: DICOM file utility functions
// - **dataset**: Higher-level DICOM dataset operations
//
// # DICOM Compliance
//
// Implements byte order handling per DICOM standard (PS3.5) for:
// - Transfer syntax encoding (explicit/implicit VR)
// - Byte order specifications (little-endian, big-endian)
// - Multi-byte integer representation
// - File position tracking
//
// See: https://www.dicomstandard.org/
package filebase
