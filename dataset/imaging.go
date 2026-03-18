package dataset

import (
	"fmt"
	"math"
	"strconv"

	"github.com/amrshadid/go-dicom/tag"
)

// WindowingParams holds window/level parameters for display
type WindowingParams struct {
	Center int32 // Window center (level)
	Width  int32 // Window width
}

// GetWindowingParameters extracts windowing parameters from the dataset.
// Returns default parameters if not found: Center=40, Width=400 (typical for CT)
func (ds *Dataset) GetWindowingParameters() *WindowingParams {
	params := &WindowingParams{
		Center: 40,  // Default CT window center (soft tissue)
		Width:  400, // Default CT window width
	}

	// Try to get WindowCenter (0028,1050)
	if centerElem, ok := ds.Get(tag.New(0x0028, 0x1050)); ok {
		if val, err := extractStringValue(centerElem); err == nil {
			if center, err := strconv.ParseInt(val, 10, 32); err == nil {
				params.Center = int32(center)
			}
		}
	}

	// Try to get WindowWidth (0028,1051)
	if widthElem, ok := ds.Get(tag.New(0x0028, 0x1051)); ok {
		if val, err := extractStringValue(widthElem); err == nil {
			if width, err := strconv.ParseInt(val, 10, 32); err == nil {
				params.Width = int32(width)
			}
		}
	}

	return params
}

// ApplyWindowing applies window/level transformation to 16-bit pixel data.
// Maps the windowed range to 0-255 (8-bit display range) using standard DICOM windowing algorithm.
func (ds *Dataset) ApplyWindowing(center, width int32) ([][][]uint8, error) {
	// Retrieve 16-bit pixel data
	pixelArray, err := ds.PixelArray()
	if err != nil {
		return nil, err
	}

	// Type assert to 16-bit array format
	pixelData16, ok := pixelArray.([][][]uint16)
	if !ok {
		return nil, fmt.Errorf("ApplyWindowing requires 16-bit pixel data")
	}

	frames := len(pixelData16)
	if frames == 0 {
		return nil, fmt.Errorf("no frames in pixel data")
	}

	rows := len(pixelData16[0])
	if rows == 0 {
		return nil, fmt.Errorf("no rows in pixel data")
	}

	cols := len(pixelData16[0][0])
	if cols == 0 {
		return nil, fmt.Errorf("no columns in pixel data")
	}

	// Create output 8-bit array
	output := make([][][]uint8, frames)

	// Pre-calculate windowing constants for efficiency
	centerFloat := float64(center)
	widthFloat := float64(width)
	halfWidth := widthFloat / 2.0
	minValue := centerFloat - halfWidth
	maxValue := centerFloat + halfWidth
	valueRange := maxValue - minValue

	for f := 0; f < frames; f++ {
		output[f] = make([][]uint8, rows)
		for r := 0; r < rows; r++ {
			output[f][r] = make([]uint8, cols)
			for c := 0; c < cols; c++ {
				// Get original pixel value
				pixelValue := float64(pixelData16[f][r][c])

				// Apply windowing
				var displayValue uint8
				if pixelValue < minValue {
					displayValue = 0
				} else if pixelValue > maxValue {
					displayValue = 255
				} else {
					// Linear interpolation between 0-255
					normalized := (pixelValue - minValue) / valueRange
					displayValue = uint8(normalized * 255.0)
				}

				output[f][r][c] = displayValue
			}
		}
	}

	return output, nil
}

// ApplyWindowingWithParams applies windowing using stored dataset parameters.
// Falls back to default parameters if not found in dataset.
func (ds *Dataset) ApplyWindowingWithParams() ([][][]uint8, error) {
	params := ds.GetWindowingParameters()
	return ds.ApplyWindowing(params.Center, params.Width)
}

// ColorSpace represents a color space conversion type
type ColorSpace string

const (
	ColorSpaceRGB        ColorSpace = "RGB"
	ColorSpaceYCbCr      ColorSpace = "YCbCr"
	ColorSpaceHSV        ColorSpace = "HSV"
	ColorSpaceGray       ColorSpace = "GRAY"
	ColorSpaceMonochrome ColorSpace = "MONOCHROME"
)

