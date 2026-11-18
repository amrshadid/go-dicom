package encaps_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/encaps"
)

// A frame may be stored in several fragments (PS3.5 A.4), and the decode
// path took each fragment for a frame, so such an image did not decode (#238).

// encapsulate builds an encapsulated value: the Basic Offset Table item, then
// one item per fragment.
func encapsulate(offsets []uint32, fragments ...[]byte) []byte {
	var buf bytes.Buffer
	item := func(data []byte) {
		buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
		buf.Write(data)
	}
	table := make([]byte, 4*len(offsets))
	for i, o := range offsets {
		binary.LittleEndian.PutUint32(table[4*i:], o)
	}
	item(table)
	for _, f := range fragments {
		item(f)
	}
	buf.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})
	return buf.Bytes()
}

func framesOf(t *testing.T, value []byte, numberOfFrames int) ([][]byte, error) {
	t.Helper()
	enc, err := encaps.NewParser(bytes.NewReader(value), true).ParseEncapsulatedData()
	if err != nil {
		t.Fatalf("ParseEncapsulatedData: %v", err)
	}
	frames, err := encaps.Frames(enc, numberOfFrames)

	// FrameRanges makes the same decision from where the fragments lie, so
	// every case here holds it to the same answer.
	ranges, rangesErr := encaps.FrameRanges(bytes.NewReader(value), items(value), nil, numberOfFrames)
	if (err == nil) != (rangesErr == nil) {
		t.Fatalf("Frames says %v and FrameRanges says %v", err, rangesErr)
	}
	for i := range ranges {
		if got := join(value, ranges[i]); !bytes.Equal(got, frames[i]) {
			t.Errorf("FrameRanges frame %d is %q, Frames %q", i, got, frames[i])
		}
	}
	return frames, err
}

// items finds the items of an encapsulated value, each as the range of its
// content, as filereader records them.
func items(value []byte) []encaps.Range {
	var found []encaps.Range
	for at := 0; at+8 <= len(value); {
		if value[at+2] == 0xDD {
			break
		}
		length := int(binary.LittleEndian.Uint32(value[at+4:]))
		found = append(found, encaps.Range{Offset: int64(at + 8), Length: int64(length)})
		at += 8 + length
	}
	return found
}

// join reads a frame's fragments out of the value.
func join(value []byte, ranges []encaps.Range) []byte {
	var out []byte
	for _, r := range ranges {
		out = append(out, value[r.Offset:r.Offset+r.Length]...)
	}
	return out
}

