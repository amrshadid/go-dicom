package filereader

import (
	"encoding/binary"
	"fmt"
	"io"

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
// that.
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
