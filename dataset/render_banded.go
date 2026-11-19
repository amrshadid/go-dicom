package dataset

import (
	"image"
	"image/color"
)

// bandedPicture is a frame rendered from a FrameReader a band of rows at a
// time, as its rows are asked for, so memory holds one band of samples and
// one band of picture and not the frame (#533).
//
// image/png asks for rows in order, so each band is read once. Asking for a
// row of a band no longer held reads that band again: correct, and slow.
// It is neither an *image.Gray nor an *image.NRGBA, so an encoder takes its
// general path, through At; image/png writes the same bytes there, since a
// row's bytes and the filter chosen for it are the same. image/jpeg does
// not for a grayscale picture: it writes one component only for an
// *image.Gray.
type bandedPicture struct {
	r     *renderer
	frame int
	paint painter
	// pix holds rows y0 to y1 of the picture; y0 is -1 before any is drawn.
	pix    []byte
	y0, y1 int
	// err is the first band that could not be read. Its rows, and every row
	// after, are black, and RenderFramesFrom returns it.
	err error
}

// ColorModel is that of the picture RenderFrame makes of the same frame.
func (b *bandedPicture) ColorModel() color.Model {
	if b.paint.gray {
		return color.GrayModel
	}
	return color.NRGBAModel
}

// Bounds is the frame, with its origin at 0,0.
func (b *bandedPicture) Bounds() image.Rectangle {
	return image.Rect(0, 0, b.r.info.Columns, b.r.info.Rows)
}

// Opaque reports that every pixel is opaque, as every rendered pixel is.
// image/png asks, and without it reads the whole picture once to find out.
func (b *bandedPicture) Opaque() bool {
	return true
}

// At returns the pixel at x, y, drawing its band first when it is not the
// one held. Outside the frame it is the zero color, as for an *image.Gray
// or an *image.NRGBA.
func (b *bandedPicture) At(x, y int) color.Color {
	if !image.Pt(x, y).In(b.Bounds()) {
		if b.paint.gray {
			return color.Gray{}
		}
		return color.NRGBA{}
	}
	if y < b.y0 || y >= b.y1 {
		b.draw(y)
	}
	o := (y-b.y0)*b.r.info.Columns + x
	if b.paint.gray {
		return color.Gray{Y: b.pix[o]}
	}
	o *= 4
	return color.NRGBA{R: b.pix[o], G: b.pix[o+1], B: b.pix[o+2], A: b.pix[o+3]}
}

// draw renders the band that holds row y into pix.
func (b *bandedPicture) draw(y int) {
	rows := b.r.bandRows
	b.y0 = y / rows * rows
	b.y1 = min(b.y0+rows, b.r.info.Rows)
	size := 4
	if b.paint.gray {
		size = 1
	}
	need := (b.y1 - b.y0) * b.r.info.Columns * size
	if cap(b.pix) < need {
		b.pix = make([]byte, need)
	}
	b.pix = b.pix[:need]
	if b.err == nil {
		b.err = b.r.paintRows(b.frame, b.paint, b.y0, b.y1, b.pix)
	}
	if b.err != nil {
		clear(b.pix)
	}
}
