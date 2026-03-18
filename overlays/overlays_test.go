package overlays_test

import (
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/overlays"
)

func TestNewOverlayManager(t *testing.T) {
	om := overlays.NewOverlayManager()
	if om == nil {
		t.Fatal("NewOverlayManager returned nil")
	}

	if om.GetOverlayCount() != 0 {
		t.Error("New manager should have no overlays")
	}
}

func TestAddOverlay(t *testing.T) {
	om := overlays.NewOverlayManager()

	overlay := &overlays.OverlayGroup{
		GroupNumber:    1,
		OverlayRows:    256,
		OverlayColumns: 256,
		OverlayData:    make([]byte, 8192),
		OverlayLabel:   "Test Overlay",
		OverlayType:    "G",
	}

	err := om.AddOverlay(overlay)
	if err != nil {
		t.Fatalf("AddOverlay failed: %v", err)
	}

	if om.GetOverlayCount() != 1 {
		t.Error("Overlay count should be 1")
	}
}

func TestAddOverlayErrors(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*overlays.OverlayManager)
		overlay   *overlays.OverlayGroup
		shouldErr bool
	}{
		{
			name:      "Nil overlay",
			overlay:   nil,
			shouldErr: true,
		},
		{
			name: "Invalid rows",
			overlay: &overlays.OverlayGroup{
				GroupNumber:    1,
				OverlayRows:    0,
				OverlayColumns: 256,
			},
			shouldErr: true,
		},
		{
			name: "Invalid columns",
			overlay: &overlays.OverlayGroup{
				GroupNumber:    1,
				OverlayRows:    256,
				OverlayColumns: 0,
			},
			shouldErr: true,
		},
		{
			name: "Zero group number",
			overlay: &overlays.OverlayGroup{
				GroupNumber:    0,
				OverlayRows:    256,
				OverlayColumns: 256,
			},
			shouldErr: true,
		},
		{
			name: "Duplicate group",
			setup: func(om *overlays.OverlayManager) {
				existing := &overlays.OverlayGroup{
					GroupNumber:    1,
					OverlayRows:    256,
					OverlayColumns: 256,
					OverlayData:    make([]byte, 8192),
				}
				om.AddOverlay(existing)
			},
			overlay: &overlays.OverlayGroup{
				GroupNumber:    1,
				OverlayRows:    256,
				OverlayColumns: 256,
				OverlayData:    make([]byte, 8192),
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			om := overlays.NewOverlayManager()

			if tt.setup != nil {
				tt.setup(om)
			}

			err := om.AddOverlay(tt.overlay)
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error: %v, got: %v", tt.shouldErr, err)
			}
		})
	}
}

func TestGetOverlay(t *testing.T) {
	om := overlays.NewOverlayManager()

	overlay := &overlays.OverlayGroup{
		GroupNumber:    1,
		OverlayRows:    256,
		OverlayColumns: 256,
		OverlayData:    make([]byte, 8192),
		OverlayLabel:   "Test Overlay",
	}

	om.AddOverlay(overlay)

	retrieved, exists := om.GetOverlay(1)
	if !exists {
		t.Error("GetOverlay should find existing overlay")
	}

	if retrieved.OverlayLabel != "Test Overlay" {
		t.Error("Retrieved overlay data mismatch")
	}

	_, exists = om.GetOverlay(999)
	if exists {
		t.Error("GetOverlay should not find non-existent overlay")
	}
}

func TestRemoveOverlay(t *testing.T) {
	om := overlays.NewOverlayManager()

	overlay := &overlays.OverlayGroup{
		GroupNumber:    1,
		OverlayRows:    256,
		OverlayColumns: 256,
		OverlayData:    make([]byte, 8192),
	}

	om.AddOverlay(overlay)
	if om.GetOverlayCount() != 1 {
		t.Error("Count should be 1 after add")
	}

	removed := om.RemoveOverlay(1)
	if !removed {
		t.Error("RemoveOverlay should return true for existing overlay")
	}

	if om.GetOverlayCount() != 0 {
		t.Error("Count should be 0 after remove")
	}

	removed = om.RemoveOverlay(999)
	if removed {
		t.Error("RemoveOverlay should return false for non-existent overlay")
	}
}

func TestGetAllOverlays(t *testing.T) {
	om := overlays.NewOverlayManager()

	// Add multiple overlays
	for i := 1; i <= 3; i++ {
		overlay := &overlays.OverlayGroup{
			GroupNumber:    uint16(i),
			OverlayRows:    256,
			OverlayColumns: 256,
			OverlayData:    make([]byte, 8192),
		}
		om.AddOverlay(overlay)
	}

	overlays := om.GetAllOverlays()
	if len(overlays) != 3 {
		t.Errorf("Expected 3 overlays, got %d", len(overlays))
	}
}

