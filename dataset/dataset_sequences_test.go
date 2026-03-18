package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestAddSequence tests adding a sequence to a dataset
func TestAddSequence(t *testing.T) {
	ds := dataset.NewDataset()
	seq := sequence.New()

	// Add a child dataset to the sequence
	childDS := dataset.NewDataset()
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	childDS.Add(elem)
	seq.Append(childDS)

	// Add sequence to parent dataset
	seqTag := tag.New(0x0008, 0x1140) // Referenced Image Sequence
	if err := ds.AddSequence(seqTag, seq); err != nil {
		t.Fatalf("AddSequence() error = %v", err)
	}

	if !ds.HasSequence(seqTag) {
		t.Error("HasSequence() = false, want true")
	}

	// Verify parent relationship
	if !childDS.IsNested() {
		t.Error("Child dataset should be nested")
	}
	if childDS.Parent() != ds {
		t.Error("Child parent should be parent dataset")
	}
}

// TestGetSequence tests retrieving a sequence from a dataset
func TestGetSequence(t *testing.T) {
	ds := dataset.NewDataset()
	seq := sequence.New()
	seqTag := tag.New(0x0008, 0x1140)

	ds.AddSequence(seqTag, seq)

	retrieved, err := ds.GetSequence(seqTag)
	if err != nil {
		t.Fatalf("GetSequence() error = %v", err)
	}

	if retrieved == nil {
		t.Fatal("GetSequence() returned nil")
	}

	if retrieved.Length() != seq.Length() {
		t.Errorf("Length = %d, want %d", retrieved.Length(), seq.Length())
	}
}

// TestGetSequenceNonExistent tests getting a non-existent sequence
func TestGetSequenceNonExistent(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)

	_, err := ds.GetSequence(seqTag)
	if err == nil {
		t.Error("GetSequence() should return error for non-existent tag")
	}
}

// TestGetSequenceWrongVR tests getting a non-sequence element
func TestGetSequenceWrongVR(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0010, 0x0010)

	// Add non-sequence element
	elem := dataelem.NewDataElement(seqTag, dataelem.PN, []byte("John Doe"))
	ds.Add(elem)

	_, err := ds.GetSequence(seqTag)
	if err == nil {
		t.Error("GetSequence() should return error for non-sequence VR")
	}
}

// TestCreateSequence tests creating a new empty sequence
func TestCreateSequence(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)

	seq, err := ds.CreateSequence(seqTag)
	if err != nil {
		t.Fatalf("CreateSequence() error = %v", err)
	}

	if seq == nil {
		t.Fatal("CreateSequence() returned nil")
	}

	if !seq.IsEmpty() {
		t.Error("Newly created sequence should be empty")
	}

	if !ds.HasSequence(seqTag) {
		t.Error("Dataset should contain the created sequence")
	}
}

// TestAppendToSequence tests appending items to a sequence
func TestAppendToSequence(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)

	// Append to non-existent sequence (should create it)
	childDS1 := dataset.NewDataset()
	if err := ds.AppendToSequence(seqTag, childDS1); err != nil {
		t.Fatalf("AppendToSequence() error = %v", err)
	}

	// Append to existing sequence
	childDS2 := dataset.NewDataset()
	if err := ds.AppendToSequence(seqTag, childDS2); err != nil {
		t.Fatalf("AppendToSequence() error = %v", err)
	}

	length, err := ds.SequenceLength(seqTag)
	if err != nil {
		t.Fatalf("SequenceLength() error = %v", err)
	}

	if length != 2 {
		t.Errorf("SequenceLength() = %d, want 2", length)
	}
}

// TestGetSequenceItem tests retrieving specific items from a sequence
func TestGetSequenceItem(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)
	seq := sequence.New()

	childDS := dataset.NewDataset()
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe"))
	childDS.Add(elem)
	seq.Append(childDS)

	ds.AddSequence(seqTag, seq)

	item, err := ds.GetSequenceItem(seqTag, 0)
	if err != nil {
		t.Fatalf("GetSequenceItem() error = %v", err)
	}

	if item == nil {
		t.Fatal("GetSequenceItem() returned nil")
	}

	retrievedDS, ok := item.(*dataset.Dataset)
	if !ok {
		t.Fatal("Item is not a Dataset")
	}

	if !retrievedDS.Contains(tag.New(0x0010, 0x0010)) {
		t.Error("Retrieved dataset should contain PatientName tag")
	}
}

