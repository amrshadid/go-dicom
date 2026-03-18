package filebase

import (
	"fmt"
	"io"
	"sync"
)

// ByteOrder represents byte order (endianness) for DICOM data.
type ByteOrder int

const (
	LittleEndian ByteOrder = iota
	BigEndian
)

// String returns the string representation of ByteOrder.
func (bo ByteOrder) String() string {
	switch bo {
	case LittleEndian:
		return "LittleEndian"
	case BigEndian:
		return "BigEndian"
	default:
		return "Unknown"
	}
}

// Reader is an interface for reading DICOM data.
type Reader interface {
	Read(p []byte) (n int, err error)
	ReadByte() (byte, error)
	ReadBytes(b []byte) error
	ReadUint16() (uint16, error)
	ReadUint32() (uint32, error)
	Seek(offset int64, whence int) (int64, error)
	Tell() (int64, error)
	Close() error
	GetByteOrder() ByteOrder
	SetByteOrder(bo ByteOrder)
}

// Writer is an interface for writing DICOM data.
type Writer interface {
	Write(p []byte) (n int, err error)
	WriteByte(b byte) error
	WriteBytes(b []byte) error
	WriteUint16(v uint16) error
	WriteUint32(v uint32) error
	Seek(offset int64, whence int) (int64, error)
	Tell() (int64, error)
	Close() error
	Flush() error
	GetByteOrder() ByteOrder
	SetByteOrder(bo ByteOrder)
}

// FileReader implements Reader for DICOM file reading.
type FileReader struct {
	reader    io.ReadSeeker
	byteOrder ByteOrder
	position  int64
	mu        sync.RWMutex
}

// NewFileReader creates a new FileReader.
func NewFileReader(reader io.ReadSeeker) *FileReader {
	return &FileReader{
		reader:    reader,
		byteOrder: LittleEndian,
		position:  0,
	}
}

// Read reads up to len(p) bytes into p.
func (fr *FileReader) Read(p []byte) (n int, err error) {
	fr.mu.Lock()
	defer fr.mu.Unlock()

	n, err = fr.reader.Read(p)
	fr.position += int64(n)
	return n, err
}

