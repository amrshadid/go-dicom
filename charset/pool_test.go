package charset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/charset"
)

func TestDecodeBytesBuffered(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		encodings []string
		want      string
	}{
		{
			name:      "small data",
			data:      []byte("Hello World"),
			encodings: []string{"UTF-8"},
			want:      "Hello World",
		},
		{
			name:      "large data",
			data:      make([]byte, 10000),
			encodings: []string{"ISO-8859-1"},
			want:      string(make([]byte, 10000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fill large data with 'A'
			if len(tt.data) > 100 {
				for i := range tt.data {
					tt.data[i] = 'A'
				}
			}

			got, err := charset.DecodeBytesBuffered(tt.data, tt.encodings, charset.DefaultTextDelimiters)
			if err != nil {
				t.Errorf("DecodeBytesBuffered() error = %v", err)
				return
			}

			if len(tt.data) > 100 {
				// For large data, just check length
				if len(got) != len(tt.data) {
					t.Errorf("DecodeBytesBuffered() length = %d, want %d", len(got), len(tt.data))
				}
			} else {
				if got != tt.want {
					t.Errorf("DecodeBytesBuffered() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestEncodeStringBuffered(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		encodings []string
	}{
		{
			name:      "small string",
			value:     "Hello World",
			encodings: []string{"UTF-8"},
		},
		{
			name:      "large string",
			value:     string(make([]byte, 10000)),
			encodings: []string{"ISO-8859-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := charset.EncodeStringBuffered(tt.value, tt.encodings)
			if err != nil {
				t.Errorf("EncodeStringBuffered() error = %v", err)
				return
			}

			// Verify round-trip
			decoded, err := charset.DecodeBytes(encoded, tt.encodings, charset.DefaultTextDelimiters)
			if err != nil {
				t.Errorf("Round-trip decode error = %v", err)
				return
			}

			if len(decoded) != len(tt.value) {
				t.Errorf("Round-trip length = %d, want %d", len(decoded), len(tt.value))
			}
		})
	}
}

func TestBatchDecodeBytes(t *testing.T) {
	values := [][]byte{
		[]byte("Patient Name"),
		[]byte("Study Description"),
		[]byte("Series Description"),
		[]byte("Protocol Name"),
	}

	encodings := []string{"UTF-8"}

	results, err := charset.BatchDecodeBytes(values, encodings, charset.DefaultTextDelimiters)
	if err != nil {
		t.Errorf("BatchDecodeBytes() error = %v", err)
		return
	}

	if len(results) != len(values) {
		t.Errorf("BatchDecodeBytes() returned %d results, want %d", len(results), len(values))
		return
	}

	expected := []string{"Patient Name", "Study Description", "Series Description", "Protocol Name"}
	for i, want := range expected {
		if results[i] != want {
			t.Errorf("BatchDecodeBytes()[%d] = %q, want %q", i, results[i], want)
		}
	}
}

func TestBatchEncodeStrings(t *testing.T) {
	values := []string{
		"Patient Name",
		"Study Description",
		"Series Description",
		"Protocol Name",
	}

	encodings := []string{"UTF-8"}

	results, err := charset.BatchEncodeStrings(values, encodings)
	if err != nil {
		t.Errorf("BatchEncodeStrings() error = %v", err)
		return
	}

	if len(results) != len(values) {
		t.Errorf("BatchEncodeStrings() returned %d results, want %d", len(results), len(values))
		return
	}

	// Verify round-trip
	for i, encoded := range results {
		decoded, err := charset.DecodeBytes(encoded, encodings, charset.DefaultTextDelimiters)
		if err != nil {
			t.Errorf("Round-trip decode error for [%d] = %v", i, err)
			continue
		}
		if decoded != values[i] {
			t.Errorf("Round-trip [%d] = %q, want %q", i, decoded, values[i])
		}
	}
}

func TestStreamDecoder(t *testing.T) {
	decoder := charset.NewStreamDecoder([]string{"UTF-8"}, charset.DefaultTextDelimiters, 0)

	chunks := [][]byte{
		[]byte("Hello "),
		[]byte("World "),
		[]byte("from "),
		[]byte("streaming "),
		[]byte("decoder!"),
	}

	for _, chunk := range chunks {
		err := decoder.DecodeChunk(chunk)
		if err != nil {
			t.Errorf("DecodeChunk() error = %v", err)
			return
		}
	}

	result := decoder.String()
	want := "Hello World from streaming decoder!"

	if result != want {
		t.Errorf("StreamDecoder result = %q, want %q", result, want)
	}

	if decoder.Len() != len(want) {
		t.Errorf("StreamDecoder.Len() = %d, want %d", decoder.Len(), len(want))
	}
}

func TestStreamDecoder_Reset(t *testing.T) {
	decoder := charset.NewStreamDecoder([]string{"UTF-8"}, charset.DefaultTextDelimiters, 0)

	err := decoder.DecodeChunk([]byte("First chunk"))
	if err != nil {
		t.Errorf("DecodeChunk() error = %v", err)
		return
	}

	if decoder.Len() == 0 {
		t.Error("StreamDecoder should have data before reset")
	}

	decoder.Reset()

	if decoder.Len() != 0 {
		t.Errorf("StreamDecoder.Len() after reset = %d, want 0", decoder.Len())
	}

	err = decoder.DecodeChunk([]byte("Second chunk"))
	if err != nil {
		t.Errorf("DecodeChunk() after reset error = %v", err)
		return
	}

	result := decoder.String()
	if result != "Second chunk" {
		t.Errorf("StreamDecoder after reset = %q, want %q", result, "Second chunk")
	}
}

func TestStreamDecoder_LargeData(t *testing.T) {
	decoder := charset.NewStreamDecoder([]string{"ISO-8859-1"}, charset.DefaultTextDelimiters, 1024)

	// Create large data with ASCII only to avoid multi-byte issues
	totalSize := 102400 // 100KB (multiple of chunk size)
	chunkSize := 1024
	totalChunks := totalSize / chunkSize

	for i := 0; i < totalChunks; i++ {
		chunk := make([]byte, chunkSize)
		for j := range chunk {
			chunk[j] = byte('A' + (j % 26))
		}

		err := decoder.DecodeChunk(chunk)
		if err != nil {
			t.Errorf("DecodeChunk() error at chunk %d = %v", i, err)
			return
		}
	}

	result := decoder.String()
	if len(result) != totalSize {
		t.Errorf("StreamDecoder result length = %d, want %d", len(result), totalSize)
	}
}

// Benchmark buffered operations
func BenchmarkDecodeBytesBuffered_Small(b *testing.B) {
	data := []byte("Hello World")
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytesBuffered(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecodeBytesBuffered_Large(b *testing.B) {
	data := make([]byte, 100000)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytesBuffered(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkBatchDecodeBytes(b *testing.B) {
	values := make([][]byte, 100)
	for i := range values {
		values[i] = []byte("Patient Name")
	}
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.BatchDecodeBytes(values, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkBatchEncodeStrings(b *testing.B) {
	values := make([]string, 100)
	for i := range values {
		values[i] = "Patient Name"
	}
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.BatchEncodeStrings(values, encodings)
	}
}

func BenchmarkStreamDecoder(b *testing.B) {
	chunks := make([][]byte, 100)
	for i := range chunks {
		chunks[i] = []byte("Hello World ")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoder := charset.NewStreamDecoder([]string{"UTF-8"}, charset.DefaultTextDelimiters, 0)
		for _, chunk := range chunks {
			_ = decoder.DecodeChunk(chunk)
		}
		_ = decoder.String()
	}
}
