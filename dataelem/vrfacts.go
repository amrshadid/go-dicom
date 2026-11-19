package dataelem

import vrfacts "github.com/amrshadid/go-dicom/internal/vr"

// These answer from internal/vr, which the file parser shares, so that
// what is a VR and what is text is decided in one place. GetVRInfo's table
// describes the same VRs, and a test holds the two to each other.

// IsValidVR checks if a VR string is valid.
func IsValidVR(vr VR) bool {
	return vrfacts.IsValid(string(vr))
}

// IsTextVR checks if a VR is text-based.
func IsTextVR(vr VR) bool {
	return vrfacts.IsText(string(vr))
}

// IsCharacterSetVR reports whether Specific Character Set (0008,0005) applies
// to values of the VR: SH, LO, UC, ST, LT, UT and PN (PS3.5 6.1.2.3, Table
// 6.2-1). The other text VRs are in the default repertoire whatever it says.
func IsCharacterSetVR(vr VR) bool {
	return vrfacts.IsCharacterSet(string(vr))
}
