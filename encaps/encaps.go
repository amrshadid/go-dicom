// Package encaps provides DICOM encapsulation parsing and handling.
// Encapsulation is used for compressed pixel data in DICOM files.
// This module handles:
// - Parsing encapsulated pixel data with Basic Offset Table and fragments
// - Extracting individual frames from encapsulated data
// - Working with different compression formats
// - Frame boundary detection and management
package encaps

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/errors"
)

// Parser handles parsing of encapsulated DICOM pixel data.
// It reads encapsulated data and extracts frames and fragments.
type Parser struct {
	reader       io.Reader
	data         []byte
	littleEndian bool
	offset       int64
}

// NewParser creates a new encapsulation parser.
func NewParser(reader io.Reader, littleEndian bool) *Parser {
	return &Parser{
		reader:       reader,
		littleEndian: littleEndian,
		offset:       0,
	}
}

// ParseEncapsulatedData reads and parses encapsulated pixel data from the reader.
// Returns the parsed EncapsulatedData structure with frames and offset table.
func (p *Parser) ParseEncapsulatedData() (*compress.EncapsulatedData, error) {
	if p.reader == nil {
		return nil, errors.NewEncapsulationError("failed to parse encapsulation", "reader is nil")
	}

	// Read all data
	data, err := io.ReadAll(p.reader)
	if err != nil {
		return nil, errors.NewEncapsulationError("failed to read encapsulated data", fmt.Sprintf("read error: %v", err))
	}

	p.data = data
	p.offset = 0

	// Parse the encapsulation
	encData, err := p.parseEncapsulation()
	if err != nil {
		return nil, err
	}

	return encData, nil
}

// parseEncapsulation is the internal method that handles the parsing logic.
func (p *Parser) parseEncapsulation() (*compress.EncapsulatedData, error) {
	encData := &compress.EncapsulatedData{
		Fragments:  make([][]byte, 0),
		Endianness: "<",
	}

	if !p.littleEndian {
		encData.Endianness = ">"
	}

	// Parse Basic Offset Table
	bot, err := p.parseItem()
	if err != nil {
		return nil, errors.NewEncapsulationError("failed to parse basic offset table", fmt.Sprintf("item parse error: %v", err))
	}

	// Decode basic offset table
	if len(bot) > 0 {
		offsets, err := p.decodeBasicOffsetTable(bot)
		if err != nil {
			return nil, errors.NewEncapsulationError("failed to decode basic offset table", fmt.Sprintf("decode error: %v", err))
		}
		encData.BasicOffsetTable = offsets
		encData.NumberOfFrames = len(offsets)
	}

	// Parse fragments
	for p.offset < int64(len(p.data)) {
		fragment, err := p.parseItem()
		if err != nil {
			// Check if we reached end of items
			if err == io.EOF {
				break
			}
			return nil, errors.NewEncapsulationError("failed to parse fragment", fmt.Sprintf("item parse error: %v", err))
		}

		if len(fragment) > 0 {
			encData.Fragments = append(encData.Fragments, fragment)
		}
	}

	if len(encData.Fragments) == 0 {
		return nil, errors.NewEncapsulationError("failed to parse encapsulation", "no fragments found")
	}

	return encData, nil
}

// parseItem reads and returns a single DICOM item (tag-length-value).
// Items are delimited by item tags (0xFFFE, 0xE000 for data items or
// 0xFFFE, 0xE0DD for sequence delimiter).
func (p *Parser) parseItem() ([]byte, error) {
	if p.offset+8 > int64(len(p.data)) {
		return nil, io.EOF
	}

	// Read item tag (should be 0xFFFE, 0xE000 or 0xFFFE, 0xE0DD)
	group := p.readUint16()
	element := p.readUint16()

	if group != 0xFFFE {
		return nil, fmt.Errorf("invalid item tag: expected 0xFFFE, got 0x%04X", group)
	}

	// Check for sequence delimiter (0xFFFE, 0xE0DD)
	if element == 0xE0DD {
		return nil, io.EOF
	}

	// Expected to be 0xE000 (data item tag)
	if element != 0xE000 {
		return nil, fmt.Errorf("invalid item tag: expected 0xE000, got 0x%04X", element)
	}

	// Read item length
	length := p.readUint32()

	// Handle undefined length (0xFFFFFFFF means length follows in fragments)
	if length == 0xFFFFFFFF {
		// This is used in some encapsulation formats
		return p.parseFragmentedItem()
	}

	// Ensure we have enough data
	if p.offset+int64(length) > int64(len(p.data)) {
		return nil, fmt.Errorf("insufficient data for item: need %d bytes, have %d", length, int64(len(p.data))-p.offset)
	}

	// Extract item data
	itemData := p.data[p.offset : p.offset+int64(length)]
	p.offset += int64(length)

	return itemData, nil
}

