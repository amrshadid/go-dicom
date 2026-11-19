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

// TestJPEGLSThresholdsAreWrittenWhereOldCharLSDiffers proves the encoder
// writes an LSE segment exactly for the precisions and NEAR values where CharLS
// before 2.1, which dcmtk 3.7.0 bundles, would decode with other thresholds than
// T.87's, and that the segment states T.87's. Worked from both formulas for
// every NEAR, they differ at 1 NEAR value of 2 bits, 2 of 3 bits, 5 of 4 bits
// and 10 of 5 bits (from NEAR 1 up to a point), at 22 of the 32 of 6 bits and
// 43 of the 64 of 7 bits, NEAR 0 among them, and nowhere from 8 bits up. Where
// lossless, that is 6 and 7 bits only, as the test above has it.
func TestJPEGLSThresholdsAreWrittenWhereOldCharLSDiffers(t *testing.T) {
	want := map[int]int{2: 1, 3: 2, 4: 5, 5: 10, 6: 22, 7: 43}
	for bits := 2; bits <= 16; bits++ {
		maxVal := 1<<bits - 1
		bytesPer := 1
		if bits > 8 {
			bytesPer = 2
		}
		written := 0
		for near := 0; near <= maxVal/2 && near <= 255; near++ {
			stream, err := EncodeJPEGLSNearLossless(make([]byte, 3*2*bytesPer), 3, 2, 1, bits, near)
			if err != nil {
				t.Fatal(err)
			}
			p := jpeglsParams{maxVal: maxVal, near: near}
			if err := p.computeDefaults(); err != nil {
				t.Fatal(err)
			}
			// SOI and the 13-byte SOF55 come first.
			lse := stream[15:]
			hasLSE := lse[0] == 0xFF && lse[1] == markerLSE
			l1, l2, l3 := legacyCharLSThresholds(maxVal, near)
			if differ := l1 != p.t1 || l2 != p.t2 || l3 != p.t3; hasLSE != differ {
				t.Errorf("%d bits, NEAR %d: LSE written %v, old thresholds %d,%d,%d, T.87 %d,%d,%d",
					bits, near, hasLSE, l1, l2, l3, p.t1, p.t2, p.t3)
			}
			if !hasLSE {
				continue
			}
			written++
			for i, f := range []int{maxVal, p.t1, p.t2, p.t3, 64} {
				if got := int(lse[5+2*i])<<8 | int(lse[6+2*i]); got != f {
					t.Errorf("%d bits, NEAR %d: LSE field %d is %d, want %d", bits, near, i, got, f)
				}
			}
		}
		if written != want[bits] {
			t.Errorf("%d bits: LSE written at %d NEAR values, want %d", bits, written, want[bits])
		}
	}
}
