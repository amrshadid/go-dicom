package compress_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// TestInflateLimitFor covers the bound that keeps rejecting a decompression
// bomb proportional to the effort of building one.
//
// Against an absolute ceiling alone, a 5 KB file could make a parser allocate
// 256 MiB before the limit rejected it — and because io.ReadAll grows by
// doubling, the real peak was closer to twice that. The attacker picks the
// input size, so the ceiling alone sets the cost of an attack rather than the
// attacker's own effort.
func TestInflateLimitFor(t *testing.T) {
	const absolute = 256 << 20

	tests := []struct {
		name           string
		compressedSize int64
		want           int64
	}{
		{
			// The case that motivated the bound. Deflate cannot exceed roughly
			// 1032:1, so 5 KB can produce at most ~5 MB — but the ceiling alone
			// would have permitted allocating 256 MiB to discover that.
			name:           "a tiny input is held to the floor",
			compressedSize: 5 << 10,
			want:           compress.MinInflateAllowance,
		},
		{
			// A legitimately blank 512 KiB frame compresses to a few hundred
			// bytes. The floor is what keeps the ratio from rejecting it.
			name:           "a blank frame still fits under the floor",
			compressedSize: 512,
			want:           compress.MinInflateAllowance,
		},
		{
			name:           "a mid-sized input is bounded by the ratio",
			compressedSize: 64 << 10,
			want:           64 << 10 * compress.MaxInflateRatio,
		},
		{
			name:           "a large input is bounded by the absolute ceiling",
			compressedSize: 4 << 20,
			want:           absolute,
		},
		{
			// A non-seekable source cannot report its remaining length.
			name:           "an unknown size falls back to the ceiling",
			compressedSize: -1,
			want:           absolute,
		},
		{
			name:           "a zero size falls back to the ceiling",
			compressedSize: 0,
			want:           absolute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := compress.InflateLimitFor(tc.compressedSize, absolute)
			if got != tc.want {
				t.Errorf("InflateLimitFor(%d, %d) = %d, want %d",
					tc.compressedSize, absolute, got, tc.want)
			}
		})
	}
}

// TestInflateLimitForNeverExceedsTheCeiling guards the multiplication.
//
// compressedSize * MaxInflateRatio overflows int64 for sizes above roughly
// 9 PB, and an overflowed product is negative — which would compare as less
// than the ceiling and be returned as the limit, turning the bound into a
// bypass. The short-circuit before multiplying is what prevents that, so it is
// checked across the whole range rather than at a few points.
func TestInflateLimitForNeverExceedsTheCeiling(t *testing.T) {
	const absolute = 256 << 20

	sizes := []int64{
		-1 << 62, -1, 0, 1, 512, 1 << 10, 1 << 20, 1 << 30, 1 << 40,
		1<<62 - 1, 1<<63 - 1,
	}

	for _, size := range sizes {
		got := compress.InflateLimitFor(size, absolute)
		if got <= 0 {
			t.Errorf("InflateLimitFor(%d, %d) = %d, want a positive limit", size, absolute, got)
		}
		if got > absolute {
			t.Errorf("InflateLimitFor(%d, %d) = %d, which exceeds the ceiling %d",
				size, absolute, got, absolute)
		}
	}
}
