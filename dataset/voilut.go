package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/tag"
)

// GetVOILUTSequence retrieves the VOI LUT Sequence from the dataset.
// Returns the LUT parameters if present, nil otherwise.
func (ds *Dataset) GetVOILUTSequence() (*VOILUTParameters, error) {
	// VOI LUT Sequence tag is (0028,3010)
	voiLUTSeqTag := tag.New(0x0028, 0x3010)

	if !ds.HasSequence(voiLUTSeqTag) {
		return nil, fmt.Errorf("VOI LUT sequence not found")
	}

	seq, err := ds.GetSequence(voiLUTSeqTag)
	if err != nil {
		return nil, err
	}

	if seq.Length() == 0 {
		return nil, fmt.Errorf("VOI LUT sequence is empty")
	}

	// Get first item in sequence
	item, err := seq.Get(0)
	if err != nil {
		return nil, err
	}

	itemDS, ok := item.(*Dataset)
	if !ok {
		return nil, fmt.Errorf("VOI LUT sequence item is not a dataset")
	}

	// Parse LUT Descriptor (0028,3002) - required
	descriptorTag := tag.New(0x0028, 0x3002)
	descriptorBytes := itemDS.GetValue(descriptorTag)
	if descriptorBytes == nil {
		return nil, fmt.Errorf("LUT descriptor not found")
	}

	var descriptor [3]uint16
	if len(descriptorBytes) < 6 {
		return nil, fmt.Errorf("LUT descriptor too short")
	}
	descriptor[0] = binary.LittleEndian.Uint16(descriptorBytes[0:2]) // Number of entries
	descriptor[1] = binary.LittleEndian.Uint16(descriptorBytes[2:4]) // First mapped value
	descriptor[2] = binary.LittleEndian.Uint16(descriptorBytes[4:6]) // Bits per entry

	// Parse LUT Data (0028,3006) - required
	dataTag := tag.New(0x0028, 0x3006)
	dataBytes := itemDS.GetValue(dataTag)
	if dataBytes == nil {
		return nil, fmt.Errorf("LUT data not found")
	}

	// Convert bytes to uint16 array
	numEntries := int(descriptor[0])
	if numEntries == 0 {
		numEntries = 65536 // Special case: 0 means 65536
	}

	lutData := make([]uint16, numEntries)
	for i := 0; i < numEntries && i*2+1 < len(dataBytes); i++ {
		lutData[i] = binary.LittleEndian.Uint16(dataBytes[i*2 : i*2+2])
	}

	// Parse LUT Explanation (0028,3003) - optional
	explanationTag := tag.New(0x0028, 0x3003)
	explanationBytes := itemDS.GetValue(explanationTag)
	explanation := ""
	if explanationBytes != nil {
		explanation = string(explanationBytes)
	}

	return &VOILUTParameters{
		LUTData:        lutData,
		LUTDescriptor:  descriptor,
		LUTExplanation: explanation,
	}, nil
}

// ApplyVOILUTToPixelData applies VOI LUT to pixel data.
// Falls back to windowing if VOI LUT is not present.
func (ds *Dataset) ApplyVOILUTToPixelData() (interface{}, error) {
	// Try to get VOI LUT
	lut, err := ds.GetVOILUTSequence()
	if err != nil {
		// No VOI LUT, try windowing instead
		params := ds.GetWindowingParameters()
		return ds.ApplyWindowing(params.Center, params.Width)
	}

	// Get pixel data
	pixelData, err := ds.PixelArray()
	if err != nil {
		return nil, fmt.Errorf("failed to get pixel data: %w", err)
	}

	// Apply LUT based on pixel data type
	switch arr := pixelData.(type) {
	case [][][]uint8:
		return applyLUTToUint8(arr, lut)
	case [][][]uint16:
		return applyLUTToUint16(arr, lut)
	case [][][]int16:
		return applyLUTToInt16(arr, lut)
	case [][][]uint32:
		return applyLUTToUint32(arr, lut)
	default:
		return nil, fmt.Errorf("unsupported pixel data type for VOI LUT application")
	}
}

