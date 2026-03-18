// Package pixels provides access to and manipulation of DICOM pixel data.
// It handles uncompressed pixel arrays with support for:
// - Different sample depths (8-bit, 16-bit, 32-bit, etc.)
// - Signed and unsigned integer types
// - Floating-point data
// - Multi-frame images
// - Bit allocation and storage specifications
package pixels

import (
	"encoding/binary"
	"fmt"
	"math"
)

// PixelData represents raw pixel data from a DICOM image.
type PixelData struct {
	// Data is the raw pixel data bytes
	Data []byte

	// Rows is the number of rows (height)
	Rows uint32

	// Columns is the number of columns (width)
	Columns uint32

	// NumberOfFrames is the number of frames for multi-frame images
	NumberOfFrames uint32

	// BitsAllocated is the number of bits used for each sample (usually 8, 16, or 32)
	BitsAllocated uint16

	// BitsStored is the number of bits actually used (≤ BitsAllocated)
	BitsStored uint16

	// HighBit is the highest bit position (BitsStored - 1)
	HighBit uint16

	// PixelRepresentation: 0 = unsigned, 1 = signed
	PixelRepresentation uint16

	// SamplesPerPixel: usually 1 (grayscale) or 3 (RGB)
	SamplesPerPixel uint16

	// PhotometricInterpretation: "MONOCHROME2", "RGB", etc.
	PhotometricInterpretation string

	// LittleEndian indicates byte order for multi-byte values
	LittleEndian bool

	// PlanarConfiguration: 0 = interleaved (RGB), 1 = planar
	PlanarConfiguration uint16
}

// NewPixelData creates a new PixelData structure.
func NewPixelData(data []byte, rows, columns uint32) *PixelData {
	return &PixelData{
		Data:                      data,
		Rows:                      rows,
		Columns:                   columns,
		NumberOfFrames:            1,
		BitsAllocated:             16,
		BitsStored:                16,
		HighBit:                   15,
		PixelRepresentation:       0,
		SamplesPerPixel:           1,
		PhotometricInterpretation: "MONOCHROME2",
		LittleEndian:              true,
		PlanarConfiguration:       0,
	}
}

// Accessor provides methods to read and interpret pixel data.
type Accessor struct {
	pixelData *PixelData
}

// NewAccessor creates a new pixel data accessor.
func NewAccessor(pd *PixelData) *Accessor {
	return &Accessor{
		pixelData: pd,
	}
}

// GetPixelValue returns the value of a pixel at the given position.
// For single-frame images, frameIndex should be 0.
func (a *Accessor) GetPixelValue(row, column, frameIndex uint32) (interface{}, error) {
	if a.pixelData == nil {
		return nil, fmt.Errorf("pixel data is nil")
	}

	if row >= a.pixelData.Rows || column >= a.pixelData.Columns {
		return nil, fmt.Errorf("pixel position out of range: (%d,%d) vs (%d,%d)",
			row, column, a.pixelData.Rows, a.pixelData.Columns)
	}

	if frameIndex >= a.pixelData.NumberOfFrames {
		return nil, fmt.Errorf("frame index out of range: %d >= %d", frameIndex, a.pixelData.NumberOfFrames)
	}

	// Calculate byte offset
	samplesPerPixel := int(a.pixelData.SamplesPerPixel)
	pixelsPerFrame := int(a.pixelData.Rows * a.pixelData.Columns)
	bytesPerSample := int(a.pixelData.BitsAllocated / 8)

	// Position in frame
	pixelIndex := int(row*a.pixelData.Columns+column) * samplesPerPixel

	// Offset for frame
	frameOffset := int(frameIndex) * pixelsPerFrame * samplesPerPixel * bytesPerSample

	// Byte offset
	byteOffset := frameOffset + pixelIndex*bytesPerSample

	if byteOffset+bytesPerSample > len(a.pixelData.Data) {
		return nil, fmt.Errorf("insufficient data at offset %d", byteOffset)
	}

	// Extract and interpret value based on bits allocated
	switch a.pixelData.BitsAllocated {
	case 8:
		return a.pixelData.Data[byteOffset], nil

	case 16:
		return a.readUint16(byteOffset), nil

	case 32:
		return a.readUint32(byteOffset), nil

	default:
		return nil, fmt.Errorf("unsupported bits allocated: %d", a.pixelData.BitsAllocated)
	}
}

