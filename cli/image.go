package cli

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// imageOptions are what convert takes for --format png and jpeg.
type imageOptions struct {
	format  string // png or jpeg
	window  string // "center,width", or empty for the file's own
	frame   string // a frame counted from 1, or "all"
	quality int    // JPEG only
}

// convertToImages renders the frames asked for and writes them, returning the
// files written. One frame goes to output itself. With "all", each goes
// beside it as name-0001.ext, name-0002.ext and so on, since one file cannot
// hold several.
//
// The files hold pixels and nothing else: neither encoder is given any of
// the data set's attributes, so no name, date or identifier can ride along.
func convertToImages(input, output string, opts imageOptions) ([]string, error) {
	if err := opts.check(); err != nil {
		return nil, err
	}
	ds, err := readImageDataset(input)
	if err != nil {
		return nil, err
	}
	return writeImages(ds, output, opts)
}

// check refuses options that cannot be used, before anything is read.
func (opts imageOptions) check() error {
	if _, err := parseWindow(opts.window); err != nil {
		return err
	}
	if opts.format == "jpeg" && (opts.quality < 1 || opts.quality > 100) {
		return fmt.Errorf("--quality is %d; it runs from 1 to 100", opts.quality)
	}
	return nil
}

// readImageDataset reads a DICOM file as a data set.
func readImageDataset(input string) (*dataset.Dataset, error) {
	f, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	_ = f.Close()
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", input, err)
	}
	return df.GetDataset(), nil
}

// writeImages renders the frames of ds that opts asks for and writes them.
func writeImages(ds *dataset.Dataset, output string, opts imageOptions) ([]string, error) {
	render, err := parseWindow(opts.window)
	if err != nil {
		return nil, err
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("the instance has no image to convert: %w", err)
	}

	var frames []image.Image
	var names []string
	switch opts.frame {
	case "all":
		if frames, err = ds.RenderFrames(render); err != nil {
			return nil, err
		}
		ext := filepath.Ext(output)
		stem := strings.TrimSuffix(output, ext)
		for i := range frames {
			names = append(names, fmt.Sprintf("%s-%04d%s", stem, i+1, ext))
		}
	default:
		// Counted from 1, as viewers and the numbered file names do.
		n := 1
		if opts.frame != "" {
			if n, err = strconv.Atoi(opts.frame); err != nil {
				return nil, fmt.Errorf("--frame is %q; it is a frame number or all", opts.frame)
			}
		}
		if n < 1 || n > info.NumberOfFrames {
			return nil, fmt.Errorf("--frame is %d; the image has frames 1 to %d", n, info.NumberOfFrames)
		}
		img, err := ds.RenderFrame(n-1, render)
		if err != nil {
			return nil, err
		}
		frames, names = []image.Image{img}, []string{output}
	}

	for i, img := range frames {
		// Encoded before the file is created, so a failure leaves nothing.
		var buf bytes.Buffer
		if opts.format == "jpeg" {
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: opts.quality})
		} else {
			err = png.Encode(&buf, img)
		}
		if err != nil {
			return nil, fmt.Errorf("encoding frame %d: %w", i+1, err)
		}
		if err := os.WriteFile(names[i], buf.Bytes(), 0o644); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// parseWindow reads --window center,width.
func parseWindow(value string) (*dataset.RenderOptions, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("--window is %q; it is center,width", value)
	}
	center, errCenter := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	width, errWidth := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errCenter != nil || errWidth != nil {
		return nil, fmt.Errorf("--window is %q; it is center,width as numbers", value)
	}
	if width < 1 {
		return nil, fmt.Errorf("--window has a width of %v; it has to be at least 1", width)
	}
	return &dataset.RenderOptions{Window: &dataset.Window{Center: center, Width: width}}, nil
}
