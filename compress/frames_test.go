package compress_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// ============================================================================
// Frame Generation Tests
// ============================================================================

// createMockEncapsulatedData creates test encapsulated pixel data
func createMockEncapsulatedData(frames [][]byte, withBOT bool) []byte {
	buf := new(bytes.Buffer)

	// Write Basic Offset Table item tag (FFFE,E000)
	buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})

	if withBOT && len(frames) > 0 {
		// Calculate offsets
		offsets := make([]uint32, len(frames))
		currentOffset := uint32(0)
		for i := range frames {
			offsets[i] = currentOffset
			// Each frame: 8 bytes (tag+length) + data length
			currentOffset += 8 + uint32(len(frames[i]))
		}

		// Write BOT length
		botLength := uint32(len(offsets) * 4)
		lengthBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(lengthBytes, botLength)
		buf.Write(lengthBytes)

		// Write offsets
		for _, offset := range offsets {
			offsetBytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(offsetBytes, offset)
			buf.Write(offsetBytes)
		}
	} else {
		// Empty BOT (length = 0)
		buf.Write([]byte{0x00, 0x00, 0x00, 0x00})
	}

	// Write frame fragments
	for _, frame := range frames {
		// Item tag (FFFE,E000)
		buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
		// Item length
		lengthBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(lengthBytes, uint32(len(frame)))
		buf.Write(lengthBytes)
		// Item data
		buf.Write(frame)
	}

	return buf.Bytes()
}

func TestGenerateFrames(t *testing.T) {
	// Create test data with 3 frames
	frame1 := []byte{0x01, 0x02, 0x03}
	frame2 := []byte{0x04, 0x05, 0x06, 0x07}
	frame3 := []byte{0x08, 0x09}

	testData := createMockEncapsulatedData([][]byte{frame1, frame2, frame3}, true)
	buffer := bytes.NewReader(testData)

	framesChan, errorsChan := compress.GenerateFrames(buffer, 3, "<")

	// Collect frames
	var frames [][]byte
	for frame := range framesChan {
		frames = append(frames, frame)
	}

	if err := <-errorsChan; err != nil {
		t.Fatalf("GenerateFrames error: %v", err)
	}

	// Verify we got 3 frames
	if len(frames) != 3 {
		t.Fatalf("Expected 3 frames, got %d", len(frames))
	}

	// Verify frame contents
	if !bytes.Equal(frames[0], frame1) {
		t.Errorf("Frame 0 mismatch: got %v, want %v", frames[0], frame1)
	}
	if !bytes.Equal(frames[1], frame2) {
		t.Errorf("Frame 1 mismatch: got %v, want %v", frames[1], frame2)
	}
	if !bytes.Equal(frames[2], frame3) {
		t.Errorf("Frame 2 mismatch: got %v, want %v", frames[2], frame3)
	}
}

func TestGenerateFragmentedFrames(t *testing.T) {
	// Create test data
	frame1 := []byte{0x01, 0x02, 0x03}
	frame2 := []byte{0x04, 0x05}

	testData := createMockEncapsulatedData([][]byte{frame1, frame2}, true)
	buffer := bytes.NewReader(testData)

	framesChan, errorsChan := compress.GenerateFragmentedFrames(buffer, 2, "<")

	// Collect frames
	var frames []*compress.FrameInfo
	for frame := range framesChan {
		frames = append(frames, frame)
	}

	if err := <-errorsChan; err != nil {
		t.Fatalf("GenerateFragmentedFrames error: %v", err)
	}

	// Verify we got 2 frames
	if len(frames) != 2 {
		t.Fatalf("Expected 2 frames, got %d", len(frames))
	}

	// Verify frame info
	if frames[0].Index != 0 {
		t.Errorf("Frame 0 index mismatch: got %d, want 0", frames[0].Index)
	}
	if frames[0].FragmentCount != 1 {
		t.Errorf("Frame 0 fragment count mismatch: got %d, want 1", frames[0].FragmentCount)
	}

	// Verify frame data
	joinedFrame0 := bytes.Join(frames[0].Fragments, nil)
	if !bytes.Equal(joinedFrame0, frame1) {
		t.Errorf("Frame 0 data mismatch: got %v, want %v", joinedFrame0, frame1)
	}
}

func TestGetFrame(t *testing.T) {
	// Create test data with 4 frames
	frame1 := []byte{0x11, 0x12}
	frame2 := []byte{0x21, 0x22, 0x23}
	frame3 := []byte{0x31}
	frame4 := []byte{0x41, 0x42, 0x43, 0x44}

	testData := createMockEncapsulatedData([][]byte{frame1, frame2, frame3, frame4}, true)

	tests := []struct {
		name          string
		index         int
		expectedFrame []byte
	}{
		{"First frame", 0, frame1},
		{"Second frame", 1, frame2},
		{"Third frame", 2, frame3},
		{"Fourth frame", 3, frame4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buffer := bytes.NewReader(testData)
			frame, err := compress.GetFrame(buffer, tt.index, 4, "<")
			if err != nil {
				t.Fatalf("GetFrame error: %v", err)
			}

			if !bytes.Equal(frame, tt.expectedFrame) {
				t.Errorf("Frame mismatch: got %v, want %v", frame, tt.expectedFrame)
			}
		})
	}
}

