// Package vr holds what the library knows about Value Representations that
// the file parser needs: which VRs exist, which are text, which a Specific
// Character Set applies to, how wide a number of each is, and what a
// dictionary entry offering a choice allows (PS3.5 6.2, PS3.6 6).
//
// It is the one place these facts live. dataelem's exported functions
// (IsValidVR, IsTextVR, SwapByteOrder, VRChoices and the rest) answer from
// here, and so does the file parser, which must not import dataelem: dataelem
// brings waveforms with it, and a program that only reads attributes should
// not link more than the parser.
package vr

import "strings"

// all are the standard VRs (PS3.5 Table 6.2-1).
var all = map[string]bool{
	"AE": true, "AS": true, "AT": true, "CS": true, "DA": true, "DS": true, "DT": true,
	"FD": true, "FL": true, "IS": true, "LO": true, "LT": true, "OB": true, "OD": true,
	"OF": true, "OL": true, "OV": true, "OW": true, "PN": true, "SH": true, "SL": true,
	"SQ": true, "SS": true, "ST": true, "SV": true, "TM": true, "UC": true, "UI": true,
	"UL": true, "UN": true, "UR": true, "US": true, "UT": true, "UV": true,
}

// text are the VRs whose values are character strings.
var text = map[string]bool{
	"AE": true, "AS": true, "CS": true, "DA": true, "DS": true, "DT": true, "IS": true,
	"LO": true, "LT": true, "PN": true, "SH": true, "ST": true, "TM": true, "UC": true,
	"UI": true, "UR": true, "UT": true,
}

// IsValid reports whether vr is a standard VR.
func IsValid(vr string) bool { return all[vr] }

// IsText reports whether a value of vr is a character string.
func IsText(vr string) bool { return text[vr] }

// IsCharacterSet reports whether Specific Character Set (0008,0005) applies
// to values of vr: SH, LO, UC, ST, LT, UT and PN (PS3.5 6.1.2.3, Table
// 6.2-1). The other text VRs are in the default repertoire whatever it says.
func IsCharacterSet(vr string) bool {
	switch vr {
	case "SH", "LO", "UC", "ST", "LT", "UT", "PN":
		return true
	}
	return false
}

// Widths maps each VR whose value is a sequence of multi-byte numbers to the
// width of one number. Text VRs and byte-oriented VRs (OB, UN) carry no byte
// order and are absent. AT is a pair of 16-bit values (group then element),
// so it swaps as 2-byte units like US.
var Widths = map[string]int{
	"US": 2, "SS": 2, "OW": 2, "AT": 2,
	"UL": 4, "SL": 4, "FL": 4, "OL": 4, "OF": 4,
	"FD": 8, "OD": 8, "SV": 8, "UV": 8, "OV": 8,
}

// SwapBytes reverses each width-byte unit of value in place. A width below
// two, or a value shorter than one unit, is left alone.
func SwapBytes(value []byte, width int) {
	if width < 2 || len(value) < width {
		return
	}
	for off := 0; off+width <= len(value); off += width {
		for i, j := off, off+width-1; i < j; i, j = i+1, j-1 {
			value[i], value[j] = value[j], value[i]
		}
	}
}

// PixelDataSwapWidth gives the unit native Pixel Data must be reversed in
// when converting byte order, or 0 when it must not be reversed at all. See
// dataelem.PixelDataSwapWidth for why OW at 32 or 64 bits allocated is
// reversed at the sample width (#124), and OB never.
func PixelDataSwapWidth(vr string, bitsAllocated int) int {
	if vr != "OW" {
		return Widths[vr]
	}
	if bitsAllocated >= 32 && bitsAllocated%8 == 0 {
		return bitsAllocated / 8
	}
	return Widths["OW"]
}

// SwapByteOrder converts a value between big and little endian in place,
// reversing each multi-byte number according to the VR's width. It is its
// own inverse; a value of a VR with no byte order is left untouched.
func SwapByteOrder(vr string, value []byte) {
	if width, sensitive := Widths[vr]; sensitive {
		SwapBytes(value, width)
	}
}

// choiceSeparator joins the alternatives of a dictionary VR that offers a
// choice, as the standard's data dictionary writes them: "OB or OW", "US or
// SS", "US or SS or OW" (PS3.6 Table 6-1, and the notes of Annex A).
const choiceSeparator = " or "

// Choices returns the VRs a dictionary entry allows: one for an ordinary
// entry, several for one that offers a choice ("US or OW" gives US and OW).
func Choices(entry string) []string {
	return strings.Split(entry, choiceSeparator)
}

// OffersChoice reports whether a dictionary entry offers a choice of VRs.
func OffersChoice(entry string) bool {
	return strings.Contains(entry, choiceSeparator)
}

// MatchesDictionary reports whether an element's VR is one the dictionary's
// entry allows for its tag: one of the choices Choices gives.
func MatchesDictionary(actual, entry string) bool {
	for _, choice := range Choices(entry) {
		if actual == choice {
			return true
		}
	}
	return false
}
