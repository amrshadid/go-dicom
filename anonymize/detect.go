package anonymize

import (
	"errors"
	"fmt"
	"image"
	"sort"

	"github.com/amrshadid/go-dicom/dataset"
)

// Finding text that is drawn into the pixels.
//
// De-identifying an image needs to know where lettering is, not what it
// says, so this is detection and not recognition: there is no OCR engine
// here and nothing to install. What comes out is rectangles, which
// Dataset.RedactPixelRegions fills.
//
// A detector can miss. Text that is faint, very small, hand written, or
// drawn over a busy part of the picture may not be found, and a name that
// is not found is a name that is still there. Nothing here is a finding
// that an image is clean: only a person looking at it is.

// TextDetector finds where lettering is in a picture.
//
// It is an interface so that a program with a better detector, or a
// recognition engine, can use its own in place of the one here.
type TextDetector interface {
	// DetectText returns rectangles that hold lettering, in the picture's
	// own coordinates. They may overlap, and may hold more than the text.
	DetectText(img image.Image) []image.Rectangle
}

// NewTextDetector returns the detector this package has.
//
// Machine-drawn text is strokes of one color set against what is behind
// them, in characters of one height standing on one line. So the detector
// marks the pixels that differ sharply from their surroundings, joins them
// into connected shapes, keeps the shapes that are the size of a character,
// and looks for runs of them that line up. It does this at several scales,
// so that lettering from a few pixels tall to a large fraction of the
// picture is tried at the size the rules are written for.
func NewTextDetector() TextDetector { return strokeDetector{} }

