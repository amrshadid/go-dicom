package dicomweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The server turns a search URL into the identifier a C-FIND would carry and
// hands it to the archive. These tests stand a recording archive behind it,
// so each one checks either what the archive was asked or what the client
// was told. The archive's own matching is tested where it lives, in
// dcmstore, and the two together in dcmstore's web tests.

// recorder is a Backend that keeps the identifier it was asked with and
// answers with what it was given.
type recorder struct {
	asked   *dataset.Dataset
	answers []*dataset.Dataset
	err     error
}

func (r *recorder) Query(_ context.Context, identifier *dataset.Dataset) ([]*dataset.Dataset, error) {
	r.asked = identifier
	return r.answers, r.err
}

// get sends one GET to a server over backend and returns the response with
// its body read.
func get(t *testing.T, backend Backend, target string, header ...string) (*http.Response, string) {
	t.Helper()
	return send(t, backend, http.MethodGet, target, header...)
}

func send(t *testing.T, backend Backend, method, target string, header ...string) (*http.Response, string) {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		request.Header.Set(header[i], header[i+1])
	}
	recorded := httptest.NewRecorder()
	NewServer(backend).ServeHTTP(recorded, request)
	response := recorded.Result()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(body)
}

// asked returns the value the identifier holds for a tag, and whether the
// tag is in it at all.
func asked(t *testing.T, identifier *dataset.Dataset, at tag.Tag) (string, bool) {
	t.Helper()
	if identifier == nil {
		t.Fatal("the archive was not asked")
	}
	elem, ok := identifier.Get(at)
	if !ok {
		return "", false
	}
	value, _ := elem.GetValue().(string)
	return value, true
}

var (
	tagStudyDate        = tag.New(0x0008, 0x0020)
	tagStudyDescription = tag.New(0x0008, 0x1030)
	tagModality         = tag.New(0x0008, 0x0060)
	tagSOPInstanceUID   = tag.New(0x0008, 0x0018)
	tagAccessionNumber  = tag.New(0x0008, 0x0050)
	tagRequestAttrs     = tag.New(0x0040, 0x0275)
	tagRequestedProcID  = tag.New(0x0040, 0x1001)
)

// TestSearchResourceNamesTheLevel checks each search resource of PS3.18
// 10.6.1: the level asked for, and the study and series the path names
// becoming keys to match.
func TestSearchResourceNamesTheLevel(t *testing.T) {
	cases := []struct {
		path                 string
		level, study, series string
	}{
		{"/studies", "STUDY", "", ""},
		{"/studies/", "STUDY", "", ""},
		{"/series", "SERIES", "", ""},
		{"/instances", "IMAGE", "", ""},
		{"/studies/1.2.3/series", "SERIES", "1.2.3", ""},
		{"/studies/1.2.3/instances", "IMAGE", "1.2.3", ""},
		{"/studies/1.2.3/series/1.2.3.4/instances", "IMAGE", "1.2.3", "1.2.3.4"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			backend := &recorder{}
			response, body := get(t, backend, c.path)
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("status %d: %s", response.StatusCode, body)
			}
			if level, _ := asked(t, backend.asked, tagQueryRetrieveLevel); level != c.level {
				t.Errorf("level %q, want %q", level, c.level)
			}
			if study, _ := asked(t, backend.asked, tagStudyInstanceUID); study != c.study {
				t.Errorf("study %q, want %q", study, c.study)
			}
			if series, _ := asked(t, backend.asked, tagSeriesInstanceUID); series != c.series {
				t.Errorf("series %q, want %q", series, c.series)
			}
		})
	}
}

// TestSearchParametersBecomeKeys checks that an attribute named by keyword
// or by tag is matched, that includefield asks for one without matching it,
// and that neither replaces the other.
func TestSearchParametersBecomeKeys(t *testing.T) {
	backend := &recorder{}
	response, body := get(t, backend,
		"/studies?PatientName=Doe*&00080020=20240101-20241231&includefield=StudyDescription,00080060"+
			"&includefield=PatientName")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	for at, want := range map[tag.Tag]string{
		tagPatientName:      "Doe*",
		tagStudyDate:        "20240101-20241231",
		tagStudyDescription: "",
		tagModality:         "",
	} {
		got, ok := asked(t, backend.asked, at)
		if !ok || got != want {
			t.Errorf("%s: asked %q (present %v), want %q", at, got, ok, want)
		}
	}
}

// TestSearchUIDList checks that a UID given several times, or as a comma
// separated list, is one list of UIDs to match (PS3.18 8.3.4.1).
func TestSearchUIDList(t *testing.T) {
	backend := &recorder{}
	get(t, backend, "/studies?StudyInstanceUID=1.2,1.3&StudyInstanceUID=1.4")
	if got, _ := asked(t, backend.asked, tagStudyInstanceUID); got != `1.2\1.3\1.4` {
		t.Errorf("asked %q", got)
	}
}

