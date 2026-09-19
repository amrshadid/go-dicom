package dataelem

// byteOrderSensitiveVRs maps each VR whose value is a sequence of multi-byte
// numbers to the width of one number. Text VRs and byte-oriented VRs (OB, UN)
// carry no byte order and are absent.
//
// AT is a pair of 16-bit values (group then element), so it swaps as 2-byte
// units like US.
var byteOrderSensitiveVRs = map[VR]int{
	US: 2, SS: 2, OW: 2, AT: 2,
	UL: 4, SL: 4, FL: 4, OL: 4, OF: 4,
	FD: 8, OD: 8, SV: 8, UV: 8, OV: 8,
}

// IsByteOrderSensitive reports whether a VR's value is affected by byte order.
func IsByteOrderSensitive(vr VR) bool {
	_, ok := byteOrderSensitiveVRs[vr]
	return ok
}

// SwapByteOrder converts a value between big and little endian in place,
// reversing each multi-byte number according to the VR's width.
//
// The conversion is its own inverse, so one function serves both reading and
// writing. Values of byte-order-insensitive VRs are left untouched.
//
// This exists because Dataset stores values as opaque bytes with no record of
// how to interpret them, and everything downstream — pixel access, numeric
// conversion, the JSON model — reads them as little endian. Big endian values
// are converted once on read so that a data set means the same thing whatever
// the file's encoding, and converted back on write when the target syntax is
// big endian.
func SwapByteOrder(vr VR, value []byte) {
	width, sensitive := byteOrderSensitiveVRs[vr]
	if !sensitive || len(value) < width {
		return
	}

	for off := 0; off+width <= len(value); off += width {
		for i, j := off, off+width-1; i < j; i, j = i+1, j-1 {
			value[i], value[j] = value[j], value[i]
		}
	}
}
