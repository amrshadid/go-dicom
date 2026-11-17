package dcmstore_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/dicomweb"
	"github.com/amrshadid/go-dicom/tag"
)

// A DICOMweb search returns a fixed set of attributes for each level (PS3.18
// 10.6.3.3). The index held the C-FIND keys of PS3.4 and not the rest of
// that set, so a search for instances came back without the size of the
// image, which Orthanc returns for the same files (#294). These tests hold
// the attributes added for it, and the form a number is answered in.

var (
	tagRows             = tag.New(0x0028, 0x0010)
	tagColumns          = tag.New(0x0028, 0x0011)
	tagBitsAllocated    = tag.New(0x0028, 0x0100)
	tagNumberOfFrames   = tag.New(0x0028, 0x0008)
	tagTimezoneOffset   = tag.New(0x0008, 0x0201)
	tagStepStartDate    = tag.New(0x0040, 0x0244)
	tagStepStartTime    = tag.New(0x0040, 0x0245)
	tagAvailability     = tag.New(0x0008, 0x0056)
	tagInstanceUID      = tag.New(0x0008, 0x0018)
	searchKeyAttributes = []tag.Tag{tagRows, tagColumns, tagBitsAllocated, tagNumberOfFrames,
		tagTimezoneOffset, tagStepStartDate, tagStepStartTime, tagAvailability}
)

// imageInstance is an image as it arrives from a peer: its numbers as the
// little endian bytes of their VR, and one given as a Go number, which is
// how a program that builds a data set writes it.
func imageInstance() *dataset.Dataset {
	ds := instance{"P5", "ROE^JANE", "1.2.5", "20240401", "1.2.5.1", "MR", "1.2.5.1.1", "1"}.dataset()
	_ = ds.Add(dataelem.NewDataElement(tagRows, dataelem.US, []byte{0x00, 0x02}))
	_ = ds.Add(dataelem.NewDataElement(tagColumns, dataelem.US, uint16(256)))
	_ = ds.Add(dataelem.NewDataElement(tagBitsAllocated, dataelem.US, []byte{16, 0}))
	_ = ds.Add(dataelem.NewDataElement(tagNumberOfFrames, dataelem.IS, []byte("2 ")))
	_ = ds.Add(dataelem.NewDataElement(tagTimezoneOffset, dataelem.SH, []byte("+0200 ")))
	_ = ds.Add(dataelem.NewDataElement(tagStepStartDate, dataelem.DA, []byte("20240401")))
	_ = ds.Add(dataelem.NewDataElement(tagStepStartTime, dataelem.TM, []byte("093000")))
	return ds
}

func storeImage(t *testing.T) *dcmstore.Store {
	t.Helper()
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Store(context.Background(), imageInstance()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	return store
}

// imageQuery asks at IMAGE level for the attributes above, with any
// elements to match.
func imageQuery(match ...*dataelem.DataElement) *dataset.Dataset {
	q := dataset.NewDataset()
	_ = q.Add(dataelem.NewDataElement(tagLevel, dataelem.CS, "IMAGE"))
	for _, elem := range match {
		_ = q.Add(elem)
	}
	for _, at := range searchKeyAttributes {
		if !q.Contains(at) {
			_ = q.Add(dataelem.NewDataElement(at, dataelem.VR(at.GetVR()), ""))
		}
	}
	return q
}

// TestACFindAnswerEncodesItsNumbers checks that a binary number comes back
// as a value that encodes to the bytes of its VR. The index holds it as
// text, and text written into a US would send "512" as three bytes of a
// two byte number.
func TestACFindAnswerEncodesItsNumbers(t *testing.T) {
	answers, err := storeImage(t).Query(context.Background(), imageQuery())
	if err != nil || len(answers) != 1 {
		t.Fatalf("Query: %d answers, %v", len(answers), err)
	}
	for at, want := range map[tag.Tag][]byte{
		tagRows:          {0x00, 0x02},
		tagColumns:       {0x00, 0x01},
		tagBitsAllocated: {16, 0},
	} {
		elem, ok := answers[0].Get(at)
		if !ok {
			t.Errorf("%s is not in the answer", at)
			continue
		}
		got, err := dataelem.ValueBytes(elem.GetVR(), elem.GetValue())
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s encodes to % x (%v), want % x", at, got, err, want)
		}
	}
	for at, want := range map[tag.Tag]string{
		tagNumberOfFrames: "2",
		tagTimezoneOffset: "+0200",
		tagStepStartDate:  "20240401",
		tagStepStartTime:  "093000",
		tagAvailability:   "ONLINE",
	} {
		if got := textOf(t, answers[0], at); got != want {
			t.Errorf("%s is %q, want %q", at, got, want)
		}
	}
}

// TestANumberIsMatchedHoweverItIsWritten checks that Rows is matched by a
// query that carries it as bytes, as a peer does, and as a number in text,
// as a URL does.
func TestANumberIsMatchedHoweverItIsWritten(t *testing.T) {
	store := storeImage(t)
	cases := []struct {
		name  string
		value any
		want  int
	}{
		{"as bytes", []byte{0x00, 0x02}, 1},
		{"as text", "512", 1},
		{"as a Go number", uint16(512), 1},
		{"another number", []byte{0x00, 0x01}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			answers, err := store.Query(context.Background(),
				imageQuery(dataelem.NewDataElement(tagRows, dataelem.US, c.value)))
			if err != nil {
				t.Fatal(err)
			}
			if len(answers) != c.want {
				t.Errorf("%d answers, want %d", len(answers), c.want)
			}
		})
	}
}

// TestAWebSearchReturnsTheImageSize checks the same attributes through the
// search service, where a number is a JSON number, and that a search by
// Rows in the URL finds the image.
func TestAWebSearchReturnsTheImageSize(t *testing.T) {
	client := webClient(t, storeImage(t))
	instances, err := client.SearchForInstances(context.Background(), "", "",
		dicomweb.Query{Match: map[string]string{"Rows": "512"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("%d instances", len(instances))
	}
	result := instances[0]
	if got := webText(t, result, tagInstanceUID); got != "1.2.5.1.1" {
		t.Errorf("found %q", got)
	}
	for at, want := range map[tag.Tag][]byte{
		tagRows: {0x00, 0x02}, tagColumns: {0x00, 0x01}, tagBitsAllocated: {16, 0},
	} {
		elem, ok := result.Get(at)
		if !ok {
			t.Errorf("%s is not in the result", at)
			continue
		}
		if got, _ := elem.GetValue().([]byte); !bytes.Equal(got, want) {
			t.Errorf("%s is % x, want % x", at, got, want)
		}
	}
	for at, want := range map[tag.Tag]string{
		tagNumberOfFrames: "2", tagTimezoneOffset: "+0200", tagAvailability: "ONLINE",
	} {
		if got := webText(t, result, at); got != want {
			t.Errorf("%s is %q, want %q", at, got, want)
		}
	}

	series, err := client.SearchForSeries(context.Background(), "1.2.5", dicomweb.Query{})
	if err != nil || len(series) != 1 {
		t.Fatalf("series: %d, %v", len(series), err)
	}
	if got := webText(t, series[0], tagStepStartDate); got != "20240401" {
		t.Errorf("the step started on %q", got)
	}
}
