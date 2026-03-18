package compress_test

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func TestDebugTagFormat(t *testing.T) {
	// DICOM tag (FFFE,E000) in little-endian should be: FE FF 00 E0
	data := []byte{0xFE, 0xFF, 0x00, 0xE0}

	// Read as little-endian uint32
	tag := binary.LittleEndian.Uint32(data)
	fmt.Printf("Tag bytes: %X %X %X %X\n", data[0], data[1], data[2], data[3])
	fmt.Printf("Tag as uint32: 0x%08X\n", tag)
	fmt.Printf("Expected: 0x%08X\n", uint32(0xFFFEE000))

	// The correct tag should be
	correctTag := uint32(0xE000FFFE)
	fmt.Printf("Correct for little-endian: 0x%08X\n", correctTag)
}
