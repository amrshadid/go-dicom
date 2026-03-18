package filewriter

import (
	"fmt"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/hooks"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// FileMetaInfo represents DICOM file meta information for writing.
type FileMetaInfo struct {
	MediaStorageSOPClassUID         string
	MediaStorageSOPInstanceUID      string
	TransferSyntaxUID               string
	ImplementationClassUID          string
	ImplementationVersionName       string
	SourceApplicationEntityTitle    string
	SendingApplicationEntityTitle   string
	ReceivingApplicationEntityTitle string
}

// DataElement represents a DICOM data element for writing.
type DataElement struct {
	Tag    tag.Tag
	VR     string
	Value  []byte
	Length uint32
}

// DCMFileWriter writes DICOM files.
type DCMFileWriter struct {
	writer       filebase.Writer
	position     int64
	explicitVR   bool
	littleEndian bool
	settings     *config.Settings // Configuration settings for writing behavior
	hookChain    *hooks.HookChain // Hook chain for element processing
	elementCount int              // Count of elements written
}

// NewDCMFileWriter creates a new DICOM file writer.
func NewDCMFileWriter(writer filebase.Writer) *DCMFileWriter {
	return &DCMFileWriter{
		writer:       writer,
		position:     0,
		explicitVR:   true,
		littleEndian: true,
		settings:     config.Get(),         // Load global configuration settings
		hookChain:    hooks.NewHookChain(), // Initialize empty hook chain
		elementCount: 0,
	}
}

// SetExplicitVR sets whether to use explicit VR.
func (dfw *DCMFileWriter) SetExplicitVR(explicit bool) {
	dfw.explicitVR = explicit
}

// SetLittleEndian sets the byte order (little-endian or big-endian).
func (dfw *DCMFileWriter) SetLittleEndian(littleEndian bool) {
	dfw.littleEndian = littleEndian
	if littleEndian {
		dfw.writer.SetByteOrder(filebase.LittleEndian)
	} else {
		dfw.writer.SetByteOrder(filebase.BigEndian)
	}
}

// WritePreamble writes the 128-byte DICOM preamble.
func (dfw *DCMFileWriter) WritePreamble() error {
	preamble := make([]byte, 128)
	if err := dfw.writer.WriteBytes(preamble); err != nil {
		return fmt.Errorf("failed to write preamble: %w", err)
	}
	dfw.position += 128
	return nil
}

// WriteDICMPrefix writes the "DICM" magic string.
func (dfw *DCMFileWriter) WriteDICMPrefix() error {
	if err := dfw.writer.WriteBytes([]byte("DICM")); err != nil {
		return fmt.Errorf("failed to write DICM prefix: %w", err)
	}
	dfw.position += 4
	return nil
}

// WriteFileMetaInfo writes the DICOM file meta information.
func (dfw *DCMFileWriter) WriteFileMetaInfo(metaInfo *FileMetaInfo) error {
	if metaInfo == nil {
		return fmt.Errorf("file meta info is nil")
	}
	elements := make([]*DataElement, 0)

	if metaInfo.MediaStorageSOPClassUID != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0010),
			VR:     "UI",
			Value:  []byte(metaInfo.MediaStorageSOPClassUID),
			Length: uint32(len(metaInfo.MediaStorageSOPClassUID)),
		})
	}

	if metaInfo.MediaStorageSOPInstanceUID != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0012),
			VR:     "UI",
			Value:  []byte(metaInfo.MediaStorageSOPInstanceUID),
			Length: uint32(len(metaInfo.MediaStorageSOPInstanceUID)),
		})
	}

	if metaInfo.TransferSyntaxUID != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0020),
			VR:     "UI",
			Value:  []byte(metaInfo.TransferSyntaxUID),
			Length: uint32(len(metaInfo.TransferSyntaxUID)),
		})
	}

	if metaInfo.ImplementationClassUID != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0100),
			VR:     "UI",
			Value:  []byte(metaInfo.ImplementationClassUID),
			Length: uint32(len(metaInfo.ImplementationClassUID)),
		})
	}

	if metaInfo.ImplementationVersionName != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0101),
			VR:     "SH",
			Value:  []byte(metaInfo.ImplementationVersionName),
			Length: uint32(len(metaInfo.ImplementationVersionName)),
		})
	}

	if metaInfo.SourceApplicationEntityTitle != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0110),
			VR:     "AE",
			Value:  []byte(metaInfo.SourceApplicationEntityTitle),
			Length: uint32(len(metaInfo.SourceApplicationEntityTitle)),
		})
	}

	if metaInfo.SendingApplicationEntityTitle != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0111),
			VR:     "AE",
			Value:  []byte(metaInfo.SendingApplicationEntityTitle),
			Length: uint32(len(metaInfo.SendingApplicationEntityTitle)),
		})
	}

	if metaInfo.ReceivingApplicationEntityTitle != "" {
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, 0x0112),
			VR:     "AE",
			Value:  []byte(metaInfo.ReceivingApplicationEntityTitle),
			Length: uint32(len(metaInfo.ReceivingApplicationEntityTitle)),
		})
	}

	// Calculate total group length (all elements after group length tag)
	groupLength := uint32(0)
	for _, elem := range elements {
		// Tag (4) + VR (2) + Length field size + Value
		groupLength += 4 + 2
		if isShortVR(elem.VR) {
			// Short VR: 2-byte length
			groupLength += 2 + elem.Length
		} else {
			// Long VR: 2-byte reserved + 4-byte length
			groupLength += 2 + 4 + elem.Length
		}
	}

	// Write Group Length (0002,0000) - UL is a short VR with 2-byte length
	if err := dfw.WriteTag(tag.New(0x0002, 0x0000)); err != nil {
		return err
	}

	if err := dfw.writer.WriteBytes([]byte("UL")); err != nil {
		return fmt.Errorf("failed to write VR: %w", err)
	}

	// UL is a short VR, so write 2-byte length directly (not reserved + 4-byte)
	if err := dfw.writer.WriteUint16(4); err != nil { // Length of value (always 4 for UL)
		return fmt.Errorf("failed to write value length: %w", err)
	}

	if err := dfw.writer.WriteUint32(groupLength); err != nil {
		return fmt.Errorf("failed to write group length value: %w", err)
	}

	dfw.position += 4 + 2 + 2 + 4 // tag + VR + length + value

	// Write all other meta elements
	for _, elem := range elements {
		if err := dfw.WriteDataElement(elem, true); err != nil {
			return err
		}
	}

	return nil
}

