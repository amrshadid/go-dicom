package compress

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"io"
	"sync"
)

// Parsing Encapsulated Data

// ParseBasicOffsets parses the Basic Offset Table from encapsulated pixel data.
// Returns the list of frame offsets from the Basic Offset Table.
//
// The Basic Offset Table is the first item in encapsulated pixel data and contains
// 32-bit unsigned integer offsets to each frame, measured from the end of the table.
//
// Reference: DICOM PS3.5 Section A.4
func ParseBasicOffsets(buffer io.ReadSeeker, endianness string) ([]uint32, error) {
	tagBytes := make([]byte, 4)
	if _, err := buffer.Read(tagBytes); err != nil {
		return nil, fmt.Errorf("failed to read Basic Offset Table tag: %w", err)
	}

	var tag uint32
	if endianness == "<" {
		tag = binary.LittleEndian.Uint32(tagBytes)
	} else {
		tag = binary.BigEndian.Uint32(tagBytes)
	}

	if tag != ItemTag {
		return nil, fmt.Errorf("expected tag (FFFE,E000), found 0x%08X", tag)
	}

	lengthBytes := make([]byte, 4)
	if _, err := buffer.Read(lengthBytes); err != nil {
		return nil, fmt.Errorf("failed to read Basic Offset Table length: %w", err)
	}

	var length uint32
	if endianness == "<" {
		length = binary.LittleEndian.Uint32(lengthBytes)
	} else {
		length = binary.BigEndian.Uint32(lengthBytes)
	}

	if length%4 != 0 {
		return nil, fmt.Errorf("basic offset table length (%d) is not a multiple of 4", length)
	}

	if length == 0 {
		return []uint32{}, nil
	}

	numOffsets := length / 4
	offsets := make([]uint32, numOffsets)

	for i := uint32(0); i < numOffsets; i++ {
		offsetBytes := make([]byte, 4)
		if _, err := buffer.Read(offsetBytes); err != nil {
			return nil, fmt.Errorf("failed to read offset %d: %w", i, err)
		}

		if endianness == "<" {
			offsets[i] = binary.LittleEndian.Uint32(offsetBytes)
		} else {
			offsets[i] = binary.BigEndian.Uint32(offsetBytes)
		}
	}

	return offsets, nil
}

// ParseExtendedOffsetTable parses the Extended Offset Table from raw bytes.
//
// The Extended Offset Table is used for large multi-frame datasets (>4GB) where
// 32-bit offsets are insufficient. It contains 64-bit offsets and lengths.
//
// This function expects the raw data from DICOM tags:
//   - (7FE0,0001) Extended Offset Table: byte offsets (64-bit)
//   - (7FE0,0002) Extended Offset Table Lengths: frame lengths (64-bit)
//
// Reference: DICOM PS3.3 Section C.7.6.3.1.8
func ParseExtendedOffsetTable(
	offsetsData []byte,
	lengthsData []byte,
	endianness string,
) ([]uint64, []uint64, error) {
	if len(offsetsData)%8 != 0 {
		return nil, nil, fmt.Errorf("extended offset table data length (%d) is not a multiple of 8", len(offsetsData))
	}
	if len(lengthsData)%8 != 0 {
		return nil, nil, fmt.Errorf("extended offset table lengths data length (%d) is not a multiple of 8", len(lengthsData))
	}

	numOffsets := len(offsetsData) / 8
	numLengths := len(lengthsData) / 8

	if numOffsets != numLengths {
		return nil, nil, fmt.Errorf("extended offset table mismatch: %d offsets but %d lengths", numOffsets, numLengths)
	}

	offsets := make([]uint64, numOffsets)
	for i := 0; i < numOffsets; i++ {
		offsetBytes := offsetsData[i*8 : (i+1)*8]
		if endianness == "<" {
			offsets[i] = binary.LittleEndian.Uint64(offsetBytes)
		} else {
			offsets[i] = binary.BigEndian.Uint64(offsetBytes)
		}
	}

	lengths := make([]uint64, numLengths)
	for i := 0; i < numLengths; i++ {
		lengthBytes := lengthsData[i*8 : (i+1)*8]
		if endianness == "<" {
			lengths[i] = binary.LittleEndian.Uint64(lengthBytes)
		} else {
			lengths[i] = binary.BigEndian.Uint64(lengthBytes)
		}
	}

	return offsets, lengths, nil
}

