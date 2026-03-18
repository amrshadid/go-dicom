package dataelem

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/waveforms"
)

// WaveformSequenceToGroups converts a waveform sequence data element to WaveformGroup structs.
//
// This method extracts waveform data from a DICOM Waveform Sequence (0x54000100).
// Each item in the sequence becomes one WaveformGroup.
//
// Parameters:
//   - byteOrder: The byte order for parsing binary data (binary.LittleEndian or binary.BigEndian)
//
// Returns:
//   - Slice of WaveformGroup structs
//   - Error if parsing fails
//
// Example:
//
//	elem := dataset.GetElement(0x54000100) // Waveform Sequence
//	groups, err := elem.WaveformSequenceToGroups(binary.LittleEndian)
func (de *DataElement) WaveformSequenceToGroups(byteOrder binary.ByteOrder) ([]*waveforms.WaveformGroup, error) {
	de.mu.RLock()
	defer de.mu.RUnlock()

	if de.VR != SQ {
		return nil, fmt.Errorf("element is not a sequence (VR=%s)", de.VR)
	}

	seq, ok := de.Value.(*sequence.Sequence)
	if !ok {
		return nil, fmt.Errorf("value is not a sequence")
	}

	items := seq.Items()
	if len(items) == 0 {
		return []*waveforms.WaveformGroup{}, nil
	}

	var groups []*waveforms.WaveformGroup

	for i, item := range items {
		// Each item should be a map of DataElements (representing a dataset item)
		itemMap, ok := item.(map[uint32]*DataElement)
		if !ok {
			return nil, fmt.Errorf("sequence item %d is not a valid dataset", i)
		}

		group, err := parseWaveformGroupFromItem(itemMap, byteOrder)
		if err != nil {
			return nil, fmt.Errorf("failed to parse waveform group %d: %w", i, err)
		}

		groups = append(groups, group)
	}

	return groups, nil
}

// parseWaveformGroupFromItem parses a single waveform group from a sequence item.
func parseWaveformGroupFromItem(itemMap map[uint32]*DataElement, byteOrder binary.ByteOrder) (*waveforms.WaveformGroup, error) {
	group := &waveforms.WaveformGroup{}

	// Extract Waveform Originality (0x54001004)
	if elem, ok := itemMap[0x54001004]; ok {
		if val, ok := elem.Value.(string); ok {
			group.WaveformOriginality = strings.TrimSpace(val)
		}
	}

	// Extract Number of Waveform Channels (0x54001006)
	if elem, ok := itemMap[0x54001006]; ok {
		if val, ok := elem.Value.(uint16); ok {
			group.NumberOfWaveformChannels = int(val)
		} else if val, ok := elem.Value.([]uint16); ok && len(val) > 0 {
			group.NumberOfWaveformChannels = int(val[0])
		}
	}

	// Extract Number of Waveform Samples (0x5400100A)
	if elem, ok := itemMap[0x5400100A]; ok {
		if val, ok := elem.Value.(uint32); ok {
			group.NumberOfSamples = int(val)
		} else if val, ok := elem.Value.([]uint32); ok && len(val) > 0 {
			group.NumberOfSamples = int(val[0])
		}
	}

	// Extract Sampling Frequency (0x54001010)
	if elem, ok := itemMap[0x54001010]; ok {
		if val, ok := elem.Value.(string); ok {
			if freq, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
				group.SamplingFrequency = freq
			}
		} else if val, ok := elem.Value.(float64); ok {
			group.SamplingFrequency = val
		}
	}

	// Extract Multiplex Group Label (0x54003008)
	if elem, ok := itemMap[0x54003008]; ok {
		if val, ok := elem.Value.(string); ok {
			group.MultiplexGroupLabel = strings.TrimSpace(val)
		}
	}

	// Extract Sample Interpretation (0x5400106A) - if present
	if elem, ok := itemMap[0x5400106A]; ok {
		if val, ok := elem.Value.(string); ok {
			group.SampleInterpretation = strings.TrimSpace(val)
		}
	}

	// Parse channel-specific data (labels, sources, units)
	group.ChannelLabel = make([]string, group.NumberOfWaveformChannels)
	group.ChannelSource = make([]string, group.NumberOfWaveformChannels)
	group.ChannelUnits = make([]string, group.NumberOfWaveformChannels)

	// Extract channel labels, sources, units from channel definition sequence (0x54003010)
	// This is simplified - in reality, these are in a nested sequence
	for i := 0; i < group.NumberOfWaveformChannels; i++ {
		group.ChannelLabel[i] = fmt.Sprintf("Channel %d", i+1)
		group.ChannelSource[i] = "Unknown"
		group.ChannelUnits[i] = "unknown"
	}

	// Extract Waveform Data (0x54003000)
	if elem, ok := itemMap[0x54003000]; ok {
		waveformData, err := parseWaveformData(elem, group.NumberOfWaveformChannels, group.NumberOfSamples, byteOrder)
		if err != nil {
			return nil, fmt.Errorf("failed to parse waveform data: %w", err)
		}
		group.WaveformData = waveformData
	}

	return group, nil
}

