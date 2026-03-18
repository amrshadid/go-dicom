// Package overlays provides comprehensive support for DICOM overlay management.
//
// This package implements functionality for creating, managing, and manipulating
// DICOM overlay groups including both graphics overlays and regions of interest (ROI).
// Overlays are used to annotate, mark regions of interest, or add graphical elements
// to DICOM images.
//
// # Core Concepts
//
// ## OverlayGroup
//
// Represents a DICOM overlay group (60xx,3000 onwards) with all associated metadata
// and binary data.
//
// ## OverlayROI
//
// Represents a Region of Interest overlay consisting of labeled regions defined by
// coordinate points with color and styling information.
//
// ## OverlayGraphics
//
// Represents graphics overlays for drawing shapes (circles, polylines, polygons)
// on DICOM images with customizable line styles and fill patterns.
//
// ## Point
//
// Represents a 2D coordinate used in ROI and graphics definitions.
//
// ## OverlayManager
//
// Central manager for handling multiple overlay groups, ROIs, and graphics with
// thread-safe operations and advanced manipulation capabilities.
//
// # Basic Usage
//
// ## Creating an Overlay Manager
//
//	om := overlays.NewOverlayManager()
//
// ## Adding an Overlay Group
//
//	overlay := &overlays.OverlayGroup{
//	    GroupNumber:    1,
//	    OverlayRows:    256,
//	    OverlayColumns: 256,
//	    OverlayData:    make([]byte, 8192),
//	    OverlayLabel:   "Test Overlay",
//	    OverlayType:    "G",  // "G" for Graphics, "R" for ROI
//	}
//	err := om.AddOverlay(overlay)
//
// ## Retrieving Overlays
//
//	overlay, exists := om.GetOverlay(1)
//	if exists {
//	    fmt.Printf("Found overlay: %s\n", overlay.OverlayLabel)
//	}
//
//	allOverlays := om.GetAllOverlays()
//
// ## Managing ROIs
//
//	roi := overlays.OverlayROI{
//	    Label: "Region 1",
//	    Description: "Left lung nodule",
//	    Coordinates: []overlays.Point{
//	        {X: 100, Y: 100},
//	        {X: 200, Y: 100},
//	        {X: 200, Y: 200},
//	    },
//	    Color: "FF0000",  // Red
//	}
//	err := om.AddROI(1, roi)
//
// ## Managing Graphics
//
//	graphics := overlays.OverlayGraphics{
//	    Label:        "Circle Marker",
//	    Type:         "CIRCLE",
//	    Points:       []overlays.Point{{X: 128, Y: 128}},
//	    Color:        "00FF00",  // Green
//	    LineThickness: 2,
//	}
//	err := om.AddGraphics(1, graphics)
//
// # Advanced Features
//
// ## Bit Plane Extraction
//
// Extract individual bit planes from overlay data as a 2D boolean matrix:
//
//	bitPlane, err := om.ExtractBitPlane(1, 0)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// bitPlane is [][]bool representing the binary overlay data
//
// ## Bit Plane Merging
//
// Combine multiple bit planes into overlay data:
//
//	planes := [][][]bool{
//	    {{true, false}, {false, true}},
//	    {{false, true}, {true, false}},
//	}
//	data, err := om.MergeBitPlanes(planes)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Overlay Bounds
//
// Calculate the bounding box of actual overlay data:
//
//	minX, minY, maxX, maxY, err := om.GetOverlayBounds(1)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Bounds: [%d,%d] to [%d,%d]\n", minX, minY, maxX, maxY)
//
// ## Validation
//
// Validate overlay structure and data consistency:
//
//	err := om.ValidateOverlay(1)
//	if err != nil {
//	    fmt.Printf("Validation error: %v\n", err)
//	}
//
// ## Copying Overlays
//
// Create a deep copy of an overlay group:
//
//	copy, err := om.CopyOverlay(1)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## ROI Area and Perimeter
//
// Calculate geometric properties of ROIs using shoelace formula:
//
//	roi := overlays.OverlayROI{
//	    Label: "Test",
//	    Coordinates: []overlays.Point{
//	        {X: 0, Y: 0},
//	        {X: 10, Y: 0},
//	        {X: 10, Y: 10},
//	        {X: 0, Y: 10},
//	    },
//	}
//	area := overlays.GetROIArea(roi)
//	perimeter := overlays.GetROIPerimeter(roi)
//
// # OverlayGroup Fields
//
// ## Dimensions and Data
//
// - GroupNumber: Unique identifier (1-FFFF)
// - OverlayRows: Number of rows (height)
// - OverlayColumns: Number of columns (width)
// - OverlayData: Binary overlay bit data
// - OverlayBitsAllocated: Bits per pixel (usually 1)
// - OverlayBitPosition: Position in multi-bit overlay data
//
// ## Classification
//
// - OverlayType: "G" (Graphics) or "R" (ROI)
// - OverlaySubtype: CROSSHAIR, CIRCLE, POLYLINE, etc.
//
// ## Metadata
//
// - OverlayLabel: Display name for the overlay
// - OverlayDescription: Detailed text description
// - OverlayAuthor: Creator of the overlay
// - OverlayDate: Creation date (YYYYMMDD format)
// - OverlayTime: Creation time (HHMMSS.frac format)
// - CreationDateTime: Parsed time.Time value
//
// # Thread Safety
//
// All OverlayManager operations are thread-safe through internal sync.RWMutex:
//   - Multiple goroutines can read concurrently
//   - Write operations are mutually exclusive
//   - Safe for concurrent use without external synchronization
//
// Example of concurrent operations:
//
//	om := overlays.NewOverlayManager()
//
//	// Multiple readers
//	go func() {
//	    for i := 0; i < 100; i++ {
//	        _, _ = om.GetOverlay(uint16(i % 10))
//	    }
//	}()
//
//	// Single writer
//	go func() {
//	    for i := 1; i <= 100; i++ {
//	        om.AddOverlay(&overlays.OverlayGroup{
//	            GroupNumber: uint16(i),
//	            OverlayRows: 256,
//	            OverlayColumns: 256,
//	        })
//	    }
//	}()
//
// # Performance Characteristics
//
//   - **AddOverlay**: O(1) average
//   - **GetOverlay**: O(1)
//   - **RemoveOverlay**: O(1)
//   - **GetAllOverlays**: O(n) where n is number of overlays
//   - **AddROI**: O(1)
//   - **GetROIs**: O(m) where m is ROIs in group
//   - **AddGraphics**: O(1)
//   - **GetGraphics**: O(g) where g is graphics in group
//   - **ExtractBitPlane**: O(rows * columns)
//   - **MergeBitPlanes**: O(rows * columns * planes)
//   - **GetOverlayBounds**: O(rows * columns)
//   - **ValidateOverlay**: O(1) for validation checks
//   - **CopyOverlay**: O(data_size)
//   - **GetROIArea/Perimeter**: O(points)
//
// # Error Handling
//
// Operations return errors for:
//   - Nil overlays (AddOverlay)
//   - Invalid dimensions (AddOverlay, ExtractBitPlane)
//   - Overlay group not found (GetOverlay, ExtractBitPlane, etc.)
//   - Invalid group numbers (AddOverlay)
//   - Duplicate group numbers (AddOverlay)
//   - Empty labels (AddROI, AddGraphics)
//   - Insufficient points (AddROI requires >= 3 points)
//   - Invalid ROI/graphics data (AddGraphics requires >= 1 point)
//   - Invalid bit positions (ExtractBitPlane, 0-7 range)
//   - Dimension mismatch (MergeBitPlanes)
//   - Data inconsistency (ValidateOverlay)
//
// Example:
//
//	overlay := &overlays.OverlayGroup{
//	    GroupNumber: 0,  // Invalid - must be > 0
//	    OverlayRows: 256,
//	    OverlayColumns: 256,
//	}
//	err := om.AddOverlay(overlay)  // Returns error
//	if err != nil {
//	    log.Printf("Error: %v", err)
//	}
//
// # Use Cases
//
// ## Medical Image Annotation
//
//	overlay := &overlays.OverlayGroup{
//	    GroupNumber: 1,
//	    OverlayRows: 512,
//	    OverlayColumns: 512,
//	    OverlayData: extractedData,
//	    OverlayLabel: "Findings",
//	    OverlayType: "G",
//	    OverlayDescription: "Radiologist findings",
//	    OverlayAuthor: "Dr. Smith",
//	}
//	om.AddOverlay(overlay)
//
// ## Region of Interest Definition
//
//	roi := overlays.OverlayROI{
//	    Label: "Tumor",
//	    Description: "Suspicious mass in left lobe",
//	    Coordinates: tumorBoundary,  // []Point
//	    Color: "FF0000",  // Red for critical finding
//	}
//	om.AddROI(1, roi)
//	area := overlays.GetROIArea(roi)
//
// ## Graphics Overlay for Measurements
//
//	circle := overlays.OverlayGraphics{
//	    Label: "Measurement",
//	    Type: "CIRCLE",
//	    Points: []overlays.Point{{X: centerX, Y: centerY}},
//	    Color: "FFFF00",  // Yellow for measurement
//	}
//	om.AddGraphics(1, circle)
//
// # DICOM Compliance
//
// The package implements DICOM standards for:
//   - Overlay element structure (60xx,3000 onwards)
//   - Overlay group numbering (0001-FFFF)
//   - Overlay type classification (G=Graphics, R=ROI)
//   - Bit plane organization and access
//   - Overlay metadata (Author, Date, Time, Description)
//
// See: https://www.dicomstandard.org/
//
// # See Also
//
//   - dataset package: DICOM dataset structure and handling
//   - tag package: DICOM tag definitions
//   - values package: Value encoding and representation
//   - pixeldata package: Pixel data manipulation
//   - imaging package: Image processing utilities
package overlays
