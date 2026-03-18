package fileutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// ByteOrderDetector detects byte order from DICOM files.
type ByteOrderDetector struct {
	reader filebase.Reader
}

// NewByteOrderDetector creates a new byte order detector.
func NewByteOrderDetector(reader filebase.Reader) *ByteOrderDetector {
	return &ByteOrderDetector{reader: reader}
}

// DetectFromTransferSyntax detects byte order from transfer syntax UID.
func DetectFromTransferSyntax(ts uid.UID) (filebase.ByteOrder, error) {
	uidStr := ts.String()
	info := uid.GetUIDInfo(uidStr)
	if info == nil {
		return filebase.LittleEndian, fmt.Errorf("unknown transfer syntax: %s", uidStr)
	}

	// Explicit VR Little Endian
	if uidStr == "1.2.840.10008.1.2.1" {
		return filebase.LittleEndian, nil
	}

	// Explicit VR Big Endian
	if uidStr == "1.2.840.10008.1.2.2" {
		return filebase.BigEndian, nil
	}

	// All others (implicit, compressed) use little-endian
	return filebase.LittleEndian, nil
}

// DetectFromPreamble detects valid DICOM preamble and magic string.
func DetectFromPreamble(reader filebase.Reader) (bool, error) {
	pos, err := reader.Tell()
	if err != nil {
		return false, fmt.Errorf("failed to get position: %w", err)
	}

	if _, err := reader.Seek(128, io.SeekStart); err != nil {
		return false, fmt.Errorf("failed to seek to preamble: %w", err)
	}

	magic := make([]byte, 4)
	if err := reader.ReadBytes(magic); err != nil {
		return false, fmt.Errorf("failed to read magic: %w", err)
	}

	if _, err := reader.Seek(pos, io.SeekStart); err != nil {
		return false, fmt.Errorf("failed to restore position: %w", err)
	}

	return bytes.Equal(magic, []byte("DICM")), nil
}

// PadValue pads value to even length with specified byte.
func PadValue(value []byte, padByte byte) []byte {
	if len(value)%2 == 1 {
		return append(value, padByte)
	}
	return value
}

// PadValueSpace pads value to even length with space character.
func PadValueSpace(value []byte) []byte {
	return PadValue(value, 0x20)
}

// PadValueNull pads value to even length with null character.
func PadValueNull(value []byte) []byte {
	return PadValue(value, 0x00)
}

// UnpadValue removes trailing padding from value.
func UnpadValue(value []byte, padByte byte) []byte {
	for len(value) > 0 && value[len(value)-1] == padByte {
		value = value[:len(value)-1]
	}
	return value
}

// CalculateGroupLength calculates group length for data elements.
func CalculateGroupLength(elements []*filebase.Position, groupNumber uint16) uint32 {
	var length uint32
	for _, elem := range elements {
		elemTag := tag.FromInt(uint32(elem.Tag))
		if elemTag.Group() != groupNumber {
			continue
		}

		// Tag (4 bytes) + VR (2 bytes) + Reserved (2 bytes) + Length (4 bytes) + Value
		length += 4 + 2 + 2 + 4 + uint32(elem.Length)
	}
	return length
}

// AlignToEvenBoundary aligns position to next even byte boundary.
func AlignToEvenBoundary(position int64) int64 {
	if position%2 == 1 {
		return position + 1
	}
	return position
}

// ByteBufferPool manages a pool of reusable byte buffers.
type ByteBufferPool struct {
	buffers chan []byte
	size    int
}

// NewByteBufferPool creates a new byte buffer pool.
func NewByteBufferPool(poolSize int, bufferSize int) *ByteBufferPool {
	return &ByteBufferPool{
		buffers: make(chan []byte, poolSize),
		size:    bufferSize,
	}
}

// Get retrieves or allocates a buffer from the pool.
func (bp *ByteBufferPool) Get() []byte {
	select {
	case buf := <-bp.buffers:
		return buf[:0]
	default:
		return make([]byte, 0, bp.size)
	}
}

// Put returns a buffer to the pool.
func (bp *ByteBufferPool) Put(buf []byte) {
	if cap(buf) < bp.size {
		return
	}
	select {
	case bp.buffers <- buf:
	default:
	}
}

// ReadBytesWithByteOrder reads bytes and applies byte order conversion.
func ReadBytesWithByteOrder(data []byte, byteOrder filebase.ByteOrder) []byte {
	if byteOrder == filebase.BigEndian {
		reversed := make([]byte, len(data))
		for i, b := range data {
			reversed[len(data)-1-i] = b
		}
		return reversed
	}
	return data
}

