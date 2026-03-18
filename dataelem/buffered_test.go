package dataelem_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

func TestStreamingValue_Read(t *testing.T) {
	data := []byte("Test data for streaming")
	reader := bytes.NewReader(data)

	sv := dataelem.NewStreamingValue(reader, int64(len(data)))

	buffer := make([]byte, 10)
	n, err := sv.Read(buffer)

	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if n != 10 {
		t.Errorf("Read length = %d, want 10", n)
	}

	if string(buffer) != "Test data " {
		t.Errorf("Read data = %q, want 'Test data '", string(buffer))
	}
}

func TestStreamingValue_Size(t *testing.T) {
	data := []byte("Test data")
	reader := bytes.NewReader(data)

	sv := dataelem.NewStreamingValue(reader, int64(len(data)))

	if sv.Size() != 9 {
		t.Errorf("Size = %d, want 9", sv.Size())
	}
}

func TestStreamingValue_Bytes(t *testing.T) {
	data := []byte("Test data")
	reader := bytes.NewReader(data)

	sv := dataelem.NewStreamingValue(reader, int64(len(data)))

	result, err := sv.Bytes()
	if err != nil {
		t.Fatalf("Bytes failed: %v", err)
	}

	if !bytes.Equal(result, data) {
		t.Errorf("Bytes = %q, want %q", string(result), string(data))
	}
}

func TestBufferedDataElement_SetStreamingValue(t *testing.T) {
	data := []byte("Large binary data")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)

	if err := bde.SetStreamingValue(reader, int64(len(data))); err != nil {
		t.Fatalf("SetStreamingValue failed: %v", err)
	}

	if !bde.IsBuffered() {
		t.Error("Element should be marked as buffered")
	}
}

func TestBufferedDataElement_SetStreamingValueNil(t *testing.T) {
	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)

	err := bde.SetStreamingValue(nil, 0)
	if err == nil {
		t.Error("Expected error for nil reader")
	}
}

func TestBufferedDataElement_GetValue(t *testing.T) {
	data := []byte("Test value")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetStreamingValue(reader, int64(len(data)))

	val := bde.GetValue()
	if val == nil {
		t.Error("GetValue returned nil")
	}

	// Should return the BufferedValue interface
	if _, ok := val.(dataelem.BufferedValue); !ok {
		t.Errorf("GetValue type = %T, want BufferedValue", val)
	}
}

func TestBufferedDataElement_GetValueBytes(t *testing.T) {
	data := []byte("Test data for bytes")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetStreamingValue(reader, int64(len(data)))

	result, err := bde.GetValueBytes()
	if err != nil {
		t.Fatalf("GetValueBytes failed: %v", err)
	}

	if !bytes.Equal(result, data) {
		t.Errorf("GetValueBytes = %q, want %q", result, data)
	}
}

func TestBufferedDataElement_GetValueSize(t *testing.T) {
	data := []byte("12345")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetStreamingValue(reader, 5)

	if bde.GetValueSize() != 5 {
		t.Errorf("GetValueSize = %d, want 5", bde.GetValueSize())
	}
}

func TestBufferedDataElement_ReadBuffered(t *testing.T) {
	data := []byte("Stream data content")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetStreamingValue(reader, int64(len(data)))

	buffer := make([]byte, 6)
	n, err := bde.ReadBuffered(buffer)

	if err != nil {
		t.Fatalf("ReadBuffered failed: %v", err)
	}

	if n != 6 {
		t.Errorf("ReadBuffered n = %d, want 6", n)
	}

	if string(buffer) != "Stream" {
		t.Errorf("ReadBuffered data = %q, want 'Stream'", string(buffer))
	}
}

func TestBufferedDataElement_CloseBuffered(t *testing.T) {
	data := []byte("Test")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetStreamingValue(reader, int64(len(data)))

	err := bde.CloseBuffered()
	if err != nil {
		t.Fatalf("CloseBuffered failed: %v", err)
	}
}

