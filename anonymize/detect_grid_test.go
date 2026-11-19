package anonymize

import (
	"image"
	"image/color"
	"testing"
)

// TestAFixedPitchClockAndLabelsAreFound checks the real ultrasound screen
// of pydicom's corpus, examples_rgb_color.dcm, whose lettering is a
// fixed-pitch font of 4 by 6 glyphs with soft edges. Its clock and three of
// its four PWR labels were not found: each soft glyph alone measures as a
// heavy stroke or a filled box, and what was left of each label was two
// shapes. Found on the grid their cells stand on, they are covered whole,
// and every line found before still is, with no more of the screen marked.
func TestAFixedPitchClockAndLabelsAreFound(t *testing.T) {
	img := corpusPicture(t, "examples_rgb_color.dcm")
	// Every line of text on the screen, set by hand from the picture.
	lines := map[string]image.Rectangle{
		"BAPTIST MED CTR": image.Rect(10, 13, 82, 19), "630P630": image.Rect(10, 21, 44, 27),
		"78F78": image.Rect(10, 29, 34, 35), "44CG43": image.Rect(10, 37, 39, 43), "IM#2": image.Rect(11, 45, 29, 51),
		"3cm3cm M12L": image.Rect(250, 13, 309, 20), "MSCSKEL": image.Rect(275, 20, 309, 27),
		"CINE 0118": image.Rect(265, 29, 309, 35), "22G22": image.Rect(285, 37, 309, 43), "78DR78": image.Rect(280, 45, 309, 51),
		"+2:09:04":      image.Rect(141, 19, 179, 27),
		"PWR, left top": image.Rect(5, 69, 19, 75), "PWR, left bottom": image.Rect(5, 109, 19, 115),
		"PWR, right top": image.Rect(300, 69, 314, 75),
		"LYMPH":          image.Rect(125, 181, 149, 187), "NODE": image.Rect(160, 181, 179, 187),
		"TIS<0.4 MI=0.8 AO=100%": image.Rect(200, 229, 309, 235),
	}
	// The fourth label touches the color bar above it, so its letters are
	// one shape with the bar and it is still not found.
	missed := image.Rect(296, 108, 314, 115)

	regions := NewTextDetector().DetectText(img)
	var boxes []image.Rectangle
	for text, box := range lines {
		boxes = append(boxes, box)
		if got := covered(box, regions); got < 1 {
			t.Errorf("%q is covered %.0f%%", text, got*100)
		}
	}
	t.Logf("PWR, right bottom: covered %.0f%%", covered(missed, regions)*100)
	boxes = append(boxes, missed)
	// 1.8% before the grid was looked for, all of it the margins of the
	// header blocks.
	if extra := spare(img.Bounds(), boxes, regions, 8); extra > 0.02 {
		t.Errorf("%.1f%% of the screen is marked where there is no text", extra*100)
	}
}

// TestMarksOnAGridAreNotText checks what has a grid's geometry and is not
// lettering: a row of soft dots of one size on one pitch, as a scale or a
// set of indicators is drawn. Each dot is a cell, and they are all alike,
// which the glyphs of a word are not.
func TestMarksOnAGridAreNotText(t *testing.T) {
	img := canvas(320, 240, black)
	for i := 0; i < 8; i++ {
		x0 := 40 + 7*i
		for y := 0; y < 7; y++ {
			for x := 0; x < 5; x++ {
				// Brightest in the middle and falling off to the edge.
				v := uint8(255 - 30*(max(d(2*x, 4), d(2*y, 6))))
				img.SetNRGBA(x0+x, 100+y, color.NRGBA{v, v, v, 255})
			}
		}
	}
	if regions := NewTextDetector().DetectText(img); len(regions) != 0 {
		t.Errorf("a row of dots is marked as text: %v", regions)
	}
}