// parseWaveformData parses the raw waveform data bytes into channel data.
func parseWaveformData(elem *DataElement, numChannels int, numSamples int, byteOrder binary.ByteOrder) ([][]int16, error) {
	var rawData []byte

	switch v := elem.Value.(type) {
	case []byte:
		rawData = v
	case string:
		rawData = []byte(v)
	default:
		return nil, fmt.Errorf("waveform data has unexpected type: %T", elem.Value)
	}

	expectedBytes := numChannels * numSamples * 2 // 2 bytes per int16 sample
	if len(rawData) < expectedBytes {
		return nil, fmt.Errorf("insufficient waveform data: got %d bytes, expected at least %d", len(rawData), expectedBytes)
	}

	// Allocate channel data
	channelData := make([][]int16, numChannels)
	for i := range channelData {
		channelData[i] = make([]int16, numSamples)
	}

	// Parse multiplexed data (samples are interleaved: ch0_s0, ch1_s0, ch2_s0, ch0_s1, ch1_s1, ...)
	reader := bytes.NewReader(rawData)
	for sample := 0; sample < numSamples; sample++ {
		for ch := 0; ch < numChannels; ch++ {
			var value int16
			if err := binary.Read(reader, byteOrder, &value); err != nil {
				return nil, fmt.Errorf("failed to read sample %d channel %d: %w", sample, ch, err)
			}
			channelData[ch][sample] = value
		}
	}

	return channelData, nil
}

// WaveformGroupsToSequence converts WaveformGroup structs to a waveform sequence data element.
//
// This method creates a DICOM Waveform Sequence (0x54000100) from WaveformGroup structs.
//
// Parameters:
//   - groups: Slice of WaveformGroup structs
//   - byteOrder: The byte order for encoding binary data
//
// Returns:
//   - DataElement representing the waveform sequence
//   - Error if conversion fails
//
// Example:
//
//	groups := []*waveforms.WaveformGroup{wf1, wf2}
//	elem, err := dataelem.WaveformGroupsToSequence(groups, binary.LittleEndian)
//	dataset.AddElement(0x54000100, elem)
func WaveformGroupsToSequence(groups []*waveforms.WaveformGroup, byteOrder binary.ByteOrder) (*DataElement, error) {
	if len(groups) == 0 {
		return nil, fmt.Errorf("no waveform groups provided")
	}

	seq := sequence.New()

	for i, group := range groups {
		if group == nil {
			return nil, fmt.Errorf("waveform group %d is nil", i)
		}

		// Validate the waveform group
		if err := validateWaveformGroupForConversion(group, i); err != nil {
			return nil, err
		}

		item, err := waveformGroupToSequenceItem(group, byteOrder)
		if err != nil {
			return nil, fmt.Errorf("failed to convert waveform group %d: %w", i, err)
		}

		if err := seq.Append(item); err != nil {
			return nil, fmt.Errorf("failed to append sequence item %d: %w", i, err)
		}
	}

	elem := NewDataElement(uint32(0x54000100), SQ, seq)
	elem.Keyword = "WaveformSequence"
	elem.Description = "Waveform Sequence"
	return elem, nil
}

