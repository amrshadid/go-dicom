package encaps

import (
	"bytes"
	"fmt"

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

	if frames, ok := framesByTable(encData, numberOfFrames); ok {
		return frames, nil
	}

	switch {
	case numberOfFrames <= 0 || numberOfFrames == len(fragments):
		// Not knowing the count, a fragment each is all there is to go on.
		return fragments, nil
	case numberOfFrames == 1:
		return [][]byte{bytes.Join(fragments, nil)}, nil
	case len(fragments) < numberOfFrames:
		return nil, fmt.Errorf("encapsulated pixel data holds %d fragment(s) for %d frames",
			len(fragments), numberOfFrames)
	}

	frames := make([][]byte, 0, numberOfFrames)
	var current [][]byte
	for _, fragment := range fragments {
		current = append(current, fragment)
		if endsImage(fragment) {
			frames = append(frames, bytes.Join(current, nil))
			current = nil
		}
	}
	if len(current) > 0 || len(frames) != numberOfFrames {
		return nil, fmt.Errorf("encapsulated pixel data holds %d fragments for %d frames with no "+
			"Basic Offset Table, and their end-of-image markers do not divide them into that many",
			len(fragments), numberOfFrames)
	}
	return frames, nil
}

// framesByTable groups fragments by the Basic Offset Table. It reports false
// when there is no table or the table cannot be used: its entries are not
// one per frame, or one of them is not where a fragment starts. A table
// written wrongly is not rare, and the other rules may still settle it.
func framesByTable(encData *compress.EncapsulatedData, numberOfFrames int) ([][]byte, bool) {
	table := encData.BasicOffsetTable
	if len(table) == 0 || (numberOfFrames > 0 && len(table) != numberOfFrames) {
		return nil, false
	}
	offsets := encData.FragmentOffsets
	if len(offsets) != len(encData.Fragments) {
		// Fragments not parsed from a value: each follows the last, with its
		// 8-byte item header.
		offsets = make([]uint64, len(encData.Fragments))
		var at uint64
		for i, fragment := range encData.Fragments {
			offsets[i] = at
			at += uint64(len(fragment)) + 8
		}
	}

	startsAt := make(map[uint64]int, len(offsets))
	for i, offset := range offsets {
		startsAt[offset] = i
	}
	first := make([]int, len(table)+1)
	for i, entry := range table {
		index, ok := startsAt[uint64(entry)]
		if !ok || (i == 0 && index != 0) || (i > 0 && index <= first[i-1]) {
			return nil, false
		}
		first[i] = index
	}
	first[len(table)] = len(encData.Fragments)

	frames := make([][]byte, len(table))
	for i := range frames {
		frames[i] = bytes.Join(encData.Fragments[first[i]:first[i+1]], nil)
	}
	return frames, true
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