// TestSearchInsideASequence checks that Sequence.Attribute is matched inside
// the sequence's one item, which is how an identifier says it (PS3.4
// C.2.2.2.6).
func TestSearchInsideASequence(t *testing.T) {
	backend := &recorder{}
	response, body := get(t, backend, "/series?RequestAttributesSequence.RequestedProcedureID=RP7")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	seq, err := backend.asked.GetSequence(tagRequestAttrs)
	if err != nil || seq.Length() != 1 {
		t.Fatalf("the identifier's sequence: %v", err)
	}
	item, _ := seq.Get(0)
	if got, _ := asked(t, item.(*dataset.Dataset), tagRequestedProcID); got != "RP7" {
		t.Errorf("asked %q inside the sequence", got)
	}
}

// TestSearchReturnsItsLevelsAttributes checks the attributes asked for by
// default: those of the level, and those of a level above only when the
// path does not already name it (PS3.18 10.6.3.3).
func TestSearchReturnsItsLevelsAttributes(t *testing.T) {
	cases := []struct {
		path          string
		study, series bool
	}{
		{"/studies", true, false},
		{"/series", true, true},
		{"/studies/1.2/series", false, true},
		{"/instances", true, true},
		{"/studies/1.2/instances", false, true},
		{"/studies/1.2/series/1.2.3/instances", false, false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			backend := &recorder{}
			get(t, backend, c.path)
			if _, ok := asked(t, backend.asked, tagAccessionNumber); ok != c.study {
				t.Errorf("study attributes asked for: %v, want %v", ok, c.study)
			}
			if _, ok := asked(t, backend.asked, tagModality); ok != c.series {
				t.Errorf("series attributes asked for: %v, want %v", ok, c.series)
			}
			_, instance := asked(t, backend.asked, tagSOPInstanceUID)
			if want := strings.HasSuffix(c.path, "instances"); instance != want {
				t.Errorf("instance attributes asked for: %v, want %v", instance, want)
			}
		})
	}
}

// TestSearchRefusals checks each request the server will not answer, and
// that it is refused before the archive is asked anything.
func TestSearchRefusals(t *testing.T) {
	cases := []struct {
		name, method, target string
		header               []string
		status               int
	}{
		{"no such resource", "GET", "/patients", nil, http.StatusNotFound},
		{"a path that is too long", "GET", "/studies/1.2/series/1.3/instances/1.4", nil, http.StatusNotFound},
		{"a study that is not a UID", "GET", "/studies/abc/series", nil, http.StatusBadRequest},
		{"a study that is a way out", "GET", "/studies/../series", nil, http.StatusBadRequest},
		// An escaped slash is a slash by the time the path is read, so
		// this names a resource with more segments than any has.
		{"a study that hides a path", "GET", "/studies/..%2F..%2Fetc/series", nil, http.StatusNotFound},
		{"a UID with an empty component", "GET", "/studies/1..2/series", nil, http.StatusBadRequest},
		{"a UID that is too long", "GET", "/studies/" + strings.Repeat("1.", 40) + "1/series", nil, http.StatusBadRequest},
		{"another method", "DELETE", "/studies", nil, http.StatusMethodNotAllowed},
		{"an unknown attribute", "GET", "/studies?PatientNmae=Doe", nil, http.StatusBadRequest},
		{"a tag the dictionary lacks", "GET", "/studies?00091001=x", nil, http.StatusBadRequest},
		{"an attribute given twice", "GET", "/studies?PatientName=A&00100010=B", nil, http.StatusBadRequest},
		{"an attribute repeated", "GET", "/studies?PatientName=A&PatientName=B", nil, http.StatusBadRequest},
		{"the path's study asked for again", "GET", "/studies/1.2/series?StudyInstanceUID=1.3", nil, http.StatusBadRequest},
		{"inside something that is no sequence", "GET", "/studies?PatientName.PatientID=x", nil, http.StatusBadRequest},
		{"a limit that is no number", "GET", "/studies?limit=ten", nil, http.StatusBadRequest},
		{"a limit of nothing", "GET", "/studies?limit=0", nil, http.StatusBadRequest},
		{"a negative offset", "GET", "/studies?offset=-1", nil, http.StatusBadRequest},
		{"fuzzy matching that is neither", "GET", "/studies?fuzzymatching=maybe", nil, http.StatusBadRequest},
		{"an unknown field to include", "GET", "/studies?includefield=Nothing", nil, http.StatusBadRequest},
		{"XML", "GET", "/studies", []string{"Accept", `multipart/related; type="application/dicom+xml"`}, http.StatusNotAcceptable},
		{"JSON refused", "GET", "/studies", []string{"Accept", "application/dicom+json;q=0, text/html"}, http.StatusNotAcceptable},
		{"another character set", "GET", "/studies?charset=iso-8859-1", nil, http.StatusNotAcceptable},
		{"a query string that is not one", "GET", "/studies?PatientName=%zz", nil, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			backend := &recorder{}
			response, body := send(t, backend, c.method, c.target, c.header...)
			if response.StatusCode != c.status {
				t.Errorf("status %d, want %d: %s", response.StatusCode, c.status, body)
			}
			if backend.asked != nil {
				t.Error("the archive was asked")
			}
			if strings.TrimSpace(body) == "" {
				t.Error("the refusal does not say why")
			}
			if c.status == http.StatusMethodNotAllowed && response.Header.Get("Allow") != "GET" {
				t.Errorf("Allow is %q", response.Header.Get("Allow"))
			}
		})
	}
}