// DetectBurnedInText renders every frame of an image and returns the regions
// in which the detector finds lettering in any of them, ready for
// Dataset.RedactPixelRegions, which fills a region in every frame.
//
// A nil detector is the one NewTextDetector returns. No regions and no error
// means the detector found nothing, which is not the same as there being
// nothing: see the note on what a detector can miss.
func DetectBurnedInText(ds *dataset.Dataset, detector TextDetector) ([]dataset.PixelRegion, error) {
	var found []image.Rectangle
	if detector == nil {
		detector = NewTextDetector()
	}
	// A frame at a time: the detector has looked at one when the next is
	// rendered, so the frames of an image are not all pictures in memory
	// at once.
	err := ds.RenderEachFrame(nil, func(_ int, frame image.Image) error {
		found = append(found, detector.DetectText(frame)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mergedRegions(found), nil
}

// DetectBurnedInTextFrom is DetectBurnedInText for an image whose Pixel Data
// the data set does not hold: frames reads it, a frame at a time, from where
// it is kept (filereader.DICOMFile.FrameReader), and the data set, read with
// filereader.ReadOptions.SkipBulkData, says the rest. The regions are those
// DetectBurnedInText finds in the same image; memory follows a frame and not
// the image (see dataset.Dataset.RenderFramesFrom).
//
// It looks at the image as it will be written: when regions have already
// been filled in the data set (CleanPixelData), they are filled in what the
// detector sees, as they are in a data set that holds its pixels.
func DetectBurnedInTextFrom(ds *dataset.Dataset, frames dataset.FrameReader, detector TextDetector) ([]dataset.PixelRegion, error) {
	if detector == nil {
		detector = NewTextDetector()
	}
	frames, ds = ds.FramesAsWritten(frames)
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	list := make([]int, info.NumberOfFrames)
	for i := range list {
		list[i] = i
	}
	var found []image.Rectangle
	err = ds.RenderFramesFrom(frames, list, nil, func(_ int, frame image.Image) error {
		found = append(found, detector.DetectText(frame)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mergedRegions(found), nil
}

// mergedRegions joins the rectangles a detector found into regions.
func mergedRegions(found []image.Rectangle) []dataset.PixelRegion {
	regions := make([]dataset.PixelRegion, 0, len(found))
	for _, r := range mergeRectangles(found) {
		regions = append(regions, dataset.PixelRegion{X: r.Min.X, Y: r.Min.Y, Width: r.Dx(), Height: r.Dy()})
	}
	return regions
}

// MaxDetectedShare is the largest share of a picture that a detector's
// regions may cover and still be filled by an automatic redaction.
//
// It is set from what was measured, not from what text could cover. On the
// real screens of pydicom's corpus, sharp and softened, the detector marks
// at most 17.8% of a picture (an ultrasound screen with two header blocks,
// a clock, labels and a caption), and on the scans the tests draw labels
// over, at most 20.0%. Where it is wrong about a picture with no text, it is
// wrong by a great deal: on every corpus scan without text, softened five
// ways and taken at sixteen offsets, it marks either 1.6% or less, or 38.7%
// or more (an abdominal MR whose anatomy, at a quarter of its size, lines up
// like large lettering). A quarter is between the two. A picture that is
// nothing but large lettering is over it too, and is refused: a refusal
// destroys nothing, and the regions can be given by hand.
const MaxDetectedShare = 0.25

// ErrImplausibleRedaction is wrapped by the error an automatic redaction
// returns when the regions found cover more than MaxDetectedShare of the
// picture.
var ErrImplausibleRedaction = errors.New("the regions found cover more of the picture than text does")

// CheckDetectedShare returns an error wrapping ErrImplausibleRedaction when
// regions cover more than MaxDetectedShare of a picture width by height
// pixels, counting a pixel that two regions share once.
//
// It is for a caller about to fill a detector's regions without a person
// looking. Filled, a region that took in anatomy blacks out what the image
// was kept for, and nothing would say so; refused, the image stays as it
// was and the caller is told why.
func CheckDetectedShare(regions []dataset.PixelRegion, width, height int) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	covered := make([]bool, width*height)
	n := 0
	for _, r := range regions {
		box := image.Rect(r.X, r.Y, r.X+r.Width, r.Y+r.Height).Intersect(image.Rect(0, 0, width, height))
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				if i := y*width + x; !covered[i] {
					covered[i] = true
					n++
				}
			}
		}
	}
	if share := float64(n) / float64(width*height); share > MaxDetectedShare {
		return fmt.Errorf("%w: %.1f%% of a %d by %d picture, over the %.0f%% an automatic redaction fills",
			ErrImplausibleRedaction, share*100, width, height, MaxDetectedShare*100)
	}
	return nil
}

// strokeDetector is the built-in TextDetector.
type strokeDetector struct{}

// What the detector takes for a character and for a line of them, at the
// scale it is looking at. Lettering larger than these is found at a coarser
// scale, where it is this size.
const (
	// inkContrast is how far, of 255, a pixel has to stand from the mean
	// around it to be part of a stroke. Text is drawn at full contrast;
	// anatomy and noise mostly change more gently than this.
	inkContrast = 40
	// meanRadius is the half-width of the square the mean is taken over:
	// wide enough to hold a character and what is behind it.
	meanRadius = 8
	// The height of a character, in pixels at the scale looked at.
	minCharHeight = 5
	maxCharHeight = 26
	// maxScale is the coarsest scale tried: characters up to about 200
	// pixels tall.
	maxScale = 8
	// minPictureSide is the smallest side a picture has at a scale for the
	// scale to be worth looking at.
	minPictureSide = 24
)

// DetectText finds lines of lettering at each scale and returns them merged.
func (strokeDetector) DetectText(img image.Image) []image.Rectangle {
	if img == nil {
		return nil
	}
	bounds := img.Bounds()
	full := colorPlanes(img)
	var found, onGrid []image.Rectangle
	for scale := 1; scale <= maxScale; scale *= 2 {
		planes := full.reduced(scale)
		if planes.w < minPictureSide || planes.h < minPictureSide {
			break
		}
		bright, dark := inkMaps(planes)
		for pass, ink := range [][]bool{bright, dark, saturated(planes, bright)} {
			// Reduced, a picture is all blobs, and some of them line up.
			// Lettering large enough to need a coarser scale is clean and
			// regular, so there nearly every shape of a line has to be.
			overTexture := pass == 2
			strict := scale > 1 || overTexture
			characters, cells := characterShapes(ink, planes, strict)
			if !strict {
				// At the picture's own scale a character generator's grid is
				// still exact. Reduced, a pixel is an average of several and
				// the boxes of its cells are not.
				for _, line := range gridLines(cells) {
					if !onPlainGround(line, ink, planes) {
						continue
					}
					// Not grown: the cells already say where the line is, and
					// growing by color, from a line of small soft glyphs whose
					// color is allowed to vary, ran on into a color bar
					// beside a label. Widened by a cell instead: a glyph
					// narrower than the cell, a 1, a colon, a plus, has a box
					// of its own and is not in the run, and at either end of
					// it, it is in the next cell.
					pad := max(2, line.height/3)
					r := image.Rect(line.x0-line.pitch, line.y0-pad, line.x1+line.pitch, line.y1+pad)
					if r = r.Add(bounds.Min).Intersect(bounds); !r.Empty() {
						onGrid = append(onGrid, r)
					}
				}
			}
			for _, line := range textLines(characters, strict) {
				if overTexture {
					// Among the brightest pixels of a busy picture a few
					// shapes line up by chance; a word does not.
					if line.count < 4 {
						continue
					}
				} else if scale == 1 && !onPlainGround(line, ink, planes) {
					// What stands out from texture is more texture. A
					// caption drawn over it is looked for above, by its
					// brightness. Reduced, the rules are already the strict
					// ones, and the inside of a thick stroke would be taken
					// for ground that is not plain.
					continue
				}
				if !overTexture || onPlainGround(line, ink, planes) {
					// Growing a line trusts color to stop it at the edge of
					// the picture. The over-texture map holds only full
					// white, the caption's color and the brightest of the
					// texture's alike, so there it cannot: a caption over
					// saturated texture ran on across the scan and down it,
					// and the region took a third of the picture. On a plain
					// ground it still can, and large lettering needs it.
					line = extended(line, ink, planes, strict)
				}
				// Back to the picture's coordinates, with a margin: the
				// edge of a stroke is blurred over a pixel or two, and a
				// letter's edge is as readable as its middle.
				pad := max(2, line.height*scale/3)
				r := image.Rect(line.x0*scale-pad, line.y0*scale-pad, line.x1*scale+pad, line.y1*scale+pad)
				r = r.Add(bounds.Min).Intersect(bounds)
				if !r.Empty() {
					found = append(found, r)
				}
			}
		}
	}
	// A line on a grid is kept only where the rules for a character found
	// nothing. Where they found the line, their region already holds it, and
	// a grid that runs on across a double space would join two of their
	// regions into one that also takes the space between.
	for _, r := range onGrid {
		alone := true
		for _, f := range found {
			if r.Overlaps(f) {
				alone = false
				break
			}
		}
		if alone {
			found = append(found, r)
		}
	}
	return mergeRectangles(found)
}

// planes is a picture as red, green and blue samples, row by row.
type planes struct {
	w, h int
	c    [3][]uint8
}

// colorPlanes reads a picture into planes. Color is kept: lettering in one
// color on another of the same brightness is still lettering.
func colorPlanes(img image.Image) planes {
	b := img.Bounds()
	p := planes{w: b.Dx(), h: b.Dy()}
	for c := range p.c {
		p.c[c] = make([]uint8, p.w*p.h)
	}
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			p.c[0][i], p.c[1][i], p.c[2][i] = uint8(r>>8), uint8(g>>8), uint8(bl>>8)
			i++
		}
	}
	return p
}

// reduced is the picture at 1/scale of its size, each pixel the mean of the
// scale by scale pixels it stands for.
func (p planes) reduced(scale int) planes {
	if scale <= 1 {
		return p
	}
	out := planes{w: p.w / scale, h: p.h / scale}
	for c := range out.c {
		out.c[c] = make([]uint8, out.w*out.h)
		for y := 0; y < out.h; y++ {
			for x := 0; x < out.w; x++ {
				sum := 0
				for dy := 0; dy < scale; dy++ {
					row := (y*scale + dy) * p.w
					for dx := 0; dx < scale; dx++ {
						sum += int(p.c[c][row+x*scale+dx])
					}
				}
				out.c[c][y*out.w+x] = uint8(sum / (scale * scale))
			}
		}
	}
	return out
}

// inkMaps marks the pixels that stand out from what is around them: bright
// holds those lighter than their surroundings and dark those darker, taking
// for each pixel the color channel in which it stands out most.
//
// The two are kept apart because a stroke is one or the other. Marked
// together, a white letter and the dark gap beside it, which the letter's
// own brightness makes darker than the local mean, would join into one
// shape.
func inkMaps(p planes) (bright, dark []bool) {
	bright, dark = make([]bool, p.w*p.h), make([]bool, p.w*p.h)
	// Summed-area tables, so the mean over a square is four lookups.
	stride := p.w + 1
	var sums [3][]int32
	for c := range sums {
		sums[c] = make([]int32, stride*(p.h+1))
		for y := 0; y < p.h; y++ {
			row := int32(0)
			for x := 0; x < p.w; x++ {
				row += int32(p.c[c][y*p.w+x])
				sums[c][(y+1)*stride+x+1] = sums[c][y*stride+x+1] + row
			}
		}
	}
	for y := 0; y < p.h; y++ {
		y0, y1 := max(0, y-meanRadius), min(p.h, y+meanRadius+1)
		for x := 0; x < p.w; x++ {
			x0, x1 := max(0, x-meanRadius), min(p.w, x+meanRadius+1)
			n := int32((x1 - x0) * (y1 - y0))
			most := int32(0)
			for c := range sums {
				s := sums[c]
				mean := (s[y1*stride+x1] - s[y0*stride+x1] - s[y1*stride+x0] + s[y0*stride+x0]) / n
				if d := int32(p.c[c][y*p.w+x]) - mean; abs32(d) > abs32(most) {
					most = d
				}
			}
			switch {
			case most >= inkContrast:
				bright[y*p.w+x] = true
			case most <= -inkContrast:
				dark[y*p.w+x] = true
			}
		}
	}
	return bright, dark
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// shape is one connected group of ink pixels: its bounding box, which is
// exclusive at x1 and y1, how many pixels it holds, and how thick its
// strokes are.
type shape struct {
	x0, y0, x1, y1 int
	area           int
	// stroke is the typical length of an unbroken run of the shape's pixels,
	// along a row or down a column, whichever is shorter: the width of the
	// pen that drew it.
	stroke int
	// color is the mean of its pixels, and spread how far, on average, a
	// pixel is from that mean in the channel where it is farthest.
	color  [3]int
	spread int
}

func (s shape) width() int  { return s.x1 - s.x0 }
func (s shape) height() int { return s.y1 - s.y0 }

// characterShapes joins ink pixels that touch, corners included, and returns
// the shapes that could be a character: of a character's height, not much
// wider than tall, and neither a speck nor a filled block. It also returns,
// as cells, every shape with a character's box that only the tests of how a
// character is drawn turned away; gridLines decides what they are.
func characterShapes(ink []bool, p planes, strict bool) (shapes, cells []shape) {
	w, h := p.w, p.h
	seen := make([]bool, len(ink))
	var stack, pixels []int
	for start, isInk := range ink {
		if !isInk || seen[start] {
			continue
		}
		s := shape{x0: w, y0: h}
		stack = append(stack[:0], start)
		pixels = pixels[:0]
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			pixels = append(pixels, i)
			x, y := i%w, i/w
			s.area++
			s.x0, s.y0 = min(s.x0, x), min(s.y0, y)
			s.x1, s.y1 = max(s.x1, x+1), max(s.y1, y+1)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					if n := ny*w + nx; ink[n] && !seen[n] {
						seen[n] = true
						stack = append(stack, n)
					}
				}
			}
		}
		if !hasCharacterBox(s) {
			continue
		}
		s.stroke = strokeWidth(s, pixels, w)
		s.color, s.spread = colorOf(p, pixels)
		character := isCharacter(s, strict)
		if character {
			shapes = append(shapes, s)
		}
		if character || isCell(s) {
			cells = append(cells, s)
		}
	}
	return shapes, cells
}