// TestExtendedOffsetTableComesFirst: the Extended Offset Table (PS3.3
// C.7.6.3.1.8) counts as the basic one does, from the first fragment's item
// tag. The Basic Offset Table is empty here, as the standard requires beside
// an extended one, and the fragments carry no end-of-image markers, so
// nothing else could divide them.
func TestExtendedOffsetTableComesFirst(t *testing.T) {
	value := encapsulate(nil, []byte("aaaa"), []byte("bb"), []byte("cccc"), []byte("dd"))
	ranges, err := encaps.FrameRanges(bytes.NewReader(value), items(value), []uint64{0, 22}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 2 || string(join(value, ranges[0])) != "aaaabb" || string(join(value, ranges[1])) != "ccccdd" {
		t.Fatalf("frames %v", ranges)
	}
	// Each range is a fragment's content in the value: where a caller reads.
	if ranges[1][0] != (encaps.Range{Offset: 8 + 8 + 4 + 8 + 2 + 8, Length: 4}) {
		t.Errorf("the second frame starts at %+v", ranges[1][0])
	}

	// An extended table that does not fit is passed over, like a basic one,
	// and here nothing else divides the fragments.
	if _, err := encaps.FrameRanges(bytes.NewReader(value), items(value), []uint64{0, 5}, 2); err == nil {
		t.Error("an extended offset inside a fragment was used")
	}
}

func wantFrames(t *testing.T, got [][]byte, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d frames, want %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("frame %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// TestOneFrameInSeveralFragments is pydicom's examples_jpeg2k.dcm: no table,
// one frame, three fragments.
func TestOneFrameInSeveralFragments(t *testing.T) {
	got, err := framesOf(t, encapsulate(nil, []byte("aaaa"), []byte("bbbb"), []byte("cc")), 1)
	wantFrames(t, got, err, "aaaabbbbcc")
}

// TestFragmentPerFrame is the common layout, which has to stay as it was.
func TestFragmentPerFrame(t *testing.T) {
	got, err := framesOf(t, encapsulate(nil, []byte("aaaa"), []byte("bbbb")), 2)
	wantFrames(t, got, err, "aaaa", "bbbb")
}

// TestBasicOffsetTableGroupsFragments: the table gives where each frame's
// first fragment starts, counted from the first fragment's item tag, so each
// fragment's 8-byte header is in the count. The second frame here starts at
// (8 + 4) + (8 + 2) = 22.
func TestBasicOffsetTableGroupsFragments(t *testing.T) {
	value := encapsulate([]uint32{0, 22}, []byte("aaaa"), []byte("bb"), []byte("cccc"), []byte("dd"))
	got, err := framesOf(t, value, 2)
	wantFrames(t, got, err, "aaaabb", "ccccdd")

	// The table decides even when the frame count is not known.
	got, err = framesOf(t, value, 0)
	wantFrames(t, got, err, "aaaabb", "ccccdd")
}

// TestEndOfImageMarkerSeparatesFrames: with no table and more fragments than
// frames, a JPEG frame ends at the fragment that ends in FFD9, which an
// encoder may follow with a pad byte to keep the length even.
func TestEndOfImageMarkerSeparatesFrames(t *testing.T) {
	value := encapsulate(nil,
		[]byte{0xFF, 0xD8, 1, 2}, []byte{3, 4, 0xFF, 0xD9},
		[]byte{0xFF, 0xD8, 5, 6}, []byte{7, 0xFF, 0xD9, 0x00})
	got, err := framesOf(t, value, 2)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	if len(got) != 2 || !bytes.Equal(got[0], []byte{0xFF, 0xD8, 1, 2, 3, 4, 0xFF, 0xD9}) ||
		!bytes.Equal(got[1], []byte{0xFF, 0xD8, 5, 6, 7, 0xFF, 0xD9, 0x00}) {
		t.Errorf("frames %v", got)
	}
}

// TestFragmentsThatCannotBeAssigned: guessing would hand a decoder half of
// one frame and the start of the next.
func TestFragmentsThatCannotBeAssigned(t *testing.T) {
	// Three fragments, two frames, no table and no markers.
	if got, err := framesOf(t, encapsulate(nil, []byte("aaaa"), []byte("bbbb"), []byte("cccc")), 2); err == nil {
		t.Errorf("three unmarked fragments were split into %d frames", len(got))
	}
	// Fewer fragments than frames.
	if _, err := framesOf(t, encapsulate(nil, []byte("aaaa")), 2); err == nil {
		t.Error("one fragment was accepted as two frames")
	}
	// A table that names a place no fragment starts at settles nothing, and
	// neither do three unmarked fragments for two frames.
	bad := encapsulate([]uint32{0, 13}, []byte("aaaa"), []byte("bbbb"), []byte("cccc"))
	if _, err := framesOf(t, bad, 2); err == nil {
		t.Error("an offset inside a fragment was accepted")
	}
}

// TestUnusableTableFallsBackToTheCounts: a table written wrongly is not
// rare, and a fragment for each frame needs no table to read.
func TestUnusableTableFallsBackToTheCounts(t *testing.T) {
	got, err := framesOf(t, encapsulate([]uint32{0, 13}, []byte("aaaa"), []byte("bbbb")), 2)
	wantFrames(t, got, err, "aaaa", "bbbb")
}

// TestExtractFrameWithATable: a fragment inside the frame's range matched two
// conditions and was appended twice, and the offsets were compared with a
// count that left the item headers out.
func TestExtractFrameWithATable(t *testing.T) {
	value := encapsulate([]uint32{0, 22}, []byte("aaaa"), []byte("bb"), []byte("cccc"), []byte("dd"))
	enc, err := encaps.NewParser(bytes.NewReader(value), true).ParseEncapsulatedData()
	if err != nil {
		t.Fatal(err)
	}
	extractor := encaps.NewExtractor(enc)
	for i, want := range []string{"aaaabb", "ccccdd"} {
		got, err := extractor.ExtractFrame(i)
		if err != nil {
			t.Fatalf("ExtractFrame(%d): %v", i, err)
		}
		if string(got) != want {
			t.Errorf("frame %d is %q, want %q", i, got, want)
		}
	}
	if _, err := extractor.ExtractFrame(2); err == nil {
		t.Error("frame 2 of 2 was extracted")
	}
}