// ParseFragments returns the number of fragments and their offsets in the buffer.
//
// This scans through the encapsulated data starting after the Basic Offset Table
// to find all fragment items and their positions.
//
// Reference: DICOM PS3.5 Section A.4
func ParseFragments(buffer io.ReadSeeker, endianness string) (int, []uint64, error) {
	startOffset, err := buffer.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get current position: %w", err)
	}

	numFragments := 0
	var fragmentOffsets []uint64

	for {
		currentPos, err := buffer.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to get position: %w", err)
		}

		tagBytes := make([]byte, 4)
		n, err := buffer.Read(tagBytes)
		if err == io.EOF || n != 4 {
			break
		}
		if err != nil {
			return 0, nil, fmt.Errorf("failed to read tag at offset %d: %w", currentPos, err)
		}

		var tag uint32
		if endianness == "<" {
			tag = binary.LittleEndian.Uint32(tagBytes)
		} else {
			tag = binary.BigEndian.Uint32(tagBytes)
		}

		if tag == SequenceDelimiterTag {
			break
		}

		if tag != ItemTag {
			return 0, nil, fmt.Errorf("unexpected tag 0x%08X at offset %d", tag, currentPos)
		}

		lengthBytes := make([]byte, 4)
		if _, err := buffer.Read(lengthBytes); err != nil {
			return 0, nil, fmt.Errorf("failed to read length at offset %d: %w", currentPos+4, err)
		}

		var length uint32
		if endianness == "<" {
			length = binary.LittleEndian.Uint32(lengthBytes)
		} else {
			length = binary.BigEndian.Uint32(lengthBytes)
		}

		if length == 0xFFFFFFFF {
			return 0, nil, fmt.Errorf("undefined item length at offset %d", currentPos+4)
		}

		numFragments++
		fragmentOffsets = append(fragmentOffsets, uint64(currentPos))

		if _, err := buffer.Seek(int64(length), io.SeekCurrent); err != nil {
			return 0, nil, fmt.Errorf("failed to skip fragment data: %w", err)
		}
	}

	if _, err := buffer.Seek(startOffset, io.SeekStart); err != nil {
		return 0, nil, fmt.Errorf("failed to reset buffer position: %w", err)
	}

	return numFragments, fragmentOffsets, nil
}

// GenerateFragments yields frame fragments from the encapsulated pixel data.
//
// This is a generator-style function that reads fragments sequentially from the buffer.
//
// Reference: DICOM PS3.5 Section A.4
func GenerateFragments(buffer io.Reader, endianness string) (<-chan []byte, <-chan error) {
	fragments := make(chan []byte)
	errors := make(chan error, 1)

	go func() {
		defer close(fragments)
		defer close(errors)

		for {
			tagBytes := make([]byte, 4)
			n, err := buffer.Read(tagBytes)
			if err == io.EOF || n != 4 {
				return
			}
			if err != nil {
				errors <- fmt.Errorf("failed to read tag: %w", err)
				return
			}

			var tag uint32
			if endianness == "<" {
				tag = binary.LittleEndian.Uint32(tagBytes)
			} else {
				tag = binary.BigEndian.Uint32(tagBytes)
			}

			if tag == SequenceDelimiterTag {
				return
			}

			if tag != ItemTag {
				errors <- fmt.Errorf("unexpected tag 0x%08X", tag)
				return
			}

			lengthBytes := make([]byte, 4)
			if _, err := buffer.Read(lengthBytes); err != nil {
				errors <- fmt.Errorf("failed to read length: %w", err)
				return
			}

			var length uint32
			if endianness == "<" {
				length = binary.LittleEndian.Uint32(lengthBytes)
			} else {
				length = binary.BigEndian.Uint32(lengthBytes)
			}

			if length == 0xFFFFFFFF {
				errors <- fmt.Errorf("undefined item length")
				return
			}

			fragmentData := make([]byte, length)
			if _, err := io.ReadFull(buffer, fragmentData); err != nil {
				errors <- fmt.Errorf("failed to read fragment data: %w", err)
				return
			}

			fragments <- fragmentData
		}
	}()

	return fragments, errors
}

