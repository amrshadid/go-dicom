package cli

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/internal/iccembed"
	"github.com/amrshadid/go-dicom/tag"
)

// imageOptions are what convert takes for --format png and jpeg.
type imageOptions struct {
	format  string // png or jpeg
	window  string // "center,width", or empty for the file's own
	frame   string // a frame counted from 1, or "all"
	quality int    // JPEG only
	// outline is regions to draw a line around in each picture, for a
	// preview of what a detector found.
	outline []dataset.PixelRegion
}

// convertToImages renders the frames asked for and writes them, returning the
// files written. One frame goes to output itself. With "all", each goes
// beside it as name-0001.ext, name-0002.ext and so on, since one file cannot
// hold several.
//
// The Pixel Data is left in the file and read a frame, or a band of rows,
// at a time (#533): one frame of a hundred costs one frame, and a 200 MB
// frame written as PNG costs a band. A file that cannot be read that way
// is read whole, as before.
//
// The files hold pixels and, when the image has one, its ICC profile, which
// describes a device and not a patient: neither encoder is given any other
// attribute, so no name, date or identifier can ride along.
func convertToImages(input, output string, opts imageOptions) ([]string, error) {
	if err := opts.check(); err != nil {
		return nil, err
	}
	f, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	ds, frames := openFramesInFile(f)
	if frames == nil {
		if ds, err = readImageDataset(input); err != nil {
			return nil, err
		}
		return writeImages(ds, output, opts)
	}
	return writeRendered(ds, output, opts, func(list []int, render *dataset.RenderOptions,
		use func(int, image.Image) error) error {
		return ds.RenderFramesFrom(frames, list, render, use)
	})
}