// ReadByte reads a single byte.
func (fr *FileReader) ReadByte() (byte, error) {
	b := make([]byte, 1)
	_, err := fr.Read(b)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// ReadBytes reads exactly len(b) bytes into b.
// Returns a descriptive error if EOF is reached before reading all requested bytes.
func (fr *FileReader) ReadBytes(b []byte) error {
	n, err := fr.Read(b)
	if err != nil && err != io.EOF {
		return err
	}
	if n < len(b) {
		if err == io.EOF {
			return fmt.Errorf("expected %d bytes, got %d: reached end of file", len(b), n)
		}
		return fmt.Errorf("expected %d bytes, got %d", len(b), n)
	}
	return nil
}

// ReadUint16 reads a 16-bit unsigned integer.
func (fr *FileReader) ReadUint16() (uint16, error) {
	b := make([]byte, 2)
	if err := fr.ReadBytes(b); err != nil {
		return 0, err
	}

	fr.mu.RLock()
	bo := fr.byteOrder
	fr.mu.RUnlock()

	if bo == LittleEndian {
		return uint16(b[0]) | (uint16(b[1]) << 8), nil
	}
	return uint16(b[1]) | (uint16(b[0]) << 8), nil
}

// ReadUint32 reads a 32-bit unsigned integer.
func (fr *FileReader) ReadUint32() (uint32, error) {
	b := make([]byte, 4)
	if err := fr.ReadBytes(b); err != nil {
		return 0, err
	}

	fr.mu.RLock()
	bo := fr.byteOrder
	fr.mu.RUnlock()

	if bo == LittleEndian {
		return uint32(b[0]) | (uint32(b[1]) << 8) | (uint32(b[2]) << 16) | (uint32(b[3]) << 24), nil
	}
	return uint32(b[3]) | (uint32(b[2]) << 8) | (uint32(b[1]) << 16) | (uint32(b[0]) << 24), nil
}

// Seek seeks to the specified position.
func (fr *FileReader) Seek(offset int64, whence int) (int64, error) {
	fr.mu.Lock()
	defer fr.mu.Unlock()

	pos, err := fr.reader.Seek(offset, whence)
	if err != nil {
		return 0, err
	}
	fr.position = pos
	return pos, nil
}

// Tell returns the current position.
func (fr *FileReader) Tell() (int64, error) {
	fr.mu.RLock()
	defer fr.mu.RUnlock()

	return fr.position, nil
}

// Close closes the reader.
func (fr *FileReader) Close() error {
	if closer, ok := fr.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// GetByteOrder returns the current byte order.
func (fr *FileReader) GetByteOrder() ByteOrder {
	fr.mu.RLock()
	defer fr.mu.RUnlock()

	return fr.byteOrder
}

// SetByteOrder sets the byte order.
func (fr *FileReader) SetByteOrder(bo ByteOrder) {
	fr.mu.Lock()
	defer fr.mu.Unlock()

	fr.byteOrder = bo
}

// FileWriter implements Writer for DICOM file writing.
type FileWriter struct {
	writer    io.WriteSeeker
	byteOrder ByteOrder
	position  int64
	mu        sync.RWMutex
}

// NewFileWriter creates a new FileWriter.
func NewFileWriter(writer io.WriteSeeker) *FileWriter {
	return &FileWriter{
		writer:    writer,
		byteOrder: LittleEndian,
		position:  0,
	}
}

// Write writes len(p) bytes from p.
func (fw *FileWriter) Write(p []byte) (n int, err error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	n, err = fw.writer.Write(p)
	fw.position += int64(n)
	return n, err
}

// WriteByte writes a single byte.
func (fw *FileWriter) WriteByte(b byte) error {
	return fw.WriteBytes([]byte{b})
}

// WriteBytes writes all bytes from b.
func (fw *FileWriter) WriteBytes(b []byte) error {
	n, err := fw.Write(b)
	if err != nil {
		return err
	}
	if n < len(b) {
		return fmt.Errorf("expected to write %d bytes, wrote %d", len(b), n)
	}
	return nil
}

// WriteUint16 writes a 16-bit unsigned integer.
func (fw *FileWriter) WriteUint16(v uint16) error {
	fw.mu.RLock()
	bo := fw.byteOrder
	fw.mu.RUnlock()

	var b []byte
	if bo == LittleEndian {
		b = []byte{byte(v), byte(v >> 8)}
	} else {
		b = []byte{byte(v >> 8), byte(v)}
	}

	return fw.WriteBytes(b)
}

// WriteUint32 writes a 32-bit unsigned integer.
func (fw *FileWriter) WriteUint32(v uint32) error {
	fw.mu.RLock()
	bo := fw.byteOrder
	fw.mu.RUnlock()

	var b []byte
	if bo == LittleEndian {
		b = []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
	} else {
		b = []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	}

	return fw.WriteBytes(b)
}

// Seek seeks to the specified position.
func (fw *FileWriter) Seek(offset int64, whence int) (int64, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	pos, err := fw.writer.Seek(offset, whence)
	if err != nil {
		return 0, err
	}
	fw.position = pos
	return pos, nil
}

// Tell returns the current position.
func (fw *FileWriter) Tell() (int64, error) {
	fw.mu.RLock()
	defer fw.mu.RUnlock()

	return fw.position, nil
}

// Close closes the writer.
func (fw *FileWriter) Close() error {
	if closer, ok := fw.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Flush flushes any buffered data.
func (fw *FileWriter) Flush() error {
	if flusher, ok := fw.writer.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}

// GetByteOrder returns the current byte order.
func (fw *FileWriter) GetByteOrder() ByteOrder {
	fw.mu.RLock()
	defer fw.mu.RUnlock()

	return fw.byteOrder
}

// SetByteOrder sets the byte order.
func (fw *FileWriter) SetByteOrder(bo ByteOrder) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.byteOrder = bo
}

// BufferPool provides reusable byte buffers to reduce allocations.
type BufferPool struct {
	pool *sync.Pool
	size int
}

// NewBufferPool creates a new BufferPool with the specified buffer size.
func NewBufferPool(size int) *BufferPool {
	return &BufferPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, size)
			},
		},
		size: size,
	}
}

// Get retrieves a buffer from the pool.
func (bp *BufferPool) Get() []byte {
	return bp.pool.Get().([]byte)
}

// Put returns a buffer to the pool.
func (bp *BufferPool) Put(b []byte) {
	if cap(b) >= bp.size {
		bp.pool.Put(b[:0]) //nolint:staticcheck // SA6002: using []byte with sync.Pool is intentional for this buffer pool pattern
	}
}

// Position represents a position in a file with metadata.
type Position struct {
	Offset int64
	Tag    uint32
	VR     string
	Length uint32
}

// ExecuteReadHook executes a hook for read operations.
func ExecuteReadHook(reader Reader, bytesRead int, position int64) map[string]interface{} {
	result := make(map[string]interface{})
	result["bytes_read"] = bytesRead
	result["position"] = position
	result["byte_order"] = reader.GetByteOrder().String()
	return result
}

// ExecuteWriteHook executes a hook for write operations.
func ExecuteWriteHook(writer Writer, bytesWritten int, position int64) map[string]interface{} {
	result := make(map[string]interface{})
	result["bytes_written"] = bytesWritten
	result["position"] = position
	result["byte_order"] = writer.GetByteOrder().String()
	return result
}

// ExecuteByteOrderHook executes a hook for byte order changes.
func ExecuteByteOrderHook(oldByteOrder, newByteOrder ByteOrder) map[string]interface{} {
	result := make(map[string]interface{})
	result["old_byte_order"] = oldByteOrder.String()
	result["new_byte_order"] = newByteOrder.String()
	return result
}

// ExecuteSeekHook executes a hook for seek operations.
func ExecuteSeekHook(oldPosition, newPosition int64) map[string]interface{} {
	result := make(map[string]interface{})
	result["old_position"] = oldPosition
	result["new_position"] = newPosition
	result["offset"] = newPosition - oldPosition
	return result
}
