package dicomweb

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/encaps"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// The frames of an instance (PS3.18 10.4.1.1.4, "Frame Pixel Data"):
//
//	GET /studies/{study}/series/{series}/instances/{instance}/frames/{list}
//
// which is how a web viewer loads an image. A frame is read from the file
// by where it lies, so one frame of a long multi-frame image costs that
// frame and the attributes, not the instance: the file is read with its bulk
// data left in place, filereader.DICOMFile.FrameRanges says which bytes hold
// each frame, and those are all that is read of the pixel data.

// mediaOctetStream is uncompressed pixel data (PS3.18 8.7.3.3.1). Its one
// transfer syntax for frames is Explicit VR Little Endian.
const mediaOctetStream = "application/octet-stream"

// frameMedia is the media type of a compressed frame in each transfer
// syntax (PS3.18 8.7.3.3.2). A syntax missing here is not served as a frame
// as stored: a part needs a type a client can read it by.
var frameMedia = map[string]string{
	compress.JPEGBaseline:            "image/jpeg",
	compress.JPEGExtended:            "image/jpeg",
	compress.JPEGLosslessNonHierarch: "image/jpeg",
	compress.JPEGLossless:            "image/jpeg",
	compress.JPEGLSLossless:          "image/jls",
	compress.JPEGLSNearLossless:      "image/jls",
	compress.JPEG2000Lossless:        "image/jp2",
	compress.JPEG2000Lossy:           "image/jp2",
	"1.2.840.10008.1.2.4.92":         "image/jpx",
	"1.2.840.10008.1.2.4.93":         "image/jpx",
	compress.RLELossless:             mediaRLE,
}

// mediaRLE is RLE Lossless as IANA registered it. The standard's text has
// also called it image/x-dicom-rle, and a request may still say that.
const mediaRLE = "image/dicom-rle"

// frameWant is one form a request accepts frames in.
type frameWant struct {
	// asStored takes each frame as the archive holds it.
	asStored bool
	// media is a compressed type the stored syntax has to be one of, and
	// syntax, when it is not empty, the one syntax of it that will do. With
	// neither this nor asStored, the frames are wanted uncompressed.
	media, syntax string
}

// frameWantsOf reads what a request for frames accepts, in the order it
// lists them (PS3.18 8.7.3.3).
//
// A request that says nothing, accepts anything, or asks for
// application/octet-stream is given the standard's default: uncompressed,
// Explicit VR Little Endian. This differs from an instance, which is sent
// as stored when nothing is said. A frame carries no header to say how its
// bytes are coded, and the standard names the default for it.
//
// application/octet-stream with transfer-syntax=* is, in the standard, any
// uncompressed syntax. It is what viewers send to mean "as stored, whatever
// it is", and Orthanc and dcm4chee answer it so, each part typed by its
// syntax; so it is answered so here.
func frameWantsOf(r *http.Request) ([]frameWant, string) {
	accept := strings.Join(r.Header.Values("Accept"), ",")
	if parameter := r.URL.Query().Get("accept"); parameter != "" {
		accept = parameter
	}
	if strings.TrimSpace(accept) == "" {
		return []frameWant{{}}, accept
	}
	var wants []frameWant
	for _, entry := range strings.Split(accept, ",") {
		media, parameters, err := mime.ParseMediaType(entry)
		if err != nil {
			continue
		}
		if q, err := strconv.ParseFloat(parameters["q"], 64); err == nil && q == 0 {
			continue
		}
		syntax := parameters["transfer-syntax"]
		if media == "multipart/related" {
			media = strings.ToLower(parameters["type"])
		} else if media != "*/*" && media != "multipart/*" {
			// A frame is sent in a multipart body whatever its type
			// (PS3.18 8.7.3.3); a type on its own is not something this
			// resource answers.
			continue
		}
		switch media {
		case "", "*/*", "multipart/*":
			wants = append(wants, frameWant{})
		case mediaOctetStream:
			switch syntax {
			case "*":
				wants = append(wants, frameWant{asStored: true})
			case "", explicitVRLittleEndian:
				wants = append(wants, frameWant{})
			}
		case "image/x-dicom-rle":
			wants = append(wants, frameWant{media: mediaRLE, syntax: strings.TrimPrefix(syntax, "*")})
		default:
			wants = append(wants, frameWant{media: media, syntax: strings.TrimPrefix(syntax, "*")})
		}
	}
	return wants, accept
}

