package anonymize

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// The detector is measured on pictures made here, where the test knows
// where every letter is: text drawn in a small font at known positions, in
// several sizes and colors, on plain backgrounds and beside and over
// texture like an ultrasound's. Two numbers come out of each picture: how
// much of each line of text the regions cover, and how much of the picture
// they cover that held no text.

// font5x7 is a 5 by 7 font, one byte for each column of a glyph with the
// top row in bit 0: the classic one small displays use.
var font5x7 = map[rune][5]byte{
	'0': {0x3E, 0x51, 0x49, 0x45, 0x3E}, '1': {0x00, 0x42, 0x7F, 0x40, 0x00},
	'2': {0x42, 0x61, 0x51, 0x49, 0x46}, '3': {0x21, 0x41, 0x45, 0x4B, 0x31},
	'4': {0x18, 0x14, 0x12, 0x7F, 0x10}, '5': {0x27, 0x45, 0x45, 0x45, 0x39},
	'6': {0x3C, 0x4A, 0x49, 0x49, 0x30}, '7': {0x01, 0x71, 0x09, 0x05, 0x03},
	'8': {0x36, 0x49, 0x49, 0x49, 0x36}, '9': {0x06, 0x49, 0x49, 0x29, 0x1E},
	'A': {0x7E, 0x11, 0x11, 0x11, 0x7E}, 'B': {0x7F, 0x49, 0x49, 0x49, 0x36},
	'C': {0x3E, 0x41, 0x41, 0x41, 0x22}, 'D': {0x7F, 0x41, 0x41, 0x22, 0x1C},
	'E': {0x7F, 0x49, 0x49, 0x49, 0x41}, 'F': {0x7F, 0x09, 0x09, 0x09, 0x01},
	'G': {0x3E, 0x41, 0x49, 0x49, 0x7A}, 'H': {0x7F, 0x08, 0x08, 0x08, 0x7F},
	'I': {0x00, 0x41, 0x7F, 0x41, 0x00}, 'J': {0x20, 0x40, 0x41, 0x3F, 0x01},
	'K': {0x7F, 0x08, 0x14, 0x22, 0x41}, 'L': {0x7F, 0x40, 0x40, 0x40, 0x40},
	'M': {0x7F, 0x02, 0x0C, 0x02, 0x7F}, 'N': {0x7F, 0x04, 0x08, 0x10, 0x7F},
	'O': {0x3E, 0x41, 0x41, 0x41, 0x3E}, 'P': {0x7F, 0x09, 0x09, 0x09, 0x06},
	'Q': {0x3E, 0x41, 0x51, 0x21, 0x5E}, 'R': {0x7F, 0x09, 0x19, 0x29, 0x46},
	'S': {0x46, 0x49, 0x49, 0x49, 0x31}, 'T': {0x01, 0x01, 0x7F, 0x01, 0x01},
	'U': {0x3F, 0x40, 0x40, 0x40, 0x3F}, 'V': {0x1F, 0x20, 0x40, 0x20, 0x1F},
	'W': {0x3F, 0x40, 0x38, 0x40, 0x3F}, 'X': {0x63, 0x14, 0x08, 0x14, 0x63},
	'Y': {0x07, 0x08, 0x70, 0x08, 0x07}, 'Z': {0x61, 0x51, 0x49, 0x45, 0x43},
	'^': {0x04, 0x02, 0x01, 0x02, 0x04}, '.': {0x00, 0x60, 0x60, 0x00, 0x00},
	'-': {0x08, 0x08, 0x08, 0x08, 0x08}, '/': {0x20, 0x10, 0x08, 0x04, 0x02},
	':': {0x00, 0x36, 0x36, 0x00, 0x00},
}

// drawText draws a line of text with its top left corner at (x, y), each
// font pixel size by size picture pixels, and returns the box it fills.
func drawText(img *image.NRGBA, text string, x, y, size int, ink color.NRGBA) image.Rectangle {
	at := x
	for _, r := range text {
		if glyph, ok := font5x7[r]; ok {
			for col := 0; col < 5; col++ {
				for row := 0; row < 7; row++ {
					if glyph[col]&(1<<uint(row)) == 0 {
						continue
					}
					for dy := 0; dy < size; dy++ {
						for dx := 0; dx < size; dx++ {
							img.SetNRGBA(at+col*size+dx, y+row*size+dy, ink)
						}
					}
				}
			}
		}
		at += 6 * size // a column of space after each glyph, and for a space
	}
	return image.Rect(x, y, at-size, y+7*size)
}

