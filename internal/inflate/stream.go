package inflate

import (
	"bufio"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"sync"
)

// window is how many of the bytes just read a Stream keeps, so that a short
// step backwards (the parser peeks a tag and returns to it) is served from
// memory and not by inflating the stream again from its start.
const window = 256 << 10

// Stream is the inflated bytes of a raw DEFLATE stream (RFC 1951) as an
// io.ReadSeeker, held without the whole of them in memory.
//
// It is how a Deflated Explicit VR Little Endian data set (PS3.5 A.5) is read
// and its values are copied from, when the data set can be larger than one
// wants to hold: a 200 MB value in a deflated file would otherwise be 200 MB of
// memory to find out where it ends.
//
// Reading goes forward from where the stream is. A Seek ahead is made by
// inflating and dropping what lies between; one behind, inside the last
// window, is served from the bytes kept; one further behind restarts the
// stream from its beginning in the source and inflates up to the place. A
// reader that goes through the data set once in order inflates it once.
// Memory is the window, the buffer in front of the source and the inflater's
// own 32 KiB, whatever the size of the data set.
//
// A Stream is not safe for concurrent use; Source is.
type Stream struct {
	src   io.ReadSeeker
	start int64 // where the deflate stream begins in src
	end   int64 // the inflated length, or the most it may have when not measured
	// measured reports that end is the length the stream was found to have.
	measured bool

	br  *bufio.Reader
	zr  io.ReadCloser
	pos int64  // inflated bytes consumed from zr
	win []byte // the bytes before pos, the last of them, ending at pos
	cur int64  // where the next Read is from
}

// Open measures the deflated stream src holds from its current position to
// its end, by inflating it once and counting, and returns a Stream over its
// inflated bytes. compressedSize is how many bytes of src it may take, or not
// positive when unknown, and absoluteMax the most it may inflate to; the bound
// is LimitFor's. A stream that expands past it, or does not inflate, is an
// error, with the same text whole-reading it gave.
func Open(src io.ReadSeeker, compressedSize, absoluteMax int64) (*Stream, error) {
	start, err := src.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	s := newStream(src, start)
	limit := LimitFor(compressedSize, absoluteMax)
	if err := s.restart(); err != nil {
		return nil, err
	}
	// One byte past the limit: if it materializes the rest is not worth
	// inflating.
	n, err := io.Copy(io.Discard, io.LimitReader(s.zr, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, fmt.Errorf("data set of %d compressed bytes expands beyond the %d byte limit allowed for its size",
			compressedSize, limit)
	}
	s.end, s.measured = n, true
	s.zr.Close()
	s.zr = nil // the first read starts the stream again
	return s, nil
}

// OpenUnmeasured returns a Stream over the deflated stream src holds from
// offset start, without inflating it to measure it: for a stream already
// known to be within bounds, which is how a value source is made for a file
// that has been read. Reads stop at absoluteMax bytes, or at the end of the
// stream.
func OpenUnmeasured(src io.ReadSeeker, start, absoluteMax int64) *Stream {
	s := newStream(src, start)
	s.end = absoluteMax
	return s
}

func newStream(src io.ReadSeeker, start int64) *Stream {
	return &Stream{src: src, start: start, br: bufio.NewReaderSize(src, 64<<10)}
}

// restart begins the stream again from its first byte.
func (s *Stream) restart() error {
	if _, err := s.src.Seek(s.start, io.SeekStart); err != nil {
		return err
	}
	s.br.Reset(s.src)
	if s.zr == nil {
		s.zr = flate.NewReader(s.br)
	} else if err := s.zr.(flate.Resetter).Reset(s.br, nil); err != nil {
		return err
	}
	s.pos, s.win = 0, s.win[:0]
	return nil
}

// remember keeps the last window bytes read.
func (s *Stream) remember(b []byte) {
	s.pos += int64(len(b))
	if len(b) >= window {
		s.win = append(s.win[:0], b[len(b)-window:]...)
		return
	}
	s.win = append(s.win, b...)
	if len(s.win) > 2*window {
		s.win = append(s.win[:0], s.win[len(s.win)-window:]...)
	}
}

// Read fills p from where the stream is, as a file does: it returns fewer
// bytes only at the end of the data. The file parser reads a value with one
// call and takes a short read for a truncated file, and an inflater, left
// alone, returns what one block holds.
func (s *Stream) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		n, err := s.readSome(p[total:])
		total += n
		if err != nil {
			if err == io.EOF && total > 0 {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}

// readSome reads inflated bytes from where the stream is: at least one, unless
// there are none left.
func (s *Stream) readSome(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if s.cur >= s.end {
		return 0, io.EOF
	}
	if rest := s.end - s.cur; int64(len(p)) > rest {
		p = p[:rest]
	}

	if s.cur < s.pos {
		if base := s.pos - int64(len(s.win)); s.cur >= base {
			n := copy(p, s.win[s.cur-base:])
			s.cur += int64(n)
			return n, nil
		}
		if err := s.restart(); err != nil {
			return 0, err
		}
	}
	if s.zr == nil {
		if err := s.restart(); err != nil {
			return 0, err
		}
	}
	if s.cur > s.pos {
		if err := s.discard(s.cur - s.pos); err != nil {
			return 0, err
		}
	}

	n, err := s.zr.Read(p)
	s.remember(p[:n])
	s.cur += int64(n)
	if err == io.EOF {
		if n > 0 {
			return n, nil
		}
		if s.measured && s.cur < s.end {
			return 0, io.ErrUnexpectedEOF
		}
		s.end = s.cur // not measured: this is where it ends
	}
	return n, err
}

// discard inflates and drops n bytes.
func (s *Stream) discard(n int64) error {
	buf := make([]byte, min(n, 64<<10))
	for n > 0 {
		chunk := buf[:min(n, int64(len(buf)))]
		got, err := io.ReadFull(s.zr, chunk)
		s.remember(chunk[:got])
		n -= int64(got)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				// Past the end of the stream: there is nothing there to read.
				s.end = min(s.end, s.pos)
				return io.EOF
			}
			return err
		}
	}
	return nil
}

// Seek sets where the next Read is from. It moves nothing until the next
// Read; io.SeekEnd counts from the inflated length, which is known only to a
// stream that Open measured.
func (s *Stream) Seek(offset int64, whence int) (int64, error) {
	var at int64
	switch whence {
	case io.SeekStart:
		at = offset
	case io.SeekCurrent:
		at = s.cur + offset
	case io.SeekEnd:
		if !s.measured {
			return 0, errors.New("inflate: the end of a stream that was not measured is not known")
		}
		at = s.end + offset
	default:
		return 0, errors.New("inflate: invalid whence")
	}
	if at < 0 {
		return 0, errors.New("inflate: seek before the start of the stream")
	}
	s.cur = at
	return at, nil
}

// Close releases the inflater. It does not close the source.
func (s *Stream) Close() error {
	if s.zr == nil {
		return nil
	}
	err := s.zr.Close()
	s.zr = nil
	return err
}

// Source serves ranges of the inflated bytes as an io.ReaderAt, from a
// Stream: what filewriter's SetValueSource takes. Ranges asked for in
// increasing order, as a writer copying the values of a data set does, inflate
// the stream once; one that lies behind the current position restarts it.
// It is safe for concurrent use, one call at a time.
type Source struct {
	mu sync.Mutex
	s  *Stream
}

// NewSource makes a Source of s.
func NewSource(s *Stream) *Source { return &Source{s: s} }

// ReadAt reads len(p) inflated bytes from offset off.
func (r *Source) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.s.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	n, err := io.ReadFull(r.s, p)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}