// ConvertColorSpace converts pixel data between color spaces.
// Currently supports RGB ↔ YCbCr conversions (most common in DICOM).
func (ds *Dataset) ConvertColorSpace(fromSpace, toSpace ColorSpace) (interface{}, error) {
	// Get current pixel array
	pixelArray, err := ds.PixelArray()
	if err != nil {
		return nil, err
	}

	// Check if it's an RGB image (3 samples)
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	if info.SamplesPerPixel < 3 {
		return nil, fmt.Errorf("color space conversion requires at least 3 samples per pixel, got %d", info.SamplesPerPixel)
	}

	// Handle conversions
	switch {
	case fromSpace == ColorSpaceRGB && toSpace == ColorSpaceYCbCr:
		return convertRGBToYCbCr(pixelArray, info)
	case fromSpace == ColorSpaceYCbCr && toSpace == ColorSpaceRGB:
		return convertYCbCrToRGB(pixelArray, info)
	case fromSpace == ColorSpaceRGB && toSpace == ColorSpaceHSV:
		return convertRGBToHSV(pixelArray, info)
	case fromSpace == ColorSpaceHSV && toSpace == ColorSpaceRGB:
		return convertHSVToRGB(pixelArray, info)
	default:
		return nil, fmt.Errorf("unsupported color space conversion: %s → %s", fromSpace, toSpace)
	}
}

// RGB to YCbCr conversion (DICOM standard)
// Uses ITU-R BT.601 formula
func convertRGBToYCbCr(pixelArray interface{}, info *PixelDataInfo) ([][][]uint8, error) {
	pixelData8, ok := pixelArray.([][][]uint8)
	if !ok {
		return nil, fmt.Errorf("RGB to YCbCr requires 8-bit pixel data")
	}

	frames := len(pixelData8)
	rows := len(pixelData8[0])
	cols := len(pixelData8[0][0])

	output := make([][][]uint8, frames)

	for f := 0; f < frames; f++ {
		output[f] = make([][]uint8, rows)
		for r := 0; r < rows; r++ {
			output[f][r] = make([]uint8, cols*3)
			for c := 0; c < cols; c++ {
				baseIdx := c * 3

				// Get RGB components (assume RGB storage)
				red := float64(pixelData8[f][r][baseIdx])
				green := float64(pixelData8[f][r][baseIdx+1])
				blue := float64(pixelData8[f][r][baseIdx+2])

				// ITU-R BT.601 formula
				y := 0.299*red + 0.587*green + 0.114*blue
				cb := 128.0 - (0.168736*red + 0.331264*green + 0.5*blue)
				cr := 128.0 + (0.5*red - 0.418688*green - 0.081312*blue)

				// Clamp to valid range
				output[f][r][baseIdx] = uint8(math.Min(255, math.Max(0, y)))
				output[f][r][baseIdx+1] = uint8(math.Min(255, math.Max(0, cb)))
				output[f][r][baseIdx+2] = uint8(math.Min(255, math.Max(0, cr)))
			}
		}
	}

	return output, nil
}

// YCbCr to RGB conversion (inverse of RGB→YCbCr)
func convertYCbCrToRGB(pixelArray interface{}, info *PixelDataInfo) ([][][]uint8, error) {
	pixelData8, ok := pixelArray.([][][]uint8)
	if !ok {
		return nil, fmt.Errorf("YCbCr to RGB requires 8-bit pixel data")
	}

	frames := len(pixelData8)
	rows := len(pixelData8[0])
	cols := len(pixelData8[0][0])

	output := make([][][]uint8, frames)

	for f := 0; f < frames; f++ {
		output[f] = make([][]uint8, rows)
		for r := 0; r < rows; r++ {
			output[f][r] = make([]uint8, cols*3)
			for c := 0; c < cols; c++ {
				baseIdx := c * 3

				// Get YCbCr components
				y := float64(pixelData8[f][r][baseIdx])
				cb := float64(pixelData8[f][r][baseIdx+1])
				cr := float64(pixelData8[f][r][baseIdx+2])

				// Inverse ITU-R BT.601 formula
				red := y + 1.402*(cr-128.0)
				green := y - 0.344136*(cb-128.0) - 0.714136*(cr-128.0)
				blue := y + 1.772*(cb-128.0)

				// Clamp to valid range
				output[f][r][baseIdx] = uint8(math.Min(255, math.Max(0, red)))
				output[f][r][baseIdx+1] = uint8(math.Min(255, math.Max(0, green)))
				output[f][r][baseIdx+2] = uint8(math.Min(255, math.Max(0, blue)))
			}
		}
	}

	return output, nil
}

