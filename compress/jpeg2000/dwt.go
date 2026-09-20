package jpeg2000

// The inverse discrete wavelet transform, PS 15444-1 Annex F.
//
// Tier-1 hands back subband coefficients: a small low-pass image and, at each
// resolution, three bands of detail. This is what puts them back together. One
// level of synthesis takes the LL of the level below plus that level's HL, LH
// and HH, interleaves them on a single grid, and filters the rows and then the
// columns; doing that NL times rebuilds the tile-component.
//
// Two filters are defined and both are used in practice. The 5/3 is integer
// and exactly invertible, which is what makes lossless JPEG 2000 lossless: a
// decoder that gets it right returns the encoder's samples and not merely close
// ones. The 9/7 is floating point and is used where the file was lossy anyway.
//
// All coordinates here are absolute, on the reference grid. That matters more
// than it looks: whether a row starts on an even or an odd coordinate decides
// which of its samples are low-pass and which are high-pass, and a tile whose
// origin is odd is filtered differently from the same samples at the origin.
// Working in absolute coordinates is why there is no special case for it.

// The 9/7 lifting parameters (Table F.4). The inverse undoes the forward
// transform's steps in reverse, so the signs here are the forward ones and the
// steps below subtract rather than add.
const (
	dwtAlpha = -1.586134342059924
	dwtBeta  = -0.052980118572961
	dwtGamma = 0.882911075530934
	dwtDelta = 0.443506852043971

	// The two scalings the synthesis begins with (Table F.4). They are not
	// reciprocals: the high-pass coefficients carry an extra factor of two,
	// because the analysis filter that produced them is normalized that way.
	// Using 1/K for both is the mistake this pair exists to name — it leaves a
	// smooth image looking almost right, since its detail coefficients are
	// small, and wrecks a detailed one. On a noise image it was 1006 of 1024
	// samples wrong, the worst by 102.
	dwtLowScale  = 1.230174104914001
	dwtHighScale = 1.625786440180958
)

// subbandGrid is one band's coefficients with the rectangle they cover.
type subbandGrid struct {
	x0, y0, x1, y1 int
	values         []float32
}

func (g *subbandGrid) width() int  { return g.x1 - g.x0 }
func (g *subbandGrid) height() int { return g.y1 - g.y0 }

// at returns the coefficient at absolute coordinates.
func (g *subbandGrid) at(x, y int) float32 {
	return g.values[(y-g.y0)*g.width()+(x-g.x0)]
}

// inverseDWT rebuilds a tile-component's samples from its resolutions.
//
// bands[r][t] is the band of type t at resolution r, already dequantized;
// resolution 0 holds only the LL band. The result covers the tile-component's
// own rectangle, in raster order.
func inverseDWT(tc TileComponent, bands map[int]map[int]*subbandGrid, reversible bool) ([]float32, error) {
	levels := len(tc.Resolutions) - 1
	if levels < 0 {
		return nil, errorf("a tile-component with no resolutions")
	}

	// The starting image is the LL band of resolution 0.
	ll := bands[0][BandLL]
	if ll == nil {
		return nil, errorf("resolution 0 has no LL band")
	}
	current := ll

	for r := 1; r <= levels; r++ {
		res := tc.Resolutions[r]
		next := &subbandGrid{
			x0: res.X0, y0: res.Y0, x1: res.X1, y1: res.Y1,
			values: make([]float32, maxInt(0, (res.X1-res.X0)*(res.Y1-res.Y0))),
		}
		interleave(next, current, bands[r])
		synthesize2D(next, reversible)
		current = next
	}

	if current.x0 != tc.X0 || current.y0 != tc.Y0 ||
		current.x1 != tc.X1 || current.y1 != tc.Y1 {
		return nil, errorf("the synthesized grid is %dx%d at (%d,%d), not the tile-component's",
			current.width(), current.height(), current.x0, current.y0)
	}
	return current.values, nil
}

// interleave is 2D_INTERLEAVE (F.3.3): the four bands laid onto one grid, the
// low-pass samples on even coordinates and the high-pass ones on odd.
//
// This is where the band coordinate formula pays off. A band's sample at (u, v)
// belongs at (2u + xob, 2v + yob) on the resolution's grid, with the offsets
// naming which half of each axis the band came from, and the result lands
// inside the resolution's own rectangle without any adjustment.
func interleave(dst, ll *subbandGrid, detail map[int]*subbandGrid) {
	place := func(band *subbandGrid, xob, yob int) {
		if band == nil {
			return
		}
		for v := band.y0; v < band.y1; v++ {
			for u := band.x0; u < band.x1; u++ {
				x, y := 2*u+xob, 2*v+yob
				if x < dst.x0 || x >= dst.x1 || y < dst.y0 || y >= dst.y1 {
					continue
				}
				dst.values[(y-dst.y0)*dst.width()+(x-dst.x0)] = band.at(u, v)
			}
		}
	}

	place(ll, 0, 0)
	place(detail[BandHL], 1, 0)
	place(detail[BandLH], 0, 1)
	place(detail[BandHH], 1, 1)
}

