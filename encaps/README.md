# Encaps

DICOM encapsulated pixel data parsing, frame extraction, validation, and reframing for compressed medical images.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/encaps"

// Parse encapsulated data
parser := encaps.NewParser(bytes.NewReader(data), true) // true = little-endian
encData, err := parser.ParseEncapsulatedData()

// Extract frames
extractor := encaps.NewExtractor(encData)
frame, err := extractor.ExtractFrame(0)

// Get statistics
stats := encaps.GetStatistics(encData)
fmt.Printf("Frames: %d, Fragments: %d\n", stats.FrameCount, stats.FragmentCount)

// Validate
validator := encaps.NewValidator()
err = validator.ValidateEncapsulation(encData)

// Reframe
reframer := encaps.NewReframer(encData, 1)
reframed, err := reframer.ReframeData()
```

## API Reference

```go
func NewParser(reader io.Reader, littleEndian bool) *Parser
func (p *Parser) ParseEncapsulatedData() (*compress.EncapsulatedData, error)

func NewExtractor(encData *compress.EncapsulatedData) *Extractor
func (e *Extractor) ExtractFrame(frameIndex int) ([]byte, error)
func (e *Extractor) GetFrameCount() int

func NewValidator() *Validator
func (v *Validator) ValidateEncapsulation(encData *compress.EncapsulatedData) error

func NewReframer(encData *compress.EncapsulatedData, targetFrames int) *Reframer
func (r *Reframer) ReframeData() (*compress.EncapsulatedData, error)

func GetStatistics(encData *compress.EncapsulatedData) *Statistics

type Statistics struct {
    FrameCount, FragmentCount int
    TotalSize, AverageFrameSize uint64
    HasBasicOffsetTable, HasExtendedOffsetTable bool
}
```

## References

- [DICOM PS3.5 Annex A.4](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Transfer Syntaxes for Encapsulation of Encoded Pixel Data
