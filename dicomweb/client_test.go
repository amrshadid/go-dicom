package dicomweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The library spoke DIMSE and had no HTTP side, so an archive that offers
// DICOMweb and nothing else was out of reach (#244). The server in these
// tests answers as PS3.18 says a server does, and checks what it is sent.

var (
	tagSOPClass    = tag.New(0x0008, 0x0016)
	tagSOPInstance = tag.New(0x0008, 0x0018)
	tagStudyUID    = tag.New(0x0020, 0x000D)
	tagPatientName = tag.New(0x0010, 0x0010)
)

const ctClass = "1.2.840.10008.5.1.4.1.1.2"

// instance builds a small CT data set.
func instance(t *testing.T, uid string) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value string
	}{
		{tagSOPClass, dataelem.UI, ctClass},
		{tagSOPInstance, dataelem.UI, uid},
		{tagStudyUID, dataelem.UI, "1.2.3.100"},
		{tagPatientName, dataelem.PN, "Doe^Jane"},
	} {
		if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	return ds
}

func part10(t *testing.T, uid string) []byte {
	t.Helper()
	data, err := encodeFile(instance(t, uid))
	if err != nil {
		t.Fatalf("encoding the instance: %v", err)
	}
	return data
}

func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL + "/dicom-web")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

// TestSearchForStudies: the query goes out as the standard's parameters, and
// each object of the DICOM JSON array comes back as a data set.
func TestSearchForStudies(t *testing.T) {
	var got *http.Request
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/dicom+json")
		fmt.Fprint(w, `[
			{"0020000D":{"vr":"UI","Value":["1.2.3.100"]},"00100010":{"vr":"PN","Value":[{"Alphabetic":"Doe^Jane"}]}},
			{"0020000D":{"vr":"UI","Value":["1.2.3.200"]}}
		]`)
	})

	studies, err := client.SearchForStudies(ctx(t), Query{
		Match:         map[string]string{"PatientName": "Doe*", "00080020": "20240101-20241231"},
		IncludeFields: []string{"StudyDescription", "00081030"},
		Limit:         25, Offset: 50, FuzzyMatching: true,
	})
	if err != nil {
		t.Fatalf("SearchForStudies: %v", err)
	}
	if len(studies) != 2 {
		t.Fatalf("%d studies, want 2", len(studies))
	}
	if uid := text(studies[0], tagStudyUID); uid != "1.2.3.100" {
		t.Errorf("first Study Instance UID is %q", uid)
	}
	if name := text(studies[0], tagPatientName); !strings.HasPrefix(name, "Doe^Jane") {
		t.Errorf("Patient's Name is %q", name)
	}

	if got.URL.Path != "/dicom-web/studies" {
		t.Errorf("the request went to %s", got.URL.Path)
	}
	q := got.URL.Query()
	for key, want := range map[string]string{
		"PatientName": "Doe*", "00080020": "20240101-20241231",
		"limit": "25", "offset": "50", "fuzzymatching": "true",
	} {
		if q.Get(key) != want {
			t.Errorf("parameter %s is %q, want %q", key, q.Get(key), want)
		}
	}
	if fields := q["includefield"]; len(fields) != 2 || fields[0] != "StudyDescription" || fields[1] != "00081030" {
		t.Errorf("includefield is %v", fields)
	}
	if accept := got.Header.Get("Accept"); !strings.Contains(accept, "application/dicom+json") {
		t.Errorf("Accept is %q", accept)
	}
}

// TestSearchPaths: each level is searched under whichever of its parents is
// known, and across the whole server when none is.
func TestSearchPaths(t *testing.T) {
	var path string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		// No match is 204 with no body, not an empty array (PS3.18 10.6.3).
		w.WriteHeader(http.StatusNoContent)
	})
	for want, search := range map[string]func() ([]*dataset.Dataset, error){
		"/dicom-web/series":                func() ([]*dataset.Dataset, error) { return client.SearchForSeries(ctx(t), "", Query{}) },
		"/dicom-web/studies/1.2/series":    func() ([]*dataset.Dataset, error) { return client.SearchForSeries(ctx(t), "1.2", Query{}) },
		"/dicom-web/instances":             func() ([]*dataset.Dataset, error) { return client.SearchForInstances(ctx(t), "", "", Query{}) },
		"/dicom-web/studies/1.2/instances": func() ([]*dataset.Dataset, error) { return client.SearchForInstances(ctx(t), "1.2", "", Query{}) },
		"/dicom-web/studies/1.2/series/3.4/instances": func() ([]*dataset.Dataset, error) {
			return client.SearchForInstances(ctx(t), "1.2", "3.4", Query{})
		},
	} {
		found, err := search()
		if err != nil {
			t.Errorf("%s: %v", want, err)
		}
		if len(found) != 0 {
			t.Errorf("%s: %d results from a 204", want, len(found))
		}
		if path != want {
			t.Errorf("the request went to %s, want %s", path, want)
		}
	}
	// A series without its study names nothing a server can look up.
	if _, err := client.SearchForInstances(ctx(t), "", "3.4", Query{}); err == nil {
		t.Error("instances of a series with no study were searched for")
	}
}

