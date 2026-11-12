package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	webCTClass = "1.2.840.10008.5.1.4.1.1.2"
	webLE      = "1.2.840.10008.1.2.1"
)

// webStudies is a QIDO-RS answer of two studies.
const webStudies = `[
 {"0020000D":{"vr":"UI","Value":["1.2.3.1"]},"00100010":{"vr":"PN","Value":[{"Alphabetic":"Doe^Jane"}]},
  "00201208":{"vr":"IS","Value":[12]}},
 {"0020000D":{"vr":"UI","Value":["1.2.3.2"]},"00100010":{"vr":"PN","Value":[{"Alphabetic":"Doe^John"}]}}
]`

func TestQidoSearchesAndPrintsWhatItFinds(t *testing.T) {
	var path, query, auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, auth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = io.WriteString(w, webStudies)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := &QidoCommand{out: &out}
	err := cmd.Execute([]string{"-match", "PatientName=Doe*", "-limit", "5", "-token", "secret", server.URL + "/dicom-web"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/dicom-web/studies" {
		t.Errorf("searched %q, want the studies resource", path)
	}
	if !strings.Contains(query, "PatientName=Doe%2A") && !strings.Contains(query, "PatientName=Doe*") {
		t.Errorf("the query %q does not carry the match", query)
	}
	if !strings.Contains(query, "limit=5") {
		t.Errorf("the query %q does not carry the limit", query)
	}
	if auth != "Bearer secret" {
		t.Errorf("Authorization = %q, want the bearer token", auth)
	}
	text := out.String()
	for _, want := range []string{"1.2.3.1", "1.2.3.2", "Doe^Jane", "Patient's Name", "Found 2 result(s)"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output lacks %q:\n%s", want, text)
		}
	}
}

func TestQidoSearchesTheLevelAsked(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-level", "series"}, "/series"},
		{[]string{"-level", "series", "-study", "1.2.3"}, "/studies/1.2.3/series"},
		{[]string{"-level", "instance", "-study", "1.2.3", "-series", "4.5"}, "/studies/1.2.3/series/4.5/instances"},
	} {
		var path string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		var out bytes.Buffer
		cmd := &QidoCommand{out: &out}
		if err := cmd.Execute(append(tc.args, server.URL)); err != nil {
			t.Errorf("%v: %v", tc.args, err)
		}
		server.Close()
		if path != tc.want {
			t.Errorf("%v searched %q, want %q", tc.args, path, tc.want)
		}
		if !strings.Contains(out.String(), "Found 0 result(s)") {
			t.Errorf("%v: no match is not reported as none:\n%s", tc.args, out.String())
		}
	}
}

func TestQidoPrintsDICOMJSONWhenAsked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, webStudies)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := &QidoCommand{out: &out}
	if err := cmd.Execute([]string{"-json", server.URL}); err != nil {
		t.Fatal(err)
	}
	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("-json did not print JSON alone: %v\n%s", err, out.String())
	}
	if len(results) != 2 || results[0]["0020000D"] == nil {
		t.Errorf("the JSON is not the two studies: %s", out.String())
	}
}

