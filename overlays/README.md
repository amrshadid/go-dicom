# Overlays

DICOM overlay management for graphics annotations and regions of interest (ROI), with bit plane operations, ROI measurements (area/perimeter), and thread-safe concurrent access.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/overlays"

om := overlays.NewOverlayManager()

// Add overlay group
overlay := &overlays.OverlayGroup{
    GroupNumber: 1, OverlayRows: 256, OverlayColumns: 256,
    OverlayData: make([]byte, 8192), OverlayType: "G",
}
om.AddOverlay(overlay)

// Add ROI
om.AddROI(1, overlays.OverlayROI{
    Label: "Region 1", Color: "FF0000",
    Coordinates: []overlays.Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}},
})

// Measurements
area := overlays.GetROIArea(roi)
perimeter := overlays.GetROIPerimeter(roi)

// Bit planes
bitPlane, _ := om.ExtractBitPlane(1, 0)
om.ValidateOverlay(1)
```

## API Reference

```go
func NewOverlayManager() *OverlayManager

// Overlay management
func (om *OverlayManager) AddOverlay(overlay *OverlayGroup) error
func (om *OverlayManager) GetOverlay(groupNumber uint16) (*OverlayGroup, bool)
func (om *OverlayManager) GetAllOverlays() []*OverlayGroup
func (om *OverlayManager) RemoveOverlay(groupNumber uint16) bool
func (om *OverlayManager) ValidateOverlay(groupNumber uint16) error
func (om *OverlayManager) CopyOverlay(groupNumber uint16) (*OverlayGroup, error)

// ROI and graphics
func (om *OverlayManager) AddROI(groupNumber uint16, roi OverlayROI) error
func (om *OverlayManager) GetROIs(groupNumber uint16) []OverlayROI
func (om *OverlayManager) AddGraphics(groupNumber uint16, graphics OverlayGraphics) error
func (om *OverlayManager) GetGraphics(groupNumber uint16) []OverlayGraphics

// Bit planes and measurements
func (om *OverlayManager) ExtractBitPlane(groupNumber uint16, bitPosition int) ([][]bool, error)
func (om *OverlayManager) MergeBitPlanes(planes [][][]bool) ([]byte, error)
func (om *OverlayManager) GetOverlayBounds(groupNumber uint16) (minX, minY, maxX, maxY int, err error)
func GetROIArea(roi OverlayROI) float64
func GetROIPerimeter(roi OverlayROI) float64

type OverlayGroup struct {
    GroupNumber uint16; OverlayRows, OverlayColumns int
    OverlayData []byte; OverlayType, OverlayLabel string
    // ...
}
type OverlayROI struct { Label string; Coordinates []Point; Color string; ... }
type OverlayGraphics struct { Label, Type string; Points []Point; Color string; ... }
type Point struct { X, Y float64 }
```

## References

- [DICOM PS3.3](https://dicom.nema.org/medical/dicom/current/output/html/part03.html) - Overlay Module information object definitions
- DICOM PS3.5 Section 5.4.2.3 - Overlay Data Element structure
