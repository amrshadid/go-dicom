package cli

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataset"
)

// redaction is what -redact asks for: the rectangles the caller names, and
// "auto" for the ones a detector finds.
//
// The two are not the same promise. A rectangle is given by someone who
// looked at the image. "auto" is the detector's guess, which can miss, so
// the commands say so wherever they report it.
type redaction struct {
	regions []dataset.PixelRegion
	auto    bool
}

func (r *redaction) String() string {
	if r.auto {
		return fmt.Sprint(len(r.regions), " region(s) and auto")
	}
	return fmt.Sprint(len(r.regions), " region(s)")
}

// Set takes one -redact value: x,y,width,height or auto.
func (r *redaction) Set(value string) error {
	if strings.EqualFold(strings.TrimSpace(value), "auto") {
		r.auto = true
		return nil
	}
	var region dataset.PixelRegion
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return fmt.Errorf("a region is x,y,width,height, or auto")
	}
	for i, field := range []*int{&region.X, &region.Y, &region.Width, &region.Height} {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return fmt.Errorf("a region is x,y,width,height in whole pixels")
		}
		*field = n
	}
	if region.Width <= 0 || region.Height <= 0 {
		return fmt.Errorf("a region needs a width and a height above zero")
	}
	r.regions = append(r.regions, region)
	return nil
}

// asked reports whether any redaction was asked for.
func (r *redaction) asked() bool { return r.auto || len(r.regions) > 0 }

// detectorCaution is said wherever a detector's regions are reported.
const detectorCaution = "A detector can miss text. Look at the image before it leaves your hands."

// checkDetectedShare refuses a detector's regions that cover more of the
// picture than text does (anonymize.CheckDetectedShare), saying what to do
// instead.
func checkDetectedShare(ds *dataset.Dataset, regions []dataset.PixelRegion) error {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return err
	}
	return implausibleHint(anonymize.CheckDetectedShare(regions, info.Columns, info.Rows))
}

// implausibleHint adds to a refused automatic redaction what to do instead.
func implausibleHint(err error) error {
	if errors.Is(err, anonymize.ErrImplausibleRedaction) {
		return fmt.Errorf("%w. Nothing was redacted: see what was found with -detect "+
			"(or share -preview), and give the regions that hold text with -redact x,y,width,height", err)
	}
	return err
}

// regionsText writes regions as -redact takes them, one to a line.
func regionsText(regions []dataset.PixelRegion) string {
	lines := make([]string, len(regions))
	for i, r := range regions {
		lines[i] = fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.Width, r.Height)
	}
	return strings.Join(lines, "\n")
}

// printDetected is -detect: the regions in which the detector finds
// lettering in a file, printed as -redact takes them, with nothing written
// and nothing changed.
func printDetected(input string) error {
	ds, err := readImageDataset(input)
	if err != nil {
		return err
	}
	regions, err := anonymize.DetectBurnedInText(ds, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", input, err)
	}
	if len(regions) == 0 {
		fmt.Println("No text was detected. " + detectorCaution)
		return nil
	}
	fmt.Println(regionsText(regions))
	return nil
}

// outlined returns a copy of a picture with the regions outlined, for a
// person to check a detector's work against.
func outlined(img image.Image, regions []dataset.PixelRegion) image.Image {
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	// Magenta: a color neither a grayscale image nor a caption is in.
	mark := color.NRGBA{255, 0, 255, 255}
	origin := img.Bounds().Min
	for _, r := range regions {
		box := image.Rect(r.X, r.Y, r.X+r.Width, r.Y+r.Height).Add(origin).Intersect(out.Bounds())
		if box.Empty() {
			continue
		}
		for x := box.Min.X; x < box.Max.X; x++ {
			out.SetNRGBA(x, box.Min.Y, mark)
			out.SetNRGBA(x, box.Max.Y-1, mark)
		}
		for y := box.Min.Y; y < box.Max.Y; y++ {
			out.SetNRGBA(box.Min.X, y, mark)
			out.SetNRGBA(box.Max.X-1, y, mark)
		}
	}
	return out
}