// TestGetSequenceDataset tests type-safe dataset retrieval
func TestGetSequenceDataset(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)
	seq := sequence.New()

	childDS := dataset.NewDataset()
	seq.Append(childDS)
	ds.AddSequence(seqTag, seq)

	retrieved, err := ds.GetSequenceDataset(seqTag, 0)
	if err != nil {
		t.Fatalf("GetSequenceDataset() error = %v", err)
	}

	if retrieved == nil {
		t.Fatal("GetSequenceDataset() returned nil")
	}
}

// TestGetSequenceDatasetWrongType tests error when item is not a dataset
func TestGetSequenceDatasetWrongType(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)
	seq := sequence.New()

	// Append non-dataset item
	seq.Append("not a dataset")
	ds.AddSequence(seqTag, seq)

	_, err := ds.GetSequenceDataset(seqTag, 0)
	if err == nil {
		t.Error("GetSequenceDataset() should return error for non-Dataset item")
	}
}

// TestRemoveSequence tests removing a sequence and clearing parent references
func TestRemoveSequence(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)
	seq := sequence.New()

	childDS := dataset.NewDataset()
	seq.Append(childDS)
	ds.AddSequence(seqTag, seq)

	// Verify parent is set
	if childDS.Parent() == nil {
		t.Error("Child should have parent before removal")
	}

	if err := ds.RemoveSequence(seqTag); err != nil {
		t.Fatalf("RemoveSequence() error = %v", err)
	}

	if ds.HasSequence(seqTag) {
		t.Error("Sequence should be removed")
	}

	// Verify parent is cleared
	if childDS.Parent() != nil {
		t.Error("Child parent should be nil after removal")
	}
}

// TestIterAll tests recursive iteration through nested sequences
func TestIterAll(t *testing.T) {
	// Create parent dataset with a sequence
	parent := dataset.NewDataset()
	parentElem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Parent"))
	parent.Add(parentElem)

	// Create nested sequence with two child datasets
	seq := sequence.New()
	seqTag := tag.New(0x0008, 0x1140)

	child1 := dataset.NewDataset()
	child1Elem := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Child1"))
	child1.Add(child1Elem)

	child2 := dataset.NewDataset()
	child2Elem := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Child2"))
	child2.Add(child2Elem)

	seq.Append(child1)
	seq.Append(child2)
	parent.AddSequence(seqTag, seq)

	// Count all elements using IterAll
	count := 0
	err := parent.IterAll(func(ds *dataset.Dataset, elem *dataelem.DataElement) error {
		count++
		return nil
	})

	if err != nil {
		t.Fatalf("IterAll() error = %v", err)
	}

	// Should find: 1 parent elem + 1 sequence elem + 1 child1 elem + 1 child2 elem = 4
	if count != 4 {
		t.Errorf("IterAll() visited %d elements, want 4", count)
	}
}

// TestIterAllNested tests deeply nested sequences
func TestIterAllNested(t *testing.T) {
	// Create 3-level hierarchy
	root := dataset.NewDataset()
	rootElem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Root"))
	root.Add(rootElem)

	// Level 1 sequence
	seq1 := sequence.New()
	level1 := dataset.NewDataset()
	level1Elem := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Level1"))
	level1.Add(level1Elem)

	// Level 2 sequence
	seq2 := sequence.New()
	level2 := dataset.NewDataset()
	level2Elem := dataelem.NewDataElement(tag.New(0x0010, 0x0030), dataelem.LO, []byte("Level2"))
	level2.Add(level2Elem)

	seq2.Append(level2)
	level1.AddSequence(tag.New(0x0008, 0x1199), seq2) // Nested sequence

	seq1.Append(level1)
	root.AddSequence(tag.New(0x0008, 0x1140), seq1)

	// Verify depth
	if level2.Depth() != 2 {
		t.Errorf("Level2 depth = %d, want 2", level2.Depth())
	}

	// Count all elements
	count := 0
	err := root.IterAll(func(ds *dataset.Dataset, elem *dataelem.DataElement) error {
		count++
		return nil
	})

	if err != nil {
		t.Fatalf("IterAll() error = %v", err)
	}

	// Should find: root (1 elem + 1 seq) + level1 (1 elem + 1 seq) + level2 (1 elem) = 5
	if count != 5 {
		t.Errorf("IterAll() visited %d elements, want 5", count)
	}
}

