package fileutil_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/fileutil"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// TestNewByteOrderDetector tests creating byte order detector.
func TestNewByteOrderDetector(t *testing.T) {
	buf := &bytes.Buffer{}
	reader := filebase.NewFileReader(&readWriteSeeker{Buffer: buf})
	detector := fileutil.NewByteOrderDetector(reader)

	if detector == nil {
		t.Fatal("NewByteOrderDetector returned nil")
	}
}

// TestDetectFromTransferSyntax tests byte order detection from transfer syntax.
func TestDetectFromTransferSyntax(t *testing.T) {
	tests := []struct {
		ts          string
		shouldError bool
	}{
		{"1.2.840.10008.1.2.1", false},
		{"1.2.840.10008.1.2.2", false},
		{"1.2.840.10008.1.2", false},
		{"invalid_uid", true},
	}

	for _, tt := range tests {
		u := uid.New(tt.ts)
		_, err := fileutil.DetectFromTransferSyntax(u)

		if tt.shouldError && err == nil {
			t.Errorf("expected error for transfer syntax %s", tt.ts)
		}

		if !tt.shouldError && err != nil {
			t.Errorf("unexpected error for transfer syntax %s: %v", tt.ts, err)
		}
	}
}

// TestDetectFromPreamble tests DICOM preamble detection.
func TestDetectFromPreamble(t *testing.T) {
	preamble := make([]byte, 128)
	magic := []byte("DICM")
	data := append(preamble, magic...)

	seeker := &readWriteSeeker{Buffer: bytes.NewBuffer(data), position: 0}
	reader := filebase.NewFileReader(seeker)

	_, err := fileutil.DetectFromPreamble(reader)
	if err != nil {
		t.Fatalf("DetectFromPreamble error: %v", err)
	}
}

// TestPadValue tests value padding.
func TestPadValue(t *testing.T) {
	result := fileutil.PadValue([]byte("test"), 0x20)
	if string(result) != "test" {
		t.Errorf("PadValue failed: %s", result)
	}

	result = fileutil.PadValue([]byte("a"), 0x20)
	if string(result) != "a " {
		t.Errorf("PadValue failed: %s", result)
	}
}

// TestPadValueSpace tests space padding.
func TestPadValueSpace(t *testing.T) {
	result := fileutil.PadValueSpace([]byte("test"))
	if string(result) != "test" {
		t.Errorf("PadValueSpace failed: %s", result)
	}
}

// TestPadValueNull tests null padding.
func TestPadValueNull(t *testing.T) {
	result := fileutil.PadValueNull([]byte("test"))
	if string(result) != "test" {
		t.Errorf("PadValueNull failed: %s", result)
	}
}

// TestUnpadValue tests value unpadding.
func TestUnpadValue(t *testing.T) {
	result := fileutil.UnpadValue([]byte("test   "), 0x20)
	if string(result) != "test" {
		t.Errorf("UnpadValue failed: %s", result)
	}
}

// TestAlignToEvenBoundary tests alignment to even boundary.
func TestAlignToEvenBoundary(t *testing.T) {
	tests := []struct {
		pos      int64
		expected int64
	}{
		{0, 0},
		{1, 2},
		{2, 2},
		{3, 4},
	}

	for _, tt := range tests {
		result := fileutil.AlignToEvenBoundary(tt.pos)
		if result != tt.expected {
			t.Errorf("AlignToEvenBoundary(%d) = %d, want %d", tt.pos, result, tt.expected)
		}
	}
}

// TestNewByteBufferPool tests byte buffer pool creation.
func TestNewByteBufferPool(t *testing.T) {
	pool := fileutil.NewByteBufferPool(10, 1024)
	if pool == nil {
		t.Fatal("NewByteBufferPool returned nil")
	}

	buf := pool.Get()
	if cap(buf) < 1024 {
		t.Errorf("buffer capacity %d < 1024", cap(buf))
	}
}

// TestUint16ToBytes tests uint16 to bytes conversion.
func TestUint16ToBytes(t *testing.T) {
	value := uint16(0x1234)

	result := fileutil.Uint16ToBytes(value, filebase.LittleEndian)
	if len(result) != 2 {
		t.Fatalf("expected 2 bytes, got %d", len(result))
	}
}

// TestUint32ToBytes tests uint32 to bytes conversion.
func TestUint32ToBytes(t *testing.T) {
	value := uint32(0x12345678)

	result := fileutil.Uint32ToBytes(value, filebase.LittleEndian)
	if len(result) != 4 {
		t.Fatalf("expected 4 bytes, got %d", len(result))
	}
}

// TestBytesToUint16 tests bytes to uint16 conversion.
func TestBytesToUint16(t *testing.T) {
	result := fileutil.BytesToUint16([]byte{0x34, 0x12}, filebase.LittleEndian)
	if result == 0 {
		t.Error("BytesToUint16 returned 0")
	}
}

// TestBytesToUint32 tests bytes to uint32 conversion.
func TestBytesToUint32(t *testing.T) {
	result := fileutil.BytesToUint32([]byte{0x78, 0x56, 0x34, 0x12}, filebase.LittleEndian)
	if result == 0 {
		t.Error("BytesToUint32 returned 0")
	}
}

