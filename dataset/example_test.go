package dataset_test

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// A Dataset holds data elements keyed by tag while preserving insertion order.
// All operations are safe for concurrent use.
func Example() {
	ds := dataset.NewDataset()

	_ = ds.Add(dataelem.NewDataElement(
		tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
	_ = ds.Add(dataelem.NewDataElement(
		tag.New(0x0010, 0x0020), dataelem.LO, []byte("PID-12345")))

	if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
		fmt.Printf("%s\n", elem.GetValue())
	}
	fmt.Println(ds.Length())

	// Output:
	// Doe^John
	// 2
}

// Elements can be selected by group, which is how DICOM organizes related
// attributes: group 0010 is patient information, 0020 is study and series.
func ExampleDataset_GetByGroup() {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("PID-1")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte("1.2.3")))

	patient := ds.GetByGroup(0x0010)
	fmt.Printf("%d patient attributes\n", len(patient))

	// Output: 2 patient attributes
}

// Sequences hold nested data sets. Each item is itself a Dataset, so the same
// API works at any depth.
func ExampleDataset_AddSequence() {
	// Build an item to nest.
	item := dataset.NewDataset()
	_ = item.Add(dataelem.NewDataElement(
		tag.New(0x0008, 0x0100), dataelem.SH, []byte("121071")))
	_ = item.Add(dataelem.NewDataElement(
		tag.New(0x0008, 0x0104), dataelem.LO, []byte("Finding")))

	seq := sequence.New()
	_ = seq.Append(item)

	ds := dataset.NewDataset()
	_ = ds.AddSequence(tag.New(0x0040, 0xA043), seq) // ConceptNameCodeSequence

	// Read it back.
	got, err := ds.GetSequence(tag.New(0x0040, 0xA043))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	first, _ := got.Get(0)
	child := first.(*dataset.Dataset)
	code, _ := child.Get(tag.New(0x0008, 0x0104))

	fmt.Printf("%d item(s), first meaning: %s\n", got.Length(), code.GetValue())

	// Output: 1 item(s), first meaning: Finding
}

// ForEach walks elements in insertion order.
func ExampleDataset_ForEach() {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("CT")))

	err := ds.ForEach(func(elem *dataelem.DataElement) error {
		t := elem.GetTag().(tag.Tag)
		fmt.Printf("%s %s %s\n", t, elem.GetVR(), elem.GetValue())
		return nil
	})
	if err != nil {
		fmt.Println("error:", err)
	}

	// Output:
	// (0010,0010) PN Doe^John
	// (0008,0060) CS CT
}
