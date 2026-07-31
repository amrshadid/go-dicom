package compress

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"strings"
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

// RLEHeaderSize is the fixed size of the RLE segment header that begins every
// RLE-compressed frame (PS3.5 §G.5): a segment count followed by 15 offsets.
const RLEHeaderSize = 64

// MaxRLESegments is the number of segment offsets the header can hold.
const MaxRLESegments = 15

// Decompress decompresses one RLE-compressed frame, inferring its layout from
// the segment count in the header.
//
// A frame is not a single byte stream. It begins with a 64-byte header
// (PS3.5 §G.5) naming up to 15 independently encoded segments, and each sample
// of each pixel is split across one segment per byte, most significant first.
// Reassembling the pixels means decoding every segment and interleaving them.
//
// An earlier version did none of that: it PackBits-decoded the whole fragment,
// header included, treating the offsets as control bytes. For MR_small_RLE.dcm
// that produced 8736 bytes where 8192 is correct, and the content was not
// pixels at any offset.
//
// The layout is inferred because this method's signature carries no image
// metadata. Segment count is samples per pixel times bytes per sample, so 1 and
// 2 are single-sample 8- and 16-bit, and 3 and 6 are three-sample. Callers that
// know the real values should use DecompressFrame, which does not guess.
func (d *RLEDecompressor) Decompress(data []byte) ([]byte, error) {
	// Empty in, empty out. A zero-length frame is not valid RLE, but callers
	// iterating frames should not have to special-case an absent one.
	if len(data) == 0 {
		return []byte{}, nil
	}

	offsets, err := parseRLEHeader(data)
	if err != nil {
		return nil, err
	}

	var samplesPerPixel, bitsAllocated int
	switch len(offsets) {
	case 1:
		samplesPerPixel, bitsAllocated = 1, 8
	case 2:
		samplesPerPixel, bitsAllocated = 1, 16
	case 3:
		samplesPerPixel, bitsAllocated = 3, 8
	case 4:
		samplesPerPixel, bitsAllocated = 1, 32
	case 6:
		samplesPerPixel, bitsAllocated = 3, 16
	default:
		return nil, fmt.Errorf("rle: cannot infer image layout from %d segments; use DecompressFrame",
			len(offsets))
	}

	return d.DecompressFrame(data, samplesPerPixel, bitsAllocated)
}

// DecompressFrame decompresses one RLE-compressed frame with the layout stated
// rather than inferred.
//
// The returned bytes are little endian and pixel-interleaved (planar
// configuration 0): for color data the samples of a pixel are adjacent, and
// for multi-byte samples the least significant byte comes first. RLE stores the
// opposite of both — one planar segment per byte position, most significant
// first — so the reassembly reverses the byte order within each sample.
func (d *RLEDecompressor) DecompressFrame(data []byte, samplesPerPixel, bitsAllocated int) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if samplesPerPixel < 1 {
		return nil, fmt.Errorf("rle: samples per pixel must be at least 1, got %d", samplesPerPixel)
	}
	if bitsAllocated < 8 || bitsAllocated%8 != 0 {
		return nil, fmt.Errorf("rle: bits allocated must be a positive multiple of 8, got %d", bitsAllocated)
	}
	bytesPerSample := bitsAllocated / 8

	offsets, err := parseRLEHeader(data)
	if err != nil {
		return nil, err
	}

	wantSegments := samplesPerPixel * bytesPerSample
	if len(offsets) != wantSegments {
		return nil, fmt.Errorf("rle: header declares %d segments but %d samples of %d bits need %d",
			len(offsets), samplesPerPixel, bitsAllocated, wantSegments)
	}

	// Decode every segment first: each holds one byte position of one sample
	// for every pixel, so they must all be the same length.
	segments := make([][]byte, len(offsets))
	pixelCount := -1
	for i, off := range offsets {
		end := len(data)
		if i+1 < len(offsets) {
			end = int(offsets[i+1])
		}
		if int(off) > len(data) || end > len(data) || int(off) > end {
			return nil, fmt.Errorf("rle: segment %d spans [%d,%d) outside a %d byte frame",
				i, off, end, len(data))
		}

		segment, err := decodeRLESegment(data[off:end])
		if err != nil {
			return nil, fmt.Errorf("rle: segment %d: %w", i, err)
		}
		if pixelCount == -1 {
			pixelCount = len(segment)
		} else if len(segment) != pixelCount {
			return nil, fmt.Errorf("rle: segment %d decoded to %d bytes, segment 0 gave %d; "+
				"every segment holds one byte per pixel and they must agree",
				i, len(segment), pixelCount)
		}
		segments[i] = segment
	}

	out := make([]byte, pixelCount*wantSegments)
	for sample := 0; sample < samplesPerPixel; sample++ {
		for b := 0; b < bytesPerSample; b++ {
			segment := segments[sample*bytesPerSample+b]

			// Segments run most significant byte first; the output is little
			// endian, so segment b lands at the mirrored position.
			dst := sample*bytesPerSample + (bytesPerSample - 1 - b)
			stride := samplesPerPixel * bytesPerSample
			for p := 0; p < pixelCount; p++ {
				out[p*stride+dst] = segment[p]
			}
		}
	}

	return out, nil
}

