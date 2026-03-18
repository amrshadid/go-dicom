package compress

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Buffered Item

// bufferedItem represents a single encapsulated frame buffer.
//
// This is used internally by EncapsulatedBuffer to manage individual frames
// that will be lazily read and encapsulated.
type bufferedItem struct {
	buffer  io.ReadSeeker
	length  uint32 // Total length including item tag (8 bytes) + data + padding
	dataLen uint32 // Actual data length (without tag/padding)
	padding bool   // Whether padding byte is needed
	itemTag []byte // Pre-computed item tag and length (8 bytes)
}

// newBufferedItem creates a new buffered item from a frame buffer.
func newBufferedItem(buffer io.ReadSeeker) (*bufferedItem, error) {
	currentPos, err := buffer.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("failed to get current position: %w", err)
	}

	endPos, err := buffer.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to end: %w", err)
	}

	dataLen := uint32(endPos)

	if _, err := buffer.Seek(currentPos, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to restore position: %w", err)
	}

	if dataLen > 0xFFFFFFFE {
		return nil, fmt.Errorf("buffer size %d exceeds maximum of 4294967294 bytes", dataLen)
	}

	padding := (dataLen % 2) != 0

	totalLen := uint32(8) + dataLen
	if padding {
		totalLen++
	}

	itemTag := make([]byte, 8)
	binary.LittleEndian.PutUint32(itemTag[0:4], ItemTag)
	binary.LittleEndian.PutUint32(itemTag[4:8], dataLen)

	return &bufferedItem{
		buffer:  buffer,
		length:  totalLen,
		dataLen: dataLen,
		padding: padding,
		itemTag: itemTag,
	}, nil
}

// read reads data from the encapsulated item starting at the given position.
//
// The position is relative to the start of the item tag (position 0).
// This handles reading from the item tag, data, or padding byte.
func (b *bufferedItem) read(start int64, size int) ([]byte, error) {
	if start < 0 || start >= int64(b.length) {
		return []byte{}, nil
	}

	result := make([]byte, 0, size)
	remaining := size
	offset := start

	for remaining > 0 && offset < int64(b.length) {
		if offset < 8 {
			toRead := min(remaining, 8-int(offset))
			result = append(result, b.itemTag[offset:offset+int64(toRead)]...)
			offset += int64(toRead)
			remaining -= toRead
		} else if offset < int64(8+b.dataLen) {
			dataOffset := offset - 8
			if _, err := b.buffer.Seek(dataOffset, io.SeekStart); err != nil {
				return nil, fmt.Errorf("failed to seek in buffer: %w", err)
			}

			toRead := min(remaining, int(b.dataLen)-int(dataOffset))
			chunk := make([]byte, toRead)
			n, err := b.buffer.Read(chunk)
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("failed to read from buffer: %w", err)
			}

			result = append(result, chunk[:n]...)
			offset += int64(n)
			remaining -= n
		} else if b.padding && offset == int64(b.length-1) {
			result = append(result, 0x00)
			offset++
			remaining--
		} else {
			break
		}
	}

	return result, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Encapsulated Buffer

// EncapsulatedBuffer manages lazy encapsulation of multiple frame buffers.
//
// This class allows encapsulating pixel data frames without loading all frames
// into memory at once. It implements io.ReadSeeker and can be used as a source
// for writing encapsulated pixel data to DICOM files.
//
// The buffer automatically:
//   - Adds Basic Offset Table (optionally with offsets)
//   - Wraps each frame with DICOM item tags
//   - Adds padding bytes where needed
//   - Supports seeking and reading like a regular buffer
type EncapsulatedBuffer struct {
	items       []*bufferedItem
	useBOT      bool
	offset      int64
	itemOffsets []int64 // Offsets to start of each item (including BOT)
	totalLength int64   // Total length of encapsulated data
	botData     []byte  // Pre-computed Basic Offset Table
}

