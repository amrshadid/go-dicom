package dataelem

import vrfacts "github.com/amrshadid/go-dicom/internal/vr"

// byteOrderSensitiveVRs maps each VR whose value is a sequence of multi-byte
// numbers to the width of one number. Text VRs and byte-oriented VRs (OB, UN)
// carry no byte order and are absent.
//
// AT is a pair of 16-bit values (group then element), so it swaps as 2-byte
// units like US.
//
// The widths are internal/vr's, which the file parser shares; this is the
// same table keyed by this package's VR type.
var byteOrderSensitiveVRs = func() map[VR]int {
	widths := make(map[VR]int, len(vrfacts.Widths))
	for name, width := range vrfacts.Widths {
		widths[VR(name)] = width
	}
	return widths
}()

// IsByteOrderSensitive reports whether a VR's value is affected by byte order.
func IsByteOrderSensitive(vr VR) bool {
	_, ok := byteOrderSensitiveVRs[vr]
	return ok
}

// SwapBytes reverses each width-byte unit of value in place. A width below two,
// or a value shorter than one unit, is left alone.
//
// SwapByteOrder takes the width from the VR, which is right for every value
// except Pixel Data: OW says two-byte words, and a 32- or 64-bit sample needs
// its whole width reversed (see PixelDataSwapWidth).
func SwapBytes(value []byte, width int) {
	vrfacts.SwapBytes(value, width)
}

// PixelDataSwapWidth gives the unit native Pixel Data must be reversed in when
// converting byte order, or 0 when it must not be reversed at all.
//
// Pixel Data is OW, so the VR's width is two bytes. That is right until Bits
// Allocated is 32 or 64, when reversing in two-byte units leaves each sample
// with its halves transposed: an RT Dose of 1249000 became 250085395 (#124).
// pydicom reads the raw bytes at the sample width, which is what archives
// expect whatever the VR alone implies.
//
// OB is bytes and is never reversed (PS3.5 7.3), whatever Bits Allocated says;
// a file with 32-bit samples in OB is non-conformant (PS3.5 A.2 requires OW
// above 8 bits) and reversing it would corrupt data the VR says to leave alone.
func PixelDataSwapWidth(vr VR, bitsAllocated int) int {
	return vrfacts.PixelDataSwapWidth(string(vr), bitsAllocated)
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
	vrfacts.SwapByteOrder(string(vr), value)
}
