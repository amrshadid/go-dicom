package dataelem_test

import (
	"fmt"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

func TestBatchOperation_Add(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil)
	if err := batch.Add(elem); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if batch.Count() != 1 {
		t.Errorf("Count = %d, want 1", batch.Count())
	}
}

func TestBatchOperation_AddNil(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	err := batch.Add(nil)
	if err == nil {
		t.Error("Expected error when adding nil element")
	}
}

func TestBatchOperation_AddMultiple(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil)
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, nil)
	elem3 := dataelem.NewDataElement(tag.New(0x0010, 0x0030), dataelem.DA, nil)

	if err := batch.AddMultiple(elem1, elem2, elem3); err != nil {
		t.Fatalf("AddMultiple failed: %v", err)
	}

	if batch.Count() != 3 {
		t.Errorf("Count = %d, want 3", batch.Count())
	}
}

func TestBatchOperation_Get(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Smith^John")
	batch.Add(elem)

	retrieved, err := batch.Get(0)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.GetTag() != elem.GetTag() {
		t.Errorf("Retrieved tag mismatch")
	}
}

func TestBatchOperation_GetOutOfRange(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil))

	_, err := batch.Get(10)
	if err == nil {
		t.Error("Expected error for out of range index")
	}
}

func TestBatchOperation_GetAll(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	elem1 := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil)
	elem2 := dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, nil)

	batch.Add(elem1)
	batch.Add(elem2)

	all := batch.GetAll()
	if len(all) != 2 {
		t.Errorf("GetAll count = %d, want 2", len(all))
	}
}

func TestBatchOperation_Clear(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil))
	batch.Clear()

	if batch.Count() != 0 {
		t.Errorf("After Clear, count = %d, want 0", batch.Count())
	}
}

func TestBatchOperation_Filter(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, nil))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0030), dataelem.PN, nil))

	filtered := batch.FilterByVR(dataelem.PN)
	if filtered.Count() != 2 {
		t.Errorf("Filtered count = %d, want 2", filtered.Count())
	}
}

func TestBatchOperation_ProcessSequential(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Smith^John"))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, "12345"))

	processed := 0
	results := batch.ProcessSequential(func(elem *dataelem.DataElement) error {
		processed++
		return nil
	}, true)

	if processed != 2 {
		t.Errorf("Processed = %d, want 2", processed)
	}

	if len(results) != 0 {
		t.Errorf("Results count = %d, want 0", len(results))
	}
}

func TestBatchOperation_ProcessSequentialWithError(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, nil))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0030), dataelem.DA, nil))

	processed := 0
	results := batch.ProcessSequential(func(elem *dataelem.DataElement) error {
		processed++
		if processed == 2 {
			return fmt.Errorf("error at index 1")
		}
		return nil
	}, false)

	if len(results) != 1 {
		t.Errorf("Results count = %d, want 1", len(results))
	}

	if processed != 2 {
		t.Errorf("Stopped at processed = %d, want 2", processed)
	}
}

func TestBatchOperation_Map(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Smith"))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, "12345"))

	results := batch.Map(func(elem *dataelem.DataElement) interface{} {
		return elem.GetKeyword()
	})

	if len(results) != 2 {
		t.Errorf("Map results count = %d, want 2", len(results))
	}
}

func TestBatchOperation_ForEach(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, nil))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, nil))

	count := 0
	batch.ForEach(func(i int, elem *dataelem.DataElement) {
		count++
	})

	if count != 2 {
		t.Errorf("ForEach count = %d, want 2", count)
	}
}

func TestBatchOperation_Clone(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Test")
	batch.Add(elem)

	cloned := batch.Clone()
	if cloned.Count() != 1 {
		t.Errorf("Cloned count = %d, want 1", cloned.Count())
	}

	// Modify original and check clone is unaffected
	batch.Clear()
	if cloned.Count() != 1 {
		t.Error("Clone was affected by original modification")
	}
}

func TestBatchOperation_Summary(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Smith^John"))
	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, "12345"))

	summary := batch.Summary()

	if summary["total_elements"] != 2 {
		t.Errorf("Summary total = %v, want 2", summary["total_elements"])
	}
}

func TestConvertBatchRawDataElements(t *testing.T) {
	rawElements := []*dataelem.RawDataElement{
		dataelem.NewRawDataElement(
			tag.New(0x0008, 0x0020),
			dataelem.DA,
			4,
			[]byte("2023"),
			0,
			false,
			true,
			false,
		),
		dataelem.NewRawDataElement(
			tag.New(0x0010, 0x0010),
			dataelem.PN,
			5,
			[]byte("Smith"),
			0,
			false,
			true,
			false,
		),
	}

	result := dataelem.ConvertBatchRawDataElements(rawElements)

	if result.SuccessRate() <= 0 {
		t.Errorf("Success rate = %f, want > 0", result.SuccessRate())
	}

	if result.HasErrors() && result.Total == 0 {
		t.Error("Result has errors but total is 0")
	}
}

func TestBatchOperation_ToJSON(t *testing.T) {
	batch := dataelem.NewBatchOperation(10)

	batch.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Smith^John"))

	jsonList := batch.ToJSON()
	if len(jsonList) != 1 {
		t.Errorf("JSON list length = %d, want 1", len(jsonList))
	}
}