// NewEncapsulatedBuffer creates a new encapsulated buffer from frame buffers.
//
// Parameters:
//   - buffers: List of io.ReadSeeker for each frame (one frame per buffer)
//   - useBOT: If true, include offsets in Basic Offset Table; if false, BOT is empty
//
// Returns:
//   - *EncapsulatedBuffer: The encapsulated buffer ready for reading
//   - error: Any error during initialization
//
// Example:
//
//	frame1 := bytes.NewReader(compressedFrame1)
//	frame2 := bytes.NewReader(compressedFrame2)
//	encapBuf, err := NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2}, true)
func NewEncapsulatedBuffer(buffers []io.ReadSeeker, useBOT bool) (*EncapsulatedBuffer, error) {
	if len(buffers) == 0 {
		return nil, fmt.Errorf("at least one buffer is required")
	}

	items := make([]*bufferedItem, len(buffers))
	for i, buf := range buffers {
		item, err := newBufferedItem(buf)
		if err != nil {
			return nil, fmt.Errorf("failed to create buffered item %d: %w", i, err)
		}
		items[i] = item
	}

	eb := &EncapsulatedBuffer{
		items:  items,
		useBOT: useBOT,
		offset: 0,
	}

	if err := eb.buildBOT(); err != nil {
		return nil, fmt.Errorf("failed to build Basic Offset Table: %w", err)
	}

	eb.itemOffsets = []int64{0}
	currentOffset := int64(len(eb.botData))

	for _, item := range items {
		eb.itemOffsets = append(eb.itemOffsets, currentOffset)
		currentOffset += int64(item.length)
	}
	eb.itemOffsets = append(eb.itemOffsets, currentOffset)

	eb.totalLength = currentOffset

	return eb, nil
}

// buildBOT builds the Basic Offset Table.
func (eb *EncapsulatedBuffer) buildBOT() error {
	if !eb.useBOT || len(eb.items) == 0 {
		eb.botData = make([]byte, 8)
		binary.LittleEndian.PutUint32(eb.botData[0:4], ItemTag)
		binary.LittleEndian.PutUint32(eb.botData[4:8], 0)
		return nil
	}

	offsets := make([]uint32, len(eb.items))
	currentOffset := uint32(0)

	for i, item := range eb.items {
		offsets[i] = currentOffset
		currentOffset += item.length
	}

	botLength := uint32(len(offsets) * 4)
	eb.botData = make([]byte, 8+botLength)

	binary.LittleEndian.PutUint32(eb.botData[0:4], ItemTag)
	binary.LittleEndian.PutUint32(eb.botData[4:8], botLength)

	for i, offset := range offsets {
		binary.LittleEndian.PutUint32(eb.botData[8+i*4:8+(i+1)*4], offset)
	}

	return nil
}

// Read reads up to len(p) bytes from the encapsulated buffer.
//
// Implements io.Reader interface.
func (eb *EncapsulatedBuffer) Read(p []byte) (n int, err error) {
	if eb.offset >= eb.totalLength {
		return 0, io.EOF
	}

	size := len(p)
	if int64(size) > eb.totalLength-eb.offset {
		size = int(eb.totalLength - eb.offset)
	}

	result := make([]byte, 0, size)
	remaining := size

	for remaining > 0 && eb.offset < eb.totalLength {
		itemIdx := -1
		for i := 0; i < len(eb.itemOffsets)-1; i++ {
			if eb.itemOffsets[i] <= eb.offset && eb.offset < eb.itemOffsets[i+1] {
				itemIdx = i
				break
			}
		}

		if itemIdx == -1 {
			break
		}

		var chunk []byte
		if itemIdx == 0 {
			start := eb.offset
			toRead := min(remaining, len(eb.botData)-int(start))
			chunk = eb.botData[start : start+int64(toRead)]
		} else {
			item := eb.items[itemIdx-1]
			itemStart := eb.itemOffsets[itemIdx]
			relativeOffset := eb.offset - itemStart

			chunk, err = item.read(relativeOffset, remaining)
			if err != nil {
				return len(result), err
			}
		}

		if len(chunk) == 0 {
			break
		}

		result = append(result, chunk...)
		eb.offset += int64(len(chunk))
		remaining -= len(chunk)
	}

	copy(p, result)
	return len(result), nil
}

