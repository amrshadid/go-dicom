package compress

import "testing"

// TestJPEGLSDefaultThresholdsFollowT87 proves the default thresholds are the
// ones T.87 C.2.4.1.1.1 gives, worked out by hand from its formulas, at every
// precision from 2 to 8 and at the NEAR values where its CLAMP decides the
// answer. CLAMP(i, j, MAXVAL) is j when i is above MAXVAL or below j: at 3 bits
// and NEAR 2, T2 = max(3, 7/32 + 10) = 10 is above MAXVAL 7, so T2 is T1 = 6,
// not 7. The 12 and 16 bit rows are the familiar 18, 67, 276.
func TestJPEGLSDefaultThresholdsFollowT87(t *testing.T) {
	cases := []struct {
		bits, near, t1, t2, t3 int
	}{
		{2, 0, 2, 3, 3}, // T3 = 4 is above MAXVAL 3, so it is T2
		{3, 0, 2, 3, 4},
		{4, 0, 2, 3, 4},
		{5, 0, 2, 3, 4},
		{6, 0, 2, 3, 5},  // 21/4: the MAXVAL < 128 branch
		{7, 0, 2, 3, 10}, // 21/2
		{8, 0, 3, 7, 21},
		{12, 0, 18, 67, 276},
		{16, 0, 18, 67, 276},

		{2, 1, 3, 3, 3},
		{3, 2, 6, 6, 6},
		{3, 3, 4, 4, 4}, // T1 = 9 is above MAXVAL 7, so it is NEAR+1
		{4, 7, 8, 8, 8},
		{5, 15, 16, 16, 16},
		{6, 1, 3, 6, 12},
		{6, 31, 32, 32, 32},
		{7, 2, 7, 13, 24},
		{7, 63, 64, 64, 64},
		{8, 3, 12, 22, 42},
		{8, 127, 128, 128, 128},
	}
	for _, tc := range cases {
		p := jpeglsParams{maxVal: 1<<tc.bits - 1, near: tc.near}
		if err := p.computeDefaults(); err != nil {
			t.Fatalf("%d bits, NEAR %d: %v", tc.bits, tc.near, err)
		}
		if p.t1 != tc.t1 || p.t2 != tc.t2 || p.t3 != tc.t3 || p.reset != 64 {
			t.Errorf("%d bits, NEAR %d: thresholds %d,%d,%d RESET %d, T.87 gives %d,%d,%d RESET 64",
				tc.bits, tc.near, p.t1, p.t2, p.t3, p.reset, tc.t1, tc.t2, tc.t3)
		}
	}
}

// TestJPEGLSLegacyThresholdsDifferOnlyAtSixAndSevenBits proves the claim the
// refusal of a dcmtk-coded frame rests on: the thresholds CharLS before 2.1
// used are T.87's at every lossless precision from 2 to 16 except 6 and 7 bits.
func TestJPEGLSLegacyThresholdsDifferOnlyAtSixAndSevenBits(t *testing.T) {
	for bits := 2; bits <= 16; bits++ {
		p := jpeglsParams{maxVal: 1<<bits - 1}
		if err := p.computeDefaults(); err != nil {
			t.Fatal(err)
		}
		l1, l2, l3 := legacyCharLSThresholds(p.maxVal, 0)
		differ := l1 != p.t1 || l2 != p.t2 || l3 != p.t3
		if differ != (bits == 6 || bits == 7) {
			t.Errorf("%d bits: legacy %d,%d,%d, T.87 %d,%d,%d", bits, l1, l2, l3, p.t1, p.t2, p.t3)
		}
	}
}