// TestErrorCarriesWhatTheServerSaid: a status the request did not want is an
// error holding the code and the body, which is where a server says why.
func TestErrorCarriesWhatTheServerSaid(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "limit must not exceed 100", http.StatusBadRequest)
	})
	_, err := client.SearchForStudies(ctx(t), Query{Limit: 1000})
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("the error is %T (%v), want a *StatusError", err, err)
	}
	if status.StatusCode != 400 || !strings.Contains(status.Body, "limit must not exceed") {
		t.Errorf("status %d, body %q", status.StatusCode, status.Body)
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "limit must not exceed") {
		t.Errorf("the message %q leaves out the status or the reason", err)
	}
}

// multipartResponse writes parts as multipart/related; type="application/dicom".
func multipartResponse(w http.ResponseWriter, parts ...[]byte) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, part := range parts {
		pw, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/dicom"}})
		_, _ = pw.Write(part)
	}
	_ = mw.Close()
	// The boundary quoted, as servers send it and as RFC 2046 allows.
	w.Header().Set("Content-Type", `multipart/related; type="application/dicom"; boundary="`+mw.Boundary()+`"`)
	_, _ = w.Write(body.Bytes())
}

// TestRetrieveStudy: each part of the multipart response is a Part 10 file
// and comes back as its data set.
func TestRetrieveStudy(t *testing.T) {
	var got *http.Request
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		multipartResponse(w, part10(t, "1.2.3.1"), part10(t, "1.2.3.2"))
	})
	instances, err := client.RetrieveStudy(ctx(t), "1.2.3.100")
	if err != nil {
		t.Fatalf("RetrieveStudy: %v", err)
	}
	if len(instances) != 2 || text(instances[0], tagSOPInstance) != "1.2.3.1" || text(instances[1], tagSOPInstance) != "1.2.3.2" {
		t.Fatalf("retrieved %d instances", len(instances))
	}
	if got.URL.Path != "/dicom-web/studies/1.2.3.100" {
		t.Errorf("the request went to %s", got.URL.Path)
	}
	accept := got.Header.Get("Accept")
	mediaType, params, err := mime.ParseMediaType(accept)
	if err != nil || mediaType != "multipart/related" || params["type"] != "application/dicom" {
		t.Errorf("Accept is %q", accept)
	}
}

// TestRetrieveInstance: one instance, at its own path, and a server that
// answers with the file itself and no multipart wrapping is read as well.
func TestRetrieveInstance(t *testing.T) {
	var path string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/dicom")
		_, _ = w.Write(part10(t, "1.2.3.9"))
	})
	ds, err := client.RetrieveInstance(ctx(t), "1.2", "3.4", "1.2.3.9")
	if err != nil {
		t.Fatalf("RetrieveInstance: %v", err)
	}
	if text(ds, tagSOPInstance) != "1.2.3.9" {
		t.Errorf("retrieved instance %q", text(ds, tagSOPInstance))
	}
	if path != "/dicom-web/studies/1.2/series/3.4/instances/1.2.3.9" {
		t.Errorf("the request went to %s", path)
	}
	for _, args := range [][3]string{{"", "3.4", "5.6"}, {"1.2", "", "5.6"}, {"1.2", "3.4", ""}} {
		if _, err := client.RetrieveInstance(ctx(t), args[0], args[1], args[2]); err == nil {
			t.Errorf("an instance was retrieved with %v", args)
		}
	}
}