// isCell reports whether a shape with a character's box could be a
// character that the tests of isCharacter turn away for how it is drawn:
// strokes that run together, a glyph that fills its box, color that varies.
// That is what a small font softened by blur or by smoothed edges looks like,
// and also what a blob does, so a cell is not taken for a character alone.
// gridLines takes cells only in a run that has the geometry of a character
// generator, which a blob does not.
func isCell(s shape) bool {
	w, h := s.width(), s.height()
	filled := float64(s.area) / float64(w*h)
	if s.area < 6 || (filled < 0.15 && s.area < h) {
		return false
	}
	// A block of one flat color is a block, however it is placed. A glyph
	// that fills its cell does so with ink of varying brightness: the gaps
	// between its strokes, blended, are lighter than its ground and darker
	// than its strokes.
	if filled > 0.92 && s.spread <= flatColor {
		return false
	}
	return s.spread <= colorTolerance(maxColorSpread, h, false)
}

// gridLines finds runs of cells laid out as a character generator lays out
// text: boxes of one width and one height, standing on one row, their left
// edges a whole number of cells apart. The modality draws every character
// of a fixed-pitch font in a cell of one size, so its full-height glyphs have
// identical boxes on an exact pitch however their strokes are blurred, and a
// colon or a space only skips a cell. A blob of anatomy or noise matches
// another in all four of its edges seldom; three of them on one pitch, rarely.
//
// A run is three cells or more, of one color, and not all of one area: three
// identical marks in a row, a dashed rule or a row of ticks, are a pattern,
// and the glyphs of a word differ.
func gridLines(cells []shape) []line {
	type row struct{ y0, y1, w int }
	rows := map[row][]shape{}
	for _, s := range cells {
		k := row{s.y0, s.y1, s.width()}
		rows[k] = append(rows[k], s)
	}
	var lines []line
	for k, members := range rows {
		if len(members) < 3 {
			continue
		}
		sort.Slice(members, func(i, j int) bool { return members[i].x0 < members[j].x0 })
		// The pitch is the smallest step between neighbors: a cell's width
		// and the space after it, which is less than half a character's
		// height in any font.
		pitch := 0
		for i := 1; i < len(members); i++ {
			if step := members[i].x0 - members[i-1].x0; step > 0 && (pitch == 0 || step < pitch) {
				pitch = step
			}
		}
		h := k.y1 - k.y0
		if pitch <= k.w || pitch > k.w+max(2, h/2) {
			continue
		}
		start := 0
		for i := 1; i <= len(members); i++ {
			if i < len(members) {
				// Up to two empty cells, a colon and a space, stay in the run.
				step := members[i].x0 - members[i-1].x0
				if step%pitch == 0 && step <= 3*pitch {
					continue
				}
			}
			if l, ok := gridLine(members[start:i], h); ok {
				l.pitch = pitch
				lines = append(lines, l)
			}
			start = i
		}
	}
	// In reading order, as textLines returns them, whatever order the rows
	// came out of the map in.
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].y0 != lines[j].y0 {
			return lines[i].y0 < lines[j].y0
		}
		return lines[i].x0 < lines[j].x0
	})
	return lines
}

