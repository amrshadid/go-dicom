package compress

import (
	"math/rand"
	"testing"
)

// TestGradientBucketsAreQuantize proves the table the JPEG-LS decoder reads a
// gradient's bucket from holds what quantize gives, for every gradient two
// samples can have and a few past either end, lossless and near-lossless, at
// 8, 12 and 16 bits.
func TestGradientBucketsAreQuantize(t *testing.T) {
	for _, maxVal := range []int{3, 255, 4095, 65535} {
		for _, near := range []int{0, 1, 3} {
			if near > maxVal/2 {
				continue
			}
			p := &jpeglsParams{maxVal: maxVal, near: near}
			if err := p.computeDefaults(); err != nil {
				t.Fatalf("MAXVAL %d NEAR %d: %v", maxVal, near, err)
			}
			g := p.buckets()
			for d := int32(-maxVal - 3); d <= int32(maxVal+3); d++ {
				if got, want := g.of(p, d), p.quantize(d); got != want {
					t.Fatalf("MAXVAL %d NEAR %d: bucket of %d is %d, quantize gives %d", maxVal, near, d, got, want)
				}
			}
		}
	}
	var none gradientBuckets
	p := &jpeglsParams{maxVal: 255}
	if err := p.computeDefaults(); err != nil {
		t.Fatal(err)
	}
	if got, want := none.of(p, -40), p.quantize(-40); got != want {
		t.Errorf("without a table the bucket of -40 is %d, quantize gives %d", got, want)
	}
}

// entropyLike is random data with the bytes a bit reader treats specially put
// in often: 0xFF, the zero that follows it in a T.81 scan, and runs of zeros,
// which are long unary codes.
func entropyLike(rng *rand.Rand, n int) []byte {
	data := make([]byte, n)
	for i := range data {
		switch rng.Intn(8) {
		case 0:
			data[i] = 0xFF
		case 1, 2:
			data[i] = 0x00
		case 3:
			data[i] = byte(1 << rng.Intn(8))
		default:
			data[i] = byte(rng.Intn(256))
		}
	}
	return data
}

// TestJPEGLSReaderReadsManyBitsAsItReadsOne proves readBits and readUnary of
// the JPEG-LS bit reader give what reading the same bits one at a time gives,
// and leave the reader at the same bit, through stuffed bytes, past a marker
// and past the end of the data.
func TestJPEGLSReaderReadsManyBitsAsItReadsOne(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	consumed := func(r *jpeglsBitReader) int { return r.loaded + 8*r.overrun - int(r.n) }
	for round := 0; round < 300; round++ {
		data := entropyLike(rng, 1+rng.Intn(200))
		fast, slow := &jpeglsBitReader{data: data}, &jpeglsBitReader{data: data}
		for step := 0; step < 400; step++ {
			if rng.Intn(2) == 0 {
				count := rng.Intn(33)
				var want int32
				for i := 0; i < count; i++ {
					want = want<<1 | slow.readBit()
				}
				if got := fast.readBits(count); got != want {
					t.Fatalf("round %d step %d: readBits(%d) = %#x, bit by bit %#x", round, step, count, got, want)
				}
			} else {
				limit := 1 + rng.Intn(40)
				want, failed := 0, false
				for slow.readBit() == 0 {
					want++
					if want > limit {
						failed = true
						break
					}
				}
				got, err := fast.readUnary(limit)
				if (err != nil) != failed {
					t.Fatalf("round %d step %d: readUnary(%d) error %v, bit by bit failed = %v", round, step, limit, err, failed)
				}
				if failed {
					break // a decoder stops here; where each reader stands is not compared
				}
				if got != want {
					t.Fatalf("round %d step %d: readUnary(%d) = %d, bit by bit %d", round, step, limit, got, want)
				}
			}
			if consumed(fast) != consumed(slow) {
				t.Fatalf("round %d step %d: %d bits consumed, bit by bit %d", round, step, consumed(fast), consumed(slow))
			}
		}
	}
}

// TestJPEGReaderReadsManyBitsAsItReadsOne proves the same of readBits of the
// T.81 bit reader, which the lossless and the extended JPEG decoders share.
func TestJPEGReaderReadsManyBitsAsItReadsOne(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for round := 0; round < 300; round++ {
		data := entropyLike(rng, 1+rng.Intn(200))
		fast, slow := &bitReader{data: data}, &bitReader{data: data}
		for step := 0; step < 400; step++ {
			count := rng.Intn(17)
			var want int32
			for i := 0; i < count; i++ {
				want = want<<1 | slow.readBit()
			}
			if got := fast.readBits(count); got != want {
				t.Fatalf("round %d step %d: readBits(%d) = %#x, bit by bit %#x", round, step, count, got, want)
			}
		}
	}
}
