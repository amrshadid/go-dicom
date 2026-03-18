package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"unicode"

	"github.com/amrshadid/go-dicom/charset"
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

// readDICOMFile reads a DICOM file and returns its elements.
func readDICOMFile(filename string) ([]DicomElement, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var elements []DicomElement

	preamble := make([]byte, 132)
	n, err := file.Read(preamble)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read preamble: %w", err)
	}

	if n == 132 && string(preamble[128:132]) == "DICM" {
	} else {
		_, _ = file.Seek(0, io.SeekStart)
	}

	// Read elements
	buffer := make([]byte, 65536)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read error: %w", err)
		}
		if n == 0 {
			break
		}

		// Parse buffer for DICOM elements
		elems := parseElements(buffer[:n])
		elements = append(elements, elems...)

		if err == io.EOF {
			break
		}
	}

	return elements, nil
}

// parseElements parses raw bytes into DICOM elements.
func parseElements(data []byte) []DicomElement {
	var elements []DicomElement
	buf := bytes.NewReader(data)

	for buf.Len() >= 8 {
		// Read group and element (4 bytes total)
		groupBytes := make([]byte, 2)
		elemBytes := make([]byte, 2)

		if _, err := buf.Read(groupBytes); err != nil {
			break
		}
		if _, err := buf.Read(elemBytes); err != nil {
			break
		}

		group := binary.LittleEndian.Uint16(groupBytes)
		element := binary.LittleEndian.Uint16(elemBytes)

		// Skip if not a valid tag range
		if group == 0 && element == 0 {
			_, _ = buf.ReadByte()
			continue
		}

		// Read VR (2 bytes if explicit)
		vrBytes := make([]byte, 2)
		if _, err := buf.Read(vrBytes); err != nil {
			break
		}

		vr := string(vrBytes)

		// Read length
		var length uint32
		if isShortVR(vr) {
			// Two-byte length for short VR
			lenBytes := make([]byte, 2)
			if _, err := buf.Read(lenBytes); err != nil {
				break
			}
			length = uint32(binary.LittleEndian.Uint16(lenBytes))
		} else {
			// Skip 2 reserved bytes and read 4-byte length
			reserved := make([]byte, 2)
			_, _ = buf.Read(reserved)
			lenBytes := make([]byte, 4)
			if _, err := buf.Read(lenBytes); err != nil {
				break
			}
			length = binary.LittleEndian.Uint32(lenBytes)
		}

		// Read value
		if length > 0 && length < 100000000 { // Sanity check (100MB max)
			value := make([]byte, length)
			n, err := buf.Read(value)
			if err != nil && err != io.EOF {
				break
			}
			if n != int(length) {
				// Incomplete read, skip
				break
			}

			tag := fmt.Sprintf("%04X,%04X", group, element)
			elem := DicomElement{
				Tag:   tag,
				Name:  getElementName(group, element),
				VR:    strings.TrimRight(vr, "\x00"),
				Value: value,
				Depth: 0,
			}
			elements = append(elements, elem)
		}
	}

	return elements
}

// isShortVR returns true if VR has 2-byte length field (not 4-byte).
func isShortVR(vr string) bool {
	short := map[string]bool{
		"AE": true, "AS": true, "AT": true, "CS": true, "DA": true,
		"DS": true, "DT": true, "FD": true, "FL": true, "IS": true,
		"LO": true, "LT": true, "PN": true, "SH": true, "SL": true,
		"SQ": true, "SS": true, "ST": true, "TM": true, "UI": true,
		"UL": true, "US": true, "UT": true,
	}
	return short[strings.TrimSpace(vr)]
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
