package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// This library ships a private dictionary of 6883 attributes across the
// vendors, and nothing consulted it. A private attribute went into the JSON
// model as UN with its value base64-encoded, whatever the dictionary knew.
//
// Looking one up is not a lookup by tag. PS3.5 7.8.1 gives a vendor a group and
// lets it claim any block from 0x10 to 0xFF within it: the block is named by a
// private creator at (gggg,00xx), and the vendor's attributes then live at
// (gggg,xxee). The same attribute is (0043,1001) in a file that claimed block
// 0x10 and (0043,4301) in one that claimed 0x43. A dictionary keyed on the
// canonical block answers nothing for the second file unless the tag is
// normalized first.

// privateDataSet builds a data set with one private block and one attribute in
// it, at whichever block the caller wants to pretend the file claimed.
func privateDataSet(t *testing.T, block uint16, vendor string, vr dataelem.VR, value string) *dataset.Dataset {
	t.Helper()

	ds := dataset.NewDataset()
	if len(vendor)%2 == 1 {
		vendor += " "
	}
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0043, block), dataelem.LO,
		[]byte(vendor))); err != nil {
		t.Fatalf("adding the private creator: %v", err)
	}
	// The attribute the block owns: element is block<<8 | the low byte.
	element := block<<8 | 0x01
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0043, element), vr,
		[]byte(value))); err != nil {
		t.Fatalf("adding the private attribute: %v", err)
	}
	return ds
}

func vrOfElement(t *testing.T, ds *dataset.Dataset, key string) string {
	t.Helper()
	m, err := ds.ToDICOMJSON()
	if err != nil {
		t.Fatalf("ToDICOMJSON: %v", err)
	}
	elem, ok := m[key]
	if !ok {
		t.Fatalf("the document has no element %s", key)
	}
	return elem.VR
}

// TestPrivateAttributeVRComesFromTheDictionary covers the canonical block.
//
// GEMS_PARM_01 owns (0043,1001), Bitmap of Prescan Options, which the
// dictionary gives VR SS.
func TestPrivateAttributeVRComesFromTheDictionary(t *testing.T) {
	ds := privateDataSet(t, 0x10, "GEMS_PARM_01", dataelem.UN, "\x01\x00")

	if got := vrOfElement(t, ds, "00431001"); got != "SS" {
		t.Errorf("the private attribute's VR is %q, want SS from the GEMS_PARM_01 "+
			"dictionary", got)
	}
}

// TestPrivateAttributeVRSurvivesADifferentBlock is the case a plain lookup gets
// wrong.
//
// The same attribute, in a file that claimed block 0x43 instead of 0x10, is
// stored at (0043,4301). Looking that tag up in the dictionary finds nothing.
func TestPrivateAttributeVRSurvivesADifferentBlock(t *testing.T) {
	ds := privateDataSet(t, 0x43, "GEMS_PARM_01", dataelem.UN, "\x01\x00")

	if got := vrOfElement(t, ds, "00434301"); got != "SS" {
		t.Errorf("the private attribute's VR is %q, want SS; the block the file "+
			"claimed is not the block the dictionary stores it under", got)
	}
}

// TestPrivateAttributeOfAnUnknownVendorStaysUnknown covers the other direction.
//
// A vendor the dictionary does not have says nothing about its attributes, and
// guessing would be worse than admitting it.
func TestPrivateAttributeOfAnUnknownVendorStaysUnknown(t *testing.T) {
	ds := privateDataSet(t, 0x10, "NOT A REAL VENDOR", dataelem.UN, "\x01\x00")

	if got := vrOfElement(t, ds, "00431001"); got != "UN" {
		t.Errorf("the private attribute's VR is %q, want UN", got)
	}
}

// TestPrivateAttributeWithNoCreatorStaysUnknown covers a block nobody claimed.
func TestPrivateAttributeWithNoCreatorStaysUnknown(t *testing.T) {
	ds := dataset.NewDataset()
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0043, 0x1001), dataelem.UN,
		[]byte("\x01\x00"))); err != nil {
		t.Fatalf("adding the private attribute: %v", err)
	}

	if got := vrOfElement(t, ds, "00431001"); got != "UN" {
		t.Errorf("the private attribute's VR is %q, want UN; without a private "+
			"creator nothing identifies the vendor", got)
	}
}

// TestPrivateCreatorIsLongStringWithoutAVR covers the element that had no VR at
// all.
//
// An implicit VR file stores every element without one, so a private creator
// arrives with an empty VR rather than UN. The two were handled separately and
// only UN reached the private rules, so the same element came out as LO in one
// encoding and UN in the other. pydicom's priv_SQ.dcm is such a file.
func TestPrivateCreatorIsLongStringWithoutAVR(t *testing.T) {
	for _, vr := range []dataelem.VR{"", dataelem.UN} {
		name := string(vr)
		if name == "" {
			name = "absent"
		}
		t.Run(name, func(t *testing.T) {
			ds := dataset.NewDataset()
			if err := ds.Add(dataelem.NewDataElement(tag.New(0x3F03, 0x0010), vr,
				[]byte("aaabbbccc MEDICAL SYSTEMS "))); err != nil {
				t.Fatalf("adding the private creator: %v", err)
			}
			if got := vrOfElement(t, ds, "3F030010"); got != "LO" {
				t.Errorf("the private creator's VR is %q, want LO", got)
			}
		})
	}
}
