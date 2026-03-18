package filebase_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
)

// TestByteOrderString tests the String method of ByteOrder.
func TestByteOrderString(t *testing.T) {
	tests := []struct {
		bo       filebase.ByteOrder
		expected string
	}{
		{filebase.LittleEndian, "LittleEndian"},
		{filebase.BigEndian, "BigEndian"},
		{filebase.ByteOrder(99), "Unknown"},
	}

	for _, tt := range tests {
		got := tt.bo.String()
		if got != tt.expected {
			t.Errorf("ByteOrder.String() = %s, want %s", got, tt.expected)
		}
	}
}

// TestNewFileReader tests creating a new FileReader.
func TestNewFileReader(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	buf := bytes.NewReader(data)

	reader := filebase.NewFileReader(buf)

	if reader == nil {
		t.Fatal("NewFileReader returned nil")
	}

	if reader.GetByteOrder() != filebase.LittleEndian {
		t.Errorf("Default byte order = %v, want LittleEndian", reader.GetByteOrder())
	}

	pos, _ := reader.Tell()
	if pos != 0 {
		t.Errorf("Initial position = %d, want 0", pos)
	}
}

// TestFileReaderReadByte tests reading a single byte.
func TestFileReaderReadByte(t *testing.T) {
	data := []byte{0x42}
	reader := filebase.NewFileReader(bytes.NewReader(data))

	b, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("ReadByte() error = %v", err)
	}

	if b != 0x42 {
		t.Errorf("ReadByte() = 0x%02x, want 0x42", b)
	}
}

// TestFileReaderReadBytes tests reading multiple bytes.
func TestFileReaderReadBytes(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	reader := filebase.NewFileReader(bytes.NewReader(data))

	b := make([]byte, 4)
	err := reader.ReadBytes(b)
	if err != nil {
		t.Fatalf("ReadBytes() error = %v", err)
	}

	if !bytes.Equal(b, data) {
		t.Errorf("ReadBytes() = %v, want %v", b, data)
	}
}

// TestFileReaderReadBytesShort tests reading when not enough bytes available.
func TestFileReaderReadBytesShort(t *testing.T) {
	data := []byte{0x01, 0x02}
	reader := filebase.NewFileReader(bytes.NewReader(data))

	b := make([]byte, 4)
	err := reader.ReadBytes(b)
	if err == nil {
		t.Error("ReadBytes() should return error for insufficient data")
	}
}

// TestFileReaderReadUint16LittleEndian tests reading uint16 in little-endian.
func TestFileReaderReadUint16LittleEndian(t *testing.T) {
	data := []byte{0x34, 0x12} // Little-endian 0x1234
	reader := filebase.NewFileReader(bytes.NewReader(data))
	reader.SetByteOrder(filebase.LittleEndian)

	v, err := reader.ReadUint16()
	if err != nil {
		t.Fatalf("ReadUint16() error = %v", err)
	}

	if v != 0x1234 {
		t.Errorf("ReadUint16() = 0x%04x, want 0x1234", v)
	}
}

// TestFileReaderReadUint16BigEndian tests reading uint16 in big-endian.
func TestFileReaderReadUint16BigEndian(t *testing.T) {
	data := []byte{0x12, 0x34} // Big-endian 0x1234
	reader := filebase.NewFileReader(bytes.NewReader(data))
	reader.SetByteOrder(filebase.BigEndian)

	v, err := reader.ReadUint16()
	if err != nil {
		t.Fatalf("ReadUint16() error = %v", err)
	}

	if v != 0x1234 {
		t.Errorf("ReadUint16() = 0x%04x, want 0x1234", v)
	}
}

// TestFileReaderReadUint32LittleEndian tests reading uint32 in little-endian.
func TestFileReaderReadUint32LittleEndian(t *testing.T) {
	data := []byte{0x78, 0x56, 0x34, 0x12} // Little-endian 0x12345678
	reader := filebase.NewFileReader(bytes.NewReader(data))
	reader.SetByteOrder(filebase.LittleEndian)

	v, err := reader.ReadUint32()
	if err != nil {
		t.Fatalf("ReadUint32() error = %v", err)
	}

	if v != 0x12345678 {
		t.Errorf("ReadUint32() = 0x%08x, want 0x12345678", v)
	}
}

// TestFileReaderReadUint32BigEndian tests reading uint32 in big-endian.
func TestFileReaderReadUint32BigEndian(t *testing.T) {
	data := []byte{0x12, 0x34, 0x56, 0x78} // Big-endian 0x12345678
	reader := filebase.NewFileReader(bytes.NewReader(data))
	reader.SetByteOrder(filebase.BigEndian)

	v, err := reader.ReadUint32()
	if err != nil {
		t.Fatalf("ReadUint32() error = %v", err)
	}

	if v != 0x12345678 {
		t.Errorf("ReadUint32() = 0x%08x, want 0x12345678", v)
	}
}

