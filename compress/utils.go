package compress

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Utility Functions for Encapsulated Data

// ItemizeFragment wraps a fragment with DICOM item tag and length.
// Each fragment must be wrapped with item tag (FFFE,E000) and 4-byte length.
// Reference: DICOM PS3.5 Section 7.5
func ItemizeFragment(fragment []byte, endianness string) ([]byte, error) {
	if len(fragment) == 0 {
		return nil, fmt.Errorf("cannot itemize empty fragment")
	}

	buf := new(bytes.Buffer)

	itemTag := []byte{0xFE, 0xFF, 0x00, 0xE0}
	buf.Write(itemTag)

	lengthBytes := make([]byte, 4)
	if endianness == "<" {
		binary.LittleEndian.PutUint32(lengthBytes, uint32(len(fragment)))
	} else {
		binary.BigEndian.PutUint32(lengthBytes, uint32(len(fragment)))
	}
	buf.Write(lengthBytes)

	buf.Write(fragment)

	return buf.Bytes(), nil
}

// FragmentFrame splits a frame into multiple fragments.
// Each fragment must be an even number of bytes >= 2.
// The last fragment may be padded with 0x00 if necessary.
// Reference: DICOM PS3.5 Annex A.4
func FragmentFrame(frame []byte, numFragments int) ([][]byte, error) {
	frameLength := len(frame)

	if numFragments < 1 {
		return nil, fmt.Errorf("number of fragments must be >= 1")
	}

	if numFragments > (frameLength+1)/2 {
		return nil, fmt.Errorf("too many fragments requested (minimum fragment size is 2 bytes)")
	}

	fragmentLength := frameLength / numFragments

	if fragmentLength%2 != 0 {
		fragmentLength++
	}

	fragments := make([][]byte, 0, numFragments)

	for i := 0; i < numFragments-1; i++ {
		offset := i * fragmentLength
		endOffset := offset + fragmentLength
		if endOffset > frameLength {
			endOffset = frameLength
		}
		fragments = append(fragments, frame[offset:endOffset])
	}

	lastOffset := fragmentLength * (numFragments - 1)
	lastFragment := frame[lastOffset:]

	if len(lastFragment)%2 != 0 {
		lastFragment = append(lastFragment, 0x00)
	}

	fragments = append(fragments, lastFragment)

	return fragments, nil
}

// ItemizeFrame splits a frame into fragments and wraps each with item tags.
// This combines FragmentFrame and ItemizeFragment operations.
// Reference: DICOM PS3.5 Sections 7.5 and A.4
func ItemizeFrame(frame []byte, numFragments int, endianness string) ([][]byte, error) {
	fragments, err := FragmentFrame(frame, numFragments)
	if err != nil {
		return nil, err
	}

	itemizedFragments := make([][]byte, 0, len(fragments))
	for _, fragment := range fragments {
		itemized, err := ItemizeFragment(fragment, endianness)
		if err != nil {
			return nil, fmt.Errorf("failed to itemize fragment: %w", err)
		}
		itemizedFragments = append(itemizedFragments, itemized)
	}

	return itemizedFragments, nil
}

// EncapsulateFrames encapsulates multiple frames into DICOM pixel data format.
// Creates a Basic Offset Table (optionally with offsets), itemizes each frame, and combines everything.
// Reference: DICOM PS3.5 Section 7.5 and Annex A.4
func EncapsulateFrames(frames [][]byte, fragmentsPerFrame int, includeOffsets bool) ([]byte, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames to encapsulate")
	}

	if fragmentsPerFrame < 1 {
		fragmentsPerFrame = 1
	}

	buf := new(bytes.Buffer)
	endianness := DefaultEndianness

	buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})

	var offsets []uint32
	if includeOffsets {
		currentOffset := uint64(0)
		for i, frame := range frames {
			if currentOffset > 0xFFFFFFFF {
				return nil, fmt.Errorf("total data size exceeds 32-bit limit, use Extended Offset Table")
			}
			offsets = append(offsets, uint32(currentOffset))

			itemizedFrames, err := ItemizeFrame(frame, fragmentsPerFrame, endianness)
			if err != nil {
				return nil, fmt.Errorf("failed to itemize frame %d: %w", i, err)
			}

			for _, item := range itemizedFrames {
				currentOffset += uint64(len(item))
			}
		}

		if currentOffset > 0xFFFFFFFF {
			return nil, fmt.Errorf("total data size exceeds 32-bit limit, use Extended Offset Table")
		}

		botLength := uint32(len(offsets) * 4)
		lengthBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(lengthBytes, botLength)
		buf.Write(lengthBytes)

		for _, offset := range offsets {
			offsetBytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(offsetBytes, offset)
			buf.Write(offsetBytes)
		}
	} else {
		buf.Write([]byte{0x00, 0x00, 0x00, 0x00})
	}

	for i, frame := range frames {
		itemizedFrames, err := ItemizeFrame(frame, fragmentsPerFrame, endianness)
		if err != nil {
			return nil, fmt.Errorf("failed to itemize frame %d: %w", i, err)
		}

		for _, item := range itemizedFrames {
			buf.Write(item)
		}
	}

	return buf.Bytes(), nil
}

