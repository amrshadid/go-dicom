package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/dataset"
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
	// Encodings are the character encodings the value's text is in, as
	// filereader.TextEncodings gives them for the data set it is in.
	Encodings []string
	// Length is how many bytes the value is in the file. It is more than
	// len(Value) for a bulk value listed for display, of which Value is
	// only the start.
	Length int
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
	return readDICOMElements(filename, listing{})
}

// shownBytes is how much of a value the table prints at most.
const shownBytes = 100

// listing says how readDICOMElements lists a file. The zero value reads every
// value whole, which is what a command that works with the values needs.
type listing struct {
	// skipBulk leaves bulk values in the file. The table and the summary
	// print the first hundred bytes of a value at most, and read all of it to
	// do so: a 200 MB image took 200 MB to show one line of (#274), and to
	// print a few lines of info (#328). Only the part that is printed is
	// read; Length still says how long the value is.
	skipBulk bool
	// metaGroupLength puts the File Meta Information Group Length (0002,0000)
	// first, as it leads the file. The element is not content: it counts the
	// bytes of the group it opens, and a writer works it out. What converts a
	// file or generates code to rebuild one must not carry the old count
	// along, and info has never listed it, so only show asks for it.
	metaGroupLength bool
	// whole holds every value that can be printed whole, for a listing that
	// is not cut. Bulk binary still stays in the file with skipBulk: it is
	// printed as a count of bytes either way.
	whole bool
}

// readDICOMElements is readDICOMFile, listed as how says.
func readDICOMElements(filename string, how listing) ([]DicomElement, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	options := filereader.ReadOptions{SkipBulkData: how.skipBulk, BulkDataThreshold: shownBytes}
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(file), options)
	if err != nil {
		return nil, fmt.Errorf("failed to read DICOM file: %w", err)
	}
	// Where the start of a value left in the file is read from: the file, or for
	// a deflated data set, whose positions count inflated bytes, a source that
	// inflates the stream up to the value.
	root := &displayScope{deciding: dataset.NewDataset(), source: df.ValueSource(file), whole: how.whole}

	// The file meta header first, as it appears in the file, then the data set.
	elements := make([]DicomElement, 0, 1+len(df.MetaElements)+len(df.DataElements))
	if how.metaGroupLength && df.MetaGroupLength != nil {
		elements = appendElements(elements, []*filereader.DataElementValue{df.MetaGroupLength}, 0, root)
	}
	elements = appendElements(elements, df.MetaElements, 0, root)
	elements = appendElements(elements, df.DataElements, 0, root)

	// A file that yielded nothing is not a DICOM file, and saying so is more use than
	// printing an empty table.
	//
	// The reader is deliberately tolerant — it recovers what it can from a truncated
	// or malformed file, which is the right behavior for salvage. But it returns no
	// error for a file with nothing recoverable in it, so `show /etc/hosts` printed a
	// header, a column heading, no rows, and exited zero; `show /dev/null` did the
	// same without even a warning. A mistyped filename is the common case, and it
	// looked exactly like success.
	//
	// Every conformant DICOM file has at least one element, so an empty result is
	// unambiguous.
	if len(elements) == 0 {
		return nil, fmt.Errorf("%s does not look like a DICOM file: no data elements "+
			"could be read from it", filename)
	}

	return elements, nil
}

