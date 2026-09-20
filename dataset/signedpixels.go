package dataset

import "fmt"

// Reading pixel samples as the data set says they are signed.
//
// PixelArray and PixelArrayBySample choose their Go type from Bits Allocated
// alone, so a 16-bit image comes back as [][][]uint16 whether its samples are
// signed or not. That is their documented contract and changing it would break
// every type assertion already written against it, so the signed reading lives
// here instead, in a call a caller reaches for deliberately.

// PixelArrayInterpreted returns the pixel data with each sample typed and
// valued the way the data set says to read it.
//
// Interpreted means what it means on pixels.Accessor.GetInterpretedValue:
// (0028,0103) Pixel Representation applied, and nothing else. No rescale, no
// windowing. A data set that calls its samples signed yields a signed array; one
// that calls them unsigned yields exactly what PixelArray already returns, the
// same values under the same type, so there is no reason to branch on
// signedness at the call site.
//
//	Bits Allocated   unsigned        signed
//	8                [][][]uint8     [][][]int8
//	16               [][][]uint16    [][][]int16
//	32               [][][]uint32    [][][]int32
//
// The shape is PixelArray's: color samples are flattened into the column
// dimension, so the two calls can be swapped without reshaping.
//
// Why this is not simply what PixelArray does: its return type is part of its
// documented contract, and code asserting [][][]uint16 on a signed image would
// begin to panic. Panicking is better than silently reading -2016 as 63520, but
// it is still a break, and it belongs to a major version.
func (ds *Dataset) PixelArrayInterpreted() (interface{}, error) {
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	if info.PixelRepresentation != 1 {
		// Unsigned: the existing reading is already right, down to the type.
		return ds.PixelArray()
	}

	unsigned, err := ds.PixelArray()
	if err != nil {
		return nil, err
	}

	// Bits Stored, not Bits Allocated, carries the sign. For a 13-bit sample in
	// a 16-bit word the sign bit is bit 12, so a stored 0x1830 is -2000 and not
	// the 6192 a plain conversion would give.
	bits := info.BitsStored
	if bits <= 0 || bits > info.BitsAllocated {
		bits = info.BitsAllocated
	}

	switch samples := unsigned.(type) {
	case [][][]uint8:
		return mapSamples(samples, func(v uint8) int8 {
			return int8(signExtendSample(uint64(v), bits))
		}), nil
	case [][][]uint16:
		return mapSamples(samples, func(v uint16) int16 {
			return int16(signExtendSample(uint64(v), bits))
		}), nil
	case [][][]uint32:
		return mapSamples(samples, func(v uint32) int32 {
			return int32(signExtendSample(uint64(v), bits))
		}), nil
	default:
		return nil, fmt.Errorf("pixel data of %T cannot be read as signed samples; "+
			"Bits Allocated is %d", unsigned, info.BitsAllocated)
	}
}

// signExtendSample reads the low `bits` of value as a two's complement number.
//
// It is the same rule pixels.Accessor.signExtend applies, and the reason a Go
// conversion will not do: the sign bit sits at Bits Stored, which may be well
// below the width of the word the sample is stored in.
func signExtendSample(value uint64, bits int) int64 {
	if bits <= 0 || bits >= 64 {
		return int64(value)
	}
	sign := uint64(1) << uint(bits-1)
	value &= (uint64(1) << uint(bits)) - 1
	if value&sign != 0 {
		return int64(value) - int64(uint64(1)<<uint(bits))
	}
	return int64(value)
}

// mapSamples rebuilds a frames/rows/columns array with each sample converted.
func mapSamples[In any, Out any](in [][][]In, convert func(In) Out) [][][]Out {
	out := make([][][]Out, len(in))
	for f, frame := range in {
		out[f] = make([][]Out, len(frame))
		for r, row := range frame {
			out[f][r] = make([]Out, len(row))
			for c, v := range row {
				out[f][r][c] = convert(v)
			}
		}
	}
	return out
}
