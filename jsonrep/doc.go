// Package jsonrep provides DICOM JSON Model representation support.
//
// This package implements DICOM Part 18 Web Services JSON format for representing
// DICOM datasets in JSON notation, with support for bulk data handling, streaming,
// and various storage backends.
//
// # Core Concepts
//
// ## JSONRepresentation
//
// Main handler for DICOM dataset JSON conversion, supporting marshaling and
// unmarshaling between DICOM datasets and JSON formats.
//
// ## DicomDataset
//
// Represents a DICOM dataset structure with common clinical attributes including
// patient information, study/series/SOP UIDs, dates, times, and custom attributes.
//
// ## JSONElement
//
// Represents a single DICOM element in JSON format with Value Representation (VR)
// and value information.
//
// ## JSONMessage
//
// Represents a complete DICOM message in JSON format with version, timestamp,
// elements, and optional metadata.
//
// ## BulkDataReference
//
// References large binary data stored separately with URI, content type, hash,
// and integrity verification capabilities.
//
// ## BulkDataHandler
//
// Manages bulk data storage and retrieval with configurable backends
// (file, memory, external) and integrity verification.
//
// ## Bulk Data Storage Backends
//
//   - StorageFile: Store bulk data in filesystem
//   - StorageMemory: Store bulk data in memory
//   - StorageExternal: Track references only (external storage)
//
// # Basic Usage
//
// ## Converting Dataset to JSON
//
//	jr := jsonrep.NewJSONRepresentation()
//
//	dataset := &jsonrep.DicomDataset{
//	    PatientName: "Doe^John",
//	    PatientID:   "12345",
//	    Modality:    "CT",
//	}
//
//	jsonData, err := jr.ToJSON(dataset)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Converting JSON to Dataset
//
//	jr := jsonrep.NewJSONRepresentation()
//	dataset, err := jr.FromJSON(jsonData)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Working with Bulk Data
//
//	// Create bulk data handler with file storage
//	bdh, err := jsonrep.NewBulkDataHandler("/path/to/storage", jsonrep.StorageFile)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create a bulk data reference
//	ref, err := bdh.CreateReference(largeData, "application/octet-stream")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Resolve bulk data reference
//	data, err := bdh.ResolveReference(ref)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Advanced Features
//
// ## Bulk Data Encoding
//
// Encode datasets with automatic bulk data reference creation for large values:
//
//	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 1024*1024) // 1MB threshold
//	encoded, err := encoder.EncodeDatasetWithBulk(dataset, elements)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Bulk Data Decoding
//
// Decode datasets with automatic bulk data resolution:
//
//	decoded, err := encoder.DecodeDatasetWithBulk(encoded)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Bulk Data Validation
//
// Validate bulk data references by verifying hashes:
//
//	err := bdh.ValidateReference(ref)
//	if err != nil {
//	    log.Printf("Validation failed: %v", err)
//	}
//
// ## Streaming Large Data
//
// Stream bulk data without loading entire file into memory:
//
//	reader, err := jsonrep.NewStreamBulkDataReader(bdh, ref)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer reader.Close()
//
//	// Copy to destination
//	n, err := reader.Copy(destWriter)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Transmission Optimization
//
// Create transmission-optimized JSON with inline data for small elements
// and bulk references for large ones:
//
//	compressed, err := encoder.CompressForTransmission(dataset, elements)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # DicomDataset Fields
//
// ## Patient Information
//
//   - PatientName: Patient name (PN)
//   - PatientID: Patient identifier (LO)
//   - ReferringPhysician: Referring physician (PN)
//
// ## Study Information
//
//   - StudyInstanceUID: Study unique identifier (UI)
//   - StudyDate: Study date (DA)
//   - StudyTime: Study time (TM)
//
// ## Series Information
//
//   - SeriesInstanceUID: Series unique identifier (UI)
//   - SeriesDate: Series date (DA)
//   - SeriesTime: Series time (TM)
//   - SeriesNumber: Series number (IS)
//   - Modality: Modality type (CS)
//
// ## SOP Information
//
//   - SOPClassUID: SOP class unique identifier (UI)
//   - SOPInstanceUID: SOP instance unique identifier (UI)
//   - InstanceNumber: Instance number (IS)
//
// ## Content Information
//
//   - ContentDate: Content date (DA)
//   - ContentTime: Content time (TM)
//
// ## Equipment Information
//
//   - Manufacturer: Device manufacturer (LO)
//   - ManufacturerModel: Device model (LO)
//   - InstitutionName: Institution name (LO)
//
// ## Media Information
//
//   - NumberOfFrames: Frame count (IS)
//   - ReferencedSOPSequence: Referenced SOP instances
//   - CustomAttributes: Application-specific attributes
//
// # Thread Safety
//
// All JSONRepresentation and BulkDataHandler operations are thread-safe
// through internal sync.RWMutex:
//   - Multiple goroutines can read concurrently
//   - Write operations are mutually exclusive
//   - Safe for concurrent use without external synchronization
//
// Example of concurrent operations:
//
//	jr := jsonrep.NewJSONRepresentation()
//
//	// Multiple readers
//	go func() {
//	    for i := 0; i < 100; i++ {
//	        _, _ = jr.ToJSON(dataset)
//	    }
//	}()
//
//	// Safe concurrent access
//
// # Performance Characteristics
//
//   - **ToJSON**: O(n) where n is dataset size
//   - **FromJSON**: O(n) where n is JSON size
//   - **ToJSONMessage**: O(n) where n is dataset fields
//   - **ValidateJSON**: O(n) where n is JSON size
//   - **PrettyPrintJSON**: O(n) where n is JSON size
//   - **CompactJSON**: O(n) where n is JSON size
//   - **ExtractUIDs**: O(1) for fixed dataset fields
//   - **ExtractPatientInfo**: O(1) for fixed dataset fields
//   - **MergeDatasets**: O(1) for fixed dataset fields
//   - **FilterDataset**: O(m) where m is field count
//   - **CreateReference**: O(n + h) where n is data size, h is hash calculation
//   - **ResolveReference**: O(n) where n is data size
//   - **ValidateReference**: O(n + h) for data read and hash verification
//   - **ListReferences**: O(r) where r is reference count
//   - **DeleteReference**: O(n) for file I/O or memory cleanup
//
// # Error Handling
//
// Operations return errors for:
//   - Nil datasets (ToJSON, ToJSONMessage, FilterDataset, MergeDatasets)
//   - Empty JSON data (FromJSON, ValidateJSON, PrettyPrintJSON)
//   - Invalid JSON format (FromJSON, ValidateJSON, PrettyPrintJSON, CompactJSON)
//   - Missing required UIDs (ValidateJSON)
//   - Empty bulk data (CreateReference)
//   - Nil bulk data references (ResolveReference, ValidateReference)
//   - Missing bulk data files (ResolveReference for file storage)
//   - Hash mismatches (ValidateReference)
//   - Invalid storage backends (NewBulkDataHandler)
//   - Missing fields in FilterDataset
//
// Example:
//
//	// Validate JSON before processing
//	err := jr.ValidateJSON(jsonData)
//	if err != nil {
//	    log.Printf("Validation error: %v", err)
//	}
//
// # Use Cases
//
// ## DICOM Web Service Communication
//
//	jr := jsonrep.NewJSONRepresentation()
//	jsonPayload, err := jr.ToJSON(dataset)
//	// Send jsonPayload to DICOM web service
//
// ## DICOM JSON Storage
//
//	// Convert DICOM to JSON for database storage
//	jsonData, err := jr.ToJSON(dataset)
//	// Store jsonData in NoSQL database
//
// ## Bulk Data Management
//
//	bdh, err := jsonrep.NewBulkDataHandler(baseURI, jsonrep.StorageFile)
//	ref, err := bdh.CreateReference(pixelData, "application/octet-stream")
//	// Reference stored separately from JSON metadata
//
// ## Data Transmission Optimization
//
//	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 1MB)
//	compressed, err := encoder.CompressForTransmission(dataset, elements)
//	// Small JSON payload with external bulk data references
//
// ## Multi-Format Support
//
//	jr := jsonrep.NewJSONRepresentation()
//	// Convert DICOM dataset to JSON
//	jsonData, _ := jr.ToJSON(dataset)
//	// Pretty print for human readability
//	pretty, _ := jr.PrettyPrintJSON(jsonData)
//	// Compact for transmission
//	compact, _ := jr.CompactJSON(jsonData)
//
// # DICOM JSON Model Compliance
//
// Implements DICOM Part 18 Web Services:
//   - JSON metadata format for DICOM information objects
//   - Bulk data reference handling (Part 18, Annex E)
//   - Value Representation (VR) preservation
//   - Multi-frame and sequential instance support
//   - Standard UID and attribute handling
//
// See: https://www.dicomstandard.org/ (Part 18 - Web Services)
//
// # See Also
//
//   - dataset package: DICOM dataset structure and handling
//   - tag package: DICOM tag definitions and utilities
//   - uid package: UID management and validation
//   - values package: Value encoding and representation
//   - pixeldata package: Pixel data processing
package jsonrep