// parseRLEHeader reads the segment offsets from an RLE frame header.
//
// The header states how many segments follow and where each begins, as offsets
// from the start of the frame. Trailing offsets beyond the declared count are
// reserved and must be ignored rather than treated as segments.
func parseRLEHeader(data []byte) ([]uint32, error) {
	if len(data) < RLEHeaderSize {
		return nil, fmt.Errorf("rle: frame is %d bytes, too short for the %d byte segment header",
			len(data), RLEHeaderSize)
	}

	count := binary.LittleEndian.Uint32(data[0:4])
	if count == 0 || count > MaxRLESegments {
		return nil, fmt.Errorf("rle: header declares %d segments, which is outside 1..%d",
			count, MaxRLESegments)
	}

	offsets := make([]uint32, count)
	for i := range offsets {
		off := binary.LittleEndian.Uint32(data[4+i*4 : 8+i*4])
		if off < RLEHeaderSize || int(off) > len(data) {
			return nil, fmt.Errorf("rle: segment %d starts at %d, outside the frame body [%d,%d]",
				i, off, RLEHeaderSize, len(data))
		}
		if i > 0 && off < offsets[i-1] {
			return nil, fmt.Errorf("rle: segment %d starts at %d, before segment %d at %d",
				i, off, i-1, offsets[i-1])
		}
		offsets[i] = off
	}
	return offsets, nil
}

// decodeRLESegment expands one PackBits-encoded segment (PS3.5 §G.3).
//
//   - 0x00-0x7F: the next N+1 bytes are literal
//   - 0x80:      no-op
//   - 0x81-0xFF: repeat the next byte 257-N times
//
// A run cut short by the end of the segment ends the segment rather than
// failing. PS3.5 §G.3.2 requires every segment to occupy an even number of
// bytes, so encoders append a pad byte after the last complete run — and a pad
// byte is indistinguishable from a control byte that begins a run. In
// MR_small_RLE.dcm the encoded data ends at byte 1883 having produced exactly
// the expected 4096 bytes, and the 1884th is a 0x00 pad that reads as the start
// of a one-byte literal run with nothing to follow it. Treating that as
// corruption rejects a conformant file.
//
// Genuine corruption is still caught, by the caller: every segment carries one
// byte per pixel, so DecompressFrame requires them all to decode to the same
// length. A segment truncated anywhere but the pad byte comes out short.
func decodeRLESegment(data []byte) ([]byte, error) {
	var result []byte

	for i := 0; i < len(data); {
		control := data[i]
		i++

		switch {
		case control <= 0x7F:
			n := int(control) + 1
			if i+n > len(data) {
				return result, nil
			}
			result = append(result, data[i:i+n]...)
			i += n

		case control == 0x80:
			// No-op.

		default:
			if i >= len(data) {
				return result, nil
			}
			n := 257 - int(control)
			for j := 0; j < n; j++ {
				result = append(result, data[i])
			}
			i++
		}
	}

	return result, nil
}