// gridLine makes a line of a run of cells, if it is long enough, in one
// color, and not one mark repeated.
func gridLine(run []shape, h int) (line, bool) {
	if len(run) < 3 {
		return line{}, false
	}
	l := line{x0: run[0].x0, y0: run[0].y0, x1: run[len(run)-1].x1, y1: run[0].y1, height: h, count: len(run)}
	for c := range l.color {
		values := make([]int, len(run))
		for i, s := range run {
			values[i] = s.color[c]
		}
		l.color[c] = median(values)
	}
	tolerance := colorTolerance(sameColor, h, false)
	alike := true
	for _, s := range run {
		l.ink += s.area
		if s.area != run[0].area {
			alike = false
		}
		for c := range l.color {
			if d(s.color[c], l.color[c]) > tolerance {
				return line{}, false
			}
		}
	}
	return l, !alike
}

// strokeWidth measures how thick a shape's strokes are: the median length
// of its unbroken runs of pixels along rows, or down columns, whichever is
// the shorter.
//
// A letter is drawn with a pen much thinner than the letter is tall. A blob
// of the same height, which is what texture and color flow make, is as
// thick as it is tall. The median is what tells them apart: the bar of a T
// is one long run among many short ones.
func strokeWidth(s shape, pixels []int, w int) int {
	bw, bh := s.width(), s.height()
	mask := make([]bool, bw*bh)
	for _, i := range pixels {
		mask[(i/w-s.y0)*bw+(i%w-s.x0)] = true
	}
	runs := func(outer, inner int, at func(o, i int) bool) int {
		var lengths []int
		for o := 0; o < outer; o++ {
			run := 0
			for i := 0; i <= inner; i++ {
				if i < inner && at(o, i) {
					run++
					continue
				}
				if run > 0 {
					lengths = append(lengths, run)
				}
				run = 0
			}
		}
		if len(lengths) == 0 {
			return 0
		}
		return median(lengths)
	}
	across := runs(bh, bw, func(y, x int) bool { return mask[y*bw+x] })
	down := runs(bw, bh, func(x, y int) bool { return mask[y*bw+x] })
	return min(across, down)
}