// TestNewFileMetaCache tests file metadata cache creation.
func TestNewFileMetaCache(t *testing.T) {
	cache := fileutil.NewFileMetaCache(100)
	if cache == nil {
		t.Fatal("NewFileMetaCache returned nil")
	}
}

// TestFileMetaCacheGetSet tests cache get and set.
func TestFileMetaCacheGetSet(t *testing.T) {
	cache := fileutil.NewFileMetaCache(100)

	cache.Set("key1", "value1")
	val, exists := cache.Get("key1")

	if !exists {
		t.Error("expected key to exist in cache")
	}

	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}
}

// TestFileMetaCacheClear tests cache clearing.
func TestFileMetaCacheClear(t *testing.T) {
	cache := fileutil.NewFileMetaCache(100)

	cache.Set("key1", "value1")
	cache.Clear()

	_, exists := cache.Get("key1")
	if exists {
		t.Error("expected key to not exist after clear")
	}
}

// TestFileMetaCacheStats tests cache statistics.
func TestFileMetaCacheStats(t *testing.T) {
	cache := fileutil.NewFileMetaCache(100)

	cache.Set("key1", "value1")
	cache.Get("key1")
	cache.Get("nonexistent")

	hits, _ := cache.GetStats()
	if hits == 0 {
		t.Error("expected hits > 0")
	}
}

// TestNewTagIndex tests tag index creation.
func TestNewTagIndex(t *testing.T) {
	index := fileutil.NewTagIndex()
	if index == nil {
		t.Fatal("NewTagIndex returned nil")
	}

	if index.Count() != 0 {
		t.Error("expected empty index")
	}
}

// TestTagIndexAddGet tests adding and getting positions.
func TestTagIndexAddGet(t *testing.T) {
	index := fileutil.NewTagIndex()

	pos := &filebase.Position{
		Tag:    uint32(tag.New(0x0010, 0x0010)),
		Offset: 100,
		Length: 8,
	}

	index.Add(pos)

	retrieved, exists := index.Get(tag.New(0x0010, 0x0010))
	if !exists {
		t.Error("expected position to exist")
	}

	if retrieved.Offset != 100 {
		t.Errorf("expected offset 100, got %d", retrieved.Offset)
	}
}

// TestTagIndexContains tests checking tag existence.
func TestTagIndexContains(t *testing.T) {
	index := fileutil.NewTagIndex()

	pos := &filebase.Position{
		Tag:    uint32(tag.New(0x0010, 0x0010)),
		Offset: 100,
		Length: 8,
	}

	index.Add(pos)

	if !index.Contains(tag.New(0x0010, 0x0010)) {
		t.Error("expected tag to exist in index")
	}
}

// TestTagIndexClear tests clearing the index.
func TestTagIndexClear(t *testing.T) {
	index := fileutil.NewTagIndex()

	pos := &filebase.Position{
		Tag:    uint32(tag.New(0x0010, 0x0010)),
		Offset: 100,
		Length: 8,
	}

	index.Add(pos)
	index.Clear()

	if index.Count() != 0 {
		t.Error("expected empty index after clear")
	}
}

// TestNewReaderWithBoundary tests creating a bounded reader.
func TestNewReaderWithBoundary(t *testing.T) {
	buf := bytes.NewBuffer(make([]byte, 1000))
	reader := filebase.NewFileReader(&readWriteSeeker{Buffer: buf})
	bounded := fileutil.NewReaderWithBoundary(reader, 500)

	if bounded == nil {
		t.Fatal("NewReaderWithBoundary returned nil")
	}

	if bounded.GetBytesRemaining() != 500 {
		t.Errorf("expected 500 bytes remaining, got %d", bounded.GetBytesRemaining())
	}
}

// readWriteSeeker is a test helper for mock IO operations.
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

// TestExecuteTagIndexHook tests tag index hook execution.
func TestExecuteTagIndexHook(t *testing.T) {
	index := fileutil.NewTagIndex()

	pos := &filebase.Position{
		Tag:    uint32(tag.New(0x0010, 0x0010)),
		Offset: 100,
		Length: 8,
	}

	index.Add(pos)

	result, err := fileutil.ExecuteTagIndexHook(index)
	if err != nil {
		t.Fatalf("ExecuteTagIndexHook error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	if count, ok := result["count"].(int); !ok || count != 1 {
		t.Error("expected count=1 in result")
	}
}

// TestExecuteByteOrderHook tests byte order hook execution.
func TestExecuteByteOrderHook(t *testing.T) {
	u := uid.New("1.2.840.10008.1.2.1")

	result, err := fileutil.ExecuteByteOrderHook(u)
	if err != nil {
		t.Fatalf("ExecuteByteOrderHook error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	if isLE, ok := result["is_little_endian"].(bool); !ok || !isLE {
		t.Error("expected is_little_endian=true in result")
	}
}

// TestByteOrderHookWithBigEndian tests byte order hook with big-endian.
func TestByteOrderHookWithBigEndian(t *testing.T) {
	u := uid.New("1.2.840.10008.1.2.2")

	result, err := fileutil.ExecuteByteOrderHook(u)
	if err != nil {
		t.Fatalf("ExecuteByteOrderHook error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	if isLE, ok := result["is_little_endian"].(bool); !ok || isLE {
		t.Error("expected is_little_endian=false in result")
	}
}
