package anonymize

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// The pictures the detector's rules were written against are the ones in
// detect_test.go and detect_misses_test.go: one 5 by 7 font on drawn
// canvases. A rule that fits those can fit them and nothing else. The set
// here is drawn differently on purpose, so that it measures what the rules
// do on lettering they were not shaped by: a second font, 3 by 5, at three
// sizes; four kinds of blur, three of them not used elsewhere; and real
// scans from pydicom's corpus behind the text in place of drawn speckle.
// It needs the corpus (GODICOM_PYDICOM_DATA) and is skipped without it.

// font3x5 is a 3 by 5 font, each glyph five rows of three pixels, top row
// first, '#' for ink.
var font3x5 = map[rune]string{
	'0': "###|#.#|#.#|#.#|###", '1': ".#.|##.|.#.|.#.|###", '2': "###|..#|###|#..|###",
	'3': "###|..#|.##|..#|###", '4': "#.#|#.#|###|..#|..#", '5': "###|#..|###|..#|###",
	'6': "###|#..|###|#.#|###", '7': "###|..#|.#.|.#.|.#.", '8': "###|#.#|###|#.#|###",
	'9': "###|#.#|###|..#|###", 'A': ".#.|#.#|###|#.#|#.#", 'B': "##.|#.#|##.|#.#|##.",
	'C': ".##|#..|#..|#..|.##", 'D': "##.|#.#|#.#|#.#|##.", 'E': "###|#..|##.|#..|###",
	'F': "###|#..|##.|#..|#..", 'G': ".##|#..|#.#|#.#|.##", 'H': "#.#|#.#|###|#.#|#.#",
	'I': "###|.#.|.#.|.#.|###", 'J': "..#|..#|..#|#.#|.#.", 'K': "#.#|#.#|##.|#.#|#.#",
	'L': "#..|#..|#..|#..|###", 'M': "#.#|###|###|#.#|#.#", 'N': "##.|#.#|#.#|#.#|#.#",
	'O': ".#.|#.#|#.#|#.#|.#.", 'P': "##.|#.#|##.|#..|#..", 'R': "##.|#.#|##.|#.#|#.#",
	'S': ".##|#..|.#.|..#|##.", 'T': "###|.#.|.#.|.#.|.#.", 'U': "#.#|#.#|#.#|#.#|###",
	'V': "#.#|#.#|#.#|#.#|.#.", 'W': "#.#|#.#|###|###|#.#", 'Y': "#.#|#.#|.#.|.#.|.#.",
	':': "...|.#.|...|.#.|...", '/': "..#|..#|.#.|#..|#..", '-': "...|...|###|...|...",
	'^': ".#.|#.#|...|...|...", '.': "...|...|...|...|.#.",
}