// parseFragmentedItem handles items with undefined length.
func (p *Parser) parseFragmentedItem() ([]byte, error) {
	// For fragmented items, we need to read until we find end-of-item marker
	itemStart := p.offset
	for p.offset+8 <= int64(len(p.data)) {
		group := p.readUint16()
		element := p.readUint16()
		length := p.readUint32()

		if group == 0xFFFE && element == 0xE00D {
			// End of item marker found
			if length != 0 {
				return nil, fmt.Errorf("expected item length 0 for end marker, got %d", length)
			}
			break
		}

		// Skip this fragment
		p.offset += int64(length)
	}

	return p.data[itemStart : p.offset-8], nil // Exclude end marker
}

// decodeBasicOffsetTable extracts frame offsets from the BOT data.
func (p *Parser) decodeBasicOffsetTable(data []byte) ([]uint32, error) {
	if len(data) == 0 {
		return []uint32{}, nil
	}

	// BOT entries are 4-byte unsigned integers
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("invalid basic offset table size: %d (not divisible by 4)", len(data))
	}

	count := len(data) / 4
	offsets := make([]uint32, count)

	for i := 0; i < count; i++ {
		offset := p.decodeUint32(data[i*4 : (i+1)*4])
		offsets[i] = offset
	}

	return offsets, nil
}

// readUint16 reads a uint16 from the current offset and advances the offset.
func (p *Parser) readUint16() uint16 {
	value := p.decodeUint16(p.data[p.offset : p.offset+2])
	p.offset += 2
	return value
}

// readUint32 reads a uint32 from the current offset and advances the offset.
func (p *Parser) readUint32() uint32 {
	value := p.decodeUint32(p.data[p.offset : p.offset+4])
	p.offset += 4
	return value
}

// decodeUint16 decodes a uint16 respecting the byte order.
func (p *Parser) decodeUint16(data []byte) uint16 {
	if p.littleEndian {
		return binary.LittleEndian.Uint16(data)
	}
	return binary.BigEndian.Uint16(data)
}

// decodeUint32 decodes a uint32 respecting the byte order.
func (p *Parser) decodeUint32(data []byte) uint32 {
	if p.littleEndian {
		return binary.LittleEndian.Uint32(data)
	}
	return binary.BigEndian.Uint32(data)
}

// Extractor handles extracting frames from encapsulated data.
type Extractor struct {
	encData *compress.EncapsulatedData
}

// NewExtractor creates a new frame extractor.
func NewExtractor(encData *compress.EncapsulatedData) *Extractor {
	return &Extractor{
		encData: encData,
	}
}

// ExtractFrame returns the compressed data for a specific frame.
// Frame index is 0-based. If BasicOffsetTable is present, uses it for frame boundaries.
func (e *Extractor) ExtractFrame(frameIndex int) ([]byte, error) {
	if e.encData == nil {
		return nil, errors.NewEncapsulationError("failed to extract frame", "encapsulated data is nil")
	}

	if frameIndex < 0 || (e.encData.NumberOfFrames > 0 && frameIndex >= e.encData.NumberOfFrames) {
		return nil, fmt.Errorf("frame index out of range: %d", frameIndex)
	}

	// If we have a basic offset table, use it to determine frame boundaries
	if len(e.encData.BasicOffsetTable) > 0 {
		return e.extractFrameUsingBOT(frameIndex)
	}

	// Otherwise, assume each fragment is a frame (or one frame is one fragment)
	return e.extractFrameByFragment(frameIndex)
}

