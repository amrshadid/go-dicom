package filereader

import "github.com/amrshadid/go-dicom/tag"

// The parser is in internal/filereader; these are what the tests here used to
// borrow from it when it was this package.

// pixelDataTag is (7FE0,0010).
var pixelDataTag = tag.New(0x7FE0, 0x0010)

// valuePieceSize is the first piece a value from a stream of unknown size is
// read into (internal/filereader).
const valuePieceSize = 64 << 10

// isShortVR reports whether an explicit VR has a 2-byte length (PS3.5 7.1.2).
func isShortVR(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OV", "OW", "SQ", "SV", "UC", "UN", "UR", "UT", "UV":
		return false
	default:
		return true
	}
}

// describeEncoding names an encoding for a failure message.
func describeEncoding(explicitVR, littleEndian bool) string {
	vr, order := "implicit VR", "little endian"
	if explicitVR {
		vr = "explicit VR"
	}
	if !littleEndian {
		order = "big endian"
	}
	return vr + " " + order
}
