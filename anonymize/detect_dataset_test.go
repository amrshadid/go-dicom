package anonymize

import (
	"encoding/binary"
	"image"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// An automatic redaction blacks out what the detector finds and says that
// it was automatic. It must not say more: CleanPixelData's claims, Burned
// In Annotation NO and the Clean Pixel Data Option, belong to someone who
// looked.

// screenDataset is a picture as a native RGB secondary capture.
func screenDataset(t *testing.T, img *image.NRGBA) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	add := func(at tag.Tag, vr dataelem.VR, value []byte) {
		if err := ds.Add(dataelem.NewDataElement(at, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	us := func(v int) []byte {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		return b
	}
	b := img.Bounds()
	add(tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.7\x00"))
	add(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.826.0.1.3680043.10.511.310"))
	add(tag.New(0x0028, 0x0002), dataelem.US, us(3))
	add(tag.New(0x0028, 0x0004), dataelem.CS, []byte("RGB "))
	add(tag.New(0x0028, 0x0006), dataelem.US, us(0))
	add(tag.New(0x0028, 0x0010), dataelem.US, us(b.Dy()))
	add(tag.New(0x0028, 0x0011), dataelem.US, us(b.Dx()))
	add(tag.New(0x0028, 0x0100), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0101), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0102), dataelem.US, us(7))
	add(tag.New(0x0028, 0x0103), dataelem.US, us(0))
	px := make([]byte, 0, b.Dx()*b.Dy()*3)
	for i := 0; i < len(img.Pix); i += 4 {
		px = append(px, img.Pix[i], img.Pix[i+1], img.Pix[i+2])
	}
	add(tag.New(0x7FE0, 0x0010), dataelem.OB, px)
	return ds
}

// brightest is the largest sample in a box of an RGB image's pixel data.
func brightest(t *testing.T, ds *dataset.Dataset, width int, box image.Rectangle) int {
	t.Helper()
	elem, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	px, _ := elem.GetValue().([]byte)
	most := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			for c := 0; c < 3; c++ {
				most = max(most, int(px[(y*width+x)*3+c]))
			}
		}
	}
	return most
}

// TestRedactDetectedTextBlacksOutWhatItFinds: the captions are black
// afterwards, the regions returned are the ones filled, and the record says
// it was automatic and nothing more.
func TestRedactDetectedTextBlacksOutWhatItFinds(t *testing.T) {
	img, boxes := screens[1].draw() // captions in the margins of an ultrasound
	ds := screenDataset(t, img)
	_ = ds.Add(dataelem.NewDataElement(tagBurnedInAnnotation, dataelem.CS, "YES "))

	regions, err := NewAnonymizer(BasicProfile).RedactDetectedText(ds, nil)
	if err != nil {
		t.Fatalf("RedactDetectedText: %v", err)
	}
	if len(regions) == 0 {
		t.Fatal("no regions were found")
	}
	for i, box := range boxes {
		if got := brightest(t, ds, img.Bounds().Dx(), box); got != 0 {
			t.Errorf("%q is still there: a sample of %d", screens[1].lines[i].text, got)
		}
	}
	// The picture is not blacked out wholesale: the texture is still there.
	if got := brightest(t, ds, img.Bounds().Dx(), image.Rect(120, 90, 200, 150)); got == 0 {
		t.Error("the middle of the picture was filled as well")
	}

	if got := textOf(ds, tagMethod); got != AutomaticRedactionNote {
		t.Errorf("De-identification Method is %q", got)
	}
	if got := textOf(ds, tagBurnedInAnnotation); got != "YES" {
		t.Errorf("Burned In Annotation is %q: a detector cannot say there is none", got)
	}
	if hasMethodCode(ds, cleanPixelDataCode) {
		t.Error("the Clean Pixel Data Option is recorded, which says a person answered for the pixels")
	}
}

// TestAutomaticNoteSurvivesAnonymize: de-identifying the attributes
// afterwards rewrites the method, and the note is still in it.
func TestAutomaticNoteSurvivesAnonymize(t *testing.T) {
	img, _ := screens[0].draw()
	ds := screenDataset(t, img)
	anonymizer := NewAnonymizer(BasicProfile)
	if _, err := anonymizer.RedactDetectedText(ds, nil); err != nil {
		t.Fatal(err)
	}
	if err := anonymizer.Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	method := textOf(ds, tagMethod)
	if !strings.Contains(method, AutomaticRedactionNote) || !strings.Contains(method, basicProfileCode.Meaning) {
		t.Errorf("De-identification Method is %q", method)
	}
	// And once is enough.
	if _, err := anonymizer.RedactDetectedText(ds, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Count(textOf(ds, tagMethod), AutomaticRedactionNote) > 1 {
		t.Error("the note is recorded twice")
	}
}

// TestRedactDetectedTextLeavesAPictureWithoutText: nothing found, nothing
// changed and nothing recorded.
func TestRedactDetectedTextLeavesAPictureWithoutText(t *testing.T) {
	ds := screenDataset(t, canvas(64, 64, black))
	regions, err := NewAnonymizer(BasicProfile).RedactDetectedText(ds, nil)
	if err != nil || len(regions) != 0 {
		t.Fatalf("%d regions, %v", len(regions), err)
	}
	if ds.Contains(tagMethod) {
		t.Error("a method is recorded for an image nothing was done to")
	}
}

// fixedDetector is a detector a caller supplies.
type fixedDetector []image.Rectangle

func (f fixedDetector) DetectText(image.Image) []image.Rectangle { return f }

// TestACallersDetectorIsUsed: the interface is what lets a program with a
// recognition engine use it here.
func TestACallersDetectorIsUsed(t *testing.T) {
	ds := screenDataset(t, canvas(64, 64, white))
	regions, err := DetectBurnedInText(ds, fixedDetector{image.Rect(4, 8, 20, 16)})
	if err != nil || len(regions) != 1 {
		t.Fatalf("%d regions, %v", len(regions), err)
	}
	if got := regions[0]; got != (dataset.PixelRegion{X: 4, Y: 8, Width: 16, Height: 8}) {
		t.Errorf("the region is %+v", got)
	}
}