// frameList reads a list of frame numbers: counted from 1, separated by
// commas, in the order they are to be sent.
func frameList(list string) ([]int, *requestError) {
	var frames []int
	for _, item := range strings.Split(list, ",") {
		n, err := strconv.Atoi(item)
		if err != nil || n < 1 || strings.HasPrefix(item, "+") {
			return nil, refuse(http.StatusBadRequest,
				"%q is not a list of frame numbers counted from 1, separated by commas", list)
		}
		frames = append(frames, n)
	}
	return frames, nil
}

// readerAt reads at an offset from a file opened for reading in order, for
// a backend whose files are not io.ReaderAt. It is not safe for concurrent
// use, and a request is served on one goroutine.
type readerAt struct{ file io.ReadSeeker }

func (r readerAt) ReadAt(p []byte, offset int64) (int, error) {
	if _, err := r.file.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(r.file, p)
}

// shiftedReaderAt reads a value held in memory as though it were where it
// lies in the data set, so ranges counted in the data set can be read from
// it.
type shiftedReaderAt struct {
	value []byte
	base  int64
}

func (r shiftedReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	at := offset - r.base
	if at < 0 || at > int64(len(r.value)) {
		return 0, io.EOF
	}
	n := copy(p, r.value[at:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// frameSource is an instance opened for its frames: where each frame lies,
// what to read it from, and the attributes that say how.
type frameSource struct {
	ranges [][]encaps.Range
	src    io.ReaderAt
	// attributes are the data set without its bulk data, which knows its
	// transfer syntax.
	attributes *dataset.Dataset
	syntax     string
	// swap is the width to reverse native pixel data in, read from a big
	// endian file, and value the bytes it is reversed within; swap is 0
	// when nothing is to be reversed.
	swap  int
	value encaps.Range
	// float is set when the pixels are Float or Double Float Pixel Data,
	// which are sent as frames and not drawn.
	float bool
	// packed is the length in bits of a frame of a 1-bit image whose frames
	// start inside a byte, and firstBit the bit each begins at in its first
	// byte; packed is 0 for every other image.
	packed   int64
	firstBit []int
}

// tagPixelData is Pixel Data (7FE0,0010). Float Pixel Data (7FE0,0008) and
// Double Float Pixel Data (7FE0,0009), which an image holds instead of it
// when its values are not integers, have frames as well, found by
// filereader.DICOMFile.PixelDataElement.
var tagPixelData = tag.New(0x7FE0, 0x0010)

// errNoFrames is an instance with no Pixel Data, which has no frames.
var errNoFrames = refuse(http.StatusNotFound, "the instance has no Pixel Data, so no frames")

// openFrames reads an instance's structure and where its frames lie.
func openFrames(file io.ReadSeeker) (*frameSource, error) {
	parsed, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(file),
		filereader.ReadOptions{SkipBulkData: true})
	if err != nil {
		return nil, err
	}
	pixels := parsed.PixelDataElement()
	if pixels == nil {
		return nil, errNoFrames
	}
	source := &frameSource{attributes: parsed.GetDataset(), float: pixels.Tag != tagPixelData}
	source.syntax = source.attributes.TransferSyntaxUID()
	if at, ok := file.(io.ReaderAt); ok {
		source.src = at
	} else {
		source.src = readerAt{file}
	}

	if parsed.Deflated {
		// The positions count in the inflated data set, which is not on
		// disk. It is all inflated to be read at all, so it is read whole
		// and the frame taken from the value.
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if parsed, err = filereader.ReadDICOMFile(filebase.NewFileReader(file)); err != nil {
			return nil, err
		}
		if pixels = parsed.PixelDataElement(); pixels == nil {
			return nil, errNoFrames
		}
		source.src = shiftedReaderAt{value: pixels.Value, base: pixels.Position.ValueOffset}
	}

	info, err := source.attributes.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	if !compress.IsEncapsulated(source.syntax) && !parsed.IsLittleEndian && !parsed.Deflated {
		// At the width the reader reverses a whole value at (#124), and from
		// the value's first byte: an 8-bit image in OW is reversed in pairs,
		// and a frame of odd length shares its last pair with the next.
		// Float and Double Float Pixel Data are OF and OD, reversed in fours
		// and eights.
		source.swap = dataelem.PixelDataSwapWidth(dataelem.VR(pixels.VR), info.BitsAllocated)
		source.value = encaps.Range{Offset: pixels.Position.ValueOffset, Length: int64(pixels.Length)}
	}
	source.ranges, err = parsed.FrameRanges(source.src)
	if err != nil {
		return nil, err
	}
	if bits := int64(info.Rows) * int64(info.Columns) * int64(info.SamplesPerPixel); info.BitsAllocated == 1 &&
		!pixels.UndefinedLength && bits%8 != 0 && len(source.ranges) > 1 {
		source.packed = bits
		source.firstBit = make([]int, len(source.ranges))
		for i := range source.firstBit {
			if source.firstBit[i], err = parsed.FrameFirstBit(i); err != nil {
				return nil, err
			}
		}
	}
	return source, nil
}

// read returns a frame, counted from 0, as the file holds it, and native
// samples little endian. A frame of a 1-bit image that starts inside a byte
// is moved to start at the first bit of its first byte, the bits after it
// cleared: sent on its own it has no position to start from, and that is
// how the Pixel Data of a 1-bit image of one frame is laid out (PS3.5
// 8.1.1, D.1), so it is read the same way.
func (f *frameSource) read(frame int) ([]byte, error) {
	var out []byte
	for _, r := range f.ranges[frame] {
		// Reversed in whole units counted from the value's start, so the
		// range is widened to them and cut back after.
		start, end := r.Offset, r.Offset+r.Length
		if f.swap > 1 {
			unit := int64(f.swap)
			start -= (start - f.value.Offset) % unit
			end += (unit - (end-f.value.Offset)%unit) % unit
			end = min(end, f.value.Offset+f.value.Length)
		}
		chunk := make([]byte, end-start)
		if _, err := f.src.ReadAt(chunk, start); err != nil {
			return nil, fmt.Errorf("reading frame %d: %w", frame+1, err)
		}
		if f.swap > 1 {
			dataelem.SwapBytes(chunk, f.swap)
		}
		out = append(out, chunk[r.Offset-start:r.Offset-start+r.Length]...)
	}
	if f.packed > 0 {
		out = shiftBits(out, f.firstBit[frame], f.packed)
	}
	return out, nil
}

// shiftBits returns the bits bits of data that start at bit first of its
// first byte, counted from the least significant bit, starting at bit 0
// and followed by zero bits to the end of the last byte.
func shiftBits(data []byte, first int, bits int64) []byte {
	out := make([]byte, (bits+7)/8)
	for i := range out {
		at := i*8 + first
		value := int(data[at/8]) >> (at % 8)
		if at%8 != 0 && at/8+1 < len(data) {
			value |= int(data[at/8+1]) << (8 - at%8)
		}
		out[i] = byte(value)
	}
	if rest := bits % 8; rest != 0 {
		out[len(out)-1] &= byte(1<<rest - 1)
	}
	return out
}

// choose picks the first form the request accepts that this instance can
// be sent in: its media type and transfer syntax, and whether the frames
// are to be decoded for it. It reports false when there is none.
func (f *frameSource) choose(wants []frameWant) (media, syntax string, decode, ok bool) {
	encapsulated := compress.IsEncapsulated(f.syntax)
	stored, known := frameMedia[f.syntax]
	if !encapsulated {
		stored, known = mediaOctetStream, true
	}
	_, decodable := compress.TransferSyntaxToCompressionType(f.syntax)
	for _, want := range wants {
		switch {
		case want.asStored && known && !encapsulated:
			return mediaOctetStream, explicitVRLittleEndian, false, true
		case want.asStored && known:
			return stored, f.syntax, false, true
		case want.media != "" && encapsulated && want.media == stored && (want.syntax == "" || want.syntax == f.syntax):
			return stored, f.syntax, false, true
		case want.media == "" && !want.asStored && (!encapsulated || decodable == nil):
			return mediaOctetStream, explicitVRLittleEndian, encapsulated, true
		}
	}
	return "", "", false, false
}

// frames answers a request for frames of an instance.
func (s *Server) frames(w http.ResponseWriter, r *http.Request, target retrieveTarget) {
	numbers, refused := frameList(target.frames)
	if refused != nil {
		s.fail(w, refused)
		return
	}
	file, err := s.openInstance(r, target)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer func() { _ = file.Close() }()

	source, err := openFrames(file)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, n := range numbers {
		// A frame the image does not have is a resource that does not
		// exist (PS3.18 10.4.3, Table 10.4.3-1).
		if n > len(source.ranges) {
			s.fail(w, refuse(http.StatusNotFound, "frame %d does not exist; the instance has %d", n, len(source.ranges)))
			return
		}
	}
	s.sendFrames(w, r, source, numbers, false)
}

// openInstance opens the file of the one instance a target names.
func (s *Server) openInstance(r *http.Request, target retrieveTarget) (io.ReadSeekCloser, error) {
	uids, err := s.backend.Locate(r.Context(), target.study, target.series, target.instance)
	if err != nil {
		return nil, err
	}
	if len(uids) == 0 {
		return nil, refuse(http.StatusNotFound, "the archive holds no such instance")
	}
	return s.backend.Open(r.Context(), uids[0])
}

// sendFrames writes frames, counted from 1, in the form the request
// accepts: one part each, or, when joined is set and they are sent
// uncompressed, one part holding them all, which is how Pixel Data is sent
// as bulk data.
func (s *Server) sendFrames(w http.ResponseWriter, r *http.Request, source *frameSource, numbers []int, joined bool) {
	wants, accept := frameWantsOf(r)
	media, syntax, decode, ok := source.choose(wants)
	if !ok {
		s.fail(w, refuse(http.StatusNotAcceptable,
			"the frames are held in transfer syntax %s, and nothing is encoded on request; the request accepts %q",
			source.syntax, accept))
		return
	}
	joined = joined && media == mediaOctetStream

	// The first frame is made ready before anything is written, so a frame
	// that cannot be read or decoded can still be answered with a status.
	frame := func(n int) ([]byte, error) {
		data, err := source.read(n - 1)
		if err != nil || !decode {
			return data, err
		}
		return source.attributes.DecodeFrame(data)
	}
	first, err := frame(numbers[0])
	if err != nil {
		s.fail(w, err)
		return
	}

	body := multipart.NewWriter(w)
	w.Header().Set("Content-Type", `multipart/related; type="`+media+`"; boundary=`+body.Boundary())
	w.WriteHeader(http.StatusOK)
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", media+"; transfer-syntax="+syntax)
	var out io.Writer
	for i, n := range numbers {
		data := first
		if i > 0 {
			if data, err = frame(n); err != nil {
				// The status has gone. Cutting the connection is the one
				// way left to say the body is not whole.
				panic(http.ErrAbortHandler)
			}
		}
		if out == nil || !joined {
			if out, err = body.CreatePart(header); err != nil {
				panic(http.ErrAbortHandler)
			}
		}
		if _, err := io.Copy(out, bytes.NewReader(data)); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if err := body.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}