// applyLUTToUint8 applies LUT to 8-bit unsigned pixel data.
func applyLUTToUint8(pixelData [][][]uint8, lut *VOILUTParameters) ([][][]uint8, error) {
	frames := len(pixelData)
	if frames == 0 {
		return nil, fmt.Errorf("no pixel data frames")
	}

	rows := len(pixelData[0])
	cols := len(pixelData[0][0])

	result := make([][][]uint8, frames)
	for f := range result {
		result[f] = make([][]uint8, rows)
		for r := range result[f] {
			result[f][r] = make([]uint8, cols)
			for c := range result[f][r] {
				pixelValue := int(pixelData[f][r][c])
				result[f][r][c] = uint8(applyLUT(pixelValue, lut))
			}
		}
	}

	return result, nil
}

// applyLUTToUint16 applies LUT to 16-bit unsigned pixel data.
func applyLUTToUint16(pixelData [][][]uint16, lut *VOILUTParameters) ([][][]uint16, error) {
	frames := len(pixelData)
	if frames == 0 {
		return nil, fmt.Errorf("no pixel data frames")
	}

	rows := len(pixelData[0])
	cols := len(pixelData[0][0])

	result := make([][][]uint16, frames)
	for f := range result {
		result[f] = make([][]uint16, rows)
		for r := range result[f] {
			result[f][r] = make([]uint16, cols)
			for c := range result[f][r] {
				pixelValue := int(pixelData[f][r][c])
				result[f][r][c] = uint16(applyLUT(pixelValue, lut))
			}
		}
	}

	return result, nil
}

// applyLUTToInt16 applies LUT to 16-bit signed pixel data.
func applyLUTToInt16(pixelData [][][]int16, lut *VOILUTParameters) ([][][]int16, error) {
	frames := len(pixelData)
	if frames == 0 {
		return nil, fmt.Errorf("no pixel data frames")
	}

	rows := len(pixelData[0])
	cols := len(pixelData[0][0])

	result := make([][][]int16, frames)
	for f := range result {
		result[f] = make([][]int16, rows)
		for r := range result[f] {
			result[f][r] = make([]int16, cols)
			for c := range result[f][r] {
				pixelValue := int(pixelData[f][r][c])
				result[f][r][c] = int16(applyLUT(pixelValue, lut))
			}
		}
	}

	return result, nil
}

// applyLUTToUint32 applies LUT to 32-bit unsigned pixel data.
func applyLUTToUint32(pixelData [][][]uint32, lut *VOILUTParameters) ([][][]uint32, error) {
	frames := len(pixelData)
	if frames == 0 {
		return nil, fmt.Errorf("no pixel data frames")
	}

	rows := len(pixelData[0])
	cols := len(pixelData[0][0])

	result := make([][][]uint32, frames)
	for f := range result {
		result[f] = make([][]uint32, rows)
		for r := range result[f] {
			result[f][r] = make([]uint32, cols)
			for c := range result[f][r] {
				pixelValue := int(pixelData[f][r][c])
				result[f][r][c] = uint32(applyLUT(pixelValue, lut))
			}
		}
	}

	return result, nil
}

// applyLUT applies a single LUT lookup.
func applyLUT(pixelValue int, lut *VOILUTParameters) int {
	firstMapped := int(lut.LUTDescriptor[1])
	numEntries := int(lut.LUTDescriptor[0])
	if numEntries == 0 {
		numEntries = 65536
	}

	// Calculate index into LUT
	index := pixelValue - firstMapped

	// Clamp to LUT range
	if index < 0 {
		index = 0
	} else if index >= numEntries {
		index = numEntries - 1
	}

	// Return mapped value
	return int(lut.LUTData[index])
}

// HasVOILUT checks if the dataset contains a VOI LUT Sequence.
func (ds *Dataset) HasVOILUT() bool {
	return ds.HasSequence(tag.New(0x0028, 0x3010))
}