// canvas is a picture filled with one color.
func canvas(w, h int, fill color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = fill.R, fill.G, fill.B, 255
	}
	return img
}

// speckle fills a rectangle with texture like an ultrasound's: noise
// smoothed over a few pixels, so it has blobs of every size and brightness
// and no letters.
func speckle(img *image.NRGBA, area image.Rectangle, seed int64) {
	random := rand.New(rand.NewSource(seed))
	w, h := area.Dx(), area.Dy()
	noise := make([]float64, w*h)
	for i := range noise {
		// Skewed dark, as tissue is: mostly dim with bright scatterers.
		v := random.Float64()
		noise[i] = v * v * v
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sum, n := 0.0, 0
			for dy := -2; dy <= 2; dy++ {
				for dx := -3; dx <= 3; dx++ {
					if xx, yy := x+dx, y+dy; xx >= 0 && yy >= 0 && xx < w && yy < h {
						sum += noise[yy*w+xx]
						n++
					}
				}
			}
			// Stretched over the whole range, dark in most places and
			// reaching white in a few, as a scan does.
			level := max(0, min(1, (sum/float64(n)-0.15)/0.3))
			v := uint8(255 * level * level)
			img.SetNRGBA(area.Min.X+x, area.Min.Y+y, color.NRGBA{v, v, v, 255})
		}
	}
}

// maxSpare is how much of a picture the regions may cover beyond the text
// and a margin about it. What is marked and is not text is picture that a
// redaction would take, so it is bounded; it is not zero, because a region
// is a rectangle and is drawn with room to spare.
const maxSpare = 0.05

// covered is the fraction of box that lies inside any of the regions.
func covered(box image.Rectangle, regions []image.Rectangle) float64 {
	in := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			for _, r := range regions {
				if image.Pt(x, y).In(r) {
					in++
					break
				}
			}
		}
	}
	return float64(in) / float64(box.Dx()*box.Dy())
}

// spare is the fraction of the picture that the regions cover and no text
// box, grown by a margin, does.
func spare(bounds image.Rectangle, boxes, regions []image.Rectangle, margin int) float64 {
	out := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := image.Pt(x, y)
			inRegion := false
			for _, r := range regions {
				if p.In(r) {
					inRegion = true
					break
				}
			}
			if !inRegion {
				continue
			}
			inText := false
			for _, b := range boxes {
				if p.In(b.Inset(-margin)) {
					inText = true
					break
				}
			}
			if !inText {
				out++
			}
		}
	}
	return float64(out) / float64(bounds.Dx()*bounds.Dy())
}

var (
	white  = color.NRGBA{255, 255, 255, 255}
	black  = color.NRGBA{0, 0, 0, 255}
	gray   = color.NRGBA{170, 170, 170, 255}
	yellow = color.NRGBA{255, 255, 0, 255}
	navy   = color.NRGBA{20, 40, 90, 255}
	paper  = color.NRGBA{235, 235, 235, 255}
)

// screen is one test picture: what is behind the text, and the lines on it.
type screen struct {
	name       string
	background color.NRGBA
	// texture is where speckle is drawn, if anywhere.
	texture image.Rectangle
	lines   []caption
}

type caption struct {
	text string
	x, y int
	size int
	ink  color.NRGBA
}

// screens are the pictures the detector is measured on. The texts are the
// kinds a modality burns in: a name, an identifier, a date, an institution.
var screens = []screen{
	{"white captions on black", black, image.Rectangle{}, []caption{
		{"DOE^JOHN", 8, 6, 1, white}, {"ID 12345678", 8, 18, 1, white},
		{"1985/03/14", 200, 6, 1, white}, {"ST MARY HOSPITAL", 60, 220, 2, white},
	}},
	{"captions in the margins of an ultrasound", black, image.Rect(60, 40, 260, 200), []caption{
		{"DOE^JOHN", 4, 4, 1, white}, {"12345678", 4, 14, 1, white},
		{"14:32:07", 250, 4, 1, white}, {"GEN OB", 4, 60, 1, white},
		{"ST MARY HOSPITAL", 90, 222, 1, white},
	}},
	{"a caption over the texture", black, image.Rect(20, 30, 300, 220), []caption{
		{"DOE^JOHN", 100, 100, 2, white}, {"12345678", 4, 4, 1, white},
	}},
	{"gray captions", black, image.Rectangle{}, []caption{
		{"DOE^JOHN", 10, 10, 2, gray}, {"1985/03/14", 10, 40, 1, gray},
	}},
	{"yellow on a blue header", navy, image.Rectangle{}, []caption{
		{"DOE^JOHN", 10, 8, 2, yellow}, {"ID 12345678", 160, 8, 2, yellow},
	}},
	{"black on paper, as a scanned form", paper, image.Rectangle{}, []caption{
		{"PATIENT DOE^JOHN", 20, 20, 2, black}, {"BORN 1985/03/14", 20, 50, 2, black},
		{"REFERRED BY DR SMITH", 20, 200, 1, black},
	}},
	{"large lettering", black, image.Rectangle{}, []caption{
		{"DOE^JOHN", 10, 20, 5, white}, {"12345", 10, 90, 8, white},
	}},
	{"every size at once", black, image.Rectangle{}, []caption{
		{"DOE^JOHN", 6, 4, 1, white}, {"DOE^JOHN", 6, 20, 2, white},
		{"DOE^JOHN", 6, 44, 3, white}, {"DOE^JOHN", 6, 76, 4, white},
	}},
}

