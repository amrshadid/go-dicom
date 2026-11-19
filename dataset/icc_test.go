package dataset_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/internal/icctest"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	tagICCProfile          = tag.New(0x0028, 0x2000)
	tagOpticalPathSequence = tag.New(0x0048, 0x0105)
)

// withICC is a data set holding value as its ICC Profile, padded to an even
// length as an OB value is.
func withICC(value []byte) *dataset.Dataset {
	ds := dataset.NewDataset()
	if len(value)%2 == 1 {
		value = append(value[:len(value):len(value)], 0)
	}
	_ = ds.Add(dataelem.NewDataElement(tagICCProfile, dataelem.OB, value))
	return ds
}

// TestICCProfile: the profile comes back as it was made, without the byte
// that pads an odd one to an even length; from the one optical path that
// has one when the data set itself has none; not at all when there is none
// or when two optical paths each have their own; and a value that is not a
// profile is an error, not a profile.
func TestICCProfile(t *testing.T) {
	odd := icctest.Profile("RGB ", "an odd profile")
	if len(odd)%2 == 0 {
		odd = icctest.Profile("RGB ", "an odd profile.")
	}
	if len(odd)%2 == 0 {
		t.Fatal("could not make a profile of odd length")
	}
	if got, err := withICC(odd).ICCProfile(); err != nil || !bytes.Equal(got, odd) {
		t.Errorf("an odd profile of %d bytes came back as %d (%v)", len(odd), len(got), err)
	}

	if got, err := dataset.NewDataset().ICCProfile(); got != nil || err != nil {
		t.Errorf("a data set with no profile gave %d bytes (%v)", len(got), err)
	}

	paths := func(profiles ...[]byte) *dataset.Dataset {
		ds := dataset.NewDataset()
		var items []sequence.Item
		for _, p := range profiles {
			item := dataset.NewDataset()
			if p != nil {
				item = withICC(p)
			}
			items = append(items, item)
		}
		_ = ds.Add(dataelem.NewDataElement(tagOpticalPathSequence, dataelem.SQ, sequence.NewFromItems(items...)))
		return ds
	}
	slide := icctest.Profile("RGB ", "a slide scanner")
	if got, err := paths(nil, slide).ICCProfile(); err != nil || !bytes.Equal(got, slide) {
		t.Errorf("the one optical path's profile came back as %d bytes (%v)", len(got), err)
	}
	if got, err := paths(slide, icctest.Profile("RGB ", "another")).ICCProfile(); got != nil || err != nil {
		t.Errorf("two optical paths with their own profiles gave %d bytes (%v); the frame decides which", len(got), err)
	}

	for name, value := range map[string][]byte{
		"text":                    []byte(strings.Repeat("not a profile ", 20)),
		"short":                   odd[:100],
		"larger than it says":     append(append([]byte{}, odd[:4]...), odd[4:200]...),
		"smaller than its header": func() []byte { b := append([]byte{}, odd...); b[0], b[1], b[2], b[3] = 0, 0, 0, 64; return b }(),
	} {
		if got, err := withICC(value).ICCProfile(); err == nil {
			t.Errorf("%s: %d bytes taken for a profile", name, len(got))
		}
	}
}
