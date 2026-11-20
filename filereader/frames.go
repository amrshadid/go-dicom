package filereader

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/encaps"
	"github.com/amrshadid/go-dicom/tag"
)

// FrameRanges says where each frame of the file's Pixel Data lies in src,
// the bytes the file was read from, so that one frame can be read without
// the others: frame i is the bytes of result[i], in order, joined.
//
// The pixels are those of Pixel Data (7FE0,0010), or of Float Pixel Data
// (7FE0,0008) or Double Float Pixel Data (7FE0,0009) in an image that holds
// one of those instead (PS3.3 C.7.6.3); a data set has only one of the
// three.
//
// Native pixel data gives one range a frame, Rows x Columns x Samples per
// Pixel samples of Bits Allocated each (two bytes a pixel for 8-bit
// YBR_FULL_422, which stores chroma for every second pixel). The bytes are
// as the file holds them: in a big endian file, big endian samples.
//
// The frames of a 1-bit image are packed one after another with no padding
// between them (PS3.5 8.1.1), so when a frame is not a whole number of
// bytes, frames after the first start inside a byte. Such a frame's range is
// then the bytes that hold any of its bits, a byte shared with the frame
// before or after it, and FrameFirstBit says at which bit of the first byte
// the frame begins. Every other frame begins at bit 0.
//
// Encapsulated pixel data gives the contents of the fragments that make up
// each frame, found by encaps.FrameRanges: the Extended Offset Table
// (7FE0,0001), the Basic Offset Table, and then the counts and the
// end-of-image markers. The Extended Offset Table is read from src when it
// was left there.
//
// The file is best read with ReadOptions.SkipBulkData, which records where
// every value and fragment is and reads none of them. Under the Deflated
// transfer syntax the positions, and so the ranges, count from the first
// byte of the inflated data set (see DICOMFile.Deflated), and src has to be
// that: DICOMFile.ValueSource(file) gives it, by inflating the file's stream
// as ranges are read.
func (df *DICOMFile) FrameRanges(src io.ReaderAt) ([][]encaps.Range, error) {
	pixelData := df.PixelDataElement()
	if pixelData == nil {
		return nil, fmt.Errorf("the file has no Pixel Data")
	}
	info, err := df.GetDataset().GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	frames := info.NumberOfFrames
	position := pixelData.Position

	if !pixelData.UndefinedLength {
		bits := frameBits(info)
		if bits <= 0 {
			return nil, fmt.Errorf("the image describes frames of %d bits", bits)
		}
		if need := (bits*int64(frames) + 7) / 8; int64(pixelData.Length) < need {
			return nil, fmt.Errorf("the Pixel Data holds %d bytes and %d frames of %d bits need %d",
				pixelData.Length, frames, bits, need)
		}
		ranges := make([][]encaps.Range, frames)
		for i := range ranges {
			first, end := int64(i)*bits, int64(i+1)*bits
			ranges[i] = []encaps.Range{{Offset: position.ValueOffset + first/8, Length: (end+7)/8 - first/8}}
		}
		return ranges, nil
	}

	items := make([]encaps.Range, len(position.Fragments))
	for i, fragment := range position.Fragments {
		items[i] = encaps.Range{Offset: fragment.ValueOffset, Length: fragment.End - fragment.ValueOffset}
	}
	extended, err := df.extendedOffsets(src)
	if err != nil {
		return nil, err
	}
	return encaps.FrameRanges(src, items, extended, frames)
}

// FrameFirstBit is the bit of its first byte, counted from the least
// significant, at which frame i (counted from 0) of native pixel data
// begins in the range FrameRanges gives for it. It is 0 except in a 1-bit
// image whose frames are not a whole number of bytes, where frame i begins
// at bit i x Rows x Columns x Samples per Pixel of the value (PS3.5 8.1.1),
// and a 1-bit image fills each byte from its least significant bit (PS3.5
// D.1).
func (df *DICOMFile) FrameFirstBit(i int) (int, error) {
	info, err := df.GetDataset().GetPixelDataInfo()
	if err != nil {
		return 0, err
	}
	return int(int64(i) * frameBits(info) % 8), nil
}

// frameBits is how many bits one frame of native pixel data takes.
func frameBits(info *dataset.PixelDataInfo) int64 {
	bits := int64(info.Rows) * int64(info.Columns) * int64(info.SamplesPerPixel) * int64(info.BitsAllocated)
	if info.PhotometricInterpretation == "YBR_FULL_422" && info.SamplesPerPixel == 3 && info.BitsAllocated == 8 {
		bits = bits * 2 / 3
	}
	return bits
}