func (s screen) draw() (*image.NRGBA, []image.Rectangle) {
	img := canvas(320, 240, s.background)
	if !s.texture.Empty() {
		speckle(img, s.texture, 7)
	}
	var boxes []image.Rectangle
	for _, c := range s.lines {
		boxes = append(boxes, drawText(img, c.text, c.x, c.y, c.size, c.ink))
	}
	return img, boxes
}

// TestEveryCaptionIsCovered is the measure of what the detector finds: on
// each picture, every line of text has to lie inside the regions, whole,
// and the regions may take little of the picture besides.
func TestEveryCaptionIsCovered(t *testing.T) {
	detector := NewTextDetector()
	lines, found, worst := 0, 0, 0.0
	for _, s := range screens {
		img, boxes := s.draw()
		regions := detector.DetectText(img)
		for i, box := range boxes {
			lines++
			if got := covered(box, regions); got < 1 {
				t.Errorf("%s: %q is covered %.0f%%", s.name, s.lines[i].text, got*100)
			} else {
				found++
			}
		}
		// A region has a margin about its text that grows with the text.
		margin := 6
		for _, c := range s.lines {
			margin = max(margin, 4*c.size)
		}
		extra := spare(img.Bounds(), boxes, regions, margin)
		worst = max(worst, extra)
		if extra > maxSpare {
			t.Errorf("%s: %.1f%% of the picture is marked where there is no text", s.name, extra*100)
		}
	}
	t.Logf("%d of %d lines of text covered whole; at most %.1f%% of a picture marked beside them",
		found, lines, worst*100)
}

// TestPicturesWithoutTextAreLeftAlone checks what has no lettering: a blank
// picture, texture alone, a smooth gradient, and a few straight lines and
// marks, as calipers and rulers are. Little or nothing of them is marked.
func TestPicturesWithoutTextAreLeftAlone(t *testing.T) {
	detector := NewTextDetector()

	blank := canvas(320, 240, black)
	textured := canvas(320, 240, black)
	speckle(textured, image.Rect(0, 0, 320, 240), 3)
	gradient := canvas(320, 240, black)
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			v := uint8((x + y) * 255 / 560)
			gradient.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
	marks := canvas(320, 240, black)
	for x := 20; x < 300; x++ {
		marks.SetNRGBA(x, 200, white) // a ruler
		if x%20 == 0 {
			for y := 196; y < 205; y++ {
				marks.SetNRGBA(x, y, white) // its ticks
			}
		}
	}
	for d := -4; d <= 4; d++ { // two calipers
		marks.SetNRGBA(100+d, 100, white)
		marks.SetNRGBA(100, 100+d, white)
		marks.SetNRGBA(180+d, 130, white)
		marks.SetNRGBA(180, 130+d, white)
	}

	for name, img := range map[string]*image.NRGBA{
		"a blank picture": blank, "texture alone": textured, "a gradient": gradient, "a ruler and calipers": marks,
	} {
		regions := detector.DetectText(img)
		if extra := spare(img.Bounds(), nil, regions, 0); extra > maxSpare {
			t.Errorf("%s: %.1f%% of it is marked as text: %v", name, extra*100, regions)
		}
	}
}

// TestDetectTextOnNothing checks the inputs that are not pictures.
func TestDetectTextOnNothing(t *testing.T) {
	detector := NewTextDetector()
	if got := detector.DetectText(nil); got != nil {
		t.Errorf("no picture: %v", got)
	}
	if got := detector.DetectText(image.NewGray(image.Rect(0, 0, 3, 3))); len(got) != 0 {
		t.Errorf("a picture of nine pixels: %v", got)
	}
}
