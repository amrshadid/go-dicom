package dataset

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/tag"
)

var (
	tagICCProfile          = tag.New(0x0028, 0x2000)
	tagOpticalPathSequence = tag.New(0x0048, 0x0105)
)

// ICCProfile returns the image's ICC profile (PS3.3 C.11.15): the profile
// that says what color space its RGB values, after any YBR or palette
// conversion, are in. It is ICC Profile (0028,2000) of the data set, or,
// when the data set has none, of the one item of its Optical Path Sequence
// (0048,0105) that has one, which is where a whole-slide image keeps it
// (C.8.12.5). It returns nil when there is none, or when several optical
// paths each have their own, since which one a picture is in depends on
// the frame.
//
// The profile is cut to the size its header gives: an OB value is padded
// to an even length, and a profile of odd size gains a byte that is not
// part of it. A value that is not an ICC profile, too short or without the
// "acsp" signature of ICC.1 7.2, is an error.
//
// Nothing here converts color. RenderFrame shows the samples as they are,
// and a PNG or JPEG that carries this profile lets a viewer that manages
// color show them as the image means.
func (ds *Dataset) ICCProfile() ([]byte, error) {
	raw, ok := iccValue(ds)
	if !ok {
		var found [][]byte
		for n := 0; nthItem(ds, tagOpticalPathSequence, n) != nil; n++ {
			if v, ok := iccValue(nthItem(ds, tagOpticalPathSequence, n)); ok {
				found = append(found, v)
			}
		}
		if len(found) != 1 {
			return nil, nil
		}
		raw = found[0]
	}
	// ICC.1 7.2: the header is 128 bytes; the profile's size is its first
	// four, big endian, and "acsp" is at 36.
	if len(raw) < 132 || string(raw[36:40]) != "acsp" {
		return nil, fmt.Errorf("the ICC Profile (0028,2000) of %d bytes is not an ICC profile", len(raw))
	}
	size := binary.BigEndian.Uint32(raw)
	if size < 132 || uint64(size) > uint64(len(raw)) {
		return nil, fmt.Errorf("the ICC Profile (0028,2000) says it is %d bytes and holds %d", size, len(raw))
	}
	return raw[:size], nil
}

// iccValue is the ICC Profile of one data set, when it has a value.
func iccValue(ds *Dataset) ([]byte, bool) {
	elem, ok := ds.Get(tagICCProfile)
	if !ok {
		return nil, false
	}
	raw, err := extractBytesValue(elem)
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}
