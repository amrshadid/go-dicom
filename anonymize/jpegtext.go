package anonymize

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/encaps"
	"github.com/amrshadid/go-dicom/tag"
)

// Text can hide in a JPEG stream without being in a pixel. A comment segment
// (COM, 0xFFFE) holds free text by definition, and the application segments
// (APP0 to APP15, 0xFFE0 to 0xFFEF) hold whatever the program that wrote them
// put there: an Exif block with a device serial number and a date, an XMP
// packet, a thumbnail, a vendor's note of the patient the image was taken
// of. A decoder reads none of it, and a de-identifier that looks at
// attributes and pixels does not either, so it travels in the file. The
// segments are inside the fragments of encapsulated Pixel Data, which every
// frame of the JPEG families keeps: Baseline, Extended, Lossless, and JPEG-LS,
// whose marker syntax is the same (ISO/IEC 14495-1 Annex C).

// ErrJPEGSegmentsNotSearched is wrapped by the error JPEGSegmentText and
// RemoveJPEGText return for pixel data whose segments they do not search: a
// transfer syntax that is encapsulated and is none of the JPEG families, such
// as JPEG 2000, whose own comment segments are not read here. It is an error
// and not an empty result because an empty result would say there is no text.
var ErrJPEGSegmentsNotSearched = errors.New("anonymize: the segments of this pixel data are not searched")

// JPEGSegment is a comment or application segment of one frame of JPEG data.
type JPEGSegment struct {
	// Frame is the frame the segment is in, counted from 0.
	Frame int
	// Marker is the marker's second byte: 0xFE for COM, 0xE0 to 0xEF for APP0
	// to APP15, and 0 for the bytes after the end of the image, which the
	// decoder does not read either.
	Marker byte
	// Offset is where the segment begins in the frame, at the first byte of
	// the marker.
	Offset int
	// Data is a copy of what the segment holds, without the marker and the
	// length.
	Data []byte
	// Text is what in Data reads as text: the whole of a comment, with what
	// is not printable replaced by a dot and trailing padding dropped, and
	// for any other segment the runs of four or more printable characters,
	// joined by a space. It is empty when there is none.
	Text string
	// Kept is set by RemoveJPEGText on the segments it left in the frame
	// because a decoder needs them; see there.
	Kept bool
}

// Name is the segment's marker name: COM, APP0 to APP15, or "trailing data".
func (s JPEGSegment) Name() string {
	switch {
	case s.Marker == 0xFE:
		return "COM"
	case s.Marker >= 0xE0 && s.Marker <= 0xEF:
		return fmt.Sprintf("APP%d", s.Marker-0xE0)
	}
	return "trailing data"
}

// jpegSyntax says whether a transfer syntax is one of the JPEG families whose
// frames are searched.
func jpegSyntax(ts string) bool {
	switch ts {
	case compress.JPEGBaseline, compress.JPEGExtended,
		compress.JPEGLosslessNonHierarch, compress.JPEGLossless,
		compress.JPEGLSLossless, compress.JPEGLSNearLossless:
		return true
	}
	return false
}

// jpegFrames returns the compressed frames of ds, or nil when its pixel data
// is not one the segments are searched in and that is not a reason to refuse.
func jpegFrames(ds *dataset.Dataset) ([][]byte, error) {
	elem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		return nil, nil
	}
	ts := ds.TransferSyntaxUID()
	if !jpegSyntax(ts) {
		// RLE segments are packed samples and hold no text; native pixel data
		// has no segments. Anything else that is encapsulated has some this
		// does not read.
		if ts == "" || !compress.IsEncapsulated(ts) || ts == compress.RLELossless {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: transfer syntax %s", ErrJPEGSegmentsNotSearched, ts)
	}
	raw, ok := elem.Bytes()
	if !ok {
		return nil, fmt.Errorf("anonymize: Pixel Data holds no bytes to search")
	}
	encData, err := encaps.NewParser(bytes.NewReader(raw), true).ParseEncapsulatedData()
	if err != nil {
		return nil, fmt.Errorf("anonymize: Pixel Data is not encapsulated as %s says: %w", ts, err)
	}
	frames := 0
	if info, err := ds.GetPixelDataInfo(); err == nil {
		frames = info.NumberOfFrames
		encData.NumberOfFrames = frames
	}
	return encaps.Frames(encData, frames)
}

