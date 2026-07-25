package filereader

import (
	"fmt"
	"io"
	"strings"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/hooks"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// FileMetaInfo contains DICOM file meta header information.
type FileMetaInfo struct {
	MediaStorageSOPClassUID         string
	MediaStorageSOPInstanceUID      string
	TransferSyntaxUID               string
	ImplementationClassUID          string
	ImplementationVersionName       string
	SourceApplicationEntityTitle    string
	SendingApplicationEntityTitle   string
	ReceivingApplicationEntityTitle string
	FileMetaInformationGroupLength  uint32
	FileMetaInformationVersion      []byte
}

// DCMFileReader reads DICOM files.
type DCMFileReader struct {
	reader       filebase.Reader
	fileMetaInfo *FileMetaInfo
	position     int64
	settings     *config.Settings // Configuration settings for reading behavior
	hookChain    *hooks.HookChain // Hook chain for element processing
	elementCount int              // Count of elements read
}

// NewDCMFileReader creates a new DICOM file reader.
func NewDCMFileReader(reader filebase.Reader) *DCMFileReader {
	return &DCMFileReader{
		reader:       reader,
		position:     0,
		settings:     config.Get(),         // Load global configuration settings
		hookChain:    hooks.NewHookChain(), // Initialize empty hook chain
		elementCount: 0,
	}
}

// ReadPreamble reads the 128-byte DICOM preamble.
func (dfr *DCMFileReader) ReadPreamble() error {
	preamble := make([]byte, 128)
	if err := dfr.reader.ReadBytes(preamble); err != nil {
		return fmt.Errorf("failed to read preamble: %w", err)
	}

	dfr.position += 128
	return nil
}

// ReadDICMPrefix reads and validates the "DICM" magic string.
func (dfr *DCMFileReader) ReadDICMPrefix() error {
	prefix := make([]byte, 4)
	if err := dfr.reader.ReadBytes(prefix); err != nil {
		return fmt.Errorf("failed to read DICM prefix: %w", err)
	}

	if string(prefix) != "DICM" {
		return fmt.Errorf("invalid DICM prefix: got %q, expected 'DICM'", string(prefix))
	}

	dfr.position += 4
	return nil
}

// ReadFileMetaInformationGroupLength reads the File Meta Information Group Length (0002,0000).
func (dfr *DCMFileReader) ReadFileMetaInformationGroupLength() (uint32, error) {
	tagValue, err := dfr.ReadTag()
	if err != nil {
		return 0, fmt.Errorf("failed to read meta info group length tag: %w", err)
	}

	expectedTag := tag.New(0x0002, 0x0000)
	if tagValue != expectedTag {
		return 0, fmt.Errorf("expected tag %s for file meta info group length, got %s", expectedTag.String(), tagValue.String())
	}

	vr := make([]byte, 2)
	if err := dfr.reader.ReadBytes(vr); err != nil {
		return 0, fmt.Errorf("failed to read VR: %w", err)
	}

	if string(vr) != "UL" {
		return 0, fmt.Errorf("expected VR 'UL' for file meta info group length, got %q", string(vr))
	}

	reserved := make([]byte, 2)
	if err := dfr.reader.ReadBytes(reserved); err != nil {
		return 0, fmt.Errorf("failed to read reserved bytes: %w", err)
	}

	length, err := dfr.reader.ReadUint32()
	if err != nil {
		return 0, fmt.Errorf("failed to read group length: %w", err)
	}

	dfr.position += 12
	return length, nil
}

// ReadFileMetaInfo reads the DICOM file meta information.
func (dfr *DCMFileReader) ReadFileMetaInfo() (*FileMetaInfo, error) {
	metaInfo := &FileMetaInfo{}

	groupLength, err := dfr.ReadFileMetaInformationGroupLength()
	if err != nil {
		return nil, err
	}

	metaInfo.FileMetaInformationGroupLength = groupLength
	startPosition := dfr.position

	for dfr.position-startPosition < int64(groupLength) {
		tagValue, err := dfr.ReadTag()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read meta element tag: %w", err)
		}

		vrBytes := make([]byte, 2)
		if err := dfr.reader.ReadBytes(vrBytes); err != nil {
			return nil, fmt.Errorf("failed to read VR: %w", err)
		}
		vr := string(vrBytes)

		dfr.position += 6

		var valueLength uint32
		if isShortVR(vr) {
			length := make([]byte, 2)
			if err := dfr.reader.ReadBytes(length); err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = uint32(length[0]) | (uint32(length[1]) << 8)
			dfr.position += 2
		} else {
			reserved := make([]byte, 2)
			if err := dfr.reader.ReadBytes(reserved); err != nil {
				return nil, fmt.Errorf("failed to read reserved bytes: %w", err)
			}
			dfr.position += 2

			length, err := dfr.reader.ReadUint32()
			if err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = length
			dfr.position += 4
		}

		value := make([]byte, valueLength)
		if err := dfr.reader.ReadBytes(value); err != nil {
			// Handle read error gracefully - file meta info might be incomplete/corrupted
			// Log warning but continue with what we have so far
			fmt.Printf("Warning: skipping file meta element %s (expected %d bytes): %v\n",
				tagValue.String(), valueLength, err)
			// Return what we have - rest of file may be readable
			return metaInfo, nil
		}
		dfr.position += int64(valueLength)

		if err := dfr.storeMetaValue(metaInfo, tagValue, vr, value); err != nil {
			return nil, err
		}
	}

	dfr.fileMetaInfo = metaInfo
	return metaInfo, nil
}

