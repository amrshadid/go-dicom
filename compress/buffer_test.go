package compress_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// ============================================================================
// EncapsulatedBuffer Tests
// ============================================================================

func TestNewEncapsulatedBuffer(t *testing.T) {
	// Create test frames
	frame1 := bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})
	frame2 := bytes.NewReader([]byte{0x05, 0x06, 0x07, 0x08, 0x09})

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2}, true)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	if buf == nil {
		t.Fatal("Expected non-nil buffer")
	}

	// Check total length
	// BOT: 8 bytes header + 8 bytes offsets (2 frames * 4 bytes)
	// Frame1: 8 bytes (tag+length) + 4 bytes data = 12 bytes
	// Frame2: 8 bytes (tag+length) + 5 bytes data + 1 byte padding = 14 bytes
	// Total: 16 + 12 + 14 = 42 bytes
	expectedLength := int64(16 + 12 + 14)
	if buf.Length() != expectedLength {
		t.Errorf("Length: got %d, want %d", buf.Length(), expectedLength)
	}
}

func TestEncapsulatedBufferEmptyBOT(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x11, 0x22})

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1}, false)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	// Empty BOT: 8 bytes (tag + length=0)
	// Frame: 8 bytes (tag+length) + 2 bytes data = 10 bytes
	// Total: 18 bytes
	expectedLength := int64(18)
	if buf.Length() != expectedLength {
		t.Errorf("Length: got %d, want %d", buf.Length(), expectedLength)
	}
}

func TestEncapsulatedBufferRead(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0xAA, 0xBB})
	frame2 := bytes.NewReader([]byte{0xCC, 0xDD, 0xEE})

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2}, false)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	// Read all data
	data := make([]byte, buf.Length())
	n, err := buf.Read(data)
	if err != nil && err != io.EOF {
		t.Fatalf("Read error: %v", err)
	}

	if int64(n) != buf.Length() {
		t.Errorf("Read %d bytes, want %d", n, buf.Length())
	}

	// Verify BOT (empty, 8 bytes)
	botTag := binary.LittleEndian.Uint32(data[0:4])
	if botTag != compress.ItemTag {
		t.Errorf("BOT tag: got 0x%08X, want 0x%08X", botTag, compress.ItemTag)
	}

	botLength := binary.LittleEndian.Uint32(data[4:8])
	if botLength != 0 {
		t.Errorf("BOT length: got %d, want 0", botLength)
	}

	// Verify Frame 1 (offset 8)
	frame1Tag := binary.LittleEndian.Uint32(data[8:12])
	if frame1Tag != compress.ItemTag {
		t.Errorf("Frame1 tag: got 0x%08X, want 0x%08X", frame1Tag, compress.ItemTag)
	}

	frame1Length := binary.LittleEndian.Uint32(data[12:16])
	if frame1Length != 2 {
		t.Errorf("Frame1 length: got %d, want 2", frame1Length)
	}

	if data[16] != 0xAA || data[17] != 0xBB {
		t.Errorf("Frame1 data: got [0x%02X, 0x%02X], want [0xAA, 0xBB]", data[16], data[17])
	}

	// Verify Frame 2 (offset 18)
	frame2Tag := binary.LittleEndian.Uint32(data[18:22])
	if frame2Tag != compress.ItemTag {
		t.Errorf("Frame2 tag: got 0x%08X, want 0x%08X", frame2Tag, compress.ItemTag)
	}

	frame2Length := binary.LittleEndian.Uint32(data[22:26])
	if frame2Length != 3 {
		t.Errorf("Frame2 length: got %d, want 3", frame2Length)
	}

	if data[26] != 0xCC || data[27] != 0xDD || data[28] != 0xEE {
		t.Errorf("Frame2 data: got [0x%02X, 0x%02X, 0x%02X], want [0xCC, 0xDD, 0xEE]",
			data[26], data[27], data[28])
	}

	// Frame 2 should have padding
	if data[29] != 0x00 {
		t.Errorf("Frame2 padding: got 0x%02X, want 0x00", data[29])
	}
}

func TestEncapsulatedBufferSeek(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1}, true)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	// Test SeekStart
	pos, err := buf.Seek(10, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek error: %v", err)
	}
	if pos != 10 {
		t.Errorf("Seek position: got %d, want 10", pos)
	}

	// Test SeekCurrent
	pos, err = buf.Seek(5, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek error: %v", err)
	}
	if pos != 15 {
		t.Errorf("Seek position: got %d, want 15", pos)
	}

	// Test SeekEnd
	pos, err = buf.Seek(-5, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek error: %v", err)
	}
	expectedPos := buf.Length() - 5
	if pos != expectedPos {
		t.Errorf("Seek position: got %d, want %d", pos, expectedPos)
	}

	// Test negative seek from start (should error)
	_, err = buf.Seek(-1, io.SeekStart)
	if err == nil {
		t.Error("Expected error for negative seek from start")
	}
}

