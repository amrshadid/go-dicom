package filereader

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/encaps"
	"github.com/amrshadid/go-dicom/tag"
)

// FrameRanges says where each frame of the file's Pixel Data lies in src,
// the bytes the file was read from, so that one frame can be read without
// the others: frame i is the bytes of result[i], in order, joined.
//
// Native pixel data gives one range a frame, Rows x Columns x Samples per
// Pixel samples of Bits Allocated each (two bytes a pixel for 8-bit
// YBR_FULL_422, which stores chroma for every second pixel). The bytes are
// as the file holds them: in a big endian file, big endian samples. A 1-bit
// image whose frames do not each start on a byte is refused, since a frame
// of it is not a run of bytes.
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
	pixelData := df.topLevel(tag.New(0x7FE0, 0x0010))
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
		bits := int64(info.Rows) * int64(info.Columns) * int64(info.SamplesPerPixel) * int64(info.BitsAllocated)
		if info.PhotometricInterpretation == "YBR_FULL_422" && info.SamplesPerPixel == 3 && info.BitsAllocated == 8 {
			bits = bits * 2 / 3
		}
		if bits%8 != 0 && frames > 1 {
			return nil, fmt.Errorf("a frame of this %d-bit image is %d bits long, so frames after the first "+
				"start inside a byte", info.BitsAllocated, bits)
		}
		length := (bits + 7) / 8
		if length <= 0 {
			return nil, fmt.Errorf("the image describes frames of %d bytes", length)
		}
		if need := length * int64(frames); int64(pixelData.Length) < need {
			return nil, fmt.Errorf("the Pixel Data holds %d bytes and %d frames of %d bytes need %d",
				pixelData.Length, frames, length, need)
		}
		ranges := make([][]encaps.Range, frames)
		for i := range ranges {
			ranges[i] = []encaps.Range{{Offset: position.ValueOffset + int64(i)*length, Length: length}}
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
