package charset

import (
	"sync"
)

// ESC is the escape character (0x1B) used for ISO 2022 escape sequences.
const ESC byte = 0x1B

var (
	encodingMapOnce sync.Once

	dicomToGoEncoding  map[string]string
	goToDicomEncoding  map[string]string
	encodingInfoMap    map[string]*EncodingInfo
	goEncodingInfoMap  map[string]*EncodingInfo
	escapeToGoEncoding map[string]string
	goEncodingToEscape map[string][]byte
)

// initializeEncodingMaps initializes all encoding mapping tables via sync.Once.
func initializeEncodingMaps() {
	encodingMapOnce.Do(func() {
		// Build the encoding info map first
		encodingInfoMap = make(map[string]*EncodingInfo)
		goEncodingInfoMap = make(map[string]*EncodingInfo)

		for _, info := range encodingInfoList {
			encodingInfoMap[info.DicomName] = info
			goEncodingInfoMap[info.GoEncoding] = info
		}

		// Build DICOM <-> Go encoding mappings
		dicomToGoEncoding = make(map[string]string)
		goToDicomEncoding = make(map[string]string)

		for _, info := range encodingInfoList {
			dicomToGoEncoding[info.DicomName] = info.GoEncoding
			goToDicomEncoding[info.GoEncoding] = info.DicomName
		}

		// Build escape sequence mappings
		escapeToGoEncoding = make(map[string]string)
		goEncodingToEscape = make(map[string][]byte)

		for _, info := range encodingInfoList {
			if len(info.EscapeSequence) > 0 {
				escKey := string(info.EscapeSequence)
				escapeToGoEncoding[escKey] = info.GoEncoding
				goEncodingToEscape[info.GoEncoding] = info.EscapeSequence
			}
		}
	})
}

// encodingInfoList contains all supported DICOM character encodings.
var encodingInfoList = []*EncodingInfo{
	// Default and ASCII
	{
		DicomName:      "",
		GoEncoding:     "ISO-8859-1",
		Type:           EncodingTypeSingleByte,
		Description:    "Default repertoire (ISO-IR 6)",
		EscapeSequence: []byte{ESC, '(', 'B'},
	},
	{
		DicomName:      "ISO_IR 6",
		GoEncoding:     "ISO-8859-1",
		Type:           EncodingTypeSingleByte,
		Description:    "Default repertoire (ASCII)",
		EscapeSequence: []byte{ESC, '(', 'B'},
	},
	{
		DicomName:      "ISO 2022 IR 6",
		GoEncoding:     "ISO-8859-1",
		Type:           EncodingTypeSingleByte,
		Description:    "Default repertoire with code extensions",
		EscapeSequence: []byte{ESC, '(', 'B'},
	},
	// Western European (Latin-1 to Latin-4)
	{
		DicomName:      "ISO_IR 100",
		GoEncoding:     "ISO-8859-1",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 1 (Western European)",
		EscapeSequence: nil, // No escape for single-byte without code extensions
	},
	{
		DicomName:      "ISO 2022 IR 100",
		GoEncoding:     "ISO-8859-1",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 1 with code extensions",
		EscapeSequence: []byte{ESC, '-', 'A'},
	},
	{
		DicomName:      "ISO_IR 101",
		GoEncoding:     "ISO-8859-2",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 2 (Central European)",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 101",
		GoEncoding:     "ISO-8859-2",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 2 with code extensions",
		EscapeSequence: []byte{ESC, '-', 'B'},
	},
	{
		DicomName:      "ISO_IR 109",
		GoEncoding:     "ISO-8859-3",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 3 (South European)",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 109",
		GoEncoding:     "ISO-8859-3",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 3 with code extensions",
		EscapeSequence: []byte{ESC, '-', 'C'},
	},
	{
		DicomName:      "ISO_IR 110",
		GoEncoding:     "ISO-8859-4",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 4 (North European)",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 110",
		GoEncoding:     "ISO-8859-4",
		Type:           EncodingTypeSingleByte,
		Description:    "Latin alphabet No. 4 with code extensions",
		EscapeSequence: []byte{ESC, '-', 'D'},
	},

	// Greek
	{
		DicomName:      "ISO_IR 126",
		GoEncoding:     "ISO-8859-7",
		Type:           EncodingTypeSingleByte,
		Description:    "Greek",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 126",
		GoEncoding:     "ISO-8859-7",
		Type:           EncodingTypeSingleByte,
		Description:    "Greek with code extensions",
		EscapeSequence: []byte{ESC, '-', 'F'},
	},

	// Arabic
	{
		DicomName:      "ISO_IR 127",
		GoEncoding:     "ISO-8859-6",
		Type:           EncodingTypeSingleByte,
		Description:    "Arabic",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 127",
		GoEncoding:     "ISO-8859-6",
		Type:           EncodingTypeSingleByte,
		Description:    "Arabic with code extensions",
		EscapeSequence: []byte{ESC, '-', 'G'},
	},

	// Hebrew
	{
		DicomName:      "ISO_IR 138",
		GoEncoding:     "ISO-8859-8",
		Type:           EncodingTypeSingleByte,
		Description:    "Hebrew",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 138",
		GoEncoding:     "ISO-8859-8",
		Type:           EncodingTypeSingleByte,
		Description:    "Hebrew with code extensions",
		EscapeSequence: []byte{ESC, '-', 'H'},
	},

	// Cyrillic (Russian)
	{
		DicomName:      "ISO_IR 144",
		GoEncoding:     "ISO-8859-5",
		Type:           EncodingTypeSingleByte,
		Description:    "Cyrillic",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 144",
		GoEncoding:     "ISO-8859-5",
		Type:           EncodingTypeSingleByte,
		Description:    "Cyrillic with code extensions",
		EscapeSequence: []byte{ESC, '-', 'L'},
	},

	// Turkish
	{
		DicomName:      "ISO_IR 148",
		GoEncoding:     "ISO-8859-9",
		Type:           EncodingTypeSingleByte,
		Description:    "Turkish",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 148",
		GoEncoding:     "ISO-8859-9",
		Type:           EncodingTypeSingleByte,
		Description:    "Turkish with code extensions",
		EscapeSequence: []byte{ESC, '-', 'M'},
	},

	// Thai
	{
		DicomName:      "ISO_IR 166",
		GoEncoding:     "ISO-8859-11",
		Type:           EncodingTypeSingleByte,
		Description:    "Thai",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 166",
		GoEncoding:     "ISO-8859-11",
		Type:           EncodingTypeSingleByte,
		Description:    "Thai with code extensions",
		EscapeSequence: []byte{ESC, '-', 'T'},
	},

	// Japanese - Romaji and Katakana
	{
		DicomName:      "ISO_IR 13",
		GoEncoding:     "Shift_JIS",
		Type:           EncodingTypeMultiByte,
		Description:    "Japanese (Romaji and Katakana)",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 IR 13",
		GoEncoding:     "Shift_JIS",
		Type:           EncodingTypeMultiByte,
		Description:    "Japanese (Katakana) with code extensions",
		EscapeSequence: []byte{ESC, ')', 'I'},
	},

	// Japanese - Kanji (JIS X 0208)
	{
		DicomName:          "ISO 2022 IR 87",
		GoEncoding:         "ISO-2022-JP",
		Type:               EncodingTypeMultiByte,
		Description:        "Japanese (Kanji - JIS X 0208)",
		EscapeSequence:     []byte{ESC, '$', 'B'},
		RequiresTailEscape: true,
	},

	// Japanese - Extended Kanji (JIS X 0212)
	{
		DicomName:          "ISO 2022 IR 159",
		GoEncoding:         "ISO-2022-JP",
		Type:               EncodingTypeMultiByte,
		Description:        "Japanese (Supplementary Kanji - JIS X 0212)",
		EscapeSequence:     []byte{ESC, '$', '(', 'D'},
		RequiresTailEscape: true,
	},

	// Korean
	{
		DicomName:      "ISO 2022 IR 149",
		GoEncoding:     "EUC-KR",
		Type:           EncodingTypeMultiByte,
		Description:    "Korean (KS X 1001)",
		EscapeSequence: []byte{ESC, '$', ')', 'C'},
	},

	// Chinese Simplified
	{
		DicomName:      "ISO 2022 IR 58",
		GoEncoding:     "ISO-2022-CN",
		Type:           EncodingTypeMultiByte,
		Description:    "Chinese Simplified (GB2312)",
		EscapeSequence: []byte{ESC, '$', ')', 'A'},
	},

	// Unicode UTF-8 (Stand-alone)
	{
		DicomName:      "ISO_IR 192",
		GoEncoding:     "UTF-8",
		Type:           EncodingTypeStandAlone,
		Description:    "Unicode in UTF-8",
		EscapeSequence: nil,
	},

	// Chinese GB18030 (Stand-alone)
	{
		DicomName:      "GB18030",
		GoEncoding:     "GB18030",
		Type:           EncodingTypeStandAlone,
		Description:    "Chinese (GB18030)",
		EscapeSequence: nil,
	},

	// Chinese GBK (Stand-alone)
	{
		DicomName:      "GBK",
		GoEncoding:     "GBK",
		Type:           EncodingTypeStandAlone,
		Description:    "Chinese (GBK)",
		EscapeSequence: nil,
	},
	{
		DicomName:      "ISO 2022 GBK",
		GoEncoding:     "GBK",
		Type:           EncodingTypeStandAlone,
		Description:    "Chinese (GBK) with code extensions",
		EscapeSequence: nil,
	},
}