func TestBufferedDataElement_SetValue_DisablesBuffering(t *testing.T) {
	data := []byte("Original")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetStreamingValue(reader, int64(len(data)))

	if !bde.IsBuffered() {
		t.Error("Should be buffered initially")
	}

	bde.SetValue("Regular value")

	if bde.IsBuffered() {
		t.Error("Should not be buffered after SetValue")
	}
}

func TestBufferedDataElement_GetTag(t *testing.T) {
	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)

	if bde.GetTag() != tag.New(0x0008, 0x1140) {
		t.Error("GetTag mismatch")
	}
}

func TestBufferedDataElement_GetVR(t *testing.T) {
	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)

	if bde.GetVR() != dataelem.OB {
		t.Errorf("GetVR = %s, want OB", bde.GetVR())
	}
}

func TestStreamProcessor_ProcessBufferedElement(t *testing.T) {
	data := []byte("1234567890")
	reader := bytes.NewReader(data)

	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetStreamingValue(reader, int64(len(data)))

	processor := dataelem.NewStreamProcessor(3) // 3-byte buffer

	chunks := 0
	totalBytes := 0

	err := processor.ProcessBufferedElement(bde, func(chunk []byte) error {
		chunks++
		totalBytes += len(chunk)
		return nil
	})

	if err != nil {
		t.Fatalf("ProcessBufferedElement failed: %v", err)
	}

	if chunks < 1 {
		t.Errorf("Chunks processed = %d, want >= 1", chunks)
	}
}

func TestStreamProcessor_ProcessNonBufferedElement(t *testing.T) {
	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetValue([]byte("Regular data"))

	processor := dataelem.NewStreamProcessor(4096)

	processed := false
	err := processor.ProcessBufferedElement(bde, func(chunk []byte) error {
		processed = true
		return nil
	})

	if err != nil {
		t.Fatalf("ProcessBufferedElement failed: %v", err)
	}

	if !processed {
		t.Error("Processor function should have been called")
	}
}

func TestBufferedDataElementBuilder(t *testing.T) {
	builder := dataelem.NewBufferedDataElementBuilder(tag.New(0x0008, 0x1140), dataelem.OB)

	bde, err := builder.WithValue([]byte("test")).Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if bde == nil {
		t.Error("Built element should not be nil")
	}

	if !bde.IsBuffered() == false && bde.GetValue() == nil {
		t.Error("Built element has no value")
	}
}

func TestBufferedDataElementBuilder_WithStreamingValue(t *testing.T) {
	data := []byte("streaming")
	reader := bytes.NewReader(data)

	builder := dataelem.NewBufferedDataElementBuilder(tag.New(0x0008, 0x1140), dataelem.OB)

	bde, err := builder.WithStreamingValue(reader, int64(len(data))).Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if !bde.IsBuffered() {
		t.Error("Built element should be buffered")
	}
}

func TestStreamingValue_Close(t *testing.T) {
	reader := bytes.NewReader([]byte("test"))
	sv := dataelem.NewStreamingValue(reader, 4)

	err := sv.Close()
	// bytes.Reader doesn't implement Closer, so this should return nil
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestBufferedDataElement_NonBufferedBytes(t *testing.T) {
	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetValue([]byte("regular bytes"))

	result, err := bde.GetValueBytes()
	if err != nil {
		t.Fatalf("GetValueBytes failed: %v", err)
	}

	if !bytes.Equal(result, []byte("regular bytes")) {
		t.Errorf("Bytes = %q, want 'regular bytes'", result)
	}
}

func TestBufferedDataElement_ReadBufferedNotBuffered(t *testing.T) {
	bde := dataelem.NewBufferedDataElement(tag.New(0x0008, 0x1140), dataelem.OB)
	bde.SetValue("not buffered")

	_, err := bde.ReadBuffered(make([]byte, 10))
	if err == nil {
		t.Error("Expected error when reading non-buffered element")
	}
}