func TestQidoRefusesWhatItCannotAsk(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-match", "PatientName", "http://example.invalid"},
		{"-level", "patient", "http://example.invalid"},
		{"-level", "study", "-study", "1.2", "http://example.invalid"},
		{"ftp://example.invalid"},
	} {
		cmd := &QidoCommand{out: io.Discard}
		if err := cmd.Execute(args); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

// multipartOf answers with files as a WADO-RS response.
func multipartOf(w http.ResponseWriter, files ...[]byte) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		part, _ := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/dicom"}})
		_, _ = part.Write(file)
	}
	_ = writer.Close()
	w.Header().Set("Content-Type", `multipart/related; type="application/dicom"; boundary=`+writer.Boundary())
	_, _ = w.Write(body.Bytes())
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWadoWritesEachInstanceUnderItsUID(t *testing.T) {
	source := t.TempDir()
	first := readFile(t, writeInstance(t, source, "a", webCTClass, "1.2.3.10", webLE, []byte{1, 2, 3, 4}))
	second := readFile(t, writeInstance(t, source, "b", webCTClass, "1.2.3.11", webLE, []byte{5, 6, 7, 8}))

	var path, accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, accept = r.URL.Path, r.Header.Get("Accept")
		multipartOf(w, first, second)
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "study")
	var out bytes.Buffer
	cmd := &WadoCommand{out: &out}
	if err := cmd.Execute([]string{"-study", "1.2.3", server.URL, output}); err != nil {
		t.Fatal(err)
	}
	if path != "/studies/1.2.3" {
		t.Errorf("retrieved %q, want the study", path)
	}
	if !strings.Contains(accept, "application/dicom") {
		t.Errorf("Accept = %q, which does not ask for DICOM files", accept)
	}
	if got := readFile(t, filepath.Join(output, "1.2.3.10.dcm")); !bytes.Equal(got, first) {
		t.Error("the first instance was not written as it was sent")
	}
	if got := readFile(t, filepath.Join(output, "1.2.3.11.dcm")); !bytes.Equal(got, second) {
		t.Error("the second instance was not written as it was sent")
	}
	if !strings.Contains(out.String(), "Retrieved 2 instance(s)") {
		t.Errorf("the count is not reported:\n%s", out.String())
	}
}

// A server chooses the SOP Instance UID a file is named after, so one that
// is not a UID must not choose where the file goes.
func TestWadoDoesNotLetAServerNameAFileOutsideTheOutput(t *testing.T) {
	source := t.TempDir()
	evil := readFile(t, writeInstance(t, source, "a", webCTClass, "../../escaped", webLE, nil))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		multipartOf(w, evil)
	}))
	defer server.Close()

	root := t.TempDir()
	output := filepath.Join(root, "deep", "er", "out")
	cmd := &WadoCommand{out: io.Discard}
	if err := cmd.Execute([]string{"-study", "1.2.3", server.URL, output}); err != nil {
		t.Fatal(err)
	}
	var written []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			written = append(written, path)
		}
		return nil
	})
	if len(written) != 1 || filepath.Dir(written[0]) != output {
		t.Errorf("the file went to %v, want one file inside %s", written, output)
	}
}

func TestWadoPrintsMetadataWithoutDownloading(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = io.WriteString(w, `[{"00080018":{"vr":"UI","Value":["1.2.3.10"]}}]`)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := &WadoCommand{out: &out}
	if err := cmd.Execute([]string{"-study", "1.2.3", "-series", "4.5", "-metadata", server.URL}); err != nil {
		t.Fatal(err)
	}
	if path != "/studies/1.2.3/series/4.5/metadata" {
		t.Errorf("asked for %q, want the series' metadata", path)
	}
	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil || len(results) != 1 {
		t.Errorf("the metadata was not printed as JSON alone: %v\n%s", err, out.String())
	}
}

func TestWadoNeedsAStudyAndSomewhereToWrite(t *testing.T) {
	for _, args := range [][]string{
		{"http://example.invalid", "out"},
		{"-study", "1.2", "http://example.invalid"},
		{"-study", "1.2", "-instance", "9", "http://example.invalid", "out"},
	} {
		cmd := &WadoCommand{out: io.Discard}
		if err := cmd.Execute(args); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

// storeAnswer is a STOW-RS answer that kept one instance and refused another.
func storeAnswer(stored, failed string) string {
	answer := `{"00081199":{"vr":"SQ","Value":[{"00081150":{"vr":"UI","Value":["` + webCTClass +
		`"]},"00081155":{"vr":"UI","Value":["` + stored + `"]}}]}`
	if failed != "" {
		answer += `,"00081198":{"vr":"SQ","Value":[{"00081150":{"vr":"UI","Value":["` + webCTClass +
			`"]},"00081155":{"vr":"UI","Value":["` + failed + `"]},"00081197":{"vr":"US","Value":[272]}}]}`
	}
	return answer + "}"
}

// partsOf counts the parts of a STOW-RS request.
func partsOf(t *testing.T, r *http.Request) [][]byte {
	t.Helper()
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Errorf("the request's Content-Type: %v", err)
		return nil
	}
	var parts [][]byte
	reader := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := reader.NextRawPart()
		if err != nil {
			return parts
		}
		data, _ := io.ReadAll(part)
		parts = append(parts, data)
	}
}

