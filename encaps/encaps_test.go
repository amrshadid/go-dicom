package encaps_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/encaps"
)

// Helper function to create a simple encapsulated data buffer
func createEncapsulatedBuffer(fragments [][]byte, littleEndian bool) []byte {
	var buf bytes.Buffer

	// Write Basic Offset Table (empty for simplicity)
	// Item tag (0xFFFE, 0xE000) + length (0x00000000)
	buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	if littleEndian {
		buf.Write([]byte{0x00, 0x00, 0x00, 0x00})
	} else {
		buf.Write([]byte{0x00, 0x00, 0x00, 0x00})
	}

	// Write fragments
	for _, fragment := range fragments {
		// Item tag (0xFFFE, 0xE000)
		buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})

		// Length (4 bytes)
		length := uint32(len(fragment))
		if littleEndian {
			binary.Write(&buf, binary.LittleEndian, length)
		} else {
			binary.Write(&buf, binary.BigEndian, length)
		}

		// Fragment data
		buf.Write(fragment)
	}

	return buf.Bytes()
}

// TestParserBasicEncapsulation tests parsing basic encapsulated data
func TestParserBasicEncapsulation(t *testing.T) {
	fragment1 := []byte("compressed data 1")
	fragment2 := []byte("compressed data 2")

	encapBuf := createEncapsulatedBuffer([][]byte{fragment1, fragment2}, true)

	reader := bytes.NewReader(encapBuf)
	parser := encaps.NewParser(reader, true)

	encData, err := parser.ParseEncapsulatedData()
	if err != nil {
		t.Fatalf("ParseEncapsulatedData failed: %v", err)
	}

	if encData == nil {
		t.Fatal("ParseEncapsulatedData returned nil")
	}

	if len(encData.Fragments) != 2 {
		t.Errorf("expected 2 fragments, got %d", len(encData.Fragments))
	}

	if !bytes.Equal(encData.Fragments[0], fragment1) {
		t.Errorf("fragment 1 mismatch: expected %v, got %v", fragment1, encData.Fragments[0])
	}

	if !bytes.Equal(encData.Fragments[1], fragment2) {
		t.Errorf("fragment 2 mismatch: expected %v, got %v", fragment2, encData.Fragments[1])
	}
}

// TestExtractorSingleFrame tests extracting a single frame
func TestExtractorSingleFrame(t *testing.T) {
	encData := &compress.EncapsulatedData{
		Fragments:      [][]byte{[]byte("frame1"), []byte("frame2")},
		NumberOfFrames: 2,
		Endianness:     "<",
	}

	extractor := encaps.NewExtractor(encData)

	frame0, err := extractor.ExtractFrame(0)
	if err != nil {
		t.Fatalf("ExtractFrame(0) failed: %v", err)
	}

	if !bytes.Equal(frame0, []byte("frame1")) {
		t.Errorf("frame 0 mismatch: expected 'frame1', got %v", frame0)
	}

	frame1, err := extractor.ExtractFrame(1)
	if err != nil {
		t.Fatalf("ExtractFrame(1) failed: %v", err)
	}

	if !bytes.Equal(frame1, []byte("frame2")) {
		t.Errorf("frame 1 mismatch: expected 'frame2', got %v", frame1)
	}
}

// TestExtractorFrameCount tests getting frame count
func TestExtractorFrameCount(t *testing.T) {
	encData := &compress.EncapsulatedData{
		Fragments:      [][]byte{[]byte("f1"), []byte("f2"), []byte("f3")},
		NumberOfFrames: 3,
		Endianness:     "<",
	}

	extractor := encaps.NewExtractor(encData)

	frameCount := extractor.GetFrameCount()
	if frameCount != 3 {
		t.Errorf("expected frame count 3, got %d", frameCount)
	}

	fragmentCount := extractor.GetFragmentCount()
	if fragmentCount != 3 {
		t.Errorf("expected fragment count 3, got %d", fragmentCount)
	}
}