func TestEncapsulatedBufferOffsets(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x01, 0x02})             // 2 bytes
	frame2 := bytes.NewReader([]byte{0x03, 0x04, 0x05})       // 3 bytes (odd, needs padding)
	frame3 := bytes.NewReader([]byte{0x06, 0x07, 0x08, 0x09}) // 4 bytes

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2, frame3}, true)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	offsets := buf.Offsets()
	if len(offsets) != 3 {
		t.Fatalf("Expected 3 offsets, got %d", len(offsets))
	}

	// Frame 1 starts at offset 0
	if offsets[0] != 0 {
		t.Errorf("Offset 0: got %d, want 0", offsets[0])
	}

	// Frame 2 starts after Frame 1: 8 (tag+len) + 2 (data) = 10
	if offsets[1] != 10 {
		t.Errorf("Offset 1: got %d, want 10", offsets[1])
	}

	// Frame 3 starts after Frame 2: 10 + 8 (tag+len) + 3 (data) + 1 (padding) = 22
	if offsets[2] != 22 {
		t.Errorf("Offset 2: got %d, want 22", offsets[2])
	}
}

func TestEncapsulatedBufferLengths(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x01, 0x02})       // 2 bytes
	frame2 := bytes.NewReader([]byte{0x03, 0x04, 0x05}) // 3 bytes (odd)

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2}, false)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	lengths := buf.Lengths()
	if len(lengths) != 2 {
		t.Fatalf("Expected 2 lengths, got %d", len(lengths))
	}

	// Frame 1: 8 (tag+len) + 2 (data) = 10
	if lengths[0] != 10 {
		t.Errorf("Length 0: got %d, want 10", lengths[0])
	}

	// Frame 2: 8 (tag+len) + 3 (data) + 1 (padding) = 12
	if lengths[1] != 12 {
		t.Errorf("Length 1: got %d, want 12", lengths[1])
	}
}

func TestEncapsulatedBufferExtendedOffsets(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})
	frame2 := bytes.NewReader([]byte{0x05, 0x06})

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2}, true)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	extOffsets := buf.ExtendedOffsets()
	expectedLen := 2 * 8 // 2 frames * 8 bytes per uint64
	if len(extOffsets) != expectedLen {
		t.Fatalf("ExtendedOffsets length: got %d, want %d", len(extOffsets), expectedLen)
	}

	// Verify offsets are 64-bit
	offset0 := binary.LittleEndian.Uint64(extOffsets[0:8])
	if offset0 != 0 {
		t.Errorf("Extended offset 0: got %d, want 0", offset0)
	}

	// Frame 2 offset: 8 (tag+len) + 4 (data) = 12
	offset1 := binary.LittleEndian.Uint64(extOffsets[8:16])
	if offset1 != 12 {
		t.Errorf("Extended offset 1: got %d, want 12", offset1)
	}
}

func TestEncapsulatedBufferExtendedLengths(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})
	frame2 := bytes.NewReader([]byte{0x05, 0x06, 0x07}) // Odd length

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2}, true)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	extLengths := buf.ExtendedLengths()
	expectedLen := 2 * 8 // 2 frames * 8 bytes per uint64
	if len(extLengths) != expectedLen {
		t.Fatalf("ExtendedLengths length: got %d, want %d", len(extLengths), expectedLen)
	}

	// Frame 1 length: 4 bytes (excluding 8-byte header)
	length0 := binary.LittleEndian.Uint64(extLengths[0:8])
	if length0 != 4 {
		t.Errorf("Extended length 0: got %d, want 4", length0)
	}

	// Frame 2 length: 3 bytes data (excluding 8-byte header, NOT including padding)
	length1 := binary.LittleEndian.Uint64(extLengths[8:16])
	if length1 != 3 {
		t.Errorf("Extended length 1: got %d, want 3", length1)
	}
}

