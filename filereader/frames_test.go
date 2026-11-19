package filereader

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/encaps"
)

// TestFrameRangesOfPackedOneBitImage checks a 1-bit image of three 5 x 5
// frames, 25 bits each with no padding between them (PS3.5 8.1.1): each
// frame's range is the bytes that hold any of its bits, a byte shared with
// its neighbor, and FrameFirstBit is the bit it begins at in the first.
func TestFrameRangesOfPackedOneBitImage(t *testing.T) {
	var ds bytes.Buffer
	for _, attribute := range [][]byte{
		{0x28, 0x00, 0x02, 0x00, 'U', 'S', 2, 0, 1, 0},               // Samples per Pixel
		{0x28, 0x00, 0x08, 0x00, 'I', 'S', 2, 0, '3', ' '},           // Number of Frames
		{0x28, 0x00, 0x10, 0x00, 'U', 'S', 2, 0, 5, 0},               // Rows
		{0x28, 0x00, 0x11, 0x00, 'U', 'S', 2, 0, 5, 0},               // Columns
		{0x28, 0x00, 0x00, 0x01, 'U', 'S', 2, 0, 1, 0},               // Bits Allocated
		{0xE0, 0x7F, 0x10, 0x00, 'O', 'B', 0, 0, 10, 0, 0, 0},        // Pixel Data, 75 bits in 10 bytes
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x07}, // and its value, 5 bits of padding
	} {
		ds.Write(attribute)
	}
	data := part10("1.2.840.10008.1.2.1", ds.Bytes())
	df := wholeFileWith(t, data, ReadOptions{SkipBulkData: true})
	ranges, err := df.FrameRanges(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	start := elementAt(df, 0x7FE0, 0x0010).Position.ValueOffset
	want := [][]encaps.Range{
		{{Offset: start, Length: 4}},     // bits 0 to 24
		{{Offset: start + 3, Length: 4}}, // bits 25 to 49
		{{Offset: start + 6, Length: 4}}, // bits 50 to 74
	}
	if !reflect.DeepEqual(ranges, want) {
		t.Errorf("ranges %v, want %v", ranges, want)
	}
	for i, bit := range []int{0, 1, 2} {
		if got, err := df.FrameFirstBit(i); err != nil || got != bit {
			t.Errorf("frame %d begins at bit %d (%v), want %d", i, got, err, bit)
		}
	}
}
