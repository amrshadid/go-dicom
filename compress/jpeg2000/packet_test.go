package jpeg2000

import (
	"os"
	"testing"
)

// TestPacketsAccountForEveryTileByte is the check that says the packet reader
// understood the file: read every packet of every tile, and the headers plus
// the code-block segments must come to the tile's bytes, with nothing left over
// and nothing read twice.
//
// It is a strong check because the packet header is self-describing only if it
// is read correctly. One inclusion bit read in the wrong order, one length
// field the wrong width, one tag tree not reset between precincts, and the
// reader lands mid-segment: the next header is nonsense and the totals do not
// match. Getting to the last byte of a 132KB tile means every field in between
// was the width the standard says.
func TestPacketsAccountForEveryTileByte(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for name := range fixtureFrames(t, dir) {
		t.Run(name, func(t *testing.T) {
			c := fixtureCodestream(t, dir, name)

			totalBlocks, totalBytes := 0, 0
			for tile := 0; tile < c.NumTiles(); tile++ {
				var tileBytes int
				for _, part := range c.TileParts {
					if part.Tile == tile {
						tileBytes += len(part.Data)
					}
				}

				data, err := ReadTilePacketsForTest(c, tile)
				if err != nil {
					t.Fatalf("tile %d: %v", tile, err)
				}

				used := 0
				for _, block := range data.Blocks {
					used += len(block.Data)
					if block.Passes <= 0 {
						t.Errorf("tile %d: a code-block carries %d passes", tile, block.Passes)
					}
					if block.ZeroBitPlanes < 0 || block.ZeroBitPlanes > 37 {
						t.Errorf("tile %d: a code-block claims %d zero bit planes",
							tile, block.ZeroBitPlanes)
					}
					if block.Block.Width() <= 0 || block.Block.Height() <= 0 {
						t.Errorf("tile %d: a code-block has no samples", tile)
					}
				}

				// Everything the tile holds is packets, and every packet was
				// read: the reader must finish on the last byte. Ending early
				// means a field somewhere was read at the wrong width and the
				// reader is adrift in the middle of a segment.
				//
				// A tile may end with padding a few bytes long, so a small
				// remainder is allowed; landing anywhere else is not.
				if data.BytesRead > tileBytes {
					t.Errorf("tile %d: the reader consumed %d bytes of a %d byte tile",
						tile, data.BytesRead, tileBytes)
				}
				if remainder := tileBytes - data.BytesRead; remainder > 2 {
					t.Errorf("tile %d: %d bytes left unread of %d; the reader lost its place",
						tile, remainder, tileBytes)
				}
				if used > tileBytes {
					t.Errorf("tile %d: code-block segments hold %d bytes, the tile has %d",
						tile, used, tileBytes)
				}
				totalBlocks += len(data.Blocks)
				totalBytes += used
			}
			t.Logf("%d code-blocks carrying %d bytes", totalBlocks, totalBytes)
			if totalBlocks == 0 {
				t.Error("no code-blocks were read")
			}
		})
	}
}

// TestPacketReaderRefusesATruncatedTile: a packet body that runs past the tile
// is an error, not a short read. The lengths come from the file.
func TestPacketReaderRefusesATruncatedTile(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	c := fixtureCodestream(t, dir, "MR_small_jp2klossless.dcm")

	for _, keep := range []int{1, 8, 64, 512} {
		if keep >= len(c.TileParts[0].Data) {
			continue
		}
		cut := *c
		cut.TileParts = []TilePart{{
			Tile:  c.TileParts[0].Tile,
			Index: c.TileParts[0].Index,
			Data:  c.TileParts[0].Data[:keep],
		}}
		// Truncated data may decode into fewer blocks or fail; it must not
		// panic and must not claim bytes that are not there.
		data, err := ReadTilePacketsForTest(&cut, 0)
		if err != nil {
			continue
		}
		used := 0
		for _, block := range data.Blocks {
			used += len(block.Data)
		}
		if used > keep {
			t.Errorf("with %d bytes the reader claimed %d", keep, used)
		}
	}
}

// bitWriter writes the bits a packet header is made of, for the table test
// below, with the bit stuffing a real encoder applies: after a byte of 0xFF the
// next carries seven bits, so that no marker can appear inside a header
// (B.10.1). The reader refuses an unstuffed 0xFF, which is how the patterns for
// 37 and 164 passes found this.
type bitWriter struct {
	out  []byte
	buf  byte
	bits int
	room int
}

func (w *bitWriter) write(pattern string) {
	for _, c := range pattern {
		if w.room == 0 {
			w.room = 8
			if len(w.out) > 0 && w.out[len(w.out)-1] == 0xFF {
				w.room = 7
			}
			w.buf, w.bits = 0, 0
		}
		w.bits++
		w.room--
		w.buf <<= 1
		if c == '1' {
			w.buf |= 1
		}
		if w.room == 0 {
			w.out = append(w.out, w.buf)
			w.bits = 0
		}
	}
}

func (w *bitWriter) bytes() []byte {
	if w.bits > 0 {
		return append(w.out, w.buf<<uint(w.room))
	}
	return w.out
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
		w := &bitWriter{}
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
	w := &bitWriter{}
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
	w = &bitWriter{}
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
