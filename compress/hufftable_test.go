package compress

import "testing"

// TestHuffmanCodeLengthsWithoutCodes covers the canonical code assignment.
//
// The code lengthens by one bit at every length, including a length no code
// has. Skipping that shift left every longer code one bit short, so the table
// decoded a different symbol than the encoder wrote.
//
// It went unseen because it needs an interior gap. A leading run of empty
// lengths is harmless — shifting zero leaves zero — and none of the lossless
// fixtures had a gap after their first code. The DC table of a 12-bit JPEG
// Extended frame has none of length 2, and the first symbol of the first block
// already decoded wrongly.
//
// The builder is shared with the JPEG Lossless decoder, so it was wrong there
// too, on any stream whose tables happened to skip a length.
func TestHuffmanCodeLengthsWithoutCodes(t *testing.T) {
	// One code of length 1, none of length 2, three of length 3. Canonically
	// that is 0, then 100, 101, 110: the length-3 codes start at 4, not at 2.
	counts := []byte{1, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	values := []byte{0xA0, 0xA1, 0xA2, 0xA3}

	table, err := buildHuffTable(counts, values)
	if err != nil {
		t.Fatalf("buildHuffTable: %v", err)
	}
	if table.mincode[3] != 4 || table.maxcode[3] != 6 {
		t.Fatalf("codes of length 3 run %d..%d, want 4..6; the empty length 2 did not "+
			"lengthen the code", table.mincode[3], table.maxcode[3])
	}

	// 0, 100, 101, 110 packed high bit first: 0100 1011 10 and padding.
	r := &bitReader{data: []byte{0x4B, 0x80}}
	for i, want := range values {
		got, err := r.decodeHuff(table)
		if err != nil {
			t.Fatalf("symbol %d: %v", i, err)
		}
		if got != want {
			t.Fatalf("symbol %d decoded as %02X, want %02X", i, got, want)
		}
	}
}

// TestHuffmanTableWithNoLeadingCodes checks the case that always worked keeps
// working: empty lengths before the first code shift a zero, which is a no-op.
func TestHuffmanTableWithNoLeadingCodes(t *testing.T) {
	counts := []byte{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	table, err := buildHuffTable(counts, []byte{0x10, 0x11})
	if err != nil {
		t.Fatalf("buildHuffTable: %v", err)
	}
	if table.mincode[3] != 0 || table.maxcode[3] != 1 {
		t.Errorf("codes of length 3 run %d..%d, want 0..1", table.mincode[3], table.maxcode[3])
	}
}