// WriteTag writes a DICOM tag (4 bytes: group + element).
func (dfw *DCMFileWriter) WriteTag(t tag.Tag) error {
	if err := dfw.writer.WriteUint16(t.Group()); err != nil {
		return fmt.Errorf("failed to write tag group: %w", err)
	}

	if err := dfw.writer.WriteUint16(t.Element()); err != nil {
		return fmt.Errorf("failed to write tag element: %w", err)
	}

	dfw.position += 4
	return nil
}

// WriteDataElement writes a single data element.
func (dfw *DCMFileWriter) WriteDataElement(elem *DataElement, forceExplicitVR bool) error {
	if elem == nil {
		return fmt.Errorf("data element is nil")
	}

	// Write tag
	if err := dfw.WriteTag(elem.Tag); err != nil {
		return err
	}

	explicitVR := dfw.explicitVR || forceExplicitVR

	if explicitVR {
		// Write VR (2 bytes)
		if err := dfw.writer.WriteBytes([]byte(elem.VR)); err != nil {
			return fmt.Errorf("failed to write VR: %w", err)
		}
		dfw.position += 2

		// Write length based on VR type
		if isShortVR(elem.VR) {
			// Short VR format: 2-byte length
			if err := dfw.writer.WriteUint16(uint16(elem.Length)); err != nil {
				return fmt.Errorf("failed to write value length: %w", err)
			}
			dfw.position += 2
		} else {
			// Long VR format: 2-byte reserved + 4-byte length
			if err := dfw.writer.WriteBytes([]byte{0x00, 0x00}); err != nil {
				return fmt.Errorf("failed to write reserved bytes: %w", err)
			}
			if err := dfw.writer.WriteUint32(elem.Length); err != nil {
				return fmt.Errorf("failed to write value length: %w", err)
			}
			dfw.position += 6
		}
	} else {
		// Implicit VR: 4-byte length
		if err := dfw.writer.WriteUint32(elem.Length); err != nil {
			return fmt.Errorf("failed to write value length: %w", err)
		}
		dfw.position += 4
	}

	// Write value
	if elem.Length > 0 {
		if err := dfw.writer.WriteBytes(elem.Value); err != nil {
			return fmt.Errorf("failed to write data element value: %w", err)
		}
		dfw.position += int64(elem.Length)
	}

	return nil
}

// WriteDataElements writes multiple data elements.
func (dfw *DCMFileWriter) WriteDataElements(elements []*DataElement) error {
	for _, elem := range elements {
		if err := dfw.WriteDataElement(elem, false); err != nil {
			return err
		}
	}
	return nil
}

// GetPosition returns the current position in the file.
func (dfw *DCMFileWriter) GetPosition() int64 {
	return dfw.position
}

// RegisterHook registers a hook function at the specified processing level.
// Hooks allow custom processing of data elements before writing.
// Multiple hooks can be registered at the same level; they execute in order.
func (dfw *DCMFileWriter) RegisterHook(level hooks.HookLevel, fn hooks.AdvancedHookFunc) error {
	if dfw.hookChain == nil {
		dfw.hookChain = hooks.NewHookChain()
	}
	return dfw.hookChain.RegisterHook(level, fn)
}

// GetHookChain returns the hook chain for this writer.
// Allows direct access for advanced hook chain operations.
func (dfw *DCMFileWriter) GetHookChain() *hooks.HookChain {
	if dfw.hookChain == nil {
		dfw.hookChain = hooks.NewHookChain()
	}
	return dfw.hookChain
}