// ReadTag reads a DICOM tag (4 bytes: group + element).
func (dfr *DCMFileReader) ReadTag() (tag.Tag, error) {
	group, err := dfr.reader.ReadUint16()
	if err != nil {
		return tag.Tag(0), err
	}

	element, err := dfr.reader.ReadUint16()
	if err != nil {
		return tag.Tag(0), err
	}

	dfr.position += 4
	return tag.New(uint16(group), uint16(element)), nil
}

// ReadDataElement reads a single data element from the dataset.
type DataElementValue struct {
	Tag    tag.Tag
	VR     string
	Value  []byte
	Length uint32
}

// ReadDataElement reads a data element.
func (dfr *DCMFileReader) ReadDataElement(explicitVR bool) (*DataElementValue, error) {
	tagValue, err := dfr.ReadTag()
	if err != nil {
		return nil, err
	}

	element := &DataElementValue{
		Tag: tagValue,
	}

	if explicitVR {
		vrBytes := make([]byte, 2)
		if err := dfr.reader.ReadBytes(vrBytes); err != nil {
			return nil, fmt.Errorf("failed to read VR: %w", err)
		}
		element.VR = string(vrBytes)
		dfr.position += 2

		var valueLength uint32
		if isShortVR(element.VR) {
			length := make([]byte, 2)
			if err := dfr.reader.ReadBytes(length); err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = uint32(length[0]) | (uint32(length[1]) << 8)
			dfr.position += 2
		} else {
			reserved := make([]byte, 2)
			if err := dfr.reader.ReadBytes(reserved); err != nil {
				return nil, fmt.Errorf("failed to read reserved bytes: %w", err)
			}
			dfr.position += 2

			length, err := dfr.reader.ReadUint32()
			if err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = length
			dfr.position += 4
		}
		element.Length = valueLength
	} else {
		length, err := dfr.reader.ReadUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to read value length: %w", err)
		}
		element.Length = length
		dfr.position += 4
	}

	if element.Length > 0 {
		value := make([]byte, element.Length)
		if err := dfr.reader.ReadBytes(value); err != nil {
			return nil, fmt.Errorf("failed to read data element value for tag %s (claimed %d bytes): %w",
				element.Tag.String(), element.Length, err)
		}
		element.Value = value
		dfr.position += int64(element.Length)
	}

	return element, nil
}

// GetPosition returns the current position in the file.
func (dfr *DCMFileReader) GetPosition() int64 {
	return dfr.position
}

// GetFileMetaInfo returns the file meta information.
func (dfr *DCMFileReader) GetFileMetaInfo() *FileMetaInfo {
	return dfr.fileMetaInfo
}

// RegisterHook registers a hook function at the specified processing level.
// Hooks allow custom processing of data elements during reading.
// Multiple hooks can be registered at the same level; they execute in order.
func (dfr *DCMFileReader) RegisterHook(level hooks.HookLevel, fn hooks.AdvancedHookFunc) error {
	if dfr.hookChain == nil {
		dfr.hookChain = hooks.NewHookChain()
	}
	return dfr.hookChain.RegisterHook(level, fn)
}

// GetHookChain returns the hook chain for this reader.
// Allows direct access for advanced hook chain operations.
func (dfr *DCMFileReader) GetHookChain() *hooks.HookChain {
	if dfr.hookChain == nil {
		dfr.hookChain = hooks.NewHookChain()
	}
	return dfr.hookChain
}

// GetElementCount returns the number of data elements that have been read.
func (dfr *DCMFileReader) GetElementCount() int {
	return dfr.elementCount
}

