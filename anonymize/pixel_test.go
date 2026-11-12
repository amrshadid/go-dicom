package anonymize_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// De-identification stopped at the attributes. A name drawn into the pixels
// survived it, and nothing said the image might hold one (#233).

var (
	tagBurnedIn  = tag.New(0x0028, 0x0301)
	tagModality  = tag.New(0x0008, 0x0060)
	tagPixelData = tag.New(0x7FE0, 0x0010)
)

// ultrasound is a 4 by 4, 8-bit image with every sample 200.
func ultrasound(t *testing.T) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	for element, v := range map[uint16]uint16{
		0x0002: 1, 0x0010: 4, 0x0011: 4, 0x0100: 8, 0x0101: 8, 0x0102: 7, 0x0103: 0,
	} {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		if err := ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b)); err != nil {
			t.Fatal(err)
		}
	}
	withValue(t, ds, tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2")
	withValue(t, ds, tagModality, dataelem.CS, "US")
	withValue(t, ds, tag.New(0x0010, 0x0010), dataelem.PN, "Doe^John")
	px := make([]byte, 16)
	for i := range px {
		px[i] = 200
	}
	if err := ds.Add(dataelem.NewDataElement(tagPixelData, dataelem.OB, px)); err != nil {
		t.Fatal(err)
	}
	return ds
}

func methodCodes(ds *dataset.Dataset) string {
	var codes []string
	if elem, ok := ds.Get(tag.New(0x0012, 0x0064)); ok {
		if seq, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
			for _, item := range seq.Items() {
				codes = append(codes, textValue(item.(*dataset.Dataset), tag.New(0x0008, 0x0100)))
			}
		}
	}
	return strings.Join(codes, ",")
}

// TestCleanPixelData: the region is filled, Burned In Annotation says NO, and
// the Clean Pixel Data Option is recorded beside the profile, whichever of
// the two ran first.
func TestCleanPixelData(t *testing.T) {
	region := dataset.PixelRegion{X: 0, Y: 0, Width: 4, Height: 1}
	for _, cleanFirst := range []bool{false, true} {
		ds := ultrasound(t)
		withValue(t, ds, tagBurnedIn, dataelem.CS, "YES")
		a := anonymize.NewAnonymizer(anonymize.BasicProfile)

		steps := []func() error{
			func() error { return a.Anonymize(ds) },
			func() error { return a.CleanPixelData(ds, region) },
		}
		if cleanFirst {
			steps[0], steps[1] = steps[1], steps[0]
		}
		for _, step := range steps {
			if err := step(); err != nil {
				t.Fatalf("clean first %v: %v", cleanFirst, err)
			}
		}

		elem, _ := ds.Get(tagPixelData)
		px := elem.GetValue().([]byte)
		for i, v := range px {
			if want := byte(200); i < 4 {
				want = 0
				if v != want {
					t.Fatalf("clean first %v: pixel %d in the region is %d", cleanFirst, i, v)
				}
			} else if v != want {
				t.Fatalf("clean first %v: pixel %d outside the region is %d", cleanFirst, i, v)
			}
		}
		if got := textValue(ds, tagBurnedIn); got != "NO" {
			t.Errorf("clean first %v: Burned In Annotation is %q, want NO", cleanFirst, got)
		}
		if got := methodCodes(ds); got != "113100,113101" {
			t.Errorf("clean first %v: method codes %q, want 113100,113101", cleanFirst, got)
		}
		if got := textValue(ds, tag.New(0x0012, 0x0062)); got != "YES" {
			t.Errorf("clean first %v: Patient Identity Removed is %q", cleanFirst, got)
		}
	}
}

// TestCleanPixelDataAloneClaimsNoMore: cleaning the pixels records that
// option and does not say the identity was removed, which only the profile
// can say.
func TestCleanPixelDataAloneClaimsNoMore(t *testing.T) {
	ds := ultrasound(t)
	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.CleanPixelData(ds, dataset.PixelRegion{Width: 1, Height: 1}); err != nil {
		t.Fatal(err)
	}
	if got := methodCodes(ds); got != "113101" {
		t.Errorf("method codes %q, want 113101", got)
	}
	if _, ok := ds.Get(tag.New(0x0012, 0x0062)); ok {
		t.Error("Patient Identity Removed was set with the attributes untouched")
	}
}

// TestCleanPixelDataThatFailsSaysNothing: an image that could not be cleaned
// must not come out marked as clean.
func TestCleanPixelDataThatFailsSaysNothing(t *testing.T) {
	ds := ultrasound(t)
	withValue(t, ds, tagBurnedIn, dataelem.CS, "YES")
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.4.50")

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.CleanPixelData(ds, dataset.PixelRegion{Width: 1, Height: 1}); err == nil {
		t.Fatal("compressed pixel data was cleaned")
	}
	if got := textValue(ds, tagBurnedIn); got != "YES" {
		t.Errorf("Burned In Annotation became %q", got)
	}
	if got := methodCodes(ds); got != "" {
		t.Errorf("method codes %q were recorded", got)
	}
	if err := a.CleanPixelData(ds); err == nil {
		t.Error("cleaning with no region was accepted")
	}
}

// TestBurnedInRisk: the attribute decides when it is there. When it is not,
// the kinds of image that are drawn on decide.
func TestBurnedInRisk(t *testing.T) {
	for _, c := range []struct {
		name               string
		burnedIn, modality string
		sopClass           string
		noPixels           bool
		want               bool
	}{
		{name: "says YES", burnedIn: "YES", modality: "CT", want: true},
		{name: "says NO", burnedIn: "NO", modality: "US", want: false},
		{name: "ultrasound, unsaid", modality: "US", want: true},
		{name: "CT, unsaid", modality: "CT", want: false},
		{name: "MR, unsaid", modality: "MR", want: false},
		{name: "secondary capture class", modality: "CT", sopClass: "1.2.840.10008.5.1.4.1.1.7", want: true},
		{name: "multi-frame secondary capture", modality: "OT", sopClass: "1.2.840.10008.5.1.4.1.1.7.4", want: true},
		{name: "endoscopy", modality: "ES", want: true},
		{name: "no pixels at all", burnedIn: "YES", modality: "SR", noPixels: true, want: false},
	} {
		ds := ultrasound(t)
		withValue(t, ds, tagModality, dataelem.CS, c.modality)
		if c.burnedIn != "" {
			withValue(t, ds, tagBurnedIn, dataelem.CS, c.burnedIn)
		}
		if c.sopClass != "" {
			withValue(t, ds, tag.New(0x0008, 0x0016), dataelem.UI, c.sopClass)
		}
		if c.noPixels {
			ds.Remove(tagPixelData)
		}
		risk, reason := anonymize.BurnedInRisk(ds)
		if risk != c.want {
			t.Errorf("%s: risk %v (%q), want %v", c.name, risk, reason, c.want)
		}
		if risk && reason == "" {
			t.Errorf("%s: a risk with no reason given", c.name)
		}
	}
}

// textValue reads a text value however the element holds it.
func textValue(ds *dataset.Dataset, tg tag.Tag) string {
	elem, ok := ds.Get(tg)
	if !ok {
		return ""
	}
	switch v := elem.GetValue().(type) {
	case []byte:
		return strings.TrimRight(string(v), " \x00")
	case string:
		return strings.TrimRight(v, " \x00")
	}
	return ""
}
