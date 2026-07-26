package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// DicomElement represents a DICOM data element.
type DicomElement struct {
	Tag   string
	Name  string
	VR    string
	Value []byte
	Depth int
}

// DicomInfo contains extracted key DICOM metadata.
type DicomInfo struct {
	PatientName       string
	PatientID         string
	StudyDate         string
	Modality          string
	StudyDescription  string
	SeriesDescription string
	NumberOfFrames    int
	Rows              int
	Columns           int
}

// readDICOMFile reads a DICOM file and returns its elements, flattened for
// display with Depth recording sequence nesting.
//
// This delegates to the filereader package rather than parsing the file here.
// The CLI previously carried its own parser, which read the file in 64 KB
// chunks and parsed each chunk independently: any element straddling a
// boundary desynchronized the stream, so a file larger than one chunk lost
// most of its elements and gained invented ones with impossible VRs. It also
// never descended into sequences, and received none of the fixes made to the
// real reader.
func readDICOMFile(filename string) ([]DicomElement, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
	if err != nil {
		return nil, fmt.Errorf("failed to read DICOM file: %w", err)
	}

	// The file meta header first, as it appears in the file, then the data set.
	elements := make([]DicomElement, 0, len(df.MetaElements)+len(df.DataElements))
	elements = appendElements(elements, df.MetaElements, 0)
	elements = appendElements(elements, df.DataElements, 0)
	return elements, nil
}

// appendElements flattens parsed elements for display, descending into
// sequences and recording the nesting level in Depth.
func appendElements(out []DicomElement, elems []*filereader.DataElementValue, depth int) []DicomElement {
	for _, e := range elems {
		out = append(out, DicomElement{
			Tag:   fmt.Sprintf("%04X,%04X", e.Tag.Group(), e.Tag.Element()),
			Name:  getElementName(e.Tag.Group(), e.Tag.Element()),
			VR:    e.VR,
			Value: e.Value,
			Depth: depth,
		})

		// Sequence items nest one level deeper than the sequence itself.
		for _, item := range e.Items {
			out = appendElements(out, item.Elements, depth+1)
		}
	}
	return out
}

// getElementName maps DICOM tag to human-readable name using the dictionary.
func getElementName(group, element uint16) string {
	// Create tag from group and element
	t := tag.New(group, element)

	// Try to get from dictionary first
	if info := t.GetInfo(); info != nil {
		return info.Name
	}

	// Fallback: try keyword
	keyword := t.GetKeyword()
	if keyword != "" {
		return keyword
	}

	// Final fallback: return tag as string
	return fmt.Sprintf("Tag_%04X_%04X", group, element)
}

// formatValue converts bytes to displayable string.
func formatValue(vr string, value []byte, maxLen int) string {
	if len(value) == 0 {
		return ""
	}

	// Limit length for display
	if len(value) > maxLen {
		return fmt.Sprintf("%s... (%d bytes)", formatValueRaw(vr, value[:maxLen]), len(value))
	}

	return formatValueRaw(vr, value)
}

// decodeTextWithCharset decodes text value with charset support
func decodeTextWithCharset(value []byte, vr string) string {
	if len(value) == 0 {
		return ""
	}

	// Try charset-aware decoding for text VRs
	if vr == "PN" || vr == "LO" || vr == "LT" || vr == "SH" || vr == "ST" || vr == "UT" {
		decoded, err := charset.DecodeBytes(value, []string{charset.DefaultEncoding}, charset.DefaultTextDelimiters)
		if err == nil {
			return decoded
		}
	}

	// Fallback to simple string conversion
	return strings.TrimRight(string(value), "\x00")
}

// formatValueRaw formats value based on VR type.
func formatValueRaw(vr string, value []byte) string {
	vr = strings.TrimSpace(vr)

	// Binary VRs
	if vr == "OB" || vr == "OW" || vr == "OF" || vr == "OD" || vr == "OL" {
		return fmt.Sprintf("<%d bytes>", len(value))
	}

	// Numeric VRs - format as numbers
	switch vr {
	case "UL": // Unsigned Long (4 bytes)
		if len(value) == 4 {
			return fmt.Sprintf("%d", binary.LittleEndian.Uint32(value))
		}
	case "US": // Unsigned Short (2 bytes)
		if len(value) == 2 {
			return fmt.Sprintf("%d", binary.LittleEndian.Uint16(value))
		}
	case "SL": // Signed Long (4 bytes)
		if len(value) == 4 {
			return fmt.Sprintf("%d", int32(binary.LittleEndian.Uint32(value)))
		}
	case "SS": // Signed Short (2 bytes)
		if len(value) == 2 {
			return fmt.Sprintf("%d", int16(binary.LittleEndian.Uint16(value)))
		}
	case "FD": // Floating Point Double (8 bytes)
		if len(value) == 8 {
			bits := binary.LittleEndian.Uint64(value)
			return fmt.Sprintf("%f", math.Float64frombits(bits))
		}
	case "FL": // Floating Point Single (4 bytes)
		if len(value) == 4 {
			bits := binary.LittleEndian.Uint32(value)
			return fmt.Sprintf("%f", math.Float32frombits(bits))
		}
	}

	// Text-based VRs with charset support
	strVal := decodeTextWithCharset(value, vr)

	// Check if printable
	printable := true
	for _, r := range strVal {
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			printable = false
			break
		}
	}

	if printable {
		return strVal
	}

	// Binary representation
	return fmt.Sprintf("<%d bytes>", len(value))
}

// isPrivateTag checks if a tag is private (odd group number).
func isPrivateTag(tag string) bool {
	parts := strings.Split(tag, ",")
	if len(parts) != 2 {
		return false
	}

	var group uint16
	_, _ = fmt.Sscanf(parts[0], "%X", &group)
	return group%2 == 1
}