// CanDecompress reports whether data begins with a plausible RLE segment
// header.
//
// This previously returned false unconditionally, with a comment that RLE
// cannot be identified without decompressing it. That is not so: the 64-byte
// header is checkable on its own, and returning false meant callers that ask
// before decoding were told RLE data was not RLE.
func (d *RLEDecompressor) CanDecompress(data []byte) bool {
	_, err := parseRLEHeader(data)
	return err == nil
}

// JPEGDecompressor handles JPEG decompression.
type JPEGDecompressor struct {
	mu sync.RWMutex

	// KeepYCbCr leaves a color-transformed JPEG in its own color space rather
	// than converting to RGB. Set it when the instance's Photometric
	// Interpretation is one of the YBR forms.
	KeepYCbCr bool
}

// NewJPEGDecompressor creates a new JPEG decompressor.
func NewJPEGDecompressor() *JPEGDecompressor {
	return &JPEGDecompressor{}
}

// NewJPEGDecompressorForPhotometric returns a decompressor that leaves the
// samples in the color space the Photometric Interpretation names.
//
// A JPEG carrying YCbCr is normally converted to RGB on decode. That is right
// for an instance whose attribute says RGB and wrong for one that says
// YBR_FULL, where the attribute describes the samples as stored and a reader
// will convert them itself.
func NewJPEGDecompressorForPhotometric(photometric string) *JPEGDecompressor {
	return &JPEGDecompressor{KeepYCbCr: strings.HasPrefix(photometric, "YBR")}
}

// Decompress decompresses JPEG data and returns raw pixel data.
func (d *JPEGDecompressor) Decompress(data []byte) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(data) == 0 {
		return nil, fmt.Errorf("empty data for JPEG decompression")
	}

	// The standard library covers 8-bit frames, baseline and progressive alike,
	// and is the better decoder for them. It rejects 12-bit precision, which
	// JPEG Extended allows and which DICOM uses for the extra depth that made
	// the syntax worth defining; those go to the decoder here.
	if sequentialJPEGPrecision(data) == 12 {
		img, err := decodeSequentialJPEG(data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode 12-bit JPEG: %w", err)
		}
		return img.pack(), nil
	}

	reader := bytes.NewReader(data)
	img, err := jpeg.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JPEG: %w", err)
	}

	return samplesFromImage(img, d.KeepYCbCr), nil
}

// samplesFromImage flattens a decoded JPEG to DICOM pixel data.
//
// The sample count has to follow the image, not a guess. This emitted three
// bytes per pixel unconditionally, so a grayscale frame came back at triple
// length with every value repeated — and the accessors above, sizing from
// SamplesPerPixel, then kept the first third of it. The picture was the first
// third of the rows, each pixel smeared across three, and nothing reported a
// problem.
//
// keepYCbCr leaves a color-transformed JPEG in its own color space. A DICOM
// instance whose Photometric Interpretation is YBR_FULL describes the samples
// as stored, and converting them to RGB while the attribute still says YBR
// leaves a reader to apply the conversion a second time.
func samplesFromImage(img image.Image, keepYCbCr bool) []byte {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	switch src := img.(type) {
	case *image.Gray:
		out := make([]byte, 0, width*height)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				out = append(out, src.GrayAt(x, y).Y)
			}
		}
		return out

	case *image.YCbCr:
		out := make([]byte, 0, width*height*3)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if keepYCbCr {
					c := src.YCbCrAt(x, y)
					out = append(out, c.Y, c.Cb, c.Cr)
					continue
				}
				r, g, b, _ := src.At(x, y).RGBA()
				out = append(out, byte(r>>8), byte(g>>8), byte(b>>8))
			}
		}
		return out
	}

	out := make([]byte, 0, width*height*3)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			out = append(out, byte(r>>8), byte(g>>8), byte(b>>8))
		}
	}
	return out
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