// hasCharacterBox reports whether a shape's box is one a character could
// have: of a character's height, and no wider than a few letters that
// touch. A rule or a frame is wider than that.
func hasCharacterBox(s shape) bool {
	w, h := s.width(), s.height()
	return h >= minCharHeight && h <= maxCharHeight && w <= 4*h+2
}

// isCharacter reports whether a shape with a character's box is drawn like
// one: a letter, a digit or a mark of punctuation tall enough to count.
func isCharacter(s shape, strict bool) bool {
	w, h := s.width(), s.height()
	// Strokes fill part of their box: less is scattered noise, and all of
	// it is a block, which nothing but a 1 or an l is. A single slanted
	// stroke, a slash, fills little of its box and is still one stroke
	// from top to bottom: a shape is connected, so as many pixels as it is
	// tall means it runs the whole height.
	filled := float64(s.area) / float64(w*h)
	if s.area < 6 || (filled < 0.15 && s.area < h) || (filled > 0.92 && w > 2) {
		return false
	}
	// Drawn with a pen, not filled in: a third of the height is a heavy
	// bold face, and beyond it is a blob.
	if s.stroke > max(2, h/3) {
		return false
	}
	// In one color. The edge of a stroke is blended with what is behind
	// it, which is allowed for; a shape that shades from one color to
	// another is a picture of something.
	return s.spread <= colorTolerance(maxColorSpread, h, strict)
}