// PixelDataElement returns the element of the data set itself that holds
// its pixels: Pixel Data (7FE0,0010), or Float Pixel Data (7FE0,0008) or
// Double Float Pixel Data (7FE0,0009) in an image that has one of those
// instead (PS3.3 C.7.6.3). It returns nil when there is none.
func (df *DICOMFile) PixelDataElement() *DataElementValue {
	for _, t := range []tag.Tag{tag.New(0x7FE0, 0x0010), tag.New(0x7FE0, 0x0008), tag.New(0x7FE0, 0x0009)} {
		if element := df.topLevel(t); element != nil {
			return element
		}
	}
	return nil
}

// extendedOffsets reads the Extended Offset Table, from src when the value
// was left in the file. It returns nil when the file has none.
func (df *DICOMFile) extendedOffsets(src io.ReaderAt) ([]uint64, error) {
	element := df.topLevel(tag.New(0x7FE0, 0x0001))
	if element == nil || element.Length == 0 {
		return nil, nil
	}
	raw := element.Value
	if element.ValueSkipped {
		raw = make([]byte, element.Length)
		if _, err := src.ReadAt(raw, element.Position.ValueOffset); err != nil {
			return nil, fmt.Errorf("reading the Extended Offset Table: %w", err)
		}
	}
	if len(raw)%8 != 0 {
		return nil, fmt.Errorf("the Extended Offset Table is %d bytes, not a whole number of 8-byte offsets", len(raw))
	}
	// Every syntax that encapsulates pixel data is little endian (PS3.5
	// A.4), so the table is too.
	offsets := make([]uint64, len(raw)/8)
	for i := range offsets {
		offsets[i] = binary.LittleEndian.Uint64(raw[8*i:])
	}
	return offsets, nil
}

// FrameReader reads the frames of a file's Pixel Data one at a time, by
// position, from the bytes the file was read from, so that one frame costs
// one frame of memory whatever the size of the rest. It satisfies
// dataset.FrameReader, which Dataset.RenderFramesFrom renders from, and is
// what the DICOMweb server reads a frame with.
//
// A native frame comes out little endian whatever the file's byte order,
// as the reader gives the whole value: a big endian file is reversed at the
// width the reader reverses Pixel Data at (dataelem.PixelDataSwapWidth),
// counted from the value's first byte, so an 8-bit image in OW is reversed
// in pairs and a frame of odd length shares its last pair with the next.
// A compressed frame comes out as its fragments joined.
type FrameReader struct {
	ranges [][]encaps.Range
	src    io.ReaderAt
	// swap is the width to reverse native samples in, and value the bytes
	// they are reversed within; swap is 0 when nothing is to be reversed.
	swap  int
	value encaps.Range
	// packed is the length in bits of a frame of a 1-bit image whose frames
	// start inside a byte, and firstBit the bit each begins at in its first
	// byte; packed is 0 for every other image.
	packed   int64
	firstBit []int
	// encapsulated is set when the frames are compressed.
	encapsulated bool
}

// FrameReader returns a reader of the frames of the file's Pixel Data from
// src, which is what FrameRanges says of src: the bytes the file was read
// from, or for a deflated file the inflated data set.
func (df *DICOMFile) FrameReader(src io.ReaderAt) (*FrameReader, error) {
	pixels := df.PixelDataElement()
	if pixels == nil {
		return nil, fmt.Errorf("the file has no Pixel Data")
	}
	info, err := df.GetDataset().GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	reader := &FrameReader{src: src, encapsulated: pixels.UndefinedLength}
	if reader.ranges, err = df.FrameRanges(src); err != nil {
		return nil, err
	}
	if !pixels.UndefinedLength && !df.IsLittleEndian && !df.Deflated {
		// Float and Double Float Pixel Data are OF and OD, reversed in fours
		// and eights.
		reader.swap = dataelem.PixelDataSwapWidth(dataelem.VR(pixels.VR), info.BitsAllocated)
		reader.value = encaps.Range{Offset: pixels.Position.ValueOffset, Length: int64(pixels.Length)}
	}
	if bits := int64(info.Rows) * int64(info.Columns) * int64(info.SamplesPerPixel); info.BitsAllocated == 1 &&
		!pixels.UndefinedLength && bits%8 != 0 && len(reader.ranges) > 1 {
		reader.packed = bits
		reader.firstBit = make([]int, len(reader.ranges))
		for i := range reader.firstBit {
			if reader.firstBit[i], err = df.FrameFirstBit(i); err != nil {
				return nil, err
			}
		}
	}
	return reader, nil
}