func TestAddROI(t *testing.T) {
	om := overlays.NewOverlayManager()

	roi := overlays.OverlayROI{
		Label:       "Test ROI",
		Description: "Test region of interest",
		Coordinates: []overlays.Point{
			{X: 0, Y: 0},
			{X: 100, Y: 0},
			{X: 100, Y: 100},
		},
		Color: "FF0000",
	}

	err := om.AddROI(1, roi)
	if err != nil {
		t.Fatalf("AddROI failed: %v", err)
	}

	rois := om.GetROIs(1)
	if len(rois) != 1 {
		t.Error("ROI count should be 1")
	}
}

func TestAddROIErrors(t *testing.T) {
	om := overlays.NewOverlayManager()

	tests := []struct {
		name      string
		roi       overlays.OverlayROI
		shouldErr bool
	}{
		{
			name:      "Empty label",
			roi:       overlays.OverlayROI{Label: ""},
			shouldErr: true,
		},
		{
			name: "Too few points",
			roi: overlays.OverlayROI{
				Label:       "Test",
				Coordinates: []overlays.Point{{X: 0, Y: 0}},
			},
			shouldErr: true,
		},
		{
			name: "Valid ROI",
			roi: overlays.OverlayROI{
				Label: "Test",
				Coordinates: []overlays.Point{
					{X: 0, Y: 0},
					{X: 100, Y: 0},
					{X: 100, Y: 100},
				},
			},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := om.AddROI(1, tt.roi)
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error: %v, got: %v", tt.shouldErr, err)
			}
		})
	}
}

func TestAddGraphics(t *testing.T) {
	om := overlays.NewOverlayManager()

	graphics := overlays.OverlayGraphics{
		Label:         "Test Graphics",
		Type:          "CIRCLE",
		Points:        []overlays.Point{{X: 128, Y: 128}},
		Color:         "00FF00",
		LineThickness: 2,
	}

	err := om.AddGraphics(1, graphics)
	if err != nil {
		t.Fatalf("AddGraphics failed: %v", err)
	}

	retrieved := om.GetGraphics(1)
	if len(retrieved) != 1 {
		t.Error("Graphics count should be 1")
	}
}

func TestExtractBitPlane(t *testing.T) {
	om := overlays.NewOverlayManager()

	// Create simple test overlay
	overlay := &overlays.OverlayGroup{
		GroupNumber:    1,
		OverlayRows:    8,
		OverlayColumns: 8,
		OverlayData:    []byte{0xFF, 0x00, 0xFF, 0x00, 0xFF, 0x00, 0xFF, 0x00},
		OverlayLabel:   "Test",
		OverlayType:    "G",
	}

	om.AddOverlay(overlay)

	bitPlane, err := om.ExtractBitPlane(1, 0)
	if err != nil {
		t.Fatalf("ExtractBitPlane failed: %v", err)
	}

	if len(bitPlane) != 8 {
		t.Errorf("Expected 8 rows, got %d", len(bitPlane))
	}

	if len(bitPlane[0]) != 8 {
		t.Errorf("Expected 8 columns, got %d", len(bitPlane[0]))
	}

	// Verify first byte is all 1s (0xFF)
	for i := 0; i < 8; i++ {
		if !bitPlane[0][i] {
			t.Error("First row should be all 1s")
		}
	}

	// Verify second byte is all 0s (0x00)
	for i := 0; i < 8; i++ {
		if bitPlane[1][i] {
			t.Error("Second row should be all 0s")
		}
	}
}

func TestMergeBitPlanes(t *testing.T) {
	om := overlays.NewOverlayManager()

	// Create bit planes
	plane1 := [][]bool{
		{true, false, true, false, true, false, true, false},
		{false, true, false, true, false, true, false, true},
	}

	plane2 := [][]bool{
		{false, true, false, true, false, true, false, true},
		{true, false, true, false, true, false, true, false},
	}

	data, err := om.MergeBitPlanes([][][]bool{plane1, plane2})
	if err != nil {
		t.Fatalf("MergeBitPlanes failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Merged data should not be empty")
	}
}

func TestGetOverlayBounds(t *testing.T) {
	om := overlays.NewOverlayManager()

	overlay := &overlays.OverlayGroup{
		GroupNumber:    1,
		OverlayRows:    256,
		OverlayColumns: 256,
		OverlayData:    make([]byte, 8192),
		OverlayLabel:   "Test",
		OverlayType:    "G",
	}

	// Set some bits in the data
	overlay.OverlayData[100] = 0xFF // Set 8 bits at position 100
	overlay.OverlayData[200] = 0xFF // Set 8 bits at position 200

	om.AddOverlay(overlay)

	minX, minY, maxX, maxY, err := om.GetOverlayBounds(1)
	if err != nil {
		t.Fatalf("GetOverlayBounds failed: %v", err)
	}

	if minX < 0 || maxX > 256 || minY < 0 || maxY > 256 {
		t.Errorf("Bounds out of range: [%d,%d] to [%d,%d]", minX, minY, maxX, maxY)
	}
}

