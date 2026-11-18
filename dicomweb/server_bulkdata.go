package dicomweb

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// Bulk data (PS3.18 10.4.1.1.5): the values metadata leaves out, each at
// the URI metadata gives for it in their place (PS3.18 F.2.6).
//
// The standard leaves the form of the URI to the server. Here it is the
// instance's own resource, then bulkdata, then the path to the element:
// its tag, and for one inside a sequence the sequence's tag, the item
// counted from 0, and the tag within, as many times as it is nested:
//
//	…/instances/{instance}/bulkdata/7FE00010
//	…/instances/{instance}/bulkdata/00880200/0/7FE00010
//
// The value is read from the file by where it lies, as metadata leaves it
// there, so a value costs its own bytes.

// bulkPath is the path to an element: a tag at each level, and between two
// tags the item of the sequence the first one is.
type bulkPath struct {
	tags  []tag.Tag
	items []int
}

// bulkPathOf reads the part of a bulk data URI after bulkdata.
func bulkPathOf(segments []string) (bulkPath, *requestError) {
	var path bulkPath
	bad := refuse(http.StatusBadRequest, "%q is not a path to an element: a tag of eight hexadecimal digits, "+
		"and for one inside a sequence the item counted from 0 and the tag within", strings.Join(segments, "/"))
	if len(segments)%2 == 0 {
		return path, bad
	}
	for i, segment := range segments {
		if i%2 == 1 {
			item, err := strconv.Atoi(segment)
			if err != nil || item < 0 || strings.HasPrefix(segment, "+") {
				return path, bad
			}
			path.items = append(path.items, item)
			continue
		}
		value, err := strconv.ParseUint(segment, 16, 32)
		if err != nil || len(segment) != 8 {
			return path, bad
		}
		path.tags = append(path.tags, tag.New(uint16(value>>16), uint16(value)))
	}
	return path, nil
}

// String writes the path as a URI gives it.
func (p bulkPath) String() string {
	var b strings.Builder
	for i, t := range p.tags {
		if i > 0 {
			fmt.Fprintf(&b, "/%d/", p.items[i-1])
		}
		fmt.Fprintf(&b, "%04X%04X", t.Group(), t.Element())
	}
	return b.String()
}

// find walks elements to the one the path names, or returns nil.
func (p bulkPath) find(elements []*filereader.DataElementValue) *filereader.DataElementValue {
	for level, want := range p.tags {
		var found *filereader.DataElementValue
		for _, element := range elements {
			if element.Tag == want {
				found = element
				break
			}
		}
		if found == nil || level == len(p.tags)-1 {
			return found
		}
		item := p.items[level]
		if item >= len(found.Items) {
			return nil
		}
		elements = found.Items[item].Elements
	}
	return nil
}

// isBulkVR reports whether a VR is one whose value is bytes, which is what
// bulk data is. A value nobody gave a VR may be either.
func isBulkVR(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OV", "OW", "UN", "", "OB or OW":
		return true
	}
	return false
}

// bulkdata answers a request for one bulk value of an instance.
func (s *Server) bulkdata(w http.ResponseWriter, r *http.Request, target retrieveTarget) {
	path, refused := bulkPathOf(target.bulkdata)
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

	// Pixel Data of the instance itself is its frames, sent as the frames
	// resource sends them: the frames' own coding when that is asked for,
	// or all of them uncompressed in one part.
	if len(path.tags) == 1 && path.tags[0] == tagPixelData {
		source, err := openFrames(file)
		if err != nil {
			s.fail(w, err)
			return
		}
		numbers := make([]int, len(source.ranges))
		for i := range numbers {
			numbers[i] = i + 1
		}
		s.sendFrames(w, r, source, numbers, true)
		return
	}

	wants, accept := frameWantsOf(r)
	uncompressed := false
	for _, want := range wants {
		uncompressed = uncompressed || want.media == ""
	}
	if !uncompressed {
		s.fail(w, refuse(http.StatusNotAcceptable,
			"bulk data other than Pixel Data is %s; the request accepts %q", mediaOctetStream, accept))
		return
	}
	parsed, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(file),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: metadataThreshold})
	if err != nil {
		s.fail(w, err)
		return
	}
	element := path.find(parsed.DataElements)
	if element == nil || element.UndefinedLength || element.VR == "SQ" || !isBulkVR(element.VR) {
		s.fail(w, refuse(http.StatusNotFound, "the instance has no bulk value at %s", path))
		return
	}
	value := element.Value
	switch {
	case element.ValueSkipped && parsed.Deflated:
		// Positions count in the inflated data set, which is not on disk.
		// It is all inflated to be read at all, so it is read whole.
		if _, err = file.Seek(0, io.SeekStart); err == nil {
			parsed, err = filereader.ReadDICOMFile(filebase.NewFileReader(file))
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		inflated := path.find(parsed.DataElements)
		if inflated == nil {
			s.fail(w, fmt.Errorf("%s is not where it was when the file was read in full", path))
			return
		}
		value = inflated.Value
	case element.ValueSkipped:
		value = make([]byte, element.Length)
		if _, err := readerAtOf(file).ReadAt(value, element.Position.ValueOffset); err != nil {
			s.fail(w, err)
			return
		}
		// A value read here is as the file has it; one the reader read it
		// has already put in little endian.
		if !parsed.IsLittleEndian {
			dataelem.SwapByteOrder(dataelem.VR(element.VR), value)
		}
	}

	body := multipart.NewWriter(w)
	w.Header().Set("Content-Type", `multipart/related; type="`+mediaOctetStream+`"; boundary=`+body.Boundary())
	w.WriteHeader(http.StatusOK)
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", mediaOctetStream+"; transfer-syntax="+explicitVRLittleEndian)
	out, err := body.CreatePart(header)
	if err == nil {
		_, err = out.Write(value)
	}
	if err == nil {
		err = body.Close()
	}
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

// readerAtOf reads a file by offset, by its own ReadAt when it has one.
func readerAtOf(file io.ReadSeeker) io.ReaderAt {
	if at, ok := file.(io.ReaderAt); ok {
		return at
	}
	return readerAt{file}
}

// bulkDataURIs puts a BulkDataURI in metadata where each value was left in
// the file (PS3.18 F.2.6): elements are the file's, read with its bulk data
// skipped, and described is the DICOM JSON made of them.
func bulkDataURIs(described map[string]dataset.DICOMJSONElement, elements []*filereader.DataElementValue,
	base string, path bulkPath) {
	for _, element := range elements {
		key := fmt.Sprintf("%04X%04X", element.Tag.Group(), element.Tag.Element())
		entry, ok := described[key]
		if !ok {
			continue
		}
		at := bulkPath{tags: append(append([]tag.Tag{}, path.tags...), element.Tag), items: path.items}
		if element.ValueSkipped && !(len(at.tags) > 1 && element.Tag == tagPixelData && element.UndefinedLength) {
			entry.BulkDataURI = base + "/bulkdata/" + at.String()
			described[key] = entry
			continue
		}
		for i, item := range element.Items {
			if i >= len(entry.Value) {
				break
			}
			nested, ok := entry.Value[i].(map[string]dataset.DICOMJSONElement)
			if !ok {
				continue
			}
			bulkDataURIs(nested, item.Elements, base, bulkPath{
				tags:  at.tags,
				items: append(append([]int{}, path.items...), i),
			})
		}
	}
}
