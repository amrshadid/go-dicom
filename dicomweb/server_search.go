package dicomweb

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The search service, QIDO-RS (PS3.18 10.6).
//
// A search is a C-FIND written as a URL: the resource names the level, the
// query parameters name the attributes to match and to return. So the server
// builds the identifier a C-FIND would carry and asks the archive, and what
// matches over DIMSE is what matches over HTTP.

var (
	tagQueryRetrieveLevel = tag.New(0x0008, 0x0052)
	tagStudyInstanceUID   = tag.New(0x0020, 0x000D)
	tagSeriesInstanceUID  = tag.New(0x0020, 0x000E)
)

// The Query/Retrieve Level of each search resource (PS3.4 C.6.2.1).
const (
	levelStudy    = "STUDY"
	levelSeries   = "SERIES"
	levelInstance = "IMAGE"
)

// The attributes a search returns whether or not it is asked for them
// (PS3.18 10.6.3.3, Tables 10.6.3-3 to 10.6.3-5). Retrieve URL (0008,1190) is
// in each table too: the server knows it and the archive does not, so it is
// added to each result and never asked of the archive.
var (
	studyAttributes = []tag.Tag{
		tag.New(0x0008, 0x0020), // StudyDate
		tag.New(0x0008, 0x0030), // StudyTime
		tag.New(0x0008, 0x0050), // AccessionNumber
		tag.New(0x0008, 0x0056), // InstanceAvailability
		tag.New(0x0008, 0x0061), // ModalitiesInStudy
		tag.New(0x0008, 0x0090), // ReferringPhysicianName
		tag.New(0x0008, 0x0201), // TimezoneOffsetFromUTC
		tag.New(0x0010, 0x0010), // PatientName
		tag.New(0x0010, 0x0020), // PatientID
		tag.New(0x0010, 0x0030), // PatientBirthDate
		tag.New(0x0010, 0x0040), // PatientSex
		tagStudyInstanceUID,
		tag.New(0x0020, 0x0010), // StudyID
		tag.New(0x0020, 0x1206), // NumberOfStudyRelatedSeries
		tag.New(0x0020, 0x1208), // NumberOfStudyRelatedInstances
	}
	seriesAttributes = []tag.Tag{
		tag.New(0x0008, 0x0060), // Modality
		tag.New(0x0008, 0x0201), // TimezoneOffsetFromUTC
		tag.New(0x0008, 0x103E), // SeriesDescription
		tagSeriesInstanceUID,
		tag.New(0x0020, 0x0011), // SeriesNumber
		tag.New(0x0020, 0x1209), // NumberOfSeriesRelatedInstances
		tag.New(0x0040, 0x0244), // PerformedProcedureStepStartDate
		tag.New(0x0040, 0x0245), // PerformedProcedureStepStartTime
		tag.New(0x0040, 0x0275), // RequestAttributesSequence
	}
	instanceAttributes = []tag.Tag{
		tag.New(0x0008, 0x0016), // SOPClassUID
		tag.New(0x0008, 0x0018), // SOPInstanceUID
		tag.New(0x0008, 0x0056), // InstanceAvailability
		tag.New(0x0008, 0x0201), // TimezoneOffsetFromUTC
		tag.New(0x0020, 0x0013), // InstanceNumber
		tag.New(0x0028, 0x0008), // NumberOfFrames
		tag.New(0x0028, 0x0010), // Rows
		tag.New(0x0028, 0x0011), // Columns
		tag.New(0x0028, 0x0100), // BitsAllocated
	}
)

// searchTarget is what a search resource names: a level, and the study and
// series it searches within, when the path gives them.
type searchTarget struct {
	level         string
	study, series string
}

// searchTargetOf reads a request path as a search resource.
func searchTargetOf(path string) (searchTarget, *requestError) {
	var target searchTarget
	switch s := pathSegments(path); {
	case len(s) == 1 && s[0] == "studies":
		target.level = levelStudy
	case len(s) == 1 && s[0] == "series":
		target.level = levelSeries
	case len(s) == 1 && s[0] == "instances":
		target.level = levelInstance
	case len(s) == 3 && s[0] == "studies" && s[2] == "series":
		target.level, target.study = levelSeries, s[1]
	case len(s) == 3 && s[0] == "studies" && s[2] == "instances":
		target.level, target.study = levelInstance, s[1]
	case len(s) == 5 && s[0] == "studies" && s[2] == "series" && s[4] == "instances":
		target.level, target.study, target.series = levelInstance, s[1], s[3]
	default:
		return target, refuse(http.StatusNotFound, "there is no such resource")
	}
	for _, uid := range []string{target.study, target.series} {
		if uid != "" && !validUID(uid) {
			return target, refuse(http.StatusBadRequest, "%q is not a UID", uid)
		}
	}
	return target, nil
}

