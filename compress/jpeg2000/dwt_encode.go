package jpeg2000

// The forward discrete wavelet transform, PS 15444-1 Annex F.4, for the
// reversible 5/3 filter and the irreversible 9/7: the other half of dwt.go.
//
// Each level takes the samples of one resolution, filters the columns and
// then the rows, and splits what comes out into the LL band, which is the
// next resolution down, and the three detail bands of this one. The inverse
// in dwt.go runs the same steps backwards, rows before columns, so the order
// here is fixed by it: with integer rounding in every step, undoing them in
// another order would not return the samples.
//
// Coordinates are absolute, on the reference grid, as in dwt.go and for the
// same reason: whether a coordinate is even or odd decides whether its sample
// becomes low-pass or high-pass.

// forwardDWT decomposes a tile-component's samples into its subbands.
//
// samples covers the tile-component's rectangle in raster order and holds
// whole numbers. The result is what inverseDWT takes: bands[r][t] is the band
// of type t at resolution r, and resolution 0 holds the LL band alone.
func forwardDWT(tc TileComponent, samples []float32) (map[int]map[int]*subbandGrid, error) {
	return forwardDWTFilter(tc, samples, false)
}

// forwardDWTFilter is forwardDWT with either filter: the irreversible 9/7
// when irreversible is set, for any real-valued samples.
func forwardDWTFilter(tc TileComponent, samples []float32, irreversible bool) (map[int]map[int]*subbandGrid, error) {
	levels := len(tc.Resolutions) - 1
	if levels < 0 {
		return nil, errorf("a tile-component with no resolutions")
	}
	if len(samples) != tc.Width()*tc.Height() {
		return nil, errorf("%d samples for a tile-component of %dx%d", len(samples), tc.Width(), tc.Height())
	}

	current := &subbandGrid{x0: tc.X0, y0: tc.Y0, x1: tc.X1, y1: tc.Y1, values: append([]float32(nil), samples...)}
	bands := make(map[int]map[int]*subbandGrid, levels+1)

	for r := levels; r >= 1; r-- {
		analyze2D(current, irreversible)

		bands[r] = make(map[int]*subbandGrid, 3)
		for _, band := range tc.Resolutions[r].Bands {
			bands[r][band.Type] = deinterleave(current, band.X0, band.Y0, band.X1, band.Y1, band.Type)
		}
		below := tc.Resolutions[r-1]
		current = deinterleave(current, below.X0, below.Y0, below.X1, below.Y1, BandLL)
	}
	bands[0] = map[int]*subbandGrid{BandLL: current}
	return bands, nil
}

// deinterleave is 2D_DEINTERLEAVE (F.4.5): one band taken off the grid the
// analysis left it on. A band's sample (u, v) sits at (2u + xob, 2v + yob),
// the offsets naming which half of each axis the band is: the inverse of
// interleave in dwt.go.
func deinterleave(src *subbandGrid, x0, y0, x1, y1, band int) *subbandGrid {
	xob, yob := 0, 0
	switch band {
	case BandHL:
		xob = 1
	case BandLH:
		yob = 1
	case BandHH:
		xob, yob = 1, 1
	}
	out := &subbandGrid{x0: x0, y0: y0, x1: x1, y1: y1}
	if x1 <= x0 || y1 <= y0 {
		return out
	}
	out.values = make([]float32, (x1-x0)*(y1-y0))
	for v := y0; v < y1; v++ {
		for u := x0; u < x1; u++ {
			out.values[(v-y0)*(x1-x0)+(u-x0)] = src.at(2*u+xob, 2*v+yob)
		}
	}
	return out
}

// analyze2D is 2D_SD (F.4.2): the columns filtered, then the rows, which is
// the inverse's order backwards.
func analyze2D(g *subbandGrid, irreversible bool) {
	w, h := g.width(), g.height()
	if w <= 0 || h <= 0 {
		return
	}

	var buf []int32
	var fbuf []float64
	line := func(x []float32, i0, i1 int) {
		if irreversible {
			fbuf = analyze97(x, i0, i1, fbuf)
		} else {
			buf = analyze53(x, i0, i1, buf)
		}
	}
	column := make([]float32, h)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			column[y] = g.values[y*w+x]
		}
		line(column, g.y0, g.y1)
		for y := 0; y < h; y++ {
			g.values[y*w+x] = column[y]
		}
	}
	for y := 0; y < h; y++ {
		line(g.values[y*w:(y+1)*w], g.x0, g.x1)
	}
}

