package inflate

import (
	"bytes"
	"compress/flate"
	"io"
	"math/rand"
	"testing"
)

// deflated returns n bytes of data that neither repeat nor are random enough
// to be incompressible, and the raw DEFLATE stream of them.
func deflated(t *testing.T, n int) (data, stream []byte) {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	data = make([]byte, n)
	for i := range data {
		data[i] = byte(rng.Intn(16)) + byte(i/4096)
	}
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data, buf.Bytes()
}

// A Stream gives the inflated bytes at any position, forward, a little
// backward, and far backward (which inflates again from the start), and the
// length it measured.
func TestStreamSeeksForwardAndBack(t *testing.T) {
	data, stream := deflated(t, 3<<20)
	s, err := Open(bytes.NewReader(stream), int64(len(stream)), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if end, err := s.Seek(0, io.SeekEnd); err != nil || end != int64(len(data)) {
		t.Fatalf("length = %d, %v; want %d", end, err, len(data))
	}

	// Positions in the order a parser might ask: ahead, a few bytes back, far
	// ahead, far back (past the window), the last bytes.
	for _, at := range []int64{0, 100, 90, 1 << 20, (1 << 20) - 8, 2 << 20, 5, int64(len(data)) - 10} {
		if _, err := s.Seek(at, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, 64)
		n, err := io.ReadFull(s, got)
		want := data[at:min(at+64, int64(len(data)))]
		if err != nil && err != io.ErrUnexpectedEOF || !bytes.Equal(got[:n], want) {
			t.Fatalf("at %d: read %d bytes, err %v, wrong bytes", at, n, err)
		}
	}
	if _, err := s.Seek(int64(len(data)), io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Read(make([]byte, 8)); n != 0 || err != io.EOF {
		t.Errorf("read at the end = %d, %v; want 0, EOF", n, err)
	}
}

// A Stream read from its start to its end holds only a window of it, and
// gives the whole: what makes a 200 MB value cost no more memory than a small
// one.
func TestStreamHoldsOnlyAWindow(t *testing.T) {
	data, stream := deflated(t, 4<<20)
	s, err := Open(bytes.NewReader(stream), int64(len(stream)), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	buf := make([]byte, 100<<10)
	for {
		n, err := s.Read(buf)
		out.Write(buf[:n])
		if len(s.win) > 2*window {
			t.Fatalf("the stream holds %d bytes behind it, more than %d", len(s.win), 2*window)
		}
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Error("the bytes read are not the data")
	}
}

// A stream that inflates to more than its size allows is refused when it is
// opened, with the words the parser gave, and one that is not a stream is an
// error and not an empty data set.
func TestOpenRefusesABombAndGarbage(t *testing.T) {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = w.Write(make([]byte, 64<<20)) // 64 MiB of zeros in a few dozen KiB
	_ = w.Close()
	if _, err := Open(bytes.NewReader(buf.Bytes()), int64(buf.Len()), 16<<20); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("expands beyond the")) {
		t.Errorf("a bomb: err = %v", err)
	}
	if _, err := Open(bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}), 5, 1<<20); err == nil {
		t.Error("garbage was opened")
	}
}

// A Source serves ranges in increasing order by inflating the stream once, and
// one behind the position by inflating again, each the same bytes.
func TestSourceReadsRangesInAnyOrder(t *testing.T) {
	data, stream := deflated(t, 3<<20)
	src := NewSource(OpenUnmeasured(bytes.NewReader(stream), 0, 1<<30))
	for _, r := range [][2]int64{{0, 1000}, {1000, 500000}, {1 << 21, 1 << 18}, {10, 20}, {int64(len(data)) - 5, 5}} {
		got := make([]byte, r[1])
		if n, err := src.ReadAt(got, r[0]); err != nil || n != len(got) || !bytes.Equal(got, data[r[0]:r[0]+r[1]]) {
			t.Fatalf("range %v: %d bytes, %v", r, n, err)
		}
	}
	// Past the end: a short read and an error, as io.ReaderAt says.
	got := make([]byte, 10)
	if n, err := src.ReadAt(got, int64(len(data))-4); n != 4 || err == nil {
		t.Errorf("a range past the end: %d bytes, %v", n, err)
	}
}

// One Read of a long buffer fills it, as a file does, where an inflater gives
// a block: the file parser takes a read that returns fewer bytes for a file
// that ends early.
func TestStreamReadFillsTheBuffer(t *testing.T) {
	data, stream := deflated(t, 1<<20)
	s, err := Open(bytes.NewReader(stream), int64(len(stream)), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 600<<10)
	if n, err := s.Read(got); err != nil || n != len(got) || !bytes.Equal(got, data[:len(got)]) {
		t.Fatalf("Read = %d, %v; want %d bytes", n, err, len(got))
	}
	rest := make([]byte, 600<<10)
	if n, err := s.Read(rest); err != nil || n != len(data)-len(got) {
		t.Errorf("Read at the end = %d, %v; want %d bytes", n, err, len(data)-len(got))
	}
}