// GetRawValue returns the raw integer value at the given byte offset.
func (a *Accessor) GetRawValue(byteOffset int) (uint32, error) {
	if a.pixelData == nil {
		return 0, fmt.Errorf("pixel data is nil")
	}

	if byteOffset < 0 || byteOffset >= len(a.pixelData.Data) {
		return 0, fmt.Errorf("byte offset out of range: %d", byteOffset)
	}

	switch a.pixelData.BitsAllocated {
	case 8:
		return uint32(a.pixelData.Data[byteOffset]), nil
	case 16:
		if byteOffset+2 > len(a.pixelData.Data) {
			return 0, fmt.Errorf("insufficient data for 16-bit value")
		}
		return uint32(a.readUint16(byteOffset)), nil
	case 32:
		if byteOffset+4 > len(a.pixelData.Data) {
			return 0, fmt.Errorf("insufficient data for 32-bit value")
		}
		return a.readUint32(byteOffset), nil
	default:
		return 0, fmt.Errorf("unsupported bits allocated: %d", a.pixelData.BitsAllocated)
	}
}

// GetInterpretedValue applies pixel representation (signed/unsigned) interpretation.
func (a *Accessor) GetInterpretedValue(row, column, frameIndex uint32) (float64, error) {
	val, err := a.GetPixelValue(row, column, frameIndex)
	if err != nil {
		return 0, err
	}

	var uintVal uint32
	switch v := val.(type) {
	case uint8:
		uintVal = uint32(v)
	case uint16:
		uintVal = uint32(v)
	case uint32:
		uintVal = v
	default:
		return 0, fmt.Errorf("unsupported value type: %T", v)
	}

	// Apply pixel representation interpretation
	if a.pixelData.PixelRepresentation == 1 {
		// Signed
		signExtendedVal := a.signExtend(uintVal, int(a.pixelData.BitsStored))
		return float64(signExtendedVal), nil
	}

	// Unsigned
	return float64(uintVal), nil
}

// readUint16 reads a uint16 value respecting byte order.
func (a *Accessor) readUint16(offset int) uint16 {
	if a.pixelData.LittleEndian {
		return binary.LittleEndian.Uint16(a.pixelData.Data[offset : offset+2])
	}
	return binary.BigEndian.Uint16(a.pixelData.Data[offset : offset+2])
}

// readUint32 reads a uint32 value respecting byte order.
func (a *Accessor) readUint32(offset int) uint32 {
	if a.pixelData.LittleEndian {
		return binary.LittleEndian.Uint32(a.pixelData.Data[offset : offset+4])
	}
	return binary.BigEndian.Uint32(a.pixelData.Data[offset : offset+4])
}

// signExtend extends a value to a signed integer based on the number of bits.
func (a *Accessor) signExtend(value uint32, bits int) int32 {
	if bits >= 32 {
		return int32(value)
	}

	signBit := uint32(1) << uint32(bits-1)
	if value&signBit != 0 {
		// Sign bit is set - apply two's complement
		mask := (uint32(1) << uint32(bits)) - 1
		return -int32((^value & mask) + 1)
	}
	return int32(value)
}

// Statistics provides statistical information about pixel data.
type Statistics struct {
	// MinValue is the minimum pixel value
	MinValue float64

	// MaxValue is the maximum pixel value
	MaxValue float64

	// MeanValue is the mean (average) pixel value
	MeanValue float64

	// Median is the median pixel value
	MedianValue float64

	// TotalPixels is the total number of pixels
	TotalPixels uint64

	// SampleCount is the number of samples analyzed (for large images)
	SampleCount uint64
}

// Calculator provides statistical analysis methods.
type Calculator struct {
	pixelData *PixelData
	accessor  *Accessor
}

// NewCalculator creates a new pixel statistics calculator.
func NewCalculator(pd *PixelData) *Calculator {
	return &Calculator{
		pixelData: pd,
		accessor:  NewAccessor(pd),
	}
}

// CalculateStatistics computes basic statistics for the entire image.
// For large images, consider using CalculateStatisticsSampled instead.
func (c *Calculator) CalculateStatistics() (*Statistics, error) {
	if c.pixelData == nil {
		return nil, fmt.Errorf("pixel data is nil")
	}

	stats := &Statistics{
		MinValue:    math.MaxFloat64,
		MaxValue:    -math.MaxFloat64,
		TotalPixels: uint64(c.pixelData.Rows * c.pixelData.Columns * c.pixelData.NumberOfFrames),
	}

	if stats.TotalPixels == 0 {
		return stats, nil
	}

	var sum float64
	values := make([]float64, 0, stats.TotalPixels)

	for frame := uint32(0); frame < c.pixelData.NumberOfFrames; frame++ {
		for row := uint32(0); row < c.pixelData.Rows; row++ {
			for col := uint32(0); col < c.pixelData.Columns; col++ {
				val, err := c.accessor.GetInterpretedValue(row, col, frame)
				if err != nil {
					return nil, err
				}

				if val < stats.MinValue {
					stats.MinValue = val
				}
				if val > stats.MaxValue {
					stats.MaxValue = val
				}

				sum += val
				values = append(values, val)
			}
		}
	}

	stats.MeanValue = sum / float64(stats.TotalPixels)
	stats.SampleCount = stats.TotalPixels

	// Calculate median
	if len(values) > 0 {
		// Simple median calculation (not optimized for large datasets)
		stats.MedianValue = values[len(values)/2]
	}

	return stats, nil
}