// TestValidatorValidEncapsulation tests validation of valid data
func TestValidatorValidEncapsulation(t *testing.T) {
	encData := &compress.EncapsulatedData{
		Fragments:        [][]byte{[]byte("data1"), []byte("data2")},
		NumberOfFrames:   2,
		BasicOffsetTable: []uint32{0, 5},
		Endianness:       "<",
	}

	validator := encaps.NewValidator()
	err := validator.ValidateEncapsulation(encData)
	if err != nil {
		t.Errorf("validation should pass but got error: %v", err)
	}
}

// TestValidatorInvalidEncapsulation tests validation of invalid data
func TestValidatorInvalidEncapsulation(t *testing.T) {
	// Test with nil encapsulation
	validator := encaps.NewValidator()
	err := validator.ValidateEncapsulation(nil)
	if err == nil {
		t.Error("validation should fail for nil encapsulation")
	}

	// Test with no fragments
	encData := &compress.EncapsulatedData{
		Fragments:  [][]byte{},
		Endianness: "<",
	}
	err = validator.ValidateEncapsulation(encData)
	if err == nil {
		t.Error("validation should fail for no fragments")
	}

	// Test with invalid endianness
	encData = &compress.EncapsulatedData{
		Fragments:  [][]byte{[]byte("data")},
		Endianness: "X",
	}
	err = validator.ValidateEncapsulation(encData)
	if err == nil {
		t.Error("validation should fail for invalid endianness")
	}
}

// TestValidatorBOTConsistency tests BOT consistency checking
func TestValidatorBOTConsistency(t *testing.T) {
	// BOT count != NumberOfFrames
	encData := &compress.EncapsulatedData{
		Fragments:        [][]byte{[]byte("data1"), []byte("data2"), []byte("data3")},
		NumberOfFrames:   3,
		BasicOffsetTable: []uint32{0, 5}, // Only 2 offsets for 3 frames
		Endianness:       "<",
	}

	validator := encaps.NewValidator()
	err := validator.ValidateEncapsulation(encData)
	if err == nil {
		t.Error("validation should fail for BOT/NumberOfFrames mismatch")
	}
}

// TestGetStatistics tests statistics generation
func TestGetStatistics(t *testing.T) {
	encData := &compress.EncapsulatedData{
		Fragments:        [][]byte{[]byte("frame1"), []byte("frame2"), []byte("frame3")},
		NumberOfFrames:   3,
		BasicOffsetTable: []uint32{0, 6, 12},
		Endianness:       "<",
	}

	stats := encaps.GetStatistics(encData)

	if stats.FrameCount != 3 {
		t.Errorf("expected frame count 3, got %d", stats.FrameCount)
	}

	if stats.FragmentCount != 3 {
		t.Errorf("expected fragment count 3, got %d", stats.FragmentCount)
	}

	expectedSize := uint64(6 + 6 + 6) // 3 fragments of 6 bytes each
	if stats.TotalSize != expectedSize {
		t.Errorf("expected total size %d, got %d", expectedSize, stats.TotalSize)
	}

	expectedAvg := expectedSize / 3
	if stats.AverageFrameSize != expectedAvg {
		t.Errorf("expected average frame size %d, got %d", expectedAvg, stats.AverageFrameSize)
	}

	if !stats.HasBasicOffsetTable {
		t.Error("expected HasBasicOffsetTable to be true")
	}
}

// TestReframerBasicReframing tests basic reframing operation
func TestReframerBasicReframing(t *testing.T) {
	encData := &compress.EncapsulatedData{
		Fragments:      [][]byte{[]byte("AAAA"), []byte("BBBB"), []byte("CCCC"), []byte("DDDD")},
		NumberOfFrames: 4,
		Endianness:     "<",
	}

	reframer := encaps.NewReframer(encData, 2)
	newEncData, err := reframer.ReframeData()
	if err != nil {
		t.Fatalf("ReframeData failed: %v", err)
	}

	if newEncData.NumberOfFrames != 2 {
		t.Errorf("expected 2 frames after reframing, got %d", newEncData.NumberOfFrames)
	}

	if len(newEncData.Fragments) != 2 {
		t.Errorf("expected 2 fragments after reframing, got %d", len(newEncData.Fragments))
	}
}

