package filewriter

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/waveforms"
)

// WaveformSequenceTag is the DICOM tag (5400,0100) that contains waveform data.
const WaveformSequenceTag = tag.Tag(0x54000100)

// DICOMFileWriterWithWaveforms extends DICOMFileWriter with waveform writing support.
type DICOMFileWriterWithWaveforms struct {
	*DICOMFileWriter
	waveformGroups []*waveforms.WaveformGroup
}

// NewDICOMFileWriterWithWaveforms creates a new DICOM file writer with waveform support.
//
// Parameters:
//   - dfw: An existing DICOMFileWriter
//
// Example:
//
//	baseWriter := filewriter.NewDICOMFileWriter(filebaseWriter)
//	writer := filewriter.NewDICOMFileWriterWithWaveforms(baseWriter)
func NewDICOMFileWriterWithWaveforms(dfw *DICOMFileWriter) *DICOMFileWriterWithWaveforms {
	if dfw == nil {
		return nil
	}

	return &DICOMFileWriterWithWaveforms{
		DICOMFileWriter: dfw,
		waveformGroups:  make([]*waveforms.WaveformGroup, 0),
	}
}

// AddWaveform adds a waveform group to be written to the DICOM file.
//
// Waveform groups will be written as a Waveform Sequence (5400,0100) when
// WriteDataElements is called.
//
// Parameters:
//   - wf: The WaveformGroup to add
//
// Returns:
//   - Error if the waveform is invalid
//
// Example:
//
//	wf := &waveforms.WaveformGroup{
//	    NumberOfWaveformChannels: 2,
//	    NumberOfSamples:          1000,
//	    SamplingFrequency:        500.0,
//	    WaveformOriginality:      "ORIGINAL",
//	    WaveformData:             [][]int16{{...}, {...}},
//	}
//	err := writer.AddWaveform(wf)
func (dfww *DICOMFileWriterWithWaveforms) AddWaveform(wf *waveforms.WaveformGroup) error {
	if wf == nil {
		return fmt.Errorf("waveform group is nil")
	}

	if err := validateWaveformGroup(wf); err != nil {
		return fmt.Errorf("invalid waveform group: %w", err)
	}

	dfww.waveformGroups = append(dfww.waveformGroups, wf)
	return nil
}

// AddWaveforms adds multiple waveform groups at once.
//
// Parameters:
//   - groups: Slice of WaveformGroup structs to add
//
// Returns:
//   - Error if any waveform is invalid
//
// Example:
//
//	groups := []*waveforms.WaveformGroup{wf1, wf2}
//	err := writer.AddWaveforms(groups)
func (dfww *DICOMFileWriterWithWaveforms) AddWaveforms(groups []*waveforms.WaveformGroup) error {
	if len(groups) == 0 {
		return fmt.Errorf("no waveform groups provided")
	}

	for i, wf := range groups {
		if err := dfww.AddWaveform(wf); err != nil {
			return fmt.Errorf("failed to add waveform group %d: %w", i, err)
		}
	}

	return nil
}

// GetWaveforms returns all waveform groups that will be written.
func (dfww *DICOMFileWriterWithWaveforms) GetWaveforms() []*waveforms.WaveformGroup {
	return dfww.waveformGroups
}

// ClearWaveforms removes all waveform groups.
func (dfww *DICOMFileWriterWithWaveforms) ClearWaveforms() {
	dfww.waveformGroups = make([]*waveforms.WaveformGroup, 0)
}

// HasWaveforms returns true if any waveform groups have been added.
func (dfww *DICOMFileWriterWithWaveforms) HasWaveforms() bool {
	return len(dfww.waveformGroups) > 0
}

