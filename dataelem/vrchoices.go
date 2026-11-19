package dataelem

import "strings"

// vrChoiceSeparator joins the alternatives of a dictionary VR that offers a
// choice, as the DICOM standard's data dictionary writes them: "OB or OW",
// "US or SS", "US or SS or OW" (PS3.6 Table 6-1, and the notes of Annex A).
const vrChoiceSeparator = " or "

// VRChoices returns the VRs a dictionary entry allows: one for an ordinary
// entry, several for one that offers a choice ("US or OW" gives US and OW).
//
// The choices are read from the dictionary's own string, not from a list kept
// beside it. Three packages kept such a list, each its own, and they disagreed:
// none of them had "US or OW" (LUT Data) or "US or SS or OW" (Gray Lookup Table
// Data) in all three, so a conformant element was reported as a VR mismatch.
func VRChoices(entry string) []string {
	return strings.Split(entry, vrChoiceSeparator)
}

// OffersVRChoice reports whether a dictionary entry offers a choice of VRs,
// as "OB or OW" does.
func OffersVRChoice(entry string) bool {
	return strings.Contains(entry, vrChoiceSeparator)
}

// VRMatchesDictionary reports whether an element's VR is one the dictionary's
// entry allows for its tag: the entry itself, or any one of the choices it
// offers. It is the one check behind the VR mismatch reported when an element
// is read, added to a data set or validated.
//
// The entry itself matches because that is what an element read from an
// Implicit VR file carries: its VR comes from the dictionary, and where the
// dictionary offers a choice the reader keeps the whole of it, "OB or OW" for
// Pixel Data. Compared with each choice only, such an element was reported as
// a mismatch with itself (#483).
func VRMatchesDictionary(actual, entry string) bool {
	if actual == entry {
		return true
	}
	for _, choice := range VRChoices(entry) {
		if actual == choice {
			return true
		}
	}
	return false
}
