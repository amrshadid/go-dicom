package dataelem

import (
	"fmt"
	"github.com/amrshadid/go-dicom/tag"
	"io"
	"sync"
)

// BufferedValue represents a value that can be read from a stream
type BufferedValue interface {
	// Read reads up to len(p) bytes into p
	Read(p []byte) (n int, err error)

	// Size returns the total size of the buffered value in bytes
	Size() int64

	// Close closes the underlying reader if applicable
	Close() error

	// Bytes returns all bytes (may load entire value into memory)
	Bytes() ([]byte, error)
}

// StreamingValue wraps an io.Reader as a BufferedValue
type StreamingValue struct {
	reader io.Reader
	size   int64
	buffer []byte
	once   sync.Once
	err    error
}

// NewStreamingValue creates a new streaming value from a reader
func NewStreamingValue(reader io.Reader, size int64) *StreamingValue {
	return &StreamingValue{
		reader: reader,
		size:   size,
	}
}

// Read implements BufferedValue.Read
func (sv *StreamingValue) Read(p []byte) (int, error) {
	if sv.reader == nil {
		return 0, io.EOF
	}
	return sv.reader.Read(p)
}

// Size implements BufferedValue.Size
func (sv *StreamingValue) Size() int64 {
	return sv.size
}

// Close implements BufferedValue.Close
func (sv *StreamingValue) Close() error {
	if closer, ok := sv.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Bytes loads all bytes into memory
func (sv *StreamingValue) Bytes() ([]byte, error) {
	sv.once.Do(func() {
		sv.buffer, sv.err = io.ReadAll(sv.reader)
	})

	if sv.err != nil {
		return nil, sv.err
	}

	return sv.buffer, nil
}

// BufferedDataElement wraps a DataElement with support for large binary values
type BufferedDataElement struct {
	elem        *DataElement
	bufferedVal BufferedValue
	isBuffered  bool
	mu          sync.RWMutex
}

// NewBufferedDataElement creates a data element with buffered value support
func NewBufferedDataElement(tag interface{}, vr VR) *BufferedDataElement {
	elem := NewDataElement(tag, vr, nil)
	return &BufferedDataElement{
		elem:       elem,
		isBuffered: false,
	}
}

// SetStreamingValue sets a streaming value for large binary data
func (bde *BufferedDataElement) SetStreamingValue(reader io.Reader, size int64) error {
	bde.mu.Lock()
	defer bde.mu.Unlock()

	if reader == nil {
		return fmt.Errorf("reader cannot be nil")
	}

	bde.bufferedVal = NewStreamingValue(reader, size)
	bde.isBuffered = true

	return nil
}

// GetValue returns the value, loading from buffer if necessary
func (bde *BufferedDataElement) GetValue() interface{} {
	bde.mu.RLock()
	defer bde.mu.RUnlock()

	if bde.isBuffered && bde.bufferedVal != nil {
		// For buffered values, we might return a proxy or the bytes
		// This is intentionally lazy-loading
		return bde.bufferedVal
	}

	return bde.elem.GetValue()
}

// GetValueBytes gets the value as bytes, loading if necessary
func (bde *BufferedDataElement) GetValueBytes() ([]byte, error) {
	bde.mu.RLock()
	defer bde.mu.RUnlock()

	if bde.isBuffered && bde.bufferedVal != nil {
		return bde.bufferedVal.Bytes()
	}

	// For non-buffered values, convert to bytes
	val := bde.elem.GetValue()
	if val == nil {
		return []byte{}, nil
	}

	// Handle byte slices
	if byteVal, ok := val.([]byte); ok {
		return byteVal, nil
	}

	// Handle strings
	if strVal, ok := val.(string); ok {
		return []byte(strVal), nil
	}

	return nil, fmt.Errorf("cannot convert value of type %T to bytes", val)
}

// GetValueSize returns the size of the buffered value
func (bde *BufferedDataElement) GetValueSize() int64 {
	bde.mu.RLock()
	defer bde.mu.RUnlock()

	if bde.isBuffered && bde.bufferedVal != nil {
		return bde.bufferedVal.Size()
	}

	// For non-buffered values, estimate from actual data
	val := bde.elem.GetValue()
	if val == nil {
		return 0
	}

	if byteVal, ok := val.([]byte); ok {
		return int64(len(byteVal))
	}

	if strVal, ok := val.(string); ok {
		return int64(len(strVal))
	}

	return 0
}

// IsBuffered checks if this element uses buffered values
func (bde *BufferedDataElement) IsBuffered() bool {
	bde.mu.RLock()
	defer bde.mu.RUnlock()

	return bde.isBuffered
}

// ReadBuffered reads from the buffered value into the provided buffer
func (bde *BufferedDataElement) ReadBuffered(p []byte) (int, error) {
	bde.mu.RLock()
	defer bde.mu.RUnlock()

	if !bde.isBuffered || bde.bufferedVal == nil {
		return 0, fmt.Errorf("element does not have buffered value")
	}

	return bde.bufferedVal.Read(p)
}

// CloseBuffered closes the underlying buffered resource
func (bde *BufferedDataElement) CloseBuffered() error {
	bde.mu.Lock()
	defer bde.mu.Unlock()

	if bde.isBuffered && bde.bufferedVal != nil {
		return bde.bufferedVal.Close()
	}

	return nil
}

// SetValue sets a regular value and disables buffering
func (bde *BufferedDataElement) SetValue(value interface{}) {
	bde.mu.Lock()
	defer bde.mu.Unlock()

	bde.elem.SetValue(value)
	bde.isBuffered = false
	bde.bufferedVal = nil
}

// Delegate methods to wrapped DataElement
func (bde *BufferedDataElement) GetTag() interface{} {
	return bde.elem.GetTag()
}

// Tag returns the element's tag, reporting whether the stored value could be
// read as one. It mirrors DataElement.Tag so a buffered element can be used
// wherever a plain one can without the caller reaching for the untyped form.
func (bde *BufferedDataElement) Tag() (tag.Tag, bool) {
	return bde.elem.Tag()
}

// MustTag returns the element's tag, or zero if it is not a tag.
func (bde *BufferedDataElement) MustTag() tag.Tag {
	return bde.elem.MustTag()
}

func (bde *BufferedDataElement) GetVR() VR {
	return bde.elem.GetVR()
}

func (bde *BufferedDataElement) GetVM() int {
	return bde.elem.GetVM()
}

func (bde *BufferedDataElement) GetKeyword() string {
	return bde.elem.GetKeyword()
}

func (bde *BufferedDataElement) GetDescription() string {
	return bde.elem.GetDescription()
}

func (bde *BufferedDataElement) IsEmpty() bool {
	return bde.elem.IsEmpty()
}

func (bde *BufferedDataElement) IsMultiValue() bool {
	return bde.elem.IsMultiValue()
}

// StreamProcessor processes data elements with streaming/buffered values
type StreamProcessor struct {
	bufferSize int
}

// NewStreamProcessor creates a new stream processor
func NewStreamProcessor(bufferSize int) *StreamProcessor {
	if bufferSize <= 0 {
		bufferSize = 4096 // Default 4KB buffer
	}
	return &StreamProcessor{
		bufferSize: bufferSize,
	}
}

// ProcessBufferedElement processes a buffered element in chunks
func (sp *StreamProcessor) ProcessBufferedElement(bde *BufferedDataElement,
	processor func([]byte) error) error {

	if !bde.IsBuffered() {
		// For non-buffered elements, process all bytes at once
		bytes, err := bde.GetValueBytes()
		if err != nil {
			return fmt.Errorf("failed to get value bytes: %w", err)
		}
		return processor(bytes)
	}

	// Process buffered element in chunks
	buffer := make([]byte, sp.bufferSize)

	for {
		n, err := bde.ReadBuffered(buffer)
		if n > 0 {
			if procErr := processor(buffer[:n]); procErr != nil {
				return procErr
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}

// BufferedDataElementBuilder helps construct buffered data elements
type BufferedDataElementBuilder struct {
	elem   *BufferedDataElement
	errors []error
}

// NewBufferedDataElementBuilder creates a new builder
func NewBufferedDataElementBuilder(tag interface{}, vr VR) *BufferedDataElementBuilder {
	return &BufferedDataElementBuilder{
		elem:   NewBufferedDataElement(tag, vr),
		errors: []error{},
	}
}

// WithValue sets a regular value
func (b *BufferedDataElementBuilder) WithValue(value interface{}) *BufferedDataElementBuilder {
	b.elem.SetValue(value)
	return b
}

// WithStreamingValue sets a streaming value
func (b *BufferedDataElementBuilder) WithStreamingValue(reader io.Reader, size int64) *BufferedDataElementBuilder {
	if err := b.elem.SetStreamingValue(reader, size); err != nil {
		b.errors = append(b.errors, err)
	}
	return b
}

// Build returns the constructed element or an error
func (b *BufferedDataElementBuilder) Build() (*BufferedDataElement, error) {
	if len(b.errors) > 0 {
		return nil, b.errors[0]
	}
	return b.elem, nil
}