// TestFindInSequences tests searching for datasets in sequences
func TestFindInSequences(t *testing.T) {
	parent := dataset.NewDataset()
	seq := sequence.New()

	// Create child datasets with specific tags
	child1 := dataset.NewDataset()
	child1.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("CT")))

	child2 := dataset.NewDataset()
	child2.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("MR")))

	child3 := dataset.NewDataset()
	child3.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("CT")))

	seq.Append(child1)
	seq.Append(child2)
	seq.Append(child3)
	parent.AddSequence(tag.New(0x0008, 0x1140), seq)

	// Find all CT modality datasets
	results := parent.FindInSequences(func(ds *dataset.Dataset) bool {
		elem, exists := ds.Get(tag.New(0x0008, 0x0060))
		if !exists {
			return false
		}
		value := elem.GetValue()
		if b, ok := value.([]byte); ok {
			return string(b) == "CT"
		}
		return false
	})

	if len(results) != 2 {
		t.Errorf("FindInSequences() found %d datasets, want 2", len(results))
	}
}

// TestGetAllSequences tests retrieving all sequences from a dataset
func TestGetAllSequences(t *testing.T) {
	ds := dataset.NewDataset()

	// Add multiple sequences
	seq1 := sequence.New()
	seq2 := sequence.New()
	seq3 := sequence.New()

	ds.AddSequence(tag.New(0x0008, 0x1140), seq1)
	ds.AddSequence(tag.New(0x0008, 0x1150), seq2)
	ds.AddSequence(tag.New(0x0008, 0x1199), seq3)

	// Add non-sequence element
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John")))

	sequences := ds.GetAllSequences()

	if len(sequences) != 3 {
		t.Errorf("GetAllSequences() returned %d sequences, want 3", len(sequences))
	}
}

// TestCloneWithSequences tests deep cloning including sequences
func TestCloneWithSequences(t *testing.T) {
	original := dataset.NewDataset()
	originalElem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Original"))
	original.Add(originalElem)

	// Add sequence with child dataset
	seq := sequence.New()
	child := dataset.NewDataset()
	childElem := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("Child"))
	child.Add(childElem)
	seq.Append(child)

	seqTag := tag.New(0x0008, 0x1140)
	original.AddSequence(seqTag, seq)

	// Clone with sequences
	cloned := original.CloneWithSequences()

	if cloned == original {
		t.Error("Cloned dataset should be a different instance")
	}

	// Verify sequence was cloned
	if !cloned.HasSequence(seqTag) {
		t.Error("Cloned dataset should have sequence")
	}

	clonedSeq, _ := cloned.GetSequence(seqTag)
	clonedChild, _ := clonedSeq.Get(0)
	clonedChildDS := clonedChild.(*dataset.Dataset)

	if clonedChildDS == child {
		t.Error("Cloned child should be a different instance")
	}

	// Verify parent relationship
	if clonedChildDS.Parent() != cloned {
		t.Error("Cloned child parent should be cloned dataset")
	}

	// Modify original and verify clone is unaffected
	original.SetValue(tag.New(0x0010, 0x0010), []byte("Modified"))
	clonedValue := cloned.GetValue(tag.New(0x0010, 0x0010))
	if string(clonedValue) != "Original" {
		t.Error("Cloned dataset should be independent")
	}
}

// TestSequenceLengthNonExistent tests getting length of non-existent sequence
func TestSequenceLengthNonExistent(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)

	_, err := ds.SequenceLength(seqTag)
	if err == nil {
		t.Error("SequenceLength() should return error for non-existent sequence")
	}
}