// GenerateFragmentedFrames yields fragmented frames from encapsulated pixel data.
//
// This function returns frames where each frame consists of one or more fragments.
// It handles multiple encapsulation scenarios based on the Basic Offset Table
// and number of frames.
//
// Parameters:
//   - buffer: Reader containing encapsulated pixel data starting at BOT
//   - numberOfFrames: Expected number of frames (from DICOM tag 0028,0008)
//   - endianness: Byte order ("<" for little-endian, ">" for big-endian)
//
// Returns: Two channels - one for FrameInfo and one for errors
//
// Reference: DICOM PS3.5 Section A.4
func GenerateFragmentedFrames(
	buffer io.ReadSeeker,
	numberOfFrames int,
	endianness string,
) (<-chan *FrameInfo, <-chan error) {
	frames := make(chan *FrameInfo)
	errors := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errors)

		basicOffsets, err := ParseBasicOffsets(buffer, endianness)
		if err != nil {
			errors <- fmt.Errorf("failed to parse Basic Offset Table: %w", err)
			return
		}

		numFragments, fragmentOffsets, err := ParseFragments(buffer, endianness)
		if err != nil {
			errors <- fmt.Errorf("failed to parse fragments: %w", err)
			return
		}

		fragmentsChan, fragmentErrors := GenerateFragments(buffer, endianness)

		var allFragments [][]byte
		for fragment := range fragmentsChan {
			allFragments = append(allFragments, fragment)
		}
		if err := <-fragmentErrors; err != nil {
			errors <- err
			return
		}

		if len(basicOffsets) > 0 {
			generateFramesFromBOT(allFragments, basicOffsets, fragmentOffsets, frames)
		} else if numFragments == 1 {
			frames <- &FrameInfo{
				Index:         0,
				Offset:        fragmentOffsets[0],
				FragmentCount: 1,
				Fragments:     [][]byte{allFragments[0]},
				Length:        uint64(len(allFragments[0])),
			}
		} else if numberOfFrames == 0 {
			errors <- fmt.Errorf("number of frames required when BOT is empty and multiple fragments exist")
			return
		} else if numFragments == numberOfFrames {
			for i, fragment := range allFragments {
				frames <- &FrameInfo{
					Index:         i,
					Offset:        fragmentOffsets[i],
					FragmentCount: 1,
					Fragments:     [][]byte{fragment},
					Length:        uint64(len(fragment)),
				}
			}
		} else if numberOfFrames == 1 {
			totalLength := uint64(0)
			for _, frag := range allFragments {
				totalLength += uint64(len(frag))
			}
			frames <- &FrameInfo{
				Index:         0,
				Offset:        fragmentOffsets[0],
				FragmentCount: len(allFragments),
				Fragments:     allFragments,
				Length:        totalLength,
			}
		} else if numFragments > numberOfFrames {
			generateFramesFromJPEGMarkers(allFragments, fragmentOffsets, numberOfFrames, frames)
		} else {
			errors <- fmt.Errorf("insufficient fragments (%d) for frames (%d)", numFragments, numberOfFrames)
		}
	}()

	return frames, errors
}

// generateFramesFromBOT uses the Basic Offset Table to split fragments into frames
func generateFramesFromBOT(
	fragments [][]byte,
	basicOffsets []uint32,
	fragmentOffsets []uint64,
	frames chan<- *FrameInfo,
) {
	currentFragIdx := 0
	currentOffset := uint64(0)

	for frameIdx := 0; frameIdx < len(basicOffsets); frameIdx++ {
		frameFragments := [][]byte{}
		frameLength := uint64(0)
		frameStartOffset := fragmentOffsets[currentFragIdx]

		var nextBoundary uint64
		if frameIdx < len(basicOffsets)-1 {
			nextBoundary = uint64(basicOffsets[frameIdx+1])
		} else {
			nextBoundary = ^uint64(0)
		}

		for currentFragIdx < len(fragments) && currentOffset < nextBoundary {
			frameFragments = append(frameFragments, fragments[currentFragIdx])
			frameLength += uint64(len(fragments[currentFragIdx]))
			currentOffset += uint64(len(fragments[currentFragIdx])) + 8
			currentFragIdx++
		}

		frames <- &FrameInfo{
			Index:         frameIdx,
			Offset:        frameStartOffset,
			FragmentCount: len(frameFragments),
			Fragments:     frameFragments,
			Length:        frameLength,
		}
	}
}

