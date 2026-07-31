package dataset

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
)

// GetImage converts pixel data to a standard Go image.Image interface.
// Handles both grayscale and color images.
// For 16-bit data, converts to 8-bit using windowing.
func (ds *Dataset) GetImage() (image.Image, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}

	pixelArray, err := ds.PixelArray()
	if err != nil {
		return nil, err
	}

	// Single frame only for now
	if info.NumberOfFrames > 1 {
		return nil, fmt.Errorf("GetImage only supports single-frame images, got %d frames", info.NumberOfFrames)
	}

	// Handle based on samples per pixel
	switch info.SamplesPerPixel {
	case 1:
		// Grayscale image
		return createGrayscaleImage(pixelArray, info)
	case 3:
		// RGB or YCbCr color image
		return createColorImage(pixelArray, info)
	default:
		return nil, fmt.Errorf("unsupported samples per pixel: %d", info.SamplesPerPixel)
	}
}

// SaveAsPNG exports the pixel data to a PNG file.
// Automatically handles windowing for 16-bit data.
// Creates a standard PNG image viewable in any image viewer.
func (ds *Dataset) SaveAsPNG(filepath string) error {
	img, err := ds.GetImage()
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("failed to encode PNG: %w", err)
	}

	return nil
}

// SaveAsWindowedPNG exports pixel data to PNG with custom windowing applied.
// Useful for saving multiple views of the same dataset with different windows.
// Example: Save bone, lung, and soft tissue windows of a CT image.
func (ds *Dataset) SaveAsWindowedPNG(filepath string, center, width int32) error {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return err
	}

	// Only works with 16-bit data for proper windowing
	if info.BitsAllocated != 16 {
		return fmt.Errorf("SaveAsWindowedPNG requires 16-bit pixel data")
	}

	// Apply windowing
	windowed, err := ds.ApplyWindowing(center, width)
	if err != nil {
		return fmt.Errorf("failed to apply windowing: %w", err)
	}

	// Create grayscale image from windowed 8-bit data
	img := createGrayscaleImageFrom8Bit(windowed, info)

	// Save as PNG
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("failed to encode PNG: %w", err)
	}

	return nil
}

// SaveAsJPEG exports the pixel data to a JPEG file with specified quality.
// Quality should be between 1-100 (higher = better quality, larger file).
// Useful for web display and compression.
func (ds *Dataset) SaveAsJPEG(filepath string, quality int) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("quality must be between 1-100, got %d", quality)
	}

	img, err := ds.GetImage()
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: quality}); err != nil {
		return fmt.Errorf("failed to encode JPEG: %w", err)
	}

	return nil
}

// SaveAsWindowedJPEG exports pixel data to JPEG with windowing and quality control.
func (ds *Dataset) SaveAsWindowedJPEG(filepath string, center, width int32, quality int) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("quality must be between 1-100, got %d", quality)
	}

	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return err
	}

	// Only works with 16-bit data for proper windowing
	if info.BitsAllocated != 16 {
		return fmt.Errorf("SaveAsWindowedJPEG requires 16-bit pixel data")
	}

	// Apply windowing
	windowed, err := ds.ApplyWindowing(center, width)
	if err != nil {
		return fmt.Errorf("failed to apply windowing: %w", err)
	}

	// Create grayscale image from windowed 8-bit data
	img := createGrayscaleImageFrom8Bit(windowed, info)

	// Save as JPEG
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: quality}); err != nil {
		return fmt.Errorf("failed to encode JPEG: %w", err)
	}

	return nil
}

// ImageExportOptions allows customization of image export behavior
type ImageExportOptions struct {
	Quality             int   // JPEG quality (1-100)
	AutoWindow          bool  // Auto-apply windowing for 16-bit
	WindowCenter        int32 // Window center for auto-windowing
	WindowWidth         int32 // Window width for auto-windowing
	MaxDimension        int   // Maximum width/height (0 = no limit)
	PreserveAspectRatio bool  // Scale while preserving aspect ratio
}

