package dicomweb

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// The retrieve service, WADO-RS (PS3.18 10.4): the instances of a study, of
// a series or one instance as Part 10 files, and their metadata as DICOM
// JSON.
//
// An instance is sent as the archive holds it, byte for byte, whenever the
// request allows: that is the cheapest answer and the only one that is
// certain to be the instance that was stored. It is decoded only for a
// request that asks for Explicit VR Little Endian of an instance held in
// another syntax, which is what a request that names no transfer syntax
// asks for (PS3.18 8.7.3.5.2).

// mediaDICOM is a Part 10 file (PS3.18 8.7.3.1).
const mediaDICOM = "application/dicom"

// The transfer syntaxes retrieval reasons about.
const (
	// anyTransferSyntax asks for each instance as it is stored.
	anyTransferSyntax = "*"
	// explicitVRLittleEndian is the default of a request that names none.
	explicitVRLittleEndian = "1.2.840.10008.1.2.1"
)

// retrieveTarget is what a retrieve resource names: a study, one of its
// series, or one instance of that series; and whether it is their metadata
// that is asked for, or frames of the instance.
type retrieveTarget struct {
	study, series, instance string
	metadata                bool
	// frames is the list of frame numbers of a frames resource, as the path
	// gives it, or empty.
	frames string
	// bulkdata is the path to an element of a bulk data resource, a
	// segment a level, or nil.
	bulkdata []string
	// render is "rendered" or "thumbnail" for those resources, of a study,
	// a series, an instance or frames; otherwise empty.
	render string
}

// retrieveTargetOf reads a request path as a retrieve resource, and reports
// whether it is one.
func retrieveTargetOf(path string) (retrieveTarget, bool, *requestError) {
	var target retrieveTarget
	s := pathSegments(path)
	if n := len(s); n > 2 && (s[n-1] == "rendered" || s[n-1] == "thumbnail") {
		target.render = s[n-1]
		s = s[:n-1]
	}
	if n := len(s); target.render == "" && n > 1 && s[n-1] == "metadata" {
		target.metadata = true
		s = s[:n-1]
	} else if n == 8 && s[6] == "frames" {
		target.frames = s[7]
		s = s[:6]
	} else if n >= 8 && s[6] == "bulkdata" && target.render == "" {
		target.bulkdata = s[7:]
		s = s[:6]
	}
	switch {
	case len(s) == 2 && s[0] == "studies":
		target.study = s[1]
	case len(s) == 4 && s[0] == "studies" && s[2] == "series":
		target.study, target.series = s[1], s[3]
	case len(s) == 6 && s[0] == "studies" && s[2] == "series" && s[4] == "instances":
		target.study, target.series, target.instance = s[1], s[3], s[5]
	default:
		return target, false, nil
	}
	for _, uid := range []string{target.study, target.series, target.instance} {
		if uid != "" && !validUID(uid) {
			return target, true, refuse(http.StatusBadRequest, "%q is not a UID", uid)
		}
	}
	return target, true, nil
}

// wanted is what a retrieve request accepts.
type wanted struct {
	// transferSyntax is the syntax asked for, or anyTransferSyntax.
	transferSyntax string
	// bare asks for the file itself and not a multipart body holding it,
	// which only a request for one instance can be given.
	bare bool
}