// colorTolerance is how much a color may vary for lettering of a height.
// Strokes a pixel wide are all edge: each pixel is part letter and part
// what is behind it, so small lettering is allowed twice the variation.
// Not at a reduced scale, where a pixel is already an average and the
// lettering looked for is large and clean.
func colorTolerance(limit, height int, strict bool) int {
	if height <= smallCharHeight && !strict {
		return 2 * limit
	}
	return limit
}

// flatColor is how little, of 255, the color of a shape varies when it was
// drawn in one color with a hard edge.
const flatColor = 12

// smallCharHeight is the height up to which strokes are a pixel wide.
const smallCharHeight = 9

// How much the color of lettering may vary, of 255: within one character,
// and from one character of a line to the next.
const (
	maxColorSpread = 48
	sameColor      = 40
)

// colorOf is the mean color of a shape's pixels and how far they spread
// from it.
func colorOf(p planes, pixels []int) (mean [3]int, spread int) {
	for _, i := range pixels {
		for c := range mean {
			mean[c] += int(p.c[c][i])
		}
	}
	for c := range mean {
		mean[c] /= len(pixels)
	}
	total := 0
	for _, i := range pixels {
		far := 0
		for c := range mean {
			d := int(p.c[c][i]) - mean[c]
			if d < 0 {
				d = -d
			}
			far = max(far, d)
		}
		total += far
	}
	return mean, total / len(pixels)
}

// line is a run of characters taken for a line of text.
type line struct {
	x0, y0, x1, y1 int
	// height is the height of a typical character in it, and color the
	// color it is written in.
	height int
	color  [3]int
	// count is how many shapes it was made of, and ink how many pixels
	// they hold.
	count, ink int
	// pitch is, for a line found on a character generator's grid, the
	// width of one cell.
	pitch int
}

// saturated marks the pixels that stand out and are also as bright as a
// pixel can be. Captions are drawn at full white far more often than a
// picture reaches it, so over texture, where everything stands out a
// little, this is the map in which a caption is still alone.
func saturated(p planes, bright []bool) []bool {
	const full = 245
	out := make([]bool, len(bright))
	for i, isBright := range bright {
		out[i] = isBright && (p.c[0][i] >= full || p.c[1][i] >= full || p.c[2][i] >= full)
	}
	return out
}

// plainGround is how far, of 255, a pixel behind a line may be from the
// mean of them and still be the ground's color: a header, a margin, paper.
const plainGround = 28

// onPlainGround reports whether what is behind a line, the pixels of its box
// that are not ink, is close to one color.
func onPlainGround(l line, ink []bool, p planes) bool {
	// The ground's color is the median of what is not ink, and the test is
	// a count: the inside of a thick stroke and the blended edge of a thin
	// one are not ink and not ground either, and a mean would be pulled
	// toward them. Most of the box near one color is a plain ground.
	var counts [3][256]int
	n := 0
	for y := l.y0; y < l.y1; y++ {
		for x := l.x0; x < l.x1; x++ {
			if i := y*p.w + x; !ink[i] {
				for c := range counts {
					counts[c][p.c[c][i]]++
				}
				n++
			}
		}
	}
	if n == 0 {
		return false
	}
	var sum [3]int
	for c := range counts {
		seen := 0
		for v, count := range counts[c] {
			if seen += count; seen*2 >= n {
				sum[c] = v
				break
			}
		}
	}
	plain := 0
	for y := l.y0; y < l.y1; y++ {
		for x := l.x0; x < l.x1; x++ {
			if i := y*p.w + x; !ink[i] {
				far := 0
				for c := range sum {
					far = max(far, d(int(p.c[c][i]), sum[c]))
				}
				if far <= plainGround {
					plain++
				}
			}
		}
	}
	return plain*3 >= n*2
}

