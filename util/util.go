package util

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// Hex2Bytes converts a hex string with optional whitespace to bytes.
func Hex2Bytes(hexString string) ([]byte, error) {
	// Remove all whitespace
	cleaned := strings.ReplaceAll(hexString, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")

	// Decode hex string
	return hex.DecodeString(cleaned)
}

// Bytes2Hex converts bytes to a hex string with spaces between pairs.
func Bytes2Hex(data []byte) string {
	hexStr := hex.EncodeToString(data)
	var result strings.Builder

	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			result.WriteString(" ")
		}
		result.WriteString(hexStr[i : i+2])
	}

	return result.String()
}

// PrintCharacter returns a printable character or '.' for non-printable.
func PrintCharacter(ordchr byte) string {
	if ordchr > 31 && ordchr < 126 && ordchr != 92 {
		return string(ordchr)
	}
	return "."
}

// HexDump returns a formatted hex dump of data in hexdump -C format.
func HexDump(data []byte, startAddress int, stopAddress int) string {
	return HexDumpReader(strings.NewReader(string(data)), startAddress, stopAddress, true)
}

// HexDumpReader returns a formatted hex dump of a reader with optional address display.
func HexDumpReader(r io.Reader, startAddress int, stopAddress int, showAddress bool) string {
	var lines []string
	data := make([]byte, 16)
	offset := startAddress

	for {
		n, err := r.Read(data)
		if err != nil && err != io.EOF {
			break
		}

		if n == 0 {
			break
		}

		if stopAddress > 0 && offset > stopAddress {
			break
		}

		var line strings.Builder

		// Print offset if requested
		if showAddress {
			line.WriteString(fmt.Sprintf("%08X  ", offset))
		}

		// Print hex bytes
		for i := 0; i < 16; i++ {
			if i < n {
				line.WriteString(fmt.Sprintf("%02X ", data[i]))
			} else {
				line.WriteString("   ")
			}
		}

		// Print ASCII representation
		line.WriteString(" |")
		for i := 0; i < n; i++ {
			line.WriteString(PrintCharacter(data[i]))
		}
		line.WriteString("|")

		lines = append(lines, line.String())
		offset += n

		if n < 16 {
			break
		}
	}

	return strings.Join(lines, "\n")
}

// DatasetInfo provides utility information about a dataset.
type DatasetInfo struct {
	PatientName    string
	PatientID      string
	StudyDate      string
	Modality       string
	SeriesNumber   int
	InstanceNumber int
	Rows           int
	Columns        int
	BitsAllocated  int
	BitsStored     int
	SOPClassUID    string
	SOPInstanceUID string
}

// GetDatasetInfo extracts common clinical information from a dataset.
func GetDatasetInfo(ds *dataset.Dataset) (*DatasetInfo, error) {
	info := &DatasetInfo{}

	// Patient Name (0010,0010)
	if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
		info.PatientName = extractString(elem.GetValue())
	}

	// Patient ID (0010,0020)
	if elem, ok := ds.Get(tag.New(0x0010, 0x0020)); ok {
		info.PatientID = extractString(elem.GetValue())
	}

	// Study Date (0008,0020)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0020)); ok {
		info.StudyDate = extractString(elem.GetValue())
	}

	// Modality (0008,0060)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0060)); ok {
		info.Modality = extractString(elem.GetValue())
	}

	// Series Number (0020,0011)
	if elem, ok := ds.Get(tag.New(0x0020, 0x0011)); ok {
		info.SeriesNumber = extractInt(elem.GetValue())
	}

	// Instance Number (0020,0013)
	if elem, ok := ds.Get(tag.New(0x0020, 0x0013)); ok {
		info.InstanceNumber = extractInt(elem.GetValue())
	}

	// SOP Class UID (0008,0016)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0016)); ok {
		info.SOPClassUID = extractString(elem.GetValue())
	}

	// SOP Instance UID (0008,0018)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0018)); ok {
		info.SOPInstanceUID = extractString(elem.GetValue())
	}

	// Get pixel data info if available
	if pixelInfo, err := ds.GetPixelDataInfo(); err == nil {
		info.Rows = pixelInfo.Rows
		info.Columns = pixelInfo.Columns
		info.BitsAllocated = pixelInfo.BitsAllocated
		info.BitsStored = pixelInfo.BitsStored
	}

	return info, nil
}

// PrintDatasetInfo prints dataset information in a human-readable format.
func PrintDatasetInfo(info *DatasetInfo) {
	fmt.Println("=== DICOM Dataset Information ===")
	if info.PatientName != "" {
		fmt.Printf("Patient Name:     %s\n", info.PatientName)
	}
	if info.PatientID != "" {
		fmt.Printf("Patient ID:       %s\n", info.PatientID)
	}
	if info.StudyDate != "" {
		fmt.Printf("Study Date:       %s\n", info.StudyDate)
	}
	if info.Modality != "" {
		fmt.Printf("Modality:         %s\n", info.Modality)
	}
	if info.SeriesNumber > 0 {
		fmt.Printf("Series Number:    %d\n", info.SeriesNumber)
	}
	if info.InstanceNumber > 0 {
		fmt.Printf("Instance Number:  %d\n", info.InstanceNumber)
	}
	if info.SOPClassUID != "" {
		fmt.Printf("SOP Class UID:    %s\n", info.SOPClassUID)
	}
	if info.SOPInstanceUID != "" {
		fmt.Printf("SOP Instance UID: %s\n", info.SOPInstanceUID)
	}
	if info.Rows > 0 && info.Columns > 0 {
		fmt.Printf("Image Dimensions: %d x %d\n", info.Rows, info.Columns)
	}
	if info.BitsAllocated > 0 {
		fmt.Printf("Bits Allocated:   %d\n", info.BitsAllocated)
	}
	fmt.Println("==================================")
}

