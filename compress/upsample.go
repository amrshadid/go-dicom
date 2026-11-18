package compress

import "image"

// Chroma upsampling for JPEG frames whose color components are subsampled.
//
// T.81 A.2 leaves how a decoder brings a subsampled component back to full
// resolution to the decoder. Repeating each sample is one answer, and the
// image is no less right for it, but it is not what the tools a decoded image
// is compared with do: libjpeg, which dcmtk embeds and which pydicom reaches
// through Pillow (libjpeg-turbo), uses its "fancy upsampling" by default, a
// triangle filter between neighboring chroma samples. Repetition differed
// from them by up to 43 levels at a color edge in pydicom's corpus.
//
// This is that filter, for the two layouts libjpeg filters: twice as wide
// (h2v1, 4:2:2) and twice as wide and high (h2v2, 4:2:0). It is written from
// the description of h2v1_fancy_upsample and h2v2_fancy_upsample in libjpeg's
// jdsample.c (IJG libjpeg 6b, unchanged in libjpeg-turbo), not from its code:
//
//   - Each output sample is 3/4 of the nearer input sample and 1/4 of the
//     next nearer, so the two outputs of one input lie a quarter of the way
//     toward each neighbor. In two dimensions the 3:1 weighting is applied
//     down, between the input row and the row above (for the upper output
//     row) or below (for the lower), and then across, in 9:3:3:1 overall.
//   - Rounding alternates between the two outputs of a pair, +1 and +2 in
//     h2v1 (out of 4) and +8 and +7 in h2v2 (out of 16), so it has no bias.
//   - At the left and right edges the missing neighbor is the edge sample
//     itself; at the top and bottom the missing row is the edge row
//     (libjpeg's jdmainct.c gives the first and last row groups themselves as
//     context). Only the component's own samples are read, ceil(width/2) of
//     them across and, for 4:2:0, ceil(height/2) down: the samples the
//     encoder added to fill out the last MCU are never used.
//
// Other layouts (4:4:0, 4:1:1, 4:1:0) are repeated, as libjpeg 6b does, and
// so is a component of one or two samples across, which libjpeg does not
// filter either.

// fancyChroma returns src's Cb and Cr at one sample a pixel, row by row over
// src's bounds, when its chroma is 4:2:2 or 4:2:0. ok is false for any other
// layout, which the caller repeats.
func fancyChroma(src *image.YCbCr) (cb, cr []byte, ok bool) {
	var vertical bool
	switch src.SubsampleRatio {
	case image.YCbCrSubsampleRatio422:
	case image.YCbCrSubsampleRatio420:
		vertical = true
	default:
		return nil, nil, false
	}
	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return nil, nil, false
	}
	cw, ch := (width+1)/2, height
	if vertical {
		ch = (height + 1) / 2
	}
	// libjpeg filters only a component more than two samples wide, and
	// repeats a narrower one (jinit_upsampler); so does this, or an image
	// of one or two chroma columns would differ from it by tens of levels.
	if cw <= 2 {
		return nil, nil, false
	}
	cb = make([]byte, width*height)
	cr = make([]byte, width*height)
	sums := make([]int, cw)
	wide := make([]byte, 2*cw) // a doubled row, of which width samples are kept
	for _, p := range []struct {
		plane []byte
		out   []byte
	}{{src.Cb, cb}, {src.Cr, cr}} {
		// chromaRow is the component's own row j, cw samples.
		chromaRow := func(j int) []byte {
			y := b.Min.Y + j
			if vertical {
				y = b.Min.Y + 2*j
			}
			at := src.COffset(b.Min.X, y)
			return p.plane[at : at+cw]
		}
		for y := 0; y < height; y++ {
			if !vertical {
				upsampleH2V1(chromaRow(y), wide)
			} else {
				// The upper output row of a pair leans on the row above, the
				// lower on the row below, and an edge row on itself.
				j := y / 2
				near := j - 1
				if y%2 == 1 {
					near = j + 1
				}
				this, other := chromaRow(j), chromaRow(min(max(near, 0), ch-1))
				for i := range sums {
					sums[i] = 3*int(this[i]) + int(other[i])
				}
				upsampleH2V2(sums, wide)
			}
			copy(p.out[y*width:(y+1)*width], wide)
		}
	}
	return cb, cr, true
}

// upsampleH2V1 doubles one row of at least three samples across, into out,
// which holds twice as many.
func upsampleH2V1(in []byte, out []byte) {
	n := len(in)
	out[0] = in[0]
	out[1] = byte((3*int(in[0]) + int(in[1]) + 2) >> 2)
	for i := 1; i < n-1; i++ {
		v := 3 * int(in[i])
		out[2*i] = byte((v + int(in[i-1]) + 1) >> 2)
		out[2*i+1] = byte((v + int(in[i+1]) + 2) >> 2)
	}
	out[2*n-2] = byte((3*int(in[n-1]) + int(in[n-2]) + 1) >> 2)
	out[2*n-1] = in[n-1]
}

// upsampleH2V2 doubles one row of at least three column sums (3 times the
// nearer row plus the further, so 4 times a sample's scale) across, into out,
// which holds twice as many.
func upsampleH2V2(sums []int, out []byte) {
	n := len(sums)
	out[0] = byte((4*sums[0] + 8) >> 4)
	out[1] = byte((3*sums[0] + sums[1] + 7) >> 4)
	for i := 1; i < n-1; i++ {
		v := 3 * sums[i]
		out[2*i] = byte((v + sums[i-1] + 8) >> 4)
		out[2*i+1] = byte((v + sums[i+1] + 7) >> 4)
	}
	out[2*n-2] = byte((3*sums[n-1] + sums[n-2] + 8) >> 4)
	out[2*n-1] = byte((4*sums[n-1] + 7) >> 4)
}