// wantedOf reads what a retrieve request accepts (PS3.18 8.7.3.5).
//
// multipart/related; type="application/dicom" is the standard's form, with a
// transfer-syntax parameter, or without one for Explicit VR Little Endian.
// A request that accepts anything, or says nothing, is given each instance
// as it is stored: it did not ask for a syntax, and the stored file is the
// answer that changes nothing. application/dicom on its own asks for the
// file with no multipart around it.
func wantedOf(r *http.Request) (wanted, *requestError) {
	accept := strings.Join(r.Header.Values("Accept"), ",")
	if parameter := r.URL.Query().Get("accept"); parameter != "" {
		accept = parameter
	}
	if strings.TrimSpace(accept) == "" {
		return wanted{transferSyntax: anyTransferSyntax}, nil
	}

	var fallback *wanted
	for _, entry := range strings.Split(accept, ",") {
		media, parameters, err := mime.ParseMediaType(entry)
		if err != nil {
			continue
		}
		if q, err := strconv.ParseFloat(parameters["q"], 64); err == nil && q == 0 {
			continue
		}
		syntax := parameters["transfer-syntax"]
		switch {
		case media == "multipart/related" && strings.EqualFold(parameters["type"], mediaDICOM):
			if syntax == "" {
				syntax = explicitVRLittleEndian
			}
			return wanted{transferSyntax: syntax}, nil
		case media == mediaDICOM:
			if syntax == "" {
				syntax = anyTransferSyntax
			}
			return wanted{transferSyntax: syntax, bare: true}, nil
		case media == "*/*" || media == "multipart/*" || (media == "multipart/related" && parameters["type"] == ""):
			fallback = &wanted{transferSyntax: anyTransferSyntax}
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return wanted{}, refuse(http.StatusNotAcceptable,
		`instances are retrieved as multipart/related; type="%s"; the request accepts %q`, mediaDICOM, accept)
}

// maxMetaHeader bounds the File Meta Information read to learn a file's
// transfer syntax. The header of a real file is a few hundred bytes.
const maxMetaHeader = 64 << 10

// storedSyntax reads the start of a Part 10 file and returns the transfer
// syntax its File Meta Information names, leaving the file at its start.
func storedSyntax(file io.ReadSeeker) (syntax string, err error) {
	// The preamble, "DICM", and the group length element (PS3.10 7.1):
	// 128 + 4 + (4 tag, 2 VR, 2 length, 4 value).
	const fixed = 144
	head := make([]byte, fixed)
	if _, err := io.ReadFull(file, head); err != nil {
		return "", fmt.Errorf("the stored file is too short to be a DICOM file: %w", err)
	}
	if string(head[128:132]) != "DICM" {
		return "", fmt.Errorf("the stored file has no DICM prefix")
	}
	length := binary.LittleEndian.Uint32(head[140:144])
	if length > maxMetaHeader {
		return "", fmt.Errorf("the stored file's meta header claims %d bytes", length)
	}
	rest := make([]byte, length)
	if _, err := io.ReadFull(file, rest); err != nil {
		return "", fmt.Errorf("the stored file ends inside its meta header: %w", err)
	}
	head = append(head, rest...)
	// The meta information begins after the preamble and the prefix.
	meta, err := filereader.NewDCMFileReader(filebase.NewFileReader(bytes.NewReader(head[132:]))).ReadFileMetaInfo()
	if err != nil {
		return "", fmt.Errorf("the stored file's meta header: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	syntax = strings.TrimRight(meta.TransferSyntaxUID, " \x00")
	if syntax == "" {
		return "", fmt.Errorf("the stored file's meta header names no transfer syntax")
	}
	return syntax, nil
}

// part is one instance ready to be sent: its transfer syntax and its bytes,
// which are the stored file when it is sent as it is.
type part struct {
	syntax string
	body   io.Reader
	close  func()
}

// open prepares an instance for a request. It reports false, with no
// error, for an instance the request does not accept: one held in a syntax
// other than the one asked for, which is not Explicit VR Little Endian.
func (s *Server) open(r *http.Request, uid string, want wanted) (*part, bool, error) {
	file, err := s.backend.Open(r.Context(), uid)
	if err != nil {
		return nil, false, err
	}
	syntax, err := storedSyntax(file)
	if err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("%s: %w", uid, err)
	}
	switch want.transferSyntax {
	case anyTransferSyntax, syntax:
		return &part{syntax: syntax, body: file, close: func() { _ = file.Close() }}, true, nil
	case explicitVRLittleEndian:
		defer func() { _ = file.Close() }()
		data, err := toExplicitVRLittleEndian(file)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", uid, err)
		}
		return &part{syntax: explicitVRLittleEndian, body: bytes.NewReader(data), close: func() {}}, true, nil
	}
	_ = file.Close()
	return nil, false, nil
}

// toExplicitVRLittleEndian reads a Part 10 file and writes it again with
// its pixel data decoded, which is how the default transfer syntax is
// served for an instance held compressed.
func toExplicitVRLittleEndian(file io.Reader) ([]byte, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	parsed, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		return nil, err
	}
	ds := parsed.GetDataset()
	if err := ds.DecompressInPlace(); err != nil {
		return nil, err
	}
	return encodeFile(ds)
}

// retrieve answers a request for instances.
func (s *Server) retrieve(w http.ResponseWriter, r *http.Request, target retrieveTarget) {
	want, refused := wantedOf(r)
	if refused != nil {
		s.fail(w, refused)
		return
	}
	if want.bare && target.instance == "" {
		s.fail(w, refuse(http.StatusNotAcceptable,
			`several instances are retrieved as multipart/related; type="%s"`, mediaDICOM))
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

	// The first instance the request accepts is prepared before anything
	// is written, so a request that accepts none can still be refused.
	skipped := 0
	var first *part
	for first == nil && len(uids) > 0 {
		prepared, ok, err := s.open(r, uids[0], want)
		uids = uids[1:]
		if err != nil {
			s.fail(w, err)
			return
		}
		if !ok {
			skipped++
			continue
		}
		first = prepared
	}
	if first == nil {
		s.fail(w, refuse(http.StatusNotAcceptable,
			"none of the instances is held in transfer syntax %s", want.transferSyntax))
		return
	}

	if want.bare {
		defer first.close()
		w.Header().Set("Content-Type", mediaDICOM+"; transfer-syntax="+first.syntax)
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, first.body)
		return
	}

	body := multipart.NewWriter(w)
	w.Header().Set("Content-Type",
		`multipart/related; type="`+mediaDICOM+`"; boundary=`+body.Boundary())
	// Sent before the rest are opened, so a study is never held whole. An
	// instance found unacceptable after this point is left out, and the
	// status cannot say so any more; one already found makes it 206
	// (PS3.18 10.4.3.1).
	if skipped > 0 {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	send := func(p *part) {
		defer p.close()
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", mediaDICOM+"; transfer-syntax="+p.syntax)
		out, err := body.CreatePart(header)
		if err == nil {
			_, err = io.Copy(out, p.body)
		}
		if err != nil {
			// The status has gone. Cutting the connection is the one way
			// left to say the body is not whole.
			panic(http.ErrAbortHandler)
		}
	}
	send(first)
	for _, uid := range uids {
		prepared, ok, err := s.open(r, uid, want)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		if ok {
			send(prepared)
		}
	}
	if err := body.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// metadataThreshold is the longest binary value metadata carries inline.
// Longer ones, and Pixel Data whatever its length, are bulk data.
const metadataThreshold = 1 << 10

// metadata answers a request for the metadata of instances: each data set
// as DICOM JSON with its bulk data left out (PS3.18 10.4.1.1.2), and a
// BulkDataURI where each bulk value was, at which the bulk data resource
// serves it (PS3.18 F.2.6).
func (s *Server) metadata(w http.ResponseWriter, r *http.Request, target retrieveTarget) {
	contentType, refused := jsonContentType(r)
	if refused != nil {
		s.fail(w, refused)
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

	results := make([]map[string]dataset.DICOMJSONElement, 0, len(uids))
	for _, uid := range uids {
		result, err := s.instanceMetadata(r, uid)
		if err != nil {
			s.fail(w, err)
			return
		}
		results = append(results, result)
	}
	body, err := json.Marshal(results)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// instanceMetadata reads one instance without its bulk data, so memory
// follows the attributes and not the image.
func (s *Server) instanceMetadata(r *http.Request, uid string) (map[string]dataset.DICOMJSONElement, error) {
	file, err := s.backend.Open(r.Context(), uid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	parsed, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(file),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: metadataThreshold})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", uid, err)
	}
	ds := parsed.GetDataset()
	described, err := ds.ToDICOMJSON()
	if err != nil {
		return nil, err
	}
	// The instance's own resource, by the UIDs it holds, which are the ones
	// the archive found it by.
	base := retrieveURL(serviceRoot(r), ds, levelInstance)
	if base != "" {
		bulkDataURIs(described, parsed.DataElements, base, bulkPath{})
	}
	return described, nil
}