// JPEGSegmentText reads the comment and application segments of every frame
// of encapsulated JPEG Pixel Data (Baseline, Extended, Lossless and JPEG-LS)
// and returns them in frame and file order, with the text they hold, so that a
// caller can show what travels in an image the profiles do not look at. Bytes
// after a frame's end-of-image marker, other than the pad byte that makes the
// fragment even, are returned too, as a segment named "trailing data".
//
// A data set with no Pixel Data, native pixel data or RLE has no such
// segments and returns none. A syntax this does not search, such as JPEG 2000,
// returns an error wrapping [ErrJPEGSegmentsNotSearched]. A frame that is not
// well formed JPEG returns an error naming it. The data set is not changed.
func JPEGSegmentText(ds *dataset.Dataset) ([]JPEGSegment, error) {
	if ds == nil {
		return nil, fmt.Errorf("dataset is nil")
	}
	frames, err := jpegFrames(ds)
	if err != nil {
		return nil, err
	}
	var out []JPEGSegment
	for i, frame := range frames {
		parsed, err := scanJPEG(frame)
		if err != nil {
			return nil, fmt.Errorf("anonymize: frame %d: %w", i, err)
		}
		out = append(out, parsed.interesting(frame, i)...)
	}
	return out, nil
}

// RemoveJPEGText removes the comment segments and the application segments
// that a decoder does not need from every frame of encapsulated JPEG Pixel
// Data, and the bytes after each frame's end-of-image marker, and returns what
// it removed, with the segments it kept marked Kept. The entropy-coded image
// data is copied byte for byte, never decoded and coded again, so the frames
// decode to the same samples as before; the fragments are laid out again, one
// to a frame, with a Basic Offset Table, and an Extended Offset Table the
// image had is removed because it would no longer be right.
//
// Kept, by marker, and why:
//
//   - APP0 holding JFIF is kept, because the JFIF header says the three
//     components are YCbCr, which a decoder takes as the color transform. Its
//     thumbnail, if it has one, is dropped (the segment is rewritten with a
//     thumbnail of 0 by 0), since a thumbnail is a small image that can carry
//     the text the full one does. Any other APP0, JFXX included, is removed.
//   - APP14 holding Adobe is kept, cut to its 12 defined bytes, because its
//     transform byte says whether three components are YCbCr or RGB.
//   - APP8 holding mrfx in a JPEG-LS frame is kept, because it is the HP color
//     transform extension: a decoder undoes the transform with it, and without
//     it the samples come out as the transformed ones.
//   - Every other APPn is removed: Exif and XMP (APP1), an ICC profile (APP2,
//     which no decoder applies to the samples; DICOM carries the profile in
//     ICC Profile (0028,2000)), SPIFF (APP8), Photoshop (APP13) and the rest
//     hold metadata, not samples. A decoder that does not know a segment
//     skips it by its length, which is the whole point of a segment.
//   - Every COM is removed.
//
// The data set is changed only when something is removed, and then only its
// Pixel Data and Extended Offset Table; on an error it is left as it was.
// Errors are those of [JPEGSegmentText]. Run it before [Anonymizer.Anonymize]
// when the image is to be shared; [Anonymizer.SetRemoveJPEGText] does.
func RemoveJPEGText(ds *dataset.Dataset) ([]JPEGSegment, error) {
	if ds == nil {
		return nil, fmt.Errorf("dataset is nil")
	}
	frames, err := jpegFrames(ds)
	if err != nil || len(frames) == 0 {
		return nil, err
	}
	var report []JPEGSegment
	changed := false
	cleaned := make([][]byte, len(frames))
	for i, frame := range frames {
		parsed, err := scanJPEG(frame)
		if err != nil {
			return nil, fmt.Errorf("anonymize: frame %d: %w", i, err)
		}
		out, segments := parsed.clean(frame, i)
		report = append(report, segments...)
		if !bytes.Equal(out, frame) {
			changed = true
		}
		cleaned[i] = out
	}
	if !changed {
		return report, nil
	}
	for i := range cleaned {
		if len(cleaned[i])%2 == 1 {
			cleaned[i] = append(append([]byte{}, cleaned[i]...), 0x00)
		}
	}
	value, err := compress.EncapsulateFrames(cleaned, 1, true)
	if err != nil {
		return nil, fmt.Errorf("anonymize: encapsulating the cleaned frames: %w", err)
	}
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, _ := ds.Get(pixelData)
	elem.SetVR(dataelem.OB) // encapsulated pixel data is OB (PS3.5 A.4)
	if err := ds.SetValue(pixelData, value); err != nil {
		return nil, fmt.Errorf("anonymize: replacing Pixel Data: %w", err)
	}
	ds.Remove(tag.New(0x7FE0, 0x0001))
	ds.Remove(tag.New(0x7FE0, 0x0002))
	return report, nil
}

