package dataelem_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// TestVRMatchesEveryChoiceTheDictionaryOffers proves that, for every entry of
// the standard dictionary, each VR the entry offers is accepted and a VR it does
// not offer is not. The entries are read from the dictionary, so a choice added
// to a later edition of the standard is covered without a list here to update:
// LUT Data (0028,3006) is "US or OW" and Gray Lookup Table Data (0028,1200) is
// "US or SS or OW", which a hand-written list had left out.
func TestVRMatchesEveryChoiceTheDictionaryOffers(t *testing.T) {
	choices := map[string]int{}
	check := func(label, entry string) {
		for _, vr := range standardVRs {
			offered := false
			for _, c := range dataelem.VRChoices(entry) {
				if c == string(vr) {
					offered = true
				}
			}
			if got := dataelem.VRMatchesDictionary(string(vr), entry); got != offered {
				t.Errorf("%s %q: VRMatchesDictionary(%s) = %v, want %v", label, entry, vr, got, offered)
			}
		}
		if dataelem.OffersVRChoice(entry) {
			choices[entry]++
		}
	}
	for tg, info := range tag.StandardDicomDictionary {
		check(tag.FromInt(tg).String(), info.VR)
	}
	for _, info := range tag.RepeaterDictionary {
		check(info.Mask, info.VR)
	}
	for _, entry := range []string{"OB or OW", "US or SS", "US or OW", "US or SS or OW"} {
		if choices[entry] == 0 {
			t.Errorf("the dictionary has no entry %q any more; this test no longer covers it", entry)
		}
	}
}

// TestLUTDataIsNotAVRMismatch is the case of the report: LUT Data, as US or as
// OW, and Gray Lookup Table Data in each of its three VRs.
func TestLUTDataIsNotAVRMismatch(t *testing.T) {
	for _, tc := range []struct {
		entry, vr string
		want      bool
	}{
		{"US or OW", "US", true}, {"US or OW", "OW", true}, {"US or OW", "SS", false}, {"US or OW", "OB", false},
		{"US or SS or OW", "US", true}, {"US or SS or OW", "SS", true}, {"US or SS or OW", "OW", true},
		{"US or SS or OW", "OB", false}, {"US", "US", true}, {"US", "SS", false},
	} {
		if got := dataelem.VRMatchesDictionary(tc.vr, tc.entry); got != tc.want {
			t.Errorf("VRMatchesDictionary(%q, %q) = %v, want %v", tc.vr, tc.entry, got, tc.want)
		}
	}
	if got := tag.GlobalDictionary().GetVR(tag.New(0x0028, 0x3006)); got != "US or OW" {
		t.Errorf("the dictionary's VR for LUT Data is %q, want \"US or OW\"", got)
	}
}

// TestElementValidationAcceptsEveryVROfLUTData proves the check on a data
// element itself, in the mode that turns a mismatch into an error, takes LUT
// Data as US and as OW, Smallest Image Pixel Value as US and as SS, and Pixel
// Data as OB and as OW, and still refuses a VR the dictionary does not offer.
func TestElementValidationAcceptsEveryVROfLUTData(t *testing.T) {
	settings := config.Get()
	saved := settings.GetReadingValidationMode()
	settings.SetReadingValidationMode(config.RAISE)
	defer settings.SetReadingValidationMode(saved)

	lutData, smallest, pixelData := tag.New(0x0028, 0x3006), tag.New(0x0028, 0x0106), tag.New(0x7FE0, 0x0010)
	for _, tc := range []struct {
		tg      tag.Tag
		vr      dataelem.VR
		refused bool
	}{
		{lutData, dataelem.US, false}, {lutData, dataelem.OW, false}, {lutData, dataelem.SS, true},
		{smallest, dataelem.US, false}, {smallest, dataelem.SS, false}, {smallest, dataelem.OW, true},
		{pixelData, dataelem.OB, false}, {pixelData, dataelem.OW, false}, {pixelData, dataelem.US, true},
	} {
		err := dataelem.NewDataElement(tc.tg, tc.vr, []byte{0, 0}).ValidateWithConfig(true)
		mismatch := err != nil && strings.Contains(err.Error(), "VR mismatch")
		if mismatch != tc.refused {
			t.Errorf("%s as %s: VR mismatch = %v (err %v), want %v", tc.tg, tc.vr, mismatch, err, tc.refused)
		}
	}
}
