package dataset

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

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

	// PlanarConfiguration (0028,0006) — only meaningful when SamplesPerPixel > 1.
	//
	// The field existed on PixelDataInfo and was never read from the data set, so
	// it was always 0 and color-by-plane images came back with their channels
	// scrambled: the first pixel reading as the first three reds.
	if planarElem, ok := ds.Get(tag.New(0x0028, 0x0006)); ok {
		if val, err := extractUintValue(planarElem); err == nil {
			info.PlanarConfiguration = int(val)
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
//
// Color samples are flattened into the column dimension: a 100x100 RGB frame
// comes back as 100 rows of 300 values, with each pixel's samples adjacent.
// That differs from PixelDataShape, which reports color data as four
// dimensions. Use PixelArrayBySample for that shape.
//
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

	pixelBytes, err := ds.pixelBytesForDecoding(info)
	if err != nil {
		return nil, err
	}

	// Parse based on bit depth
	switch info.BitsAllocated {
	case 1:
		return parsePixelData1Bit(pixelBytes, info)
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

// DecodedPixelData returns the pixel data as native, uncompressed bytes,
// decompressing it if the data set's transfer syntax says it is compressed.
//
// This is what RawPixelData is not: that returns the element's value as stored,
// which for a compressed instance is encapsulated fragments rather than pixels.
// Anything that has to hand pixel data to something expecting an uncompressed
// layout — writing a native file, or sending over a presentation context that
// negotiated an uncompressed syntax — needs this instead.
//
// It fails rather than returning the compressed bytes when no decoder is
// available for the syntax, because bytes that are not pixels are worse than an
// error to whatever receives them.
func (ds *Dataset) DecodedPixelData() ([]byte, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	return ds.pixelBytesForDecoding(info)
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

// parsePixelData1Bit unpacks single-bit pixel data, which is how Segmentation
// objects and overlays store a mask.
//
// The bits run continuously across the whole frame, least significant first
// within each byte, with no padding between rows — so a row does not begin on a
// byte boundary unless its width happens to be a multiple of eight, and
// unpacking row by row would shear the image.
func parsePixelData1Bit(data []byte, info *PixelDataInfo) ([][][]uint8, error) {
	perFrame := info.Rows * info.Columns * info.SamplesPerPixel
	total := perFrame * info.NumberOfFrames
	need := (total + 7) / 8
	if len(data) < need {
		return nil, fmt.Errorf("pixel data holds %d bytes, need %d for %d frames of %dx%d single-bit samples",
			len(data), need, info.NumberOfFrames, info.Rows, info.Columns)
	}

	out := make([][][]uint8, info.NumberOfFrames)
	bit := 0
	for f := range out {
		out[f] = make([][]uint8, info.Rows)
		for r := range out[f] {
			row := make([]uint8, info.Columns*info.SamplesPerPixel)
			for c := range row {
				row[c] = (data[bit/8] >> (bit % 8)) & 1
				bit++
			}
			out[f][r] = row
		}
	}
	return out, nil
}

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

	// Trailing padding is stripped. PS3.5 §6.2 pads a text value to an even
	// length with a space, or with NUL for UI, and that padding is not part of
	// the value. Returning it untrimmed made NumberOfFrames of "2 " fail
	// strconv.Atoi, so every multi-frame image silently reported one frame —
	// and for a compressed image that means the frames after the first are
	// dropped without an error.
	switch v := value.(type) {
	case string:
		return strings.TrimRight(v, " \x00"), nil
	case []byte:
		return strings.TrimRight(string(v), " \x00"), nil
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

// pixelBytesForDecoding returns the raw, uncompressed pixel buffer for this
// data set, decompressing encapsulated pixel data when necessary.
//
// PixelData under a compressed transfer syntax is not pixels: it is an
// encapsulation holding one or more compressed frames. Handing those bytes to
// the pixel parsers produced "insufficient pixel data at frame 0, row N, col M"
// on every compressed file, because the parsers were reading a JPEG or RLE
// stream as though it were samples.
func (ds *Dataset) pixelBytesForDecoding(info *PixelDataInfo) ([]byte, error) {
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

	compression, encapsulated := ds.pixelCompression(pixelBytes, info)
	if !encapsulated {
		// Native pixel data is laid out exactly as the header describes, so both
		// corrections belong here. Encapsulated data does not: a decoder's output
		// convention governs it, and the ones in this package already produce
		// pixel-interleaved little-endian samples.
		return normalizeNativePixelBytes(pixelBytes, info, ds.TransferSyntaxUID()), nil
	}

	encData, err := ds.ExtractEncapsulatedFrames()
	if err != nil {
		return nil, fmt.Errorf("failed to read encapsulated pixel data: %w", err)
	}

	var out []byte
	for i, fragment := range encData.Fragments {
		frame, err := decompressPixelFrame(compression, fragment, info)
		if err != nil {
			return nil, fmt.Errorf("failed to decode %s frame %d: %w", compression, i, err)
		}
		out = append(out, frame...)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("encapsulated pixel data held no frames")
	}
	return out, nil
}

// normalizeNativePixelBytes puts native pixel data into the layout every
// accessor above assumes: samples pixel-interleaved, little endian.
func normalizeNativePixelBytes(pixelBytes []byte, info *PixelDataInfo, transferSyntax string) []byte {
	pixelBytes = fixWideBigEndianSamples(pixelBytes, info, transferSyntax)
	pixelBytes = upsampleYBR422(pixelBytes, info)
	return deinterleavePlanes(pixelBytes, info)
}

// upsampleYBR422 expands horizontally subsampled chroma to one sample per pixel.
//
// YBR_FULL_422 native data stores a luminance for every pixel but a chroma pair
// for every second one, laid out Y1 Y2 Cb Cr per pixel pair — two bytes per
// pixel where SamplesPerPixel says three. Everything downstream sizes from
// SamplesPerPixel, so without expanding it here a frame is simply reported as
// too short, which is what it was.
//
// The chroma is replicated across the pair, which is what pydicom does and the
// only choice that needs no filter.
func upsampleYBR422(pixelBytes []byte, info *PixelDataInfo) []byte {
	if info.PhotometricInterpretation != "YBR_FULL_422" || info.SamplesPerPixel != 3 {
		return pixelBytes
	}
	if info.BitsAllocated != 8 {
		// Subsampling at other depths is not something encoders produce, and
		// guessing at the layout would be worse than the length error.
		return pixelBytes
	}

	pixels := info.Rows * info.Columns * info.NumberOfFrames
	if pixels <= 0 || pixels%2 != 0 {
		return pixelBytes
	}
	stored := pixels * 2
	if len(pixelBytes) < stored || len(pixelBytes) >= pixels*3 {
		// Either too short to be 4:2:2, or already full size — in which case the
		// attribute describes the color space but not a subsampled layout.
		return pixelBytes
	}

	out := make([]byte, pixels*3)
	for pair := 0; pair*4+3 < stored; pair++ {
		y1, y2 := pixelBytes[pair*4], pixelBytes[pair*4+1]
		cb, cr := pixelBytes[pair*4+2], pixelBytes[pair*4+3]
		i := pair * 6
		out[i], out[i+1], out[i+2] = y1, cb, cr
		out[i+3], out[i+4], out[i+5] = y2, cb, cr
	}
	return out
}

// fixWideBigEndianSamples repairs samples wider than the VR they arrived in.
//
// Explicit VR Big Endian is byte-swapped on read according to the VR, and pixel
// data is OW — two-byte words. That is right until BitsAllocated is 32 or 64, at
// which point each sample has had its 16-bit halves swapped but not its whole
// width, and every value comes out scrambled. RT Dose is the common case: a dose
// of 1249000 reads back as 250085395.
//
// Swapping the 16-bit words within each sample completes the reversal. pydicom
// reaches the same values by interpreting the raw bytes at the sample width,
// which is what archives expect regardless of what the VR alone would imply.
func fixWideBigEndianSamples(pixelBytes []byte, info *PixelDataInfo, transferSyntax string) []byte {
	const explicitVRBigEndian = "1.2.840.10008.1.2.2"
	if transferSyntax != explicitVRBigEndian {
		return pixelBytes
	}
	width := info.BitsAllocated / 8
	if width <= 2 {
		return pixelBytes
	}

	out := make([]byte, len(pixelBytes))
	copy(out, pixelBytes)
	for start := 0; start+width <= len(out); start += width {
		sample := out[start : start+width]
		for i, j := 0, len(sample)-2; i < j; i, j = i+2, j-2 {
			sample[i], sample[j] = sample[j], sample[i]
			sample[i+1], sample[j+1] = sample[j+1], sample[i+1]
		}
	}
	return out
}

// deinterleavePlanes converts color-by-plane pixel data to color-by-pixel.
//
// Planar Configuration 1 stores every red sample, then every green, then every
// blue. The accessors above all assume the interleaved form, so without this a
// color image comes back with its channels scrambled — the first pixel reading
// as the first three reds rather than as one pixel's red, green and blue.
func deinterleavePlanes(pixelBytes []byte, info *PixelDataInfo) []byte {
	if info.PlanarConfiguration != 1 || info.SamplesPerPixel < 2 {
		return pixelBytes
	}
	width := info.BitsAllocated / 8
	if width < 1 {
		return pixelBytes
	}

	samples := info.SamplesPerPixel
	perFrame := info.Rows * info.Columns
	frameBytes := perFrame * samples * width
	if perFrame <= 0 || frameBytes <= 0 || len(pixelBytes) < frameBytes {
		// Not enough to reorder safely; the callers below report the shortfall
		// with the sizes involved, which is more use than a silent reshuffle.
		return pixelBytes
	}

	out := make([]byte, len(pixelBytes))
	copy(out, pixelBytes)

	for frame := 0; frame+frameBytes <= len(pixelBytes); frame += frameBytes {
		src := pixelBytes[frame : frame+frameBytes]
		dst := out[frame : frame+frameBytes]
		for sample := 0; sample < samples; sample++ {
			plane := src[sample*perFrame*width:]
			for pixel := 0; pixel < perFrame; pixel++ {
				copy(dst[(pixel*samples+sample)*width:], plane[pixel*width:(pixel+1)*width])
			}
		}
	}
	return out
}

// pixelCompression reports how the pixel data is compressed and whether it is
// encapsulated.
//
// The transfer syntax is the authority, and readers record it on the data set.
// When it is absent — a Dataset assembled by hand, or received without one —
// the structure is used instead: encapsulated pixel data begins with an item
// tag, and raw pixel data is exactly as long as the image dimensions require.
func (ds *Dataset) pixelCompression(pixelBytes []byte, info *PixelDataInfo) (compress.CompressionType, bool) {
	if uid := ds.TransferSyntaxUID(); uid != "" {
		compression, err := compress.TransferSyntaxToCompressionType(uid)
		if err != nil {
			return compress.UNCOMPRESSED, false
		}
		// Deflate compresses the whole data set, not the pixel element, so by
		// the time PixelData is in hand it is already raw.
		if compression == compress.UNCOMPRESSED || compression == compress.DEFLATE {
			return compression, false
		}
		return compression, true
	}

	expected := info.BytesPerFrame * info.NumberOfFrames
	if expected > 0 && len(pixelBytes) == expected {
		return compress.UNCOMPRESSED, false
	}
	if len(pixelBytes) >= 8 && pixelBytes[0] == 0xFE && pixelBytes[1] == 0xFF &&
		pixelBytes[2] == 0x00 && pixelBytes[3] == 0xE0 {
		return sniffPixelCompression(pixelBytes), true
	}
	return compress.UNCOMPRESSED, false
}

// sniffPixelCompression guesses a codec from the first fragment, for data sets
// carrying no transfer syntax. RLE frames begin with a parseable segment
// header; JPEG of every flavor begins with the SOI marker.
func sniffPixelCompression(pixelBytes []byte) compress.CompressionType {
	encData, err := ds_parseEncapsulated(pixelBytes)
	if err != nil || len(encData.Fragments) == 0 {
		return compress.UNCOMPRESSED
	}
	first := encData.Fragments[0]
	if len(first) >= 2 && first[0] == 0xFF && first[1] == 0xD8 {
		return compress.JPEG
	}
	if compress.NewRLEDecompressor().CanDecompress(first) {
		return compress.RLE
	}
	return compress.UNCOMPRESSED
}

// ds_parseEncapsulated parses encapsulated pixel data bytes without needing a
// Dataset, so sniffing does not have to re-enter the element lookup.
func ds_parseEncapsulated(pixelBytes []byte) (*compress.EncapsulatedData, error) {
	return encaps.NewParser(bytes.NewReader(pixelBytes), true).ParseEncapsulatedData()
}

// decompressPixelFrame decodes one compressed frame into raw samples.
func decompressPixelFrame(compression compress.CompressionType, fragment []byte, info *PixelDataInfo) ([]byte, error) {
	// RLE needs the image layout: its segments are planar, one per byte of
	// each sample, and interleaving them requires knowing how many of each.
	if compression == compress.RLE {
		return compress.NewRLEDecompressor().DecompressFrame(
			fragment, info.SamplesPerPixel, info.BitsAllocated)
	}

	// Baseline and extended JPEG are decodable with the standard library. The
	// Photometric Interpretation goes with it: it decides whether a
	// color-transformed frame should come back as RGB or stay in YBR.
	if compression == compress.JPEG {
		return compress.NewJPEGDecompressorForPhotometric(info.PhotometricInterpretation).
			Decompress(fragment)
	}

	// Everything else — JPEG-LS, JPEG 2000, JPEG Lossless — needs a decoder the
	// caller supplies. The built-in entry points for those are placeholders
	// that only describe the C library they would need.
	decoder, err := compress.GetExternalRegistry().GetExternalDecoder(compression)
	if err != nil {
		return nil, fmt.Errorf("no decoder available for %s; supply one with "+
			"compress.GetExternalRegistry().RegisterExternalDecoder: %w", compression, err)
	}
	return decoder.Decompress(fragment)
}

// PixelArrayBySample returns pixel data with color samples in their own
// dimension, matching the shape PixelDataShape reports.
//
// For multi-sample data the result is [frames][rows][columns][samples]; for
// single-sample data it is [frames][rows][columns], the same as PixelArray.
// The concrete type follows BitsAllocated as it does there.
//
// This exists because PixelArray flattens samples into the column dimension: a
// 100x100 RGB frame comes back from it as 100 rows of 300 values, R, G and B
// adjacent. Those values and their order are correct, and code that indexes
// them accordingly works — but the shape contradicts PixelDataShape, which has
// always reported four dimensions for color data. Changing PixelArray would
// break every caller that type-switches on [][][]uint8, so the honest shape is
// offered alongside rather than in place of it.
func (ds *Dataset) PixelArrayBySample() (interface{}, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	if info.SamplesPerPixel <= 1 {
		return ds.PixelArray()
	}

	pixelBytes, err := ds.pixelBytesForDecoding(info)
	if err != nil {
		return nil, err
	}

	switch info.BitsAllocated {
	case 8:
		return reshapeBySample(pixelBytes, info, func(b []byte) uint8 { return b[0] }, 1)
	case 16:
		return reshapeBySample16(pixelBytes, info)
	case 32:
		return reshapeBySample32(pixelBytes, info)
	default:
		return nil, fmt.Errorf("unsupported bit depth for per-sample access: %d", info.BitsAllocated)
	}
}

// reshapeBySample32 is the 32-bit form.
//
// PixelArray has handled 32 bits since it existed; only the per-sample accessor
// stopped at 16, so a 32-bit color image had no route to its channels at all.
func reshapeBySample32(data []byte, info *PixelDataInfo) ([][][][]uint32, error) {
	need := info.NumberOfFrames * info.Rows * info.Columns * info.SamplesPerPixel * 4
	if len(data) < need {
		return nil, fmt.Errorf("pixel data holds %d bytes, need %d for %d frames of %dx%d with %d samples",
			len(data), need, info.NumberOfFrames, info.Rows, info.Columns, info.SamplesPerPixel)
	}

	out := make([][][][]uint32, info.NumberOfFrames)
	offset := 0
	for f := range out {
		out[f] = make([][][]uint32, info.Rows)
		for r := range out[f] {
			out[f][r] = make([][]uint32, info.Columns)
			for c := range out[f][r] {
				samples := make([]uint32, info.SamplesPerPixel)
				for s := range samples {
					samples[s] = binary.LittleEndian.Uint32(data[offset:])
					offset += 4
				}
				out[f][r][c] = samples
			}
		}
	}
	return out, nil
}

// reshapeBySample splits 8-bit pixel data into [frames][rows][columns][samples].
func reshapeBySample(data []byte, info *PixelDataInfo, get func([]byte) uint8, width int) ([][][][]uint8, error) {
	need := info.NumberOfFrames * info.Rows * info.Columns * info.SamplesPerPixel * width
	if len(data) < need {
		return nil, fmt.Errorf("pixel data holds %d bytes, need %d for %d frames of %dx%d with %d samples",
			len(data), need, info.NumberOfFrames, info.Rows, info.Columns, info.SamplesPerPixel)
	}

	out := make([][][][]uint8, info.NumberOfFrames)
	offset := 0
	for f := range out {
		out[f] = make([][][]uint8, info.Rows)
		for r := range out[f] {
			out[f][r] = make([][]uint8, info.Columns)
			for c := range out[f][r] {
				samples := make([]uint8, info.SamplesPerPixel)
				for s := range samples {
					samples[s] = get(data[offset:])
					offset += width
				}
				out[f][r][c] = samples
			}
		}
	}
	return out, nil
}

// reshapeBySample16 splits 16-bit pixel data into [frames][rows][columns][samples].
func reshapeBySample16(data []byte, info *PixelDataInfo) ([][][][]uint16, error) {
	need := info.NumberOfFrames * info.Rows * info.Columns * info.SamplesPerPixel * 2
	if len(data) < need {
		return nil, fmt.Errorf("pixel data holds %d bytes, need %d for %d frames of %dx%d with %d samples",
			len(data), need, info.NumberOfFrames, info.Rows, info.Columns, info.SamplesPerPixel)
	}

	out := make([][][][]uint16, info.NumberOfFrames)
	offset := 0
	for f := range out {
		out[f] = make([][][]uint16, info.Rows)
		for r := range out[f] {
			out[f][r] = make([][]uint16, info.Columns)
			for c := range out[f][r] {
				samples := make([]uint16, info.SamplesPerPixel)
				for s := range samples {
					// Values reach here little endian: filereader normalizes
					// big endian files while parsing.
					samples[s] = binary.LittleEndian.Uint16(data[offset:])
					offset += 2
				}
				out[f][r][c] = samples
			}
		}
	}
	return out, nil
}