// SetRemoveJPEGText makes Anonymize call [RemoveJPEGText] before it applies
// the profile, so that the comment and application segments of encapsulated
// JPEG Pixel Data do not travel with the de-identified image. It is off by
// default, which leaves the pixel data as it was. When on, a syntax whose
// segments are not searched makes Anonymize return an error wrapping
// [ErrJPEGSegmentsNotSearched] and change nothing.
func (a *Anonymizer) SetRemoveJPEGText(remove bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.removeJPEGText = remove
}

// jpegSegment is a marker segment of a frame: where it starts at its marker,
// where it ends after its last byte.
type jpegSegment struct {
	marker     byte
	start, end int
}

// jpegScan is the structure of one frame.
type jpegScan struct {
	segments []jpegSegment
	// eoi is the offset of the end-of-image marker's first byte.
	eoi int
	// ls is set for JPEG-LS, which a start-of-frame of 0xF7 says.
	ls bool
}

// scanJPEG walks a frame's markers: each segment by its length, and the
// entropy-coded data after a scan header by the rule for byte stuffing. In
// JPEG a 0xFF in the data is followed by 0x00; in JPEG-LS by a byte whose top
// bit is 0, so a marker there is a 0xFF followed by a byte that has it set.
// Restart markers are inside the scan in both.
func scanJPEG(frame []byte) (*jpegScan, error) {
	n := len(frame)
	if n < 4 || frame[0] != 0xFF || frame[1] != 0xD8 {
		return nil, fmt.Errorf("does not begin with a start-of-image marker")
	}
	scan := &jpegScan{eoi: -1}
	pos, inScan := 2, false
	for pos < n {
		if inScan {
			// Entropy-coded data, to the next marker that is not stuffing or a
			// restart.
			for pos < n {
				if frame[pos] != 0xFF {
					pos++
					continue
				}
				if pos+1 >= n {
					return nil, fmt.Errorf("ends inside its image data")
				}
				next := frame[pos+1]
				switch {
				case next == 0xFF: // fill before a marker
					pos++
					continue
				case scan.ls && next&0x80 == 0:
					pos += 2
					continue
				case !scan.ls && next == 0x00:
					pos += 2
					continue
				case next >= 0xD0 && next <= 0xD7: // restart
					pos += 2
					continue
				}
				break
			}
			inScan = false
			if pos >= n {
				return nil, fmt.Errorf("ends inside its image data")
			}
		}
		if frame[pos] != 0xFF {
			return nil, fmt.Errorf("expected a marker at offset %d", pos)
		}
		for pos+1 < n && frame[pos+1] == 0xFF { // fill
			pos++
		}
		if pos+1 >= n {
			return nil, fmt.Errorf("ends inside a marker")
		}
		m := frame[pos+1]
		switch {
		case m == 0xD9:
			scan.eoi = pos
			return scan, nil
		case m == 0xD8 || m == 0x01 || (m >= 0xD0 && m <= 0xD7):
			pos += 2
			continue
		case m == 0x00:
			return nil, fmt.Errorf("a stuffed byte outside image data at offset %d", pos)
		}
		if pos+4 > n {
			return nil, fmt.Errorf("ends inside the segment at offset %d", pos)
		}
		length := int(binary.BigEndian.Uint16(frame[pos+2:]))
		if length < 2 || pos+2+length > n {
			return nil, fmt.Errorf("the segment at offset %d declares %d bytes and the frame has %d left",
				pos, length, n-pos-2)
		}
		scan.segments = append(scan.segments, jpegSegment{marker: m, start: pos, end: pos + 2 + length})
		if m == 0xF7 {
			scan.ls = true
		}
		pos += 2 + length
		inScan = m == 0xDA
	}
	return nil, fmt.Errorf("has no end-of-image marker")
}

// isText says whether a segment is one this package searches for text.
func (s jpegSegment) isText() bool { return s.marker == 0xFE || (s.marker >= 0xE0 && s.marker <= 0xEF) }