// WriteBytesWithByteOrder writes bytes in specified byte order.
func WriteBytesWithByteOrder(value []byte, byteOrder filebase.ByteOrder) []byte {
	if byteOrder == filebase.BigEndian {
		reversed := make([]byte, len(value))
		for i, b := range value {
			reversed[len(value)-1-i] = b
		}
		return reversed
	}
	return value
}

// Uint16ToBytes converts uint16 to bytes with specified byte order.
func Uint16ToBytes(value uint16, byteOrder filebase.ByteOrder) []byte {
	buf := make([]byte, 2)
	if byteOrder == filebase.LittleEndian {
		binary.LittleEndian.PutUint16(buf, value)
	} else {
		binary.BigEndian.PutUint16(buf, value)
	}
	return buf
}

// Uint32ToBytes converts uint32 to bytes with specified byte order.
func Uint32ToBytes(value uint32, byteOrder filebase.ByteOrder) []byte {
	buf := make([]byte, 4)
	if byteOrder == filebase.LittleEndian {
		binary.LittleEndian.PutUint32(buf, value)
	} else {
		binary.BigEndian.PutUint32(buf, value)
	}
	return buf
}

// BytesToUint16 converts bytes to uint16 with specified byte order.
func BytesToUint16(data []byte, byteOrder filebase.ByteOrder) uint16 {
	if len(data) < 2 {
		return 0
	}
	if byteOrder == filebase.LittleEndian {
		return binary.LittleEndian.Uint16(data)
	}
	return binary.BigEndian.Uint16(data)
}

// BytesToUint32 converts bytes to uint32 with specified byte order.
func BytesToUint32(data []byte, byteOrder filebase.ByteOrder) uint32 {
	if len(data) < 4 {
		return 0
	}
	if byteOrder == filebase.LittleEndian {
		return binary.LittleEndian.Uint32(data)
	}
	return binary.BigEndian.Uint32(data)
}

// FileMetaCache caches file metadata for quick access.
type FileMetaCache struct {
	mu      sync.RWMutex
	cache   map[string]interface{}
	maxSize int
	hits    int64
	misses  int64
}

// NewFileMetaCache creates a new file metadata cache.
func NewFileMetaCache(maxSize int) *FileMetaCache {
	return &FileMetaCache{
		cache:   make(map[string]interface{}),
		maxSize: maxSize,
	}
}

// Get retrieves a cached value.
func (fmc *FileMetaCache) Get(key string) (interface{}, bool) {
	fmc.mu.RLock()
	defer fmc.mu.RUnlock()

	val, exists := fmc.cache[key]
	if exists {
		fmc.hits++
	} else {
		fmc.misses++
	}
	return val, exists
}

// Set stores a value in the cache.
func (fmc *FileMetaCache) Set(key string, value interface{}) {
	fmc.mu.Lock()
	defer fmc.mu.Unlock()

	if len(fmc.cache) >= fmc.maxSize && fmc.cache[key] == nil {
		for k := range fmc.cache {
			delete(fmc.cache, k)
			break
		}
	}

	fmc.cache[key] = value
}

// Clear clears the cache.
func (fmc *FileMetaCache) Clear() {
	fmc.mu.Lock()
	defer fmc.mu.Unlock()

	fmc.cache = make(map[string]interface{})
	fmc.hits = 0
	fmc.misses = 0
}

// GetStats returns cache statistics.
func (fmc *FileMetaCache) GetStats() (hits, misses int64) {
	fmc.mu.RLock()
	defer fmc.mu.RUnlock()

	return fmc.hits, fmc.misses
}

// TagIndex maps tags to their file positions.
type TagIndex struct {
	mu       sync.RWMutex
	index    map[uint32]*filebase.Position
	elements []*filebase.Position
}

// NewTagIndex creates a new tag index.
func NewTagIndex() *TagIndex {
	return &TagIndex{
		index:    make(map[uint32]*filebase.Position),
		elements: make([]*filebase.Position, 0),
	}
}

// Add adds a position entry to the index.
func (ti *TagIndex) Add(position *filebase.Position) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	ti.index[position.Tag] = position
	ti.elements = append(ti.elements, position)
}

// Get retrieves a position by tag.
func (ti *TagIndex) Get(t tag.Tag) (*filebase.Position, bool) {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	pos, exists := ti.index[uint32(t)]
	return pos, exists
}

// GetAll retrieves all positions.
func (ti *TagIndex) GetAll() []*filebase.Position {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	result := make([]*filebase.Position, len(ti.elements))
	copy(result, ti.elements)
	return result
}

// Contains checks if a tag exists in the index.
func (ti *TagIndex) Contains(t tag.Tag) bool {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	_, exists := ti.index[uint32(t)]
	return exists
}

// Count returns the number of indexed elements.
func (ti *TagIndex) Count() int {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	return len(ti.elements)
}

