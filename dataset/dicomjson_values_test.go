package dataset_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The file reader holds every value as bytes, and the JSON writer read only
// bytes. An element built in memory holds a Go value, which the file writer
// and the network encoder both render through dataelem.ValueBytes; the JSON
// writer dropped it, so a data set that wrote to disk whole came out of JSON
// as a list of value representations with nothing in them (#295).

// TestValuesBuiltInMemoryAreWritten checks that each kind of value
// dataelem.ValueBytes accepts gives the JSON its bytes would.
func TestValuesBuiltInMemoryAreWritten(t *testing.T) {
	cases := []struct {
		name  string
		tag   tag.Tag
		vr    dataelem.VR
		value any
		raw   string
	}{
		{"string", tag.New(0x0008, 0x0020), dataelem.DA, "20240101", "20240101"},
		{"string holding several values", tag.New(0x0008, 0x0061), dataelem.CS, `CT\MR`, `CT\MR`},
		{"list of strings", tag.New(0x0008, 0x0008), dataelem.CS, []string{"ORIGINAL", "PRIMARY"}, `ORIGINAL\PRIMARY`},
		{"person name as text", tag.New(0x0010, 0x0010), dataelem.PN, "Doe^John", "Doe^John"},
		{"person name", tag.New(0x0008, 0x0090), dataelem.PN,
			dataelem.PersonName{Alphabetic: "Roe^Jane", Ideographic: "山田^花子"}, "Roe^Jane=山田^花子"},
		{"integer string from a number", tag.New(0x0020, 0x1208), dataelem.IS, 3, "3"},
		{"decimal strings from numbers", tag.New(0x0028, 0x0030), dataelem.DS, []float64{0.5, 0.25}, `0.5\0.25`},
		{"unsigned short", tag.New(0x0028, 0x0010), dataelem.US, uint16(512), "\x00\x02"},
		{"unsigned shorts", tag.New(0x0028, 0x0034), dataelem.US, []int{4, 3}, "\x04\x00\x03\x00"},
		{"double", tag.New(0x0018, 0x9087), dataelem.FD, 1.5, "\x00\x00\x00\x00\x00\x00\xf8\x3f"},
		{"attribute tag", tag.New(0x0020, 0x9165), dataelem.AT, tag.New(0x0018, 0x9087), "\x18\x00\x87\x90"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			built := dataset.NewDataset()
			if err := built.Add(dataelem.NewDataElement(c.tag, c.vr, c.value)); err != nil {
				t.Fatal(err)
			}
			read := dataset.NewDataset()
			if err := read.Add(dataelem.NewDataElement(c.tag, c.vr, []byte(c.raw))); err != nil {
				t.Fatal(err)
			}
			got, want := jsonOf(t, built), jsonOf(t, read)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("built in memory: %v\nread as bytes:   %v", got, want)
			}
			for key, element := range got {
				if _, ok := element.(map[string]any)["Value"]; !ok {
					t.Errorf("%s has no Value: %v", key, element)
				}
			}
		})
	}
}

// TestValueThatDoesNotFitIsRefused checks that a value its VR cannot hold is
// an error. Written without its value it would be a valid document that says
// the element is empty, which nothing downstream can tell from the truth.
func TestValueThatDoesNotFitIsRefused(t *testing.T) {
	ds := dataset.NewDataset()
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.US, 70000)); err != nil {
		t.Fatal(err)
	}
	_, err := ds.ToDICOMJSONString()
	if err == nil {
		t.Fatal("70000 was written as an unsigned short")
	}
	if !strings.Contains(err.Error(), "00280010") {
		t.Errorf("the error does not name the element: %v", err)
	}
}
