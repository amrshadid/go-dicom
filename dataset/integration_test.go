package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDatasetDataElemIntegration tests integration with dataelem package
func TestDatasetDataElemIntegration(t *testing.T) {
	ds := dataset.NewDataset()

	// Create element
	elem := dataelem.NewDataElement(
		tag.New(0x0010, 0x0010),
		dataelem.PN,
		[]byte("Smith^John"),
	)

	// Add to dataset
	if err := ds.Add(elem); err != nil {
		t.Fatalf("Failed to add element: %v", err)
	}

	// Retrieve and verify
	retrieved, exists := ds.Get(tag.New(0x0010, 0x0010))
	if !exists {
		t.Fatal("Element not found in dataset")
	}

	if retrieved.GetVR() != dataelem.PN {
		t.Errorf("VR mismatch: got %s, want %s", retrieved.GetVR(), dataelem.PN)
	}

	if string(retrieved.GetValue().([]byte)) != "Smith^John" {
		t.Errorf("Value mismatch: got %s, want Smith^John", retrieved.GetValue())
	}
}

// TestDatasetTagIntegration tests integration with tag package
func TestDatasetTagIntegration(t *testing.T) {
	ds := dataset.NewDataset()

	// Add by tag
	tag1 := tag.New(0x0010, 0x0010) // Patient Name
	elem1 := dataelem.NewDataElement(tag1, dataelem.PN, []byte("Test^Patient"))
	ds.Add(elem1)

	// Query by group
	elems := ds.GetByGroup(0x0010)
	if len(elems) == 0 {
		t.Fatal("GetByGroup returned no elements")
	}

	// Check tag operations
	if !ds.Contains(tag1) {
		t.Fatal("Contains check failed")
	}

	tags := ds.Tags()
	if len(tags) == 0 {
		t.Fatal("Tags() returned no tags")
	}
}

// TestDatasetKeywordIntegration tests keyword-based access
func TestDatasetKeywordIntegration(t *testing.T) {
	ds := dataset.NewDataset()

	// Add by keyword
	if err := ds.AddByKeyword("PatientName", dataelem.PN, []byte("Doe^Jane")); err != nil {
		t.Fatalf("Failed to add by keyword: %v", err)
	}

	// Get by keyword
	elem, exists := ds.GetByKeyword("PatientName")
	if !exists {
		t.Fatal("Element not found by keyword")
	}

	value := string(elem.GetValue().([]byte))
	if value != "Doe^Jane" {
		t.Errorf("Value mismatch: got %s, want Doe^Jane", value)
	}

	// Check contains by keyword
	if !ds.ContainsByKeyword("PatientName") {
		t.Fatal("ContainsByKeyword returned false")
	}
}

// TestDatasetMergeIntegration tests dataset merging
func TestDatasetMergeIntegration(t *testing.T) {
	ds1 := dataset.NewDataset()
	ds2 := dataset.NewDataset()

	// Add to first dataset
	elem1 := dataelem.NewDataElement(
		tag.New(0x0010, 0x0010),
		dataelem.PN,
		[]byte("Smith^John"),
	)
	ds1.Add(elem1)

	// Add to second dataset
	elem2 := dataelem.NewDataElement(
		tag.New(0x0010, 0x0020),
		dataelem.LO,
		[]byte("12345"),
	)
	ds2.Add(elem2)

	// Merge
	if err := ds1.Merge(ds2); err != nil {
		t.Fatalf("Failed to merge: %v", err)
	}

	// Verify both elements are in first dataset
	if ds1.Length() != 2 {
		t.Errorf("Expected 2 elements after merge, got %d", ds1.Length())
	}

	if !ds1.Contains(tag.New(0x0010, 0x0010)) {
		t.Fatal("First element missing after merge")
	}

	if !ds1.Contains(tag.New(0x0010, 0x0020)) {
		t.Fatal("Second element missing after merge")
	}
}

// TestDatasetCloneIntegration tests deep cloning
func TestDatasetCloneIntegration(t *testing.T) {
	original := dataset.NewDataset()

	// Add elements
	elem := dataelem.NewDataElement(
		tag.New(0x0010, 0x0010),
		dataelem.PN,
		[]byte("Test^Name"),
	)
	original.Add(elem)

	// Clone
	cloned := original.Clone()

	// Verify clone has same elements
	if cloned.Length() != original.Length() {
		t.Errorf("Cloned dataset length mismatch: got %d, want %d",
			cloned.Length(), original.Length())
	}

	// Modify clone
	elem2 := dataelem.NewDataElement(
		tag.New(0x0010, 0x0020),
		dataelem.LO,
		[]byte("12345"),
	)
	cloned.Add(elem2)

	// Verify original is unchanged
	if original.Length() != 1 {
		t.Errorf("Original dataset was modified: length is %d, want 1", original.Length())
	}

	if cloned.Length() != 2 {
		t.Errorf("Cloned dataset modification failed: length is %d, want 2", cloned.Length())
	}
}