// WriteWaveformSequence writes the waveform sequence element to the file.
//
// This should be called as part of writing the dataset. It creates the
// Waveform Sequence (5400,0100) tag and writes all waveform groups.
//
// Returns:
//   - Error if writing fails
//
// Example:
//
//	writer.AddWaveforms(groups)
//	err := writer.WriteWaveformSequence()
func (dfww *DICOMFileWriterWithWaveforms) WriteWaveformSequence() error {
	if len(dfww.waveformGroups) == 0 {
		return nil // Nothing to write
	}

	// Determine byte order
	// Note: DICOMFileWriter doesn't expose byte order directly
	// Default to little-endian which is most common
	var byteOrder binary.ByteOrder = binary.LittleEndian

	// Convert waveform groups to sequence element
	seqElem, err := dataelem.WaveformGroupsToSequence(dfww.waveformGroups, byteOrder)
	if err != nil {
		return fmt.Errorf("failed to create waveform sequence: %w", err)
	}

	// Convert to writer's DataElement format
	writerElem, err := convertDataElementToWriterFormat(seqElem)
	if err != nil {
		return fmt.Errorf("failed to convert waveform sequence: %w", err)
	}

	// Add the element to the dataset
	if err := dfww.AddDataElement(writerElem); err != nil {
		return fmt.Errorf("failed to add waveform sequence: %w", err)
	}

	return nil
}

// validateWaveformGroup validates a waveform group before writing.
func validateWaveformGroup(wf *waveforms.WaveformGroup) error {
	if wf.NumberOfWaveformChannels <= 0 {
		return fmt.Errorf("number of channels must be positive")
	}

	if wf.NumberOfSamples <= 0 {
		return fmt.Errorf("number of samples must be positive")
	}

	if wf.SamplingFrequency <= 0 {
		return fmt.Errorf("sampling frequency must be positive")
	}

	if len(wf.WaveformData) != wf.NumberOfWaveformChannels {
		return fmt.Errorf("waveform data channels (%d) doesn't match declared channels (%d)",
			len(wf.WaveformData), wf.NumberOfWaveformChannels)
	}

	for ch, data := range wf.WaveformData {
		if len(data) != wf.NumberOfSamples {
			return fmt.Errorf("channel %d has %d samples, expected %d",
				ch, len(data), wf.NumberOfSamples)
		}
	}

	return nil
}

// convertDataElementToWriterFormat converts a dataelem.DataElement to filewriter.DataElement.
// This is needed because dataelem and filewriter use different element types.
func convertDataElementToWriterFormat(de *dataelem.DataElement) (*DataElement, error) {
	if de == nil {
		return nil, fmt.Errorf("data element is nil")
	}

	// Get tag. Tag understands every form NewDataElement's untyped parameter
	// allows, so this no longer has to enumerate them and fall over on the next
	// one somebody uses.
	tagValue, ok := de.Tag()
	if !ok {
		return nil, fmt.Errorf("unsupported tag type: %T", de.GetTag())
	}

	// For sequences, serialize items with Item/Item Delimiter/Sequence Delimiter tags.
	// Uses undefined length encoding (0xFFFFFFFF) for the sequence and each item.
	if de.GetVR() == dataelem.SQ {
		seqValue := de.GetValue()
		if seqValue == nil {
			// Empty sequence: just write sequence tag with zero length
			return &DataElement{
				Tag:    tagValue,
				VR:     "SQ",
				Value:  nil,
				Length: 0,
			}, nil
		}

		var seqBytes []byte

		// The sequence value is a *sequence.Sequence containing items.
		// Each item is a map[uint32]*dataelem.DataElement.
		// Try to iterate over the sequence items via reflection-free approach.
		// The sequence value from WaveformGroupsToSequence is *sequence.Sequence
		// whose Items() returns []Item ([]interface{}).
		switch sq := seqValue.(type) {
		default:
			return nil, fmt.Errorf("unsupported sequence value type: %T", sq)
		case interface{ Items() []interface{} }:
			items := sq.Items()
			for _, rawItem := range items {
				itemMap, ok := rawItem.(map[uint32]*dataelem.DataElement)
				if !ok {
					return nil, fmt.Errorf("sequence item has unexpected type: %T", rawItem)
				}

				// Collect serialized bytes for this item's data elements
				var itemDataBytes []byte
				for _, childDE := range itemMap {
					childWriter, err := convertDataElementToWriterFormat(childDE)
					if err != nil {
						return nil, fmt.Errorf("failed to convert sequence child element: %w", err)
					}

					// Serialize child element: Tag (4) + VR (2) + Length (2 or 6) + Value
					elemBytes := serializeDataElement(childWriter)
					itemDataBytes = append(itemDataBytes, elemBytes...)
				}

				// Write Item tag (FFFE,E000) with undefined length
				itemTag := []byte{0xFE, 0xFF, 0x00, 0xE0}
				itemLength := []byte{0xFF, 0xFF, 0xFF, 0xFF} // undefined length
				seqBytes = append(seqBytes, itemTag...)
				seqBytes = append(seqBytes, itemLength...)

				// Write item data elements
				seqBytes = append(seqBytes, itemDataBytes...)

				// Write Item Delimitation tag (FFFE,E00D) with zero length
				itemDelimTag := []byte{0xFE, 0xFF, 0x0D, 0xE0}
				itemDelimLength := []byte{0x00, 0x00, 0x00, 0x00}
				seqBytes = append(seqBytes, itemDelimTag...)
				seqBytes = append(seqBytes, itemDelimLength...)
			}
		}

		// Write Sequence Delimitation tag (FFFE,E0DD) with zero length
		seqDelimTag := []byte{0xFE, 0xFF, 0xDD, 0xE0}
		seqDelimLength := []byte{0x00, 0x00, 0x00, 0x00}
		seqBytes = append(seqBytes, seqDelimTag...)
		seqBytes = append(seqBytes, seqDelimLength...)

		return &DataElement{
			Tag:    tagValue,
			VR:     "SQ",
			Value:  seqBytes,
			Length: 0xFFFFFFFF, // undefined length
		}, nil
	}

	// Get value as bytes
	var valueBytes []byte
	switch v := de.GetValue().(type) {
	case []byte:
		valueBytes = v
	case string:
		valueBytes = []byte(v)
	default:
		return nil, fmt.Errorf("unsupported value type for serialization: %T", de.GetValue())
	}

	writerElem := &DataElement{
		Tag:    tagValue,
		VR:     string(de.GetVR()),
		Value:  valueBytes,
		Length: uint32(len(valueBytes)),
	}

	return writerElem, nil
}