// TestFileReaderSeek tests seeking in the reader.
func TestFileReaderSeek(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	reader := filebase.NewFileReader(bytes.NewReader(data))

	// Seek to offset 2
	pos, err := reader.Seek(2, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}

	if pos != 2 {
		t.Errorf("Seek() position = %d, want 2", pos)
	}

	// Read should start at offset 2
	b, _ := reader.ReadByte()
	if b != 0x03 {
		t.Errorf("ReadByte() after Seek = 0x%02x, want 0x03", b)
	}
}

// TestFileReaderTell tests getting the current position.
func TestFileReaderTell(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	reader := filebase.NewFileReader(bytes.NewReader(data))

	// Initial position
	pos, _ := reader.Tell()
	if pos != 0 {
		t.Errorf("Tell() initial = %d, want 0", pos)
	}

	// After reading 2 bytes
	reader.ReadBytes(make([]byte, 2))
	pos, _ = reader.Tell()
	if pos != 2 {
		t.Errorf("Tell() after read = %d, want 2", pos)
	}
}

// TestFileReaderSetByteOrder tests setting byte order.
func TestFileReaderSetByteOrder(t *testing.T) {
	reader := filebase.NewFileReader(bytes.NewReader(nil))

	if reader.GetByteOrder() != filebase.LittleEndian {
		t.Error("Default should be LittleEndian")
	}

	reader.SetByteOrder(filebase.BigEndian)
	if reader.GetByteOrder() != filebase.BigEndian {
		t.Error("SetByteOrder() failed")
	}
}

// TestNewFileWriter tests creating a new FileWriter.
func TestNewFileWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})

	if writer == nil {
		t.Fatal("NewFileWriter returned nil")
	}

	if writer.GetByteOrder() != filebase.LittleEndian {
		t.Errorf("Default byte order = %v, want LittleEndian", writer.GetByteOrder())
	}
}

// TestFileWriterWriteByte tests writing a single byte.
func TestFileWriterWriteByte(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})

	err := writer.WriteByte(0x42)
	if err != nil {
		t.Fatalf("WriteByte() error = %v", err)
	}

	if buf.Bytes()[0] != 0x42 {
		t.Errorf("WriteByte() wrote 0x%02x, want 0x42", buf.Bytes()[0])
	}
}

// TestFileWriterWriteBytes tests writing multiple bytes.
func TestFileWriterWriteBytes(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})

	data := []byte{0x01, 0x02, 0x03, 0x04}
	err := writer.WriteBytes(data)
	if err != nil {
		t.Fatalf("WriteBytes() error = %v", err)
	}

	if !bytes.Equal(buf.Bytes(), data) {
		t.Errorf("WriteBytes() wrote %v, want %v", buf.Bytes(), data)
	}
}

// TestFileWriterWriteUint16LittleEndian tests writing uint16 in little-endian.
func TestFileWriterWriteUint16LittleEndian(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	writer.SetByteOrder(filebase.LittleEndian)

	err := writer.WriteUint16(0x1234)
	if err != nil {
		t.Fatalf("WriteUint16() error = %v", err)
	}

	expected := []byte{0x34, 0x12}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("WriteUint16() wrote %v, want %v", buf.Bytes(), expected)
	}
}

// TestFileWriterWriteUint16BigEndian tests writing uint16 in big-endian.
func TestFileWriterWriteUint16BigEndian(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	writer.SetByteOrder(filebase.BigEndian)

	err := writer.WriteUint16(0x1234)
	if err != nil {
		t.Fatalf("WriteUint16() error = %v", err)
	}

	expected := []byte{0x12, 0x34}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("WriteUint16() wrote %v, want %v", buf.Bytes(), expected)
	}
}

// TestFileWriterWriteUint32LittleEndian tests writing uint32 in little-endian.
func TestFileWriterWriteUint32LittleEndian(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	writer.SetByteOrder(filebase.LittleEndian)

	err := writer.WriteUint32(0x12345678)
	if err != nil {
		t.Fatalf("WriteUint32() error = %v", err)
	}

	expected := []byte{0x78, 0x56, 0x34, 0x12}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("WriteUint32() wrote %v, want %v", buf.Bytes(), expected)
	}
}

