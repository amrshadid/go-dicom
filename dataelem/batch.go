package dataelem

import (
	"fmt"
	"sync"
)

// BatchOperation represents a batch of data elements to process together
type BatchOperation struct {
	elements []*DataElement
	errors   map[int]error // Index -> error mapping
	mu       sync.RWMutex
}

// NewBatchOperation creates a new batch operation
func NewBatchOperation(capacity int) *BatchOperation {
	return &BatchOperation{
		elements: make([]*DataElement, 0, capacity),
		errors:   make(map[int]error),
	}
}

// Add adds a data element to the batch
func (bo *BatchOperation) Add(elem *DataElement) error {
	bo.mu.Lock()
	defer bo.mu.Unlock()

	if elem == nil {
		return fmt.Errorf("cannot add nil element to batch")
	}

	bo.elements = append(bo.elements, elem)
	return nil
}

// AddMultiple adds multiple data elements at once
func (bo *BatchOperation) AddMultiple(elems ...*DataElement) error {
	bo.mu.Lock()
	defer bo.mu.Unlock()

	for i, elem := range elems {
		if elem == nil {
			return fmt.Errorf("element at index %d is nil", i)
		}
		bo.elements = append(bo.elements, elem)
	}
	return nil
}

// Count returns the number of elements in the batch
func (bo *BatchOperation) Count() int {
	bo.mu.RLock()
	defer bo.mu.RUnlock()
	return len(bo.elements)
}

// Get returns the element at the specified index
func (bo *BatchOperation) Get(index int) (*DataElement, error) {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	if index < 0 || index >= len(bo.elements) {
		return nil, fmt.Errorf("index out of range: %d", index)
	}
	return bo.elements[index], nil
}

// GetAll returns all elements in the batch
func (bo *BatchOperation) GetAll() []*DataElement {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	result := make([]*DataElement, len(bo.elements))
	copy(result, bo.elements)
	return result
}

// ProcessParallel applies a processing function to all elements in parallel
// Returns a map of index -> error for failed operations
func (bo *BatchOperation) ProcessParallel(fn func(*DataElement) error, numWorkers int) map[int]error {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	if numWorkers <= 0 {
		numWorkers = 1
	}

	results := make(map[int]error)
	var resultsMu sync.Mutex

	// Create work channel
	type work struct {
		index int
		elem  *DataElement
	}

	workChan := make(chan work, len(bo.elements))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < numWorkers && i < len(bo.elements); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range workChan {
				if err := fn(w.elem); err != nil {
					resultsMu.Lock()
					results[w.index] = err
					resultsMu.Unlock()
				}
			}
		}()
	}

	// Send work
	for i, elem := range bo.elements {
		workChan <- work{index: i, elem: elem}
	}
	close(workChan)

	// Wait for completion
	wg.Wait()

	return results
}

// ProcessSequential applies a processing function to all elements sequentially
// Stops on first error unless continueOnError is true
func (bo *BatchOperation) ProcessSequential(fn func(*DataElement) error, continueOnError bool) map[int]error {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	results := make(map[int]error)

	for i, elem := range bo.elements {
		if err := fn(elem); err != nil {
			results[i] = err
			if !continueOnError {
				break
			}
		}
	}

	return results
}

// Filter returns a new batch containing only elements that match the predicate
func (bo *BatchOperation) Filter(predicate func(*DataElement) bool) *BatchOperation {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	newBatch := NewBatchOperation(len(bo.elements))
	for _, elem := range bo.elements {
		if predicate(elem) {
			newBatch.elements = append(newBatch.elements, elem)
		}
	}
	return newBatch
}

// FilterByVR returns a new batch containing only elements with the specified VR
func (bo *BatchOperation) FilterByVR(vr VR) *BatchOperation {
	return bo.Filter(func(elem *DataElement) bool {
		return elem.GetVR() == vr
	})
}

// FilterByKeyword returns a new batch containing only elements matching the keyword pattern
func (bo *BatchOperation) FilterByKeyword(keyword string) *BatchOperation {
	return bo.Filter(func(elem *DataElement) bool {
		return elem.GetKeyword() == keyword
	})
}

