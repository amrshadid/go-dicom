package network

import (
	"bytes"
	"compress/flate"
	"strings"
	"testing"
)

// deflateBomb builds a small deflate stream that expands to size bytes.
// Highly repetitive input compresses to a tiny fraction of its output, which
// is exactly what a decompression bomb exploits.
func deflateBomb(t *testing.T, size int) []byte {
	t.Helper()

	var out bytes.Buffer
	w, err := flate.NewWriter(&out, flate.BestCompression)
	if err != nil {
		t.Fatalf("flate.NewWriter: %v", err)
	}

	// Write in chunks so the test does not hold the whole payload in memory.
	chunk := bytes.Repeat([]byte{0}, 1<<20)
	for written := 0; written < size; written += len(chunk) {
		n := len(chunk)
		if remaining := size - written; remaining < n {
			n = remaining
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	return out.Bytes()
}

// TestInflateRejectsDecompressionBomb verifies that a deflated data set which
// expands past the limit is refused rather than allocated.
//
// The Deflated Explicit VR Little Endian transfer syntax is negotiable by any
// peer, so this path is reachable before authentication: a few kilobytes on the
// wire could otherwise expand without bound.
func TestInflateRejectsDecompressionBomb(t *testing.T) {
	// Just over the limit, so the guard must trip.
	bomb := deflateBomb(t, int(MaxInflatedDatasetSize)+(1<<20))

	t.Logf("bomb: %d compressed bytes expanding to over %d",
		len(bomb), MaxInflatedDatasetSize)
	if int64(len(bomb)) > MaxInflatedDatasetSize/100 {
		t.Fatalf("fixture is not compressed enough to be a meaningful test")
	}

	_, err := inflateBytes(bomb)
	if err == nil {
		t.Fatal("expected an error for a data set expanding past the limit, got nil")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error %q does not mention the limit", err)
	}
}

// TestInflateAcceptsNormalData verifies the guard rejects only what is over the
// limit, not ordinary deflated data.
func TestInflateAcceptsNormalData(t *testing.T) {
	original := bytes.Repeat([]byte("DICOM data set content. "), 4096)

	var compressed bytes.Buffer
	w, _ := flate.NewWriter(&compressed, flate.DefaultCompression)
	if _, err := w.Write(original); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got, err := inflateBytes(compressed.Bytes())
	if err != nil {
		t.Fatalf("inflateBytes on ordinary data: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("round trip altered the data (%d bytes in, %d out)", len(original), len(got))
	}
}

// TestDecodeDatasetRejectsBombOverTheWire verifies the guard applies through
// the exported decoder, which is the path a peer actually reaches.
func TestDecodeDatasetRejectsBombOverTheWire(t *testing.T) {
	bomb := deflateBomb(t, int(MaxInflatedDatasetSize)+(1<<20))

	_, err := DecodeDataset(bomb, DeflatedExplicitVRLittleEndianUID)
	if err == nil {
		t.Fatal("expected DecodeDataset to reject a decompression bomb, got nil")
	}
}
