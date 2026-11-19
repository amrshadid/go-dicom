package dataelem

import vrfacts "github.com/amrshadid/go-dicom/internal/vr"

// VRChoices returns the VRs a dictionary entry allows: one for an ordinary
// entry, several for one that offers a choice ("US or OW" gives US and OW).
//
// The choices are read from the dictionary's own string, not from a list kept
// beside it. Three packages kept such a list, each its own, and they disagreed:
// none of them had "US or OW" (LUT Data) or "US or SS or OW" (Gray Lookup Table
// Data) in all three, so a conformant element was reported as a VR mismatch.
func VRChoices(entry string) []string {
	return vrfacts.Choices(entry)
}

// OffersVRChoice reports whether a dictionary entry offers a choice of VRs,
// as "OB or OW" does.
func OffersVRChoice(entry string) bool {
	return vrfacts.OffersChoice(entry)
}

// VRMatchesDictionary reports whether an element's VR is one the dictionary's
// entry allows for its tag: the entry itself, or any one of the choices it
// offers. It is the one check behind the VR mismatch reported when an element
// is read, added to a data set or validated.
func VRMatchesDictionary(actual, entry string) bool {
	return vrfacts.MatchesDictionary(actual, entry)
}
