package dataelem

import (
	"bytes"
	"testing"
)

// Byte order is where a wrong answer looks most like a right one: swapping the
// halves of a 32-bit value gives another plausible number, and an RT Dose of
// 1249000 read back as 250085395 is not obviously wrong to anything downstream.
//
// This table had no tests at all, and it was missing the three 64-bit value
// representations DICOM added in 2018. A big-endian file carrying an SV, UV or
// OV would have had its values left in the wrong order — silently, since a VR
// absent from the table is treated as carrying no byte order rather than as
// unknown.

// TestWidthsMatchTheValueRepresentations pins the width of each entry.
func TestWidthsMatchTheValueRepresentations(t *testing.T) {
	want := map[VR]int{
		US: 2, SS: 2, OW: 2, AT: 2,
		UL: 4, SL: 4, FL: 4, OL: 4, OF: 4,
		FD: 8, OD: 8, SV: 8, UV: 8, OV: 8,
	}
	for vr, width := range want {
		got, ok := byteOrderSensitiveVRs[vr]
		if !ok {
			t.Errorf("%s is not treated as byte-order sensitive; a big-endian file "+
				"carrying it would keep its values in the wrong order", vr)
			continue
		}
		if got != width {
			t.Errorf("%s swaps in %d-byte units, want %d", vr, got, width)
		}
	}
	for vr := range byteOrderSensitiveVRs {
		if _, expected := want[vr]; !expected {
			t.Errorf("%s is treated as byte-order sensitive and is not in the expected set", vr)
		}
	}
}

// TestTextAndByteVRsAreNotSwapped covers the other half.
//
// Swapping a text value would reverse its characters two at a time, and OB and
// UN are byte streams whose meaning the VR does not describe.
func TestTextAndByteVRsAreNotSwapped(t *testing.T) {
	for _, vr := range []VR{PN, LO, UI, CS, DA, TM, DS, IS, OB, UN, UT, ST, LT, SH, AE, AS} {
		if IsByteOrderSensitive(vr) {
			t.Errorf("%s is treated as byte-order sensitive", vr)
		}
		value := []byte("Doe^John")
		original := append([]byte(nil), value...)
		SwapByteOrder(vr, value)
		if !bytes.Equal(value, original) {
			t.Errorf("%s value was changed: %q became %q", vr, original, value)
		}
	}
}

// TestSwapIsItsOwnInverse is the property the single function relies on.
func TestSwapIsItsOwnInverse(t *testing.T) {
	for vr := range byteOrderSensitiveVRs {
		value := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		original := append([]byte(nil), value...)

		SwapByteOrder(vr, value)
		if bytes.Equal(value, original) {
			t.Errorf("%s: swapping changed nothing", vr)
		}
		SwapByteOrder(vr, value)
		if !bytes.Equal(value, original) {
			t.Errorf("%s: swapping twice gave % X, want % X", vr, value, original)
		}
	}
}

// TestSwapWidths checks each width reverses the right unit.
func TestSwapWidths(t *testing.T) {
	tests := []struct {
		vr    VR
		value []byte
		want  []byte
	}{
		{US, []byte{0x01, 0x02, 0x03, 0x04}, []byte{0x02, 0x01, 0x04, 0x03}},
		{UL, []byte{0x01, 0x02, 0x03, 0x04}, []byte{0x04, 0x03, 0x02, 0x01}},
		{SV, []byte{1, 2, 3, 4, 5, 6, 7, 8}, []byte{8, 7, 6, 5, 4, 3, 2, 1}},
		{UV, []byte{1, 2, 3, 4, 5, 6, 7, 8}, []byte{8, 7, 6, 5, 4, 3, 2, 1}},
		{OV, []byte{1, 2, 3, 4, 5, 6, 7, 8}, []byte{8, 7, 6, 5, 4, 3, 2, 1}},
		// An attribute tag is a pair of 16-bit values, not one 32-bit value:
		// (0028,0010) stays (0028,0010) rather than becoming (1000,2800).
		{AT, []byte{0x28, 0x00, 0x10, 0x00}, []byte{0x00, 0x28, 0x00, 0x10}},
	}

	for _, tc := range tests {
		t.Run(string(tc.vr), func(t *testing.T) {
			value := append([]byte(nil), tc.value...)
			SwapByteOrder(tc.vr, value)
			if !bytes.Equal(value, tc.want) {
				t.Errorf("% X became % X, want % X", tc.value, value, tc.want)
			}
		})
	}
}

// TestPartialTrailingValueIsLeftAlone covers a value that is not a whole number
// of units, which a truncated file produces.
//
// The trailing bytes are left as they are rather than reversed as a short unit,
// since reversing them would invent a value the file does not contain.
func TestPartialTrailingValueIsLeftAlone(t *testing.T) {
	value := []byte{1, 2, 3, 4, 5} // one whole UL and one spare byte
	SwapByteOrder(UL, value)

	if want := []byte{4, 3, 2, 1, 5}; !bytes.Equal(value, want) {
		t.Errorf("got % X, want % X", value, want)
	}
}

// TestShorterThanOneUnitIsLeftAlone covers the same at the start.
func TestShorterThanOneUnitIsLeftAlone(t *testing.T) {
	value := []byte{1, 2, 3}
	SwapByteOrder(UL, value)
	if want := []byte{1, 2, 3}; !bytes.Equal(value, want) {
		t.Errorf("got % X, want % X", value, want)
	}
}