// TestReframerInvalidInput tests reframing with invalid input
func TestReframerInvalidInput(t *testing.T) {
	// Test with nil encapsulation
	reframer := encaps.NewReframer(nil, 2)
	_, err := reframer.ReframeData()
	if err == nil {
		t.Error("reframing nil encapsulation should fail")
	}

	// Test with invalid target frames
	encData := &compress.EncapsulatedData{
		Fragments:  [][]byte{[]byte("data")},
		Endianness: "<",
	}

	reframer = encaps.NewReframer(encData, 0)
	_, err = reframer.ReframeData()
	if err == nil {
		t.Error("reframing with 0 target frames should fail")
	}
}

// TestParserWithNilReader tests parser with nil reader
func TestParserWithNilReader(t *testing.T) {
	parser := encaps.NewParser(nil, true)
	_, err := parser.ParseEncapsulatedData()
	if err == nil {
		t.Error("parsing with nil reader should fail")
	}
}

// TestExtractorWithNilData tests extractor with nil encapsulation
func TestExtractorWithNilData(t *testing.T) {
	extractor := encaps.NewExtractor(nil)

	frameCount := extractor.GetFrameCount()
	if frameCount != 0 {
		t.Errorf("expected frame count 0 for nil encapsulation, got %d", frameCount)
	}

	_, err := extractor.ExtractFrame(0)
	if err == nil {
		t.Error("extracting from nil encapsulation should fail")
	}
}

// TestExtractorOutOfRange tests frame extraction with out-of-range index
func TestExtractorOutOfRange(t *testing.T) {
	encData := &compress.EncapsulatedData{
		Fragments:      [][]byte{[]byte("frame1"), []byte("frame2")},
		NumberOfFrames: 2,
		Endianness:     "<",
	}

	extractor := encaps.NewExtractor(encData)

	_, err := extractor.ExtractFrame(5)
	if err == nil {
		t.Error("extracting out-of-range frame should fail")
	}

	_, err = extractor.ExtractFrame(-1)
	if err == nil {
		t.Error("extracting negative frame index should fail")
	}
}

// TestGetStatisticsWithNilData tests statistics generation with nil data
func TestGetStatisticsWithNilData(t *testing.T) {
	stats := encaps.GetStatistics(nil)
	if stats == nil {
		t.Fatal("GetStatistics returned nil")
	}

	if stats.FrameCount != 0 {
		t.Errorf("expected frame count 0, got %d", stats.FrameCount)
	}

	if stats.TotalSize != 0 {
		t.Errorf("expected total size 0, got %d", stats.TotalSize)
	}
}

// TestGetStatisticsWithExtendedTables tests statistics with extended offset tables
func TestGetStatisticsWithExtendedTables(t *testing.T) {
	encData := &compress.EncapsulatedData{
		Fragments:                  [][]byte{[]byte("data1"), []byte("data2")},
		NumberOfFrames:             2,
		ExtendedOffsetTable:        []uint64{0, 5},
		ExtendedOffsetTableLengths: []uint64{5, 5},
		Endianness:                 "<",
	}

	stats := encaps.GetStatistics(encData)

	if !stats.HasExtendedOffsetTable {
		t.Error("expected HasExtendedOffsetTable to be true")
	}

	if stats.FrameCount != 2 {
		t.Errorf("expected 2 frames, got %d", stats.FrameCount)
	}
}

// TestParserEmptyFragments tests parser with empty fragments
func TestParserEmptyFragments(t *testing.T) {
	var buf bytes.Buffer

	// Write only Basic Offset Table
	buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	reader := bytes.NewReader(buf.Bytes())
	parser := encaps.NewParser(reader, true)

	encData, err := parser.ParseEncapsulatedData()
	if err == nil {
		// Should fail because no fragments
		if len(encData.Fragments) == 0 {
			t.Errorf("expected parsing to fail with no fragments")
		}
	}
}