// generateFramesFromJPEGMarkers uses JPEG EOI markers to split fragments into frames
func generateFramesFromJPEGMarkers(
	fragments [][]byte,
	fragmentOffsets []uint64,
	numberOfFrames int,
	frames chan<- *FrameInfo,
) {
	frameIdx := 0
	currentFragments := [][]byte{}
	frameStartOffset := fragmentOffsets[0]
	frameLength := uint64(0)

	for fragIdx, fragment := range fragments {
		currentFragments = append(currentFragments, fragment)
		frameLength += uint64(len(fragment))

		if len(fragment) >= 2 {
			hasEOI := false
			searchStart := len(fragment) - 10
			if searchStart < 0 {
				searchStart = 0
			}
			for i := searchStart; i < len(fragment)-1; i++ {
				if fragment[i] == 0xFF && fragment[i+1] == 0xD9 {
					hasEOI = true
					break
				}
			}

			if hasEOI {
				frames <- &FrameInfo{
					Index:         frameIdx,
					Offset:        frameStartOffset,
					FragmentCount: len(currentFragments),
					Fragments:     currentFragments,
					Length:        frameLength,
				}

				frameIdx++
				currentFragments = [][]byte{}
				frameLength = 0
				if fragIdx+1 < len(fragmentOffsets) {
					frameStartOffset = fragmentOffsets[fragIdx+1]
				}

				if frameIdx >= numberOfFrames {
					return
				}
			}
		}
	}

	if len(currentFragments) > 0 && frameIdx < numberOfFrames {
		frames <- &FrameInfo{
			Index:         frameIdx,
			Offset:        frameStartOffset,
			FragmentCount: len(currentFragments),
			Fragments:     currentFragments,
			Length:        frameLength,
		}
	}
}

// GenerateFrames yields complete frames from encapsulated pixel data.
//
// This is a convenience function that joins fragmented frames into complete
// byte arrays, making it easier to process frames without worrying about
// fragment boundaries.
//
// Parameters:
//   - buffer: Reader containing encapsulated pixel data starting at BOT
//   - numberOfFrames: Expected number of frames (from DICOM tag 0028,0008)
//   - endianness: Byte order ("<" for little-endian, ">" for big-endian)
//
// Returns: Two channels - one for complete frame data and one for errors
//
// Reference: DICOM PS3.5 Section A.4
func GenerateFrames(
	buffer io.ReadSeeker,
	numberOfFrames int,
	endianness string,
) (<-chan []byte, <-chan error) {
	frames := make(chan []byte)
	errors := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errors)

		fragmentedFrames, fragErrors := GenerateFragmentedFrames(buffer, numberOfFrames, endianness)

		for frameInfo := range fragmentedFrames {
			// Join all fragments into a single byte array
			var completeFrame []byte
			for _, fragment := range frameInfo.Fragments {
				completeFrame = append(completeFrame, fragment...)
			}
			frames <- completeFrame
		}

		if err := <-fragErrors; err != nil {
			errors <- err
		}
	}()

	return frames, errors
}

