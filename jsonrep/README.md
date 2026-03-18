# JSONRep

DICOM JSON Model (Part 18) representation with dataset-to-JSON conversion, bulk data handling with file/memory/external storage backends, and streaming support.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/jsonrep"

jr := jsonrep.NewJSONRepresentation()

// Convert dataset to/from JSON
jsonData, _ := jr.ToJSON(dataset)
dataset, _ := jr.FromJSON(jsonData)

// Validate and format
err := jr.ValidateJSON(jsonData)
pretty, _ := jr.PrettyPrintJSON(jsonData)

// Bulk data handling
bdh, _ := jsonrep.NewBulkDataHandler("/storage", jsonrep.StorageFile)
ref, _ := bdh.CreateReference(pixelData, "application/octet-stream")
data, _ := bdh.ResolveReference(ref)
bdh.ValidateReference(ref)

// Encode with bulk data support
encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 1024*1024) // 1MB threshold
encoded, _ := encoder.EncodeDatasetWithBulk(dataset, elements)
```

## API Reference

```go
func NewJSONRepresentation() *JSONRepresentation
func (jr *JSONRepresentation) ToJSON(dataset *DicomDataset) ([]byte, error)
func (jr *JSONRepresentation) FromJSON(data []byte) (*DicomDataset, error)
func (jr *JSONRepresentation) ToJSONMessage(dataset *DicomDataset, metadata map[string]interface{}) (*JSONMessage, error)
func (jr *JSONRepresentation) ValidateJSON(data []byte) error
func (jr *JSONRepresentation) PrettyPrintJSON(data []byte) (string, error)
func (jr *JSONRepresentation) CompactJSON(data []byte) ([]byte, error)
func (jr *JSONRepresentation) ExtractUIDs(dataset *DicomDataset) map[string]string
func (jr *JSONRepresentation) MergeDatasets(dest, src *DicomDataset) (*DicomDataset, error)
func (jr *JSONRepresentation) FilterDataset(dataset *DicomDataset, fields []string) (*DicomDataset, error)

// Bulk data
type BulkDataStorage int // StorageFile, StorageMemory, StorageExternal
func NewBulkDataHandler(baseURI string, storage BulkDataStorage) (*BulkDataHandler, error)
func (bdh *BulkDataHandler) CreateReference(data []byte, contentType string) (*BulkDataReference, error)
func (bdh *BulkDataHandler) ResolveReference(ref *BulkDataReference) ([]byte, error)
func (bdh *BulkDataHandler) ValidateReference(ref *BulkDataReference) error
func (bdh *BulkDataHandler) ListReferences() []*BulkDataReference
func (bdh *BulkDataHandler) DeleteReference(ref *BulkDataReference) error
func (bdh *BulkDataHandler) GetStats() BulkDataStats

// Streaming
func NewStreamBulkDataReader(handler *BulkDataHandler, ref *BulkDataReference) (*StreamBulkDataReader, error)
func StreamBulkData(src, dst *BulkDataHandler, ref *BulkDataReference) error
```

## References

- [DICOM PS3.18](https://dicom.nema.org/medical/dicom/current/output/html/part18.html) - Web Services, JSON metadata format, bulk data handling
