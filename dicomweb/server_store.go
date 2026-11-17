package dicomweb

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The store service, STOW-RS (PS3.18 10.5): Part 10 files sent as the parts
// of one request, each kept or refused on its own, and an answer that says
// which.

// DefaultMaxStoreBytes bounds the body of one store request when the server
// is not given another bound: 4 GiB, as the client's bound on a response
// is. A request that runs past it is refused, and what it had already sent
// whole is kept.
const DefaultMaxStoreBytes = 4 << 30

// The reasons an instance is not kept (PS3.18 Table 10.5.3-1, the statuses
// of PS3.4 B.2.3).
const (
	// reasonProcessingFailure: the archive could not keep it.
	reasonProcessingFailure = 0x0110
	// reasonDoesNotMatch: the instance is not of the study it was sent to.
	reasonDoesNotMatch = 0xA900
)

// stored is what became of one part of a store request.
type stored struct {
	class, instance, study, series string
	// reason is 0 for an instance that was kept.
	reason uint16
}

// store answers a store request: to the service, or into a study when the
// path names one.
func (s *Server) store(w http.ResponseWriter, r *http.Request, study string) {
	contentType, refused := jsonContentType(r)
	if refused != nil {
		s.fail(w, refused)
		return
	}
	media, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/related" || parameters["boundary"] == "" {
		s.fail(w, refuse(http.StatusUnsupportedMediaType,
			`instances are stored as multipart/related; type="%s"`, mediaDICOM))
		return
	}
	// Metadata with its bulk data in separate parts (PS3.18 10.5.1.1.2) is
	// the other form the standard has, and is not taken.
	if kind := parameters["type"]; kind != "" && !strings.EqualFold(kind, mediaDICOM) {
		s.fail(w, refuse(http.StatusUnsupportedMediaType,
			`instances are stored as multipart/related; type="%s", not type="%s"`, mediaDICOM, kind))
		return
	}

	var outcomes []stored
	unreadable := 0
	reader := multipart.NewReader(http.MaxBytesReader(w, r.Body, s.maxStore), parameters["boundary"])
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			break
		}
		var data []byte
		if err == nil {
			data, err = io.ReadAll(part)
		}
		if err != nil {
			// What came whole before this is kept, and the client is told
			// the request was not: it cannot be told which parts were lost.
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				s.fail(w, refuse(http.StatusRequestEntityTooLarge,
					"the request is larger than the %d bytes this service takes", s.maxStore))
				return
			}
			s.fail(w, refuse(http.StatusBadRequest, "the request body is not well formed multipart"))
			return
		}
		outcome, ok := s.storeOne(r, study, data)
		if !ok {
			unreadable++
			continue
		}
		outcomes = append(outcomes, outcome)
	}
	if len(outcomes) == 0 && unreadable == 0 {
		s.fail(w, refuse(http.StatusBadRequest, "the request holds no instances"))
		return
	}

	kept := 0
	for _, outcome := range outcomes {
		if outcome.reason == 0 {
			kept++
		}
	}
	// PS3.18 10.5.3: 200 when every instance was kept, 202 when some were,
	// 409 when none was.
	status := http.StatusOK
	switch {
	case kept == 0:
		status = http.StatusConflict
	case kept < len(outcomes) || unreadable > 0:
		status = http.StatusAccepted
	}

	answer, err := storeAnswer(serviceRoot(r), study, outcomes).ToDICOMJSON()
	if err != nil {
		s.fail(w, err)
		return
	}
	body, err := json.Marshal(answer)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// storeOne keeps one part. It reports false for a part that cannot be read
// as an instance at all: there is then no SOP Instance UID to answer for,
// so it is counted and not listed.
func (s *Server) storeOne(r *http.Request, study string, data []byte) (stored, bool) {
	parsed, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		return stored{}, false
	}
	ds := parsed.GetDataset()
	outcome := stored{
		class:    text(ds, tag.New(0x0008, 0x0016)),
		instance: text(ds, tag.New(0x0008, 0x0018)),
		study:    text(ds, tagStudyInstanceUID),
		series:   text(ds, tagSeriesInstanceUID),
	}
	if !validUID(outcome.class) || !validUID(outcome.instance) {
		return stored{}, false
	}
	switch {
	case study != "" && outcome.study != study:
		outcome.reason = reasonDoesNotMatch
	case s.backend.Put(r.Context(), ds) != nil:
		// Why is the archive's to know, and may name its files.
		outcome.reason = reasonProcessingFailure
	}
	return outcome, true
}

// storeAnswer is the data set a store request is answered with (PS3.18
// 10.5.3.2, PS3.3 C.4.19): the instances kept, each with where it is
// retrieved from, and the ones refused, each with why.
func storeAnswer(root, study string, outcomes []stored) *dataset.Dataset {
	kept, refused := sequence.New(), sequence.New()
	studies := map[string]bool{}
	for _, outcome := range outcomes {
		item := dataset.NewDataset()
		_ = item.Add(dataelem.NewDataElement(tagRefSOPClass, dataelem.UI, outcome.class))
		_ = item.Add(dataelem.NewDataElement(tagRefSOPInstance, dataelem.UI, outcome.instance))
		if outcome.reason != 0 {
			_ = item.Add(dataelem.NewDataElement(tagFailureReason, dataelem.US, outcome.reason))
			_ = refused.Append(item)
			continue
		}
		studies[outcome.study] = true
		if validUID(outcome.study) && validUID(outcome.series) {
			_ = item.Add(dataelem.NewDataElement(tagRetrieveURL, dataelem.UR,
				root+"/studies/"+outcome.study+"/series/"+outcome.series+"/instances/"+outcome.instance))
		}
		_ = kept.Append(item)
	}

	answer := dataset.NewDataset()
	// One URL for the study, when what was kept is of one study.
	if study == "" && len(studies) == 1 {
		for only := range studies {
			study = only
		}
	}
	if validUID(study) && kept.Length() > 0 {
		_ = answer.Add(dataelem.NewDataElement(tagRetrieveURL, dataelem.UR, root+"/studies/"+study))
	}
	if kept.Length() > 0 {
		_ = answer.Add(dataelem.NewDataElement(tagReferencedSOPs, dataelem.SQ, kept))
	}
	if refused.Length() > 0 {
		_ = answer.Add(dataelem.NewDataElement(tagFailedSOPs, dataelem.SQ, refused))
	}
	return answer
}