// SaveWithOptions exports image with advanced options
func (ds *Dataset) SaveWithOptions(filepath string, format string, opts *ImageExportOptions) error {
	if opts == nil {
		opts = &ImageExportOptions{
			Quality:             90,
			AutoWindow:          false,
			WindowCenter:        40,
			WindowWidth:         400,
			MaxDimension:        0,
			PreserveAspectRatio: true,
		}
	}

	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return err
	}

	var img image.Image

	// Apply windowing if requested and data is 16-bit
	if opts.AutoWindow && info.BitsAllocated == 16 {
		windowed, err := ds.ApplyWindowing(opts.WindowCenter, opts.WindowWidth)
		if err != nil {
			return fmt.Errorf("failed to apply windowing: %w", err)
		}
		img = createGrayscaleImageFrom8Bit(windowed, info)
	} else {
		img, err = ds.GetImage()
		if err != nil {
			return fmt.Errorf("failed to get image: %w", err)
		}
	}

	// Apply scaling if needed
	if opts.MaxDimension > 0 {
		img = scaleImageToFit(img, opts.MaxDimension, opts.PreserveAspectRatio)
	}

	// Save based on format
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	switch format {
	case "png":
		if err := png.Encode(file, img); err != nil {
			return fmt.Errorf("failed to encode PNG: %w", err)
		}
	case "jpg", "jpeg":
		if err := jpeg.Encode(file, img, &jpeg.Options{Quality: opts.Quality}); err != nil {
			return fmt.Errorf("failed to encode JPEG: %w", err)
		}
	default:
		return fmt.Errorf("unsupported format: %s (use 'png' or 'jpeg')", format)
	}

	return nil
}

// Helper functions

// createGrayscaleImage creates a grayscale image from pixel array
func createGrayscaleImage(pixelArray interface{}, info *PixelDataInfo) (image.Image, error) {
	switch arr := pixelArray.(type) {
	case [][][]uint8:
		return createGrayscaleImageFrom8Bit(arr, info), nil
	case [][][]uint16:
		// Convert 16-bit to 8-bit using windowing
		windowed, err := applyDefaultWindowing(arr, info)
		if err != nil {
			return nil, err
		}
		return createGrayscaleImageFrom8Bit(windowed, info), nil
	case [][][]uint32:
		// Convert 32-bit to 8-bit
		windowed := convertUint32To8Bit(arr)
		return createGrayscaleImageFrom8Bit(windowed, info), nil
	default:
		return nil, fmt.Errorf("unsupported pixel data type for image creation")
	}
}

// createColorImage creates an RGB image from color pixel array
func createColorImage(pixelArray interface{}, info *PixelDataInfo) (image.Image, error) {
	switch arr := pixelArray.(type) {
	case [][][]uint8:
		return createRGBImageFrom8Bit(arr, info), nil
	case [][][]uint16:
		// Convert 16-bit to 8-bit (if color data is stored as 16-bit, unlikely)
		windowed, err := applyDefaultWindowing(arr, info)
		if err != nil {
			return nil, err
		}
		return createRGBImageFrom8Bit(windowed, info), nil
	default:
		return nil, fmt.Errorf("unsupported pixel data type for color image creation")
	}
}

// createGrayscaleImageFrom8Bit creates a standard Go grayscale image
func createGrayscaleImageFrom8Bit(pixelData [][][]uint8, info *PixelDataInfo) image.Image {
	// Use first frame only
	if len(pixelData) == 0 {
		return image.NewGray(image.Rect(0, 0, 0, 0))
	}

	frameData := pixelData[0]
	rows := len(frameData)
	cols := 0
	if rows > 0 {
		cols = len(frameData[0])
	}

	// Create grayscale image
	img := image.NewGray(image.Rect(0, 0, cols, rows))

	// Copy pixel data
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			pixel := frameData[r][c]
			img.SetGray(c, r, color.Gray{Y: pixel})
		}
	}

	return img
}