// TestHasSequence tests checking for sequence presence
func TestHasSequence(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)

	if ds.HasSequence(seqTag) {
		t.Error("HasSequence() = true for non-existent sequence")
	}

	seq := sequence.New()
	ds.AddSequence(seqTag, seq)

	if !ds.HasSequence(seqTag) {
		t.Error("HasSequence() = false for existing sequence")
	}
}

// TestAddSequenceNil tests adding nil sequence
func TestAddSequenceNil(t *testing.T) {
	ds := dataset.NewDataset()
	seqTag := tag.New(0x0008, 0x1140)

	err := ds.AddSequence(seqTag, nil)
	if err == nil {
		t.Error("AddSequence() should return error for nil sequence")
	}
}

// TestCountNestedDatasets tests counting nested datasets
func TestCountNestedDatasets(t *testing.T) {
	// Test empty dataset
	ds := dataset.NewDataset()
	count := ds.CountNestedDatasets()
	if count != 0 {
		t.Errorf("CountNestedDatasets() = %d, want 0 for empty dataset", count)
	}

	// Test with one level of nesting
	seq1 := sequence.New()
	child1 := dataset.NewDataset()
	child2 := dataset.NewDataset()
	seq1.Append(child1)
	seq1.Append(child2)
	ds.AddSequence(tag.New(0x0008, 0x1140), seq1)

	count = ds.CountNestedDatasets()
	if count != 2 {
		t.Errorf("CountNestedDatasets() = %d, want 2", count)
	}

	// Test with multiple levels of nesting
	seq2 := sequence.New()
	grandchild1 := dataset.NewDataset()
	grandchild2 := dataset.NewDataset()
	seq2.Append(grandchild1)
	seq2.Append(grandchild2)
	child1.AddSequence(tag.New(0x0008, 0x1199), seq2)

	count = ds.CountNestedDatasets()
	if count != 4 {
		t.Errorf("CountNestedDatasets() = %d, want 4 (2 children + 2 grandchildren)", count)
	}

	// Test with multiple sequences at root level
	seq3 := sequence.New()
	child3 := dataset.NewDataset()
	seq3.Append(child3)
	ds.AddSequence(tag.New(0x0008, 0x1150), seq3)

	count = ds.CountNestedDatasets()
	if count != 5 {
		t.Errorf("CountNestedDatasets() = %d, want 5", count)
	}
}

// TestCountNestedDatasetsComplex tests complex hierarchy
func TestCountNestedDatasetsComplex(t *testing.T) {
	root := dataset.NewDataset()

	// Create a complex 3-level hierarchy
	// Level 1: 2 sequences with 2 items each = 4 datasets
	seq1 := sequence.New()
	l1_child1 := dataset.NewDataset()
	l1_child2 := dataset.NewDataset()
	seq1.Append(l1_child1)
	seq1.Append(l1_child2)
	root.AddSequence(tag.New(0x0008, 0x1140), seq1)

	seq2 := sequence.New()
	l1_child3 := dataset.NewDataset()
	l1_child4 := dataset.NewDataset()
	seq2.Append(l1_child3)
	seq2.Append(l1_child4)
	root.AddSequence(tag.New(0x0008, 0x1150), seq2)

	// Level 2: Add nested sequence to l1_child1 with 2 items
	seq3 := sequence.New()
	l2_child1 := dataset.NewDataset()
	l2_child2 := dataset.NewDataset()
	seq3.Append(l2_child1)
	seq3.Append(l2_child2)
	l1_child1.AddSequence(tag.New(0x0008, 0x1199), seq3)

	// Level 3: Add deeply nested sequence to l2_child1 with 1 item
	seq4 := sequence.New()
	l3_child1 := dataset.NewDataset()
	seq4.Append(l3_child1)
	l2_child1.AddSequence(tag.New(0x0040, 0x0260), seq4)

	// Total: 4 (level 1) + 2 (level 2) + 1 (level 3) = 7
	count := root.CountNestedDatasets()
	if count != 7 {
		t.Errorf("CountNestedDatasetsComplex() = %d, want 7", count)
	}
}
