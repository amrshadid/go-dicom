package pixels_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/amrshadid/go-dicom/pixels"
)

// Helper function to create 16-bit pixel data
func create16BitPixelData(rows, columns uint32, values []uint16) *pixels.PixelData {
	data := make([]byte, len(values)*2)
	for i, v := range values {
		binary.LittleEndian.PutUint16(data[i*2:], v)
	}

	pd := pixels.NewPixelData(data, rows, columns)
	pd.BitsAllocated = 16
	pd.BitsStored = 16
	pd.HighBit = 15
	pd.LittleEndian = true
	return pd
}

// Helper function to create 8-bit pixel data
func create8BitPixelData(rows, columns uint32, values []uint8) *pixels.PixelData {
	data := make([]byte, len(values))
	copy(data, values)

	pd := pixels.NewPixelData(data, rows, columns)
	pd.BitsAllocated = 8
	pd.BitsStored = 8
	pd.HighBit = 7
	pd.LittleEndian = true
	return pd
}

// TestNewPixelData tests pixel data creation
func TestNewPixelData(t *testing.T) {
	data := make([]byte, 1024)
	pd := pixels.NewPixelData(data, 32, 32)

	if len(pd.Data) != len(data) {
		t.Error("data length mismatch")
	}

	if pd.Rows != 32 || pd.Columns != 32 {
		t.Errorf("dimensions mismatch: %dx%d", pd.Rows, pd.Columns)
	}

	if pd.NumberOfFrames != 1 {
		t.Errorf("expected 1 frame, got %d", pd.NumberOfFrames)
	}

	if !pd.LittleEndian {
		t.Error("expected little-endian")
	}
}

// TestAccessorGetPixelValue tests reading pixel values
func TestAccessorGetPixelValue(t *testing.T) {
	values := []uint16{100, 200, 300, 400}
	pd := create16BitPixelData(2, 2, values)

	accessor := pixels.NewAccessor(pd)

	// Test reading pixels
	val, err := accessor.GetPixelValue(0, 0, 0)
	if err != nil {
		t.Fatalf("GetPixelValue failed: %v", err)
	}

	if v, ok := val.(uint16); !ok || v != 100 {
		t.Errorf("expected 100, got %v", val)
	}

	val, err = accessor.GetPixelValue(0, 1, 0)
	if err != nil {
		t.Fatalf("GetPixelValue failed: %v", err)
	}

	if v, ok := val.(uint16); !ok || v != 200 {
		t.Errorf("expected 200, got %v", val)
	}
}

// TestAccessor8Bit tests 8-bit pixel reading
func TestAccessor8Bit(t *testing.T) {
	values := []uint8{10, 20, 30, 40}
	pd := create8BitPixelData(2, 2, values)

	accessor := pixels.NewAccessor(pd)

	val, err := accessor.GetPixelValue(0, 0, 0)
	if err != nil {
		t.Fatalf("GetPixelValue failed: %v", err)
	}

	if v, ok := val.(uint8); !ok || v != 10 {
		t.Errorf("expected 10, got %v", val)
	}
}

// TestAccessorOutOfRange tests boundary checking
func TestAccessorOutOfRange(t *testing.T) {
	values := []uint16{1, 2, 3, 4}
	pd := create16BitPixelData(2, 2, values)
	accessor := pixels.NewAccessor(pd)

	_, err := accessor.GetPixelValue(2, 0, 0)
	if err == nil {
		t.Error("expected error for row out of range")
	}

	_, err = accessor.GetPixelValue(0, 2, 0)
	if err == nil {
		t.Error("expected error for column out of range")
	}

	_, err = accessor.GetPixelValue(0, 0, 1)
	if err == nil {
		t.Error("expected error for frame out of range")
	}
}

// TestAccessorInterpretedValue tests signed value interpretation
func TestAccessorInterpretedValue(t *testing.T) {
	values := []uint16{100, 200, 300, 400}
	pd := create16BitPixelData(2, 2, values)
	pd.PixelRepresentation = 0 // unsigned

	accessor := pixels.NewAccessor(pd)

	val, err := accessor.GetInterpretedValue(0, 0, 0)
	if err != nil {
		t.Fatalf("GetInterpretedValue failed: %v", err)
	}

	if val != 100.0 {
		t.Errorf("expected 100.0, got %v", val)
	}
}