// GetModalityLUT retrieves the Modality LUT Sequence.
// Modality LUT is different from VOI LUT - it's applied first for rescaling.
func (ds *Dataset) GetModalityLUT() (*VOILUTParameters, error) {
	// Modality LUT Sequence tag is (0028,3000)
	modalityLUTSeqTag := tag.New(0x0028, 0x3000)

	if !ds.HasSequence(modalityLUTSeqTag) {
		return nil, fmt.Errorf("modality LUT sequence not found")
	}

	seq, err := ds.GetSequence(modalityLUTSeqTag)
	if err != nil {
		return nil, err
	}

	if seq.Length() == 0 {
		return nil, fmt.Errorf("modality LUT sequence is empty")
	}

	// Get first item
	item, err := seq.Get(0)
	if err != nil {
		return nil, err
	}

	itemDS, ok := item.(*Dataset)
	if !ok {
		return nil, fmt.Errorf("modality LUT sequence item is not a dataset")
	}

	// Parse LUT Descriptor (0028,3002)
	descriptorTag := tag.New(0x0028, 0x3002)
	descriptorBytes := itemDS.GetValue(descriptorTag)
	if descriptorBytes == nil {
		return nil, fmt.Errorf("LUT descriptor not found")
	}

	var descriptor [3]uint16
	if len(descriptorBytes) < 6 {
		return nil, fmt.Errorf("LUT descriptor too short")
	}
	descriptor[0] = binary.LittleEndian.Uint16(descriptorBytes[0:2])
	descriptor[1] = binary.LittleEndian.Uint16(descriptorBytes[2:4])
	descriptor[2] = binary.LittleEndian.Uint16(descriptorBytes[4:6])

	// Parse LUT Data (0028,3006)
	dataTag := tag.New(0x0028, 0x3006)
	dataBytes := itemDS.GetValue(dataTag)
	if dataBytes == nil {
		return nil, fmt.Errorf("LUT data not found")
	}

	numEntries := int(descriptor[0])
	if numEntries == 0 {
		numEntries = 65536
	}

	lutData := make([]uint16, numEntries)
	for i := 0; i < numEntries && i*2+1 < len(dataBytes); i++ {
		lutData[i] = binary.LittleEndian.Uint16(dataBytes[i*2 : i*2+2])
	}

	// Parse explanation
	explanationTag := tag.New(0x0028, 0x3003)
	explanationBytes := itemDS.GetValue(explanationTag)
	explanation := ""
	if explanationBytes != nil {
		explanation = string(explanationBytes)
	}

	return &VOILUTParameters{
		LUTData:        lutData,
		LUTDescriptor:  descriptor,
		LUTExplanation: explanation,
	}, nil
}

// HasModalityLUT checks if the dataset contains a Modality LUT Sequence.
func (ds *Dataset) HasModalityLUT() bool {
	return ds.HasSequence(tag.New(0x0028, 0x3000))
}

// ApplyPresentationLUT applies the full presentation pipeline:
// 1. Modality LUT (or rescale slope/intercept)
// 2. VOI LUT (or windowing)
func (ds *Dataset) ApplyPresentationLUT() (interface{}, error) {
	// Step 1: Apply Modality LUT or rescale
	pixelData, err := ds.PixelArray()
	if err != nil {
		return nil, fmt.Errorf("failed to get pixel data: %w", err)
	}

	// Check for Modality LUT
	if ds.HasModalityLUT() {
		modalityLUT, err := ds.GetModalityLUT()
		if err == nil {
			// Apply Modality LUT
			pixelData, err = ds.applyLUTToData(pixelData, modalityLUT)
			if err != nil {
				return nil, fmt.Errorf("failed to apply Modality LUT: %w", err)
			}
		}
	} else {
		// Apply rescale slope/intercept if present
		info, err := ds.GetPixelDataInfo()
		if err == nil && (info.RescaleSlope != 1.0 || info.RescaleIntercept != 0.0) { //nolint:staticcheck // SA9003
			// Rescale applied during windowing
		}
	}

	// Step 2: Apply VOI LUT or windowing
	if ds.HasVOILUT() {
		voiLUT, err := ds.GetVOILUTSequence()
		if err == nil {
			return ds.applyLUTToData(pixelData, voiLUT)
		}
	}

	// Fall back to windowing
	params := ds.GetWindowingParameters()
	return ds.ApplyWindowing(params.Center, params.Width)
}

// applyLUTToData applies LUT to any pixel data type.
func (ds *Dataset) applyLUTToData(pixelData interface{}, lut *VOILUTParameters) (interface{}, error) {
	switch arr := pixelData.(type) {
	case [][][]uint8:
		return applyLUTToUint8(arr, lut)
	case [][][]uint16:
		return applyLUTToUint16(arr, lut)
	case [][][]int16:
		return applyLUTToInt16(arr, lut)
	case [][][]uint32:
		return applyLUTToUint32(arr, lut)
	default:
		return nil, fmt.Errorf("unsupported pixel data type")
	}
}