// analyze97 is 1D_SD with the irreversible 9/7 filter (F.4.8.2): the four
// lifting steps of Table F.4 in the standard's order, then the scaling, each
// the exact reverse of a step synthesize97 in dwt.go takes. The scaling is
// the one that function undoes, low-pass by 1/1.230174 and high-pass by
// 1/1.625786: the decoder's normalization, which OpenJPEG shares, and so the
// one a quantization step is chosen against. buf is working space, returned
// for reuse.
func analyze97(x []float32, i0, i1 int, buf []float64) []float64 {
	n := i1 - i0
	if n <= 0 {
		return buf
	}
	if n == 1 {
		// As for the 5/3: a lone sample at an odd coordinate is doubled.
		if !isEven(i0) {
			x[0] *= 2
		}
		return buf
	}

	// Four steps each reaching one neighbor: four samples of padding at each
	// end, the region still correct narrowing by one a step.
	const pad = 4
	if cap(buf) < n+2*pad {
		buf = make([]float64, n+2*pad)
	}
	buf = buf[:n+2*pad]
	for k := range buf {
		buf[k] = float64(extended(x, i0, i1, i0-pad+k))
	}
	at := func(i int) int { return i - i0 + pad }
	lo, hi := i0-pad, i1+pad

	lift := func(odd bool, c float64, from, to int) {
		for i := from; i < to; i++ {
			if isEven(i) == odd {
				continue
			}
			buf[at(i)] += c * (buf[at(i-1)] + buf[at(i+1)])
		}
	}
	lift(true, dwtAlpha, lo+1, hi-1)
	lift(false, dwtBeta, lo+2, hi-2)
	lift(true, dwtGamma, lo+3, hi-3)
	lift(false, dwtDelta, lo+4, hi-4)

	for i := i0; i < i1; i++ {
		if isEven(i) {
			buf[at(i)] /= dwtLowScale
		} else {
			buf[at(i)] /= dwtHighScale
		}
	}
	for k := 0; k < n; k++ {
		x[k] = float32(buf[pad+k])
	}
	return buf
}

// analyze53 is 1D_SD with the reversible 5/3 filter (F.4.8.1): one row or
// column of samples filtered, in place, into low-pass coefficients at even
// coordinates and high-pass ones at odd. x[0] is the sample at coordinate i0.
// buf is working space, returned so that a caller filtering many lines
// allocates it once.
//
// The two lifting steps, in the standard's order:
//
//	Y(2n+1) = X(2n+1) - floor((X(2n) + X(2n+2)) / 2)
//	Y(2n)   = X(2n)   + floor((Y(2n-1) + Y(2n+1) + 2) / 4)
//
// The shifts round towards negative infinity, as the standard's floors do
// and Go's division would not. Beyond its ends the signal is reflected about
// its first and last sample (F.4.7), as extended reads it.
func analyze53(x []float32, i0, i1 int, buf []int32) []int32 {
	n := i1 - i0
	if n <= 0 {
		return buf
	}
	if n == 1 {
		// A line of one sample has nothing to predict it from. At an odd
		// coordinate it is a high-pass coefficient and is doubled (F.4.8.1),
		// which the inverse halves.
		if !isEven(i0) {
			x[0] *= 2
		}
		return buf
	}

	// Two samples beyond each end is as far as either step reaches.
	const pad = 2
	if cap(buf) < n+2*pad {
		buf = make([]int32, n+2*pad)
	}
	buf = buf[:n+2*pad]
	for k := range buf {
		buf[k] = int32(extended(x, i0, i1, i0-pad+k))
	}
	at := func(i int) int { return i - i0 + pad }

	// The high-pass step, one coordinate beyond each end, since the low-pass
	// step reads its neighbors there.
	for i := i0 - 1; i <= i1; i++ {
		if !isEven(i) {
			buf[at(i)] -= (buf[at(i-1)] + buf[at(i+1)]) >> 1
		}
	}
	for i := i0; i < i1; i++ {
		if isEven(i) {
			buf[at(i)] += (buf[at(i-1)] + buf[at(i+1)] + 2) >> 2
		}
	}
	for k := 0; k < n; k++ {
		x[k] = float32(buf[pad+k])
	}
	return buf
}
