package dataelem

import "testing"

// IsValidVR and IsTextVR answer from the table the file parser shares, and
// GetVRInfo from its own; the two must describe the same VRs.
func TestVRFactsAgreeWithGetVRInfo(t *testing.T) {
	for _, v := range AllVRs() {
		info := GetVRInfo(v)
		if info == nil || !IsValidVR(v) {
			t.Errorf("%s: GetVRInfo %v, IsValidVR %v", v, info, IsValidVR(v))
			continue
		}
		if info.IsText != IsTextVR(v) {
			t.Errorf("%s: GetVRInfo says text %v, IsTextVR %v", v, info.IsText, IsTextVR(v))
		}
	}
	for _, v := range []VR{"", "XX", "ob", "OB or OW"} {
		if IsValidVR(v) || GetVRInfo(v) != nil {
			t.Errorf("%q is taken for a VR", v)
		}
	}
}