// openFramesInFile reads a file's attributes, every value but Pixel Data,
// and a reader of its frames by position. It returns a nil reader for a
// file whose frames are not read that way, which is then read whole: a
// deflated file, whose positions count in the inflated data set; one that
// names no transfer syntax, or one with no decoder here, whose refusal is
// the whole read's; Float or Double Float Pixel Data, which is not
// rendered; and one whose structure does not read, so that the error is
// the one the whole read gives.
func openFramesInFile(f *os.File) (*dataset.Dataset, *filereader.FrameReader) {
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
	if err != nil || df.Deflated {
		return nil, nil
	}
	if pixels := df.PixelDataElement(); pixels == nil || pixels.Tag != tag.New(0x7FE0, 0x0010) {
		return nil, nil
	}
	ds := df.GetDataset()
	syntax := ds.TransferSyntaxUID()
	if _, err := compress.TransferSyntaxToCompressionType(syntax); syntax == "" ||
		(compress.IsEncapsulated(syntax) && err != nil) {
		return nil, nil
	}
	frames, err := df.FrameReader(f)
	if err != nil {
		return nil, nil
	}
	return ds, frames
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

// writeImages renders the frames of ds, which holds its Pixel Data, that
// opts asks for and writes them.
func writeImages(ds *dataset.Dataset, output string, opts imageOptions) ([]string, error) {
	return writeRendered(ds, output, opts, func(list []int, render *dataset.RenderOptions,
		use func(int, image.Image) error) error {
		if len(list) == 1 {
			img, err := ds.RenderFrame(list[0], render)
			if err != nil {
				return err
			}
			return use(list[0], img)
		}
		return ds.RenderEachFrame(render, use)
	})
}

// renderFrames renders the frames listed, counted from 0, and hands each to
// use as it is made.
type renderFrames func(list []int, render *dataset.RenderOptions, use func(frame int, img image.Image) error) error

// writeRendered renders the frames of ds that opts asks for through render
// and writes them.
//
// Each frame is rendered, encoded straight into a file and let go before the
// next is made, so memory holds one picture and none of the encoded bytes
// (#331). Rendering every frame first and encoding each into memory held all
// of them and their encodings: 418 MB for a hundred megapixel frames, and a
// 200 MB image of noise took 482 MB to become one PNG of 100 MB. The files
// are written under temporary names and take theirs only when every one is
// written, so a failure still leaves nothing.
func writeRendered(ds *dataset.Dataset, output string, opts imageOptions, render renderFrames) ([]string, error) {
	options, err := parseWindow(opts.window)
	if err != nil {
		return nil, err
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("the instance has no image to convert: %w", err)
	}

	var pending imageFiles
	defer pending.discard()
	// The image's ICC profile goes into every picture, unchanged: it says
	// what color space the samples are in, so a viewer that manages color
	// shows them as the image means. Nothing is converted (#546).
	if pending.profile, err = ds.ICCProfile(); err != nil {
		pending.warn(err)
	}

	switch opts.frame {
	case "all":
		ext := filepath.Ext(output)
		stem := strings.TrimSuffix(output, ext)
		list := make([]int, info.NumberOfFrames)
		for f := range list {
			list[f] = f
		}
		err = render(list, options, func(f int, img image.Image) error {
			if err := pending.write(fmt.Sprintf("%s-%04d%s", stem, f+1, ext), img, f+1, opts); err != nil {
				return err
			}
			// The frame is garbage now, and the collector would leave it
			// until the heap had doubled: with 200 MB of pixels held, that
			// is 200 MB of written frames. The pixels are bytes, which the
			// collector does not scan, so collecting costs little.
			runtime.GC()
			return nil
		})
		if err != nil {
			return nil, err
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
		err = render([]int{n - 1}, options, func(_ int, img image.Image) error {
			return pending.write(output, img, 1, opts)
		})
		if err != nil {
			return nil, err
		}
	}
	return pending.keep()
}

// imageFiles are pictures written under temporary names beside the names
// they will have.
type imageFiles struct {
	names, temporary []string
	// profile is the ICC profile each picture carries, nil for none.
	profile []byte
	warned  bool
}

// warn says once that the pictures are written without the image's ICC
// profile, and why.
func (p *imageFiles) warn(err error) {
	if !p.warned {
		fmt.Fprintf(os.Stderr, "warning: %v; the pictures are written without it\n", err)
		p.warned = true
	}
}

// carrying returns the writer to encode img into: out itself, or one that
// writes the profile into the PNG or JPEG as it goes.
func (p *imageFiles) carrying(out io.Writer, img image.Image, format string) io.Writer {
	if p.profile == nil {
		return out
	}
	if err := iccembed.Fits(p.profile, img); err != nil {
		p.warn(err)
		return out
	}
	if format != "jpeg" {
		return iccembed.PNG(out, p.profile)
	}
	w, err := iccembed.JPEG(out, p.profile)
	if err != nil {
		p.warn(err)
		return out
	}
	return w
}

// write encodes img into a temporary file beside name. frame numbers it in an
// error.
func (p *imageFiles) write(name string, img image.Image, frame int, opts imageOptions) error {
	if len(opts.outline) > 0 {
		img = outlined(img, opts.outline)
	}
	if opts.format == "jpeg" {
		img = asGray(img)
	}
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".*.partial")
	if err != nil {
		return err
	}
	p.names, p.temporary = append(p.names, name), append(p.temporary, f.Name())

	// A megabyte at a time: the encoders write in small pieces, and a write
	// to the file for each made a hundred frames take twice as long.
	out := bufio.NewWriterSize(f, 1<<20)
	encoder := p.carrying(out, img, opts.format)
	if opts.format == "jpeg" {
		err = jpeg.Encode(encoder, img, &jpeg.Options{Quality: opts.quality})
	} else {
		err = png.Encode(encoder, img)
	}
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("encoding frame %d: %w", frame, err)
	}
	if err := out.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// CreateTemp makes the file readable by its owner alone; a picture is
	// written as os.WriteFile wrote it before.
	return os.Chmod(f.Name(), 0o644)
}

// keep gives every file its name, and returns the names. When one cannot
// take its name, those that already have theirs are removed: some of the
// frames would pass for all of them.
func (p *imageFiles) keep() ([]string, error) {
	for i, name := range p.names {
		if err := os.Rename(p.temporary[i], name); err != nil {
			for _, kept := range p.names[:i] {
				_ = os.Remove(kept)
			}
			return nil, err
		}
		p.temporary[i] = ""
	}
	return p.names, nil
}

// discard removes whatever is still under a temporary name.
func (p *imageFiles) discard() {
	for _, name := range p.temporary {
		if name != "" {
			_ = os.Remove(name)
		}
	}
}

// asGray returns a grayscale picture that is not an *image.Gray, a frame
// RenderFramesFrom draws a band at a time, as an *image.Gray, and any other
// picture as it is. image/jpeg writes one component only for an
// *image.Gray, and three for any other grayscale picture: a larger file
// than the one this command has always written.
func asGray(img image.Image) image.Image {
	if _, ok := img.(*image.Gray); ok || img.ColorModel() != color.GrayModel {
		return img
	}
	bounds := img.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.Pix[gray.PixOffset(x, y)] = img.At(x, y).(color.Gray).Y
		}
	}
	return gray
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
