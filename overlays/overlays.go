package overlays

import (
	"fmt"
	"sync"
	"time"
)

// OverlayGroup represents a DICOM overlay group (60xx,3000 onwards)
type OverlayGroup struct {
	GroupNumber          uint16    // Overlay group number (0001-FFFF)
	OverlayRows          int       // Number of rows in overlay
	OverlayColumns       int       // Number of columns in overlay
	OverlayData          []byte    // Binary overlay data
	OverlayLabel         string    // Label for the overlay
	OverlayType          string    // "G" (Graphics) or "R" (ROI)
	OverlaySubtype       string    // Subtype (e.g., CROSSHAIR, CIRCLE, POLYLINE)
	OverlayBitsAllocated int       // Bits allocated (usually 1)
	OverlayBitPosition   int       // Bit position in overlay data
	OverlayDescription   string    // Text description
	OverlayAuthor        string    // Author/creator of overlay
	OverlayDate          string    // Creation date (YYYYMMDD)
	OverlayTime          string    // Creation time (HHMMSS.frac)
	CreationDateTime     time.Time // Parsed creation time
}

// OverlayROI represents a Region of Interest overlay
type OverlayROI struct {
	Label           string
	Description     string
	ReferencedFrame int
	Coordinates     []Point // Points defining the ROI
	Color           string  // Display color (e.g., "FF0000" for red)
	LineThickness   int
}

// Point represents a 2D coordinate
type Point struct {
	X float64
	Y float64
}

// OverlayGraphics represents a graphics overlay
type OverlayGraphics struct {
	Label         string
	Type          string // CROSSHAIR, CIRCLE, POLYLINE, POLYGON, etc.
	Points        []Point
	Color         string
	LineStyle     string // SOLID, DASH, etc.
	LineThickness int
	FillPattern   string // For filled shapes
	FillColor     string
}

// OverlayManager manages multiple overlay groups
type OverlayManager struct {
	overlays map[uint16]*OverlayGroup
	rois     map[uint16][]OverlayROI
	graphics map[uint16][]OverlayGraphics
	mu       sync.RWMutex
}

// NewOverlayManager creates a new overlay manager
func NewOverlayManager() *OverlayManager {
	return &OverlayManager{
		overlays: make(map[uint16]*OverlayGroup),
		rois:     make(map[uint16][]OverlayROI),
		graphics: make(map[uint16][]OverlayGraphics),
	}
}

// AddOverlay adds an overlay group to the manager
func (om *OverlayManager) AddOverlay(overlay *OverlayGroup) error {
	if overlay == nil {
		return fmt.Errorf("overlay cannot be nil")
	}

	if overlay.OverlayRows <= 0 || overlay.OverlayColumns <= 0 {
		return fmt.Errorf("overlay dimensions must be positive")
	}

	if overlay.GroupNumber == 0 {
		return fmt.Errorf("overlay group number must be non-zero")
	}

	om.mu.Lock()
	defer om.mu.Unlock()

	if _, exists := om.overlays[overlay.GroupNumber]; exists {
		return fmt.Errorf("overlay group %d already exists", overlay.GroupNumber)
	}

	om.overlays[overlay.GroupNumber] = overlay
	return nil
}

// GetOverlay retrieves an overlay group
func (om *OverlayManager) GetOverlay(groupNumber uint16) (*OverlayGroup, bool) {
	om.mu.RLock()
	defer om.mu.RUnlock()

	overlay, exists := om.overlays[groupNumber]
	return overlay, exists
}

// GetAllOverlays returns all overlay groups
func (om *OverlayManager) GetAllOverlays() []*OverlayGroup {
	om.mu.RLock()
	defer om.mu.RUnlock()

	overlays := make([]*OverlayGroup, 0, len(om.overlays))
	for _, overlay := range om.overlays {
		overlays = append(overlays, overlay)
	}
	return overlays
}

// RemoveOverlay removes an overlay group
func (om *OverlayManager) RemoveOverlay(groupNumber uint16) bool {
	om.mu.Lock()
	defer om.mu.Unlock()

	if _, exists := om.overlays[groupNumber]; exists {
		delete(om.overlays, groupNumber)
		delete(om.rois, groupNumber)
		delete(om.graphics, groupNumber)
		return true
	}
	return false
}

// GetOverlayCount returns the number of overlay groups
func (om *OverlayManager) GetOverlayCount() int {
	om.mu.RLock()
	defer om.mu.RUnlock()

	return len(om.overlays)
}

