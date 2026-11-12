package dataset

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
)

// GetImage renders a single-frame image for display, as RenderFrame does:
// the samples read with their sign, rescaled, and windowed by the data set's
// own Window Center and Width, or by the range of its values when it has
// none; MONOCHROME1 inverted; YBR and PALETTE COLOR converted to RGB.
//
// It used to window every 16-bit image with a fixed 40/400 over the stored
// values, which shows a CT as white (#253). For a frame of a multi-frame
// image, or another window, use RenderFrame.
func (ds *Dataset) GetImage() (image.Image, error) {
	return ds.renderSingle(nil)
}

// renderSingle renders the one frame of a single-frame image.
func (ds *Dataset) renderSingle(opts *RenderOptions) (image.Image, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	if info.NumberOfFrames > 1 {
		return nil, fmt.Errorf("GetImage only supports single-frame images, got %d frames; "+
			"RenderFrame renders one frame of them", info.NumberOfFrames)
	}
	return ds.RenderFrame(0, opts)
}

// windowOf is the render option for a window given as integers.
func windowOf(center, width int32) *RenderOptions {
	return &RenderOptions{Window: &Window{Center: float64(center), Width: float64(width)}}
}

// writeImage encodes img to path as PNG, or as JPEG when quality is not 0.
func writeImage(path string, img image.Image, quality int) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	if quality == 0 {
		if err := png.Encode(file, img); err != nil {
			return fmt.Errorf("failed to encode PNG: %w", err)
		}
		return nil
	}
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: quality}); err != nil {
		return fmt.Errorf("failed to encode JPEG: %w", err)
	}
	return nil
}

// SaveAsPNG writes the image GetImage renders to a PNG file.
func (ds *Dataset) SaveAsPNG(filepath string) error {
	img, err := ds.GetImage()
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}
	return writeImage(filepath, img, 0)
}

// SaveAsWindowedPNG writes the image to a PNG file under a window of the
// caller's: bone, lung and soft tissue views of one CT, say. The window is
// in the units the rescale produces, Hounsfield units for CT, as the data
// set's own is. It applies to grayscale images.
func (ds *Dataset) SaveAsWindowedPNG(filepath string, center, width int32) error {
	img, err := ds.renderSingle(windowOf(center, width))
	if err != nil {
		return fmt.Errorf("failed to apply windowing: %w", err)
	}
	return writeImage(filepath, img, 0)
}

// SaveAsJPEG writes the image GetImage renders to a JPEG file. Quality runs
// from 1 to 100.
func (ds *Dataset) SaveAsJPEG(filepath string, quality int) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("quality must be between 1-100, got %d", quality)
	}
	img, err := ds.GetImage()
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}
	return writeImage(filepath, img, quality)
}

// SaveAsWindowedJPEG is SaveAsWindowedPNG for a JPEG file.
func (ds *Dataset) SaveAsWindowedJPEG(filepath string, center, width int32, quality int) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("quality must be between 1-100, got %d", quality)
	}
	img, err := ds.renderSingle(windowOf(center, width))
	if err != nil {
		return fmt.Errorf("failed to apply windowing: %w", err)
	}
	return writeImage(filepath, img, quality)
}

// ImageExportOptions allows customization of image export behavior
type ImageExportOptions struct {
	Quality             int   // JPEG quality (1-100)
	AutoWindow          bool  // Apply WindowCenter and WindowWidth instead of the data set's own window
	WindowCenter        int32 // Window center, in rescaled units
	WindowWidth         int32 // Window width, in rescaled units
	MaxDimension        int   // Maximum width/height (0 = no limit)
	PreserveAspectRatio bool  // Scale while preserving aspect ratio
}

// SaveWithOptions writes the image as "png", "jpg" or "jpeg", rendered as
// GetImage does, or under the options' window when AutoWindow is set, and
// scaled down to MaxDimension when that is given.
func (ds *Dataset) SaveWithOptions(filepath string, format string, opts *ImageExportOptions) error {
	if opts == nil {
		opts = &ImageExportOptions{Quality: 90, PreserveAspectRatio: true}
	}
	quality := 0
	switch format {
	case "png":
	case "jpg", "jpeg":
		// Options given without a quality are not a request for the worst one.
		quality = opts.Quality
		if quality < 1 {
			quality = 90
		}
		quality = min(quality, 100)
	default:
		return fmt.Errorf("unsupported format: %s (use 'png' or 'jpeg')", format)
	}

	var render *RenderOptions
	if opts.AutoWindow {
		render = windowOf(opts.WindowCenter, opts.WindowWidth)
	}
	img, err := ds.renderSingle(render)
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}
	if opts.MaxDimension > 0 {
		img = scaleImageToFit(img, opts.MaxDimension, opts.PreserveAspectRatio)
	}
	return writeImage(filepath, img, quality)
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

	// A long thin image still has a row and a column.
	newWidth := max(int(float64(width)*scaleFactor), 1)
	newHeight := max(int(float64(height)*scaleFactor), 1)

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