// waveformGroupToSequenceItem converts a single WaveformGroup to a sequence item.
func waveformGroupToSequenceItem(group *waveforms.WaveformGroup, byteOrder binary.ByteOrder) (map[uint32]*DataElement, error) {
	item := make(map[uint32]*DataElement)

	// Waveform Originality (0x54001004)
	if group.WaveformOriginality != "" {
		item[0x54001004] = NewDataElementWithKeyword(
			uint32(0x54001004),
			CS,
			group.WaveformOriginality,
			"WaveformOriginality",
			"Waveform Originality",
		)
	}

	// Number of Waveform Channels (0x54001006)
	item[0x54001006] = NewDataElementWithKeyword(
		uint32(0x54001006),
		US,
		[]uint16{uint16(group.NumberOfWaveformChannels)},
		"NumberOfWaveformChannels",
		"Number of Waveform Channels",
	)

	// Number of Waveform Samples (0x5400100A)
	item[0x5400100A] = NewDataElementWithKeyword(
		uint32(0x5400100A),
		UL,
		[]uint32{uint32(group.NumberOfSamples)},
		"NumberOfWaveformSamples",
		"Number of Waveform Samples",
	)

	// Sampling Frequency (0x54001010)
	freqStr := fmt.Sprintf("%.6f", group.SamplingFrequency)
	item[0x54001010] = NewDataElementWithKeyword(
		uint32(0x54001010),
		DS,
		freqStr,
		"SamplingFrequency",
		"Sampling Frequency",
	)

	// Multiplex Group Label (0x54003008)
	if group.MultiplexGroupLabel != "" {
		item[0x54003008] = NewDataElementWithKeyword(
			uint32(0x54003008),
			SH,
			group.MultiplexGroupLabel,
			"MultiplexGroupLabel",
			"Multiplex Group Label",
		)
	}

	// Waveform Data (0x54003000)
	waveformBytes, err := encodeWaveformData(group.WaveformData, group.NumberOfWaveformChannels, group.NumberOfSamples, byteOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to encode waveform data: %w", err)
	}

	item[0x54003000] = NewDataElementWithKeyword(
		uint32(0x54003000),
		OW, // Other Word (16-bit data)
		waveformBytes,
		"WaveformData",
		"Waveform Data",
	)

	return item, nil
}

// encodeWaveformData encodes channel data into multiplexed byte array.
func encodeWaveformData(channelData [][]int16, numChannels int, numSamples int, byteOrder binary.ByteOrder) ([]byte, error) {
	if len(channelData) != numChannels {
		return nil, fmt.Errorf("channel data length mismatch: got %d, expected %d", len(channelData), numChannels)
	}

	buf := new(bytes.Buffer)

	// Write multiplexed data (samples are interleaved)
	for sample := 0; sample < numSamples; sample++ {
		for ch := 0; ch < numChannels; ch++ {
			if sample >= len(channelData[ch]) {
				return nil, fmt.Errorf("channel %d has insufficient samples: need %d, got %d", ch, numSamples, len(channelData[ch]))
			}

			value := channelData[ch][sample]
			if err := binary.Write(buf, byteOrder, value); err != nil {
				return nil, fmt.Errorf("failed to write sample %d channel %d: %w", sample, ch, err)
			}
		}
	}

	return buf.Bytes(), nil
}

// IsWaveformSequence checks if the data element is a waveform sequence.
func (de *DataElement) IsWaveformSequence() bool {
	de.mu.RLock()
	defer de.mu.RUnlock()

	// Check if tag is 0x54000100 (Waveform Sequence)
	switch t := de.tag.(type) {
	case uint32:
		return t == 0x54000100
	case int:
		return uint32(t) == 0x54000100
	case string:
		// Handle string tag representations
		return t == "54000100" || t == "(5400,0100)"
	default:
		return false
	}
}

// GetWaveformTimestamp extracts the timestamp from waveform metadata if available.
func GetWaveformTimestamp(group *waveforms.WaveformGroup) time.Time {
	if !group.TimeStamp.IsZero() {
		return group.TimeStamp
	}
	return time.Now()
}

// validateWaveformGroupForConversion validates a waveform group before conversion.
func validateWaveformGroupForConversion(group *waveforms.WaveformGroup, index int) error {
	if group.NumberOfWaveformChannels <= 0 {
		return fmt.Errorf("waveform group %d: number of channels must be positive (got %d)", index, group.NumberOfWaveformChannels)
	}

	if group.NumberOfSamples <= 0 {
		return fmt.Errorf("waveform group %d: number of samples must be positive (got %d)", index, group.NumberOfSamples)
	}

	if group.SamplingFrequency <= 0 {
		return fmt.Errorf("waveform group %d: sampling frequency must be positive (got %.2f)", index, group.SamplingFrequency)
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
