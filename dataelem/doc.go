// Package dataelem provides core DICOM data element structures and operations.
//
// # Overview
//
// The dataelem package implements the fundamental DICOM data element concept -
// the basic building block of DICOM files. A DataElement consists of:
//   - Tag: A 32-bit identifier (group, element numbers)
//   - VR: Value Representation (data type code, 28 types supported)
//   - VM: Value Multiplicity (number of values)
//   - Value: The actual data
//   - Keyword: Human-readable name from DICOM standard
//
// This package supports all 28 DICOM Value Representations (VRs):
// AE, AS, AT, CS, DA, DS, DT, FD, FL, IS, LO, LT, OB, OD, OF, OL, OW, PN,
// SH, SL, SQ, SS, ST, TM, UI, UL, UN, UT
//
// # Core Types
//
// ## DataElement
//
// The primary type representing a DICOM data element:
//
//	elem := dataelem.NewDataElement(0x00100010, dataelem.PN, "Smith^John")
//	name, _ := elem.GetValue()
//	fmt.Println(name) // Output: Smith^John
//
// DataElements are thread-safe for concurrent reads via RWMutex. For write
// operations, use explicit locking.
//
// ## SequenceItem
//
// A container for nested DataElements within a sequence (VR = SQ):
//
//	item := dataelem.NewSequenceItem()
//	item.AddDataElement(dataelem.NewDataElement(0x00100010, dataelem.PN, "John"))
//	items := item.GetDataElements()
//
// Sequences enable hierarchical DICOM data structures commonly used for
// Referenced Images, Referenced Procedures, Waveforms, and more.
//
// ## RawDataElement
//
// Immutable representation of a data element's raw bytes. Used when reading
// from files before value conversion:
//
//	raw := dataelem.NewRawDataElement(0x00100010, "PN", 4, []byte("John"))
//	// RawDataElement is immutable - no SetValue() method
//
// # Value Representations (VRs)
//
// Each VR defines specific rules for data storage and interpretation:
//
// ## Text VRs
//
//   - AE (Application Entity): ASCII string, max 16 chars, medical device identifier
//   - AS (Age String): "nnnD", "nnnM", "nnnW", "nnnY" format (e.g., "045Y")
//   - CS (Code String): ASCII uppercase, max 16 chars, fixed vocabulary
//   - LO (Long String): ASCII string, max 64 chars, no control characters
//   - LT (Long Text): ASCII string, max 10240 chars, may include newlines
//   - PN (Person Name): Alphabetic^Ideographic^Phonetic with subcomponents
//   - SH (Short String): ASCII string, max 16 chars
//   - ST (Short Text): ASCII string, max 1024 chars
//   - UI (Unique Identifier): DICOM UID format (numeric components dot-separated)
//   - UT (Unlimited Text): UTF-8 string, max 2^32-2 chars
//
// ## Numeric VRs
//
//   - DS (Decimal String): Decimal number represented as ASCII (e.g., "123.45")
//   - FD (Floating Point Double): 64-bit IEEE 754 float
//   - FL (Floating Point Single): 32-bit IEEE 754 float
//   - IS (Integer String): Integer represented as ASCII (e.g., "12345")
//   - OD (Other Double): Byte sequence containing 64-bit floats
//   - OF (Other Float): Byte sequence containing 32-bit floats
//   - SL (Signed Long): 32-bit signed integer
//   - SS (Signed Short): 16-bit signed integer
//   - UL (Unsigned Long): 32-bit unsigned integer
//   - US (Unsigned Short): 16-bit unsigned integer
//
// ## Binary VRs
//
//   - AT (Attribute Tag): 4 bytes representing another tag
//   - OB (Other Byte): Binary data (common for pixel data, overlays)
//   - OL (Other Long): Binary data with 32-bit elements
//   - OW (Other Word): Binary data with 16-bit elements
//   - UN (Unknown): Binary data with unknown structure
//
// ## Special VRs
//
//   - SQ (Sequence): Container for nested items (DataElements)
//
// # VR Information
//
// Query VR metadata:
//
//	info := dataelem.GetVRInfo(dataelem.PN)
//	fmt.Printf("PN: bytes=%d, bytes2=%s\n", info.Bytes, info.Bytes2)
//	// Output: PN: bytes=0, bytes2=00
//
// Available fields in VRInfo:
//   - Code: The 2-character VR code
//   - Bytes: Minimum bytes per value
//   - Bytes2: Optional 2-byte format (for text VRs with padding)
//   - Padding: Padding character (null or space)
//   - Type: VRType enum (TEXT, NUMERIC, BINARY, SEQUENCE)
//
// # Value Conversion
//
// The convert.go file provides type-safe conversion between Go types and
// DICOM binary formats:
//
//	// Convert uint16 to DICOM US (Unsigned Short)
//	bytes := dataelem.ConvertToBytes(uint16(42), binary.LittleEndian, dataelem.US)
//
//	// Convert DICOM IS (Integer String) to int64
//	value, _ := dataelem.ConvertFromBytes([]byte("12345"), dataelem.IS)
//	intVal := value.(int64) // Type assertion
//
// Supported conversions handle:
//   - Byte order (Little Endian, Big Endian)
//   - Multi-value encoding (values separated by backslash in text VRs)
//   - Character set awareness (handled by charset module)
//   - Padding requirements (null bytes or spaces)
//
// # Validation
//
// Comprehensive validation with configurable behavior:
//
//	// Config-aware validation respecting global settings
//	if err := elem.ValidateWithConfig(isReading bool); err != nil {
//	    log.Printf("Validation error: %v", err)
//	}
//
//	// Direct VR validation without config
//	if err := elem.ValidateVR(); err != nil {
//	    log.Printf("VR validation failed: %v", err)
//	}
//
// Validation checks:
//   - VR type correctness
//   - Value length constraints
//   - Format compliance (UIDs, dates, times)
//   - Multi-value structure
//   - Dictionary conformance
//
// Validation modes (controlled by config):
//   - RAISE: Validation errors are returned as errors
//   - WARN: Validation errors are logged as warnings, function returns nil
//   - IGNORE: Validation errors are silently ignored
//
// # Caching
//
// ValueCache provides thread-safe caching of converted values with TTL support:
//
//	cache := dataelem.NewValueCache(1000) // Max 1000 entries
//	cache.Set("key", value, time.Hour)    // 1 hour TTL
//	if val, ok := cache.Get("key"); ok {
//	    fmt.Println(val)
//	}
//
// Cache features:
//   - Configurable max size with LRU eviction
//   - TTL (Time To Live) per entry
//   - Hit/miss statistics
//   - Enable/disable toggle
//   - Thread-safe access with separate lock for counters
//
// # JSON Representation
//
// DICOM JSON Model (DICOM Part 18) support for web APIs:
//
//	elem := dataelem.NewDataElement(0x00100010, dataelem.PN, "Smith^John")
//	jsonData, _ := elem.ToJSON()
//	// Output: {"00100010":{"vr":"PN","Value":["Smith^John"]}}
//
// JSON includes:
//   - VR field (DICOM standard requirement)
//   - Value field (array of values, even for single values)
//   - Bulk data references (for large binary data)
//   - InlineBinary (base64 encoded for small binary)
//
// # Character Set Encoding
//
// Multi-byte character set support for text VRs:
//
//	// Handle SpecificCharacterSet (ISO_IR 87 = Japanese)
//	decoded := dataelem.DecodeText([]byte("..."), []string{"ISO_IR 87"})
//
// Supported character sets include:
//   - ASCII (default)
//   - Latin alphabets (ISO_IR 100, 101, 109, 110, 144)
//   - Asian (ISO_IR 87 Japanese, 149 Korean)
//   - Escape sequence support for multi-byte encodings
//
// # Waveform Data
//
// Special handling for waveform sequences (5400,0100):
//
//	groups, _ := elem.WaveformSequenceToGroups(binary.LittleEndian)
//	for _, group := range groups {
//	    fmt.Printf("Channels: %d, Samples: %d, Rate: %.1f Hz\n",
//	        group.NumberOfWaveformChannels,
//	        group.NumberOfSamples,
//	        group.SamplingFrequency)
//	}
//
// Waveform groups contain:
//   - Multi-channel signal data ([][]int16, [][]int32, etc.)
//   - Sampling frequency
//   - Multiplex group label
//   - Sample interpretation
//
// # Batch Operations
//
// Process multiple DataElements efficiently:
//
//	batch := dataelem.NewBatchOperation()
//	batch.Add(elem1, elem2, elem3)
//
//	// Filter elements
//	filtered := batch.Filter(func(e *dataelem.DataElement) bool {
//	    return e.GetVR() == dataelem.PN
//	})
//
//	// Map transformation
//	results := batch.Map(func(e *dataelem.DataElement) string {
//	    return e.GetTagName()
//	})
//
//	// Sequential processing with error handling
//	batch.ProcessSequential(func(e *dataelem.DataElement) error {
//	    return e.ValidateWithConfig(false)
//	})
//
// Batch supports:
//   - Adding/removing elements
//   - Filtering by predicate
//   - Mapping to other types
//   - Iteration (ForEach)
//   - Sequential/parallel processing
//   - JSON marshaling of all elements
//
// # Streaming Values
//
// Handle large binary data efficiently without loading into memory:
//
//	stream := dataelem.NewStreamingValue(reader, size)
//	buffered := dataelem.NewBufferedDataElement(0x7FE00010, dataelem.OB)
//	buffered.SetStreamingValue(stream)
//
// Streaming supports:
//   - Reading data incrementally
//   - Querying total size without reading all data
//   - Buffering strategy (in-memory vs. streaming)
//   - Lazy evaluation of large values
//
// This is critical for pixel data (7FE0,0010) in large medical images that
// can be hundreds of MB.
//
// # Dictionary Integration
//
// Access DICOM standard information:
//
//	info := elem.GetDictionaryInfo()
//	if info != nil {
//	    fmt.Printf("Tag: %s, Name: %s, VR: %s, VM: %s\n",
//	        info.Tag, info.Name, info.VR, info.VM)
//	}
//
// Dictionary information includes:
//   - Tag (hex format "0010,0010")
//   - Name (human-readable)
//   - VR (expected Value Representation)
//   - VM (expected Value Multiplicity)
//   - Retired status
//
// # Thread Safety
//
// DataElement uses RWMutex for thread-safe concurrent access:
//
//	// Safe for concurrent reads
//	elem.GetValue()     // No lock held, only reads
//	elem.GetTag()       // No lock held, only reads
//	elem.GetVR()        // No lock held, only reads
//
//	// Safe for writes (should not use concurrently with reads)
//	elem.SetValue(newValue)
//	elem.Clear()
//
// Note: The mutex prevents data races but does not provide semantic
// consistency. Serialize writes externally if needed.
//
// # Common Patterns
//
// ## Create and populate a data element
//
//	elem := dataelem.NewDataElement(0x00100010, dataelem.PN, "")
//	elem.SetValue("Smith^John^Michael^Dr.^Jr.")
//	if err := elem.ValidateVR(); err != nil {
//	    log.Fatal(err)
//	}
//
// ## Handle multi-value elements
//
//	elem := dataelem.NewDataElement(0x0018102B, dataelem.DS, "")
//	elem.SetValue([]string{"100.0", "200.5", "300.25"})
//	elem.UpdateVM() // Updates VM based on value
//
// ## Create a sequence with items
//
//	seq := dataelem.NewDataElement(0x300A0040, dataelem.SQ, []*dataelem.SequenceItem{})
//	item := dataelem.NewSequenceItem()
//	item.AddDataElement(dataelem.NewDataElement(0x00100010, dataelem.PN, "Patient1"))
//	// Add item to sequence...
//
// ## Parse structured text (Person Name)
//
//	elem := dataelem.NewDataElement(0x00100010, dataelem.PN, "Smith^John")
//	pn := elem.ParsePersonName()
//	if pn != nil {
//	    fmt.Printf("Last: %s, First: %s\n", pn.Alphabetic.LastName, pn.Alphabetic.FirstName)
//	}
//
// ## Work with numeric values
//
//	elemDS := dataelem.NewDataElement(0x00181041, dataelem.DS, "123.45")
//	ds, _ := elemDS.ParseDecimalString()
//	fmt.Printf("Float value: %.2f\n", ds.Value)
//
//	elemIS := dataelem.NewDataElement(0x00200013, dataelem.IS, "42")
//	is, _ := elemIS.ParseIntegerString()
//	fmt.Printf("Int value: %d\n", is.Value)
//
// ## Clone with modifications
//
//	original := dataelem.NewDataElement(0x00100010, dataelem.PN, "Smith")
//	modified := original.Clone()
//	modified.SetValue("Smith^John")
//	// original.Value == "Smith" (unchanged)
//	// modified.Value == "Smith^John" (changed)
//
// # Performance Considerations
//
// 1. **Caching**: Use ValueCache for frequently accessed converted values
// 2. **Batch Operations**: Process multiple elements together for efficiency
// 3. **Streaming**: Use StreamingValue for large pixel/overlay data
// 4. **Buffering**: BufferedDataElement reduces memory for large values
// 5. **Immutable Access**: Use RawDataElement when no conversion needed
//
// # Integration with Other Modules
//
// - **dataset**: Composite structure containing multiple DataElements
// - **tag**: Tag definitions and utilities
// - **valuerep**: Value representation parsing and validation
// - **config**: Configuration settings for validation modes and behavior
// - **charset**: Character set encoding/decoding
// - **waveforms**: Waveform group structures
//
// # DICOM Standard References
//
// - Part 5: Data Structures and Encoding (DataElement format)
// - Part 6: Data Dictionary (VR definitions)
// - Part 18: Web API (JSON Model)
// - Part 20: Imaging and Waveforms (Waveform sequences)
package dataelem
