package filereader

import (
	"bytes"
	"os"
	"path/filepath"
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

// TestFrameReaderReadsPartOfAFrameAsTheWholeFrame: any run of bytes of a
// native frame read with ReadFrameBytes is that run of the frame
// ReadFrame gives, and both are the whole read's Pixel Data, in little
// and big endian files, at 8, 16 and 32 bits, starting and ending at odd
// bytes where a big endian file is reversed in pairs or fours. A compressed
// frame has no bytes to read part of and is refused.
func TestFrameReaderReadsPartOfAFrameAsTheWholeFrame(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, name := range []string{"CT_small.dcm", "MR_small_bigendian.dcm", "rtdose_expb.dcm",
		"SC_rgb_small_odd_big_endian.dcm", "ExplVR_BigEnd.dcm", "rtdose.dcm"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		whole := wholeFileWith(t, data, ReadOptions{})
		pixels := elementAt(whole, 0x7FE0, 0x0010).Value
		df := wholeFileWith(t, data, ReadOptions{SkipBulkData: true})
		reader, err := df.FrameReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var joined []byte
		for i := 0; i < reader.Frames(); i++ {
			frame, err := reader.ReadFrame(i)
			if err != nil {
				t.Fatalf("%s frame %d: %v", name, i, err)
			}
			joined = append(joined, frame...)
			for _, part := range [][2]int{{0, len(frame)}, {1, 3}, {3, 5}, {len(frame) / 2, len(frame)/2 + 7}, {len(frame) - 3, 3}} {
				offset, n := part[0], min(part[1], len(frame)-part[0])
				got := make([]byte, n)
				if err := reader.ReadFrameBytes(i, int64(offset), got); err != nil {
					t.Fatalf("%s frame %d, bytes %d+%d: %v", name, i, offset, n, err)
				}
				if !bytes.Equal(got, frame[offset:offset+n]) {
					t.Errorf("%s frame %d, bytes %d+%d: % x, and the frame has % x", name, i, offset, n,
						got, frame[offset:offset+n])
				}
			}
		}
		if !bytes.HasPrefix(pixels, joined) {
			t.Errorf("%s: the frames read by position are not the whole read's Pixel Data", name)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "SC_rgb_rle_2frame.dcm"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := wholeFileWith(t, data, ReadOptions{SkipBulkData: true}).FrameReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.ReadFrameBytes(0, 0, make([]byte, 4)); err == nil {
		t.Error("part of a compressed frame was read")
	}
}
