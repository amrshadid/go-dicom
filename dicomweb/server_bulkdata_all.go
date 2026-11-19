package dicomweb

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// The bulk data of a study, a series or an instance as one resource
// (PS3.18 10.4.1.1.5, Table 10.4.1.5-1):
//
//	GET /studies/{study}/bulkdata
//	GET /studies/{study}/series/{series}/bulkdata
//	GET /studies/{study}/series/{series}/instances/{instance}/bulkdata
//
// Every value metadata gives a BulkDataURI for, each in a part of its own
// whose Content-Location is that URI (PS3.18 8.6.1.2: "The Content-Location
// of the Bulk Data part shall contain the corresponding BulkDataURI used in
// the referencing Metadata"), so a client that holds the metadata can put
// each part back where it belongs. The instances come in the archive's
// order and the values of each in the order of its data set.
//
// One instance is open at a time and each value is read from the file by
// where it lies, as the single resource reads it, so a series costs its
// largest value, not the series.

// allBulkData answers a request for the bulk data of a study, a series or
// an instance.
func (s *Server) allBulkData(w http.ResponseWriter, r *http.Request, target retrieveTarget) {
	// The bulk data of several values is multipart/related with
	// application/octet-stream parts, the one form Table 10.4.4-1 requires.
	// A single-part type is for a resource of one item (its Note 3), and a
	// compressed type alone cannot hold the values that are not pixels, so
	// both are refused. transfer-syntax=* sends compressed pixels as they
	// are stored, each frame in its own type.
	wants, accept := frameWantsOf(r)
	var allowed []frameWant
	for _, want := range wants {
		if want.media == "" {
			allowed = append(allowed, want)
		}
	}
	if len(allowed) == 0 {
		s.fail(w, refuse(http.StatusNotAcceptable,
			`bulk data of several values is multipart/related; type="%s"; the request accepts %q`, mediaOctetStream, accept))
		return
	}
	uids, err := s.backend.Locate(r.Context(), target.study, target.series, target.instance)
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(uids) == 0 {
		s.fail(w, refuse(http.StatusNotFound, "the archive holds no such instances"))
		return
	}

	// The body is begun with the first part, so that whatever fails before
	// it can still be answered with a status. After it, the status has gone
	// and cutting the connection is the one way left to say the body is not
	// whole.
	var body *multipart.Writer
	part := func(header textproto.MIMEHeader) (io.Writer, error) {
		if body == nil {
			body = multipart.NewWriter(w)
			w.Header().Set("Content-Type", `multipart/related; type="`+mediaOctetStream+`"; boundary=`+body.Boundary())
			w.WriteHeader(http.StatusOK)
		}
		return body.CreatePart(header)
	}
	for _, uid := range uids {
		if err := s.instanceBulkData(r, uid, allowed, part); err != nil {
			if body == nil {
				s.fail(w, err)
				return
			}
			panic(http.ErrAbortHandler)
		}
	}
	if body == nil {
		// A multipart body has at least one part (RFC 2046 5.1.1); a
		// resource with nothing in it is answered with no body.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := body.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// instanceBulkData sends each bulk value of one instance, each in a part
// part begins, in the forms wants accepts.
func (s *Server) instanceBulkData(r *http.Request, uid string, wants []frameWant,
	part func(textproto.MIMEHeader) (io.Writer, error)) error {
	file, err := s.backend.Open(r.Context(), uid)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	parsed, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(file),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: metadataThreshold})
	if err != nil {
		return fmt.Errorf("%s: %w", uid, err)
	}
	// The paths are the ones metadata names, found by the same walk, so
	// the parts are exactly the values whose URIs metadata gives.
	ds := parsed.GetDataset()
	described, err := ds.ToDICOMJSON()
	if err != nil {
		return err
	}
	base := retrieveURL(serviceRoot(r), ds, levelInstance)
	if base == "" {
		return nil
	}
	for _, path := range bulkDataURIs(described, parsed.DataElements, base, bulkPath{}) {
		location := base + "/bulkdata/" + path.String()
		element := path.find(parsed.DataElements)
		if element == nil {
			return fmt.Errorf("%s: %s is not in the instance", uid, path)
		}
		if len(path.tags) == 1 && element.Tag == tagPixelData {
			if err := sendPixelsAsBulkData(file, wants, base, location, part); err != nil {
				return err
			}
			continue
		}
		value, err := bulkValue(file, parsed, path, element)
		if err != nil {
			return err
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", mediaOctetStream+"; transfer-syntax="+explicitVRLittleEndian)
		header.Set("Content-Location", location)
		out, err := part(header)
		if err == nil {
			_, err = out.Write(value)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// sendPixelsAsBulkData sends the pixels of the instance in file as the
// single bulk data resource sends them: uncompressed, every frame in one
// part at the pixels' BulkDataURI; or, for compressed pixels asked for as
// stored, a part a frame in the frames' own type, each at its frame's
// resource (PS3.18 10.4.1.1.6). A frame is read and written before the
// next is read, so the pixels cost one frame.
func sendPixelsAsBulkData(file io.ReadSeeker, wants []frameWant, base, location string,
	part func(textproto.MIMEHeader) (io.Writer, error)) error {
	// The file has been read for its attributes; the frames are found by
	// reading it again from the start.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	source, err := openFrames(file)
	if err != nil {
		return err
	}
	media, syntax, decode, ok := source.choose(wants)
	if !ok {
		return refuse(http.StatusNotAcceptable,
			"the pixels at %s are held in transfer syntax %s, which this server does not decode", location, source.syntax)
	}
	joined := media == mediaOctetStream
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", media+"; transfer-syntax="+syntax)
	var out io.Writer
	for n := 1; n <= source.count(); n++ {
		// Read before its part is begun, so the first frame of the first
		// instance that cannot be read or decoded is still answered with a
		// status.
		frame, err := source.frame(n, decode)
		if err != nil {
			return err
		}
		if out == nil || !joined {
			if joined {
				header.Set("Content-Location", location)
			} else {
				header.Set("Content-Location", fmt.Sprintf("%s/frames/%d", base, n))
			}
			if out, err = part(header); err != nil {
				return err
			}
		}
		if _, err := out.Write(frame); err != nil {
			return err
		}
	}
	return nil
}
