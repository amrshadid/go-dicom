package dataset_test

import (
	"sync"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDatasetThreadSafe tests thread safety.
func TestDatasetThreadSafe(t *testing.T) {
	ds := dataset.NewDataset()
	var wg sync.WaitGroup

	// 10 goroutines adding elements
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				elem := dataelem.NewDataElement(
					tag.New(uint16(0x0010+id), uint16(0x0010+j)),
					dataelem.LO,
					[]byte("value"),
				)
				ds.Add(elem)
			}
		}(i)
	}

	wg.Wait()

	if ds.Length() != 100 {
		t.Errorf("Length() = %d, want 100", ds.Length())
	}

	// 5 goroutines reading
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				ds.GetAll()
				ds.Tags()
			}
		}()
	}

	wg.Wait()
}

// TestDatasetConcurrentModification tests concurrent reads and writes
func TestDatasetConcurrentModification(t *testing.T) {
	ds := dataset.NewDataset()
	var wg sync.WaitGroup

	// Add initial elements
	for i := 0; i < 10; i++ {
		elem := dataelem.NewDataElement(
			tag.New(0x0010, uint16(0x0010+i)),
			dataelem.LO,
			[]byte("initial"),
		)
		ds.Add(elem)
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				ds.SetValue(
					tag.New(0x0010, uint16(0x0010+j%10)),
					[]byte("updated"),
				)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				ds.GetValue(tag.New(0x0010, uint16(0x0010+j%10)))
				ds.Contains(tag.New(0x0010, uint16(0x0010+j%10)))
			}
		}()
	}

	wg.Wait()

	if ds.Length() != 10 {
		t.Errorf("Length() = %d, want 10", ds.Length())
	}
}

// TestDatasetConcurrentRemove tests concurrent removal
func TestDatasetConcurrentRemove(t *testing.T) {
	ds := dataset.NewDataset()

	// Add 100 elements
	for i := 0; i < 100; i++ {
		elem := dataelem.NewDataElement(
			tag.New(uint16(0x0010+i/10), uint16(0x0010+i%10)),
			dataelem.LO,
			[]byte("value"),
		)
		ds.Add(elem)
	}

	var wg sync.WaitGroup

	// 10 goroutines each removing 10 elements
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ds.Remove(tag.New(uint16(0x0010+id), uint16(0x0010+j)))
			}
		}(i)
	}

	wg.Wait()

	if ds.Length() != 0 {
		t.Errorf("Length() = %d, want 0", ds.Length())
	}
}

// TestDatasetConcurrentMerge tests concurrent merge operations
func TestDatasetConcurrentMerge(t *testing.T) {
	ds := dataset.NewDataset()
	var wg sync.WaitGroup

	// 5 goroutines each merging a small dataset
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			tempDS := dataset.NewDataset()
			for j := 0; j < 5; j++ {
				elem := dataelem.NewDataElement(
					tag.New(uint16(0x0010+id), uint16(0x0010+j)),
					dataelem.LO,
					[]byte("value"),
				)
				tempDS.Add(elem)
			}

			ds.Merge(tempDS)
		}(i)
	}

	wg.Wait()

	if ds.Length() != 25 {
		t.Errorf("Length() = %d, want 25", ds.Length())
	}
}
