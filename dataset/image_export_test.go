package dataset_test

import (
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// GetImage and the SaveAs functions built on it turned 16 bits into 8 with a
// window of 40/400 over the stored values read as unsigned, whatever the
// image was (#253). A CT came out white: its stored values are a thousand
// above the Hounsfield units the window is given in.

// ctRow is the image of TestRenderRescalesThenWindows: -200, -160, 40, 239
// and 300 HU under a 40/400 window.
func ctRow() *dataset.Dataset {
	return picture{allocated: 16, stored: 16, photometric: "MONOCHROME2",
		px: words(824, 864, 1064, 1263, 1324),
		text: map[tag.Tag]string{
			tagIntercept: "-1024", tagSlope: "1",
			tagWindowCenter: "40", tagWindowWidth: "400",
		}}.dataset()
}

func grayRow(t *testing.T, img image.Image) []uint8 {
	t.Helper()
	b := img.Bounds()
	out := make([]uint8, 0, b.Dx())
	for x := b.Min.X; x < b.Max.X; x++ {
		r, g, bl, _ := img.At(x, b.Min.Y).RGBA()
		if r != g || g != bl {
			t.Fatalf("pixel %d is not gray", x)
		}
		out = append(out, uint8(r>>8))
	}
	return out
}

func decodePNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestGetImageRendersAsAViewerDoes(t *testing.T) {
	img, err := ctRow().GetImage()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := grayRow(t, img), []uint8{0, 0, 128, 255, 255}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}
}

func TestGetImageReadsSignedSamples(t *testing.T) {
	ds := picture{allocated: 16, stored: 16, signed: true, photometric: "MONOCHROME2",
		px:   words(-1000, 0, 1000),
		text: map[tag.Tag]string{tagWindowCenter: "0", tagWindowWidth: "2000"}}.dataset()
	img, err := ds.GetImage()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := grayRow(t, img), []uint8{0, 128, 255}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}
}

func TestGetImageNamesWhatRendersAFrame(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", frames: 2, px: []byte{1, 2, 3, 4}}.dataset()
	_, err := ds.GetImage()
	if err == nil || !strings.Contains(err.Error(), "RenderFrame") {
		t.Errorf("GetImage on two frames = %v, want an error naming RenderFrame", err)
	}
}

func TestSaveAsPNGWritesTheRenderedImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ct.png")
	if err := ctRow().SaveAsPNG(path); err != nil {
		t.Fatal(err)
	}
	if got, want := grayRow(t, decodePNG(t, path)), []uint8{0, 0, 128, 255, 255}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}
}

// The window is given in the units the rescale produces, as the data set's
// own is: 300/200 over the row above is 200 to 400 HU.
func TestSaveAsWindowedPNGWindowsTheRescaledValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ct.png")
	if err := ctRow().SaveAsWindowedPNG(path, 300, 200); err != nil {
		t.Fatal(err)
	}
	got := grayRow(t, decodePNG(t, path))
	// -200, -160 and 40 HU are below the window, 239 inside it, 300 at its middle.
	if got[0] != 0 || got[1] != 0 || got[2] != 0 || got[3] == 0 || got[3] >= got[4] || got[4] < 126 || got[4] > 130 {
		t.Errorf("levels %v, want three black, then rising to the middle", got)
	}

	// An 8-bit image has a window as well.
	small := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", px: []byte{0, 100, 200}}.dataset()
	if err := small.SaveAsWindowedPNG(path, 100, 50); err != nil {
		t.Fatalf("an 8-bit image was refused: %v", err)
	}
	if got := grayRow(t, decodePNG(t, path)); got[0] != 0 || got[2] != 255 {
		t.Errorf("8-bit levels %v, want black below the window and white above", got)
	}
}

func TestSaveAsJPEG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ct.jpg")
	if err := ctRow().SaveAsJPEG(path, 0); err == nil {
		t.Error("quality 0 was accepted")
	}
	if err := ctRow().SaveAsWindowedJPEG(path, 40, 400, 95); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 5 || img.Bounds().Dy() != 1 {
		t.Errorf("the JPEG is %v, want 5 by 1", img.Bounds())
	}
}

func TestSaveWithOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ct.png")
	// With no window asked for, the data set's own.
	if err := ctRow().SaveWithOptions(path, "png", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := grayRow(t, decodePNG(t, path)), []uint8{0, 0, 128, 255, 255}; !sameLevels(got, want) {
		t.Errorf("levels %v, want %v", got, want)
	}
	// AutoWindow applies the options' window, and the picture is scaled to fit.
	wide := picture{allocated: 16, stored: 16, photometric: "MONOCHROME2",
		px: words(0, 0, 1000, 1000, 2000, 2000, 3000, 3000)}.dataset()
	opts := &dataset.ImageExportOptions{AutoWindow: true, WindowCenter: 1000, WindowWidth: 500,
		MaxDimension: 4, PreserveAspectRatio: true}
	if err := wide.SaveWithOptions(path, "png", opts); err != nil {
		t.Fatal(err)
	}
	img := decodePNG(t, path)
	if img.Bounds().Dx() != 4 {
		t.Fatalf("the picture is %d wide, want 4", img.Bounds().Dx())
	}
	if got := grayRow(t, img); got[0] != 0 || got[3] != 255 || got[1] < 100 || got[1] > 160 {
		t.Errorf("levels %v, want black, the window's middle, white, white", got)
	}
	if err := ctRow().SaveWithOptions(path, "gif", nil); err == nil {
		t.Error("an unknown format was accepted")
	}
}