// Map applies a transformation function to all elements and returns results
func (bo *BatchOperation) Map(fn func(*DataElement) interface{}) []interface{} {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	results := make([]interface{}, len(bo.elements))
	for i, elem := range bo.elements {
		results[i] = fn(elem)
	}
	return results
}

// ForEach applies a function to each element
func (bo *BatchOperation) ForEach(fn func(int, *DataElement)) {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	for i, elem := range bo.elements {
		fn(i, elem)
	}
}

// Clear removes all elements from the batch
func (bo *BatchOperation) Clear() {
	bo.mu.Lock()
	defer bo.mu.Unlock()

	bo.elements = bo.elements[:0]
	bo.errors = make(map[int]error)
}

// Validate validates all elements in the batch
func (bo *BatchOperation) Validate(isReading bool) map[int]error {
	results := make(map[int]error)
	var resultsMu sync.Mutex

	bo.mu.RLock()
	elems := make([]*DataElement, len(bo.elements))
	copy(elems, bo.elements)
	bo.mu.RUnlock()

	for i, elem := range elems {
		if err := elem.ValidateWithConfig(isReading); err != nil {
			resultsMu.Lock()
			results[i] = err
			resultsMu.Unlock()
		}
	}

	return results
}

// Clone creates a deep copy of the batch
func (bo *BatchOperation) Clone() *BatchOperation {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	newBatch := NewBatchOperation(len(bo.elements))
	for _, elem := range bo.elements {
		cloned := elem.Clone()
		newBatch.elements = append(newBatch.elements, cloned)
	}
	return newBatch
}

// ToJSON converts all elements to JSON representation
func (bo *BatchOperation) ToJSON() []map[string]interface{} {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(bo.elements))
	for _, elem := range bo.elements {
		jsonDict, err := elem.ToJSONDict()
		if err == nil && jsonDict != nil {
			result = append(result, jsonDict)
		}
	}
	return result
}

// Summary provides statistics about the batch
func (bo *BatchOperation) Summary() map[string]interface{} {
	bo.mu.RLock()
	defer bo.mu.RUnlock()

	vrCounts := make(map[string]int)
	vmSum := 0
	emptyCount := 0

	for _, elem := range bo.elements {
		vrCounts[string(elem.GetVR())]++
		vmSum += elem.GetVM()
		if elem.IsEmpty() {
			emptyCount++
		}
	}

	return map[string]interface{}{
		"total_elements":  len(bo.elements),
		"empty_elements":  emptyCount,
		"vr_distribution": vrCounts,
		"total_vm":        vmSum,
		"avg_vm":          float64(vmSum) / float64(len(bo.elements)),
	}
}

// BatchConversionResult holds results from batch conversion
type BatchConversionResult struct {
	Successful []*DataElement
	Failed     map[int]error
	Total      int
}

// ConvertBatchRawDataElements converts multiple RawDataElements at once
func ConvertBatchRawDataElements(rawElements []*RawDataElement) *BatchConversionResult {
	result := &BatchConversionResult{
		Successful: make([]*DataElement, 0, len(rawElements)),
		Failed:     make(map[int]error),
		Total:      len(rawElements),
	}

	for i, raw := range rawElements {
		if raw == nil {
			result.Failed[i] = fmt.Errorf("raw element at index %d is nil", i)
			continue
		}

		de, err := ConvertRawDataElement(raw)
		if err != nil {
			result.Failed[i] = err
		} else {
			result.Successful = append(result.Successful, de)
		}
	}

	return result
}

// SuccessRate returns the percentage of successful conversions
func (bcr *BatchConversionResult) SuccessRate() float64 {
	if bcr.Total == 0 {
		return 0
	}
	return float64(len(bcr.Successful)) / float64(bcr.Total) * 100
}

// HasErrors checks if there were any conversion failures
func (bcr *BatchConversionResult) HasErrors() bool {
	return len(bcr.Failed) > 0
}

// ErrorCount returns the number of failed conversions
func (bcr *BatchConversionResult) ErrorCount() int {
	return len(bcr.Failed)
}