// synthesize2D is 2D_SR (F.3.2): the rows filtered, then the columns.
func synthesize2D(g *subbandGrid, reversible bool) {
	w, h := g.width(), g.height()
	if w <= 0 || h <= 0 {
		return
	}

	row := make([]float32, w)
	for y := 0; y < h; y++ {
		copy(row, g.values[y*w:(y+1)*w])
		synthesize1D(row, g.x0, g.x1, reversible)
		copy(g.values[y*w:(y+1)*w], row)
	}

	column := make([]float32, h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			column[y] = g.values[y*w+x]
		}
		synthesize1D(column, g.y0, g.y1, reversible)
		for y := 0; y < h; y++ {
			g.values[y*w+x] = column[y]
		}
	}
}

// synthesize1D is 1D_SR (F.3.4): one row or column of interleaved coefficients
// filtered back into samples, in place. y[0] is the sample at coordinate i0.
func synthesize1D(y []float32, i0, i1 int, reversible bool) {
	if i1-i0 == 1 {
		// A single sample is its own reconstruction if it is a low-pass one,
		// and a high-pass sample alone carries twice its value (F.3.7).
		if i0%2 != 0 {
			y[0] /= 2
		}
		return
	}
	if i1-i0 <= 0 {
		return
	}

	if reversible {
		synthesize53(y, i0, i1)
		return
	}
	synthesize97(y, i0, i1)
}

// extended reads the signal at any coordinate, mirroring at both ends.
//
// The filter needs samples beyond the edge, and the standard's answer (F.3.7)
// is to reflect the signal about its first and last sample. Reflection rather
// than zero-filling is what keeps an edge from turning into an artificial step,
// and the period is 2(n−1) because the end samples are not repeated.
func extended(y []float32, i0, i1, i int) float32 {
	n := i1 - i0
	if n == 1 {
		return y[0]
	}
	k := (i - i0) % (2 * (n - 1))
	if k < 0 {
		k += 2 * (n - 1)
	}
	if k >= n {
		k = 2*(n-1) - k
	}
	return y[k]
}

// synthesize53 is the reversible 5/3 filter (F.3.8.2), the whole of lossless
// JPEG 2000's claim to be lossless. It is integer arithmetic on values that
// are whole numbers, and the two shifts round towards negative infinity, which
// is what the standard's division bars mean and what Go's / would not do.
func synthesize53(y []float32, i0, i1 int) {
	n := i1 - i0
	const pad = 2
	buf := make([]int32, n+2*pad)
	for k := range buf {
		buf[k] = int32(extended(y, i0, i1, i0-pad+k))
	}
	at := func(x int) int { return x - i0 + pad }

	// The even samples, each corrected by its two odd neighbors, over one
	// position more than the signal holds at each end: the odd step below
	// reads them.
	for x := i0 - 1; x <= i1; x++ {
		if !isEven(x) {
			continue
		}
		buf[at(x)] -= (buf[at(x-1)] + buf[at(x+1)] + 2) >> 2
	}
	// Then the odd samples, from the even ones just reconstructed.
	for x := i0; x < i1; x++ {
		if isEven(x) {
			continue
		}
		buf[at(x)] += (buf[at(x-1)] + buf[at(x+1)]) >> 1
	}

	for k := 0; k < n; k++ {
		y[k] = float32(buf[pad+k])
	}
}

// synthesize97 is the irreversible 9/7 filter (F.3.8.2): the two scalings the
// forward transform ended with, undone first, then its four lifting steps
// undone in reverse order.
func synthesize97(y []float32, i0, i1 int) {
	n := i1 - i0
	// A copy padded by four at each end. Each lifting step can then read its
	// neighbors with no bounds test, and each step narrows the region that is
	// still correct by one on each side — four steps, four of padding, and the
	// signal itself is what is left.
	const pad = 4
	buf := make([]float64, n+2*pad)
	for k := range buf {
		buf[k] = float64(extended(y, i0, i1, i0-pad+k))
	}
	at := func(x int) int { return x - i0 + pad }
	lo, hi := i0-pad, i1+pad

	// Undo the scaling the forward transform ended with.
	for x := lo; x < hi; x++ {
		if isEven(x) {
			buf[at(x)] *= dwtLowScale
		} else {
			buf[at(x)] *= dwtHighScale
		}
	}

	// Then the lifting steps in reverse, each undone by subtracting what the
	// forward step added.
	lift := func(odd bool, c float64, from, to int) {
		for x := from; x < to; x++ {
			if isEven(x) == odd {
				continue
			}
			buf[at(x)] -= c * (buf[at(x-1)] + buf[at(x+1)])
		}
	}
	lift(false, dwtDelta, lo+1, hi-1)
	lift(true, dwtGamma, lo+2, hi-2)
	lift(false, dwtBeta, lo+3, hi-3)
	lift(true, dwtAlpha, lo+4, hi-4)

	for k := 0; k < n; k++ {
		y[k] = float32(buf[pad+k])
	}
}

// isEven is parity on the reference grid, where a coordinate can be negative
// and Go's % keeps the sign of the dividend.
func isEven(x int) bool { return x%2 == 0 }