// GetFrame extracts a single frame by index from encapsulated pixel data.
//
// This function provides random access to frames, allowing you to extract
// a specific frame without processing all previous frames. It's more efficient
// than iterating through GenerateFrames when you only need one frame.
//
// Parameters:
//   - buffer: ReadSeeker containing encapsulated pixel data starting at BOT
//   - index: Zero-based frame index to extract
//   - numberOfFrames: Total expected number of frames (from DICOM tag 0028,0008)
//   - endianness: Byte order ("<" for little-endian, ">" for big-endian)
//
// Returns: Complete frame data as bytes
//
// Reference: DICOM PS3.5 Section A.4
func GetFrame(
	buffer io.ReadSeeker,
	index int,
	numberOfFrames int,
	endianness string,
) ([]byte, error) {
	if index < 0 {
		return nil, fmt.Errorf("frame index must be non-negative, got %d", index)
	}

	startPosition, err := buffer.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("failed to get starting position: %w", err)
	}

	basicOffsets, err := ParseBasicOffsets(buffer, endianness)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Basic Offset Table: %w", err)
	}

	numFragments, fragmentOffsets, err := ParseFragments(buffer, endianness)
	if err != nil {
		return nil, fmt.Errorf("failed to parse fragments: %w", err)
	}

	if _, err := buffer.Seek(startPosition, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to reset buffer: %w", err)
	}
	if _, err := ParseBasicOffsets(buffer, endianness); err != nil {
		return nil, fmt.Errorf("failed to parse basic offsets: %w", err)
	}

	if len(basicOffsets) > 0 {
		return getFrameFromBOT(buffer, index, basicOffsets, endianness)
	} else if numFragments == 1 {
		if index != 0 {
			return nil, fmt.Errorf("only frame 0 exists, requested index %d", index)
		}
		fragmentsChan, errorsChan := GenerateFragments(buffer, endianness)
		frame := <-fragmentsChan
		if err := <-errorsChan; err != nil {
			return nil, err
		}
		return frame, nil
	} else if numberOfFrames == 0 {
		return nil, fmt.Errorf("number of frames required when BOT is empty")
	} else if numFragments == numberOfFrames {
		if index >= numFragments {
			return nil, fmt.Errorf("frame index %d out of range (0-%d)", index, numFragments-1)
		}
		return getFrameByFragmentIndex(buffer, index, fragmentOffsets, endianness)
	} else if numberOfFrames == 1 {
		if index != 0 {
			return nil, fmt.Errorf("only frame 0 exists, requested index %d", index)
		}
		return getAllFragmentsAsFrame(buffer, endianness)
	} else {
		return getFrameViaGenerator(buffer, index, numberOfFrames, startPosition, endianness)
	}
}

// getFrameFromBOT uses the Basic Offset Table for direct frame access
func getFrameFromBOT(
	buffer io.ReadSeeker,
	index int,
	basicOffsets []uint32,
	endianness string,
) ([]byte, error) {
	if index >= len(basicOffsets) {
		return nil, fmt.Errorf("frame index %d out of range (0-%d)", index, len(basicOffsets)-1)
	}

	// Seek to frame offset
	offset := int64(basicOffsets[index])
	if _, err := buffer.Seek(offset, io.SeekCurrent); err != nil {
		return nil, fmt.Errorf("failed to seek to frame %d: %w", index, err)
	}

	// Determine frame length
	var frameLength int64
	if index < len(basicOffsets)-1 {
		frameLength = int64(basicOffsets[index+1] - basicOffsets[index])
	} else {
		frameLength = -1 // Read until sequence delimiter for last frame
	}

	// Read fragments for this frame
	var frameData []byte
	fragmentsChan, errorsChan := GenerateFragments(buffer, endianness)

	if frameLength > 0 {
		// Known length - read specific amount
		bytesRead := int64(0)
		for fragment := range fragmentsChan {
			if bytesRead < frameLength {
				frameData = append(frameData, fragment...)
				bytesRead += int64(len(fragment)) + 8 // +8 for item tag/length
			}
			// Continue consuming the channel even after we have enough data
			// to avoid blocking the generator goroutine
		}
	} else {
		// Unknown length - read all remaining fragments
		for fragment := range fragmentsChan {
			frameData = append(frameData, fragment...)
		}
	}

	if err := <-errorsChan; err != nil {
		return nil, err
	}

	return frameData, nil
}

// getFrameByFragmentIndex gets a frame when there's 1:1 fragment-to-frame mapping
func getFrameByFragmentIndex(
	buffer io.ReadSeeker,
	index int,
	fragmentOffsets []uint64,
	endianness string,
) ([]byte, error) {
	// Seek to the specific fragment
	if _, err := buffer.Seek(int64(fragmentOffsets[index]), io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to fragment %d: %w", index, err)
	}

	fragmentsChan, errorsChan := GenerateFragments(buffer, endianness)
	frame := <-fragmentsChan
	if err := <-errorsChan; err != nil {
		return nil, err
	}

	return frame, nil
}

