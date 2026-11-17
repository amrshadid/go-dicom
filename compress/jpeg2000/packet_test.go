package jpeg2000

import (
	"testing"
)

// write puts a pattern of bits, written as '0' and '1', into a header.
func (w *headerWriter) write(pattern string) {
	for _, c := range pattern {
		bit := 0
		if c == '1' {
			bit = 1
		}
		w.writeBit(bit)
	}
}

// TestPassCountTable checks the code of Table B.4 bit pattern by bit pattern.
//
// This one is written out in full because I had it backwards: I read a 1 as
// "one pass" when a 0 ends the code, and every fixture then decoded a plausible
// but wrong packet — one code-block of 80 bytes where a 2x2 band cannot hold
// 80 bytes, and five packets that looked empty because the reader was adrift
// in the middle of a segment.
func TestPassCountTable(t *testing.T) {
	tests := []struct {
		bits  string
		want  int
		about string
	}{
		{"0", 1, "a single 0 ends the code"},
		{"10", 2, ""},
		{"1100", 3, "two more bits, 0 to 2"},
		{"1101", 4, ""},
		{"1110", 5, ""},
		{"111100000", 6, "escape to five bits, 0 to 30"},
		{"111100001", 7, ""},
		{"111111110", 36, "the last five-bit value"},
		{"1111111110000000", 37, "escape again to seven bits"},
		{"1111111111111111", 164, "the most passes the code can carry"},
	}
	for _, tc := range tests {
		w := &headerWriter{}
		w.write(tc.bits)
		got, err := readPassCount(newBitReader(w.bytes()))
		if err != nil {
			t.Errorf("%s: %v", tc.bits, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %d passes, want %d %s", tc.bits, got, tc.want, tc.about)
		}
	}
}

// TestSegmentLengthFieldGrows covers B.10.7.1: the length field starts at three
// bits and each 1 bit before it adds one, so a block whose contributions grow
// does not pay for a wide field in every packet. The width also grows with the
// pass count.
func TestSegmentLengthFieldGrows(t *testing.T) {
	// No growth: three bits plus log2(1) = 0, value 5.
	w := &headerWriter{}
	w.write("0" + "101")
	b := &blockState{lblock: 3}
	got, err := readSegmentLength(newBitReader(w.bytes()), b, 1)
	if err != nil || got != 5 {
		t.Errorf("length = %d (%v), want 5", got, err)
	}
	if b.lblock != 3 {
		t.Errorf("lblock grew to %d with no signal", b.lblock)
	}

	// Two 1 bits: five bits of length, and eight passes adds three more.
	w = &headerWriter{}
	w.write("110" + "10000000")
	b = &blockState{lblock: 3}
	got, err = readSegmentLength(newBitReader(w.bytes()), b, 8)
	if err != nil || got != 128 {
		t.Errorf("length = %d (%v), want 128", got, err)
	}
	if b.lblock != 5 {
		t.Errorf("lblock = %d, want 5", b.lblock)
	}
}