// returned lists the attributes a search at the target returns by default:
// those of its level, and those of each level above that the path does not
// already name (PS3.18 10.6.3.3).
func (t searchTarget) returned() []tag.Tag {
	var tags []tag.Tag
	if t.study == "" || t.level == levelStudy {
		tags = append(tags, studyAttributes...)
	}
	if t.level != levelStudy && (t.series == "" || t.level == levelSeries) {
		tags = append(tags, seriesAttributes...)
	}
	if t.level == levelInstance {
		tags = append(tags, instanceAttributes...)
	}
	return tags
}

// paging is the part of the results a search asks for.
type paging struct {
	// offset is how many results to skip, and limit how many to return at
	// most. A limit of 0 is no limit.
	offset, limit int
}

// The query parameters of a search that are not attributes (PS3.18 10.6.1.2
// and 8.3.3).
const (
	parameterIncludeField  = "includefield"
	parameterLimit         = "limit"
	parameterOffset        = "offset"
	parameterFuzzyMatching = "fuzzymatching"
	parameterAccept        = "accept"
	parameterCharset       = "charset"
)

// identifier builds the C-FIND identifier of a search from its target and
// its query parameters, with the results asked for and whether fuzzy
// matching was.
func (t searchTarget) identifier(values url.Values) (*dataset.Dataset, paging, bool, *requestError) {
	var page paging
	fuzzy := false
	identifier := dataset.NewDataset()
	_ = identifier.Add(dataelem.NewDataElement(tagQueryRetrieveLevel, dataelem.CS, t.level))

	// What the path names is matched, and may not be asked for again with
	// another value.
	given := map[string]bool{}
	for _, uid := range []struct {
		tag   tag.Tag
		value string
	}{{tagStudyInstanceUID, t.study}, {tagSeriesInstanceUID, t.series}} {
		if uid.value == "" {
			continue
		}
		_ = identifier.Add(dataelem.NewDataElement(uid.tag, dataelem.UI, uid.value))
		given[attributeKey([]tag.Tag{uid.tag})] = true
	}

	// In a fixed order, so the same request is refused for the same reason.
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	var include []string
	for _, name := range names {
		value := values[name]
		switch name {
		case parameterIncludeField:
			// Several fields may share one parameter, separated by commas.
			for _, fields := range value {
				include = append(include, strings.Split(fields, ",")...)
			}
		case parameterLimit, parameterOffset:
			n, err := strconv.Atoi(value[len(value)-1])
			// A limit of 0 would ask for nothing, which no client means.
			if err != nil || n < 0 || (name == parameterLimit && n == 0) {
				return nil, page, false, refuse(http.StatusBadRequest,
					"%s=%s is not a number of results", name, value[len(value)-1])
			}
			if name == parameterLimit {
				page.limit = n
			} else {
				page.offset = n
			}
		case parameterFuzzyMatching:
			switch strings.ToLower(value[len(value)-1]) {
			case "true":
				fuzzy = true
			case "false":
			default:
				return nil, page, false, refuse(http.StatusBadRequest,
					"fuzzymatching=%s is neither true nor false", value[len(value)-1])
			}
		case parameterAccept:
			// Read with the Accept header, by jsonContentType.
		case parameterCharset:
			if !strings.EqualFold(value[len(value)-1], "utf-8") {
				return nil, page, false, refuse(http.StatusNotAcceptable,
					"search results are UTF-8; the request asks for %s", value[len(value)-1])
			}
		default:
			path, err := attributePath(name)
			if err != nil {
				return nil, page, false, err
			}
			key := attributeKey(path)
			if given[key] {
				return nil, page, false, refuse(http.StatusBadRequest,
					"%s is given more than once, or by the path", name)
			}
			given[key] = true
			matched, err := matchValue(name, path, value)
			if err != nil {
				return nil, page, false, err
			}
			setAttribute(identifier, path, matched)
		}
	}

	for _, field := range include {
		field = strings.TrimSpace(field)
		// "all" asks for every attribute the archive holds for the level.
		// The defaults are what it is known to hold.
		if field == "" || strings.EqualFold(field, "all") {
			continue
		}
		path, err := attributePath(field)
		if err != nil {
			return nil, page, false, err
		}
		setAttribute(identifier, path, "")
	}
	for _, t := range t.returned() {
		setAttribute(identifier, []tag.Tag{t}, "")
	}
	return identifier, page, fuzzy, nil
}

