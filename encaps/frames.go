package encaps

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/compress"
)

// Frames groups the fragments of encapsulated pixel data into frames, each
// returned as one compressed codestream.
//
// A frame may be stored in several fragments (PS3.5 A.4), so a fragment is
// not a frame. numberOfFrames is the image's Number of Frames, or 0 when it
// is not known. The rules, in order:
//
//   - A Basic Offset Table with an entry per frame decides: each entry is
//     where a frame's first fragment starts.
//   - As many fragments as frames: one each.
//   - One frame: every fragment.
//   - More fragments than frames: a frame ends with the fragment that ends
//     in the JPEG end-of-image marker, which JPEG, JPEG-LS and JPEG 2000
//     codestreams all close with.
//
// Anything else is an error. Splitting by guesswork hands a decoder half of
// one frame and the start of the next, and it fails somewhere inside with
// nothing to say why.
func Frames(encData *compress.EncapsulatedData, numberOfFrames int) ([][]byte, error) {
	if encData == nil || len(encData.Fragments) == 0 {
		return nil, fmt.Errorf("encapsulated pixel data holds no fragments")
	}
	fragments := encData.Fragments

	offsets := encData.FragmentOffsets
	if len(offsets) != len(fragments) {
		// Fragments not parsed from a value: each follows the last, with its
		// 8-byte item header.
		offsets = make([]uint64, len(fragments))
		var at uint64
		for i, fragment := range fragments {
			offsets[i] = at
			at += uint64(len(fragment)) + 8
		}
	}
	table := make([]uint64, len(encData.BasicOffsetTable))
	for i, entry := range encData.BasicOffsetTable {
		table[i] = uint64(entry)
	}

	first, err := frameSpans(offsets, [][]uint64{table}, numberOfFrames,
		func(i int) (bool, error) { return endsImage(fragments[i]), nil })
	if err != nil {
		return nil, err
	}
	frames := make([][]byte, len(first)-1)
	for i := range frames {
		if first[i+1]-first[i] == 1 {
			frames[i] = fragments[first[i]]
			continue
		}
		frames[i] = bytes.Join(fragments[first[i]:first[i+1]], nil)
	}
	return frames, nil
}

// Range is a run of bytes in a file: Length bytes starting at Offset.
type Range struct {
	Offset, Length int64
}

// FrameRanges says where each frame of encapsulated pixel data lies in a
// file, without reading the fragments that hold it: frame i is the contents
// of the fragments in result[i], in order, joined.
//
// items are the items of the Pixel Data value as they lie in src, each the
// content that follows its 8-byte item header, the Basic Offset Table first
// (filereader records them in ElementPosition.Fragments). extendedOffsets is
// the Extended Offset Table (7FE0,0001), or nil when the file has none. The
// Basic Offset Table is read from src; so are the last bytes of a fragment
// when only its end-of-image marker can say where a frame ends.
//
// The rules are those of Frames, with the Extended Offset Table before the
// Basic one: an offset table with an entry per frame decides; then as many
// fragments as frames; then one frame in every fragment; then the
// end-of-image marker. Both tables count from the first byte of the first
// fragment's item tag (PS3.5 A.4, PS3.3 C.7.6.3.1.8).
func FrameRanges(src io.ReaderAt, items []Range, extendedOffsets []uint64, numberOfFrames int) ([][]Range, error) {
	if len(items) < 2 {
		return nil, fmt.Errorf("encapsulated pixel data holds no fragments")
	}
	table := items[0]
	fragments := items[1:]

	// Offsets as the tables count them: from the first fragment's item tag,
	// each item's header included.
	base := fragments[0].Offset - 8
	offsets := make([]uint64, len(fragments))
	for i, fragment := range fragments {
		if fragment.Offset-8 < base || fragment.Length < 0 {
			return nil, fmt.Errorf("encapsulated pixel data: fragment %d is out of order", i)
		}
		offsets[i] = uint64(fragment.Offset - 8 - base)
	}

	var basic []uint64
	if table.Length > 0 {
		// Four bytes an entry. A table that is not is not one, and the other
		// rules may still settle it, as they do for one that points nowhere.
		if table.Length%4 == 0 && table.Length <= int64(4*len(fragments)) {
			raw := make([]byte, table.Length)
			if _, err := src.ReadAt(raw, table.Offset); err != nil {
				return nil, fmt.Errorf("reading the Basic Offset Table: %w", err)
			}
			basic = make([]uint64, len(raw)/4)
			for i := range basic {
				basic[i] = uint64(binary.LittleEndian.Uint32(raw[4*i:]))
			}
		}
	}

	ends := func(i int) (bool, error) {
		// Three bytes: the marker and the pad byte an encoder may add.
		fragment := fragments[i]
		n := fragment.Length
		if n > 3 {
			n = 3
		}
		tail := make([]byte, n)
		if _, err := src.ReadAt(tail, fragment.Offset+fragment.Length-n); err != nil {
			return false, fmt.Errorf("reading fragment %d: %w", i, err)
		}
		return endsImage(tail), nil
	}
	first, err := frameSpans(offsets, [][]uint64{extendedOffsets, basic}, numberOfFrames, ends)
	if err != nil {
		return nil, err
	}
	frames := make([][]Range, len(first)-1)
	for i := range frames {
		for _, fragment := range fragments[first[i]:first[i+1]] {
			frames[i] = append(frames[i], Range{Offset: fragment.Offset, Length: fragment.Length})
		}
	}
	return frames, nil
}

