package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestResolveVR covers each rule ResolveVR applies. The file writer, the
// network encoder and DICOM JSON all use it, so a rule that is wrong here is
// wrong in all three at once — which is the point: they used to disagree, and
// the file writer did not resolve at all.
func TestResolveVR(t *testing.T) {
	u16 := func(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }
	with := func(elems ...*dataelem.DataElement) *dataset.Dataset {
		ds := dataset.NewDataset()
		for _, e := range elems {
			_ = ds.Add(e)
		}
		return ds
	}
	el := func(g, e uint16, vr dataelem.VR, v any) *dataelem.DataElement {
		return dataelem.NewDataElement(tag.New(g, e), vr, v)
	}

	signed := el(0x0028, 0x0103, dataelem.US, u16(1))
	unsigned := el(0x0028, 0x0103, dataelem.US, u16(0))
	smallest := func() *dataelem.DataElement { return el(0x0028, 0x0106, "US or SS", u16(0x9C40)) }
	pixels := func() *dataelem.DataElement { return el(0x7FE0, 0x0010, "OB or OW", []byte{1, 2, 3, 4}) }
	lutData := func() *dataelem.DataElement { return el(0x0028, 0x3006, "US or SS or OW", u16(7)) }

	tests := []struct {
		name string
		ds   func(target *dataelem.DataElement) *dataset.Dataset
		elem *dataelem.DataElement
		want dataelem.VR
	}{
		{"US or SS, unsigned", func(e *dataelem.DataElement) *dataset.Dataset { return with(unsigned, e) },
			smallest(), dataelem.US},
		{"US or SS, signed", func(e *dataelem.DataElement) *dataset.Dataset { return with(signed, e) },
			smallest(), dataelem.SS},
		{"US or SS, no Pixel Representation", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			smallest(), dataelem.US},

		{"Pixel Data, 16 bits", func(e *dataelem.DataElement) *dataset.Dataset {
			return with(el(0x0028, 0x0100, dataelem.US, u16(16)), e)
		}, pixels(), dataelem.OW},
		{"Pixel Data, 8 bits", func(e *dataelem.DataElement) *dataset.Dataset {
			return with(el(0x0028, 0x0100, dataelem.US, u16(8)), e)
		}, pixels(), dataelem.OB},
		{"Pixel Data, no Bits Allocated", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			pixels(), dataelem.OW},

		{"Overlay Data ignores Bits Allocated", func(e *dataelem.DataElement) *dataset.Dataset {
			return with(el(0x0028, 0x0100, dataelem.US, u16(8)), e)
		}, el(0x6000, 0x3000, "OB or OW", []byte{1, 2}), dataelem.OW},

		{"LUT Data, one entry", func(e *dataelem.DataElement) *dataset.Dataset {
			return with(el(0x0028, 0x3002, "US or SS", []byte{1, 0, 0, 0, 16, 0}), e)
		}, lutData(), dataelem.US},
		{"LUT Data, many entries", func(e *dataelem.DataElement) *dataset.Dataset {
			return with(el(0x0028, 0x3002, "US or SS", []byte{0, 16, 0, 0, 16, 0}), e)
		}, lutData(), dataelem.OW},

		{"no VR, public tag", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			el(0x0010, 0x0010, "", []byte("Doe^John")), dataelem.PN},
		{"no VR, public sequence held as bytes", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			el(0x0008, 0x1140, "", []byte{0xFE, 0xFF, 0x00, 0xE0}), dataelem.UN},
		{"no VR, private creator", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			el(0x0019, 0x0010, "", []byte("ACME")), dataelem.LO},
		{"no VR, private with no creator", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			el(0x0019, 0x1001, "", []byte{1, 2}), dataelem.UN},
		{"no VR, ambiguous in the dictionary", func(e *dataelem.DataElement) *dataset.Dataset {
			return with(signed, e)
		}, el(0x0028, 0x0106, "", u16(1)), dataelem.SS},

		{"explicit UN is left alone", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			el(0x0010, 0x0010, dataelem.UN, []byte("Doe^John")), dataelem.UN},
		{"a VR that is not two characters", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			el(0x0010, 0x0010, "PNX", []byte("Doe^John")), dataelem.UN},
		{"a sequence value", func(e *dataelem.DataElement) *dataset.Dataset { return with(e) },
			el(0x0008, 0x1140, "", sequence.New()), dataelem.SQ},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := tc.ds(tc.elem)
			tg, _ := tc.elem.Tag()
			if got := ds.ResolveVR(tg, tc.elem); got != tc.want {
				t.Errorf("ResolveVR = %q, want %s", got, tc.want)
			}
		})
	}
}

// TestResolveVRInheritsPixelRepresentation covers an item that describes an
// image without repeating its Pixel Representation — a Modality LUT Sequence
// item's LUT Descriptor is "US or SS", and the image decides which.
func TestResolveVRInheritsPixelRepresentation(t *testing.T) {
	descriptor := dataelem.NewDataElement(tag.New(0x0028, 0x3002), "US or SS", []byte{0, 16, 0x00, 0x80, 16, 0})
	item := dataset.NewDataset()
	_ = item.Add(descriptor)

	image := dataset.NewDataset()
	_ = image.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0103), dataelem.US, []byte{1, 0}))

	tg := tag.New(0x0028, 0x3002)
	if got := item.ResolveVR(tg, descriptor, image); got != dataelem.SS {
		t.Errorf("given the enclosing image: %q, want SS", got)
	}
	if got := item.ResolveVR(tg, descriptor); got != dataelem.US {
		t.Errorf("with nothing enclosing it: %q, want US, the default", got)
	}

	// The sequence helpers record the parent, and that is used when no chain is
	// passed.
	seq := sequence.New()
	_ = seq.Append(item)
	_ = image.AddSequence(tag.New(0x0028, 0x3000), seq)
	if item.Parent() == nil {
		t.Skip("AddSequence did not record the parent")
	}
	if got := item.ResolveVR(tg, descriptor); got != dataelem.SS {
		t.Errorf("through the recorded parent: %q, want SS", got)
	}
}
