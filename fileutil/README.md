# FileUtil

Supporting utilities for DICOM file processing: byte order detection, data conversion, value padding, bounded reading, metadata caching, tag indexing, and codec integration.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/fileutil"

// Byte order detection
bo, _ := fileutil.DetectFromTransferSyntax(uid.New("1.2.840.10008.1.2.1"))
isValid, _ := fileutil.DetectFromPreamble(reader)

// Data conversion
bytes := fileutil.Uint16ToBytes(0x1234, filebase.LittleEndian)
value := fileutil.BytesToUint16(bytes, filebase.LittleEndian)

// Value padding
padded := fileutil.PadValueSpace([]byte("A"))   // pads to even length with 0x20
unpadded := fileutil.UnpadValue(padded, 0x20)

// Bounded reading
bounded := fileutil.NewReaderWithBoundary(reader, 1024)
remaining := bounded.GetBytesRemaining()

// Codec integration
ci := fileutil.NewCodecIntegration()
decompressed, _ := ci.Decompress(data, compress.DEFLATE)
```

## API Reference

```go
// Byte order
func DetectFromTransferSyntax(ts uid.UID) (filebase.ByteOrder, error)
func DetectFromPreamble(reader filebase.Reader) (bool, error)

// Conversion
func Uint16ToBytes(value uint16, byteOrder filebase.ByteOrder) []byte
func Uint32ToBytes(value uint32, byteOrder filebase.ByteOrder) []byte
func BytesToUint16(data []byte, byteOrder filebase.ByteOrder) uint16
func BytesToUint32(data []byte, byteOrder filebase.ByteOrder) uint32

// Padding
func PadValue(value []byte, padByte byte) []byte
func PadValueSpace(value []byte) []byte
func PadValueNull(value []byte) []byte
func UnpadValue(value []byte, padByte byte) []byte
func AlignToEvenBoundary(position int64) int64

// Caching
func NewFileMetaCache(maxSize int) *FileMetaCache
func NewTagIndex() *TagIndex
func NewByteBufferPool(poolSize, bufferSize int) *ByteBufferPool

// Bounded reading
func NewReaderWithBoundary(reader filebase.Reader, boundary int64) *ReaderWithBoundary

// Codec integration
func NewCodecIntegration() *CodecIntegration
func (ci *CodecIntegration) Decompress(data []byte, ct compress.CompressionType) ([]byte, error)
func (ci *CodecIntegration) IsCompressionSupported(ct compress.CompressionType) bool

// Deferred pixel data
func NewDeferredPixelDataReader(filePath string, ct compress.CompressionType, offset, length int64, ci *CodecIntegration) *DeferredPixelDataReader
```

## References

- [DICOM PS3.5 Section 5.2](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Byte Ordering
- [DICOM PS3.10 Section 7](https://dicom.nema.org/medical/dicom/current/output/html/part10.html) - File Format Detection