// TestFileWriterWriteUint32BigEndian tests writing uint32 in big-endian.
func TestFileWriterWriteUint32BigEndian(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	writer.SetByteOrder(filebase.BigEndian)

	err := writer.WriteUint32(0x12345678)
	if err != nil {
		t.Fatalf("WriteUint32() error = %v", err)
	}

	expected := []byte{0x12, 0x34, 0x56, 0x78}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("WriteUint32() wrote %v, want %v", buf.Bytes(), expected)
	}
}

// TestBufferPool tests the BufferPool functionality.
func TestBufferPool(t *testing.T) {
	pool := filebase.NewBufferPool(1024)

	// Get a buffer
	buf := pool.Get()
	if cap(buf) < 1024 {
		t.Errorf("Buffer capacity = %d, want >= 1024", cap(buf))
	}

	// Use the buffer
	buf = append(buf, 1, 2, 3)

	// Return to pool
	pool.Put(buf)

	// Get another buffer (should be from pool)
	buf2 := pool.Get()
	if cap(buf2) < 1024 {
		t.Errorf("Retrieved buffer capacity = %d, want >= 1024", cap(buf2))
	}
}

// TestPosition tests the Position struct.
func TestPosition(t *testing.T) {
	pos := filebase.Position{
		Offset: 128,
		Tag:    0x00100010,
		VR:     "PN",
		Length: 16,
	}

	if pos.Offset != 128 {
		t.Errorf("Position.Offset = %d, want 128", pos.Offset)
	}

	if pos.Tag != 0x00100010 {
		t.Errorf("Position.Tag = 0x%08x, want 0x00100010", pos.Tag)
	}

	if pos.VR != "PN" {
		t.Errorf("Position.VR = %s, want PN", pos.VR)
	}

	if pos.Length != 16 {
		t.Errorf("Position.Length = %d, want 16", pos.Length)
	}
}

// readWriteSeeker is a test helper that implements io.ReadWriteSeeker.
type readWriteSeeker struct {
	*bytes.Buffer
	position int64
}

func (rws *readWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		rws.position = offset
	case io.SeekCurrent:
		rws.position += offset
	case io.SeekEnd:
		rws.position = int64(rws.Len()) + offset
	}
	return rws.position, nil
}

// TestReadWriteSequence tests reading and writing in sequence.
func TestReadWriteSequence(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})

	// Write some data
	writer.WriteUint16(0x1234)
	writer.WriteUint32(0x56789ABC)

	// Read it back
	reader := filebase.NewFileReader(bytes.NewReader(buf.Bytes()))
	reader.SetByteOrder(filebase.LittleEndian)

	v16, _ := reader.ReadUint16()
	v32, _ := reader.ReadUint32()

	if v16 != 0x1234 {
		t.Errorf("Read uint16 = 0x%04x, want 0x1234", v16)
	}

	if v32 != 0x56789ABC {
		t.Errorf("Read uint32 = 0x%08x, want 0x56789ABC", v32)
	}
}

// TestReaderInterfaceCompliance tests that FileReader implements Reader.
func TestReaderInterfaceCompliance(t *testing.T) {
	var _ filebase.Reader = (*filebase.FileReader)(nil)
}

// TestWriterInterfaceCompliance tests that FileWriter implements Writer.
func TestWriterInterfaceCompliance(t *testing.T) {
	var _ filebase.Writer = (*filebase.FileWriter)(nil)
}

// TestByteOrderSwitching tests switching byte order during reading.
func TestByteOrderSwitching(t *testing.T) {
	data := []byte{0x34, 0x12}
	reader := filebase.NewFileReader(bytes.NewReader(data))

	// Read as little-endian first
	reader.SetByteOrder(filebase.LittleEndian)
	reader.Seek(0, io.SeekStart)
	v1, _ := reader.ReadUint16()

	// Switch to big-endian and read again
	reader.Seek(0, io.SeekStart)
	reader.SetByteOrder(filebase.BigEndian)
	v2, _ := reader.ReadUint16()

	if v1 == v2 {
		t.Error("Byte order switch should produce different values")
	}
}

// TestMultipleReads tests multiple consecutive reads.
func TestMultipleReads(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	reader := filebase.NewFileReader(bytes.NewReader(data))

	// Read three bytes individually
	b1, _ := reader.ReadByte()
	b2, _ := reader.ReadByte()
	b3, _ := reader.ReadByte()

	if b1 != 0x01 || b2 != 0x02 || b3 != 0x03 {
		t.Errorf("Multiple reads failed: got %02x %02x %02x", b1, b2, b3)
	}

	pos, _ := reader.Tell()
	if pos != 3 {
		t.Errorf("Position after 3 reads = %d, want 3", pos)
	}
}