// GetEncodingInfo returns the EncodingInfo for a DICOM character set value.
func GetEncodingInfo(dicomName string) *EncodingInfo {
	initializeEncodingMaps()
	return encodingInfoMap[dicomName]
}

// GetEncodingInfoByGoName returns the EncodingInfo for a Go encoding name.
func GetEncodingInfoByGoName(goEncoding string) *EncodingInfo {
	initializeEncodingMaps()
	return goEncodingInfoMap[goEncoding]
}

// DicomToGoEncoding converts a DICOM character set value to a Go encoding name.
func DicomToGoEncoding(dicomName string) string {
	initializeEncodingMaps()
	return dicomToGoEncoding[dicomName]
}

// GoToDicomEncoding converts a Go encoding name to a DICOM character set value.
func GoToDicomEncoding(goEncoding string) string {
	initializeEncodingMaps()
	return goToDicomEncoding[goEncoding]
}

// EscapeToGoEncoding converts an ISO 2022 escape sequence to a Go encoding name.
func EscapeToGoEncoding(escapeSeq []byte) string {
	initializeEncodingMaps()
	return escapeToGoEncoding[string(escapeSeq)]
}

// GoEncodingToEscape returns the ISO 2022 escape sequence for a Go encoding name.
func GoEncodingToEscape(goEncoding string) []byte {
	initializeEncodingMaps()
	return goEncodingToEscape[goEncoding]
}

// IsStandAloneEncoding returns true if the encoding is stand-alone.
func IsStandAloneEncoding(dicomName string) bool {
	info := GetEncodingInfo(dicomName)
	return info != nil && info.IsStandAlone()
}

// RequiresTailEscape returns true if the encoding requires a tail escape sequence.
func RequiresTailEscape(goEncoding string) bool {
	info := GetEncodingInfoByGoName(goEncoding)
	return info != nil && info.RequiresTailEscape
}