// extractDICOMInfo extracts key metadata from elements.
func extractDICOMInfo(elements []DicomElement) DicomInfo {
	info := DicomInfo{}

	for _, elem := range elements {
		switch elem.Tag {
		case "0010,0010": // PatientName
			info.PatientName = strings.TrimSpace(formatValueRaw("PN", elem.Value))
		case "0010,0020": // PatientID
			info.PatientID = strings.TrimSpace(formatValueRaw("LO", elem.Value))
		case "0008,0020": // StudyDate
			info.StudyDate = strings.TrimSpace(formatValueRaw("DA", elem.Value))
		case "0008,0060": // Modality
			info.Modality = strings.TrimSpace(formatValueRaw("CS", elem.Value))
		case "0008,1030": // StudyDescription
			info.StudyDescription = strings.TrimSpace(formatValueRaw("LO", elem.Value))
		case "0008,103E": // SeriesDescription
			info.SeriesDescription = strings.TrimSpace(formatValueRaw("LO", elem.Value))
		case "0020,1209": // NumberOfFrames
			if len(elem.Value) > 0 {
				_, _ = fmt.Sscanf(string(elem.Value), "%d", &info.NumberOfFrames)
			}
		case "0028,0010": // Rows
			if len(elem.Value) >= 2 {
				info.Rows = int(binary.LittleEndian.Uint16(elem.Value))
			}
		case "0028,0011": // Columns
			if len(elem.Value) >= 2 {
				info.Columns = int(binary.LittleEndian.Uint16(elem.Value))
			}
		}
	}

	return info
}

// convertToJSON converts DICOM elements to JSON format.
func convertToJSON(elements []DicomElement) ([]byte, error) {
	// Create a simple JSON structure
	jsonMap := make(map[string]interface{})

	for _, elem := range elements {
		// Skip sequences and complex types for basic JSON
		if elem.VR == "SQ" {
			continue
		}

		// Convert value to appropriate type
		var val interface{}
		switch elem.VR {
		case "OB", "OW", "OF", "OD", "OL":
			val = fmt.Sprintf("<%d bytes>", len(elem.Value))
		case "US", "UL", "SL", "SS": // Numeric
			val = formatValueRaw(elem.VR, elem.Value)
		default: // Text
			val = formatValueRaw(elem.VR, elem.Value)
		}

		// Use tag as key with element name as description
		key := elem.Name
		if key == "" {
			key = elem.Tag
		}

		jsonMap[key] = map[string]interface{}{
			"tag":   elem.Tag,
			"vr":    elem.VR,
			"value": val,
		}
	}

	return json.MarshalIndent(jsonMap, "", "  ")
}

// convertToCSV converts DICOM elements to CSV format.
func convertToCSV(elements []DicomElement) ([]byte, error) {
	buf := new(bytes.Buffer)
	writer := csv.NewWriter(buf)

	// Write header
	_ = writer.Write([]string{"Tag", "Name", "VR", "Length", "Value"})

	// Write data
	for _, elem := range elements {
		value := formatValue(elem.VR, elem.Value, 100)
		_ = writer.Write([]string{
			elem.Tag,
			elem.Name,
			elem.VR,
			fmt.Sprintf("%d", len(elem.Value)),
			value,
		})
	}

	writer.Flush()
	return buf.Bytes(), writer.Error()
}

// convertToNifti is a placeholder for NIfTI conversion.
func convertToNifti(elements []DicomElement) ([]byte, error) {
	// Basic NIfTI header structure
	// This is a minimal implementation - full NIfTI is complex

	// Find pixel data
	var pixelDataElem *DicomElement

	for i := range elements {
		if elements[i].Tag == "7FE0,0010" { // PixelData
			pixelDataElem = &elements[i]
			break
		}
	}

	if pixelDataElem == nil {
		return nil, fmt.Errorf("no pixel data found")
	}

	// Create a minimal NIfTI-like header (348 bytes fixed + image data)
	header := make([]byte, 348)

	// NIfTI magic string
	copy(header[344:348], []byte("n+1\x00"))

	// Combine header + pixel data
	result := append(header, pixelDataElem.Value...)
	return result, nil
}

// GetDICOMTagInfo retrieves dictionary metadata for a specific tag.
func GetDICOMTagInfo(tagStr string) (map[string]string, error) {
	// Parse tag string (format: "XXXX,XXXX")
	parts := strings.Split(tagStr, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid tag format: expected XXXX,XXXX, got %s", tagStr)
	}

	var group, element uint16
	_, err := fmt.Sscanf(parts[0], "%X", &group)
	if err != nil {
		return nil, fmt.Errorf("invalid group in tag: %s", parts[0])
	}

	_, err = fmt.Sscanf(parts[1], "%X", &element)
	if err != nil {
		return nil, fmt.Errorf("invalid element in tag: %s", parts[1])
	}

	// Create tag and get info from dictionary
	t := tag.New(group, element)
	info := t.GetInfo()

	result := make(map[string]string)
	result["Tag"] = tagStr
	result["Hex"] = t.Hex()

	if info == nil {
		result["Status"] = "Unknown"
		if t.IsPrivate() {
			result["Type"] = "Private"
		}
		return result, nil
	}

	result["Name"] = info.Name
	result["VR"] = info.VR
	result["VM"] = info.VM
	result["Keyword"] = info.Keyword

	if info.Retired {
		result["Status"] = "Retired"
	} else {
		result["Status"] = "Active"
	}

	if t.IsPrivate() {
		result["Type"] = "Private"
	} else {
		result["Type"] = "Standard"
	}

	return result, nil
}
