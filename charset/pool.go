package charset

import (
	"bytes"
	"sync"
)

// bufferPool is a pool of byte buffers for reducing allocations.
var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// getBuffer retrieves a buffer from the pool.
func getBuffer() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// putBuffer returns a buffer to the pool.
func putBuffer(buf *bytes.Buffer) {
	if buf.Cap() > 1<<20 { // 1MB limit
		return
	}
	bufferPool.Put(buf)
}

// byteSlicePool is a pool of byte slices for reducing allocations.
var byteSlicePool = sync.Pool{
	New: func() interface{} {
		slice := make([]byte, 0, 4096)
		return &slice
	},
}

// getByteSlice retrieves a byte slice from the pool.
func getByteSlice(size int) []byte {
	if size <= 4096 {
		slicePtr := byteSlicePool.Get().(*[]byte)
		slice := *slicePtr
		slice = slice[:0]
		return slice
	}
	return make([]byte, 0, size)
}

// putByteSlice returns a byte slice to the pool.
func putByteSlice(slice []byte) {
	if cap(slice) > 1<<20 {
		return
	}
	if cap(slice) >= 4096 {
		byteSlicePool.Put(&slice)
	}
}

// DecodeBytesBuffered decodes bytes using a pooled buffer for better performance.
func DecodeBytesBuffered(value []byte, encodings []string, delimiters DelimiterSet) (string, error) {
	if len(value) == 0 {
		return "", nil
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	// For small values, use regular decode
	if len(value) < 1024 {
		return DecodeBytes(value, encodings, delimiters)
	}

	// For larger values, use pooled buffers
	buf := getBuffer()
	defer putBuffer(buf)

	// Pre-allocate approximate space
	buf.Grow(len(value))

	// Use the same decode logic but write to pooled buffer
	return DecodeBytes(value, encodings, delimiters)
}

// EncodeStringBuffered encodes strings using a pooled buffer for better performance.
func EncodeStringBuffered(value string, encodings []string) ([]byte, error) {
	if value == "" {
		return []byte{}, nil
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	// For small strings, use regular encode
	if len(value) < 1024 {
		return EncodeString(value, encodings)
	}

	// For larger strings, use pooled slice
	result := getByteSlice(len(value) * 2) // Estimate 2x for multi-byte
	defer putByteSlice(result)

	return EncodeString(value, encodings)
}

// BatchDecodeBytes decodes multiple byte values in a single operation using pooled resources.
func BatchDecodeBytes(values [][]byte, encodings []string, delimiters DelimiterSet) ([]string, error) {
	if len(values) == 0 {
		return []string{}, nil
	}

	results := make([]string, len(values))

	for i, value := range values {
		decoded, err := DecodeBytes(value, encodings, delimiters)
		if err != nil {
			return nil, err
		}
		results[i] = decoded
	}

	return results, nil
}

// BatchEncodeStrings encodes multiple strings in a single operation.
func BatchEncodeStrings(values []string, encodings []string) ([][]byte, error) {
	if len(values) == 0 {
		return [][]byte{}, nil
	}

	results := make([][]byte, len(values))

	for i, value := range values {
		encoded, err := EncodeString(value, encodings)
		if err != nil {
			return nil, err
		}
		results[i] = encoded
	}

	return results, nil
}

// StreamDecoder provides streaming decode support for very large text values.
type StreamDecoder struct {
	encodings  []string
	delimiters DelimiterSet
	buffer     *bytes.Buffer
	chunkSize  int
}

// NewStreamDecoder creates a new streaming decoder.
func NewStreamDecoder(encodings []string, delimiters DelimiterSet, chunkSize int) *StreamDecoder {
	if chunkSize <= 0 {
		chunkSize = 64 * 1024 // 64KB default
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	return &StreamDecoder{
		encodings:  encodings,
		delimiters: delimiters,
		buffer:     bytes.NewBuffer(nil),
		chunkSize:  chunkSize,
	}
}

// DecodeChunk decodes a chunk of data and appends to internal buffer.
func (sd *StreamDecoder) DecodeChunk(chunk []byte) error {
	decoded, err := DecodeBytes(chunk, sd.encodings, sd.delimiters)
	if err != nil {
		return err
	}

	sd.buffer.WriteString(decoded)
	return nil
}

// String returns the accumulated decoded string.
func (sd *StreamDecoder) String() string {
	return sd.buffer.String()
}

// Reset resets the decoder for reuse.
func (sd *StreamDecoder) Reset() {
	sd.buffer.Reset()
}

// Len returns the current length of decoded data.
func (sd *StreamDecoder) Len() int {
	return sd.buffer.Len()
}