// getAllFragmentsAsFrame reads all fragments and joins them into one frame
func getAllFragmentsAsFrame(buffer io.ReadSeeker, endianness string) ([]byte, error) {
	var frameData []byte
	fragmentsChan, errorsChan := GenerateFragments(buffer, endianness)

	for fragment := range fragmentsChan {
		frameData = append(frameData, fragment...)
	}

	if err := <-errorsChan; err != nil {
		return nil, err
	}

	return frameData, nil
}

// getFrameViaGenerator uses the generator as a fallback (less efficient)
func getFrameViaGenerator(
	buffer io.ReadSeeker,
	index int,
	numberOfFrames int,
	startPosition int64,
	endianness string,
) ([]byte, error) {
	// Reset buffer to start
	if _, err := buffer.Seek(startPosition, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to reset buffer: %w", err)
	}

	framesChan, errorsChan := GenerateFrames(buffer, numberOfFrames, endianness)

	// Iterate to the desired frame
	frameIdx := 0
	for frame := range framesChan {
		if frameIdx == index {
			return frame, nil
		}
		frameIdx++
	}

	if err := <-errorsChan; err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("frame index %d not found", index)
}

// Decompressor Implementations

// DeflateDecompressor handles DEFLATE (zlib) decompression.
type DeflateDecompressor struct {
	mu sync.RWMutex
}

// MaxDecompressedSize bounds how large decompressed output may become.
//
// DEFLATE reaches ratios above 1000:1 on repetitive input, so a few kilobytes
// of crafted data can expand without limit — a decompression bomb. SECURITY.md
// warns callers about this for compressed transfer syntaxes from untrusted
// sources; this constant is what makes that warning enforceable rather than
// advisory. 256 MiB is far above any legitimate DICOM pixel data while keeping
// a hostile input cheap to reject.
const MaxDecompressedSize int64 = 256 << 20

// NewDeflateDecompressor creates a new DEFLATE decompressor.
func NewDeflateDecompressor() *DeflateDecompressor {
	return &DeflateDecompressor{}
}

// Decompress decompresses DEFLATE-compressed data.
func (d *DeflateDecompressor) Decompress(data []byte) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(data) == 0 {
		return nil, fmt.Errorf("empty data for DEFLATE decompression")
	}

	reader := bytes.NewReader(data)
	zlibReader, err := zlib.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer zlibReader.Close()

	// Read one byte past the limit: if that byte materializes, the input
	// expands beyond what is allowed and the rest is not worth decompressing.
	limited := io.LimitReader(zlibReader, MaxDecompressedSize+1)
	decompressed, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress DEFLATE data: %w", err)
	}
	if int64(len(decompressed)) > MaxDecompressedSize {
		return nil, fmt.Errorf(
			"DEFLATE data expands beyond the %d byte limit; refusing to continue "+
				"(input was %d bytes, a ratio of at least %d:1)",
			MaxDecompressedSize, len(data), MaxDecompressedSize/int64(max(len(data), 1)))
	}

	return decompressed, nil
}

// CanDecompress checks if data looks like DEFLATE.
func (d *DeflateDecompressor) CanDecompress(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	// DEFLATE data starts with 0x78
	return data[0] == 0x78
}

// RLEDecompressor handles RLE (Run-Length Encoding) decompression.
type RLEDecompressor struct {
	mu sync.RWMutex
}

// NewRLEDecompressor creates a new RLE decompressor.
func NewRLEDecompressor() *RLEDecompressor {
	return &RLEDecompressor{}
}

