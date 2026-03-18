package dataset

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/encaps"
	"github.com/amrshadid/go-dicom/pixels"
	"github.com/amrshadid/go-dicom/tag"
)

// GetPixelDataInfo extracts pixel data metadata from the dataset.
// Returns an error if required pixel data tags are missing.
func (ds *Dataset) GetPixelDataInfo() (*PixelDataInfo, error) {
	info := &PixelDataInfo{
		BitsAllocated:       8,
		BitsStored:          8,
		Rows:                0,
		Columns:             0,
		NumberOfFrames:      1,
		SamplesPerPixel:     1,
		PixelRepresentation: 0,
	}

	// BitsAllocated (0028,0100) - required
	if bitsAllocElem, ok := ds.Get(tag.New(0x0028, 0x0100)); ok {
		if val, err := extractUintValue(bitsAllocElem); err == nil {
			info.BitsAllocated = int(val)
		}
	}

	// BitsStored (0028,0101)
	if bitsStoredElem, ok := ds.Get(tag.New(0x0028, 0x0101)); ok {
		if val, err := extractUintValue(bitsStoredElem); err == nil {
			info.BitsStored = int(val)
		}
	}

	// HighBit (0028,0102)
	if highBitElem, ok := ds.Get(tag.New(0x0028, 0x0102)); ok {
		if val, err := extractUintValue(highBitElem); err == nil {
			info.HighBit = int(val)
		}
	}

	// Rows (0028,0010) - required
	if rowsElem, ok := ds.Get(tag.New(0x0028, 0x0010)); ok {
		if val, err := extractUintValue(rowsElem); err == nil {
			info.Rows = int(val)
		}
	}

	// Columns (0028,0011) - required
	if colsElem, ok := ds.Get(tag.New(0x0028, 0x0011)); ok {
		if val, err := extractUintValue(colsElem); err == nil {
			info.Columns = int(val)
		}
	}

	// NumberOfFrames (0028,0008) - optional, default 1
	if framesElem, ok := ds.Get(tag.New(0x0028, 0x0008)); ok {
		if val, err := extractStringValue(framesElem); err == nil {
			if frames, err := strconv.Atoi(val); err == nil && frames > 0 {
				info.NumberOfFrames = frames
			}
		}
	}

	// SamplesPerPixel (0028,0002) - optional, default 1
	if samplesElem, ok := ds.Get(tag.New(0x0028, 0x0002)); ok {
		if val, err := extractUintValue(samplesElem); err == nil {
			info.SamplesPerPixel = int(val)
		}
	}

	// PixelRepresentation (0028,0103) - 0 unsigned, 1 signed
	if pixrepElem, ok := ds.Get(tag.New(0x0028, 0x0103)); ok {
		if val, err := extractUintValue(pixrepElem); err == nil {
			info.PixelRepresentation = int(val)
		}
	}

	// PhotometricInterpretation (0028,0004) - optional
	if photomElem, ok := ds.Get(tag.New(0x0028, 0x0004)); ok {
		if val, err := extractStringValue(photomElem); err == nil {
			info.PhotometricInterpretation = val
		}
	}

	// Validate required fields
	if info.Rows == 0 || info.Columns == 0 {
		return nil, fmt.Errorf("invalid pixel dimensions: rows=%d, columns=%d", info.Rows, info.Columns)
	}

	// Calculate bytes per frame
	info.BytesPerFrame = (info.Rows * info.Columns * info.SamplesPerPixel * info.BitsAllocated) / 8

	return info, nil
}

// PixelArray returns the pixel data as a multi-dimensional array interface.
// The return type depends on BitsAllocated:
//   - For 8-bit: returns [][][]uint8 (frames, rows, cols) for grayscale or [][][][][]uint8 (frames, rows, cols, samples)
//   - For 16-bit: returns [][][]uint16 (frames, rows, cols) for grayscale
//   - For 32-bit: returns [][][]uint32 (frames, rows, cols) for grayscale
//
// For single-frame images, the first dimension is 1.
// Returns error if pixel data is not present or cannot be parsed.
// Leverages the pixels module for efficient data access when appropriate.
func (ds *Dataset) PixelArray() (interface{}, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	// Get pixel data element
	pixelDataElem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		return nil, fmt.Errorf("pixel data element (7FE0,0010) not found")
	}

	pixelBytes, err := extractBytesValue(pixelDataElem)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pixel data bytes: %w", err)
	}

	if len(pixelBytes) == 0 {
		return nil, fmt.Errorf("pixel data is empty")
	}

	// Parse based on bit depth
	switch info.BitsAllocated {
	case 8:
		return parsePixelData8Bit(pixelBytes, info)
	case 16:
		return parsePixelData16Bit(pixelBytes, info)
	case 32:
		return parsePixelData32Bit(pixelBytes, info)
	case 64:
		return parsePixelData64Bit(pixelBytes, info)
	default:
		return nil, fmt.Errorf("unsupported bit depth: %d", info.BitsAllocated)
	}
}

