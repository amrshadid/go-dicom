package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDatasetForEach tests iterating over elements.
func TestDatasetForEach(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Doe"))

	ds.Add(elem1)
	ds.Add(elem2)

	count := 0
	ds.ForEach(func(elem *dataelem.DataElement) error {
		count++
		return nil
	})

	if count != 2 {
		t.Errorf("ForEach() iterated %d times, want 2", count)
	}
}

// TestDatasetTags tests getting all tags.
func TestDatasetTags(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))

	ds.Add(elem1)
	ds.Add(elem2)

	tags := ds.Tags()

	if len(tags) != 2 {
		t.Errorf("Tags() returned %d tags, want 2", len(tags))
	}

	elem1Tag := elem1.GetTag()
	if t1, ok := elem1Tag.(tag.Tag); ok && tags[0] != t1 {
		t.Errorf("Tags() order incorrect")
	}
}

// TestDatasetGetByGroup tests getting elements by group.
func TestDatasetGetByGroup(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Doe"))
	elem3 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	group10 := ds.GetByGroup(0x0010)
	if len(group10) != 2 {
		t.Errorf("GetByGroup() returned %d elements, want 2", len(group10))
	}

	group20 := ds.GetByGroup(0x0020)
	if len(group20) != 1 {
		t.Errorf("GetByGroup() returned %d elements, want 1", len(group20))
	}
}

// TestDatasetGetByVR tests getting elements by VR.
func TestDatasetGetByVR(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.PN, []byte("Doe"))
	elem3 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	pnElems := ds.GetByVR(dataelem.PN)
	if len(pnElems) != 2 {
		t.Errorf("GetByVR() returned %d elements, want 2", len(pnElems))
	}
}

// TestDatasetGetByTagRange tests getting elements by tag range.
func TestDatasetGetByTagRange(t *testing.T) {
	ds := dataset.NewDataset()

	for i := 0; i < 5; i++ {
		elem := dataelem.NewDataElement(
			tag.New(0x0010, uint16(0x0010+i*2)),
			dataelem.LO,
			[]byte("value"),
		)
		ds.Add(elem)
	}

	start := tag.New(0x0010, 0x0010)
	end := tag.New(0x0010, 0x0018)

	inRange := ds.GetByTagRange(start, end)
	if len(inRange) != 5 {
		t.Errorf("GetByTagRange() returned %d elements, want 5", len(inRange))
	}
}

// TestDatasetFilteredElements tests filtering elements.
func TestDatasetFilteredElements(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Doe"))
	elem3 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	filtered := ds.FilteredElements(func(elem *dataelem.DataElement) bool {
		val := elem.GetValue()
		if b, ok := val.([]byte); ok {
			return len(b) > 3
		}
		return false
	})

	if len(filtered) != 2 {
		t.Errorf("FilteredElements() returned %d elements, want 2", len(filtered))
	}
}

// TestDatasetGetElements tests getting multiple elements.
func TestDatasetGetElements(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Doe"))
	elem3 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	retrieved := ds.GetElements(
		tag.New(0x0010, 0x0010),
		tag.New(0x0020, 0x0010),
	)

	if len(retrieved) != 2 {
		t.Errorf("GetElements() returned %d elements, want 2", len(retrieved))
	}
}

// TestDatasetRemoveElements tests removing multiple elements.
func TestDatasetRemoveElements(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Doe"))
	elem3 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	count := ds.RemoveElements(
		tag.New(0x0010, 0x0010),
		tag.New(0x0020, 0x0010),
	)

	if count != 2 {
		t.Errorf("RemoveElements() removed %d elements, want 2", count)
	}

	if ds.Length() != 1 {
		t.Errorf("Length() = %d, want 1", ds.Length())
	}
}

// TestDatasetSorted tests getting sorted dataset.
func TestDatasetSorted(t *testing.T) {
	ds := dataset.NewDataset()

	// Add in non-sorted order
	elem3 := dataelem.NewDataElement(tag.New(0x0030, 0x0010), dataelem.UI, []byte("3"))
	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("1"))
	elem2 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.LO, []byte("2"))

	ds.Add(elem3)
	ds.Add(elem1)
	ds.Add(elem2)

	sorted := ds.SortedDataset()
	all := sorted.GetAll()

	elemTag0 := all[0].GetTag()
	if t0, ok := elemTag0.(tag.Tag); !ok || t0.Group() != 0x0010 {
		t.Error("SortedDataset() order incorrect")
	}
	elemTag1 := all[1].GetTag()
	if t1, ok := elemTag1.(tag.Tag); !ok || t1.Group() != 0x0020 {
		t.Error("SortedDataset() order incorrect")
	}
	elemTag2 := all[2].GetTag()
	if t2, ok := elemTag2.(tag.Tag); !ok || t2.Group() != 0x0030 {
		t.Error("SortedDataset() order incorrect")
	}
}

// TestDatasetGetStatistics tests getting statistics.
func TestDatasetGetStatistics(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Doe"))
	elem3 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.PN, []byte("Extra"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	stats := ds.GetStatistics()

	if stats.TotalElements != 3 {
		t.Errorf("TotalElements = %d, want 3", stats.TotalElements)
	}

	if stats.ByVR["PN"] != 2 {
		t.Errorf("ByVR[PN] = %d, want 2", stats.ByVR["PN"])
	}

	if stats.ByGroup[0x0010] != 2 {
		t.Errorf("ByGroup[0x0010] = %d, want 2", stats.ByGroup[0x0010])
	}
}