// Seek sets the offset for the next Read operation.
//
// Implements io.Seeker interface.
func (eb *EncapsulatedBuffer) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64

	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return 0, fmt.Errorf("negative seek offset %d", offset)
		}
		newOffset = offset
	case io.SeekCurrent:
		newOffset = eb.offset + offset
		if newOffset < 0 {
			newOffset = 0
		}
	case io.SeekEnd:
		newOffset = eb.totalLength + offset
		if newOffset < 0 {
			newOffset = 0
		}
	default:
		return 0, fmt.Errorf("invalid whence value %d", whence)
	}

	eb.offset = newOffset
	return eb.offset, nil
}

// Length returns the total length of the encapsulated data.
func (eb *EncapsulatedBuffer) Length() int64 {
	return eb.totalLength
}

// Offsets returns the frame offsets (starting at 0 for the first frame).
func (eb *EncapsulatedBuffer) Offsets() []uint32 {
	offsets := make([]uint32, len(eb.items))
	currentOffset := uint32(0)

	for i, item := range eb.items {
		offsets[i] = currentOffset
		currentOffset += item.length
	}

	return offsets
}

// Lengths returns the lengths of each encapsulated frame.
func (eb *EncapsulatedBuffer) Lengths() []uint32 {
	lengths := make([]uint32, len(eb.items))
	for i, item := range eb.items {
		lengths[i] = item.length
	}
	return lengths
}

// ExtendedOffsets returns encoded Extended Offset Table data (64-bit offsets).
//
// Returns the bytes that should be stored in DICOM tag (7FE0,0001).
func (eb *EncapsulatedBuffer) ExtendedOffsets() []byte {
	offsets := make([]byte, len(eb.items)*8)
	currentOffset := uint64(0)

	for i, item := range eb.items {
		binary.LittleEndian.PutUint64(offsets[i*8:(i+1)*8], currentOffset)
		currentOffset += uint64(item.length)
	}

	return offsets
}

// ExtendedLengths returns encoded Extended Offset Table Lengths data (64-bit lengths).
//
// Returns the bytes that should be stored in DICOM tag (7FE0,0002).
// Note: This excludes the item tag and item length (8 bytes) AND excludes padding.
func (eb *EncapsulatedBuffer) ExtendedLengths() []byte {
	lengths := make([]byte, len(eb.items)*8)

	for i, item := range eb.items {
		// Use dataLen which is the actual data length (no tag, no padding)
		frameLength := uint64(item.dataLen)
		binary.LittleEndian.PutUint64(lengths[i*8:(i+1)*8], frameLength)
	}

	return lengths
}

// Convenience Functions

// EncapsulateBuffer creates an EncapsulatedBuffer from frame buffers.
// This is a convenience function that wraps NewEncapsulatedBuffer.
// See package documentation for usage examples.
func EncapsulateBuffer(buffers []io.ReadSeeker, useBOT bool) (*EncapsulatedBuffer, error) {
	return NewEncapsulatedBuffer(buffers, useBOT)
}

// EncapsulateExtendedBuffer creates an EncapsulatedBuffer with Extended Offset Table data.
// For large datasets (>4GB), returns both encapsulated buffer and extended offset table data.
// See package documentation for usage examples.
func EncapsulateExtendedBuffer(buffers []io.ReadSeeker) (*EncapsulatedBuffer, []byte, []byte, error) {
	// Create EncapsulatedBuffer with empty BOT (Extended table is used instead)
	eb, err := NewEncapsulatedBuffer(buffers, false)
	if err != nil {
		return nil, nil, nil, err
	}

	// Get Extended Offset Table data
	extOffsets := eb.ExtendedOffsets()
	extLengths := eb.ExtendedLengths()

	return eb, extOffsets, extLengths, nil
}