// study is a C-FIND answer at STUDY level, as an archive returns one: its
// level, what it knows, and an attribute it was asked for and does not hold.
func study(uid, name string) *dataset.Dataset {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tagQueryRetrieveLevel, dataelem.CS, "STUDY"))
	_ = ds.Add(dataelem.NewDataElement(tagStudyInstanceUID, dataelem.UI, uid))
	_ = ds.Add(dataelem.NewDataElement(tagPatientName, dataelem.PN, name))
	_ = ds.Add(dataelem.NewDataElement(tagAccessionNumber, dataelem.SH, ""))
	_ = ds.Add(dataelem.NewDataElement(tagRequestAttrs, dataelem.SQ, sequence.New()))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x1208), dataelem.IS, "3"))
	return ds
}

// decode reads a search answer as the JSON it is.
func decode(t *testing.T, body string) []map[string]map[string]any {
	t.Helper()
	var results []map[string]map[string]any
	if err := json.Unmarshal([]byte(body), &results); err != nil {
		t.Fatalf("the answer is not a JSON array of objects: %v\n%s", err, body)
	}
	return results
}

// TestSearchAnswer checks what a match looks like on the wire: DICOM JSON,
// the values the archive gave, and neither the C-FIND level nor an
// attribute the archive holds nothing for.
func TestSearchAnswer(t *testing.T) {
	backend := &recorder{answers: []*dataset.Dataset{study("1.2.1", "Doe^John"), study("1.2.2", "Buc^Jérôme")}}
	response, body := get(t, backend, "/studies")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Type"); got != "application/dicom+json" {
		t.Errorf("Content-Type is %q", got)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q", got)
	}

	results := decode(t, body)
	if len(results) != 2 {
		t.Fatalf("%d results", len(results))
	}
	first := results[0]
	if got := first["0020000D"]["Value"].([]any)[0]; got != "1.2.1" {
		t.Errorf("study UID %v", got)
	}
	if got := first["00100010"]["Value"].([]any)[0].(map[string]any)["Alphabetic"]; got != "Doe^John" {
		t.Errorf("name %v", got)
	}
	// IS is a JSON number (PS3.18 F.2.3).
	if got := first["00201208"]["Value"].([]any)[0]; got != float64(3) {
		t.Errorf("number of instances %v (%T)", got, got)
	}
	if got := results[1]["00100010"]["Value"].([]any)[0].(map[string]any)["Alphabetic"]; got != "Buc^Jérôme" {
		t.Errorf("name %v", got)
	}
	for _, absent := range []string{"00080052", "00080050", "00400275"} {
		if _, ok := first[absent]; ok {
			t.Errorf("the result carries %s", absent)
		}
	}
}

// TestSearchWithNoMatch checks that nothing found is 204 with no body
// (PS3.18 10.6.3.2), not an empty list under 200.
func TestSearchWithNoMatch(t *testing.T) {
	response, body := get(t, &recorder{}, "/studies?PatientName=Nobody")
	if response.StatusCode != http.StatusNoContent || body != "" {
		t.Errorf("status %d, body %q", response.StatusCode, body)
	}
}

