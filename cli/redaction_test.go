package cli

import (
	"encoding/binary"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// -redact auto, -detect and share -preview put a detector's guess where a
// person's rectangles were. What these tests hold is that the guess is used
// and is never passed off as more: the text goes, the image is not called
// clean, and every report of it says it can miss.

// The caption written into the test image, a 5 by 7 glyph to a letter with
// the top row in bit 0 of each column.
var captionGlyphs = [][5]byte{
	{0x7F, 0x08, 0x08, 0x08, 0x7F}, // H
	{0x7F, 0x49, 0x49, 0x49, 0x41}, // E
	{0x7F, 0x40, 0x40, 0x40, 0x40}, // L
	{0x7F, 0x40, 0x40, 0x40, 0x40}, // L
	{0x3E, 0x41, 0x41, 0x41, 0x3E}, // O
	{0x7F, 0x09, 0x09, 0x09, 0x01}, // F
	{0x7F, 0x08, 0x14, 0x22, 0x41}, // K
}

const (
	captionedSize = 96
	captionX      = 8
	captionY      = 10
	captionScale  = 2
)

// writeCaptionedInstance writes a 96 by 96 ultrasound whose top margin
// holds a caption in white, 14 pixels tall, with a gray square below it for
// the picture itself.
func writeCaptionedInstance(t *testing.T, path string) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	add := func(tg tag.Tag, vr dataelem.VR, value interface{}) {
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	us := func(v uint16) []byte {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		return b
	}
	add(tagSOPClassUID, dataelem.UI, "1.2.840.10008.5.1.4.1.1.6.1")
	add(tagSOPInstanceUID, dataelem.UI, "1.2.826.0.1.3680043.10.511.310.1")
	add(tag.New(0x0008, 0x0060), dataelem.CS, "US")
	add(tagPatientName, dataelem.PN, "Doe^John")
	add(tagStudyUID, dataelem.UI, testStudyUID)
	add(tag.New(0x0028, 0x0002), dataelem.US, us(1))
	add(tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2")
	add(tag.New(0x0028, 0x0010), dataelem.US, us(captionedSize))
	add(tag.New(0x0028, 0x0011), dataelem.US, us(captionedSize))
	add(tag.New(0x0028, 0x0100), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0101), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0102), dataelem.US, us(7))
	add(tag.New(0x0028, 0x0103), dataelem.US, us(0))

	px := make([]byte, captionedSize*captionedSize)
	for y := 40; y < 80; y++ {
		for x := 20; x < 76; x++ {
			px[y*captionedSize+x] = 90
		}
	}
	for g, glyph := range captionGlyphs {
		for col := 0; col < 5; col++ {
			for row := 0; row < 7; row++ {
				if glyph[col]&(1<<uint(row)) == 0 {
					continue
				}
				for dy := 0; dy < captionScale; dy++ {
					for dx := 0; dx < captionScale; dx++ {
						x := captionX + (g*6+col)*captionScale + dx
						y := captionY + row*captionScale + dy
						px[y*captionedSize+x] = 255
					}
				}
			}
		}
	}
	add(tag.New(0x7FE0, 0x0010), dataelem.OB, px)
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.6.1", "1.2.826.0.1.3680043.10.511.310.1", ds); err != nil {
		t.Fatal(err)
	}
}

// captured runs fn and returns what it printed.
func captured(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

// captionBrightest is the brightest sample in the caption's rows and in
// the picture below it.
func captionBrightest(px []byte) (caption, picture byte) {
	for y := captionY; y < captionY+7*captionScale; y++ {
		for x := 0; x < captionedSize; x++ {
			caption = max(caption, px[y*captionedSize+x])
		}
	}
	for y := 40; y < 80; y++ {
		for x := 20; x < 76; x++ {
			picture = max(picture, px[y*captionedSize+x])
		}
	}
	return caption, picture
}

// TestAnonymizeRedactAuto: the caption is gone, the picture is not, the
// record says the redaction was automatic, and Burned In Annotation does
// not say NO.
func TestAnonymizeRedactAuto(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "us.dcm"), filepath.Join(dir, "out.dcm")
	writeCaptionedInstance(t, in)

	printed, err := captured(t, func() error { return runAnonymize("-redact", "auto", in, out) })
	if err != nil {
		t.Fatalf("anonymize -redact auto: %v", err)
	}
	if !strings.Contains(printed, "detected and filled") {
		t.Errorf("the command does not say what it filled:\n%s", printed)
	}

	ds, err := readImageDataset(out)
	if err != nil {
		t.Fatal(err)
	}
	elem, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	caption, picture := captionBrightest(elem.GetValue().([]byte))
	if caption != 0 {
		t.Errorf("the caption is still there: a sample of %d", caption)
	}
	if picture != 90 {
		t.Errorf("the picture below the caption is %d, was 90", picture)
	}
	if method := datasetText(ds, tag.New(0x0012, 0x0063)); !strings.Contains(method, anonymize.AutomaticRedactionNote) {
		t.Errorf("De-identification Method is %q", method)
	}
	if got := datasetText(ds, tag.New(0x0028, 0x0301)); got == "NO" {
		t.Error("Burned In Annotation is NO after a redaction nobody verified")
	}
}

