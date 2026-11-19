package anonymize

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestMostOfAScanIsNotRedactedAutomatically checks a scan with no lettering
// on which the detector is wrong by a great deal: pydicom's
// examples_overlay.dcm, an abdominal MR, smoothed as a picture resampled by
// half a pixel is. At a quarter of its size three blobs of its anatomy line
// up like large lettering and the region grown from them takes 46.6% of
// the picture. An automatic redaction of it is refused by name, and the
// image is left as it was: no pixel blacked out, nothing recorded.
func TestMostOfAScanIsNotRedactedAutomatically(t *testing.T) {
	img := convolved(corpusPicture(t, "examples_overlay.dcm"), [][]int{{1, 1}, {1, 1}})
	ds := screenDataset(t, img)
	before := pixelBytes(t, ds)

	regions, err := NewAnonymizer(BasicProfile).RedactDetectedText(ds, nil)
	if !errors.Is(err, ErrImplausibleRedaction) {
		t.Fatalf("RedactDetectedText: %d regions, error %v; want a refusal", len(regions), err)
	}
	if !strings.Contains(err.Error(), "46.6% of a 484 by 300 picture") {
		t.Errorf("the refusal does not say how much was found: %v", err)
	}
	if len(regions) != 0 {
		t.Errorf("%d regions returned with a refusal", len(regions))
	}
	if !bytes.Equal(pixelBytes(t, ds), before) {
		t.Error("the pixels were changed by a redaction that was refused")
	}
	if ds.Contains(tagMethod) {
		t.Error("a method is recorded for an image nothing was done to")
	}
}

// TestCheckDetectedShare checks the bound itself: a pixel two regions share
// is counted once, a share of exactly a quarter is filled, more is refused,
// and a region partly off the picture counts only what is on it.
func TestCheckDetectedShare(t *testing.T) {
	cases := []struct {
		name    string
		regions []dataset.PixelRegion
		refused bool
	}{
		{"nothing", nil, false},
		{"a quarter exactly", []dataset.PixelRegion{{X: 0, Y: 0, Width: 50, Height: 50}}, false},
		{"two that overlap, a quarter between them", []dataset.PixelRegion{
			{X: 0, Y: 0, Width: 50, Height: 30}, {X: 0, Y: 20, Width: 50, Height: 30},
		}, false},
		{"one pixel over a quarter", []dataset.PixelRegion{
			{X: 0, Y: 0, Width: 50, Height: 50}, {X: 99, Y: 99, Width: 1, Height: 1},
		}, true},
		{"mostly off the picture", []dataset.PixelRegion{{X: 50, Y: 40, Width: 400, Height: 400}}, true},
		{"off the picture", []dataset.PixelRegion{{X: 100, Y: 0, Width: 400, Height: 400}}, false},
	}
	for _, c := range cases {
		err := CheckDetectedShare(c.regions, 100, 100)
		if refused := errors.Is(err, ErrImplausibleRedaction); refused != c.refused {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// pixelBytes is a copy of a data set's native pixel data.
func pixelBytes(t *testing.T, ds *dataset.Dataset) []byte {
	t.Helper()
	elem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("no pixel data")
	}
	px, _ := elem.GetValue().([]byte)
	return append([]byte(nil), px...)
}