// attributePath reads an attribute as a search names it: a keyword or a tag
// of eight hexadecimal digits, or, for an attribute inside a sequence, the
// sequences that hold it and then the attribute, separated by dots
// (PS3.18 8.3.4.1).
func attributePath(name string) ([]tag.Tag, *requestError) {
	parts := strings.Split(name, ".")
	path := make([]tag.Tag, 0, len(parts))
	for i, part := range parts {
		t, ok := attributeTag(part)
		if !ok {
			return nil, refuse(http.StatusBadRequest,
				"%q is not a parameter of a search, nor an attribute by keyword or tag", part)
		}
		if i < len(parts)-1 && attributeVR(t) != dataelem.SQ {
			return nil, refuse(http.StatusBadRequest,
				"%s is not a sequence, so nothing can be named inside it", part)
		}
		path = append(path, t)
	}
	return path, nil
}

// attributeTag finds a standard attribute by keyword or by tag.
func attributeTag(name string) (tag.Tag, bool) {
	if t := tag.GlobalDictionary().GetByKeyword(name); t != 0 {
		return t, true
	}
	if len(name) != 8 {
		return 0, false
	}
	value, err := strconv.ParseUint(name, 16, 32)
	if err != nil {
		return 0, false
	}
	t := tag.Tag(value)
	// A tag the dictionary does not hold has no VR to match by.
	if tag.GlobalDictionary().Get(t) == nil {
		return 0, false
	}
	return t, true
}

// attributeVR is the dictionary's VR for a standard attribute. Where the
// dictionary gives two, as "US or SS", it is the first: an identifier holds
// no image to decide between them.
func attributeVR(t tag.Tag) dataelem.VR {
	vr, _, _ := strings.Cut(t.GetVR(), " ")
	return dataelem.VR(vr)
}

// attributeKey names an attribute path, however the request spelled it.
func attributeKey(path []tag.Tag) string {
	parts := make([]string, len(path))
	for i, t := range path {
		parts[i] = t.String()
	}
	return strings.Join(parts, ".")
}