// createRGBImageFrom8Bit creates a standard Go RGB image
func createRGBImageFrom8Bit(pixelData [][][]uint8, info *PixelDataInfo) image.Image {
	// Use first frame only
	if len(pixelData) == 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}

	frameData := pixelData[0]
	rows := len(frameData)
	cols := 0
	if rows > 0 {
		cols = len(frameData[0]) / 3 // 3 samples per pixel (RGB)
	}

	// Create RGBA image
	img := image.NewRGBA(image.Rect(0, 0, cols, rows))

	// Copy pixel data
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			baseIdx := c * 3
			red := frameData[r][baseIdx]
			green := frameData[r][baseIdx+1]
			blue := frameData[r][baseIdx+2]
			img.SetRGBA(c, r, color.RGBA{R: red, G: green, B: blue, A: 255})
		}
	}

	return img
}

// applyDefaultWindowing converts 16-bit to 8-bit using default CT window
func applyDefaultWindowing(pixelData [][][]uint16, info *PixelDataInfo) ([][][]uint8, error) {
	// Use default soft tissue window for CT
	center := int32(40)
	width := int32(400)

	frames := len(pixelData)
	rows := len(pixelData[0])
	cols := len(pixelData[0][0])

	output := make([][][]uint8, frames)
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
				pixelValue := float64(pixelData[f][r][c])

				var displayValue uint8
				if pixelValue < minValue {
					displayValue = 0
				} else if pixelValue > maxValue {
					displayValue = 255
				} else {
					normalized := (pixelValue - minValue) / valueRange
					displayValue = uint8(normalized * 255.0)
				}

				output[f][r][c] = displayValue
			}
		}
	}

	return output, nil
}

// convertUint32To8Bit converts 32-bit pixel data to 8-bit
func convertUint32To8Bit(pixelData [][][]uint32) [][][]uint8 {
	frames := len(pixelData)
	output := make([][][]uint8, frames)

	for f := 0; f < frames; f++ {
		rows := len(pixelData[f])
		output[f] = make([][]uint8, rows)

		for r := 0; r < rows; r++ {
			cols := len(pixelData[f][r])
			output[f][r] = make([]uint8, cols)

			for c := 0; c < cols; c++ {
				// Simple truncation (could also do scaling)
				output[f][r][c] = uint8(pixelData[f][r][c] & 0xFF)
			}
		}
	}

	return output
}

// scaleImageToFit scales image to fit within maxDimension
func scaleImageToFit(img image.Image, maxDimension int, preserveAspect bool) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width <= maxDimension && height <= maxDimension {
		return img // No scaling needed
	}

	// Calculate scale factor
	var scaleFactor float64
	if preserveAspect {
		if width > height {
			scaleFactor = float64(maxDimension) / float64(width)
		} else {
			scaleFactor = float64(maxDimension) / float64(height)
		}
	} else {
		// Scale to fit exactly (may distort)
		scaleW := float64(maxDimension) / float64(width)
		scaleH := float64(maxDimension) / float64(height)
		if scaleW < scaleH {
			scaleFactor = scaleW
		} else {
			scaleFactor = scaleH
		}
	}

	newWidth := int(float64(width) * scaleFactor)
	newHeight := int(float64(height) * scaleFactor)

	// Create new image (simple nearest-neighbor scaling)
	scaled := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Simple nearest-neighbor scaling (could use better algorithm)
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			srcX := int(float64(x) / scaleFactor)
			srcY := int(float64(y) / scaleFactor)

			// Clamp to bounds
			if srcX >= width {
				srcX = width - 1
			}
			if srcY >= height {
				srcY = height - 1
			}

			r, g, b, a := img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY).RGBA()
			scaled.SetRGBA(x, y, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: uint8(a >> 8),
			})
		}
	}

	return scaled
}