// Decompress decompresses RLE-compressed data.
//
// RLE format in DICOM:
//   - 0x00-0x7F: Copy next N+1 bytes as-is
//   - 0x80: Special padding byte (skip)
//   - 0x81-0xFF: Repeat next byte 257-N times
//
// Reference: DICOM PS3.5 Section G.3
func (d *RLEDecompressor) Decompress(data []byte) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(data) == 0 {
		return []byte{}, nil
	}

	var result []byte
	i := 0

	for i < len(data) {
		controlByte := data[i]
		i++

		if controlByte <= 0x7F {
			// Copy mode: copy next controlByte+1 bytes as-is
			copyLength := int(controlByte) + 1
			if i+copyLength > len(data) {
				return nil, fmt.Errorf("rle: insufficient data for copy operation at offset %d", i-1)
			}
			result = append(result, data[i:i+copyLength]...)
			i += copyLength
		} else if controlByte == 0x80 {
			// Padding byte (skip)
			continue
		} else {
			// Run mode: repeat next byte 257-controlByte times
			if i >= len(data) {
				return nil, fmt.Errorf("rle: insufficient data for run operation at offset %d", i-1)
			}
			runByte := data[i]
			i++
			runLength := 257 - int(controlByte)
			for j := 0; j < runLength; j++ {
				result = append(result, runByte)
			}
		}
	}

	return result, nil
}

// CanDecompress checks if data looks like RLE.
func (d *RLEDecompressor) CanDecompress(data []byte) bool {
	// RLE data can't easily be identified without trying to decompress
	return false
}

// JPEGDecompressor handles JPEG decompression.
type JPEGDecompressor struct {
	mu sync.RWMutex
}

// NewJPEGDecompressor creates a new JPEG decompressor.
func NewJPEGDecompressor() *JPEGDecompressor {
	return &JPEGDecompressor{}
}

// Decompress decompresses JPEG data and returns raw pixel data.
func (d *JPEGDecompressor) Decompress(data []byte) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(data) == 0 {
		return nil, fmt.Errorf("empty data for JPEG decompression")
	}

	reader := bytes.NewReader(data)
	img, err := jpeg.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JPEG: %w", err)
	}

	// Convert image to raw pixel data
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	pixelData := make([]byte, 0, width*height*3) // Assume RGB

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			// Convert from 16-bit to 8-bit
			pixelData = append(pixelData, byte(r>>8), byte(g>>8), byte(b>>8))
		}
	}

	return pixelData, nil
}

// CanDecompress checks if data looks like JPEG.
func (d *JPEGDecompressor) CanDecompress(data []byte) bool {
	if len(data) < 3 {
		return false
	}
	// JPEG data starts with 0xFF 0xD8 0xFF (SOI marker)
	return data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
}

// Decompressor Registry

// NewDecompressorRegistry creates a new decompressor registry with built-in decompressors.
func NewDecompressorRegistry() *DecompressorRegistry {
	registry := &DecompressorRegistry{
		decompressors: make(map[CompressionType]Decompressor),
	}

	// Register built-in decompressors
	_ = registry.Register(DEFLATE, NewDeflateDecompressor())
	_ = registry.Register(RLE, NewRLEDecompressor())
	_ = registry.Register(JPEG, NewJPEGDecompressor())

	return registry
}

// Register registers a decompressor for a specific compression type.
func (r *DecompressorRegistry) Register(compressionType CompressionType, decompressor Decompressor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if decompressor == nil {
		return fmt.Errorf("decompressor cannot be nil")
	}

	r.decompressors[compressionType] = decompressor
	return nil
}

// Get retrieves a registered decompressor.
func (r *DecompressorRegistry) Get(compressionType CompressionType) (Decompressor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	decompressor, exists := r.decompressors[compressionType]
	if !exists {
		return nil, fmt.Errorf("decompressor for %s not found", compressionType)
	}

	return decompressor, nil
}

// List returns all registered compression types.
func (r *DecompressorRegistry) List() []CompressionType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]CompressionType, 0, len(r.decompressors))
	for t := range r.decompressors {
		types = append(types, t)
	}
	return types
}

// Decompress decompresses data using the registered decompressor.
func (r *DecompressorRegistry) Decompress(compressionType CompressionType, data []byte) ([]byte, error) {
	decompressor, err := r.Get(compressionType)
	if err != nil {
		return nil, err
	}

	return decompressor.Decompress(data)
}

// Compressor Implementations

// DeflateCompressor handles DEFLATE (zlib) compression.
type DeflateCompressor struct {
	compressionLevel int
	mu               sync.RWMutex
}