// AddROI adds an ROI overlay to a specific group
func (om *OverlayManager) AddROI(groupNumber uint16, roi OverlayROI) error {
	if roi.Label == "" {
		return fmt.Errorf("ROI label cannot be empty")
	}

	if len(roi.Coordinates) < 3 {
		return fmt.Errorf("ROI must have at least 3 points")
	}

	om.mu.Lock()
	defer om.mu.Unlock()

	om.rois[groupNumber] = append(om.rois[groupNumber], roi)
	return nil
}

// GetROIs retrieves all ROIs for a specific group
func (om *OverlayManager) GetROIs(groupNumber uint16) []OverlayROI {
	om.mu.RLock()
	defer om.mu.RUnlock()

	rois := om.rois[groupNumber]
	// Return copy
	result := make([]OverlayROI, len(rois))
	copy(result, rois)
	return result
}

// AddGraphics adds a graphics overlay to a specific group
func (om *OverlayManager) AddGraphics(groupNumber uint16, graphics OverlayGraphics) error {
	if graphics.Label == "" {
		return fmt.Errorf("graphics label cannot be empty")
	}

	if len(graphics.Points) == 0 {
		return fmt.Errorf("graphics must have at least one point")
	}

	om.mu.Lock()
	defer om.mu.Unlock()

	om.graphics[groupNumber] = append(om.graphics[groupNumber], graphics)
	return nil
}

// GetGraphics retrieves all graphics for a specific group
func (om *OverlayManager) GetGraphics(groupNumber uint16) []OverlayGraphics {
	om.mu.RLock()
	defer om.mu.RUnlock()

	graphics := om.graphics[groupNumber]
	// Return copy
	result := make([]OverlayGraphics, len(graphics))
	copy(result, graphics)
	return result
}

// ExtractBitPlane extracts a single bit plane from overlay data
// Returns a 2D slice of booleans
func (om *OverlayManager) ExtractBitPlane(groupNumber uint16, bitPosition int) ([][]bool, error) {
	om.mu.RLock()
	overlay, exists := om.overlays[groupNumber]
	om.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("overlay group %d not found", groupNumber)
	}

	if bitPosition < 0 || bitPosition >= 8 {
		return nil, fmt.Errorf("bit position must be 0-7")
	}

	result := make([][]bool, overlay.OverlayRows)
	for i := 0; i < overlay.OverlayRows; i++ {
		result[i] = make([]bool, overlay.OverlayColumns)

		for j := 0; j < overlay.OverlayColumns; j++ {
			// Calculate byte index and bit within byte
			pixelIndex := i*overlay.OverlayColumns + j
			byteIndex := pixelIndex / 8
			bitInByte := uint8(pixelIndex % 8)

			if byteIndex < len(overlay.OverlayData) {
				byte := overlay.OverlayData[byteIndex]
				result[i][j] = (byte & (1 << bitInByte)) != 0
			}
		}
	}

	return result, nil
}

// MergeBitPlanes merges multiple bit planes into overlay data
func (om *OverlayManager) MergeBitPlanes(planes [][][]bool) ([]byte, error) {
	if len(planes) == 0 {
		return nil, fmt.Errorf("at least one bit plane required")
	}

	rows := len(planes[0])
	cols := len(planes[0][0])

	// Validate all planes have same dimensions
	for _, plane := range planes {
		if len(plane) != rows {
			return nil, fmt.Errorf("all planes must have same dimensions")
		}
		for _, row := range plane {
			if len(row) != cols {
				return nil, fmt.Errorf("all rows must have same width")
			}
		}
	}

	// Calculate required bytes
	totalPixels := rows * cols
	totalBytes := (totalPixels + 7) / 8

	result := make([]byte, totalBytes)

	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			pixelIndex := i*cols + j
			byteIndex := pixelIndex / 8
			bitInByte := uint8(pixelIndex % 8)

			// Check if any plane has a 1 at this position
			for _, plane := range planes {
				if i < len(plane) && j < len(plane[i]) {
					if plane[i][j] {
						result[byteIndex] |= (1 << bitInByte)
						break
					}
				}
			}
		}
	}

	return result, nil
}

