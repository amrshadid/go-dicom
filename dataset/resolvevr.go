package dataset

import (
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/internal/vr"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// ResolveVR settles on the one value representation an element is written with.
//
// A data set read from an Implicit VR file carries no VR of its own: the reader
// takes it from the dictionary, and for some attributes the dictionary has no
// single answer. Pixel Data is "OB or OW" and Smallest Image Pixel Value is
// "US or SS", because the attribute takes its VR from another one in the same
// data set. An encoder that writes that verbatim puts eight bytes where the
// standard allows exactly two (PS3.5 6.2), and every element after it is read
// from the wrong offset — the file writer did, and lost Pixel Data doing it. One
// that picks arbitrarily writes something accepted and wrong: US read as SS
// turns 40000 into -25536.
//
// So the deciding attribute is consulted, as pydicom and dcmtk do:
//
//   - "US or SS" follows Pixel Representation (0028,0103), PS3.3 C.7.6.3,
//     from this data set or the nearest enclosing one that has it: an item
//     describing an image rarely repeats it. Absent, unsigned.
//   - Pixel Data is OB when Bits Allocated (0028,0100) is 8 or fewer and OW
//     otherwise (PS3.5 A.1, A.2).
//   - LUT Data (0028,3006) is US when its LUT Descriptor declares a single
//     entry and OW otherwise (PS3.3 C.11.1.1.1).
//   - Any other choice that offers OW is OW. Overlay Data must be OW when
//     implicit (PS3.5 8.1.2), waveform data may always be OW, and OW keeps the
//     words exactly as they were read.
//
// An element with no VR at all takes the private dictionary's, then the public
// dictionary's, and is UN when neither knows it. Whatever is still not two
// characters is UN, which is what the standard calls a VR nobody knows. An
// explicit UN is left alone.
//
// enclosing lists the data sets this one is an item of, nearest first. Without
// them the parents recorded by the sequence helpers are used, which a data set
// read from a file does not have.
func (ds *Dataset) ResolveVR(t tag.Tag, elem *dataelem.DataElement, enclosing ...*Dataset) dataelem.VR {
	_, isSequence := elem.GetValue().(*sequence.Sequence)
	stored := elem.GetVR()

	// What the rules read of the element and of the data sets around it is
	// only needed for an element with no VR of its own or one whose
	// dictionary VR offers a choice, so it is not gathered for the rest.
	var raw []byte
	var rawOK bool
	var outers []vr.Scope
	if !isSequence && (stored == "" || dataelem.OffersVRChoice(string(stored))) {
		raw, rawOK = elem.Bytes()
		outers = ds.outerScopes(enclosing)
	}
	return dataelem.VR(vr.Resolve(t, string(stored), isSequence, raw, rawOK, datasetScope{ds}, outers))
}

// outerScopes lists the data sets this one is an item of, nearest first: the
// ones given, or when none is the parents the sequence helpers recorded.
func (ds *Dataset) outerScopes(enclosing []*Dataset) []vr.Scope {
	var outers []vr.Scope
	if len(enclosing) > 0 {
		for _, outer := range enclosing {
			if outer != nil {
				outers = append(outers, datasetScope{outer})
			}
		}
		return outers
	}
	for outer := ds.Parent(); outer != nil; outer = outer.Parent() {
		outers = append(outers, datasetScope{outer})
	}
	return outers
}
