package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// A private attribute's VR comes from the private dictionary, which is GDCM's
// (scripts/private-dictionary.py). The table it replaced was generated with VRs
// in rotation, so (0019,1008) under SIEMENS MR HEADER, a CS, was listed as SL,
// and trusting it relabeled the attributes of every implicit VR file written
// out (#186); for that while every private attribute was UN. GDCM's entries are
// used again where the VR is unambiguous and the value fits it (#188).
//
// Looking one up is not a lookup by tag. PS3.5 7.8.1 lets a vendor claim any
// block from 0x10 to 0xFF of its group, named by a private creator at
// (gggg,00xx), so the same attribute is (0043,1001) in one file and (0043,4301)
// in another, and the tag is normalized to block 0x10 before the lookup.

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

// TestPrivateAttributeVRComesFromGDCM covers both the canonical block and one
// a file chose for itself: GEMS_PARM_01 owns (0043,xx01), Bitmap of Prescan
// Options, which GDCM gives VR SS.
func TestPrivateAttributeVRComesFromGDCM(t *testing.T) {
	for block, key := range map[uint16]string{0x10: "00431001", 0x43: "00434301"} {
		ds := privateDataSet(t, block, "GEMS_PARM_01", dataelem.UN, "\x01\x00")
		if got := vrOfElement(t, ds, key); got != "SS" {
			t.Errorf("block 0x%02X: the private attribute's VR is %q, want SS", block, got)
		}
	}
}

// TestImplicitPrivateAttributeTakesItsRealVR: where real Siemens MR files hold
// them, (0019,1008) is a CS and (0019,100A) a US. The generated table called
// them SL and FL. ResolveVR, which the file writer and the network encoder
// ask, now gives the real ones, and keeps UN for a value the VR cannot hold,
// so a wrong entry cannot make a file pydicom refuses to read.
func TestImplicitPrivateAttributeTakesItsRealVR(t *testing.T) {
	ds := dataset.NewDataset()
	add := func(element uint16, value string) {
		if err := ds.Add(dataelem.NewDataElement(tag.New(0x0019, element), "", []byte(value))); err != nil {
			t.Fatal(err)
		}
	}
	add(0x0010, "SIEMENS MR HEADER ")
	add(0x1008, "IMAGE NUM 4 ")
	add(0x100A, "\x10\x00")

	for element, want := range map[uint16]dataelem.VR{0x0010: dataelem.LO, 0x1008: dataelem.CS, 0x100A: dataelem.US} {
		attribute := tag.New(0x0019, element)
		elem, _ := ds.Get(attribute)
		if got := ds.ResolveVR(attribute, elem); got != want {
			t.Errorf("(0019,%04X): ResolveVR gave %q, want %q", element, got, want)
		}
	}

	// Three bytes are not a whole number of US values.
	add(0x100A, "\x10\x00\x00")
	elem, _ := ds.Get(tag.New(0x0019, 0x100A))
	if got := ds.ResolveVR(tag.New(0x0019, 0x100A), elem); got != dataelem.UN {
		t.Errorf("a 3-byte (0019,100A) resolved to %q, want UN", got)
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