// TestDetectPrintsRegionsAndWritesNothing: -detect names a region over the
// caption in the form -redact takes, for both commands, and leaves no file.
func TestDetectPrintsRegionsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "us.dcm")
	writeCaptionedInstance(t, in)

	for name, run := range map[string]func(...string) error{"anonymize": runAnonymize, "share": runShare} {
		printed, err := captured(t, func() error { return run("-detect", in) })
		if err != nil {
			t.Fatalf("%s -detect: %v", name, err)
		}
		var region redaction
		lines := strings.Fields(printed)
		if len(lines) == 0 || region.Set(lines[0]) != nil {
			t.Fatalf("%s -detect printed %q, not a region", name, printed)
		}
		r := region.regions[0]
		if r.X > captionX || r.Y > captionY || r.X+r.Width < captionX+len(captionGlyphs)*6*captionScale-captionScale ||
			r.Y+r.Height < captionY+7*captionScale {
			t.Errorf("%s -detect: %+v does not cover the caption", name, r)
		}
		if err := run("-detect", in, filepath.Join(dir, "out")); err == nil {
			t.Errorf("%s -detect took an output", name)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("-detect left %d files in the directory", len(entries))
	}
}

// TestShareRedactAutoAndPreview: auto fills the caption and says a detector
// can miss; preview fills nothing, outlines the caption and says it is not
// for handing on; and the two are not given together.
func TestShareRedactAutoAndPreview(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "us.dcm")
	writeCaptionedInstance(t, in)
	read := func(path string) []byte {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		img, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		px := make([]byte, 0, captionedSize*captionedSize)
		for y := 0; y < captionedSize; y++ {
			for x := 0; x < captionedSize; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				// The outline is magenta: bright in red and blue, not green.
				px = append(px, byte(min(r, g, b)>>8))
			}
		}
		return px
	}

	auto := filepath.Join(dir, "auto.png")
	printed, err := captured(t, func() error { return runShare("-redact", "auto", in, auto) })
	if err != nil {
		t.Fatalf("share -redact auto: %v", err)
	}
	if !strings.Contains(printed, "can miss") {
		t.Errorf("share -redact auto does not say a detector can miss:\n%s", printed)
	}
	if caption, picture := captionBrightest(read(auto)); caption != 0 || picture == 0 {
		t.Errorf("after auto the caption is %d and the picture %d", caption, picture)
	}

	preview := filepath.Join(dir, "preview.png")
	printed, err = captured(t, func() error { return runShare("-preview", in, preview) })
	if err != nil {
		t.Fatalf("share -preview: %v", err)
	}
	if !strings.Contains(printed, "Do not hand it on") {
		t.Errorf("share -preview does not say what it is:\n%s", printed)
	}
	if caption, _ := captionBrightest(read(preview)); caption == 0 {
		t.Error("the preview has the caption filled")
	}

	if err := runShare("-preview", "-redact", "auto", in, filepath.Join(dir, "both.png")); err == nil {
		t.Error("-preview and -redact were taken together")
	}
}

// TestRedactTakesAutoAndRegions: the flag's two forms, and what it refuses.
func TestRedactTakesAutoAndRegions(t *testing.T) {
	var r redaction
	for _, value := range []string{"auto", "AUTO", "1,2,3,4", " 5, 6, 7, 8 "} {
		if err := r.Set(value); err != nil {
			t.Errorf("-redact %q: %v", value, err)
		}
	}
	if !r.auto || len(r.regions) != 2 || !r.asked() {
		t.Errorf("the flag holds %+v", r)
	}
	for _, bad := range []string{"", "automatic", "1,2,3", "1,2,0,4", "a,b,c,d"} {
		var fresh redaction
		if err := fresh.Set(bad); err == nil {
			t.Errorf("-redact %q was accepted", bad)
		}
	}
}
