package compress

import (
	"encoding/binary"
	"fmt"
)

// atBitsAllocated takes a frame packed at its codestream's own width, one byte
// a sample at a precision of 8 or fewer and two above, and returns it at the
// width Bits Allocated states.
//
// A narrower sample is widened with its bit pattern kept, as the two-byte
// packing already keeps a 12-bit sample inside 16: the value is the same, and
// Pixel Representation decides above this how its top bits are read.
//
// A precision wider than Bits Allocated is refused. Writing it two bytes a
// sample made a frame twice the size the data set describes, and cutting it to
// one would keep the low bits of every sample, a picture that looks like one.
func atBitsAllocated(packed []byte, precision, bitsAllocated int) ([]byte, error) {
	if bitsAllocated != 8 && bitsAllocated != 16 && bitsAllocated != 32 {
		return nil, fmt.Errorf("a sample of %d bits allocated is not 8, 16 or 32 bits wide", bitsAllocated)
	}
	if precision > bitsAllocated {
		return nil, fmt.Errorf("the frame is coded at %d bits, more than the %d bits allocated to a sample",
			precision, bitsAllocated)
	}
	from := 1
	if precision > 8 {
		from = 2
	}
	to := bitsAllocated / 8
	if from == to {
		return packed, nil
	}

	samples := len(packed) / from
	out := make([]byte, samples*to)
	for i := 0; i < samples; i++ {
		v := uint32(packed[i*from])
		if from == 2 {
			v |= uint32(packed[i*from+1]) << 8
		}
		if to == 2 {
			binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
		} else {
			binary.LittleEndian.PutUint32(out[i*4:], v)
		}
	}
	return out, nil
}