// GetElementCount returns the number of data elements that have been written.
func (dfw *DCMFileWriter) GetElementCount() int {
	return dfw.elementCount
}

// Flush flushes any buffered data.
func (dfw *DCMFileWriter) Flush() error {
	return dfw.writer.Flush()
}

// Close closes the writer.
func (dfw *DCMFileWriter) Close() error {
	if err := dfw.Flush(); err != nil {
		return err
	}
	return dfw.writer.Close()
}

// Helper functions

// ConvertDataElementToRaw converts a DataElement to a RawDataElement for hook processing.
func ConvertDataElementToRaw(elem *DataElement) (*hooks.RawDataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("data element cannot be nil")
	}
	vr := elem.VR
	raw := &hooks.RawDataElement{
		Tag:   elem.Tag.String(),
		VR:    &vr,
		Value: elem.Value,
	}
	return raw, nil
}

func isShortVR(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OW", "SQ", "UC", "UN", "UR", "UT":
		return false
	default:
		return true
	}
}

// WriteDICOMFile writes a complete DICOM file.
type DICOMFileWriter struct {
	writer       *DCMFileWriter
	metaInfo     *FileMetaInfo
	dataElements []*DataElement
}

// NewDICOMFileWriter creates a new DICOM file writer.
func NewDICOMFileWriter(writer filebase.Writer) *DICOMFileWriter {
	return &DICOMFileWriter{
		writer:       NewDCMFileWriter(writer),
		dataElements: make([]*DataElement, 0),
	}
}

// SetFileMetaInfo sets the file meta information.
func (dfw *DICOMFileWriter) SetFileMetaInfo(metaInfo *FileMetaInfo) {
	dfw.metaInfo = metaInfo

	// Set transfer syntax properties
	if metaInfo != nil && metaInfo.TransferSyntaxUID != "" {
		explicitVR, littleEndian := determineTransferSyntax(metaInfo.TransferSyntaxUID)
		dfw.writer.SetExplicitVR(explicitVR)
		dfw.writer.SetLittleEndian(littleEndian)
	}
}

// AddDataElement adds a data element to the dataset.
func (dfw *DICOMFileWriter) AddDataElement(elem *DataElement) error {
	if elem == nil {
		return fmt.Errorf("data element is nil")
	}
	dfw.dataElements = append(dfw.dataElements, elem)
	return nil
}

// AddDataElements adds multiple data elements to the dataset.
func (dfw *DICOMFileWriter) AddDataElements(elements []*DataElement) error {
	for _, elem := range elements {
		if err := dfw.AddDataElement(elem); err != nil {
			return err
		}
	}
	return nil
}

// Write writes the complete DICOM file.
func (dfw *DICOMFileWriter) Write() error {
	// Write preamble
	if err := dfw.writer.WritePreamble(); err != nil {
		return fmt.Errorf("failed to write preamble: %w", err)
	}

	// Write DICM magic
	if err := dfw.writer.WriteDICMPrefix(); err != nil {
		return fmt.Errorf("failed to write DICM prefix: %w", err)
	}

	// Write file meta information
	if dfw.metaInfo != nil {
		if err := dfw.writer.WriteFileMetaInfo(dfw.metaInfo); err != nil {
			return fmt.Errorf("failed to write file meta information: %w", err)
		}
	}

	// Write dataset elements
	if err := dfw.writer.WriteDataElements(dfw.dataElements); err != nil {
		return fmt.Errorf("failed to write data elements: %w", err)
	}

	return nil
}

// Close closes the writer.
func (dfw *DICOMFileWriter) Close() error {
	return dfw.writer.Close()
}

// determineTransferSyntax determines if a transfer syntax uses explicit VR and little-endian.
// Uses the uid module for transfer syntax classification.
func determineTransferSyntax(ts string) (bool, bool) {
	// Default: Implicit VR Little Endian
	explicitVR := false
	littleEndian := true

	// Validate and classify transfer syntax using uid module
	u := uid.New(ts)
	if !u.IsValid() {
		// Invalid UID, use default
		return explicitVR, littleEndian
	}

	// Check for big-endian (explicit VR big endian)
	if ts == uid.BigEndianTransferSyntax().String() {
		return true, false
	}

	// Check for implicit VR little endian (default)
	if ts == uid.ImplicitVRLittleEndian().String() {
		return false, true
	}

	// Check for explicit VR little endian
	if ts == uid.ExplicitVRLittleEndian().String() {
		return true, true
	}

	// Check for any other uncompressed native format
	if !uid.IsCompressed(u) {
		// Any other uncompressed syntax is explicit VR little endian
		return true, true
	}

	// All compressed transfer syntaxes are explicit VR, little endian
	if uid.IsCompressed(u) {
		return true, true
	}

	// Default fallback
	return explicitVR, littleEndian
}