// PrettyPrint prints a dataset with nice indentation.
func PrettyPrint(ds *dataset.Dataset, indentLevel int) {
	indent := strings.Repeat("  ", indentLevel)

	allTags := ds.Tags()
	for _, t := range allTags {
		if elem, ok := ds.Get(t); ok {
			vr := "?"
			if info := t.GetInfo(); info != nil {
				vr = info.VR
			}
			fmt.Printf("%s%s [%s]: %v\n", indent, t.String(), vr, extractString(elem.GetValue()))
		}
	}
}

// ==================== FIXER UTILITIES ====================

// FixSeparatorConfig configures fixing of invalid DICOM element separators.
type FixSeparatorConfig struct {
	InvalidSeparator  byte
	ForVRs            []string
	ProcessUnknownVRs bool
}

// DefaultFixSeparatorConfig returns a config for fixing common separator issues.
func DefaultFixSeparatorConfig() *FixSeparatorConfig {
	return &FixSeparatorConfig{
		InvalidSeparator:  ' ', // Space instead of backslash
		ForVRs:            []string{"DS", "IS"},
		ProcessUnknownVRs: true,
	}
}

// FixSeparator fixes DICOM files with invalid separators by replacing them with backslashes.
func FixSeparator(ds *dataset.Dataset, config *FixSeparatorConfig) (*dataset.Dataset, error) {
	if ds == nil || config == nil {
		return ds, nil
	}

	// Create a new dataset to avoid modifying the original
	newDS := dataset.NewDataset()

	// Build a map for faster VR lookup
	vrMap := make(map[string]bool)
	for _, vr := range config.ForVRs {
		vrMap[vr] = true
	}

	// Iterate through all tags in the original dataset
	allTags := ds.Tags()
	for _, t := range allTags {
		elem, ok := ds.Get(t)
		if !ok {
			continue
		}

		// Get VR for this tag
		tagInfo := t.GetInfo()
		var vr string
		var isKnownVR bool
		if tagInfo != nil {
			vr = tagInfo.VR
			isKnownVR = true
		} else {
			vr = "UN" // Unknown VR
			isKnownVR = false
		}

		// Check if we should process this VR
		// Process if: VR is in the ForVRs list, or (ProcessUnknownVRs is true and either VR is unknown or no ForVRs specified)
		inVRList := vrMap[vr]
		shouldFix := inVRList || (config.ProcessUnknownVRs && (len(config.ForVRs) == 0 || !isKnownVR))

		if shouldFix && elem.GetValue() != nil {
			// Fix the separator in the value
			fixedElem := fixElementSeparator(elem, t, config.InvalidSeparator)
			_ = newDS.Add(fixedElem)
		} else {
			// Copy the element as-is
			_ = newDS.Add(elem)
		}
	}

	return newDS, nil
}

// fixElementSeparator fixes separators within a single element's value.
func fixElementSeparator(elem *dataelem.DataElement, t tag.Tag, invalidSeparator byte) *dataelem.DataElement {
	value := elem.GetValue()
	var fixedValue interface{}

	// Handle different value types
	switch v := value.(type) {
	case string:
		fixedValue = strings.ReplaceAll(v, string(invalidSeparator), "\\")

	case []byte:
		// Convert bytes to string, fix, convert back
		valueStr := string(v)
		fixedStr := strings.ReplaceAll(valueStr, string(invalidSeparator), "\\")
		fixedValue = []byte(fixedStr)

	case []string:
		// For multiple values, fix each one
		fixedValues := make([]string, len(v))
		for i, s := range v {
			fixedValues[i] = strings.ReplaceAll(s, string(invalidSeparator), "\\")
		}
		fixedValue = fixedValues

	default:
		// For other types, convert to string, fix, and set back if successful
		str := fmt.Sprintf("%v", v)
		fixedStr := strings.ReplaceAll(str, string(invalidSeparator), "\\")
		if str != fixedStr {
			fixedValue = fixedStr
		} else {
			fixedValue = v
		}
	}

	// Create a new element with the fixed value
	newElem := dataelem.NewDataElement(t, elem.VR, []byte{})
	newElem.SetValue(fixedValue)
	return newElem
}

// ==================== HELPER FUNCTIONS ====================

func extractString(elem interface{}) string {
	if elem == nil {
		return ""
	}

	switch v := elem.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func extractInt(elem interface{}) int {
	if elem == nil {
		return 0
	}

	switch v := elem.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint32:
		return int(v)
	case string:
		var i int
		_, _ = fmt.Sscanf(v, "%d", &i)
		return i
	case []byte:
		var i int
		_, _ = fmt.Sscanf(string(v), "%d", &i)
		return i
	default:
		return 0
	}
}

// DumpDataset returns a string representation of all dataset elements.
func DumpDataset(ds *dataset.Dataset) string {
	var sb strings.Builder
	sb.WriteString("=== DICOM Dataset Dump ===\n")

	allTags := ds.Tags()
	for _, t := range allTags {
		if elem, ok := ds.Get(t); ok {
			vr := "?"
			if info := t.GetInfo(); info != nil {
				vr = info.VR
			}
			sb.WriteString(fmt.Sprintf("%s [%s]: %v\n", t.String(), vr, extractString(elem.GetValue())))
		}
	}

	sb.WriteString("==========================\n")
	return sb.String()
}