func TestGetFrameOutOfBounds(t *testing.T) {
	frame1 := []byte{0x01, 0x02}
	testData := createMockEncapsulatedData([][]byte{frame1}, true)
	buffer := bytes.NewReader(testData)

	_, err := compress.GetFrame(buffer, 5, 1, "<")
	if err == nil {
		t.Error("Expected error for out of bounds frame index")
	}
}

func TestGetFrameNegativeIndex(t *testing.T) {
	frame1 := []byte{0x01, 0x02}
	testData := createMockEncapsulatedData([][]byte{frame1}, true)
	buffer := bytes.NewReader(testData)

	_, err := compress.GetFrame(buffer, -1, 1, "<")
	if err == nil {
		t.Error("Expected error for negative frame index")
	}
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestGenerateFramesSingleFragment(t *testing.T) {
	// Single fragment = single frame
	frame1 := []byte{0xFF, 0xD8, 0xFF, 0xD9} // JPEG-like data
	testData := createMockEncapsulatedData([][]byte{frame1}, false)
	buffer := bytes.NewReader(testData)

	framesChan, errorsChan := compress.GenerateFrames(buffer, 1, "<")

	frames := [][]byte{}
	for frame := range framesChan {
		frames = append(frames, frame)
	}

	if err := <-errorsChan; err != nil {
		t.Fatalf("GenerateFrames error: %v", err)
	}

	if len(frames) != 1 {
		t.Fatalf("Expected 1 frame, got %d", len(frames))
	}

	if !bytes.Equal(frames[0], frame1) {
		t.Errorf("Frame mismatch: got %v, want %v", frames[0], frame1)
	}
}

func TestGenerateFramesEmptyBOT(t *testing.T) {
	// Multiple frames with empty BOT (1:1 fragment-to-frame ratio)
	frame1 := []byte{0x01, 0x02}
	frame2 := []byte{0x03, 0x04}
	testData := createMockEncapsulatedData([][]byte{frame1, frame2}, false)
	buffer := bytes.NewReader(testData)

	framesChan, errorsChan := compress.GenerateFrames(buffer, 2, "<")

	frames := [][]byte{}
	for frame := range framesChan {
		frames = append(frames, frame)
	}

	if err := <-errorsChan; err != nil {
		t.Fatalf("GenerateFrames error: %v", err)
	}

	if len(frames) != 2 {
		t.Fatalf("Expected 2 frames, got %d", len(frames))
	}
}

func TestGetFrameSingleFrame(t *testing.T) {
	frame1 := []byte{0xAA, 0xBB, 0xCC}
	testData := createMockEncapsulatedData([][]byte{frame1}, true)
	buffer := bytes.NewReader(testData)

	frame, err := compress.GetFrame(buffer, 0, 1, "<")
	if err != nil {
		t.Fatalf("GetFrame error: %v", err)
	}

	if !bytes.Equal(frame, frame1) {
		t.Errorf("Frame mismatch: got %v, want %v", frame, frame1)
	}

	// Try to get frame 1 (should fail)
	buffer2 := bytes.NewReader(testData)
	_, err = compress.GetFrame(buffer2, 1, 1, "<")
	if err == nil {
		t.Error("Expected error when requesting frame 1 from single-frame data")
	}
}

// ============================================================================
// Performance Tests
// ============================================================================

func BenchmarkGenerateFrames(b *testing.B) {
	// Create test data with 10 frames
	frames := make([][]byte, 10)
	for i := 0; i < 10; i++ {
		frames[i] = make([]byte, 1000)
		for j := range frames[i] {
			frames[i][j] = byte(i)
		}
	}

	testData := createMockEncapsulatedData(frames, true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer := bytes.NewReader(testData)
		framesChan, errorsChan := compress.GenerateFrames(buffer, 10, "<")

		for range framesChan {
			// Just consume frames
		}
		<-errorsChan
	}
}

func BenchmarkGetFrame(b *testing.B) {
	// Create test data with 10 frames
	frames := make([][]byte, 10)
	for i := 0; i < 10; i++ {
		frames[i] = make([]byte, 1000)
		for j := range frames[i] {
			frames[i][j] = byte(i)
		}
	}

	testData := createMockEncapsulatedData(frames, true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer := bytes.NewReader(testData)
		// Get middle frame (index 5)
		_, err := compress.GetFrame(buffer, 5, 10, "<")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// Extended Offset Table Tests
// ============================================================================

func TestParseExtendedOffsetTable(t *testing.T) {
	// Create test data: 3 frames with 64-bit offsets and lengths
	offsetsData := make([]byte, 24) // 3 offsets * 8 bytes
	lengthsData := make([]byte, 24) // 3 lengths * 8 bytes

	// Frame 0: offset=0, length=1000
	binary.LittleEndian.PutUint64(offsetsData[0:8], 0)
	binary.LittleEndian.PutUint64(lengthsData[0:8], 1000)

	// Frame 1: offset=1000, length=2000
	binary.LittleEndian.PutUint64(offsetsData[8:16], 1000)
	binary.LittleEndian.PutUint64(lengthsData[8:16], 2000)

	// Frame 2: offset=3000, length=1500
	binary.LittleEndian.PutUint64(offsetsData[16:24], 3000)
	binary.LittleEndian.PutUint64(lengthsData[16:24], 1500)

	offsets, lengths, err := compress.ParseExtendedOffsetTable(offsetsData, lengthsData, "<")
	if err != nil {
		t.Fatalf("ParseExtendedOffsetTable error: %v", err)
	}

	// Verify offsets
	if len(offsets) != 3 {
		t.Fatalf("Expected 3 offsets, got %d", len(offsets))
	}
	if offsets[0] != 0 {
		t.Errorf("Offset 0: expected 0, got %d", offsets[0])
	}
	if offsets[1] != 1000 {
		t.Errorf("Offset 1: expected 1000, got %d", offsets[1])
	}
	if offsets[2] != 3000 {
		t.Errorf("Offset 2: expected 3000, got %d", offsets[2])
	}

	// Verify lengths
	if len(lengths) != 3 {
		t.Fatalf("Expected 3 lengths, got %d", len(lengths))
	}
	if lengths[0] != 1000 {
		t.Errorf("Length 0: expected 1000, got %d", lengths[0])
	}
	if lengths[1] != 2000 {
		t.Errorf("Length 1: expected 2000, got %d", lengths[1])
	}
	if lengths[2] != 1500 {
		t.Errorf("Length 2: expected 1500, got %d", lengths[2])
	}
}

func TestParseExtendedOffsetTableBigEndian(t *testing.T) {
	// Create test data with big-endian encoding
	offsetsData := make([]byte, 16) // 2 offsets * 8 bytes
	lengthsData := make([]byte, 16) // 2 lengths * 8 bytes

	// Frame 0: offset=0, length=5000
	binary.BigEndian.PutUint64(offsetsData[0:8], 0)
	binary.BigEndian.PutUint64(lengthsData[0:8], 5000)

	// Frame 1: offset=5000, length=10000
	binary.BigEndian.PutUint64(offsetsData[8:16], 5000)
	binary.BigEndian.PutUint64(lengthsData[8:16], 10000)

	offsets, lengths, err := compress.ParseExtendedOffsetTable(offsetsData, lengthsData, ">")
	if err != nil {
		t.Fatalf("ParseExtendedOffsetTable error: %v", err)
	}

	if offsets[0] != 0 || offsets[1] != 5000 {
		t.Errorf("Offsets mismatch: got %v, want [0 5000]", offsets)
	}
	if lengths[0] != 5000 || lengths[1] != 10000 {
		t.Errorf("Lengths mismatch: got %v, want [5000 10000]", lengths)
	}
}

func TestParseExtendedOffsetTableErrors(t *testing.T) {
	tests := []struct {
		name          string
		offsetsData   []byte
		lengthsData   []byte
		expectedError string
	}{
		{
			name:          "Invalid offsets length",
			offsetsData:   []byte{0x01, 0x02, 0x03}, // Not multiple of 8
			lengthsData:   make([]byte, 8),
			expectedError: "not a multiple of 8",
		},
		{
			name:          "Invalid lengths length",
			offsetsData:   make([]byte, 8),
			lengthsData:   []byte{0x01, 0x02, 0x03, 0x04, 0x05}, // Not multiple of 8
			expectedError: "not a multiple of 8",
		},
		{
			name:          "Mismatched counts",
			offsetsData:   make([]byte, 16), // 2 offsets
			lengthsData:   make([]byte, 24), // 3 lengths
			expectedError: "mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := compress.ParseExtendedOffsetTable(tt.offsetsData, tt.lengthsData, "<")
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("Error message doesn't contain '%s': %v", tt.expectedError, err)
			}
		})
	}
}

func TestParseExtendedOffsetTableLargeOffsets(t *testing.T) {
	// Test with offsets larger than 32-bit max (>4GB)
	offsetsData := make([]byte, 8)
	lengthsData := make([]byte, 8)

	largeOffset := uint64(5000000000) // ~5GB
	largeLength := uint64(1000000000) // ~1GB

	binary.LittleEndian.PutUint64(offsetsData, largeOffset)
	binary.LittleEndian.PutUint64(lengthsData, largeLength)

	offsets, lengths, err := compress.ParseExtendedOffsetTable(offsetsData, lengthsData, "<")
	if err != nil {
		t.Fatalf("ParseExtendedOffsetTable error: %v", err)
	}

	if offsets[0] != largeOffset {
		t.Errorf("Large offset mismatch: got %d, want %d", offsets[0], largeOffset)
	}
	if lengths[0] != largeLength {
		t.Errorf("Large length mismatch: got %d, want %d", lengths[0], largeLength)
	}
}