// PixelArrayWithAccessor returns pixel data using the pixels module Accessor for efficient random access.
// This is preferred when you need to read individual pixels or groups of pixels.
// Returns error if pixel data is invalid or inaccessible.
func (ds *Dataset) PixelArrayWithAccessor() (*pixels.Accessor, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	// Get pixel data element
	pixelDataElem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		return nil, fmt.Errorf("pixel data element (7FE0,0010) not found")
	}

	pixelBytes, err := extractBytesValue(pixelDataElem)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pixel data bytes: %w", err)
	}

	// Create PixelData struct for the pixels module
	pd := pixels.NewPixelData(pixelBytes, uint32(info.Rows), uint32(info.Columns))
	pd.BitsAllocated = uint16(info.BitsAllocated)
	pd.BitsStored = uint16(info.BitsStored)
	pd.HighBit = uint16(info.HighBit)
	pd.NumberOfFrames = uint32(info.NumberOfFrames)
	pd.SamplesPerPixel = uint16(info.SamplesPerPixel)
	pd.PixelRepresentation = 0 // unsigned by default
	pd.PhotometricInterpretation = info.PhotometricInterpretation
	pd.LittleEndian = true // DICOM default

	// Validate pixel data using pixels module validator
	validator := pixels.NewValidator()
	if err := validator.ValidatePixelData(pd); err != nil {
		return nil, fmt.Errorf("pixel data validation failed: %w", err)
	}

	// Return accessor for efficient pixel access
	return pixels.NewAccessor(pd), nil
}

// PixelDataShape returns the shape of the pixel array as [frames, rows, cols] or
// [frames, rows, cols, samples] for multi-sample data.
func (ds *Dataset) PixelDataShape() ([]int, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	if info.SamplesPerPixel > 1 {
		return []int{info.NumberOfFrames, info.Rows, info.Columns, info.SamplesPerPixel}, nil
	}
	return []int{info.NumberOfFrames, info.Rows, info.Columns}, nil
}

// RawPixelData returns the raw pixel data as a byte slice.
func (ds *Dataset) RawPixelData() ([]byte, error) {
	pixelDataElem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		return nil, fmt.Errorf("pixel data element (7FE0,0010) not found")
	}

	return extractBytesValue(pixelDataElem)
}

// ExtractEncapsulatedFrames extracts individual frames from encapsulated (compressed) pixel data.
// Uses the encaps module Parser and Extractor for standard DICOM encapsulation handling.
// Returns the extracted frame data as bytes.
func (ds *Dataset) ExtractEncapsulatedFrames() (*compress.EncapsulatedData, error) {
	// Get raw pixel data element
	pixelDataElem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		return nil, fmt.Errorf("pixel data element (7FE0,0010) not found")
	}

	pixelBytes, err := extractBytesValue(pixelDataElem)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pixel data bytes: %w", err)
	}

	// Parse encapsulated data using encaps module
	reader := bytes.NewReader(pixelBytes)
	parser := encaps.NewParser(reader, true) // DICOM default is little-endian
	encData, err := parser.ParseEncapsulatedData()
	if err != nil {
		return nil, fmt.Errorf("failed to parse encapsulated data: %w", err)
	}

	return encData, nil
}

// GetEncapsulatedFrame extracts a specific frame from encapsulated pixel data.
// frameIndex is 0-based. Returns the frame data as compressed bytes.
func (ds *Dataset) GetEncapsulatedFrame(frameIndex int) ([]byte, error) {
	encData, err := ds.ExtractEncapsulatedFrames()
	if err != nil {
		return nil, err
	}

	// Extract specific frame using encaps module extractor
	extractor := encaps.NewExtractor(encData)
	return extractor.ExtractFrame(frameIndex)
}

// GetEncapsulationStatistics returns information about the encapsulated pixel data structure.
// Useful for understanding frame count, fragment organization, and data distribution.
func (ds *Dataset) GetEncapsulationStatistics() (*encaps.Statistics, error) {
	encData, err := ds.ExtractEncapsulatedFrames()
	if err != nil {
		return nil, err
	}

	// Get statistics using encaps module
	stats := encaps.GetStatistics(encData)
	return stats, nil
}

