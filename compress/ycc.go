package compress

// YCbCr to RGB as libjpeg converts it.
//
// T.81 does not define a color conversion; JFIF (T.871) gives the equations
//
//	R = Y + 1.402 (Cr - 128)
//	G = Y - 0.344136 (Cb - 128) - 0.714136 (Cr - 128)
//	B = Y + 1.772 (Cb - 128)
//
// and leaves the arithmetic to the decoder. Two decoders that both follow the
// equations differ by a level where they round differently, and the ones a
// color JPEG is compared with here, dcmtk (IJG libjpeg 6b) and Pillow
// (libjpeg-turbo), share libjpeg's arithmetic, described in its jdcolor.c
// ("ycc_rgb_convert"): each coefficient is scaled by 2^16 and rounded to an
// integer (1.40200 to 91881, 1.77200 to 116130, 0.71414 to 46802, 0.34414 to
// 22554); R and B add half of 2^16 to the scaled product and shift right by 16;
// G sums its two scaled products with the one half before a single shift; the
// shifts are arithmetic, so a negative product rounds toward minus infinity;
// and Y is added after the shift, the sum clamped to 0..255.
//
// image/color's YCbCr conversion uses the same coefficients but scales Y with
// the products and rounds after, so it differed from libjpeg by up to two
// levels on frames that are not subsampled, three with the chroma filter.
// This is written from that description, not from libjpeg's source.
const (
	yccShift = 16
	yccHalf  = 1 << (yccShift - 1)
	yccCrR   = 91881  // 1.40200 * 2^16, rounded
	yccCbB   = 116130 // 1.77200 * 2^16, rounded
	yccCrG   = 46802  // 0.71414 * 2^16, rounded
	yccCbG   = 22554  // 0.34414 * 2^16, rounded
)

// The per-value terms, as libjpeg tabulates them: crR and cbB already shifted,
// crG and cbG still scaled, cbG carrying the one half.
var yccCrRTable, yccCbBTable, yccCrGTable, yccCbGTable = func() (crR, cbB, crG, cbG [256]int32) {
	for i := range crR {
		x := int32(i) - 128
		crR[i] = (yccCrR*x + yccHalf) >> yccShift
		cbB[i] = (yccCbB*x + yccHalf) >> yccShift
		crG[i] = -yccCrG * x
		cbG[i] = -yccCbG*x + yccHalf
	}
	return
}()

// yccToRGB converts one sample from YCbCr to RGB with libjpeg's arithmetic.
func yccToRGB(y, cb, cr uint8) (r, g, b uint8) {
	yy := int32(y)
	return clampByte(yy + yccCrRTable[cr]),
		clampByte(yy + (yccCbGTable[cb]+yccCrGTable[cr])>>yccShift),
		clampByte(yy + yccCbBTable[cb])
}

func clampByte(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
