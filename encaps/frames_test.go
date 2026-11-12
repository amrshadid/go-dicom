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
	return encaps.Frames(enc, numberOfFrames)
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