// Clear clears the index.
func (ti *TagIndex) Clear() {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	ti.index = make(map[uint32]*filebase.Position)
	ti.elements = make([]*filebase.Position, 0)
}

// ReaderWithBoundary limits reading to a maximum byte count.
type ReaderWithBoundary struct {
	reader   filebase.Reader
	boundary int64
	read     int64
	mu       sync.RWMutex
}

// NewReaderWithBoundary creates a reader with a byte boundary limit.
func NewReaderWithBoundary(reader filebase.Reader, boundary int64) *ReaderWithBoundary {
	return &ReaderWithBoundary{
		reader:   reader,
		boundary: boundary,
		read:     0,
	}
}

// ReadBytes reads bytes up to the boundary.
func (rwb *ReaderWithBoundary) ReadBytes(b []byte) error {
	rwb.mu.Lock()
	defer rwb.mu.Unlock()

	if rwb.read+int64(len(b)) > rwb.boundary {
		return fmt.Errorf("would exceed boundary: %d + %d > %d", rwb.read, len(b), rwb.boundary)
	}

	if err := rwb.reader.ReadBytes(b); err != nil {
		return err
	}

	rwb.read += int64(len(b))
	return nil
}

// ReadByte reads a single byte.
func (rwb *ReaderWithBoundary) ReadByte() (byte, error) {
	rwb.mu.Lock()
	defer rwb.mu.Unlock()

	if rwb.read+1 > rwb.boundary {
		return 0, fmt.Errorf("would exceed boundary")
	}

	b, err := rwb.reader.ReadByte()
	if err != nil {
		return 0, err
	}

	rwb.read++
	return b, nil
}

// ReadUint16 reads a uint16 up to the boundary.
func (rwb *ReaderWithBoundary) ReadUint16() (uint16, error) {
	rwb.mu.Lock()
	defer rwb.mu.Unlock()

	if rwb.read+2 > rwb.boundary {
		return 0, fmt.Errorf("would exceed boundary")
	}

	v, err := rwb.reader.ReadUint16()
	if err != nil {
		return 0, err
	}

	rwb.read += 2
	return v, nil
}

// ReadUint32 reads a uint32 up to the boundary.
func (rwb *ReaderWithBoundary) ReadUint32() (uint32, error) {
	rwb.mu.Lock()
	defer rwb.mu.Unlock()

	if rwb.read+4 > rwb.boundary {
		return 0, fmt.Errorf("would exceed boundary")
	}

	v, err := rwb.reader.ReadUint32()
	if err != nil {
		return 0, err
	}

	rwb.read += 4
	return v, nil
}

// Seek seeks within the boundary.
func (rwb *ReaderWithBoundary) Seek(offset int64, whence int) (int64, error) {
	rwb.mu.Lock()
	defer rwb.mu.Unlock()

	pos, err := rwb.reader.Seek(offset, whence)
	if err != nil {
		return 0, err
	}

	if pos < 0 || pos > rwb.boundary {
		return 0, fmt.Errorf("seek position out of boundary")
	}

	rwb.read = pos
	return pos, nil
}

// Tell returns the current position.
func (rwb *ReaderWithBoundary) Tell() (int64, error) {
	rwb.mu.RLock()
	defer rwb.mu.RUnlock()

	return rwb.read, nil
}

// Close closes the underlying reader.
func (rwb *ReaderWithBoundary) Close() error {
	return rwb.reader.Close()
}

// GetByteOrder returns the byte order.
func (rwb *ReaderWithBoundary) GetByteOrder() filebase.ByteOrder {
	return rwb.reader.GetByteOrder()
}

// SetByteOrder sets the byte order.
func (rwb *ReaderWithBoundary) SetByteOrder(bo filebase.ByteOrder) {
	rwb.reader.SetByteOrder(bo)
}

// GetBytesRead returns bytes read so far.
func (rwb *ReaderWithBoundary) GetBytesRead() int64 {
	rwb.mu.RLock()
	defer rwb.mu.RUnlock()

	return rwb.read
}

// GetBytesRemaining returns bytes remaining before boundary.
func (rwb *ReaderWithBoundary) GetBytesRemaining() int64 {
	rwb.mu.RLock()
	defer rwb.mu.RUnlock()

	return rwb.boundary - rwb.read
}

// ExecuteTagIndexHook executes a hook on tag index for processing.
func ExecuteTagIndexHook(index *TagIndex) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	result["count"] = index.Count()
	result["elements"] = index.GetAll()
	return result, nil
}

// ExecuteByteOrderHook executes a hook for byte order detection.
func ExecuteByteOrderHook(ts uid.UID) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	order, err := DetectFromTransferSyntax(ts)
	if err != nil {
		return result, err
	}
	result["byte_order"] = order
	result["is_little_endian"] = (order == filebase.LittleEndian)
	return result, nil
}