func TestEncapsulatedBufferReadInChunks(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x11, 0x22, 0x33, 0x44})

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1}, false)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	// Read in small chunks
	chunk1 := make([]byte, 5)
	n1, err := buf.Read(chunk1)
	if err != nil {
		t.Fatalf("Read chunk 1 error: %v", err)
	}
	if n1 != 5 {
		t.Errorf("Chunk 1: read %d bytes, want 5", n1)
	}

	chunk2 := make([]byte, 5)
	n2, err := buf.Read(chunk2)
	if err != nil {
		t.Fatalf("Read chunk 2 error: %v", err)
	}
	if n2 != 5 {
		t.Errorf("Chunk 2: read %d bytes, want 5", n2)
	}

	// Read remaining
	chunk3 := make([]byte, 10)
	n3, err := buf.Read(chunk3)
	if err != nil && err != io.EOF {
		t.Fatalf("Read chunk 3 error: %v", err)
	}

	expectedRemaining := int(buf.Length()) - n1 - n2
	if n3 != expectedRemaining {
		t.Errorf("Chunk 3: read %d bytes, want %d", n3, expectedRemaining)
	}
}

func TestEncapsulatedBufferEmptyBuffers(t *testing.T) {
	_, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{}, true)
	if err == nil {
		t.Error("Expected error for empty buffers list")
	}
}

func TestEncapsulatedBufferWithBOT(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0xFF, 0xD8}) // JPEG SOI marker
	frame2 := bytes.NewReader([]byte{0xFF, 0xD9}) // JPEG EOI marker

	buf, err := compress.NewEncapsulatedBuffer([]io.ReadSeeker{frame1, frame2}, true)
	if err != nil {
		t.Fatalf("NewEncapsulatedBuffer error: %v", err)
	}

	// Read BOT section
	botSection := make([]byte, 16) // 8 bytes header + 8 bytes offsets
	n, err := buf.Read(botSection)
	if err != nil {
		t.Fatalf("Read BOT error: %v", err)
	}
	if n != 16 {
		t.Errorf("Read %d bytes, want 16", n)
	}

	// Verify BOT tag
	tag := binary.LittleEndian.Uint32(botSection[0:4])
	if tag != compress.ItemTag {
		t.Errorf("BOT tag: got 0x%08X, want 0x%08X", tag, compress.ItemTag)
	}

	// Verify BOT length (2 frames * 4 bytes = 8)
	length := binary.LittleEndian.Uint32(botSection[4:8])
	if length != 8 {
		t.Errorf("BOT length: got %d, want 8", length)
	}

	// Verify first offset is 0
	offset0 := binary.LittleEndian.Uint32(botSection[8:12])
	if offset0 != 0 {
		t.Errorf("Offset 0: got %d, want 0", offset0)
	}

	// Verify second offset (8 bytes header + 2 bytes data = 10)
	offset1 := binary.LittleEndian.Uint32(botSection[12:16])
	if offset1 != 10 {
		t.Errorf("Offset 1: got %d, want 10", offset1)
	}
}

// ============================================================================
// Convenience Function Tests
// ============================================================================

func TestEncapsulateBuffer(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})
	frame2 := bytes.NewReader([]byte{0x05, 0x06})

	// Test with BOT
	buf, err := compress.EncapsulateBuffer([]io.ReadSeeker{frame1, frame2}, true)
	if err != nil {
		t.Fatalf("EncapsulateBuffer error: %v", err)
	}

	if buf == nil {
		t.Fatal("Expected non-nil buffer")
	}

	// Verify it is an EncapsulatedBuffer with correct properties
	if buf.Length() <= 0 {
		t.Error("Buffer length should be > 0")
	}

	// Read and verify BOT is present
	data := make([]byte, 16)
	n, err := buf.Read(data)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	// BOT should be 16 bytes (8 header + 8 offsets for 2 frames)
	if n < 16 {
		t.Errorf("Expected at least 16 bytes for BOT, got %d", n)
	}

	// Verify BOT tag
	tag := binary.LittleEndian.Uint32(data[0:4])
	if tag != compress.ItemTag {
		t.Errorf("BOT tag: got 0x%08X, want 0x%08X", tag, compress.ItemTag)
	}

	// Verify BOT length is not 0 (since useBOT=true)
	botLen := binary.LittleEndian.Uint32(data[4:8])
	if botLen == 0 {
		t.Error("Expected non-empty BOT, but length is 0")
	}
}