// IterFrames returns a channel that yields each frame of pixel data.
// Only useful for multi-frame images.
func (ds *Dataset) IterFrames() (<-chan [][][]uint16, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	if info.BitsAllocated != 16 {
		return nil, fmt.Errorf("IterFrames only supports 16-bit pixel data")
	}

	pixelBytes, err := ds.RawPixelData()
	if err != nil {
		return nil, err
	}

	ch := make(chan [][][]uint16, 1)

	go func() {
		defer close(ch)
		frameSize := info.BytesPerFrame
		offset := 0

		for frame := 0; frame < info.NumberOfFrames; frame++ {
			if offset+frameSize > len(pixelBytes) {
				return
			}

			frameData := make([][][]uint16, info.Rows)
			for row := 0; row < info.Rows; row++ {
				frameData[row] = make([][]uint16, info.Columns)
				for col := 0; col < info.Columns; col++ {
					pixel := binary.LittleEndian.Uint16(pixelBytes[offset : offset+2])
					frameData[row][col] = []uint16{pixel}
					offset += 2
				}
			}

			ch <- frameData
		}
	}()

	return ch, nil
}

// GetPixelStatistics returns statistical analysis of pixel data using the pixels module Calculator.
// This is useful for analyzing image properties like min/max/mean intensity values.
// For large images, use GetPixelStatisticsSampled instead.
func (ds *Dataset) GetPixelStatistics() (*pixels.Statistics, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	// Get pixel data element
	pixelDataElem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		return nil, fmt.Errorf("pixel data element (7FE0,0010) not found")
	}

	pixelBytes, err := extractBytesValue(pixelDataElem)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pixel data bytes: %w", err)
	}

	// Create PixelData struct for the pixels module
	pd := pixels.NewPixelData(pixelBytes, uint32(info.Rows), uint32(info.Columns))
	pd.BitsAllocated = uint16(info.BitsAllocated)
	pd.BitsStored = uint16(info.BitsStored)
	pd.HighBit = uint16(info.HighBit)
	pd.NumberOfFrames = uint32(info.NumberOfFrames)
	pd.SamplesPerPixel = uint16(info.SamplesPerPixel)
	pd.PixelRepresentation = 0 // unsigned by default
	pd.PhotometricInterpretation = info.PhotometricInterpretation
	pd.LittleEndian = true // DICOM default

	// Calculate statistics using pixels module calculator
	calculator := pixels.NewCalculator(pd)
	return calculator.CalculateStatistics()
}

// GetPixelStatisticsSampled returns statistical analysis based on a sample of pixel data.
// sampleRate should be between 0.0 and 1.0 (e.g., 0.1 for 10% sample).
// Useful for analyzing very large images without computing all pixels.
func (ds *Dataset) GetPixelStatisticsSampled(sampleRate float64) (*pixels.Statistics, error) {
	if sampleRate <= 0.0 || sampleRate > 1.0 {
		return nil, fmt.Errorf("sample rate must be between 0.0 and 1.0, got %v", sampleRate)
	}

	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	// Get pixel data element
	pixelDataElem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		return nil, fmt.Errorf("pixel data element (7FE0,0010) not found")
	}

	pixelBytes, err := extractBytesValue(pixelDataElem)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pixel data bytes: %w", err)
	}

	// Create PixelData struct for the pixels module
	pd := pixels.NewPixelData(pixelBytes, uint32(info.Rows), uint32(info.Columns))
	pd.BitsAllocated = uint16(info.BitsAllocated)
	pd.BitsStored = uint16(info.BitsStored)
	pd.HighBit = uint16(info.HighBit)
	pd.NumberOfFrames = uint32(info.NumberOfFrames)
	pd.SamplesPerPixel = uint16(info.SamplesPerPixel)
	pd.PixelRepresentation = 0 // unsigned by default
	pd.PhotometricInterpretation = info.PhotometricInterpretation
	pd.LittleEndian = true // DICOM default

	// Calculate sampled statistics using pixels module calculator
	calculator := pixels.NewCalculator(pd)
	return calculator.CalculateStatisticsSampled(sampleRate)
}

// Helper functions

func parsePixelData8Bit(data []byte, info *PixelDataInfo) ([][][]uint8, error) {
	result := make([][][]uint8, info.NumberOfFrames)

	offset := 0
	for frame := 0; frame < info.NumberOfFrames; frame++ {
		result[frame] = make([][]uint8, info.Rows)
		for row := 0; row < info.Rows; row++ {
			result[frame][row] = make([]uint8, info.Columns*info.SamplesPerPixel)
			for col := 0; col < info.Columns*info.SamplesPerPixel; col++ {
				if offset >= len(data) {
					return nil, fmt.Errorf("insufficient pixel data at frame %d, row %d, col %d", frame, row, col)
				}
				result[frame][row][col] = data[offset]
				offset++
			}
		}
	}

	return result, nil
}

