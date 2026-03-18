# Compress

Compression, decompression, and encapsulation handling for DICOM pixel data. Supports JPEG, RLE, DEFLATE, JPEG-LS, and JPEG 2000 via built-in and external codecs.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/compress"

// Decompress pixel data
registry := compress.NewDecompressorRegistry()
decompressed, err := registry.Decompress(compress.DEFLATE, compressedData)

// Encapsulate compressed frames
frames := [][]byte{frame1, frame2, frame3}
encapsulated, err := compress.EncapsulateFrames(frames, 1, true)

// Extract frames from encapsulated data
framesChan, errorsChan := compress.GenerateFrames(buffer, numberOfFrames, "<")

// Get compression info
info := compress.GetCompressionInfo(compress.JPEG_LS)
```

## API Reference

```go
type CompressionType string // UNCOMPRESSED, DEFLATE, RLE, JPEG, JPEG_LOSSLESS, JPEG_LS, JPEG_2000

type Decompressor interface {
    Decompress(data []byte) ([]byte, error)
    CanDecompress(data []byte) bool
}

type Compressor interface {
    Compress(data []byte) ([]byte, error)
}

func NewDecompressorRegistry() *DecompressorRegistry
func ParseBasicOffsets(buffer, endianness) ([]uint32, error)
func ParseFragments(buffer, endianness) (int, []int64, error)
func GenerateFrames(buffer, numberOfFrames, endianness) (chan []byte, chan error)
func GetFrame(buffer, frameIndex, numberOfFrames, endianness) ([]byte, error)
func EncapsulateFrames(frames [][]byte, fragmentsPerFrame int, withOffsets bool) ([]byte, error)
func FragmentFrame(frameData []byte, numFragments int) ([][]byte, error)
func NewEncapsulatedBuffer(readers []io.ReadSeeker, includeOffsets bool) (*EncapsulatedBuffer, error)
func GetCompressionInfo(ct CompressionType) CompressionInfo
```

## References

- [DICOM PS3.5 Annex A.4](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Transfer Syntaxes for Encapsulation
- DICOM PS3.5 Annex G-J - RLE, JPEG, JPEG-LS, JPEG 2000 Transfer Syntaxes