// TestRetrieveEachStopsWhenAsked: a caller working through a large study can
// stop, and its error is the one returned.
func TestRetrieveEachStopsWhenAsked(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		multipartResponse(w, part10(t, "1.2.3.1"), part10(t, "1.2.3.2"), part10(t, "1.2.3.3"))
	})
	stop := errors.New("enough")
	seen := 0
	err := client.RetrieveEach(ctx(t), "1.2.3.100", "", "", func(p Part) error {
		seen++
		if seen == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || seen != 2 {
		t.Errorf("saw %d parts and got %v", seen, err)
	}
}

// TestResponseSizeIsBounded: a server, or something answering in its place,
// cannot make the client hold whatever it likes.
func TestResponseSizeIsBounded(t *testing.T) {
	big := part10(t, "1.2.3.1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		multipartResponse(w, big, big, big, big)
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, WithMaxResponseBytes(int64(len(big))+200))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RetrieveStudy(ctx(t), "1.2.3.100"); err == nil {
		t.Error("a response over the limit was read to the end")
	}
}

// TestRetrieveMetadata: the instances of a study as DICOM JSON, without
// their bulk data.
func TestRetrieveMetadata(t *testing.T) {
	var got *http.Request
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/dicom+json")
		fmt.Fprint(w, `[{"00080018":{"vr":"UI","Value":["1.2.3.1"]},"7FE00010":{"vr":"OW","BulkDataURI":"http://x/bulk/1"}}]`)
	})
	metadata, err := client.RetrieveMetadata(ctx(t), "1.2", "3.4", "")
	if err != nil {
		t.Fatalf("RetrieveMetadata: %v", err)
	}
	if len(metadata) != 1 || text(metadata[0], tagSOPInstance) != "1.2.3.1" {
		t.Fatalf("metadata for %d instances", len(metadata))
	}
	if got.URL.Path != "/dicom-web/studies/1.2/series/3.4/metadata" {
		t.Errorf("the request went to %s", got.URL.Path)
	}
}

// storeResponse is what a server answers a STOW-RS request with: the
// instances it kept and the ones it did not (PS3.18 10.5.3, PS3.3 C.4.19).
const storeResponse = `{
	"00081190":{"vr":"UR","Value":["http://x/dicom-web/studies/1.2.3.100"]},
	"00081199":{"vr":"SQ","Value":[{
		"00081150":{"vr":"UI","Value":["1.2.840.10008.5.1.4.1.1.2"]},
		"00081155":{"vr":"UI","Value":["1.2.3.1"]},
		"00081190":{"vr":"UR","Value":["http://x/dicom-web/studies/1.2.3.100/series/9/instances/1.2.3.1"]}}]},
	"00081198":{"vr":"SQ","Value":[{
		"00081150":{"vr":"UI","Value":["1.2.840.10008.5.1.4.1.1.2"]},
		"00081155":{"vr":"UI","Value":["1.2.3.2"]},
		"00081197":{"vr":"US","Value":[272]}}]}
}`

// TestStore: the request is multipart/related a server can take apart into
// the files that were sent, and the response says what became of each.
func TestStore(t *testing.T) {
	files := [][]byte{part10(t, "1.2.3.1"), part10(t, "1.2.3.2")}
	var received [][]byte
	var path, method, accept string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, accept = r.URL.Path, r.Method, r.Header.Get("Accept")
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/related" || params["type"] != "application/dicom" {
			http.Error(w, "not multipart/related; type=application/dicom: "+r.Header.Get("Content-Type"), http.StatusUnsupportedMediaType)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if ct := part.Header.Get("Content-Type"); ct != "application/dicom" {
				http.Error(w, "part is "+ct, http.StatusUnsupportedMediaType)
				return
			}
			data, _ := io.ReadAll(part)
			received = append(received, data)
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		w.WriteHeader(http.StatusAccepted) // some stored, some not
		fmt.Fprint(w, storeResponse)
	})

	result, err := client.Store(ctx(t), "1.2.3.100", files...)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if method != http.MethodPost || path != "/dicom-web/studies/1.2.3.100" {
		t.Errorf("%s %s", method, path)
	}
	if !strings.Contains(accept, "application/dicom+json") {
		t.Errorf("Accept is %q", accept)
	}
	if len(received) != 2 || !bytes.Equal(received[0], files[0]) || !bytes.Equal(received[1], files[1]) {
		t.Fatalf("the server took %d parts out of the request, and they are not the files sent", len(received))
	}

	if len(result.Stored) != 1 || result.Stored[0].SOPInstanceUID != "1.2.3.1" ||
		!strings.HasSuffix(result.Stored[0].RetrieveURL, "/instances/1.2.3.1") {
		t.Errorf("stored: %+v", result.Stored)
	}
	if len(result.Failed) != 1 || result.Failed[0].SOPInstanceUID != "1.2.3.2" || result.Failed[0].FailureReason != 272 {
		t.Errorf("failed: %+v", result.Failed)
	}
	if result.RetrieveURL != "http://x/dicom-web/studies/1.2.3.100" {
		t.Errorf("study retrieve URL %q", result.RetrieveURL)
	}
}