// TestCalculatorStatistics tests statistics calculation
func TestCalculatorStatistics(t *testing.T) {
	values := []uint16{10, 20, 30, 40}
	pd := create16BitPixelData(2, 2, values)

	calculator := pixels.NewCalculator(pd)
	stats, err := calculator.CalculateStatistics()

	if err != nil {
		t.Fatalf("CalculateStatistics failed: %v", err)
	}

	if stats.MinValue != 10.0 {
		t.Errorf("expected min 10.0, got %v", stats.MinValue)
	}

	if stats.MaxValue != 40.0 {
		t.Errorf("expected max 40.0, got %v", stats.MaxValue)
	}

	expectedMean := (10.0 + 20.0 + 30.0 + 40.0) / 4.0
	if stats.MeanValue != expectedMean {
		t.Errorf("expected mean %v, got %v", expectedMean, stats.MeanValue)
	}

	if stats.TotalPixels != 4 {
		t.Errorf("expected 4 pixels, got %d", stats.TotalPixels)
	}
}

// TestCalculatorSampledStatistics tests sampled statistics
func TestCalculatorSampledStatistics(t *testing.T) {
	values := []uint16{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	pd := create16BitPixelData(2, 5, values)

	calculator := pixels.NewCalculator(pd)
	stats, err := calculator.CalculateStatisticsSampled(0.5)

	if err != nil {
		t.Fatalf("CalculateStatisticsSampled failed: %v", err)
	}

	if stats.TotalPixels != 10 {
		t.Errorf("expected 10 total pixels, got %d", stats.TotalPixels)
	}

	if stats.SampleCount == 0 {
		t.Error("expected non-zero sample count")
	}

	if stats.SampleCount >= stats.TotalPixels {
		t.Errorf("sample count (%d) should be less than total (%d)", stats.SampleCount, stats.TotalPixels)
	}
}

// TestCalculatorInvalidSampleRate tests invalid sample rates
func TestCalculatorInvalidSampleRate(t *testing.T) {
	values := []uint16{10, 20, 30, 40}
	pd := create16BitPixelData(2, 2, values)
	calculator := pixels.NewCalculator(pd)

	_, err := calculator.CalculateStatisticsSampled(0)
	if err == nil {
		t.Error("expected error for sample rate 0")
	}

	_, err = calculator.CalculateStatisticsSampled(1.5)
	if err == nil {
		t.Error("expected error for sample rate > 1.0")
	}
}

// TestValidatorValidData tests validation of valid data
func TestValidatorValidData(t *testing.T) {
	values := []uint16{10, 20, 30, 40}
	pd := create16BitPixelData(2, 2, values)

	validator := pixels.NewValidator()
	err := validator.ValidatePixelData(pd)

	if err != nil {
		t.Errorf("validation should pass: %v", err)
	}
}

// TestValidatorInvalidData tests validation failures
func TestValidatorInvalidData(t *testing.T) {
	validator := pixels.NewValidator()

	// Test nil data
	err := validator.ValidatePixelData(nil)
	if err == nil {
		t.Error("expected error for nil pixel data")
	}

	// Test zero dimensions
	pd := pixels.NewPixelData(make([]byte, 100), 0, 10)
	err = validator.ValidatePixelData(pd)
	if err == nil {
		t.Error("expected error for zero rows")
	}

	// Test bits stored > bits allocated
	pd = pixels.NewPixelData(make([]byte, 100), 10, 10)
	pd.BitsAllocated = 8
	pd.BitsStored = 16
	err = validator.ValidatePixelData(pd)
	if err == nil {
		t.Error("expected error for bits stored > bits allocated")
	}

	// Test insufficient data
	pd = pixels.NewPixelData(make([]byte, 10), 100, 100)
	err = validator.ValidatePixelData(pd)
	if err == nil {
		t.Error("expected error for insufficient data")
	}
}

// TestGetDimensions tests dimension retrieval
func TestGetDimensions(t *testing.T) {
	pd := pixels.NewPixelData(make([]byte, 100), 32, 64)
	pd.NumberOfFrames = 3

	w, h, f := pd.GetDimensions()

	if w != 64 || h != 32 || f != 3 {
		t.Errorf("expected 64x32x3, got %dx%dx%d", w, h, f)
	}
}

// TestGetBytesPerPixel tests bytes per pixel calculation
func TestGetBytesPerPixel(t *testing.T) {
	pd := pixels.NewPixelData(make([]byte, 100), 10, 10)
	pd.BitsAllocated = 16
	pd.SamplesPerPixel = 1

	bpp := pd.GetBytesPerPixel()
	if bpp != 2 {
		t.Errorf("expected 2 bytes per pixel, got %d", bpp)
	}

	pd.SamplesPerPixel = 3
	bpp = pd.GetBytesPerPixel()
	if bpp != 6 {
		t.Errorf("expected 6 bytes per pixel, got %d", bpp)
	}
}

// TestGetFrameSize tests frame size calculation
func TestGetFrameSize(t *testing.T) {
	pd := pixels.NewPixelData(make([]byte, 100), 10, 20)
	pd.BitsAllocated = 16
	pd.SamplesPerPixel = 1

	frameSize := pd.GetFrameSize()
	expected := uint32(10 * 20 * 2) // rows * cols * (bits/8)

	if frameSize != expected {
		t.Errorf("expected frame size %d, got %d", expected, frameSize)
	}
}

// TestGetPixelRange tests pixel range calculation
func TestGetPixelRange(t *testing.T) {
	pd := pixels.NewPixelData(make([]byte, 100), 10, 10)
	pd.BitsStored = 16
	pd.PixelRepresentation = 0 // unsigned

	min, max := pd.GetPixelRange()

	if min != 0 {
		t.Errorf("expected unsigned min 0, got %v", min)
	}

	expectedMax := float64((uint64(1) << 16) - 1)
	if max != expectedMax {
		t.Errorf("expected unsigned max %v, got %v", expectedMax, max)
	}

	// Test signed range
	pd.PixelRepresentation = 1
	min, max = pd.GetPixelRange()

	expectedMin := -float64(int64(1) << 15)
	expectedMax = float64(int64(1)<<15 - 1)

	if min != expectedMin || max != expectedMax {
		t.Errorf("expected signed range [%v, %v], got [%v, %v]", expectedMin, expectedMax, min, max)
	}
}

// TestAccessorWithNilData tests accessor with nil pixel data
func TestAccessorWithNilData(t *testing.T) {
	accessor := pixels.NewAccessor(nil)

	_, err := accessor.GetPixelValue(0, 0, 0)
	if err == nil {
		t.Error("expected error for nil pixel data")
	}

	_, err = accessor.GetInterpretedValue(0, 0, 0)
	if err == nil {
		t.Error("expected error for nil pixel data")
	}
}

// TestCalculatorWithNilData tests calculator with nil data
func TestCalculatorWithNilData(t *testing.T) {
	calculator := pixels.NewCalculator(nil)

	_, err := calculator.CalculateStatistics()
	if err == nil {
		t.Error("expected error for nil pixel data")
	}
}

// TestMultiFrameImage tests multi-frame image handling
func TestMultiFrameImage(t *testing.T) {
	// Create a 2x2 image with 2 frames
	values := []uint16{
		// Frame 0
		10, 20,
		30, 40,
		// Frame 1
		50, 60,
		70, 80,
	}

	pd := create16BitPixelData(2, 2, values)
	pd.NumberOfFrames = 2

	accessor := pixels.NewAccessor(pd)

	// Read from frame 0
	val0, _ := accessor.GetPixelValue(0, 0, 0)
	if v, _ := val0.(uint16); v != 10 {
		t.Errorf("frame 0 pixel mismatch: expected 10, got %d", v)
	}

	// Read from frame 1
	val1, _ := accessor.GetPixelValue(0, 0, 1)
	if v, _ := val1.(uint16); v != 50 {
		t.Errorf("frame 1 pixel mismatch: expected 50, got %d", v)
	}
}

// TestAccessorGetRawValue tests raw value reading
func TestAccessorGetRawValue(t *testing.T) {
	values := []uint16{100, 200, 300, 400}
	pd := create16BitPixelData(2, 2, values)
	accessor := pixels.NewAccessor(pd)

	val, err := accessor.GetRawValue(0)
	if err != nil {
		t.Fatalf("GetRawValue failed: %v", err)
	}

	if val != 100 {
		t.Errorf("expected 100, got %d", val)
	}

	val, err = accessor.GetRawValue(2)
	if err != nil {
		t.Fatalf("GetRawValue failed: %v", err)
	}

	if val != 200 {
		t.Errorf("expected 200, got %d", val)
	}
}

// TestAccessorOutOfRangeByteOffset tests out-of-range byte offset
func TestAccessorOutOfRangeByteOffset(t *testing.T) {
	values := []uint16{100, 200}
	pd := create16BitPixelData(1, 2, values)
	accessor := pixels.NewAccessor(pd)

	_, err := accessor.GetRawValue(100)
	if err == nil {
		t.Error("expected error for out-of-range byte offset")
	}
}

// TestCalculatorEmptyImage tests statistics for empty image
func TestCalculatorEmptyImage(t *testing.T) {
	pd := pixels.NewPixelData(make([]byte, 0), 0, 0)
	calculator := pixels.NewCalculator(pd)

	stats, err := calculator.CalculateStatistics()
	if err != nil {
		t.Fatalf("CalculateStatistics failed: %v", err)
	}

	if stats.TotalPixels != 0 {
		t.Errorf("expected 0 pixels, got %d", stats.TotalPixels)
	}

	if stats.MinValue != math.MaxFloat64 || stats.MaxValue != -math.MaxFloat64 {
		t.Error("expected default min/max for empty image")
	}
}
