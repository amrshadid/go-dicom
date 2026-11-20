package valuerep_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/valuerep"
)

// TestEveryStandardVRIsValid proves IsValidVR takes every VR PS3.5 6.2 defines,
// the 64-bit OV, SV and UV among them, which it refused, by checking it against
// the list package dataelem holds.
func TestEveryStandardVRIsValid(t *testing.T) {
	for _, vr := range []dataelem.VR{
		dataelem.AE, dataelem.AS, dataelem.AT, dataelem.CS, dataelem.DA, dataelem.DS, dataelem.DT,
		dataelem.FD, dataelem.FL, dataelem.IS, dataelem.LO, dataelem.LT, dataelem.OB, dataelem.OD,
		dataelem.OF, dataelem.OL, dataelem.OV, dataelem.OW, dataelem.PN, dataelem.SH, dataelem.SL,
		dataelem.SQ, dataelem.SS, dataelem.ST, dataelem.SV, dataelem.TM, dataelem.UC, dataelem.UI,
		dataelem.UL, dataelem.UN, dataelem.UR, dataelem.US, dataelem.UT, dataelem.UV,
	} {
		if !valuerep.IsValidVR(string(vr)) {
			t.Errorf("IsValidVR(%q) = false", vr)
		}
	}
	for _, code := range []string{"", "XX", "ov", "U"} {
		if valuerep.IsValidVR(code) {
			t.Errorf("IsValidVR(%q) = true", code)
		}
	}
}
