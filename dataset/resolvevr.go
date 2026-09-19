package dataset

import (
	"encoding/binary"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	bitsAllocatedTag       = tag.New(0x0028, 0x0100)
	pixelRepresentationTag = tag.New(0x0028, 0x0103)
	lutDescriptorTag       = tag.New(0x0028, 0x3002)
	lutDataTag             = tag.New(0x0028, 0x3006)
	pixelDataTag           = tag.New(0x7FE0, 0x0010)
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
	if _, ok := elem.GetValue().(*sequence.Sequence); ok {
		return dataelem.SQ
	}

	vr := elem.GetVR()
	if vr == "" {
		if private := ds.privateVR(t); private != "" {
			return private
		}
		vr = dataelem.VR(t.GetVR())
		if vr == dataelem.SQ {
			// The value is bytes, not items. Claiming SQ would describe a
			// structure nobody parsed.
			return dataelem.UN
		}
	}

	if strings.Contains(string(vr), " or ") {
		vr = ds.resolveAmbiguousVR(t, vr, enclosing)
	}
	if len(vr) != 2 {
		return dataelem.UN
	}
	return vr
}

// resolveAmbiguousVR applies the rules ResolveVR describes to a dictionary VR
// that offers a choice.
func (ds *Dataset) resolveAmbiguousVR(t tag.Tag, vr dataelem.VR, enclosing []*Dataset) dataelem.VR {
	switch {
	case t == pixelDataTag:
		if bits, ok := ds.uint16Value(bitsAllocatedTag); ok && bits <= 8 {
			return dataelem.OB
		}
		return dataelem.OW

	case t == lutDataTag:
		if entries, ok := ds.uint16Value(lutDescriptorTag); ok && entries == 1 {
			return dataelem.US
		}
		return dataelem.OW

	case vr == "US or SS":
		if ds.unsignedPixelValues(enclosing) {
			return dataelem.US
		}
		return dataelem.SS

	case strings.Contains(string(vr), "OW"):
		return dataelem.OW

	case vr == "OB or OD":
		return dataelem.OB
	}
	return vr
}

// unsignedPixelValues reports what Pixel Representation says, taken from the
// nearest data set that has it. Absent everywhere, it is 0 — unsigned — which is
// the standard's default and the common case.
func (ds *Dataset) unsignedPixelValues(enclosing []*Dataset) bool {
	if v, ok := ds.uint16Value(pixelRepresentationTag); ok {
		return v == 0
	}
	if len(enclosing) > 0 {
		for _, outer := range enclosing {
			if outer == nil {
				continue
			}
			if v, ok := outer.uint16Value(pixelRepresentationTag); ok {
				return v == 0
			}
		}
		return true
	}
	for outer := ds.Parent(); outer != nil; outer = outer.Parent() {
		if v, ok := outer.uint16Value(pixelRepresentationTag); ok {
			return v == 0
		}
	}
	return true
}

// uint16Value reads the first value of a two-byte unsigned element.
func (ds *Dataset) uint16Value(t tag.Tag) (uint16, bool) {
	elem, ok := ds.Get(t)
	if !ok {
		return 0, false
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok || len(raw) < 2 {
		return 0, false
	}
	return binary.LittleEndian.Uint16(raw), true
}
