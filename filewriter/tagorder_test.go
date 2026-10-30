package filewriter_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestElementsAreWrittenInTagOrder: PS3.5 7.1 orders a data set's elements by
// ascending tag. They were written in the order they were added, so an
// attribute added to a file read from disk landed after its Pixel Data, and
// at Explicit VR Big Endian a 32-bit image whose Pixel Data was added before
// Bits Allocated was swapped at 16 bits, #124 again (#214). Items too.
func TestElementsAreWrittenInTagOrder(t *testing.T) {
	item := dataset.NewDataset()
	_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1155), dataelem.UI, "1.2.3"))
	_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1150), dataelem.UI, "1.2.840.10008.5.1.4.1.1.2"))
	seq := sequence.New()
	_ = seq.Append(item)

	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, make([]byte, 8)))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.US, []byte{32, 0}))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0012, 0x0062), dataelem.CS, "YES"))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1140), dataelem.SQ, seq))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.2"))

	var check func(level string, elements []*filewriter.DataElement)
	check = func(level string, elements []*filewriter.DataElement) {
		for i := 1; i < len(elements); i++ {
			if elements[i].Tag <= elements[i-1].Tag {
				t.Errorf("%s: %s written after %s", level, elements[i].Tag, elements[i-1].Tag)
			}
		}
		for _, e := range elements {
			for _, it := range e.Items {
				check(level+" > "+e.Tag.String(), it.Elements)
			}
		}
	}
	check("data set", filewriter.ElementsFromDataset(ds))
}