// RGB to HSV conversion (useful for certain medical imaging tasks)
func convertRGBToHSV(pixelArray interface{}, info *PixelDataInfo) ([][][]uint8, error) {
	pixelData8, ok := pixelArray.([][][]uint8)
	if !ok {
		return nil, fmt.Errorf("RGB to HSV requires 8-bit pixel data")
	}

	frames := len(pixelData8)
	rows := len(pixelData8[0])
	cols := len(pixelData8[0][0])

	output := make([][][]uint8, frames)

	for f := 0; f < frames; f++ {
		output[f] = make([][]uint8, rows)
		for r := 0; r < rows; r++ {
			output[f][r] = make([]uint8, cols*3)
			for c := 0; c < cols; c++ {
				baseIdx := c * 3

				// Get normalized RGB (0-1 range)
				r_norm := float64(pixelData8[f][r][baseIdx]) / 255.0
				g_norm := float64(pixelData8[f][r][baseIdx+1]) / 255.0
				b_norm := float64(pixelData8[f][r][baseIdx+2]) / 255.0

				// Calculate HSV
				cmax := math.Max(r_norm, math.Max(g_norm, b_norm))
				cmin := math.Min(r_norm, math.Min(g_norm, b_norm))
				delta := cmax - cmin

				// Hue (0-360°, scaled to 0-255)
				var hue float64
				if delta == 0 {
					hue = 0
				} else if cmax == r_norm {
					hue = 60 * (math.Mod((g_norm-b_norm)/delta, 6))
				} else if cmax == g_norm {
					hue = 60 * ((b_norm-r_norm)/delta + 2)
				} else {
					hue = 60 * ((r_norm-g_norm)/delta + 4)
				}
				if hue < 0 {
					hue += 360
				}

				// Saturation (0-1, scaled to 0-255)
				saturation := 0.0
				if cmax != 0 {
					saturation = delta / cmax
				}

				// Value (0-1, scaled to 0-255)
				value := cmax

				output[f][r][baseIdx] = uint8((hue / 360.0) * 255.0)
				output[f][r][baseIdx+1] = uint8(saturation * 255.0)
				output[f][r][baseIdx+2] = uint8(value * 255.0)
			}
		}
	}

	return output, nil
}

// HSV to RGB conversion
func convertHSVToRGB(pixelArray interface{}, info *PixelDataInfo) ([][][]uint8, error) {
	pixelData8, ok := pixelArray.([][][]uint8)
	if !ok {
		return nil, fmt.Errorf("HSV to RGB requires 8-bit pixel data")
	}

	frames := len(pixelData8)
	rows := len(pixelData8[0])
	cols := len(pixelData8[0][0])

	output := make([][][]uint8, frames)

	for f := 0; f < frames; f++ {
		output[f] = make([][]uint8, rows)
		for r := 0; r < rows; r++ {
			output[f][r] = make([]uint8, cols*3)
			for c := 0; c < cols; c++ {
				baseIdx := c * 3

				// Get normalized HSV
				h := (float64(pixelData8[f][r][baseIdx]) / 255.0) * 360.0
				s := float64(pixelData8[f][r][baseIdx+1]) / 255.0
				v := float64(pixelData8[f][r][baseIdx+2]) / 255.0

				// Convert to RGB
				c := v * s
				x := c * (1 - math.Mod(h/60.0, 2) + 1)
				m := v - c

				var r_norm, g_norm, b_norm float64

				if h >= 0 && h < 60 {
					r_norm, g_norm, b_norm = c, x, 0
				} else if h >= 60 && h < 120 {
					r_norm, g_norm, b_norm = x, c, 0
				} else if h >= 120 && h < 180 {
					r_norm, g_norm, b_norm = 0, c, x
				} else if h >= 180 && h < 240 {
					r_norm, g_norm, b_norm = 0, x, c
				} else if h >= 240 && h < 300 {
					r_norm, g_norm, b_norm = x, 0, c
				} else {
					r_norm, g_norm, b_norm = c, 0, x
				}

				output[f][r][baseIdx] = uint8((r_norm + m) * 255.0)
				output[f][r][baseIdx+1] = uint8((g_norm + m) * 255.0)
				output[f][r][baseIdx+2] = uint8((b_norm + m) * 255.0)
			}
		}
	}

	return output, nil
}