func TestEncapsulateBufferWithoutBOT(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0xFF, 0xD8})

	buf, err := compress.EncapsulateBuffer([]io.ReadSeeker{frame1}, false)
	if err != nil {
		t.Fatalf("EncapsulateBuffer error: %v", err)
	}

	// Read BOT section
	data := make([]byte, 8)
	n, err := buf.Read(data)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	if n != 8 {
		t.Errorf("Expected 8 bytes for empty BOT, got %d", n)
	}

	// Verify empty BOT (length = 0)
	botLen := binary.LittleEndian.Uint32(data[4:8])
	if botLen != 0 {
		t.Errorf("Expected empty BOT (length=0), got %d", botLen)
	}
}

func TestEncapsulateExtendedBuffer(t *testing.T) {
	frame1 := bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04})
	frame2 := bytes.NewReader([]byte{0x05, 0x06, 0x07})

	buf, extOffsets, extLengths, err := compress.EncapsulateExtendedBuffer(
		[]io.ReadSeeker{frame1, frame2},
	)
	if err != nil {
		t.Fatalf("EncapsulateExtendedBuffer error: %v", err)
	}

	if buf == nil {
		t.Fatal("Expected non-nil buffer")
	}

	// Verify extended offsets
	if len(extOffsets) != 16 { // 2 frames * 8 bytes
		t.Errorf("ExtendedOffsets length: got %d, want 16", len(extOffsets))
	}

	// Verify extended lengths
	if len(extLengths) != 16 { // 2 frames * 8 bytes
		t.Errorf("ExtendedLengths length: got %d, want 16", len(extLengths))
	}

	// Verify first offset is 0
	offset0 := binary.LittleEndian.Uint64(extOffsets[0:8])
	if offset0 != 0 {
		t.Errorf("First offset: got %d, want 0", offset0)
	}

	// Verify first length is 4 (frame1 data length)
	length0 := binary.LittleEndian.Uint64(extLengths[0:8])
	if length0 != 4 {
		t.Errorf("First length: got %d, want 4", length0)
	}

	// Verify second length is 3 (frame2 data length, no padding)
	length1 := binary.LittleEndian.Uint64(extLengths[8:16])
	if length1 != 3 {
		t.Errorf("Second length: got %d, want 3", length1)
	}

	// Verify BOT is empty (since Extended Offset Table is used)
	data := make([]byte, 8)
	buf.Seek(0, io.SeekStart)
	buf.Read(data)

	botLen := binary.LittleEndian.Uint32(data[4:8])
	if botLen != 0 {
		t.Errorf("Expected empty BOT when using Extended Offset Table, got length %d", botLen)
	}
}

func TestEncapsulateExtendedBufferMultipleFrames(t *testing.T) {
	frames := make([]io.ReadSeeker, 10)
	for i := 0; i < 10; i++ {
		data := make([]byte, 100+i)
		for j := range data {
			data[j] = byte(i)
		}
		frames[i] = bytes.NewReader(data)
	}

	buf, extOffsets, extLengths, err := compress.EncapsulateExtendedBuffer(frames)
	if err != nil {
		t.Fatalf("EncapsulateExtendedBuffer error: %v", err)
	}

	// Verify we have 10 offsets and lengths
	if len(extOffsets) != 80 { // 10 * 8
		t.Errorf("ExtendedOffsets length: got %d, want 80", len(extOffsets))
	}

	if len(extLengths) != 80 { // 10 * 8
		t.Errorf("ExtendedLengths length: got %d, want 80", len(extLengths))
	}

	// Verify offsets are sequential
	expectedOffset := uint64(0)
	for i := 0; i < 10; i++ {
		offset := binary.LittleEndian.Uint64(extOffsets[i*8 : (i+1)*8])
		if offset != expectedOffset {
			t.Errorf("Offset %d: got %d, want %d", i, offset, expectedOffset)
		}

		// Each frame: 8 bytes header + (100+i) bytes data + padding if odd
		dataSize := uint64(100 + i)
		frameSize := uint64(8) + dataSize
		if dataSize%2 != 0 {
			frameSize++ // Add padding byte for odd-length data
		}
		expectedOffset += frameSize
	}

	// Verify lengths match frame sizes
	for i := 0; i < 10; i++ {
		length := binary.LittleEndian.Uint64(extLengths[i*8 : (i+1)*8])
		expectedLength := uint64(100 + i)
		if length != expectedLength {
			t.Errorf("Length %d: got %d, want %d", i, length, expectedLength)
		}
	}

	// Verify buffer is readable
	totalRead := 0
	chunk := make([]byte, 256)
	for {
		n, err := buf.Read(chunk)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}

	if int64(totalRead) != buf.Length() {
		t.Errorf("Total read: %d, expected %d", totalRead, buf.Length())
	}
}