// TestStoreThatStoresNothing: 409 means no instance was kept. It is an
// error, and the result still says which instances and why.
func TestStoreThatStoresNothing(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dicom-web/studies" {
			t.Errorf("a store with no study went to %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/dicom+json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"00081198":{"vr":"SQ","Value":[{"00081155":{"vr":"UI","Value":["1.2.3.1"]},"00081197":{"vr":"US","Value":[43264]}}]}}`)
	})
	result, err := client.StoreDatasets(ctx(t), "", instance(t, "1.2.3.1"))
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusConflict {
		t.Fatalf("the error is %v, want a 409", err)
	}
	if result == nil || len(result.Failed) != 1 || result.Failed[0].FailureReason != 43264 {
		t.Errorf("the result of a refused store is %+v", result)
	}
	if _, err := client.Store(ctx(t), ""); err == nil {
		t.Error("a store of nothing was sent")
	}
}

// TestAuthorization: credentials go on every request, in the form asked for.
func TestAuthorization(t *testing.T) {
	for name, c := range map[string]struct {
		option Option
		want   string
	}{
		"bearer": {WithBearerToken("abc.def"), "Bearer abc.def"},
		"basic":  {WithBasicAuth("user", "pass"), "Basic dXNlcjpwYXNz"},
	} {
		var auth, extra string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth, extra = r.Header.Get("Authorization"), r.Header.Get("X-Tenant")
			w.WriteHeader(http.StatusNoContent)
		}))
		client, err := NewClient(server.URL+"/", c.option, WithHeader("X-Tenant", "radiology"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.SearchForStudies(ctx(t), Query{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		server.Close()
		if auth != c.want || extra != "radiology" {
			t.Errorf("%s: Authorization %q, X-Tenant %q", name, auth, extra)
		}
	}
	for _, bad := range []string{"", "ftp://host/x", "host-only", "://nope"} {
		if _, err := NewClient(bad); err == nil {
			t.Errorf("a client was made for %q", bad)
		}
	}
}

// TestCancellation: a caller that stops waiting is not kept waiting.
func TestCancellation(t *testing.T) {
	release := make(chan struct{})
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	c, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := client.SearchForStudies(c, Query{}); err == nil {
		t.Error("a search the server never answered returned without an error")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the search took %v to give up", time.Since(start))
	}
}

// TestStoreRefusalItemizedUnderAnotherStatus: Orthanc answers 400, not 409
// or 202, when it refuses instances of a request, and itemizes them all the
// same. With many instances that account is far longer than the part of an
// error body kept for a message, and it was cut off there and lost.
func TestStoreRefusalItemizedUnderAnotherStatus(t *testing.T) {
	var failed []string
	for i := 0; i < 60; i++ {
		failed = append(failed, fmt.Sprintf(`{"00081150":{"vr":"UI","Value":["1.2.840.10008.5.1.4.1.1.7"]},`+
			`"00081155":{"vr":"UI","Value":["1.2.826.0.1.3680043.8.498.%d"]},"00081197":{"vr":"US","Value":[49152]}}`, i))
	}
	body := `{"00081198":{"vr":"SQ","Value":[` + strings.Join(failed, ",") + `]},` +
		`"00081199":{"vr":"SQ","Value":[{"00081155":{"vr":"UI","Value":["1.2.3.4"]}}]}}`
	if len(body) <= maxErrorBody {
		t.Fatalf("the answer is %d bytes, which does not exceed what an error keeps", len(body))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dicom+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Store(context.Background(), "", []byte("file"))
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusBadRequest {
		t.Fatalf("the error is %v, want the 400", err)
	}
	if len(status.Body) > maxErrorBody {
		t.Errorf("the error keeps %d bytes of the body", len(status.Body))
	}
	if result == nil || len(result.Failed) != 60 || len(result.Stored) != 1 {
		t.Fatalf("the result is %+v, want the 60 refused and the 1 kept", result)
	}
	if result.Failed[59].FailureReason != 0xC000 {
		t.Errorf("the reason is %04XH, want C000H", result.Failed[59].FailureReason)
	}
}

// A refusal that itemizes nothing has no result to give.
func TestStoreRefusalWithNoAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnsupportedMediaType)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	result, err := client.Store(context.Background(), "", []byte("file"))
	if err == nil || result != nil {
		t.Errorf("result %+v, error %v; want no result and an error", result, err)
	}
}