// LUTSequence represents a DICOM Lookup Table sequence
type LUTSequence struct {
	Data             []uint32 // LUT data
	FirstMappedValue uint32   // First input value
	NumEntries       uint32   // Number of LUT entries
	BitDepth         uint32   // Output bit depth
}

// GetVOILUT retrieves the Value of Interest Lookup Table from the dataset.
// Returns error if VOI LUT not present.
func (ds *Dataset) GetVOILUT() (*LUTSequence, error) {
	// Try to get VOILUTSequence (0028,3010)
	// This is a sequence that contains the actual LUT data
	// Implementation depends on sequence handling in dataset

	return nil, fmt.Errorf("VOI LUT retrieval not yet implemented - requires sequence handling")
}

// ApplyVOILUT applies Value of Interest Lookup Table to pixel data.
// The VOI LUT maps pixel values through a lookup table for enhanced visualization.
func (ds *Dataset) ApplyVOILUT() (interface{}, error) {
	// Get the VOI LUT from dataset
	lut, err := ds.GetVOILUT()
	if err != nil {
		// If no VOI LUT, fallback to windowing
		return ds.ApplyWindowingWithParams()
	}

	// Get pixel array
	pixelArray, err := ds.PixelArray()
	if err != nil {
		return nil, err
	}

	// Apply LUT based on data type
	switch arr := pixelArray.(type) {
	case [][][]uint16:
		return applyLUTTo16Bit(arr, lut)
	case [][][]uint32:
		return applyLUTTo32Bit(arr, lut)
	default:
		return nil, fmt.Errorf("unsupported pixel data type for LUT application")
	}
}

// applyLUTTo16Bit applies a lookup table to 16-bit pixel data
func applyLUTTo16Bit(pixelData [][][]uint16, lut *LUTSequence) ([][][]uint8, error) {
	frames := len(pixelData)
	rows := len(pixelData[0])
	cols := len(pixelData[0][0])

	output := make([][][]uint8, frames)

	for f := 0; f < frames; f++ {
		output[f] = make([][]uint8, rows)
		for r := 0; r < rows; r++ {
			output[f][r] = make([]uint8, cols)
			for c := 0; c < cols; c++ {
				pixelValue := pixelData[f][r][c]
				// Calculate LUT index
				idx := int(pixelValue) - int(lut.FirstMappedValue)
				if idx < 0 || idx >= int(lut.NumEntries) {
					output[f][r][c] = 0
				} else {
					// LUT maps to output values (typically 8-bit)
					output[f][r][c] = uint8(lut.Data[idx] & 0xFF)
				}
			}
		}
	}

	return output, nil
}

// applyLUTTo32Bit applies a lookup table to 32-bit pixel data
func applyLUTTo32Bit(pixelData [][][]uint32, lut *LUTSequence) ([][][]uint8, error) {
	frames := len(pixelData)
	rows := len(pixelData[0])
	cols := len(pixelData[0][0])

	output := make([][][]uint8, frames)

	for f := 0; f < frames; f++ {
		output[f] = make([][]uint8, rows)
		for r := 0; r < rows; r++ {
			output[f][r] = make([]uint8, cols)
			for c := 0; c < cols; c++ {
				pixelValue := pixelData[f][r][c]
				// Calculate LUT index
				idx := int(pixelValue) - int(lut.FirstMappedValue)
				if idx < 0 || idx >= int(lut.NumEntries) {
					output[f][r][c] = 0
				} else {
					// LUT maps to output values (typically 8-bit)
					output[f][r][c] = uint8(lut.Data[idx] & 0xFF)
				}
			}
		}
	}

	return output, nil
}
