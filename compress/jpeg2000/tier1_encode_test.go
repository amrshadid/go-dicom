package jpeg2000

import (
	"math/rand"
	"testing"
)

// The block encoder is checked against the block decoder: coefficients
// encoded and decoded come back as they were. The two share the context
// rules, so this shows the encoder says in each pass what the decoder asks
// in it, in the same order. What they could both have wrong is caught where
// whole codestreams are decoded by OpenJPEG.

// roundTrip encodes a block and decodes it with the band's magnitude bits
// set to what the block needs plus spare planes, as a real band has.
func roundTrip(t *testing.T, coefficients []int32, w, h, bandType, spare int) (EncodedBlock, []int32) {
	t.Helper()
	encoded, err := EncodeBlock(coefficients, w, h, bandType)
	if err != nil {
		t.Fatalf("EncodeBlock: %v", err)
	}
	decoded, err := DecodeBlock(CodeBlockData{
		Block:         CodeBlock{X1: w, Y1: h},
		BandType:      bandType,
		ZeroBitPlanes: spare,
		Passes:        encoded.Passes,
		Data:          encoded.Data,
	}, encoded.BitPlanes+spare)
	if err != nil {
		t.Fatalf("DecodeBlock: %v", err)
	}
	// The decoder answers in half units (see DecodeBlock).
	for i := range decoded {
		decoded[i] /= 2
	}
	return encoded, decoded
}

// TestBlockEncoderIsInvertedByTheDecoder covers the shapes a block comes
// in and the kinds of content a band holds: sizes that are not multiples of
// the four-row stripe, each band orientation, and coefficients from all
// zero to full range.
func TestBlockEncoderIsInvertedByTheDecoder(t *testing.T) {
	random := rand.New(rand.NewSource(308))
	contents := map[string]func(i, w int) int32{
		"sparse, as a detail band is": func(int, int) int32 {
			if random.Intn(12) != 0 {
				return 0
			}
			return int32(random.Intn(31) - 15)
		},
		"dense and small":       func(int, int) int32 { return int32(random.Intn(9) - 4) },
		"full range of 16 bits": func(int, int) int32 { return int32(random.Intn(1<<17) - 1<<16) },
		"all positive":          func(int, int) int32 { return int32(random.Intn(4000)) },
		"all negative":          func(int, int) int32 { return -int32(random.Intn(4000)) - 1 },
		"one value everywhere":  func(int, int) int32 { return -7 },
		"a gradient":            func(i, w int) int32 { return int32(i%w) - int32(i/w) },
		"one coefficient in a block": func(i, w int) int32 {
			if i == w+1 {
				return 1
			}
			return 0
		},
		"the largest the decoder reads": func(i, _ int) int32 {
			if i%3 == 0 {
				return 1<<30 - 1
			}
			return -(1<<30 - 1)
		},
	}
	sizes := [][2]int{{1, 1}, {1, 9}, {9, 1}, {2, 3}, {4, 4}, {5, 7}, {8, 5}, {16, 16}, {33, 17}, {64, 64}}

	for name, value := range contents {
		for _, size := range sizes {
			w, h := size[0], size[1]
			for _, band := range []int{BandLL, BandHL, BandLH, BandHH} {
				coefficients := make([]int32, w*h)
				any := false
				for i := range coefficients {
					coefficients[i] = value(i, w)
					any = any || coefficients[i] != 0
				}
				encoded, decoded := roundTrip(t, coefficients, w, h, band, 2)
				for i := range coefficients {
					if decoded[i] != coefficients[i] {
						t.Fatalf("%s, %dx%d, band %d: coefficient %d came back %d, was %d",
							name, w, h, band, i, decoded[i], coefficients[i])
					}
				}
				if any && encoded.Passes != 3*encoded.BitPlanes-2 {
					t.Fatalf("%s, %dx%d: %d passes for %d bit planes", name, w, h, encoded.Passes, encoded.BitPlanes)
				}
			}
		}
	}
}

// TestABlockOfZerosIsNothing checks that a block with no coefficient is not
// coded at all: no passes, no bytes, which a packet says by leaving it out.
func TestABlockOfZerosIsNothing(t *testing.T) {
	encoded, err := EncodeBlock(make([]int32, 64), 8, 8, BandHH)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.Passes != 0 || encoded.BitPlanes != 0 || len(encoded.Data) != 0 {
		t.Errorf("a block of zeros was coded as %+v", encoded)
	}
}

// TestSparseBlocksCostLittle checks that the encoder uses the contexts and
// the run-length mode, and does not merely write bits the decoder can read
// back: 4096 coefficients of which a handful are not zero must take far
// fewer bytes than the coefficients themselves.
func TestSparseBlocksCostLittle(t *testing.T) {
	coefficients := make([]int32, 64*64)
	for i := 0; i < 20; i++ {
		coefficients[(i*211)%len(coefficients)] = int32(i%5) + 1
	}
	encoded, decoded := roundTrip(t, coefficients, 64, 64, BandHH, 0)
	for i := range coefficients {
		if decoded[i] != coefficients[i] {
			t.Fatalf("coefficient %d came back %d", i, decoded[i])
		}
	}
	if len(encoded.Data) > 120 {
		t.Errorf("20 small coefficients among 4096 took %d bytes", len(encoded.Data))
	}
}

// TestEncodeBlockRefusals checks what is not a code-block.
func TestEncodeBlockRefusals(t *testing.T) {
	if _, err := EncodeBlock(make([]int32, 5), 2, 3, BandLL); err == nil {
		t.Error("5 coefficients were taken for a 2 by 3 block")
	}
	if _, err := EncodeBlock(nil, 0, 0, BandLL); err == nil {
		t.Error("a block of no samples was encoded")
	}
	if _, err := EncodeBlock([]int32{-1 << 31}, 1, 1, BandLL); err == nil {
		t.Error("a coefficient of 32 bit planes was encoded")
	}
}

// FuzzBlockRoundTrip gives the encoder arbitrary coefficients and sizes.
func FuzzBlockRoundTrip(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 250, 0, 0, 7}, uint8(4), uint8(1))
	f.Add([]byte{0, 0, 0, 0}, uint8(2), uint8(3))
	f.Fuzz(func(t *testing.T, raw []byte, width uint8, band uint8) {
		w := int(width%16) + 1
		h := len(raw) / 2 / w
		if h == 0 || h > 64 {
			return
		}
		coefficients := make([]int32, w*h)
		for i := range coefficients {
			coefficients[i] = int32(int16(uint16(raw[2*i])<<8 | uint16(raw[2*i+1])))
		}
		_, decoded := roundTrip(t, coefficients, w, h, int(band%4), 1)
		for i := range coefficients {
			if decoded[i] != coefficients[i] {
				t.Fatalf("%dx%d band %d: coefficient %d came back %d, was %d", w, h, band%4, i, decoded[i], coefficients[i])
			}
		}
	})
}
