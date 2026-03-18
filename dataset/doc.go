// Package dataset provides high-level data structure and operations for in-memory DICOM datasets.
//
// This package defines the Dataset type which represents a collection of DICOM data elements indexed
// by tag for efficient access. All Dataset operations are thread-safe using sync.RWMutex, supporting
// concurrent reads and exclusive writes.
//
// # Core Concepts
//
// ## Dataset
//
// A Dataset represents an in-memory DICOM dataset as a collection of data elements:
//   - Elements are indexed by 32-bit DICOM tag for O(1) lookup
//   - Insertion order is maintained for sequential iteration
//   - Thread-safe for concurrent access (RWMutex-protected)
//   - Supports nested datasets through Sequence elements
//
// ## DICOM Tags
//
// DICOM uses 32-bit tags in format (GGGG,EEEE) where:
//   - GGGG: Group number (16-bit)
//   - EEEE: Element number (16-bit)
//
// Tags are converted to/from uint32 for efficient indexing.
//
// ## Value Representations (VR)
//
// Each element has a VR indicating the data type (PN for person name, DS for decimal string, etc.).
// The dataset delegates VR-specific operations to other packages.
//
// # Basic Usage
//
// ## Creating a Dataset
//
//	import (
//	    "github.com/amrshadid/go-dicom/dataset"
//	    "github.com/amrshadid/go-dicom/dataelem"
//	    "github.com/amrshadid/go-dicom/tag"
//	)
//
//	// Create empty dataset
//	ds := dataset.NewDataset()
//
// ## Adding Elements by Tag
//
//	// Create element
//	elem := dataelem.NewDataElement(
//	    tag.New(0x0010, 0x0010), // (0010,0010) Patient Name
//	    dataelem.PN,              // Person Name VR
//	    []byte("Smith^John"),      // Value
//	)
//	if err := ds.Add(elem); err != nil {
//	    log.Fatal(err)
//	}
//
// ## Adding Elements by Keyword
//
//	// Add using DICOM keyword
//	if err := ds.AddByKeyword("PatientName", dataelem.PN, []byte("Smith^John")); err != nil {
//	    log.Fatal(err)
//	}
//
// ## Retrieving Elements
//
//	// Get by tag
//	elem, exists := ds.Get(tag.New(0x0010, 0x0010))
//	if exists {
//	    value := elem.GetValue()
//	}
//
//	// Get by keyword
//	elem, exists := ds.GetByKeyword("PatientName")
//	if exists {
//	    value := elem.GetValue()
//	}
//
// ## Removing Elements
//
//	// Remove by tag
//	removed := ds.Remove(tag.New(0x0010, 0x0010))
//
//	// Remove by keyword
//	removed := ds.RemoveByKeyword("PatientName")
//
// ## Updating Elements
//
//	// Update value in existing element
//	if err := ds.UpdateElement(tag.New(0x0010, 0x0010), []byte("Doe^Jane")); err != nil {
//	    log.Fatal(err)
//	}
//
// # Advanced Usage
//
// ## Querying Elements
//
// ### Get All Elements
//
//	all := ds.GetAll()
//	for _, elem := range all {
//	    fmt.Printf("%s: %v\n", elem.GetTag(), elem.GetValue())
//	}
//
// ### Get Elements by Group
//
// Retrieve all elements from a specific group (e.g., patient info group 0010):
//
//	patientGroup := ds.GetByGroup(0x0010)
//	for _, elem := range patientGroup {
//	    // Process patient elements
//	}
//
// ### Get Elements by VR
//
// Retrieve all elements with a specific VR (e.g., all person names):
//
//	personNames := ds.GetByVR(dataelem.PN)
//	for _, elem := range personNames {
//	    // Process person name elements
//	}
//
// ### Get Elements by Tag Range
//
// Retrieve all elements within a tag range:
//
//	start := tag.New(0x0008, 0x0000)
//	end := tag.New(0x0008, 0xFFFF)
//	rangeElems := ds.GetByTagRange(start, end)
//
// ### Filter Elements
//
// Retrieve elements matching custom criteria:
//
//	oddTagElems := ds.FilteredElements(func(elem *dataelem.DataElement) bool {
//	    t := elem.GetTag()
//	    return (t.Element() & 1) == 1  // Odd element numbers
//	})
//
// ### Batch Operations
//
// Get multiple elements by tags:
//
//	tags := []tag.Tag{
//	    tag.New(0x0010, 0x0010),  // Patient Name
//	    tag.New(0x0010, 0x0020),  // Patient ID
//	}
//	elems := ds.GetElements(tags...)
//
// Remove multiple elements by tags:
//
//	removed := ds.RemoveElements(tags...)
//	fmt.Printf("Removed %d elements\n", removed)
//
// ## Iteration
//
// ### ForEach
//
// Iterate over all elements in insertion order:
//
//	err := ds.ForEach(func(elem *dataelem.DataElement) error {
//	    // Process element
//	    return nil
//	})
//
// ### Tags
//
// Get all tags in insertion order:
//
//	tags := ds.Tags()
//	for _, t := range tags {
//	    fmt.Println(t.Hex())
//	}
//
// ## Dataset Manipulation
//
// ### Clone
//
// Create a deep copy of the dataset:
//
//	cloned := ds.Clone()
//	// Cloned is independent; modifications don't affect original
//
// ### Merge
//
// Merge another dataset into this one (overwrites matching tags):
//
//	other := dataset.NewDataset()
//	// ... add elements to other ...
//	if err := ds.Merge(other); err != nil {
//	    log.Fatal(err)
//	}
//
// ### Sort
//
// Get elements sorted by tag:
//
//	sorted := ds.SortedDataset()
//	// Elements in sorted dataset are in ascending tag order
//
// ### Clear
//
// Remove all elements:
//
//	ds.Clear()
//	// Dataset is now empty
//
// ## Image Processing
//
// ### Pixel Data
//
// Access pixel array with decompression support:
//
//	pixelArray, err := ds.PixelArray()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ### Windowing
//
// Apply window/level transformation for display:
//
//	params := ds.GetWindowingParameters()
//	displayData, err := ds.ApplyWindowing(params.Center, params.Width)
//
// ### VOI LUT
//
// Apply Value of Interest (VOI) Look-Up Table:
//
//	if ds.HasVOILUT() {
//	    transformed, err := ds.ApplyVOILUT()
//	}
//
// ## String Representations
//
// ### Standard String
//
// Get compact representation:
//
//	fmt.Println(ds.String())  // Lists all elements
//
// ### Detailed String
//
// Get comprehensive representation with formatting options:
//
//	str := ds.DetailedString(
//	    &StringFormatOptions{
//	        MaxValueLength: 100,
//	        ShowVR:         true,
//	        ShowVM:         true,
//	    },
//	)
//
// ### Tree View
//
// Get hierarchical representation with sequences:
//
//	tree := ds.TreeString()
//	fmt.Println(tree)  // Shows sequences with nesting
//
// # Data Structures
//
// ## Dataset
//
//	type Dataset struct {
//	    // Unexported fields:
//	    // - mu: sync.RWMutex (protects access)
//	    // - elements: map[uint32]*dataelem.DataElement (tag -> element)
//	    // - order: []uint32 (insertion order)
//	    // - parent: *Dataset (for nested sequences)
//	}
//
// Thread-safe collection of data elements indexed by tag.
//
// # API Reference
//
// ## Creation
//
// ### NewDataset
//
//	func NewDataset() *Dataset
//
// Creates new empty DICOM dataset.
//
// **Returns:** Dataset pointer
//
// **Example:**
//
//	ds := dataset.NewDataset()
//
// ## Core Operations
//
// ### Add
//
//	func (ds *Dataset) Add(elem *dataelem.DataElement) error
//
// Adds or replaces a data element.
//
// ### Get
//
//	func (ds *Dataset) Get(t tag.Tag) (*dataelem.DataElement, bool)
//
// Retrieves element by tag (O(1) lookup).
//
// ### Contains
//
//	func (ds *Dataset) Contains(t tag.Tag) bool
//
// Checks if tag exists in dataset.
//
// ### Remove
//
//	func (ds *Dataset) Remove(t tag.Tag) bool
//
// Removes element by tag, returns success status.
//
// ### Clear
//
//	func (ds *Dataset) Clear()
//
// Removes all elements from dataset.
//
// ### Length
//
//	func (ds *Dataset) Length() int
//
// Returns number of elements in dataset.
//
// ## Value Operations
//
// ### GetValue
//
//	func (ds *Dataset) GetValue(t tag.Tag) []byte
//
// Returns raw value bytes for tag (nil if not found).
//
// ### SetValue
//
//	func (ds *Dataset) SetValue(t tag.Tag, value []byte) error
//
// Sets or creates element with bytes value.
//
// ### UpdateElement
//
//	func (ds *Dataset) UpdateElement(t tag.Tag, value []byte) error
//
// Updates existing element value (error if doesn't exist).
//
// ### GetStringValue
//
//	func (ds *Dataset) GetStringValue(t tag.Tag) (string, error)
//
// Returns decoded string value for tag.
//
// ### SetStringValue
//
//	func (ds *Dataset) SetStringValue(t tag.Tag, value string, vr dataelem.VR) error
//
// Sets string value with specified VR.
//
// ### GetIntValue
//
//	func (ds *Dataset) GetIntValue(t tag.Tag) (int64, error)
//
// Returns integer value from IS (Integer String) element.
//
// ### SetIntValue
//
//	func (ds *Dataset) SetIntValue(t tag.Tag, value int64) error
//
// Sets integer value as IS element.
//
// ### GetFloatValue
//
//	func (ds *Dataset) GetFloatValue(t tag.Tag) (float64, error)
//
// Returns float value from DS (Decimal String) or FD element.
//
// ### SetFloatValue
//
//	func (ds *Dataset) SetFloatValue(t tag.Tag, value float64) error
//
// Sets float value as DS element.
//
// ## Keyword Operations
//
// ### GetByKeyword
//
//	func (ds *Dataset) GetByKeyword(keyword string) (*dataelem.DataElement, bool)
//
// Retrieves element by DICOM keyword (e.g., "PatientName").
//
// ### GetValueByKeyword
//
//	func (ds *Dataset) GetValueByKeyword(keyword string) []byte
//
// Returns value bytes for keyword.
//
// ### SetValueByKeyword
//
//	func (ds *Dataset) SetValueByKeyword(keyword string, value []byte) error
//
// Sets value for keyword.
//
// ### GetStringByKeyword
//
//	func (ds *Dataset) GetStringByKeyword(keyword string) (string, error)
//
// Returns decoded string for keyword.
//
// ### SetStringByKeyword
//
//	func (ds *Dataset) SetStringByKeyword(keyword string, value string) error
//
// Sets string value for keyword.
//
// ## Collection Operations
//
// ### GetAll
//
//	func (ds *Dataset) GetAll() []*dataelem.DataElement
//
// Returns all elements in insertion order.
//
// ### Tags
//
//	func (ds *Dataset) Tags() []tag.Tag
//
// Returns all tags in insertion order.
//
// ### ForEach
//
//	func (ds *Dataset) ForEach(fn func(*dataelem.DataElement) error) error
//
// Iterates over elements, calls function for each.
//
// ### GetElements
//
//	func (ds *Dataset) GetElements(tags ...tag.Tag) []*dataelem.DataElement
//
// Returns multiple elements by tags.
//
// ### RemoveElements
//
//	func (ds *Dataset) RemoveElements(tags ...tag.Tag) int
//
// Removes multiple elements, returns count removed.
//
// ## Query Operations
//
// ### GetByGroup
//
//	func (ds *Dataset) GetByGroup(group uint16) []*dataelem.DataElement
//
// Returns all elements in a DICOM group.
//
// ### GetByVR
//
//	func (ds *Dataset) GetByVR(vr dataelem.VR) []*dataelem.DataElement
//
// Returns all elements with specific VR.
//
// ### GetByTagRange
//
//	func (ds *Dataset) GetByTagRange(start, end tag.Tag) []*dataelem.DataElement
//
// Returns elements within tag range (inclusive).
//
// ### FilteredElements
//
//	func (ds *Dataset) FilteredElements(filter func(*dataelem.DataElement) bool) []*dataelem.DataElement
//
// Returns elements matching filter function.
//
// ## Sequence Operations
//
// ### GetSequence
//
//	func (ds *Dataset) GetSequence(t tag.Tag) (*sequence.Sequence, bool)
//
// Returns sequence element (SQ VR).
//
// ### GetSequenceByKeyword
//
//	func (ds *Dataset) GetSequenceByKeyword(keyword string) (*sequence.Sequence, bool)
//
// Returns sequence by keyword.
//
// ### AddSequence
//
//	func (ds *Dataset) AddSequence(t tag.Tag) (*sequence.Sequence, error)
//
// Creates and adds new sequence element.
//
// ## Dataset Operations
//
// ### Clone
//
//	func (ds *Dataset) Clone() *Dataset
//
// Creates deep copy of dataset.
//
// ### Merge
//
//	func (ds *Dataset) Merge(other *Dataset) error
//
// Merges other dataset into this one (overwrites matches).
//
// ### SortedDataset
//
//	func (ds *Dataset) SortedDataset() *Dataset
//
// Returns new dataset with elements sorted by tag.
//
// ## Image Processing
//
// ### PixelArray
//
//	func (ds *Dataset) PixelArray() (interface{}, error)
//
// Returns decompressed pixel array (may return [][][]uint16 or [][][]uint8).
//
// ### GetWindowingParameters
//
//	func (ds *Dataset) GetWindowingParameters() *WindowingParams
//
// Extracts window/level from dataset or returns defaults.
//
// ### ApplyWindowing
//
//	func (ds *Dataset) ApplyWindowing(center, width int32) ([][][]uint8, error)
//
// Applies window/level transformation to pixel data.
//
// ### ApplyVOILUT
//
//	func (ds *Dataset) ApplyVOILUT() (interface{}, error)
//
// Applies Value of Interest LUT if present.
//
// ### ApplyModalityLUT
//
//	func (ds *Dataset) ApplyModalityLUT() (interface{}, error)
//
// Applies modality LUT transformation if present.
//
// ### ApplyPresentationLUT
//
//	func (ds *Dataset) ApplyPresentationLUT() (interface{}, error)
//
// Applies presentation LUT if present.
//
// ### HasVOILUT
//
//	func (ds *Dataset) HasVOILUT() bool
//
// Checks if dataset has VOI LUT Sequence.
//
// ### HasModalityLUT
//
//	func (ds *Dataset) HasModalityLUT() bool
//
// Checks if dataset has Modality LUT Sequence.
//
// ## String Representations
//
// ### String
//
//	func (ds *Dataset) String() string
//
// Returns compact string representation.
//
// ### DetailedString
//
//	func (ds *Dataset) DetailedString(opts *StringFormatOptions) string
//
// Returns detailed string with formatting options.
//
// ### TreeString
//
//	func (ds *Dataset) TreeString() string
//
// Returns hierarchical representation with sequences.
//
// ### CompactString
//
//	func (ds *Dataset) CompactString() string
//
// Returns minimal string representation.
//
// ### SummaryString
//
//	func (ds *Dataset) SummaryString() string
//
// Returns summary with key elements.
//
// ## JSON Operations
//
// ### ToJSON
//
//	func (ds *Dataset) ToJSON() ([]byte, error)
//
// Converts dataset to JSON representation (DICOM Part 18).
//
// ### FromJSON
//
//	func (ds *Dataset) FromJSON(data []byte) error
//
// Loads dataset from JSON representation.
//
// ## Validation
//
// ### Validate
//
//	func (ds *Dataset) Validate() error
//
// Validates all elements against DICOM dictionary.
//
// ### ValidateElement
//
//	func (ds *Dataset) ValidateElement(t tag.Tag) error
//
// Validates specific element.
//
// # Performance Characteristics
//
// | Operation | Complexity | Notes |
// |-----------|-----------|-------|
// | NewDataset | O(1) | Simple initialization |
// | Add | O(1) | Hash map insertion |
// | Get | O(1) | Hash map lookup |
// | Contains | O(1) | Hash map lookup |
// | Remove | O(n) | Linear removal from order slice |
// | GetAll | O(n) | Returns copy of all elements |
// | Length | O(1) | Map size |
// | Clear | O(1) | Reset map and slice |
// | Clone | O(n) | Deep copy all elements |
// | ForEach | O(n) | Iterates all elements |
// | GetByGroup | O(n) | Filters by group |
// | GetByVR | O(n) | Filters by VR |
// | GetByTagRange | O(n) | Filters by range |
// | FilteredElements | O(n) | Custom filter |
// | Merge | O(m) | m = elements in other |
// | GetSequence | O(1) | Hash map lookup |
// | PixelArray | O(p) | p = pixel count (may decompress) |
// | ApplyWindowing | O(p) | p = pixel count |
//
// # Thread Safety
//
// All Dataset methods are thread-safe:
//   - Read operations (Get, Contains, GetAll, etc.) acquire RLock (concurrent)
//   - Write operations (Add, Remove, Merge, etc.) acquire Lock (exclusive)
//   - Iteration via ForEach properly protects access
//   - Safe for concurrent readers and single writer
//
// # Concurrency Example
//
//	// Safe concurrent reads
//	go func() {
//	    elem, _ := ds.Get(tag)
//	}()
//	go func() {
//	    all := ds.GetAll()
//	}()
//
//	// Exclusive write
//	ds.Add(elem)
//
// # Use Cases
//
// ## Reading DICOM Files
//
// Parse file and work with dataset in memory.
//
// ## Building DICOM Files
//
// Create dataset, add elements, write to file.
//
// ## Image Viewer
//
// Access pixel data, apply windowing and LUTs.
//
// ## DICOM Server
//
// Store and retrieve datasets, modify via protocol.
//
// ## Data Processing Pipeline
//
// Read file, modify elements, write to new file.
//
// ## Multi-modal Analysis
//
// Merge multiple datasets, query across modalities.
//
// # Limitations
//
// - Dataset elements are shallow-copied during Clone (only data element references)
// - Sequences use separate Sequence type for nested datasets
// - No built-in transaction support for multi-element operations
// - Thread-safety is fine-grained (operation-level, not dataset-level)
//
// # Related Packages
//
// - **dataelem**: Data element definitions and operations
// - **tag**: DICOM tag definitions and dictionary
// - **element**: Value encoding/decoding
// - **sequence**: DICOM sequence handling
// - **filereader**: Reading DICOM files into datasets
// - **filewriter**: Writing datasets to DICOM files
// - **compress**: Pixel data decompression
// - **jsonrep**: JSON representation conversion
//
// # Best Practices
//
// ## Use Keywords When Available
//
// Keywords are more readable than tag numbers:
//
//	// Better
//	ds.GetByKeyword("PatientName")
//	// Rather than
//	ds.Get(tag.New(0x0010, 0x0010))
//
// ## Check Existence Before Access
//
// Always check for existence:
//
//	if elem, ok := ds.Get(t); ok {
//	    value := elem.GetValue()
//	}
//
// ## Use Type-Specific Methods
//
// Use GetStringValue, GetIntValue, etc. instead of manual conversion:
//
//	// Better
//	value, err := ds.GetStringValue(t)
//	// Rather than
//	elem, _ := ds.Get(t)
//	value := string(elem.GetValue().([]byte))
//
// ## Clone Before Modification
//
// Clone dataset when you want independent copy:
//
//	original := dataset.NewDataset()
//	// ... add elements ...
//	modified := original.Clone()
//	// Modify 'modified' without affecting 'original'
//
// ## Use Batch Operations
//
// Batch operations are more efficient than individual calls:
//
//	// Better
//	elems := ds.GetElements(tags...)
//	// Rather than
//	for _, t := range tags {
//	    elem, _ := ds.Get(t)
//	}
//
// ## Validate After Building
//
// Validate dataset after construction:
//
//	ds := dataset.NewDataset()
//	// ... add elements ...
//	if err := ds.Validate(); err != nil {
//	    log.Printf("Validation error: %v\n", err)
//	}
//
// # DICOM Compliance
//
// Implements DICOM standard for:
// - Data element storage and retrieval (PS3.5)
// - Tag indexing and organization
// - Keyword-to-tag mapping
// - Value representation handling
// - Sequence nesting
// - Windowing and VOI LUT (PS3.3)
//
// See: https://www.dicomstandard.org/
package dataset