// NewDeflateCompressor creates a new DEFLATE compressor.
func NewDeflateCompressor(compressionLevel int) *DeflateCompressor {
	if compressionLevel < flate.DefaultCompression || compressionLevel > flate.BestCompression {
		compressionLevel = flate.DefaultCompression
	}
	return &DeflateCompressor{
		compressionLevel: compressionLevel,
	}
}

// Compress compresses data using DEFLATE.
func (c *DeflateCompressor) Compress(data []byte) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(data) == 0 {
		return []byte{}, nil
	}

	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)

	_, err := writer.Write(data)
	if err != nil {
		return nil, fmt.Errorf("failed to compress with DEFLATE: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close DEFLATE writer: %w", err)
	}

	return buf.Bytes(), nil
}

// RLECompressor handles RLE compression.
type RLECompressor struct {
	mu sync.RWMutex
}

// NewRLECompressor creates a new RLE compressor.
func NewRLECompressor() *RLECompressor {
	return &RLECompressor{}
}

// Compress compresses data using RLE.
func (c *RLECompressor) Compress(data []byte) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(data) == 0 {
		return []byte{}, nil
	}

	var result []byte
	i := 0

	for i < len(data) {
		// Look ahead to find runs
		runByte := data[i]
		runLength := 1

		for i+runLength < len(data) && data[i+runLength] == runByte && runLength < 256 {
			runLength++
		}

		// If we have a run of at least 3 bytes, encode it as a run
		if runLength >= 3 {
			// Encode as run: control byte (257 - runLength) followed by the byte
			result = append(result, byte(257-runLength), runByte)
			i += runLength
		} else {
			// Literal run: find consecutive non-repeating bytes
			literalStart := i
			literalLength := 1
			for i+literalLength < len(data) && literalLength < 129 {
				// Check if next byte starts a run
				nextByte := data[i+literalLength]
				if i+literalLength+2 < len(data) &&
					data[i+literalLength+1] == nextByte &&
					data[i+literalLength+2] == nextByte {
					break // Start of a run, stop the literal
				}
				literalLength++
			}

			// Encode literal: control byte (literalLength-1) followed by bytes
			result = append(result, byte(literalLength-1))
			result = append(result, data[literalStart:literalStart+literalLength]...)
			i += literalLength
		}
	}

	return result, nil
}

// MaxInflateRatio is the largest expansion a deflated stream may claim relative
// to its own compressed size.
//
// A ratio bound catches what an absolute bound alone cannot. An absolute limit
// of 256 MiB permits a 300 KB file to allocate 256 MiB before being rejected,
// and because io.ReadAll grows its buffer by doubling, the true peak is closer
// to twice that. The attacker chooses the input size; without a ratio the cost
// of rejecting a bomb is set entirely by the limit rather than by the effort
// spent constructing it.
//
// 1000:1 is deliberately generous. Deflate reaches it on genuinely repetitive
// medical data — an all-black CT frame is mostly zeros — so a tighter ratio
// would reject legitimate files.
const MaxInflateRatio int64 = 1000

// MinInflateAllowance is the smallest output any deflated stream may produce
// regardless of how small its compressed form is.
//
// Without a floor the ratio bound would reject a 500-byte stream holding a
// legitimately blank 512 KiB frame. The floor is what keeps the ratio safe to
// apply to genuinely tiny inputs.
const MinInflateAllowance int64 = 8 << 20

// InflateLimitFor returns how many bytes a deflated stream of compressedSize
// bytes may be allowed to produce, given an absolute ceiling.
//
// Pass a non-positive compressedSize when the compressed length is unknown; the
// absolute ceiling is then the only bound available.
func InflateLimitFor(compressedSize, absoluteMax int64) int64 {
	// Short-circuit before multiplying, so a large compressedSize cannot
	// overflow into a small or negative limit.
	if compressedSize <= 0 || compressedSize > absoluteMax/MaxInflateRatio {
		return absoluteMax
	}

	limit := compressedSize * MaxInflateRatio
	if limit < MinInflateAllowance {
		limit = MinInflateAllowance
	}
	if limit > absoluteMax {
		limit = absoluteMax
	}
	return limit
}