// trailing returns the bytes after the end-of-image marker that are not the
// pad byte, or nil.
func (p *jpegScan) trailing(frame []byte) []byte {
	rest := frame[p.eoi+2:]
	// One byte is the pad that makes the fragment even, whatever its value:
	// a writer's choice, and no room for text.
	if len(rest) <= 1 {
		return nil
	}
	if len(bytes.TrimRight(rest, "\x00")) == 0 {
		return nil
	}
	return rest
}

// interesting lists the segments to report, as JPEGSegment values.
func (p *jpegScan) interesting(frame []byte, index int) []JPEGSegment {
	var out []JPEGSegment
	for _, s := range p.segments {
		if s.isText() {
			out = append(out, newJPEGSegment(index, s.marker, s.start, frame[s.start+4:s.end]))
		}
	}
	if rest := p.trailing(frame); rest != nil {
		out = append(out, newJPEGSegment(index, 0, p.eoi+2, rest))
	}
	return out
}

// newJPEGSegment builds the report entry for a segment's payload.
func newJPEGSegment(frame int, marker byte, offset int, data []byte) JPEGSegment {
	s := JPEGSegment{Frame: frame, Marker: marker, Offset: offset, Data: append([]byte{}, data...)}
	s.Text = segmentText(marker, data)
	return s
}

// segmentText is what a payload reads as: a comment whole, anything else as
// the runs of four or more printable characters.
func segmentText(marker byte, data []byte) string {
	printable := func(b byte) bool { return b >= 0x20 && b < 0x7F || b == '\t' || b == '\n' || b == '\r' }
	if marker == 0xFE {
		trimmed := bytes.TrimRight(data, "\x00 \r\n")
		out := make([]byte, len(trimmed))
		for i, b := range trimmed {
			if printable(b) {
				out[i] = b
			} else {
				out[i] = '.'
			}
		}
		return string(out)
	}
	var runs []string
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= 4 {
			runs = append(runs, strings.TrimSpace(string(data[start:end])))
		}
		start = -1
	}
	for i, b := range data {
		if printable(b) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(data))
	return strings.Join(runs, " ")
}

// clean returns the frame without the segments a decoder does not need, and
// the report of what was removed and kept.
func (p *jpegScan) clean(frame []byte, index int) ([]byte, []JPEGSegment) {
	var report []JPEGSegment
	out := make([]byte, 0, len(frame))
	at := 0
	for _, s := range p.segments {
		if !s.isText() {
			continue
		}
		payload := frame[s.start+4 : s.end]
		keep, replacement := p.keepSegment(s, payload)
		entry := newJPEGSegment(index, s.marker, s.start, payload)
		entry.Kept = keep
		report = append(report, entry)
		if !keep {
			out = append(out, frame[at:s.start]...)
			at = s.end
			continue
		}
		if replacement != nil {
			out = append(out, frame[at:s.start]...)
			out = append(out, replacement...)
			at = s.end
		}
	}
	// Everything after the last text segment, up to the end of the image. A
	// pad byte that kept the fragment even stays; a note after it does not.
	out = append(out, frame[at:p.eoi+2]...)
	if rest := p.trailing(frame); rest != nil {
		report = append(report, newJPEGSegment(index, 0, p.eoi+2, rest))
	} else {
		out = append(out, frame[p.eoi+2:]...)
	}
	return out, report
}

// keepSegment decides whether a text segment stays, and gives the bytes it is
// rewritten to when it stays in another form. The decisions are the ones the
// RemoveJPEGText documentation gives.
func (p *jpegScan) keepSegment(s jpegSegment, payload []byte) (keep bool, replacement []byte) {
	switch {
	case s.marker == 0xE0 && bytes.HasPrefix(payload, []byte("JFIF\x00")) && len(payload) >= 14:
		if len(payload) == 14 && payload[12] == 0 && payload[13] == 0 {
			return true, nil
		}
		// The 14 defined bytes with a thumbnail of 0 by 0.
		head := append([]byte{}, payload[:14]...)
		head[12], head[13] = 0, 0
		seg := []byte{0xFF, 0xE0, 0, 16}
		return true, append(seg, head...)
	case s.marker == 0xEE && bytes.HasPrefix(payload, []byte("Adobe")) && len(payload) >= 12:
		if len(payload) == 12 {
			return true, nil
		}
		return true, append([]byte{0xFF, 0xEE, 0, 14}, payload[:12]...)
	case s.marker == 0xE8 && p.ls && bytes.HasPrefix(payload, []byte("mrfx")):
		return true, nil
	}
	return false, nil
}