// Compress compresses data as a single-sample 8-bit RLE frame.
//
// The result is a conformant frame: a 64-byte segment header followed by one
// PackBits segment. An earlier version emitted the PackBits stream alone, with
// no header, which no DICOM tool could read — and which this package's own
// decompressor accepted, because it had the matching defect.
//
// For anything other than 8-bit single-sample data, use CompressFrame.
func (c *RLECompressor) Compress(data []byte) ([]byte, error) {
	return c.CompressFrame(data, 1, 8)
}

// CompressFrame compresses one frame of little-endian, pixel-interleaved data
// into an RLE frame.
//
// The input layout is what DecompressFrame returns, and this reverses it:
// samples are separated into planar segments, each sample's bytes are emitted
// most significant first, and every segment is padded to an even length as
// PS3.5 §G.3.2 requires.
func (c *RLECompressor) CompressFrame(data []byte, samplesPerPixel, bitsAllocated int) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(data) == 0 {
		return []byte{}, nil
	}
	if samplesPerPixel < 1 {
		return nil, fmt.Errorf("rle: samples per pixel must be at least 1, got %d", samplesPerPixel)
	}
	if bitsAllocated < 8 || bitsAllocated%8 != 0 {
		return nil, fmt.Errorf("rle: bits allocated must be a positive multiple of 8, got %d", bitsAllocated)
	}

	bytesPerSample := bitsAllocated / 8
	stride := samplesPerPixel * bytesPerSample
	if len(data)%stride != 0 {
		return nil, fmt.Errorf("rle: %d bytes is not a whole number of %d-byte pixels", len(data), stride)
	}
	pixelCount := len(data) / stride

	segmentCount := samplesPerPixel * bytesPerSample
	if segmentCount > MaxRLESegments {
		return nil, fmt.Errorf("rle: %d samples of %d bits need %d segments, more than the %d a header holds",
			samplesPerPixel, bitsAllocated, segmentCount, MaxRLESegments)
	}

	segments := make([][]byte, 0, segmentCount)
	for sample := 0; sample < samplesPerPixel; sample++ {
		for b := 0; b < bytesPerSample; b++ {
			// Mirrors DecompressFrame: segment b holds the byte at the
			// mirrored position, since segments run most significant first
			// while the input is little endian.
			src := sample*bytesPerSample + (bytesPerSample - 1 - b)
			plane := make([]byte, pixelCount)
			for p := 0; p < pixelCount; p++ {
				plane[p] = data[p*stride+src]
			}

			encoded := encodeRLESegment(plane)
			if len(encoded)%2 != 0 {
				encoded = append(encoded, 0x00)
			}
			segments = append(segments, encoded)
		}
	}

	var out bytes.Buffer
	offset := uint32(RLEHeaderSize)
	header := make([]byte, RLEHeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(segments)))
	for i, seg := range segments {
		binary.LittleEndian.PutUint32(header[4+i*4:8+i*4], offset)
		offset += uint32(len(seg))
	}
	out.Write(header)
	for _, seg := range segments {
		out.Write(seg)
	}
	return out.Bytes(), nil
}

// encodeRLESegment PackBits-encodes one byte plane (PS3.5 §G.3).
func encodeRLESegment(data []byte) []byte {
	var result []byte
	i := 0

	for i < len(data) {
		// Look ahead to find runs
		runByte := data[i]
		runLength := 1

		// A replicate run is encoded as 257-N, and the control byte for a run
		// must land in 0x81..0xFF — so N tops out at 128. Allowing 256 emitted
		// 257-256 = 1, which a decoder reads as a two-byte *literal* run.
		for i+runLength < len(data) && data[i+runLength] == runByte && runLength < 128 {
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
			// The literal control byte is N-1 and must stay below 0x80, so a
			// literal run holds at most 128 bytes. Allowing 129 emitted 0x80,
			// which is the no-op marker, losing the bytes that followed it.
			for i+literalLength < len(data) && literalLength < 128 {
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

	return result
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