// EncapsulateExtended creates encapsulated data with Extended Offset Table support.
// Used when frame offsets exceed 32-bit limits.
// Returns: (encapsulated data, extended offsets, extended lengths).
// Reference: DICOM PS3.3 Section C.7.6.3
func EncapsulateExtended(frames [][]byte) ([]byte, []byte, []byte, error) {
	if len(frames) == 0 {
		return nil, nil, nil, fmt.Errorf("no frames to encapsulate")
	}

	buf := new(bytes.Buffer)
	endianness := DefaultEndianness

	buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00})

	var extendedOffsets []uint64
	var extendedLengths []uint64

	currentOffset := uint64(0)

	for i, frame := range frames {
		frameData := frame
		if len(frameData)%2 != 0 {
			frameData = append(frameData, 0x00)
		}

		extendedOffsets = append(extendedOffsets, currentOffset)
		extendedLengths = append(extendedLengths, uint64(len(frameData)))

		itemized, err := ItemizeFragment(frameData, endianness)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to itemize frame %d: %w", i, err)
		}

		buf.Write(itemized)
		currentOffset += uint64(len(itemized))
	}

	offsetsBuf := new(bytes.Buffer)
	for _, offset := range extendedOffsets {
		offsetBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(offsetBytes, offset)
		offsetsBuf.Write(offsetBytes)
	}

	lengthsBuf := new(bytes.Buffer)
	for _, length := range extendedLengths {
		lengthBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(lengthBytes, length)
		lengthsBuf.Write(lengthBytes)
	}

	return buf.Bytes(), offsetsBuf.Bytes(), lengthsBuf.Bytes(), nil
}

// CalculateCompressionRatio calculates the compression ratio.
func CalculateCompressionRatio(originalSize, compressedSize uint64) float64 {
	if originalSize == 0 {
		return 0.0
	}
	return float64(compressedSize) / float64(originalSize)
}

// ValidateEncapsulatedData performs basic validation on encapsulated pixel data.
func ValidateEncapsulatedData(data []byte, endianness string) error {
	if len(data) < 8 {
		return fmt.Errorf("data too short to be valid encapsulated pixel data")
	}

	reader := bytes.NewReader(data)

	_, err := ParseBasicOffsets(reader, endianness)
	if err != nil {
		return fmt.Errorf("invalid Basic Offset Table: %w", err)
	}

	numFragments, _, err := ParseFragments(reader, endianness)
	if err != nil {
		return fmt.Errorf("invalid fragments: %w", err)
	}

	if numFragments == 0 {
		return fmt.Errorf("no fragments found in encapsulated data")
	}

	return nil
}

// ReadItemTag reads and validates an item tag from a reader.
func ReadItemTag(reader io.Reader, endianness string) (uint32, error) {
	tagBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, tagBytes); err != nil {
		return 0, fmt.Errorf("failed to read item tag: %w", err)
	}

	var tag uint32
	if endianness == "<" {
		tag = binary.LittleEndian.Uint32(tagBytes)
	} else {
		tag = binary.BigEndian.Uint32(tagBytes)
	}

	return tag, nil
}

// ReadItemLength reads a 32-bit item length from a reader.
func ReadItemLength(reader io.Reader, endianness string) (uint32, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, lengthBytes); err != nil {
		return 0, fmt.Errorf("failed to read item length: %w", err)
	}

	var length uint32
	if endianness == "<" {
		length = binary.LittleEndian.Uint32(lengthBytes)
	} else {
		length = binary.BigEndian.Uint32(lengthBytes)
	}

	return length, nil
}

// DetectJPEGEndMarker searches for JPEG End of Image marker (0xFF 0xD9) in data.
func DetectJPEGEndMarker(data []byte) int {
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0xFF && data[i+1] == 0xD9 {
			return i
		}
	}
	return -1
}

// PadToEven pads data to even length if necessary.
func PadToEven(data []byte) []byte {
	if len(data)%2 != 0 {
		return append(data, 0x00)
	}
	return data
}
