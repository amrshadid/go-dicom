package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// A private attribute's VR cannot be taken from this library's private
// dictionary. Most of it is generated: tag/gen_private_dict_tool.go gives the
// attributes of a vendor VRs in rotation, so (0019,1008) under SIEMENS MR
// HEADER, a CS in real files, is listed as SL, and (0019,100A), a US, as FL.
// Trusting it relabeled the attributes of every implicit VR file the library
// wrote out: storescp stored a Siemens MR whose text read back as numbers, and
// whose 2-byte value was labeled with a 4-byte VR pydicom refuses (#186).
//
// An implicit private attribute is therefore UN, which PS3.5 6.2.2 provides
// for exactly this: a value whose VR the writer does not know, kept byte for
// byte. A private creator is still LO, which PS3.5 7.8.1 makes it by definition.

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

// TestPrivateAttributeVRIsNotGuessed: whatever the dictionary lists for the
// vendor and whichever block the file claimed, an attribute read without a VR
// stays UN in DICOM JSON, with its bytes intact.
func TestPrivateAttributeVRIsNotGuessed(t *testing.T) {
	for _, tc := range []struct {
		vendor string
		block  uint16
		key    string
	}{
		{"GEMS_PARM_01", 0x10, "00431001"},
		{"GEMS_PARM_01", 0x43, "00434301"},
	} {
		ds := privateDataSet(t, tc.block, tc.vendor, dataelem.UN, "IMAGE NUM 4 ")
		m, err := ds.ToDICOMJSON()
		if err != nil {
			t.Fatalf("%s: ToDICOMJSON: %v", tc.vendor, err)
		}
		elem := m[tc.key]
		if elem.VR != "UN" {
			t.Errorf("%s at %s: VR %q, want UN", tc.vendor, tc.key, elem.VR)
		}
		if got := elem.InlineBinary; got != "SU1BR0UgTlVNIDQg" {
			t.Errorf("%s at %s: InlineBinary %q, want the original bytes", tc.vendor, tc.key, got)
		}
	}
}

// TestImplicitPrivateAttributeIsWrittenAsUN: ResolveVR is what the file writer
// and the network encoder ask, so an implicit private attribute must come out
// of it as UN rather than as whatever the dictionary lists.
func TestImplicitPrivateAttributeIsWrittenAsUN(t *testing.T) {
	// Where real Siemens MR files hold it: (0019,1008), a CS the generated
	// dictionary lists as SL.
	ds := dataset.NewDataset()
	creatorTag, attribute := tag.New(0x0019, 0x0010), tag.New(0x0019, 0x1008)
	if err := ds.Add(dataelem.NewDataElement(creatorTag, "", []byte("SIEMENS MR HEADER "))); err != nil {
		t.Fatal(err)
	}
	if err := ds.Add(dataelem.NewDataElement(attribute, "", []byte("IMAGE NUM 4 "))); err != nil {
		t.Fatal(err)
	}

	elem, _ := ds.Get(attribute)
	if got := ds.ResolveVR(attribute, elem); got != dataelem.UN {
		t.Errorf("ResolveVR gave %q for an implicit private attribute, want UN", got)
	}
	creator, _ := ds.Get(creatorTag)
	if got := ds.ResolveVR(creatorTag, creator); got != dataelem.LO {
		t.Errorf("ResolveVR gave %q for the private creator, want LO", got)
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