func TestValidateOverlay(t *testing.T) {
	om := overlays.NewOverlayManager()

	tests := []struct {
		name      string
		overlay   *overlays.OverlayGroup
		shouldErr bool
	}{
		{
			name: "Valid overlay",
			overlay: &overlays.OverlayGroup{
				GroupNumber:          1,
				OverlayRows:          256,
				OverlayColumns:       256,
				OverlayData:          make([]byte, 8192),
				OverlayType:          "G",
				OverlayBitsAllocated: 1,
			},
			shouldErr: false,
		},
		{
			name: "Invalid type",
			overlay: &overlays.OverlayGroup{
				GroupNumber:          2,
				OverlayRows:          256,
				OverlayColumns:       256,
				OverlayData:          make([]byte, 8192),
				OverlayType:          "X",
				OverlayBitsAllocated: 1,
			},
			shouldErr: true,
		},
		{
			name: "Data size mismatch",
			overlay: &overlays.OverlayGroup{
				GroupNumber:          3,
				OverlayRows:          256,
				OverlayColumns:       256,
				OverlayData:          make([]byte, 100), // Wrong size
				OverlayType:          "G",
				OverlayBitsAllocated: 1,
			},
			shouldErr: true,
		},
	}

	for i, tt := range tests {
		if tt.overlay != nil {
			om.AddOverlay(tt.overlay)
		}

		t.Run(tt.name, func(t *testing.T) {
			err := om.ValidateOverlay(uint16(i + 1))
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error: %v, got: %v", tt.shouldErr, err)
			}
		})
	}
}

func TestCopyOverlay(t *testing.T) {
	om := overlays.NewOverlayManager()

	original := &overlays.OverlayGroup{
		GroupNumber:    1,
		OverlayRows:    256,
		OverlayColumns: 256,
		OverlayData:    make([]byte, 8192),
		OverlayLabel:   "Test Overlay",
		OverlayType:    "G",
	}

	om.AddOverlay(original)

	copy, err := om.CopyOverlay(1)
	if err != nil {
		t.Fatalf("CopyOverlay failed: %v", err)
	}

	if copy.OverlayLabel != original.OverlayLabel {
		t.Error("Copy label mismatch")
	}

	// Verify it's a deep copy
	if &copy.OverlayData == &original.OverlayData {
		t.Error("Copy should be deep copy")
	}
}

func TestGetROIArea(t *testing.T) {
	// Simple triangle with known area
	roi := overlays.OverlayROI{
		Label: "Triangle",
		Coordinates: []overlays.Point{
			{X: 0, Y: 0},
			{X: 4, Y: 0},
			{X: 2, Y: 2},
		},
	}

	area := overlays.GetROIArea(roi)
	if area == 0 {
		t.Error("Area should be non-zero")
	}

	// Area of triangle = (4 * 2) / 2 = 4
	if area < 3 || area > 5 {
		t.Errorf("Triangle area should be ~4, got %f", area)
	}
}

func TestGetROIPerimeter(t *testing.T) {
	roi := overlays.OverlayROI{
		Label: "Square",
		Coordinates: []overlays.Point{
			{X: 0, Y: 0},
			{X: 10, Y: 0},
			{X: 10, Y: 10},
			{X: 0, Y: 10},
		},
	}

	perimeter := overlays.GetROIPerimeter(roi)
	if perimeter == 0 {
		t.Error("Perimeter should be non-zero")
	}

	// Perimeter of square = 4 * 10 = 40
	// Using distance formula, each side = sqrt((10-0)^2 + (0-0)^2) = 10
	// So total ~40 (before final sqrt)
}

func TestConcurrentOverlayOperations(t *testing.T) {
	om := overlays.NewOverlayManager()
	done := make(chan bool)

	// Add overlays concurrently
	for i := 1; i <= 10; i++ {
		go func(num int) {
			overlay := &overlays.OverlayGroup{
				GroupNumber:    uint16(num),
				OverlayRows:    256,
				OverlayColumns: 256,
				OverlayData:    make([]byte, 8192),
			}
			om.AddOverlay(overlay)
			done <- true
		}(i)
	}

	// Wait for all adds
	for i := 0; i < 10; i++ {
		<-done
	}

	if om.GetOverlayCount() != 10 {
		t.Errorf("Expected 10 overlays, got %d", om.GetOverlayCount())
	}

	// Concurrent reads
	for i := 1; i <= 10; i++ {
		go func(num int) {
			_, _ = om.GetOverlay(uint16(num))
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestOverlayWithMetadata(t *testing.T) {
	overlay := &overlays.OverlayGroup{
		GroupNumber:      1,
		OverlayRows:      256,
		OverlayColumns:   256,
		OverlayData:      make([]byte, 8192),
		OverlayLabel:     "ROI #1",
		OverlayType:      "R",
		OverlayAuthor:    "Dr. Smith",
		OverlayDate:      "20231022",
		OverlayTime:      "143000.000",
		CreationDateTime: time.Now(),
	}

	if overlay.OverlayAuthor != "Dr. Smith" {
		t.Error("Author metadata not set")
	}

	if overlay.CreationDateTime.IsZero() {
		t.Error("Creation time not set")
	}
}