// extended grows a line sideways over what the rules for a character leave
// out and a reader would still read: a full stop, a colon, an equals sign,
// a letter with a tail that makes it too tall, a word a little farther off
// than the rest. It takes in any ink of the line's own color that lies in
// the line's band of rows, as long as the gap to it is no more than a wide
// space, and then looks again from there.
//
// The color is what keeps it from running on into a picture: the texture
// beside a caption is seldom the caption's color.
func extended(l line, ink []bool, p planes, strict bool) line {
	tolerance := colorTolerance(sameColor, l.height, strict)
	inLine := func(x int) (top, bottom int, found bool) {
		// A tail hangs below the line and an accent stands above it.
		reach := l.height / 2
		top, bottom = l.y1, l.y0
		for y := max(0, l.y0-reach); y < min(p.h, l.y1+reach); y++ {
			i := y*p.w + x
			if !ink[i] {
				continue
			}
			same := true
			for c := range l.color {
				if d(int(p.c[c][i]), l.color[c]) > tolerance {
					same = false
				}
			}
			if same {
				top, bottom, found = min(top, y), max(bottom, y+1), true
			}
		}
		return top, bottom, found
	}
	gap := l.height + l.height/2
	// No farther than the line is long, or a few characters for a short
	// one: enough for the rest of a caption, and a bound on what is taken
	// when the color test is fooled.
	reach := max(l.x1-l.x0, 6*l.height)
	grow := func(from, step int) {
		empty, solid := 0, 0
		before := l
		for x := from; x >= 0 && x < p.w && empty <= gap && d(x, from) <= reach; x += step {
			top, bottom, found := inLine(x)
			if !found {
				empty++
				solid = 0
				before = l
				continue
			}
			empty = 0
			// Lettering has a gap after every letter. Ink in every column
			// for longer than a few letters is a rule or the edge of
			// something, in the line's color and not part of it: the line
			// ends where that began.
			if solid++; solid > 3*l.height {
				l = before
				return
			}
			l.x0, l.x1 = min(l.x0, x), max(l.x1, x+1)
			// Only what touches the line's own rows widens it.
			if top < l.y1 && bottom > l.y0 {
				l.y0, l.y1 = min(l.y0, top), max(l.y1, bottom)
			}
		}
	}
	// Within the line first, for the tails of its own letters.
	for x := l.x0; x < l.x1; x++ {
		if top, bottom, found := inLine(x); found && top < l.y1 && bottom > l.y0 {
			l.y0, l.y1 = min(l.y0, top), max(l.y1, bottom)
		}
	}
	grow(l.x1, 1)
	grow(l.x0-1, -1)
	return l
}

// textLines groups character shapes that stand side by side and returns
// the groups that have the regularity of text: several characters of about
// one height whose feet, or whose tops, are on one line.
func textLines(shapes []shape, strict bool) []line {
	sort.Slice(shapes, func(i, j int) bool { return shapes[i].x0 < shapes[j].x0 })

	// Union-find over neighbors: two shapes are neighbors when they overlap
	// in height, are of like size, and the gap between them is no wider
	// than a space between words.
	parent := make([]int, len(shapes))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i, a := range shapes {
		for j := i + 1; j < len(shapes); j++ {
			b := shapes[j]
			// Sorted by left edge: once one begins farther off than any
			// space between words, so does every one after it.
			gap := b.x0 - a.x1
			if gap > maxCharHeight*4/3 {
				break
			}
			tall := max(a.height(), b.height())
			if gap > tall+tall/3 {
				continue
			}
			short := min(a.height(), b.height())
			overlap := min(a.y1, b.y1) - max(a.y0, b.y0)
			if overlap*2 < short || tall > short*5/2 {
				continue
			}
			parent[find(i)] = find(j)
		}
	}
	groups := map[int][]shape{}
	for i, s := range shapes {
		root := find(i)
		groups[root] = append(groups[root], s)
	}

	var lines []line
	for _, group := range groups {
		if l, ok := asLine(group, strict); ok {
			lines = append(lines, l)
		}
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].y0 != lines[j].y0 {
			return lines[i].y0 < lines[j].y0
		}
		return lines[i].x0 < lines[j].x0
	})
	return lines
}