func parsePixelData16Bit(data []byte, info *PixelDataInfo) ([][][]uint16, error) {
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("pixel data length is not even for 16-bit data")
	}

	result := make([][][]uint16, info.NumberOfFrames)
	offset := 0

	for frame := 0; frame < info.NumberOfFrames; frame++ {
		result[frame] = make([][]uint16, info.Rows)
		for row := 0; row < info.Rows; row++ {
			result[frame][row] = make([]uint16, info.Columns*info.SamplesPerPixel)
			for col := 0; col < info.Columns*info.SamplesPerPixel; col++ {
				if offset+1 >= len(data) {
					return nil, fmt.Errorf("insufficient pixel data at frame %d, row %d, col %d", frame, row, col)
				}
				result[frame][row][col] = binary.LittleEndian.Uint16(data[offset : offset+2])
				offset += 2
			}
		}
	}

	return result, nil
}

func parsePixelData32Bit(data []byte, info *PixelDataInfo) ([][][]uint32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("pixel data length is not divisible by 4 for 32-bit data")
	}

	result := make([][][]uint32, info.NumberOfFrames)
	offset := 0

	for frame := 0; frame < info.NumberOfFrames; frame++ {
		result[frame] = make([][]uint32, info.Rows)
		for row := 0; row < info.Rows; row++ {
			result[frame][row] = make([]uint32, info.Columns*info.SamplesPerPixel)
			for col := 0; col < info.Columns*info.SamplesPerPixel; col++ {
				if offset+3 >= len(data) {
					return nil, fmt.Errorf("insufficient pixel data at frame %d, row %d, col %d", frame, row, col)
				}
				result[frame][row][col] = binary.LittleEndian.Uint32(data[offset : offset+4])
				offset += 4
			}
		}
	}

	return result, nil
}

func parsePixelData64Bit(data []byte, info *PixelDataInfo) ([][][]uint64, error) {
	if len(data)%8 != 0 {
		return nil, fmt.Errorf("pixel data length is not divisible by 8 for 64-bit data")
	}

	result := make([][][]uint64, info.NumberOfFrames)
	offset := 0

	for frame := 0; frame < info.NumberOfFrames; frame++ {
		result[frame] = make([][]uint64, info.Rows)
		for row := 0; row < info.Rows; row++ {
			result[frame][row] = make([]uint64, info.Columns*info.SamplesPerPixel)
			for col := 0; col < info.Columns*info.SamplesPerPixel; col++ {
				if offset+7 >= len(data) {
					return nil, fmt.Errorf("insufficient pixel data at frame %d, row %d, col %d", frame, row, col)
				}
				result[frame][row][col] = binary.LittleEndian.Uint64(data[offset : offset+8])
				offset += 8
			}
		}
	}

	return result, nil
}

// Helper to extract uint value from data element
func extractUintValue(elem *dataelem.DataElement) (uint32, error) {
	if elem == nil {
		return 0, fmt.Errorf("element is nil")
	}

	value := elem.GetValue()
	if value == nil {
		return 0, fmt.Errorf("element value is nil")
	}

	// Handle different types
	switch v := value.(type) {
	case uint32:
		return v, nil
	case uint16:
		return uint32(v), nil
	case uint8:
		return uint32(v), nil
	case int:
		if v < 0 {
			return 0, fmt.Errorf("negative value: %d", v)
		}
		return uint32(v), nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("negative value: %d", v)
		}
		return uint32(v), nil
	case []byte:
		if len(v) >= 2 {
			return uint32(binary.LittleEndian.Uint16(v)), nil
		}
		if len(v) == 1 {
			return uint32(v[0]), nil
		}
		return 0, fmt.Errorf("byte slice too short: %d bytes", len(v))
	case string:
		// Try to parse as integer string
		val, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("cannot parse string as uint: %s", v)
		}
		return uint32(val), nil
	default:
		return 0, fmt.Errorf("unsupported type for uint conversion: %T", v)
	}
}

// Helper to extract string value from data element
func extractStringValue(elem *dataelem.DataElement) (string, error) {
	if elem == nil {
		return "", fmt.Errorf("element is nil")
	}

	value := elem.GetValue()
	if value == nil {
		return "", fmt.Errorf("element value is nil")
	}

	// Handle different types
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case uint32, uint16, uint8, int, int64:
		return fmt.Sprintf("%v", v), nil
	default:
		return "", fmt.Errorf("unsupported type for string conversion: %T", v)
	}
}

// Helper to extract bytes value from data element
func extractBytesValue(elem *dataelem.DataElement) ([]byte, error) {
	if elem == nil {
		return nil, fmt.Errorf("element is nil")
	}

	value := elem.GetValue()
	if value == nil {
		return nil, fmt.Errorf("element value is nil")
	}

	// Handle different types
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("unsupported type for bytes conversion: %T", v)
	}
}