// extractFrameUsingBOT uses the Basic Offset Table to extract frame data.
func (e *Extractor) extractFrameUsingBOT(frameIndex int) ([]byte, error) {
	if frameIndex >= len(e.encData.BasicOffsetTable) {
		return nil, fmt.Errorf("frame index %d exceeds BOT entries %d", frameIndex, len(e.encData.BasicOffsetTable))
	}

	startOffset := e.encData.BasicOffsetTable[frameIndex]

	// Determine end offset
	var endOffset uint32
	if frameIndex+1 < len(e.encData.BasicOffsetTable) {
		endOffset = e.encData.BasicOffsetTable[frameIndex+1]
	} else {
		// For last frame, calculate from fragment sizes
		endOffset = uint32(e.getTotalFragmentSize())
	}

	// Collect all fragments that fall within this frame's range
	var frameData []byte
	currentOffset := uint32(0)

	for _, fragment := range e.encData.Fragments {
		fragmentSize := uint32(len(fragment))

		// Check if this fragment is within the frame range
		if currentOffset >= startOffset && currentOffset < endOffset {
			frameData = append(frameData, fragment...)
		}

		// Also include fragments that partially overlap
		nextOffset := currentOffset + fragmentSize
		if nextOffset > startOffset && currentOffset < endOffset {
			frameData = append(frameData, fragment...)
		}

		currentOffset = nextOffset
	}

	if len(frameData) == 0 {
		return nil, fmt.Errorf("no fragment data found for frame %d (offset %d-%d)", frameIndex, startOffset, endOffset)
	}

	return frameData, nil
}

// extractFrameByFragment assumes each fragment or group of fragments forms a frame.
func (e *Extractor) extractFrameByFragment(frameIndex int) ([]byte, error) {
	if frameIndex >= len(e.encData.Fragments) {
		return nil, fmt.Errorf("frame index %d exceeds fragment count %d", frameIndex, len(e.encData.Fragments))
	}

	return e.encData.Fragments[frameIndex], nil
}

// getTotalFragmentSize returns the sum of all fragment sizes.
func (e *Extractor) getTotalFragmentSize() uint64 {
	var total uint64
	for _, fragment := range e.encData.Fragments {
		total += uint64(len(fragment))
	}
	return total
}

// GetFrameCount returns the number of frames in the encapsulated data.
func (e *Extractor) GetFrameCount() int {
	if e.encData == nil {
		return 0
	}
	if e.encData.NumberOfFrames > 0 {
		return e.encData.NumberOfFrames
	}
	// If no BOT, assume one frame per fragment (or use fragment count as estimate)
	return len(e.encData.Fragments)
}

// GetFragmentCount returns the total number of fragments.
func (e *Extractor) GetFragmentCount() int {
	if e.encData == nil {
		return 0
	}
	return len(e.encData.Fragments)
}

// Reframer handles conversion between different encapsulation formats.
type Reframer struct {
	sourceEncData *compress.EncapsulatedData
	targetFrames  int
}

// NewReframer creates a new reframer for format conversion.
func NewReframer(encData *compress.EncapsulatedData, targetFrames int) *Reframer {
	return &Reframer{
		sourceEncData: encData,
		targetFrames:  targetFrames,
	}
}