// Frames is how many frames there are to read.
func (r *FrameReader) Frames() int {
	return len(r.ranges)
}

// ReadFrame returns frame i, counted from 0, as the file holds it, native
// samples little endian. A frame of a 1-bit image that starts inside a byte
// is moved to start at the first bit of its first byte, the bits after it
// cleared: on its own it has no position to start from, and that is how the
// Pixel Data of a 1-bit image of one frame is laid out (PS3.5 8.1.1, D.1).
func (r *FrameReader) ReadFrame(i int) ([]byte, error) {
	if i < 0 || i >= len(r.ranges) {
		return nil, fmt.Errorf("frame %d does not exist; there are %d, counted from 0", i, len(r.ranges))
	}
	var out []byte
	for _, part := range r.ranges[i] {
		chunk, err := r.read(part)
		if err != nil {
			return nil, fmt.Errorf("reading frame %d: %w", i+1, err)
		}
		out = append(out, chunk...)
	}
	if r.packed > 0 {
		out = shiftBits(out, r.firstBit[i], r.packed)
	}
	return out, nil
}

// ReadFrameBytes reads len(p) bytes of native frame i, counted from 0,
// from byte offset within it, into p, little endian: a band of rows of a
// frame too large to be read whole, into a buffer used again for the next.
// A compressed frame, or a 1-bit frame that starts inside a byte, has no
// bytes to read part of, and is refused.
func (r *FrameReader) ReadFrameBytes(i int, offset int64, p []byte) error {
	if i < 0 || i >= len(r.ranges) {
		return fmt.Errorf("frame %d does not exist; there are %d, counted from 0", i, len(r.ranges))
	}
	if r.encapsulated || len(r.ranges[i]) != 1 || r.packed > 0 {
		return fmt.Errorf("frame %d is not native samples to read part of", i+1)
	}
	whole, n := r.ranges[i][0], int64(len(p))
	if offset < 0 || offset+n > whole.Length {
		return fmt.Errorf("bytes %d to %d of frame %d, which has %d", offset, offset+n, i+1, whole.Length)
	}
	part := encaps.Range{Offset: whole.Offset + offset, Length: n}
	if r.swap <= 1 {
		if _, err := r.src.ReadAt(p, part.Offset); err != nil {
			return fmt.Errorf("reading frame %d: %w", i+1, err)
		}
		return nil
	}
	chunk, err := r.read(part)
	if err != nil {
		return fmt.Errorf("reading frame %d: %w", i+1, err)
	}
	copy(p, chunk)
	return nil
}

// read reads one range, reversed to little endian where the file is big
// endian: in whole units counted from the value's start, so the range is
// widened to them and cut back after.
func (r *FrameReader) read(part encaps.Range) ([]byte, error) {
	start, end := part.Offset, part.Offset+part.Length
	if r.swap > 1 {
		unit := int64(r.swap)
		start -= (start - r.value.Offset) % unit
		end += (unit - (end-r.value.Offset)%unit) % unit
		end = min(end, r.value.Offset+r.value.Length)
	}
	chunk := make([]byte, end-start)
	if _, err := r.src.ReadAt(chunk, start); err != nil {
		return nil, err
	}
	if r.swap > 1 {
		dataelem.SwapBytes(chunk, r.swap)
	}
	return chunk[part.Offset-start : part.Offset-start+part.Length], nil
}

// shiftBits returns the bits bits of data that start at bit first of its
// first byte, counted from the least significant bit, starting at bit 0
// and followed by zero bits to the end of the last byte.
func shiftBits(data []byte, first int, bits int64) []byte {
	out := make([]byte, (bits+7)/8)
	for i := range out {
		at := i*8 + first
		value := int(data[at/8]) >> (at % 8)
		if at%8 != 0 && at/8+1 < len(data) {
			value |= int(data[at/8+1]) << (8 - at%8)
		}
		out[i] = byte(value)
	}
	if rest := bits % 8; rest != 0 {
		out[len(out)-1] &= byte(1<<rest - 1)
	}
	return out
}

// topLevel returns the element of the data set itself with this tag, not
// one inside a sequence, or nil.
func (df *DICOMFile) topLevel(t tag.Tag) *DataElementValue {
	for _, element := range df.DataElements {
		if element.Tag == t {
			return element
		}
	}
	return nil
}