// GetOverlayBounds returns the bounding box of an overlay
func (om *OverlayManager) GetOverlayBounds(groupNumber uint16) (minX, minY, maxX, maxY int, err error) {
	om.mu.RLock()
	overlay, exists := om.overlays[groupNumber]
	om.mu.RUnlock()

	if !exists {
		return 0, 0, 0, 0, fmt.Errorf("overlay group %d not found", groupNumber)
	}

	minX = overlay.OverlayColumns
	minY = overlay.OverlayRows
	maxX = 0
	maxY = 0

	// Extract bit plane to find actual data bounds
	bitPlane, err := om.ExtractBitPlane(groupNumber, 0)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	found := false
	for i := 0; i < len(bitPlane); i++ {
		for j := 0; j < len(bitPlane[i]); j++ {
			if bitPlane[i][j] {
				found = true
				if j < minX {
					minX = j
				}
				if i < minY {
					minY = i
				}
				if j > maxX {
					maxX = j
				}
				if i > maxY {
					maxY = i
				}
			}
		}
	}

	if !found {
		return 0, 0, 0, 0, fmt.Errorf("overlay contains no data")
	}

	return minX, minY, maxX, maxY, nil
}

// ValidateOverlay validates overlay structure and data
func (om *OverlayManager) ValidateOverlay(groupNumber uint16) error {
	om.mu.RLock()
	defer om.mu.RUnlock()

	overlay, exists := om.overlays[groupNumber]
	if !exists {
		return fmt.Errorf("overlay group %d not found", groupNumber)
	}

	// Validate dimensions
	if overlay.OverlayRows <= 0 || overlay.OverlayColumns <= 0 {
		return fmt.Errorf("invalid overlay dimensions: %dx%d", overlay.OverlayRows, overlay.OverlayColumns)
	}

	// Validate data size
	expectedBytes := (overlay.OverlayRows*overlay.OverlayColumns + 7) / 8
	if len(overlay.OverlayData) != expectedBytes {
		return fmt.Errorf("overlay data size mismatch: expected %d bytes, got %d", expectedBytes, len(overlay.OverlayData))
	}

	// Validate overlay type
	if overlay.OverlayType != "G" && overlay.OverlayType != "R" {
		return fmt.Errorf("invalid overlay type: %s (must be G or R)", overlay.OverlayType)
	}

	// Validate bits allocated
	if overlay.OverlayBitsAllocated != 1 && overlay.OverlayBitsAllocated != 8 && overlay.OverlayBitsAllocated != 16 {
		return fmt.Errorf("invalid bits allocated: %d", overlay.OverlayBitsAllocated)
	}

	return nil
}

// CopyOverlay creates a deep copy of an overlay
func (om *OverlayManager) CopyOverlay(groupNumber uint16) (*OverlayGroup, error) {
	om.mu.RLock()
	defer om.mu.RUnlock()

	overlay, exists := om.overlays[groupNumber]
	if !exists {
		return nil, fmt.Errorf("overlay group %d not found", groupNumber)
	}

	// Create deep copy
	dataCopy := make([]byte, len(overlay.OverlayData))
	copy(dataCopy, overlay.OverlayData)

	return &OverlayGroup{
		GroupNumber:          overlay.GroupNumber,
		OverlayRows:          overlay.OverlayRows,
		OverlayColumns:       overlay.OverlayColumns,
		OverlayData:          dataCopy,
		OverlayLabel:         overlay.OverlayLabel,
		OverlayType:          overlay.OverlayType,
		OverlaySubtype:       overlay.OverlaySubtype,
		OverlayBitsAllocated: overlay.OverlayBitsAllocated,
		OverlayBitPosition:   overlay.OverlayBitPosition,
		OverlayDescription:   overlay.OverlayDescription,
		OverlayAuthor:        overlay.OverlayAuthor,
		OverlayDate:          overlay.OverlayDate,
		OverlayTime:          overlay.OverlayTime,
		CreationDateTime:     overlay.CreationDateTime,
	}, nil
}

// GetROIArea calculates the area of an ROI (number of enclosed points)
func GetROIArea(roi OverlayROI) float64 {
	if len(roi.Coordinates) < 3 {
		return 0
	}

	// Use shoelace formula
	area := 0.0
	n := len(roi.Coordinates)

	for i := 0; i < n; i++ {
		p1 := roi.Coordinates[i]
		p2 := roi.Coordinates[(i+1)%n]
		area += p1.X*p2.Y - p2.X*p1.Y
	}

	return area / 2
}

// GetROIPerimeter calculates the perimeter of an ROI
func GetROIPerimeter(roi OverlayROI) float64 {
	if len(roi.Coordinates) < 3 {
		return 0
	}

	perimeter := 0.0
	n := len(roi.Coordinates)

	for i := 0; i < n; i++ {
		p1 := roi.Coordinates[i]
		p2 := roi.Coordinates[(i+1)%n]
		dx := p2.X - p1.X
		dy := p2.Y - p1.Y
		perimeter += (dx*dx + dy*dy) // Will sqrt after loop for efficiency
	}

	return perimeter
}
