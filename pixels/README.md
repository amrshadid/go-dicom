# Pixels

Uncompressed DICOM pixel data access with support for multiple sample depths (8/16/32-bit), signed/unsigned representations, multi-frame images, color spaces, and statistical analysis.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/pixels"

// Create pixel data
pd := pixels.NewPixelData(pixelBytes, 512, 512)
pd.BitsAllocated = 16
pd.BitsStored = 16
pd.PixelRepresentation = 0 // unsigned
pd.LittleEndian = true

// Access pixels
accessor := pixels.NewAccessor(pd)
value, _ := accessor.GetPixelValue(100, 200, 0)        // row, col, frame
interpreted, _ := accessor.GetInterpretedValue(100, 200, 0) // handles signed

// Statistics
calculator := pixels.NewCalculator(pd)
stats, _ := calculator.CalculateStatistics()
sampled, _ := calculator.CalculateStatisticsSampled(0.01) // 1% sample

// Validation
validator := pixels.NewValidator()
err := validator.ValidatePixelData(pd)

// Dimensions and range
width, height, frames := pd.GetDimensions()
min, max := pd.GetPixelRange()
```

## API Reference

```go
func NewPixelData(data []byte, columns, rows uint32) *PixelData
func NewAccessor(pd *PixelData) *Accessor
func NewCalculator(pd *PixelData) *Calculator
func NewValidator() *Validator

func (a *Accessor) GetPixelValue(row, col, frame int) (interface{}, error)
func (a *Accessor) GetInterpretedValue(row, col, frame int) (interface{}, error)
func (c *Calculator) CalculateStatistics() (*Statistics, error)
func (c *Calculator) CalculateStatisticsSampled(sampleRate float64) (*Statistics, error)
func (v *Validator) ValidatePixelData(pd *PixelData) error
func (pd *PixelData) GetDimensions() (uint32, uint32, uint32)
func (pd *PixelData) GetBytesPerPixel() int
func (pd *PixelData) GetFrameSize() int
func (pd *PixelData) GetPixelRange() (int64, int64)

type PixelData struct {
    Data []byte; Rows, Columns, NumberOfFrames uint32
    BitsAllocated, BitsStored, HighBit, PixelRepresentation uint16
    SamplesPerPixel uint16; PhotometricInterpretation string
    LittleEndian bool; PlanarConfiguration uint16
}

type Statistics struct { MinValue, MaxValue, MeanValue, MedianValue float64; TotalPixels, SampleCount uint64 }
```

## References

- [DICOM PS3.3](https://dicom.nema.org/medical/dicom/current/output/html/part03.html) - Pixel data attributes and photometric interpretation
- [DICOM PS3.5 Section 8](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Pixel data encoding
