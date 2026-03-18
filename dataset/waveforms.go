package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/waveforms"
)

// WaveformSequenceTag is the DICOM tag (5400,0100) that contains waveform data.
const WaveformSequenceTag = tag.Tag(0x54000100)

// GetWaveforms extracts waveform groups from the dataset's Waveform Sequence (5400,0100).
// Returns a slice of WaveformGroup structs containing multi-channel signal data, sampling rates, and metadata.
// Returns empty slice if no waveforms are present.
func (ds *Dataset) GetWaveforms(byteOrder binary.ByteOrder) ([]*waveforms.WaveformGroup, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	elem, exists := ds.elements[uint32(WaveformSequenceTag)]
	if !exists {
		return []*waveforms.WaveformGroup{}, nil
	}

	return elem.WaveformSequenceToGroups(byteOrder)
}

// AddWaveforms adds waveform groups to the dataset as a Waveform Sequence (5400,0100).
// Creates or replaces the waveform sequence element; each group becomes a sequence item.
func (ds *Dataset) AddWaveforms(groups []*waveforms.WaveformGroup, byteOrder binary.ByteOrder) error {
	if len(groups) == 0 {
		return fmt.Errorf("no waveform groups provided")
	}

	// Convert waveform groups to sequence element
	elem, err := dataelem.WaveformGroupsToSequence(groups, byteOrder)
	if err != nil {
		return fmt.Errorf("failed to create waveform sequence: %w", err)
	}

	// Add to dataset
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.elements[uint32(WaveformSequenceTag)] = elem
	return nil
}

// HasWaveforms checks if the dataset contains waveform data.
//
// Returns true if the Waveform Sequence (5400,0100) tag is present.
//
// Example:
//
//	if ds.HasWaveforms() {
//	    waveforms, _ := ds.GetWaveforms(binary.LittleEndian)
//	    fmt.Printf("Found %d waveform groups\n", len(waveforms))
//	}
func (ds *Dataset) HasWaveforms() bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	_, exists := ds.elements[uint32(WaveformSequenceTag)]
	return exists
}

// RemoveWaveforms removes the Waveform Sequence (5400,0100) from the dataset.
//
// Returns true if waveforms were present and removed, false otherwise.
//
// Example:
//
//	if ds.RemoveWaveforms() {
//	    fmt.Println("Waveforms removed")
//	}
func (ds *Dataset) RemoveWaveforms() bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if _, exists := ds.elements[uint32(WaveformSequenceTag)]; exists {
		delete(ds.elements, uint32(WaveformSequenceTag))
		return true
	}
	return false
}

// GetWaveformCount returns the number of waveform groups in the dataset.
//
// Returns 0 if no waveforms are present or if parsing fails.
//
// Example:
//
//	count := ds.GetWaveformCount(binary.LittleEndian)
//	fmt.Printf("Dataset contains %d waveform groups\n", count)
func (ds *Dataset) GetWaveformCount(byteOrder binary.ByteOrder) int {
	groups, err := ds.GetWaveforms(byteOrder)
	if err != nil {
		return 0
	}
	return len(groups)
}

// ValidateWaveforms validates that waveform data in the dataset is properly structured.
//
// Checks:
//   - Waveform Sequence tag exists
//   - Each group has valid channel count and sample count
//   - Each group has matching data dimensions
//
// Returns:
//   - Error describing the validation failure, or nil if valid
//
// Example:
//
//	if err := ds.ValidateWaveforms(binary.LittleEndian); err != nil {
//	    log.Printf("Waveform validation failed: %v", err)
//	}
func (ds *Dataset) ValidateWaveforms(byteOrder binary.ByteOrder) error {
	if !ds.HasWaveforms() {
		return fmt.Errorf("no waveforms present in dataset")
	}

	groups, err := ds.GetWaveforms(byteOrder)
	if err != nil {
		return fmt.Errorf("failed to extract waveforms: %w", err)
	}

	if len(groups) == 0 {
		return fmt.Errorf("waveform sequence is empty")
	}

	for i, group := range groups {
		if err := validateWaveformGroup(group, i); err != nil {
			return err
		}
	}

	return nil
}

// validateWaveformGroup validates a single waveform group.
func validateWaveformGroup(group *waveforms.WaveformGroup, index int) error {
	if group == nil {
		return fmt.Errorf("waveform group %d is nil", index)
	}

	if group.NumberOfWaveformChannels <= 0 {
		return fmt.Errorf("waveform group %d has invalid channel count: %d", index, group.NumberOfWaveformChannels)
	}

	if group.NumberOfSamples <= 0 {
		return fmt.Errorf("waveform group %d has invalid sample count: %d", index, group.NumberOfSamples)
	}

	if group.SamplingFrequency <= 0 {
		return fmt.Errorf("waveform group %d has invalid sampling frequency: %.2f", index, group.SamplingFrequency)
	}

	if len(group.WaveformData) != group.NumberOfWaveformChannels {
		return fmt.Errorf("waveform group %d: channel data count (%d) doesn't match declared channels (%d)",
			index, len(group.WaveformData), group.NumberOfWaveformChannels)
	}

	for ch, data := range group.WaveformData {
		if len(data) != group.NumberOfSamples {
			return fmt.Errorf("waveform group %d channel %d: sample count (%d) doesn't match declared samples (%d)",
				index, ch, len(data), group.NumberOfSamples)
		}
	}

	return nil
}

// GetWaveformSummary returns a human-readable summary of waveforms in the dataset.
//
// Example:
//
//	summary := ds.GetWaveformSummary(binary.LittleEndian)
//	fmt.Println(summary)
//	// Output: Dataset contains 2 waveform groups:
//	//   Group 0: 12 channels, 5000 samples at 500.0 Hz
//	//   Group 1: 8 channels, 10000 samples at 1000.0 Hz
func (ds *Dataset) GetWaveformSummary(byteOrder binary.ByteOrder) string {
	if !ds.HasWaveforms() {
		return "No waveforms in dataset"
	}

	groups, err := ds.GetWaveforms(byteOrder)
	if err != nil {
		return fmt.Sprintf("Error reading waveforms: %v", err)
	}

	if len(groups) == 0 {
		return "Waveform sequence is empty"
	}

	summary := fmt.Sprintf("Dataset contains %d waveform group(s):\n", len(groups))
	for i, group := range groups {
		summary += fmt.Sprintf("  Group %d: %d channels, %d samples at %.1f Hz",
			i, group.NumberOfWaveformChannels, group.NumberOfSamples, group.SamplingFrequency)
		if group.MultiplexGroupLabel != "" {
			summary += fmt.Sprintf(" [%s]", group.MultiplexGroupLabel)
		}
		if i < len(groups)-1 {
			summary += "\n"
		}
	}

	return summary
}
