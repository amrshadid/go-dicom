package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestNewDataset tests creating a new dataset.
func TestNewDataset(t *testing.T) {
	ds := dataset.NewDataset()

	if ds == nil {
		t.Fatal("NewDataset returned nil")
	}

	if ds.Length() != 0 {
		t.Errorf("Length() = %d, want 0", ds.Length())
	}
}

// TestDatasetAdd tests adding elements.
func TestDatasetAdd(t *testing.T) {
	ds := dataset.NewDataset()

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	if err := ds.Add(elem); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if ds.Length() != 1 {
		t.Errorf("Length() = %d, want 1", ds.Length())
	}

	// Add nil element
	err := ds.Add(nil)
	if err == nil {
		t.Error("Add(nil) should error")
	}
}

// TestDatasetGet tests retrieving elements.
func TestDatasetGet(t *testing.T) {
	ds := dataset.NewDataset()

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	ds.Add(elem)

	retrieved, ok := ds.Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("Get() should find element")
	}

	if retrieved.GetTag() != elem.GetTag() {
		t.Errorf("Get() returned wrong element")
	}

	// Get non-existent
	_, ok = ds.Get(tag.New(0x0020, 0x0010))
	if ok {
		t.Error("Get() should not find non-existent element")
	}
}

// TestDatasetContains tests checking element existence.
func TestDatasetContains(t *testing.T) {
	ds := dataset.NewDataset()

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	ds.Add(elem)

	if !ds.Contains(tag.New(0x0010, 0x0010)) {
		t.Error("Contains() should return true")
	}

	if ds.Contains(tag.New(0x0020, 0x0010)) {
		t.Error("Contains() should return false")
	}
}

// TestDatasetRemove tests removing elements.
func TestDatasetRemove(t *testing.T) {
	ds := dataset.NewDataset()

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	ds.Add(elem)

	if ds.Length() != 1 {
		t.Errorf("Length() = %d, want 1", ds.Length())
	}

	ok := ds.Remove(tag.New(0x0010, 0x0010))
	if !ok {
		t.Error("Remove() should return true")
	}

	if ds.Length() != 0 {
		t.Errorf("Length() = %d, want 0", ds.Length())
	}

	// Remove non-existent
	ok = ds.Remove(tag.New(0x0010, 0x0010))
	if ok {
		t.Error("Remove() should return false for non-existent")
	}
}

// TestDatasetClear tests clearing the dataset.
func TestDatasetClear(t *testing.T) {
	ds := dataset.NewDataset()

	for i := 0; i < 5; i++ {
		elem := dataelem.NewDataElement(tag.New(0x0010, uint16(0x0010+i)), dataelem.LO, []byte("value"))
		ds.Add(elem)
	}

	if ds.Length() != 5 {
		t.Errorf("Length() = %d, want 5", ds.Length())
	}

	ds.Clear()

	if ds.Length() != 0 {
		t.Errorf("Length() = %d, want 0", ds.Length())
	}
}

// TestDatasetClone tests cloning a dataset.
func TestDatasetClone(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("12345"))
	ds.Add(elem1)
	ds.Add(elem2)

	cloned := ds.Clone()

	if cloned.Length() != ds.Length() {
		t.Errorf("Clone() length = %d, want %d", cloned.Length(), ds.Length())
	}

	// Verify deep copy
	retrieved, _ := cloned.Get(tag.New(0x0010, 0x0010))
	value := retrieved.GetValue()
	if b, ok := value.([]byte); !ok || string(b) != "John Doe" {
		t.Errorf("Clone() value mismatch")
	}
}

// TestDatasetGetAll tests getting all elements.
func TestDatasetGetAll(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("12345"))
	elem3 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	all := ds.GetAll()

	if len(all) != 3 {
		t.Errorf("GetAll() returned %d elements, want 3", len(all))
	}

	// Verify insertion order
	if all[0].GetTag() != elem1.GetTag() {
		t.Error("GetAll() order incorrect")
	}
}

// TestDatasetReplace tests replacing elements.
func TestDatasetReplace(t *testing.T) {
	ds := dataset.NewDataset()

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	ds.Add(elem1)

	// Replace with same tag
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.LO, []byte("Jane"))
	ds.Add(elem2)

	if ds.Length() != 1 {
		t.Errorf("Length() = %d, want 1 (should replace)", ds.Length())
	}

	retrieved, _ := ds.Get(tag.New(0x0010, 0x0010))
	if retrieved.GetVR() != dataelem.LO {
		t.Errorf("VR = %s, want %s", retrieved.GetVR(), dataelem.LO)
	}
}

// TestDatasetGetValue tests getting raw values.
func TestDatasetGetValue(t *testing.T) {
	ds := dataset.NewDataset()

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	ds.Add(elem)

	value := ds.GetValue(tag.New(0x0010, 0x0010))
	if string(value) != "John Doe" {
		t.Errorf("GetValue() = %s, want John Doe", string(value))
	}

	// Non-existent
	value = ds.GetValue(tag.New(0x9999, 0x9999))
	if value != nil {
		t.Error("GetValue() should return nil for non-existent")
	}
}

// TestDatasetSetValue tests setting raw values.
func TestDatasetSetValue(t *testing.T) {
	ds := dataset.NewDataset()

	if err := ds.SetValue(tag.New(0x0010, 0x0010), []byte("John Doe")); err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}

	if ds.Length() != 1 {
		t.Errorf("Length() = %d, want 1", ds.Length())
	}

	value := ds.GetValue(tag.New(0x0010, 0x0010))
	if string(value) != "John Doe" {
		t.Errorf("GetValue() = %s, want John Doe", string(value))
	}
}

// TestDatasetUpdateElement tests updating elements.
func TestDatasetUpdateElement(t *testing.T) {
	ds := dataset.NewDataset()

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	ds.Add(elem)

	if err := ds.UpdateElement(tag.New(0x0010, 0x0010), []byte("Jane")); err != nil {
		t.Fatalf("UpdateElement() error = %v", err)
	}

	value := ds.GetValue(tag.New(0x0010, 0x0010))
	if string(value) != "Jane" {
		t.Errorf("GetValue() = %s, want Jane", string(value))
	}

	// Update non-existent
	err := ds.UpdateElement(tag.New(0x9999, 0x9999), []byte("value"))
	if err == nil {
		t.Error("UpdateElement() should error for non-existent")
	}
}

// TestDatasetMerge tests merging datasets.
func TestDatasetMerge(t *testing.T) {
	ds1 := dataset.NewDataset()
	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John"))
	ds1.Add(elem1)

	ds2 := dataset.NewDataset()
	elem2 := dataelem.NewDataElement(tag.New(0x0020, 0x0010), dataelem.UI, []byte("1.2.3"))
	ds2.Add(elem2)

	if err := ds1.Merge(ds2); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if ds1.Length() != 2 {
		t.Errorf("Merge() result length = %d, want 2", ds1.Length())
	}

	// Merge nil
	err := ds1.Merge(nil)
	if err == nil {
		t.Error("Merge(nil) should error")
	}
}
