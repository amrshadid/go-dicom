package filebase_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
)

// TestExecuteReadHook tests the read hook execution.
func TestExecuteReadHook(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	reader := filebase.NewFileReader(bytes.NewReader(data))
	reader.SetByteOrder(filebase.LittleEndian)

	result := filebase.ExecuteReadHook(reader, 4, 0)

	if result["bytes_read"] != 4 {
		t.Errorf("bytes_read = %v, want 4", result["bytes_read"])
	}
	if result["position"] != int64(0) {
		t.Errorf("position = %v, want 0", result["position"])
	}
	if result["byte_order"] != "LittleEndian" {
		t.Errorf("byte_order = %v, want LittleEndian", result["byte_order"])
	}
}

// TestExecuteWriteHook tests the write hook execution.
func TestExecuteWriteHook(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	writer.SetByteOrder(filebase.BigEndian)

	result := filebase.ExecuteWriteHook(writer, 2, 10)

	if result["bytes_written"] != 2 {
		t.Errorf("bytes_written = %v, want 2", result["bytes_written"])
	}
	if result["position"] != int64(10) {
		t.Errorf("position = %v, want 10", result["position"])
	}
	if result["byte_order"] != "BigEndian" {
		t.Errorf("byte_order = %v, want BigEndian", result["byte_order"])
	}
}

// TestExecuteByteOrderHook tests the byte order hook execution.
func TestExecuteByteOrderHook(t *testing.T) {
	result := filebase.ExecuteByteOrderHook(filebase.LittleEndian, filebase.BigEndian)

	if result["old_byte_order"] != "LittleEndian" {
		t.Errorf("old_byte_order = %v, want LittleEndian", result["old_byte_order"])
	}
	if result["new_byte_order"] != "BigEndian" {
		t.Errorf("new_byte_order = %v, want BigEndian", result["new_byte_order"])
	}
}

// TestExecuteSeekHook tests the seek hook execution.
func TestExecuteSeekHook(t *testing.T) {
	result := filebase.ExecuteSeekHook(10, 50)

	if result["old_position"] != int64(10) {
		t.Errorf("old_position = %v, want 10", result["old_position"])
	}
	if result["new_position"] != int64(50) {
		t.Errorf("new_position = %v, want 50", result["new_position"])
	}
	if result["offset"] != int64(40) {
		t.Errorf("offset = %v, want 40", result["offset"])
	}
}

// TestHooksIntegration tests integration of hooks with file operations.
func TestHooksIntegration(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	reader := filebase.NewFileReader(bytes.NewReader(data))
	reader.SetByteOrder(filebase.LittleEndian)

	// Simulate read operation with hook
	initialPos, _ := reader.Tell()
	_, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("ReadByte error = %v", err)
	}

	afterReadPos, _ := reader.Tell()
	readHookResult := filebase.ExecuteReadHook(reader, 1, initialPos)

	if readHookResult["bytes_read"] != 1 {
		t.Error("Read hook did not record correct bytes_read")
	}

	// Simulate byte order change with hook
	oldOrder := reader.GetByteOrder()
	reader.SetByteOrder(filebase.BigEndian)
	newOrder := reader.GetByteOrder()
	byteOrderHookResult := filebase.ExecuteByteOrderHook(oldOrder, newOrder)

	if byteOrderHookResult["old_byte_order"] != "LittleEndian" {
		t.Error("ByteOrder hook did not record correct old_byte_order")
	}
	if byteOrderHookResult["new_byte_order"] != "BigEndian" {
		t.Error("ByteOrder hook did not record correct new_byte_order")
	}

	// Simulate seek operation with hook
	reader.Seek(0, 0)
	seekPos, _ := reader.Tell()
	seekHookResult := filebase.ExecuteSeekHook(afterReadPos, seekPos)

	if seekHookResult["offset"] != int64(-1) {
		t.Errorf("Seek hook offset = %v, want -1", seekHookResult["offset"])
	}
}

// TestHooksWithWriteOperations tests hooks with write operations.
func TestHooksWithWriteOperations(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	writer.SetByteOrder(filebase.LittleEndian)

	// Write data and track with hook
	initialPos, _ := writer.Tell()
	err := writer.WriteBytes([]byte{0x01, 0x02, 0x03, 0x04})
	if err != nil {
		t.Fatalf("WriteBytes error = %v", err)
	}

	afterWritePos, _ := writer.Tell()
	writeHookResult := filebase.ExecuteWriteHook(writer, int(afterWritePos-initialPos), initialPos)

	if writeHookResult["bytes_written"] != 4 {
		t.Errorf("bytes_written = %v, want 4", writeHookResult["bytes_written"])
	}
}

// TestHooksDataStructure tests that hook results have correct structure.
func TestHooksDataStructure(t *testing.T) {
	// Test read hook structure
	data := []byte{0x01, 0x02}
	reader := filebase.NewFileReader(bytes.NewReader(data))
	readResult := filebase.ExecuteReadHook(reader, 2, 0)

	if _, ok := readResult["bytes_read"]; !ok {
		t.Error("Read hook result missing bytes_read field")
	}
	if _, ok := readResult["position"]; !ok {
		t.Error("Read hook result missing position field")
	}
	if _, ok := readResult["byte_order"]; !ok {
		t.Error("Read hook result missing byte_order field")
	}

	// Test byte order hook structure
	boResult := filebase.ExecuteByteOrderHook(filebase.LittleEndian, filebase.BigEndian)
	if _, ok := boResult["old_byte_order"]; !ok {
		t.Error("ByteOrder hook result missing old_byte_order field")
	}
	if _, ok := boResult["new_byte_order"]; !ok {
		t.Error("ByteOrder hook result missing new_byte_order field")
	}

	// Test seek hook structure
	seekResult := filebase.ExecuteSeekHook(0, 100)
	if _, ok := seekResult["old_position"]; !ok {
		t.Error("Seek hook result missing old_position field")
	}
	if _, ok := seekResult["new_position"]; !ok {
		t.Error("Seek hook result missing new_position field")
	}
	if _, ok := seekResult["offset"]; !ok {
		t.Error("Seek hook result missing offset field")
	}
}