// ProcessElementThroughHooks converts a raw DataElementValue to DataElement and processes it
// through the registered hook chain. This allows custom processing at various stages
// (PreValidation, PostValidation, PreCompression, PostCompression, etc.).
// Returns the processed element or an error if hook processing fails.
func (dfr *DCMFileReader) ProcessElementThroughHooks(elem *DataElementValue) (*dataelem.DataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("element cannot be nil")
	}

	// Create DataElement from raw value (elem.Tag is already tag.Tag)
	dataElem := dataelem.NewDataElement(elem.Tag, dataelem.VR(elem.VR), elem.Value)
	if dataElem == nil {
		return nil, fmt.Errorf("failed to create data element from tag %s", elem.Tag.String())
	}

	// Process through hook chain at PostValidation level by default
	// This allows hooks to access the converted element
	if dfr.hookChain != nil && dfr.hookChain.HookCount() > 0 {
		processed, err := dfr.hookChain.ExecuteHooks(dataElem, hooks.PostValidation)
		if err != nil {
			return nil, fmt.Errorf("hook processing failed for tag %s: %w", elem.Tag.String(), err)
		}
		if processed != nil {
			dataElem = processed
		}
	}

	// Increment element count after successful processing
	dfr.elementCount++

	return dataElem, nil
}

// isShortVR checks if a VR uses short format (2-byte length instead of 4-byte).
// Short format VRs have a 2-byte reserved field and 2-byte length.
// Long format VRs (OB, OD, OF, OL, OW, SQ, UC, UN, UR, UT) have 2-byte reserved and 4-byte length.
func isShortVR(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OW", "SQ", "UC", "UN", "UR", "UT":
		return false
	default:
		return true
	}
}

// storeMetaValue stores a parsed meta information value into the appropriate FileMetaInfo field.
// It handles the standard DICOM file meta information tags (Group 0002).
func (dfr *DCMFileReader) storeMetaValue(metaInfo *FileMetaInfo, t tag.Tag, vr string, value []byte) error {
	group := t.Group()
	element := t.Element()

	// Helper function to convert bytes to string and trim null terminators
	toString := func(b []byte) string {
		// Find and remove null terminator if present
		s := string(b)
		if idx := strings.Index(s, "\x00"); idx >= 0 {
			return s[:idx]
		}
		return s
	}

	switch {
	case group == 0x0002 && element == 0x0001:
		metaInfo.FileMetaInformationVersion = value
	case group == 0x0002 && element == 0x0002:
		metaInfo.MediaStorageSOPClassUID = toString(value)
	case group == 0x0002 && element == 0x0003:
		metaInfo.MediaStorageSOPInstanceUID = toString(value)
	case group == 0x0002 && element == 0x0010:
		metaInfo.TransferSyntaxUID = toString(value)
	case group == 0x0002 && element == 0x0012:
		metaInfo.ImplementationClassUID = toString(value)
	case group == 0x0002 && element == 0x0013:
		metaInfo.ImplementationVersionName = toString(value)
	// Application Entity titles are (0002,0016..0018) per PS3.6; the 0x0100
	// range previously used here belongs to Private Information attributes.
	case group == 0x0002 && element == 0x0016:
		metaInfo.SourceApplicationEntityTitle = toString(value)
	case group == 0x0002 && element == 0x0017:
		metaInfo.SendingApplicationEntityTitle = toString(value)
	case group == 0x0002 && element == 0x0018:
		metaInfo.ReceivingApplicationEntityTitle = toString(value)
	}

	return nil
}

// ConvertRawDataElement converts a raw data element using hooks.
// This integrates with the hooks system for custom VR lookup and value conversion.
func ConvertRawDataElement(elem *DataElementValue, encoding string) (*hooks.RawDataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("element cannot be nil")
	}

	// Convert to RawDataElement format
	vrPtr := &elem.VR
	raw := &hooks.RawDataElement{
		Tag:   elem.Tag.String(),
		VR:    vrPtr,
		Value: elem.Value,
	}

	return raw, nil
}

// ReadDICOMFile reads a complete DICOM file including preamble, meta header, and dataset.
type DICOMFile struct {
	FileMetaInfo   *FileMetaInfo
	DataElements   []*DataElementValue
	ExplicitVR     bool
	IsLittleEndian bool
}