// matchValue is the value to match an attribute against, from the values
// its parameter was given.
//
// A UID may be given several times, or as a list separated by commas: each
// is one of the UIDs to match (PS3.18 8.3.4.1, PS3.4 C.2.2.2.2), which an
// identifier separates with a backslash. Any other attribute is given once.
func matchValue(name string, path []tag.Tag, values []string) (string, *requestError) {
	if attributeVR(path[len(path)-1]) == dataelem.UI {
		var uids []string
		for _, value := range values {
			uids = append(uids, strings.Split(value, ",")...)
		}
		return strings.Join(uids, `\`), nil
	}
	if len(values) != 1 {
		return "", refuse(http.StatusBadRequest, "%s is given more than once", name)
	}
	return values[0], nil
}

// setAttribute puts an attribute in an identifier: with a value, to be
// matched, and without, to be returned. An attribute already there is left,
// so a value to match is not replaced by a request to return it.
//
// An attribute inside a sequence goes in the sequence's one item, which is
// how an identifier matches within a sequence (PS3.4 C.2.2.2.6).
func setAttribute(identifier *dataset.Dataset, path []tag.Tag, value string) {
	t := path[0]
	if len(path) == 1 {
		if identifier.Contains(t) {
			return
		}
		if attributeVR(t) == dataelem.SQ {
			_ = identifier.Add(dataelem.NewDataElement(t, dataelem.SQ, sequence.New()))
			return
		}
		_ = identifier.Add(dataelem.NewDataElement(t, attributeVR(t), value))
		return
	}

	seq, err := identifier.GetSequence(t)
	if err != nil || seq == nil {
		seq = sequence.New()
		_ = identifier.Add(dataelem.NewDataElement(t, dataelem.SQ, seq))
	}
	if seq.Length() == 0 {
		_ = seq.Append(dataset.NewDataset())
	}
	item, err := seq.Get(0)
	if err != nil {
		return
	}
	if itemDS, ok := item.(*dataset.Dataset); ok {
		setAttribute(itemDS, path[1:], value)
	}
}

// search answers a search request.
func (s *Server) search(w http.ResponseWriter, r *http.Request, target searchTarget) {
	contentType, refused := jsonContentType(r)
	if refused != nil {
		s.fail(w, refused)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		s.fail(w, refuse(http.StatusBadRequest, "the query string is not well formed"))
		return
	}
	identifier, page, fuzzy, refused := target.identifier(values)
	if refused != nil {
		s.fail(w, refused)
		return
	}

	matches, err := s.backend.Query(r.Context(), identifier)
	if err != nil {
		if r.Context().Err() != nil {
			// The client has gone, and there is nobody to answer.
			return
		}
		s.fail(w, err)
		return
	}

	if page.offset > len(matches) {
		page.offset = len(matches)
	}
	matches = matches[page.offset:]
	if page.limit > 0 && page.limit < len(matches) {
		matches = matches[:page.limit]
	}

	root := serviceRoot(r)
	results := make([]map[string]dataset.DICOMJSONElement, 0, len(matches))
	for _, match := range matches {
		found := searchResult(match)
		if url := retrieveURL(root, found, target.level); url != "" {
			_ = found.Add(dataelem.NewDataElement(tagRetrieveURL, dataelem.UR, url))
		}
		result, err := found.ToDICOMJSON()
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

	if fuzzy {
		// PS3.18 10.6.1.2.2: a server without fuzzy matching matches
		// literally and says so.
		w.Header().Set("Warning", `299 `+warningAgent(r)+
			` "The fuzzymatching parameter is not supported. Only literal matching has been performed."`)
	}
	if len(results) == 0 {
		// No match is a success with nothing in it (PS3.18 10.6.3.2).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// warningAgent names this server in a Warning header (RFC 7234 5.5): the
// host the request was sent to.
func warningAgent(r *http.Request) string {
	if r.Host == "" {
		return "-"
	}
	return r.Host
}

// searchResult is a match as a search returns it: what the archive knows.
//
// Query/Retrieve Level belongs to the C-FIND answer and not to the study,
// series or instance it describes. An attribute with no value is one the
// archive was asked for and does not hold: a C-FIND answer returns it empty,
// to show the key was understood, and here it is left out, so that a result
// does not claim a study has no accession number when it was never indexed.
func searchResult(match *dataset.Dataset) *dataset.Dataset {
	result := dataset.NewDataset()
	for _, elem := range match.GetAll() {
		t, ok := elem.Tag()
		if !ok || t == tagQueryRetrieveLevel || isEmpty(elem) {
			continue
		}
		_ = result.Add(elem)
	}
	return result
}

// isEmpty reports whether an element has no value, or is a sequence with no
// items.
func isEmpty(elem *dataelem.DataElement) bool {
	switch v := elem.GetValue().(type) {
	case nil:
		return true
	case *sequence.Sequence:
		return v == nil || v.Length() == 0
	case string:
		return v == ""
	case []byte:
		return len(v) == 0
	case []string:
		return len(v) == 0
	}
	return false
}

// serviceRoot is the URL this service was reached at: the scheme and host
// of the request, and whatever path the handler is mounted under.
//
// http.StripPrefix shortens the path a handler sees and leaves RequestURI
// as it was sent, so what was stripped is the part of one that the other
// no longer has. Behind a proxy this is the address the proxy used, which
// may not be the one a client can reach: a client is free to build the URL
// from the UIDs instead, and most do.
func serviceRoot(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	sent, _, _ := strings.Cut(r.RequestURI, "?")
	prefix := ""
	if seen := r.URL.EscapedPath(); strings.HasPrefix(sent, "/") && strings.HasSuffix(sent, seen) {
		prefix = strings.TrimSuffix(sent, seen)
	}
	return scheme + "://" + r.Host + prefix
}

// retrieveURL is where the study, series or instance a result describes is
// retrieved from (PS3.18 10.6.3.3), or nothing when the result does not
// carry the UIDs that name it.
func retrieveURL(root string, result *dataset.Dataset, level string) string {
	study := text(result, tagStudyInstanceUID)
	if study == "" {
		return ""
	}
	url := root + "/studies/" + study
	if level == levelStudy {
		return url
	}
	series := text(result, tagSeriesInstanceUID)
	if series == "" {
		return ""
	}
	url += "/series/" + series
	if level == levelSeries {
		return url
	}
	instance := text(result, tag.New(0x0008, 0x0018))
	if instance == "" {
		return ""
	}
	return url + "/instances/" + instance
}
