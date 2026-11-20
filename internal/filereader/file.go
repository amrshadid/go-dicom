package filereader

import "strings"

// File is a parsed DICOM file: preamble, meta header and data set, as the
// file holds them. filereader.DICOMFile has the same fields and adds what
// builds a data set from them.
type File struct {
	FileMetaInfo   *FileMetaInfo
	DataElements   []*DataElementValue
	ExplicitVR     bool
	IsLittleEndian bool

	// HasPreamble reports whether the file began with the 128-byte preamble and
	// the DICM prefix. False means a raw data set with no file meta header,
	// which is parsed as implicit VR little endian.
	HasPreamble bool

	// MetaElements holds the group-0002 file meta elements as they appeared in
	// the file, including any without a corresponding FileMetaInfo field.
	// FileMetaInfo carries the recognized ones in typed form.
	MetaElements []*DataElementValue

	// MetaGroupLength is the File Meta Information Group Length element
	// (0002,0000), which MetaElements leaves out, or nil for a file written
	// without one.
	MetaGroupLength *DataElementValue

	// DataSetOffset is where the data set begins in the file: after the
	// file meta group, or 0 for a file with none.
	DataSetOffset int64

	// Deflated reports that the data set was inflated before it was read
	// (PS3.5 A.5). The positions of its elements then count from the first
	// byte of the inflated data set, not of the file.
	Deflated bool

	// Warnings collects non-fatal issues found while parsing (unknown tags,
	// retired tags, VR mismatches, truncated meta elements). Parsing continues
	// past these; inspect the slice to surface them.
	Warnings []string

	// TagOrderViolations are the data elements, up to a thousand, whose tag
	// is not greater than the one before them in the same data set (PS3.5
	// 7.1), in the order read; TagOrderViolationCount counts them all. Each
	// is also among Warnings, in words; these are for a program to act on.
	TagOrderViolations     []TagOrderViolation
	TagOrderViolationCount int
}

// ParseSpecificCharacterSet splits a Specific Character Set (0008,0005)
// value into its defined terms, each trimmed; a value may name several,
// separated by backslashes (PS3.3 C.12.1.1.2).
func ParseSpecificCharacterSet(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := strings.Split(value, "\\")

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		result = append(result, trimmed)
	}

	return result
}
