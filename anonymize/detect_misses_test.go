package anonymize

import (
	"image"
	"image/color"
	"testing"
)

// What the detector was known to miss when it was built (#310), drawn here so
// that it is measured rather than remembered: the blurred two- and
// three-character labels and the clock of a real ultrasound screen
// (examples_rgb_color.dcm in pydicom's corpus), a caption over texture that
// is itself saturated, and dark lettering on a light, textured ground.
//
// Measured, the four are two different things. The blurred labels and the
// clock are not found: blurred, the strokes of neighboring characters run
// together and what is left is too few shapes, too unlike, to be a line. The
// caption over saturated texture and the dark lettering are found, and the
// first was found too generously: its region grew across and down the scan
// until it held a third of the picture.
//
// The real screen's clock and labels are now found (#535), on the grid of
// cells a fixed-pitch font stands on: detect_grid_test.go. The ones drawn
// here are not, and the grid cannot reach them: this font has one pixel
// between glyphs, so a three by three blur joins neighbors into one shape
// ("GE" is one, the "09" of the clock is one) and no box of a single glyph
// is left to stand on a grid.
//
// TestKnownMisses holds the line: a change to the detector may find more of
// these, and may not find fewer, or mark more of the picture beside them.

// blurred is a picture smoothed over three by three pixels, as a screen
// captured through video is: a stroke one pixel wide becomes three pixels of
// falling brightness, and a small character's strokes run together.
func blurred(img *image.NRGBA) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var sum [3]int
			n := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if p := image.Pt(x+dx, y+dy); p.In(b) {
						c := img.NRGBAAt(p.X, p.Y)
						sum[0], sum[1], sum[2] = sum[0]+int(c.R), sum[1]+int(c.G), sum[2]+int(c.B)
						n++
					}
				}
			}
			out.SetNRGBA(x, y, color.NRGBA{uint8(sum[0] / n), uint8(sum[1] / n), uint8(sum[2] / n), 255})
		}
	}
	return out
}

// brightened stretches a rectangle's brightness so that much of it is at
// full white: texture as saturated as a caption drawn over it.
func brightened(img *image.NRGBA, area image.Rectangle) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			v := uint8(min(255, int(c.R)*3))
			img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
}

// lightened makes a rectangle of texture a light ground: inverted and kept
// in the upper half of the range, as paper or a bright scan is.
func lightened(img *image.NRGBA, area image.Rectangle) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			v := uint8(255 - int(c.R))
			img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
}

// hardScreen is a picture of a known miss and the lines on it.
type hardScreen struct {
	name string
	draw func() (*image.NRGBA, []image.Rectangle, []string)
}

var hardScreens = []hardScreen{
	{"short blurred labels beside a scan", func() (*image.NRGBA, []image.Rectangle, []string) {
		img := canvas(320, 240, black)
		speckle(img, image.Rect(40, 50, 280, 200), 11)
		dim := color.NRGBA{200, 200, 200, 255}
		texts := []string{"PWR", "PWR", "PWR", "GE", "B1"}
		boxes := []image.Rectangle{
			drawText(img, "PWR", 4, 60, 1, dim), drawText(img, "PWR", 4, 120, 1, dim),
			drawText(img, "PWR", 290, 60, 1, dim), drawText(img, "GE", 150, 6, 1, dim),
			drawText(img, "B1", 4, 210, 1, dim),
		}
		return blurred(img), boxes, texts
	}},
	{"a blurred clock", func() (*image.NRGBA, []image.Rectangle, []string) {
		img := canvas(320, 240, black)
		speckle(img, image.Rect(40, 50, 280, 200), 12)
		boxes := []image.Rectangle{drawText(img, "2:09:04", 130, 20, 1, white), drawText(img, "14:32", 250, 220, 1, white)}
		return blurred(img), boxes, []string{"2:09:04", "14:32"}
	}},
	{"a caption over saturated texture", func() (*image.NRGBA, []image.Rectangle, []string) {
		img := canvas(320, 240, black)
		area := image.Rect(20, 30, 300, 220)
		speckle(img, area, 13)
		brightened(img, area)
		boxes := []image.Rectangle{drawText(img, "DOE^JOHN", 100, 100, 2, white), drawText(img, "12345678", 60, 160, 1, white)}
		return img, boxes, []string{"DOE^JOHN", "12345678"}
	}},
	{"dark text on a textured ground", func() (*image.NRGBA, []image.Rectangle, []string) {
		img := canvas(320, 240, black)
		area := image.Rect(0, 0, 320, 240)
		speckle(img, area, 14)
		lightened(img, area)
		boxes := []image.Rectangle{drawText(img, "DOE^JOHN", 100, 100, 2, black), drawText(img, "12345678", 60, 160, 1, black)}
		return img, boxes, []string{"DOE^JOHN", "12345678"}
	}},
}

// measure is what the detector does on a set of pictures: how many lines of
// text it covers whole, of how many, and the largest share of one picture it
// marks beside them.
type measure struct {
	found, lines int
	worstSpare   float64
}

func measureScreens(t *testing.T, detector TextDetector, draw []func() (*image.NRGBA, []image.Rectangle, []string), names []string) measure {
	t.Helper()
	var m measure
	for i, f := range draw {
		img, boxes, texts := f()
		regions := detector.DetectText(img)
		for j, box := range boxes {
			m.lines++
			if covered(box, regions) >= 1 {
				m.found++
			} else {
				t.Logf("%s: %q not covered whole", names[i], texts[j])
			}
		}
		extra := spare(img.Bounds(), boxes, regions, 8)
		t.Logf("%s: %.1f%% marked beside the text", names[i], extra*100)
		m.worstSpare = max(m.worstSpare, extra)
	}
	return m
}

// TestKnownMisses measures the detector on the pictures of its known misses.
// The floor is what it found when this test was written; finding more is
// welcome and should raise it.
func TestKnownMisses(t *testing.T) {
	var draw []func() (*image.NRGBA, []image.Rectangle, []string)
	var names []string
	for _, s := range hardScreens {
		draw, names = append(draw, s.draw), append(names, s.name)
	}
	m := measureScreens(t, NewTextDetector(), draw, names)
	t.Logf("known misses: %d of %d lines covered whole; at most %.1f%% of a picture marked beside them",
		m.found, m.lines, m.worstSpare*100)
	if m.found < knownMissesFound {
		t.Errorf("%d of the known misses are found; %d were", m.found, knownMissesFound)
	}
	if m.worstSpare > maxSpare {
		t.Errorf("%.1f%% of a picture is marked where there is no text", m.worstSpare*100)
	}
}

// knownMissesFound is how many lines of hardScreens the detector covers: the
// two over saturated texture and the two dark ones, and none of the seven
// blurred.
const knownMissesFound = 4