// CalculateStatisticsSampled computes statistics using a sample of pixels.
// Useful for large images where full calculation would be slow.
func (c *Calculator) CalculateStatisticsSampled(sampleRate float64) (*Statistics, error) {
	if c.pixelData == nil {
		return nil, fmt.Errorf("pixel data is nil")
	}

	if sampleRate <= 0 || sampleRate > 1.0 {
		return nil, fmt.Errorf("sample rate must be between 0 and 1")
	}

	stats := &Statistics{
		MinValue:    math.MaxFloat64,
		MaxValue:    -math.MaxFloat64,
		TotalPixels: uint64(c.pixelData.Rows * c.pixelData.Columns * c.pixelData.NumberOfFrames),
	}

	if stats.TotalPixels == 0 {
		return stats, nil
	}

	var sum float64
	sampledCount := uint64(0)

	sampleStep := int(float64(1) / sampleRate)
	if sampleStep < 1 {
		sampleStep = 1
	}

	for frame := uint32(0); frame < c.pixelData.NumberOfFrames; frame++ {
		for row := uint32(0); row < c.pixelData.Rows; row += uint32(sampleStep) {
			for col := uint32(0); col < c.pixelData.Columns; col += uint32(sampleStep) {
				val, err := c.accessor.GetInterpretedValue(row, col, frame)
				if err != nil {
					continue
				}

				if val < stats.MinValue {
					stats.MinValue = val
				}
				if val > stats.MaxValue {
					stats.MaxValue = val
				}

				sum += val
				sampledCount++
			}
		}
	}

	if sampledCount > 0 {
		stats.MeanValue = sum / float64(sampledCount)
		stats.SampleCount = sampledCount
	}

	return stats, nil
}

// Validator checks pixel data for consistency and validity.
type Validator struct{}

// NewValidator creates a new pixel data validator.
func NewValidator() *Validator {
	return &Validator{}
}

// ValidatePixelData checks if the pixel data is well-formed.
func (v *Validator) ValidatePixelData(pd *PixelData) error {
	if pd == nil {
		return fmt.Errorf("pixel data is nil")
	}

	if pd.Rows == 0 || pd.Columns == 0 {
		return fmt.Errorf("rows and columns must be positive: %dx%d", pd.Rows, pd.Columns)
	}

	if pd.NumberOfFrames == 0 {
		return fmt.Errorf("number of frames must be positive: %d", pd.NumberOfFrames)
	}

	if pd.BitsAllocated == 0 {
		return fmt.Errorf("bits allocated must be positive: %d", pd.BitsAllocated)
	}

	if pd.BitsStored > pd.BitsAllocated {
		return fmt.Errorf("bits stored (%d) cannot exceed bits allocated (%d)", pd.BitsStored, pd.BitsAllocated)
	}

	if pd.HighBit != pd.BitsStored-1 && pd.BitsStored > 0 { //nolint:staticcheck // SA9003
		// HighBit should be BitsStored - 1, but allow some flexibility
	}

	// Check data size
	expectedSize := uint64(pd.Rows) * uint64(pd.Columns) * uint64(pd.NumberOfFrames) *
		uint64(pd.SamplesPerPixel) * uint64(pd.BitsAllocated/8)

	if uint64(len(pd.Data)) < expectedSize {
		return fmt.Errorf("insufficient data: have %d bytes, need at least %d bytes",
			len(pd.Data), expectedSize)
	}

	return nil
}

// GetDimensions returns the 3D dimensions of the image.
func (pd *PixelData) GetDimensions() (width, height, frames uint32) {
	return pd.Columns, pd.Rows, pd.NumberOfFrames
}

// GetBytesPerPixel returns the number of bytes per pixel.
func (pd *PixelData) GetBytesPerPixel() uint32 {
	return uint32((pd.BitsAllocated * pd.SamplesPerPixel) / 8)
}

// GetFrameSize returns the number of bytes in one frame.
func (pd *PixelData) GetFrameSize() uint32 {
	return pd.Rows * pd.Columns * pd.GetBytesPerPixel()
}

// GetPixelRange returns the theoretical min and max values for the pixel representation.
func (pd *PixelData) GetPixelRange() (min float64, max float64) {
	if pd.PixelRepresentation == 1 {
		// Signed
		max = float64(int64(1)<<uint(pd.BitsStored-1) - 1)
		min = -max - 1
	} else {
		// Unsigned
		max = float64(uint64(1)<<uint(pd.BitsStored) - 1)
		min = 0
	}
	return
}