// drawSmallText draws a line in font3x5 with its top left corner at (x, y),
// each font pixel size by size picture pixels and a column of space between
// glyphs, and returns the box it fills.
func drawSmallText(img *image.NRGBA, text string, x, y, size int, ink color.NRGBA) image.Rectangle {
	at := x
	for _, r := range text {
		if glyph, ok := font3x5[r]; ok {
			for row, bits := range strings.Split(glyph, "|") {
				for col, bit := range bits {
					if bit != '#' {
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
		at += 4 * size
	}
	return image.Rect(x, y, at-size, y+5*size)
}

// convolved is a picture smoothed by a kernel of weights, given row by row.
func convolved(img *image.NRGBA, kernel [][]int) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	cy, cx := len(kernel)/2, len(kernel[0])/2
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var sum [3]int
			total := 0
			for ky, row := range kernel {
				for kx, k := range row {
					p := image.Pt(x+kx-cx, y+ky-cy)
					if !p.In(b) {
						continue
					}
					c := img.NRGBAAt(p.X, p.Y)
					sum[0], sum[1], sum[2] = sum[0]+k*int(c.R), sum[1]+k*int(c.G), sum[2]+k*int(c.B)
					total += k
				}
			}
			out.SetNRGBA(x, y, color.NRGBA{uint8(sum[0] / total), uint8(sum[1] / total), uint8(sum[2] / total), 255})
		}
	}
	return out
}

// unseenBlurs are the ways a screen is softened before it is stored: not at
// all, by the three by three box the known misses use, by a binomial kernel
// as a lens or a video scaler does, and by a two by two box as a picture
// resampled by half a pixel is.
var unseenBlurs = []struct {
	name   string
	kernel [][]int
}{
	{"sharp", nil},
	{"box 3x3", [][]int{{1, 1, 1}, {1, 1, 1}, {1, 1, 1}}},
	{"binomial", [][]int{{1, 2, 1}, {2, 4, 2}, {1, 2, 1}}},
	{"box 2x2", [][]int{{1, 1}, {1, 1}}},
}

// unseenScans are corpus files with no lettering in them, used as what is
// behind the text.
var unseenScans = []string{
	"693_J2KI.dcm", "liver_1frame.dcm", "image_dfl.dcm", "examples_overlay.dcm", "SC_rgb_jpeg.dcm", "CT_small.dcm",
}

// unseenLabel is a line drawn on a scan, placed by a fraction of the
// picture's size so it lands in a corner on any of them.
type unseenLabel struct {
	text   string
	fx, fy float64
	size   int
	ink    color.NRGBA
}

var unseenLabels = []unseenLabel{
	{"PWR", 0.02, 0.03, 1, color.NRGBA{200, 200, 200, 255}},
	{"GE", 0.90, 0.03, 1, color.NRGBA{200, 200, 200, 255}},
	{"R5", 0.02, 0.92, 1, color.NRGBA{255, 255, 255, 255}},
	{"12:47:09", 0.40, 0.03, 1, color.NRGBA{255, 255, 255, 255}},
	{"DOE^JANE", 0.62, 0.90, 2, color.NRGBA{255, 255, 0, 255}},
	{"ID 40981", 0.30, 0.50, 2, color.NRGBA{255, 255, 255, 255}},
	{"LAT 3", 0.04, 0.70, 3, color.NRGBA{230, 230, 230, 255}},
}

// corpusPicture is the first frame of a file of pydicom's corpus, rendered
// as the detector sees it, with its corner at (0, 0). The test is skipped
// without the corpus.
func corpusPicture(t *testing.T, name string) *image.NRGBA {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	_ = f.Close()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	frame, err := df.GetDataset().RenderFrame(0, nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b := frame.Bounds()
	img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			img.Set(x, y, frame.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return img
}

// unseenScreen is one picture of the set: a scan, softened one way, with or
// without the labels on it.
type unseenScreen struct {
	name  string
	img   *image.NRGBA
	boxes []image.Rectangle
	texts []string
}

func unseenScreens(t *testing.T, withText bool) []unseenScreen {
	t.Helper()
	var out []unseenScreen
	for _, name := range unseenScans {
		base := corpusPicture(t, name)
		b := base.Bounds()
		for _, blur := range unseenBlurs {
			img := image.NewNRGBA(base.Rect)
			copy(img.Pix, base.Pix)
			s := unseenScreen{name: name + ", " + blur.name}
			if withText {
				for _, l := range unseenLabels {
					x, y := int(l.fx*float64(b.Dx())), int(l.fy*float64(b.Dy()))
					n := len([]rune(l.text))
					if !image.Rect(x, y, x+(4*n-1)*l.size, y+5*l.size).In(img.Rect) {
						continue // a small scan holds only some of them
					}
					box := drawSmallText(img, l.text, x, y, l.size, l.ink)
					s.boxes, s.texts = append(s.boxes, box), append(s.texts, l.text)
				}
			}
			if blur.kernel != nil {
				img = convolved(img, blur.kernel)
			}
			s.img = img
			out = append(out, s)
		}
	}
	return out
}

// TestUnseenLettering measures the detector on the set above: how many of
// the lines it covers whole, and how much of each scan it marks beside
// them; then the same scans, softened the same ways, with no lettering, of
// which little may be marked. The floor is what the detector found when
// the set was made; finding more is welcome and should raise it.
func TestUnseenLettering(t *testing.T) {
	detector := NewTextDetector()
	found, lines := 0, 0
	worst, total := 0.0, 0.0
	byBlur := map[string][2]int{}
	screens := unseenScreens(t, true)
	for _, s := range screens {
		regions := detector.DetectText(s.img)
		blur := s.name[strings.Index(s.name, ", ")+2:]
		counts := byBlur[blur]
		for i, box := range s.boxes {
			lines++
			counts[1]++
			if covered(box, regions) >= 1 {
				found++
				counts[0]++
			} else {
				t.Logf("%s: %q not covered whole", s.name, s.texts[i])
			}
		}
		byBlur[blur] = counts
		extra := spare(s.img.Bounds(), s.boxes, regions, 8)
		total += extra
		if s.name == markedAsLettering {
			t.Logf("%s: %.2f%% marked beside the text (#536)", s.name, extra*100)
			continue
		}
		worst = max(worst, extra)
		if extra > maxSpare {
			t.Errorf("%s: %.1f%% of the scan is marked where there is no text", s.name, extra*100)
		}
	}
	for _, blur := range unseenBlurs {
		t.Logf("%s: %d of %d lines covered whole", blur.name, byBlur[blur.name][0], byBlur[blur.name][1])
	}
	t.Logf("unseen lettering: %d of %d lines covered whole; %.2f%% of a scan marked beside them on average, %.2f%% at most but for #536",
		found, lines, 100*total/float64(len(screens)), worst*100)
	if found < unseenFound {
		t.Errorf("%d of the unseen lines are found; %d were", found, unseenFound)
	}

	worst, total = 0.0, 0.0
	clean := unseenScreens(t, false)
	for _, s := range clean {
		extra := spare(s.img.Bounds(), nil, detector.DetectText(s.img), 0)
		total += extra
		if s.name == markedAsLettering {
			t.Logf("%s, no lettering: %.2f%% marked (#536)", s.name, extra*100)
			continue
		}
		worst = max(worst, extra)
		if extra > maxSpare {
			t.Errorf("%s, no lettering: %.1f%% of it is marked", s.name, extra*100)
		}
	}
	t.Logf("the same scans without lettering: %.2f%% marked on average, %.2f%% at most but for #536",
		100*total/float64(len(clean)), worst*100)
}

// unseenFound is how many lines of the unseen set the detector covers, of
// 164: most of what is missed is lettering of the 3 by 5 font at its own
// size, five pixels tall, which blur runs together, and lettering drawn over
// the middle of a scan.
const unseenFound = 51

// markedAsLettering is the one picture of the set on which the detector
// marks a large part of a scan with no text in it, at a reduced scale: three
// blobs of anatomy taken for large lettering and grown over the scan (#536).
// It is reported and left out of the bound: no rule of the detector was found
// that tells those blobs from large lettering at every offset of the
// reduction without losing lettering. An automatic redaction of it is
// refused instead (TestMostOfAScanIsNotRedactedAutomatically).
const markedAsLettering = "examples_overlay.dcm, box 2x2"
