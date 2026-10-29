package dataelem_test

import (
	"sort"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
)

// standardVRs is PS3.5 Table 6.2-1, all 34.
var standardVRs = []dataelem.VR{
	"AE", "AS", "AT", "CS", "DA", "DS", "DT", "FD", "FL", "IS", "LO", "LT",
	"OB", "OD", "OF", "OL", "OV", "OW", "PN", "SH", "SL", "SQ", "SS", "ST",
	"SV", "TM", "UC", "UI", "UL", "UN", "UR", "US", "UT", "UV",
}

// TestEveryStandardVRIsKnown: US was missing from the VR table, so
// IsValidVR(US) was false and GetVRInfo(US) nil, and anything that sized or
// validated a value by its VR skipped every US. AllVRs also left out US and the
// 64-bit VRs OV, SV and UV (#201).
func TestEveryStandardVRIsKnown(t *testing.T) {
	for _, vr := range standardVRs {
		if dataelem.GetVRInfo(vr) == nil || !dataelem.IsValidVR(vr) {
			t.Errorf("%s is not in the VR table", vr)
		}
	}
	if info := dataelem.GetVRInfo(dataelem.US); info == nil || info.ByteLength != 2 || !info.IsNumeric {
		t.Errorf("US info is %+v, want a 2-byte numeric VR", info)
	}

	got := append([]dataelem.VR(nil), dataelem.AllVRs()...)
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != len(standardVRs) {
		t.Fatalf("AllVRs has %d VRs, want the %d of PS3.5 Table 6.2-1: %v", len(got), len(standardVRs), got)
	}
	for i := range got {
		if got[i] != standardVRs[i] {
			t.Fatalf("AllVRs is %v, want %v", got, standardVRs)
		}
	}
}
