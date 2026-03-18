package filereader

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/waveforms"
)

// WaveformSequenceTag is the DICOM tag (5400,0100) that contains waveform data.
const WaveformSequenceTag = tag.Tag(0x54000100)

// GetWaveforms extracts waveform groups from the DICOM file.
func (df *DICOMFile) GetWaveforms() ([]*waveforms.WaveformGroup, error) {
	var byteOrder binary.ByteOrder
	if df.IsLittleEndian {
		byteOrder = binary.LittleEndian
	} else {
		byteOrder = binary.BigEndian
	}

	for _, elem := range df.DataElements {
		if elem.Tag == WaveformSequenceTag {
			de, err := df.convertToDataElement(elem)
			if err != nil {
				return nil, fmt.Errorf("failed to convert waveform sequence: %w", err)
			}

			return de.WaveformSequenceToGroups(byteOrder)
		}
	}

	return []*waveforms.WaveformGroup{}, nil
}

// HasWaveforms checks if the DICOM file contains waveform data.
// Waveforms are stored in the Waveform Sequence (tag 5400,0100).
func (df *DICOMFile) HasWaveforms() bool {
	for _, elem := range df.DataElements {
		if elem.Tag == WaveformSequenceTag {
			return true
		}
	}
	return false
}

// GetWaveformCount returns the number of waveform groups in the file.
func (df *DICOMFile) GetWaveformCount() int {
	waveforms, err := df.GetWaveforms()
	if err != nil {
		return 0
	}
	return len(waveforms)
}

// GetWaveformSummary returns a human-readable summary of waveforms in the file.
func (df *DICOMFile) GetWaveformSummary() string {
	if !df.HasWaveforms() {
		return "No waveforms in file"
	}

	groups, err := df.GetWaveforms()
	if err != nil {
		return fmt.Sprintf("Error reading waveforms: %v", err)
	}

	if len(groups) == 0 {
		return "Waveform sequence is empty"
	}

	summary := fmt.Sprintf("File contains %d waveform group(s):\n", len(groups))
	for i, group := range groups {
		summary += fmt.Sprintf("  Group %d: %d channels, %d samples at %.1f Hz",
			i, group.NumberOfWaveformChannels, group.NumberOfSamples, group.SamplingFrequency)
		if group.MultiplexGroupLabel != "" {
			summary += fmt.Sprintf(" [%s]", group.MultiplexGroupLabel)
		}
		if group.WaveformOriginality != "" {
			summary += fmt.Sprintf(" (%s)", group.WaveformOriginality)
		}
		if i < len(groups)-1 {
			summary += "\n"
		}
	}

	return summary
}

// convertToDataElement converts a DataElementValue to dataelem.DataElement.
// This is used internally when accessing waveform sequence data.
func (df *DICOMFile) convertToDataElement(dev *DataElementValue) (*dataelem.DataElement, error) {
	if dev == nil {
		return nil, fmt.Errorf("data element value is nil")
	}

	if dev.VR == "SQ" {
		seq, err := df.parseSequenceValue(dev)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sequence: %w", err)
		}

		de := dataelem.NewDataElement(uint32(dev.Tag), dataelem.SQ, seq)
		return de, nil
	}

	vrType := dataelem.VR(dev.VR)
	de := dataelem.NewDataElement(uint32(dev.Tag), vrType, dev.Value)
	return de, nil
}

// parseSequenceValue parses a sequence from a DataElementValue.
// Currently, this returns an empty sequence as a placeholder for future implementation.
func (df *DICOMFile) parseSequenceValue(dev *DataElementValue) (*sequence.Sequence, error) {
	if dev.VR != "SQ" {
		return nil, fmt.Errorf("element is not a sequence")
	}

	seq := sequence.New()
	return seq, nil
}