// serializeDataElement serializes a filewriter.DataElement to raw bytes
// using explicit VR little-endian encoding.
func serializeDataElement(elem *DataElement) []byte {
	var buf []byte

	// Tag: group (2 bytes LE) + element (2 bytes LE)
	tagVal := uint32(elem.Tag)
	group := uint16(tagVal >> 16)
	element := uint16(tagVal & 0xFFFF)
	buf = append(buf, byte(group), byte(group>>8))
	buf = append(buf, byte(element), byte(element>>8))

	// VR (2 bytes)
	buf = append(buf, []byte(elem.VR)...)

	if isShortVR(elem.VR) {
		// Short VR: 2-byte length
		length := uint16(elem.Length)
		buf = append(buf, byte(length), byte(length>>8))
	} else {
		// Long VR: 2 reserved bytes + 4-byte length
		buf = append(buf, 0x00, 0x00)
		buf = append(buf, byte(elem.Length), byte(elem.Length>>8), byte(elem.Length>>16), byte(elem.Length>>24))
	}

	// Value bytes
	if elem.Length > 0 && elem.Value != nil {
		buf = append(buf, elem.Value...)
	}

	return buf
}

// GetWaveformSummary returns a human-readable summary of waveforms to be written.
//
// Example:
//
//	writer.AddWaveforms(groups)
//	summary := writer.GetWaveformSummary()
//	fmt.Println(summary)
//	// Output: 2 waveform group(s) will be written:
//	//   Group 0: 12 channels, 5000 samples at 500.0 Hz
//	//   Group 1: 8 channels, 10000 samples at 1000.0 Hz
func (dfww *DICOMFileWriterWithWaveforms) GetWaveformSummary() string {
	if len(dfww.waveformGroups) == 0 {
		return "No waveforms to write"
	}

	summary := fmt.Sprintf("%d waveform group(s) will be written:\n", len(dfww.waveformGroups))
	for i, group := range dfww.waveformGroups {
		summary += fmt.Sprintf("  Group %d: %d channels, %d samples at %.1f Hz",
			i, group.NumberOfWaveformChannels, group.NumberOfSamples, group.SamplingFrequency)
		if group.MultiplexGroupLabel != "" {
			summary += fmt.Sprintf(" [%s]", group.MultiplexGroupLabel)
		}
		if group.WaveformOriginality != "" {
			summary += fmt.Sprintf(" (%s)", group.WaveformOriginality)
		}
		if i < len(dfww.waveformGroups)-1 {
			summary += "\n"
		}
	}

	return summary
}