// asLine decides whether a group of neighboring shapes is a line of text.
//
// What separates text from texture is regularity. The characters of a line
// are of one height, give or take a lower case letter, and stand on one
// baseline; the blobs of ultrasound speckle or of a noisy edge are of any
// height and at any level.
func asLine(group []shape, strict bool) (line, bool) {
	n := len(group)
	if n < 2 {
		return line{}, false
	}
	heights, feet, tops := make([]int, n), make([]int, n), make([]int, n)
	l := line{x0: group[0].x0, y0: group[0].y0, x1: group[0].x1, y1: group[0].y1}
	area := 0
	for i, s := range group {
		heights[i], feet[i], tops[i] = s.height(), s.y1, s.y0
		l.x0, l.y0 = min(l.x0, s.x0), min(l.y0, s.y0)
		l.x1, l.y1 = max(l.x1, s.x1), max(l.y1, s.y1)
		area += s.area
	}
	l.height = median(heights)
	tolerance := max(1, l.height/5)

	// How many stand on the line, or hang from it.
	aligned := max(near(feet, median(feet), tolerance), near(tops, median(tops), tolerance))
	// How many are of the common height, or between two thirds and one
	// and a half of it, which takes in lower case beside capitals.
	alike := 0
	for _, h := range heights {
		if h*3 >= l.height*2 && h*2 <= l.height*3 {
			alike++
		}
	}
	// How many are in the line's color: a line of text is written in one.
	colored := 0
	common := &l.color
	for c := range common {
		values := make([]int, n)
		for i, s := range group {
			values[i] = s.color[c]
		}
		common[c] = median(values)
	}
	for _, s := range group {
		same := true
		for c := range common {
			if tolerance := colorTolerance(sameColor, l.height, strict); d(s.color[c], common[c]) > tolerance {
				same = false
			}
		}
		if same {
			colored++
		}
	}
	// Ink over the whole box: strokes with space between and around them.
	filled := float64(area) / float64((l.x1-l.x0)*(l.y1-l.y0))
	if filled < 0.06 || filled > 0.8 {
		return line{}, false
	}
	// A line is longer than it is tall.
	if l.x1-l.x0 < l.height*3/2 {
		return line{}, false
	}
	if n == 2 {
		// Two shapes are a line only when they could be nothing else: the
		// same height, on the same baseline and under the same top.
		// And drawn, not grown: each in one flat color. Two blobs of
		// texture are alike often enough; two flat ones are a label.
		ok := near(feet, feet[0], 1) == 2 && near(tops, tops[0], 1) == 2 && l.height >= 6 && colored == 2 &&
			group[0].spread <= flatColor && group[1].spread <= flatColor
		l.count, l.ink = n, area
		return l, ok && !strict
	}
	// Most of them regular: a line of text has a comma and a descender in
	// it, and texture has neither most of its blobs aligned nor alike.
	l.count, l.ink = n, area
	// A few shapes line up by chance far more easily than many do, so a
	// short run has to be regular throughout.
	if n == 3 {
		return l, aligned == n && alike == n && colored == n
	}
	if strict {
		return l, aligned*5 >= n*4 && alike*5 >= n*4 && colored == n
	}
	return l, aligned*3 >= n*2 && alike*3 >= n*2 && colored*3 >= n*2
}

// d is the distance between two values.
func d(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// near counts the values within tolerance of center.
func near(values []int, center, tolerance int) int {
	n := 0
	for _, v := range values {
		if v >= center-tolerance && v <= center+tolerance {
			n++
		}
	}
	return n
}

// median is the middle value, the upper of the two for an even count.
func median(values []int) int {
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	return sorted[len(sorted)/2]
}

// mergeRectangles joins rectangles that are mostly the same region, so that
// a line found at two scales, or in two of the maps, is named once, and
// returns them top to bottom.
//
// Rectangles that only touch or cross are left as they are. Joined, a long
// line and a short one beneath it become one box that also covers the empty
// space beside the short one, and that space may be picture.
func mergeRectangles(rects []image.Rectangle) []image.Rectangle {
	out := append([]image.Rectangle(nil), rects...)
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(out) && !merged; i++ {
			for j := i + 1; j < len(out); j++ {
				common := out[i].Intersect(out[j])
				smaller := min(out[i].Dx()*out[i].Dy(), out[j].Dx()*out[j].Dy())
				if common.Dx()*common.Dy()*5 >= smaller*3 {
					out[i] = out[i].Union(out[j])
					out = append(out[:j], out[j+1:]...)
					merged = true
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Min.Y != out[j].Min.Y {
			return out[i].Min.Y < out[j].Min.Y
		}
		return out[i].Min.X < out[j].Min.X
	})
	return out
}