// appendElements flattens parsed elements for display, descending into
// sequences and recording the nesting level in Depth.
//
// outer is the scope of the data set these elements are an item of, nil at
// the top.
func appendElements(out []DicomElement, elems []*filereader.DataElementValue, depth int, outer *displayScope) []DicomElement {
	scope := newDisplayScope(elems, outer)
	for _, e := range elems {
		out = append(out, DicomElement{
			Tag:       fmt.Sprintf("%04X,%04X", e.Tag.Group(), e.Tag.Element()),
			Name:      getElementName(e.Tag.Group(), e.Tag.Element()),
			VR:        scope.vr(e),
			Value:     scope.shown(e),
			Depth:     depth,
			Encodings: scope.encodings,
			Length:    valueBytes(e),
		})

		// Sequence items nest one level deeper than the sequence itself.
		for _, item := range e.Items {
			out = appendElements(out, item.Elements, depth+1, scope)
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
	return formatValueIn(vr, value, maxLen, nil)
}

// bulkVRs are the VRs whose values are bytes, words and floats in bulk. The
// table gives their length and nothing of them.
var bulkVRs = map[string]bool{"OB": true, "OW": true, "OF": true, "OD": true, "OL": true, "OV": true}

// display is the element's value as show prints it. With a limit it is all
// of a short value, and the start of a long one with how long it is. With
// none, limit 0, it is the whole value.
func (e DicomElement) display(limit int) string {
	if e.Length == 0 {
		return ""
	}
	if limit <= 0 {
		if bulkVRs[strings.TrimSpace(e.VR)] {
			return fmt.Sprintf("<%d bytes>", e.Length)
		}
		return formatValueRawIn(e.VR, e.Value, e.Encodings)
	}
	value := e.Value
	if len(value) > limit {
		value = value[:limit]
	}
	if e.Length > limit {
		return fmt.Sprintf("%s... (%d bytes)", formatValueRawIn(e.VR, value, e.Encodings), e.Length)
	}
	return formatValueRawIn(e.VR, value, e.Encodings)
}

// formatValueIn is formatValue for a value whose text is in encodings.
func formatValueIn(vr string, value []byte, maxLen int, encodings []string) string {
	if len(value) == 0 {
		return ""
	}

	// Limit length for display
	if len(value) > maxLen {
		return fmt.Sprintf("%s... (%d bytes)", formatValueRawIn(vr, value[:maxLen], encodings), len(value))
	}

	return formatValueRawIn(vr, value, encodings)
}

// decodeTextWithCharset decodes text value with charset support
//
// encodings are those of the data set the value is in. With none it is
// read as Latin-1, which is what every value was read as before, whatever
// its file declared (#270).
func decodeTextWithCharset(value []byte, vr string, encodings []string) string {
	if len(value) == 0 {
		return ""
	}
	if len(encodings) > 0 {
		return strings.TrimRight(filereader.DecodeText(vr, value, encodings), "\x00")
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
	return formatValueRawIn(vr, value, nil)
}

// formatValueRawIn is formatValueRaw for a value whose text is in encodings.
func formatValueRawIn(vr string, value []byte, encodings []string) string {
	vr = strings.TrimSpace(vr)

	// Bytes are counted, never printed.
	if bulkVRs[vr] {
		return fmt.Sprintf("<%d bytes>", len(value))
	}
	if text, _, ok := formatNumbers(vr, value); ok {
		return text
	}

	// Text-based VRs with charset support
	strVal := decodeTextWithCharset(value, vr, encodings)

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
//
// Only the data set's own elements are looked at, not those of the items of
// its sequences. The list is flat, so an Icon Image Sequence item's Rows and
// Columns, or an Other Patient IDs Sequence item's Patient ID, came after the
// top-level ones and replaced them: info gave the icon's size as the image's
// (#335).
func extractDICOMInfo(elements []DicomElement) DicomInfo {
	info := DicomInfo{}

	for _, elem := range elements {
		if elem.Depth > 0 {
			continue
		}
		switch elem.Tag {
		case "0010,0010": // PatientName
			info.PatientName = strings.TrimSpace(formatValueRawIn("PN", elem.Value, elem.Encodings))
		case "0010,0020": // PatientID
			info.PatientID = strings.TrimSpace(formatValueRawIn("LO", elem.Value, elem.Encodings))
		case "0008,0020": // StudyDate
			info.StudyDate = strings.TrimSpace(formatValueRaw("DA", elem.Value))
		case "0008,0060": // Modality
			info.Modality = strings.TrimSpace(formatValueRaw("CS", elem.Value))
		case "0008,1030": // StudyDescription
			info.StudyDescription = strings.TrimSpace(formatValueRawIn("LO", elem.Value, elem.Encodings))
		case "0008,103E": // SeriesDescription
			info.SeriesDescription = strings.TrimSpace(formatValueRawIn("LO", elem.Value, elem.Encodings))
		case "0028,0008": // NumberOfFrames, an IS: decimal digits as text
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
			val = formatValueRawIn(elem.VR, elem.Value, elem.Encodings)
		default: // Text
			val = formatValueRawIn(elem.VR, elem.Value, elem.Encodings)
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
		value := formatValueIn(elem.VR, elem.Value, 100, elem.Encodings)
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
