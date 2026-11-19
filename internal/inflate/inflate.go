// Package inflate holds the bound on how far a deflated stream may expand,
// which the file parser and compress share. See compress.MaxInflateRatio,
// MinInflateAllowance and InflateLimitFor for why it is shaped this way;
// they are these. It lives here so that the parser, which inflates a
// Deflated Explicit VR Little Endian data set (PS3.5 A.5), does not link
// the image codecs compress brings with it.
package inflate

// MaxRatio is compress.MaxInflateRatio.
const MaxRatio int64 = 1000

// MinAllowance is compress.MinInflateAllowance.
const MinAllowance int64 = 8 << 20

// LimitFor is compress.InflateLimitFor: how many bytes a deflated stream of
// compressedSize bytes may produce, given an absolute ceiling. A
// non-positive compressedSize, unknown, gets the ceiling.
func LimitFor(compressedSize, absoluteMax int64) int64 {
	// Short-circuit before multiplying, so a large compressedSize cannot
	// overflow into a small or negative limit.
	if compressedSize <= 0 || compressedSize > absoluteMax/MaxRatio {
		return absoluteMax
	}

	limit := compressedSize * MaxRatio
	if limit < MinAllowance {
		limit = MinAllowance
	}
	if limit > absoluteMax {
		limit = absoluteMax
	}
	return limit
}