// ReadDICOMFile reads an entire DICOM file.
func ReadDICOMFile(reader filebase.Reader) (*DICOMFile, error) {
	dfr := NewDCMFileReader(reader)

	dicomFile := &DICOMFile{
		DataElements: make([]*DataElementValue, 0),
	}

	if err := dfr.ReadPreamble(); err != nil {
		return nil, fmt.Errorf("failed to read preamble: %w", err)
	}

	if err := dfr.ReadDICMPrefix(); err != nil {
		return nil, fmt.Errorf("failed to read DICM prefix: %w", err)
	}

	metaInfo, err := dfr.ReadFileMetaInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to read file meta information: %w", err)
	}

	dicomFile.FileMetaInfo = metaInfo

	ts := metaInfo.TransferSyntaxUID
	dicomFile.ExplicitVR, dicomFile.IsLittleEndian = determineTransferSyntax(ts)

	if dicomFile.IsLittleEndian {
		reader.SetByteOrder(filebase.LittleEndian)
	} else {
		reader.SetByteOrder(filebase.BigEndian)
	}

	for {
		element, err := dfr.ReadDataElement(dicomFile.ExplicitVR)
		if err != nil {
			if err == io.EOF {
				break
			}
			errMsg := err.Error()
			if strings.Contains(errMsg, "reached end of file") {
				break
			}
			// Check if this is a length-related error (corrupted element)
			if strings.Contains(errMsg, "claimed") && strings.Contains(errMsg, "bytes") {
				fmt.Printf("Warning: skipping corrupted element at position %d: %v\n", dfr.position, err)
				break
			}
			return nil, fmt.Errorf("failed to read data element: %w", err)
		}

		if err := validateDataElement(element); err != nil {
			fmt.Printf("Warning validating tag %s: %v\n", element.Tag.String(), err)
		}

		dicomFile.DataElements = append(dicomFile.DataElements, element)

		if element.Tag == tag.New(0xFFFE, 0xE0DD) || element.Tag == tag.New(0xFFFE, 0xE00D) {
			break
		}
	}

	return dicomFile, nil
}

// determineTransferSyntax determines the VR type and byte order from a transfer syntax UID.
// Returns (explicitVR, littleEndian).
//
// Examples:
// - ImplicitVRLittleEndian (default): (false, true)
// - ExplicitVRLittleEndian: (true, true)
// - ExplicitVRBigEndian: (true, false)
// - Compressed formats (JPEG, RLE, etc.): (true, true)
func determineTransferSyntax(ts string) (bool, bool) {
	explicitVR := false
	littleEndian := true

	u := uid.New(ts)
	if !u.IsValid() {
		return explicitVR, littleEndian
	}

	if ts == uid.BigEndianTransferSyntax().String() {
		return true, false
	}

	if ts == uid.ImplicitVRLittleEndian().String() {
		return false, true
	}

	if ts == uid.ExplicitVRLittleEndian().String() {
		return true, true
	}

	if !uid.IsCompressed(u) {
		return true, true
	}

	if uid.IsCompressed(u) {
		return true, true
	}

	return explicitVR, littleEndian
}

// validateDataElement validates a data element against the DICOM dictionary.
// It checks that:
// 1. Standard tags are registered in the DICOM dictionary
// 2. The tag is not retired
// 3. The VR matches the dictionary entry (or is a valid variant like "OB or OW")
//
// Private tags (odd group numbers) always pass validation.
func validateDataElement(elem *DataElementValue) error {
	dict := tag.GlobalDictionary()
	info := dict.Get(elem.Tag)

	if info == nil {
		if elem.Tag.IsPrivate() {
			return nil
		}
		return fmt.Errorf("unknown standard tag: %s", elem.Tag)
	}

	if info.Retired {
		return fmt.Errorf("tag %s (%s) is retired", elem.Tag, info.Name)
	}

	if elem.VR != "" && info.VR != "" {
		if info.VR != elem.VR && !isValidVRVariant(elem.VR, info.VR) {
			return fmt.Errorf("VR mismatch for %s: expected %s, got %s",
				elem.Tag, info.VR, elem.VR)
		}
	}

	return nil
}

// isValidVRVariant checks if a VR is a valid variant of the expected VR.
// Some DICOM tags allow multiple VR types, represented as "X or Y" in the dictionary.
// This function validates that the actual VR matches one of the allowed variants.
func isValidVRVariant(actual, expected string) bool {
	switch expected {
	case "OB or OW":
		return actual == "OB" || actual == "OW"
	case "US or SS":
		return actual == "US" || actual == "SS"
	case "US or OW":
		return actual == "US" || actual == "OW"
	case "UL or OW":
		return actual == "UL" || actual == "OW"
	case "OB or OD or OF or OL or OW or UN":
		return actual == "OB" || actual == "OD" || actual == "OF" || actual == "OL" || actual == "OW" || actual == "UN"
	default:
		return false
	}
}