// ReframeData reorganizes fragments to match a target frame count.
// Useful for handling multiple frames compressed together or individual compression.
func (r *Reframer) ReframeData() (*compress.EncapsulatedData, error) {
	if r.sourceEncData == nil {
		return nil, errors.NewEncapsulationError("failed to reframe data", "source encapsulation is nil")
	}

	if r.targetFrames <= 0 {
		return nil, errors.NewEncapsulationError("failed to reframe data", "target frames must be positive")
	}

	// For now, create a simple reframing that concatenates fragments
	// More sophisticated reframing might split or merge based on offsets
	newEncData := &compress.EncapsulatedData{
		Fragments:  make([][]byte, 0),
		Endianness: r.sourceEncData.Endianness,
	}

	// Collect all fragment data
	var allData bytes.Buffer
	for _, fragment := range r.sourceEncData.Fragments {
		allData.Write(fragment)
	}

	totalSize := allData.Len()
	frameSize := totalSize / r.targetFrames

	// Split into target frames
	data := allData.Bytes()
	offset := 0

	for i := 0; i < r.targetFrames; i++ {
		var frameData []byte

		if i == r.targetFrames-1 {
			// Last frame gets remaining data
			frameData = data[offset:]
		} else {
			frameData = data[offset : offset+frameSize]
		}

		if len(frameData) > 0 {
			newEncData.Fragments = append(newEncData.Fragments, frameData)
		}

		offset += frameSize
	}

	newEncData.NumberOfFrames = r.targetFrames
	return newEncData, nil
}

// Validator checks encapsulated data for consistency and validity.
type Validator struct{}

// NewValidator creates a new encapsulation validator.
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateEncapsulation checks if the encapsulated data is well-formed.
func (v *Validator) ValidateEncapsulation(encData *compress.EncapsulatedData) error {
	if encData == nil {
		return errors.NewEncapsulationError("validation failed", "encapsulation is nil")
	}

	// Check fragments exist
	if len(encData.Fragments) == 0 {
		return errors.NewEncapsulationError("validation failed", "no fragments present")
	}

	// Check endianness
	if encData.Endianness != "<" && encData.Endianness != ">" {
		return errors.NewEncapsulationError("validation failed", fmt.Sprintf("invalid endianness: %s", encData.Endianness))
	}

	// Check basic offset table if present
	if len(encData.BasicOffsetTable) > 0 {
		// BOT should have same count as NumberOfFrames
		if encData.NumberOfFrames != len(encData.BasicOffsetTable) {
			return errors.NewEncapsulationError(
				"validation failed",
				fmt.Sprintf("BOT count (%d) != NumberOfFrames (%d)", len(encData.BasicOffsetTable), encData.NumberOfFrames),
			)
		}

		// All offsets should be valid
		for i, offset := range encData.BasicOffsetTable {
			if offset > uint32(v.getTotalFragmentSize(encData)) {
				return errors.NewEncapsulationError(
					"validation failed",
					fmt.Sprintf("offset %d for frame %d exceeds total size", offset, i),
				)
			}
		}
	}

	return nil
}

// getTotalFragmentSize returns the sum of all fragment sizes.
func (v *Validator) getTotalFragmentSize(encData *compress.EncapsulatedData) uint64 {
	var total uint64
	for _, fragment := range encData.Fragments {
		total += uint64(len(fragment))
	}
	return total
}

// Statistics provides information about encapsulated data.
type Statistics struct {
	// FrameCount is the number of frames
	FrameCount int

	// FragmentCount is the total number of fragments
	FragmentCount int

	// TotalSize is the total size in bytes
	TotalSize uint64

	// AverageFrameSize is the average size per frame
	AverageFrameSize uint64

	// HasBasicOffsetTable indicates if BOT is present
	HasBasicOffsetTable bool

	// HasExtendedOffsetTable indicates if extended BOT is present
	HasExtendedOffsetTable bool
}

// GetStatistics returns statistics about the encapsulated data.
func GetStatistics(encData *compress.EncapsulatedData) *Statistics {
	if encData == nil {
		return &Statistics{}
	}

	stats := &Statistics{
		FrameCount:             encData.NumberOfFrames,
		FragmentCount:          len(encData.Fragments),
		HasBasicOffsetTable:    len(encData.BasicOffsetTable) > 0,
		HasExtendedOffsetTable: len(encData.ExtendedOffsetTable) > 0,
	}

	// Calculate total size
	for _, fragment := range encData.Fragments {
		stats.TotalSize += uint64(len(fragment))
	}

	// Calculate average frame size
	if stats.FrameCount > 0 {
		stats.AverageFrameSize = stats.TotalSize / uint64(stats.FrameCount)
	}

	return stats
}