// TestDatasetSortIntegration tests dataset sorting
func TestDatasetSortIntegration(t *testing.T) {
	ds := dataset.NewDataset()

	// Add elements out of order
	tags := []tag.Tag{
		tag.New(0x0010, 0x1030), // (0010,1030)
		tag.New(0x0010, 0x0010), // (0010,0010)
		tag.New(0x0010, 0x0020), // (0010,0020)
	}

	for _, tg := range tags {
		elem := dataelem.NewDataElement(tg, dataelem.LO, []byte("test"))
		ds.Add(elem)
	}

	// Get insertion order
	insertionOrder := ds.Tags()
	if insertionOrder[0] != tags[0] {
		t.Fatal("Insertion order not preserved")
	}

	// Sort dataset
	sorted := ds.SortedDataset()

	// Verify sorted order
	sortedTags := sorted.Tags()
	for i := 1; i < len(sortedTags); i++ {
		if uint32(sortedTags[i]) < uint32(sortedTags[i-1]) {
			t.Errorf("Sorted tags not in ascending order: %v, %v",
				sortedTags[i-1], sortedTags[i])
		}
	}
}

// TestDatasetFilterIntegration tests filtering operations
func TestDatasetFilterIntegration(t *testing.T) {
	ds := dataset.NewDataset()

	// Add mixed VRs
	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Name"))
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("ID"))
	elem3 := dataelem.NewDataElement(tag.New(0x0010, 0x0030), dataelem.DA, []byte("20010101"))

	ds.Add(elem1)
	ds.Add(elem2)
	ds.Add(elem3)

	// Filter by VR
	loElements := ds.GetByVR(dataelem.LO)
	if len(loElements) != 1 {
		t.Errorf("GetByVR returned %d elements, want 1", len(loElements))
	}

	// Filter with custom function
	filtered := ds.FilteredElements(func(elem *dataelem.DataElement) bool {
		return elem.GetVR() == dataelem.PN || elem.GetVR() == dataelem.DA
	})

	if len(filtered) != 2 {
		t.Errorf("FilteredElements returned %d elements, want 2", len(filtered))
	}
}

// TestDatasetIterationIntegration tests iteration safety
func TestDatasetIterationIntegration(t *testing.T) {
	ds := dataset.NewDataset()

	// Add elements
	for i := 0; i < 10; i++ {
		elem := dataelem.NewDataElement(
			tag.New(uint16(0x0010), uint16(i)),
			dataelem.LO,
			[]byte("test"),
		)
		ds.Add(elem)
	}

	// Iterate and count
	count := 0
	err := ds.ForEach(func(elem *dataelem.DataElement) error {
		count++
		return nil
	})

	if err != nil {
		t.Fatalf("ForEach returned error: %v", err)
	}

	if count != 10 {
		t.Errorf("ForEach counted %d elements, want 10", count)
	}
}

// TestDatasetStringRepIntegration tests string representations
func TestDatasetStringRepIntegration(t *testing.T) {
	ds := dataset.NewDataset()

	// Add element
	elem := dataelem.NewDataElement(
		tag.New(0x0010, 0x0010),
		dataelem.PN,
		[]byte("Test^Name"),
	)
	ds.Add(elem)

	// Test String()
	str := ds.String()
	if len(str) == 0 {
		t.Fatal("String() returned empty string")
	}

	if !stringContains(str, "Test^Name") {
		t.Errorf("String representation missing element value")
	}

	if !stringContains(str, "0010,0010") {
		t.Errorf("String representation missing tag")
	}
}

// TestDatasetBatchOperations tests batch element operations
func TestDatasetBatchOperations(t *testing.T) {
	ds := dataset.NewDataset()

	// Add elements
	tags := []tag.Tag{
		tag.New(0x0010, 0x0010),
		tag.New(0x0010, 0x0020),
		tag.New(0x0010, 0x0030),
	}

	for i, t := range tags {
		elem := dataelem.NewDataElement(t, dataelem.LO, []byte("value"))
		ds.Add(elem)
		if i > 0 {
			// Add extra element
			extra := dataelem.NewDataElement(
				tag.New(0x0020, uint16(i)),
				dataelem.LO,
				[]byte("extra"),
			)
			ds.Add(extra)
		}
	}

	// Get multiple elements
	elems := ds.GetElements(tags...)
	if len(elems) != len(tags) {
		t.Errorf("GetElements returned %d elements, want %d", len(elems), len(tags))
	}

	// Remove multiple elements
	removed := ds.RemoveElements(tags...)
	if removed != len(tags) {
		t.Errorf("RemoveElements removed %d elements, want %d", removed, len(tags))
	}
}

// Helper function to check if string contains substring
func stringContains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != "" && substr != ""
}