// TestSearchPaging checks offset and limit: the part of the results asked
// for, in the archive's order, and 204 past the end.
func TestSearchPaging(t *testing.T) {
	answers := []*dataset.Dataset{study("1.1", "A"), study("1.2", "B"), study("1.3", "C"), study("1.4", "D")}
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"1.1", "1.2", "1.3", "1.4"}},
		{"?limit=2", []string{"1.1", "1.2"}},
		{"?offset=1&limit=2", []string{"1.2", "1.3"}},
		{"?offset=3", []string{"1.4"}},
		{"?offset=3&limit=10", []string{"1.4"}},
		{"?offset=4", nil},
		{"?offset=40", nil},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			response, body := get(t, &recorder{answers: answers}, "/studies"+c.query)
			if c.want == nil {
				if response.StatusCode != http.StatusNoContent {
					t.Errorf("status %d, want 204", response.StatusCode)
				}
				return
			}
			var got []string
			for _, result := range decode(t, body) {
				got = append(got, result["0020000D"]["Value"].([]any)[0].(string))
			}
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestSearchMediaType checks the type of the answer against what the
// request accepts: DICOM JSON unless plain JSON is all it takes.
func TestSearchMediaType(t *testing.T) {
	answers := []*dataset.Dataset{study("1.1", "A")}
	cases := []struct {
		name, target, accept, want string
	}{
		{"nothing said", "/studies", "", "application/dicom+json"},
		{"anything", "/studies", "*/*", "application/dicom+json"},
		{"DICOM JSON", "/studies", "application/dicom+json", "application/dicom+json"},
		{"plain JSON only", "/studies", "application/json", "application/json"},
		{"both", "/studies", "application/json, application/dicom+json;q=0.5", "application/dicom+json"},
		{"a browser", "/studies", "text/html,application/xhtml+xml,*/*;q=0.8", "application/dicom+json"},
		{"the accept parameter", "/studies?accept=application/json", "text/html", "application/json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var header []string
			if c.accept != "" {
				header = []string{"Accept", c.accept}
			}
			response, body := get(t, &recorder{answers: answers}, c.target, header...)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", response.StatusCode, body)
			}
			if got := response.Header.Get("Content-Type"); got != c.want {
				t.Errorf("Content-Type %q, want %q", got, c.want)
			}
		})
	}
}

// TestFuzzyMatchingIsDeclined checks that a request for fuzzy matching is
// answered by literal matching and a warning that says so (PS3.18
// 10.6.1.2.2), and that a request without it carries no warning.
func TestFuzzyMatchingIsDeclined(t *testing.T) {
	answers := []*dataset.Dataset{study("1.1", "A")}
	response, _ := get(t, &recorder{answers: answers}, "/studies?fuzzymatching=true")
	warning := response.Header.Get("Warning")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(warning, "299 ") ||
		!strings.Contains(warning, "fuzzymatching") {
		t.Errorf("status %d, Warning %q", response.StatusCode, warning)
	}
	response, _ = get(t, &recorder{answers: answers}, "/studies?fuzzymatching=false")
	if got := response.Header.Get("Warning"); got != "" {
		t.Errorf("Warning %q without fuzzy matching", got)
	}
}

// TestArchiveFailureIsNotDescribed checks that an error from the archive is
// a 500 that does not repeat it: the error may name a file or a directory.
func TestArchiveFailureIsNotDescribed(t *testing.T) {
	backend := &recorder{err: errors.New("open /srv/archive/1.2.3/secret.dcm: permission denied")}
	response, body := get(t, backend, "/studies")
	if response.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d", response.StatusCode)
	}
	if strings.Contains(body, "/srv") || strings.Contains(body, "secret") {
		t.Errorf("the answer repeats the archive's error: %s", body)
	}
}

// TestClientSearchesServer checks the two halves of this package against
// each other, under the prefix a service is mounted at: what the client
// asks is what the archive is asked, and what the archive answers is what
// the client returns.
func TestClientSearchesServer(t *testing.T) {
	backend := &recorder{answers: []*dataset.Dataset{study("1.2.1", "Doe^John")}}
	service := httptest.NewServer(http.StripPrefix("/dicom-web", NewServer(backend)))
	defer service.Close()

	client, err := NewClient(service.URL + "/dicom-web")
	if err != nil {
		t.Fatal(err)
	}
	results, err := client.SearchForSeries(context.Background(), "1.2.1", Query{
		Match:         map[string]string{"Modality": "CT"},
		IncludeFields: []string{"SeriesDescription"},
		Limit:         5,
	})
	if err != nil {
		t.Fatal(err)
	}

	if level, _ := asked(t, backend.asked, tagQueryRetrieveLevel); level != "SERIES" {
		t.Errorf("level %q", level)
	}
	if study, _ := asked(t, backend.asked, tagStudyInstanceUID); study != "1.2.1" {
		t.Errorf("study %q", study)
	}
	if modality, _ := asked(t, backend.asked, tagModality); modality != "CT" {
		t.Errorf("modality %q", modality)
	}
	if len(results) != 1 {
		t.Fatalf("%d results", len(results))
	}
	if name := text(results[0], tagPatientName); name != "Doe^John" {
		t.Errorf("name %q", name)
	}
}