func TestStowSendsTheFilesOfADirectory(t *testing.T) {
	dir := t.TempDir()
	first := readFile(t, writeInstance(t, dir, "a.dcm", webCTClass, "1.2.3.10", webLE, []byte{1, 2}))
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	writeInstance(t, filepath.Join(dir, "sub"), "b.dcm", webCTClass, "1.2.3.11", webLE, []byte{3, 4})
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	var path string
	var parts [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		parts = partsOf(t, r)
		w.Header().Set("Content-Type", "application/dicom+json")
		_, _ = io.WriteString(w, `{"00081199":{"vr":"SQ","Value":[`+
			`{"00081155":{"vr":"UI","Value":["1.2.3.10"]}},{"00081155":{"vr":"UI","Value":["1.2.3.11"]}}]}}`)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := &StowCommand{out: &out}
	if err := cmd.Execute([]string{server.URL, dir}); err != nil {
		t.Fatal(err)
	}
	if path != "/studies" {
		t.Errorf("stored to %q, want the studies resource", path)
	}
	if len(parts) != 2 {
		t.Fatalf("%d parts were sent, want the two DICOM files and not the text file", len(parts))
	}
	if !bytes.Equal(parts[0], first) {
		t.Error("the first file was not sent as it is on disk")
	}
	if !strings.Contains(out.String(), "Stored 2 instance(s)") {
		t.Errorf("the count is not reported:\n%s", out.String())
	}
}

func TestStowFailsWhenTheServerRefusesAnInstance(t *testing.T) {
	dir := t.TempDir()
	a := writeInstance(t, dir, "a.dcm", webCTClass, "1.2.3.10", webLE, nil)
	b := writeInstance(t, dir, "b.dcm", webCTClass, "1.2.3.11", webLE, nil)

	for _, status := range []int{http.StatusAccepted, http.StatusConflict} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/dicom+json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, storeAnswer("1.2.3.10", "1.2.3.11"))
		}))
		var out bytes.Buffer
		cmd := &StowCommand{out: &out}
		err := cmd.Execute([]string{"-study", "1.2.3", server.URL, a, b})
		server.Close()
		if err == nil {
			t.Errorf("%d: a refused instance did not fail the command", status)
		}
		text := out.String() + fmt.Sprint(err)
		for _, want := range []string{"1.2.3.11", "0110"} {
			if !strings.Contains(text, want) {
				t.Errorf("%d: the refusal does not name %q:\n%s", status, want, text)
			}
		}
	}
}

func TestStowSplitsALargeRunOverSeveralRequests(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		writeInstance(t, dir, fmt.Sprintf("%d.dcm", i), webCTClass, fmt.Sprintf("1.2.3.%d", i), webLE, nil)
	}
	var requests, total int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		total += len(partsOf(t, r))
	}))
	defer server.Close()

	cmd := &StowCommand{out: io.Discard, batchFiles: 2}
	if err := cmd.Execute([]string{server.URL, dir}); err != nil {
		t.Fatal(err)
	}
	if requests != 3 || total != 5 {
		t.Errorf("%d files went in %d requests, want 5 in 3", total, requests)
	}
}

func TestStowWithNothingToSendIsAnError(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not an image"), 0o644)
	cmd := &StowCommand{out: io.Discard}
	if err := cmd.Execute([]string{"http://example.invalid", dir}); err == nil {
		t.Error("a directory with no DICOM file was accepted")
	}
}