// frameSpans divides fragments into frames, which is the one decision Frames
// and FrameRanges share: frame i is fragments first[i] up to first[i+1].
//
// offsets are where each fragment's item starts, as the offset tables count;
// tables are offset tables to try in turn, each used only when it has an
// entry per frame and every entry is where a fragment starts. ends reports
// whether fragment i ends in an end-of-image marker; it is asked only when
// nothing else settles the division.
func frameSpans(offsets []uint64, tables [][]uint64, numberOfFrames int, ends func(int) (bool, error)) ([]int, error) {
	n := len(offsets)
	for _, table := range tables {
		if first, ok := spansByTable(offsets, table, numberOfFrames); ok {
			return first, nil
		}
	}

	switch {
	case numberOfFrames <= 0 || numberOfFrames == n:
		// Not knowing the count, a fragment each is all there is to go on.
		first := make([]int, n+1)
		for i := range first {
			first[i] = i
		}
		return first, nil
	case numberOfFrames == 1:
		return []int{0, n}, nil
	case n < numberOfFrames:
		return nil, fmt.Errorf("encapsulated pixel data holds %d fragment(s) for %d frames",
			n, numberOfFrames)
	}

	first := []int{0}
	for i := 0; i < n; i++ {
		end, err := ends(i)
		if err != nil {
			return nil, err
		}
		if end {
			first = append(first, i+1)
		}
	}
	if first[len(first)-1] != n || len(first)-1 != numberOfFrames {
		return nil, fmt.Errorf("encapsulated pixel data holds %d fragments for %d frames with no "+
			"Basic Offset Table, and their end-of-image markers do not divide them into that many",
			n, numberOfFrames)
	}
	return first, nil
}

// spansByTable divides fragments by an offset table. It reports false when
// there is no table or the table cannot be used: its entries are not one per
// frame, or one of them is not where a fragment starts. A table written
// wrongly is not rare, and the other rules may still settle it.
func spansByTable(offsets, table []uint64, numberOfFrames int) ([]int, bool) {
	if len(table) == 0 || (numberOfFrames > 0 && len(table) != numberOfFrames) {
		return nil, false
	}
	startsAt := make(map[uint64]int, len(offsets))
	for i, offset := range offsets {
		startsAt[offset] = i
	}
	first := make([]int, len(table)+1)
	for i, entry := range table {
		index, ok := startsAt[entry]
		if !ok || (i == 0 && index != 0) || (i > 0 && index <= first[i-1]) {
			return nil, false
		}
		first[i] = index
	}
	first[len(table)] = len(offsets)
	return first, true
}

// endsImage reports whether a fragment ends a JPEG-family codestream: with
// FFD9, which an encoder may follow with a zero byte to make the length even.
func endsImage(fragment []byte) bool {
	if n := len(fragment); n >= 3 && fragment[n-1] == 0x00 {
		fragment = fragment[:n-1]
	}
	n := len(fragment)
	return n >= 2 && fragment[n-2] == 0xFF && fragment[n-1] == 0xD9
}
