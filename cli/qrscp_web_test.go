package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/dicomweb"
	"github.com/amrshadid/go-dicom/tag"
)

// qrscp -web serves the archive over HTTP. What these tests guard is who
// can reach it: a port alone stays on this machine, an address beyond it
// needs a token, and a request without the token learns nothing.

// TestWebAddressStaysOnThisMachineUnlessTold checks how -web is read. A
// bare port, with or without a colon, is this machine only: net.Listen
// reads ":8080" as every interface, and an archive must not be on the
// network by the typing of a colon.
func TestWebAddressStaysOnThisMachineUnlessTold(t *testing.T) {
	cases := []struct {
		spec    string
		address string
		local   bool
		bad     bool
	}{
		{"8080", "127.0.0.1:8080", true, false},
		{":8080", "127.0.0.1:8080", true, false},
		{" 8080 ", "127.0.0.1:8080", true, false},
		{"127.0.0.1:8080", "127.0.0.1:8080", true, false},
		{"localhost:8080", "localhost:8080", true, false},
		{"[::1]:8080", "[::1]:8080", true, false},
		{"0.0.0.0:8080", "0.0.0.0:8080", false, false},
		{"[::]:8080", "[::]:8080", false, false},
		{"192.168.1.5:8080", "192.168.1.5:8080", false, false},
		{"pacs.example.org:8080", "pacs.example.org:8080", false, false},
		{"", "", false, true},
		{"web", "", false, true},
		{"0", "", false, true},
		{"70000", "", false, true},
		{"localhost:http", "", false, true},
		{"localhost", "", false, true},
	}
	for _, c := range cases {
		address, local, err := webListenAddress(c.spec)
		if c.bad {
			if err == nil {
				t.Errorf("-web %q was read as %s", c.spec, address)
			}
			continue
		}
		if err != nil || address != c.address || local != c.local {
			t.Errorf("-web %q: %s local=%v (%v), want %s local=%v", c.spec, address, local, err, c.address, c.local)
		}
	}
}

// TestWebBeyondThisMachineNeedsAToken checks that an address other machines
// can reach is refused without a token, and taken with one from the option
// or from the environment.
func TestWebBeyondThisMachineNeedsAToken(t *testing.T) {
	t.Setenv(webTokenEnv, "")
	open := webServeFlags{address: "0.0.0.0:8080"}
	if _, _, err := open.resolve(); err == nil || !strings.Contains(err.Error(), "-web-token") {
		t.Errorf("an open archive on every interface: %v", err)
	}
	local := webServeFlags{address: "8080"}
	if _, token, err := local.resolve(); err != nil || token != "" {
		t.Errorf("this machine only, no token: %q, %v", token, err)
	}
	guarded := webServeFlags{address: "0.0.0.0:8080", token: "s3cret"}
	if _, token, err := guarded.resolve(); err != nil || token != "s3cret" {
		t.Errorf("with -web-token: %q, %v", token, err)
	}
	t.Setenv(webTokenEnv, "from-env")
	if _, token, err := open.resolve(); err != nil || token != "from-env" {
		t.Errorf("with %s: %q, %v", webTokenEnv, token, err)
	}
}

// webStore is a store holding one study.
func webStore(t *testing.T) *dcmstore.Store {
	t.Helper()
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ds := dataset.NewDataset()
	for _, e := range []struct {
		tag   tag.Tag
		vr    dataelem.VR
		value string
	}{
		{tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.2"},
		{tag.New(0x0008, 0x0018), dataelem.UI, "1.2.3.1.1"},
		{tag.New(0x0010, 0x0010), dataelem.PN, "SECRET^NAME"},
		{tag.New(0x0010, 0x0020), dataelem.LO, "P1"},
		{tag.New(0x0020, 0x000D), dataelem.UI, "1.2.3"},
		{tag.New(0x0020, 0x000E), dataelem.UI, "1.2.3.1"},
		{tag.New(0x0008, 0x0060), dataelem.CS, "CT"},
	} {
		_ = ds.Add(dataelem.NewDataElement(e.tag, e.vr, e.value))
	}
	if _, err := store.Store(context.Background(), ds); err != nil {
		t.Fatal(err)
	}
	return store
}

func webGet(t *testing.T, handler http.Handler, target, authorization string) (int, string, http.Header) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, request)
	body, _ := io.ReadAll(recorded.Result().Body)
	return recorded.Code, string(body), recorded.Result().Header
}

// TestWebTokenGuardsEveryRequest checks the token: without it, or with
// another, a request is 401 and is told nothing the archive holds; with it,
// the search is answered.
func TestWebTokenGuardsEveryRequest(t *testing.T) {
	handler := webHandler(webStore(t), "s3cret", nil)
	cases := []struct {
		name, authorization string
		status              int
	}{
		{"no token", "", http.StatusUnauthorized},
		{"another token", "Bearer guess", http.StatusUnauthorized},
		{"a prefix of the token", "Bearer s3cre", http.StatusUnauthorized},
		{"the token under another scheme", "Basic s3cret", http.StatusUnauthorized},
		{"the token", "Bearer s3cret", http.StatusOK},
		{"the scheme in another case", "bearer s3cret", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body, header := webGet(t, handler, webPrefix+"/studies", c.authorization)
			if status != c.status {
				t.Fatalf("status %d, want %d", status, c.status)
			}
			if status == http.StatusUnauthorized {
				if strings.Contains(body, "SECRET") {
					t.Error("a refused request was shown a patient's name")
				}
				if !strings.HasPrefix(header.Get("WWW-Authenticate"), "Bearer") {
					t.Errorf("WWW-Authenticate is %q", header.Get("WWW-Authenticate"))
				}
			} else if !strings.Contains(body, "SECRET^NAME") {
				t.Errorf("the search did not return the study: %s", body)
			}
		})
	}
}

// TestWebServesUnderItsPrefixOnly checks that the service is under
// /dicom-web and nowhere else, and that with no token set it answers.
func TestWebServesUnderItsPrefixOnly(t *testing.T) {
	handler := webHandler(webStore(t), "", nil)
	for target, want := range map[string]int{
		webPrefix + "/studies":         http.StatusOK,
		webPrefix + "/studies/":        http.StatusOK,
		webPrefix + "/series":          http.StatusOK,
		"/studies":                     http.StatusNotFound,
		"/":                            http.StatusNotFound,
		webPrefix + "x/studies":        http.StatusNotFound,
		webPrefix:                      http.StatusNotFound,
		webPrefix + "/patients":        http.StatusNotFound,
		webPrefix + "/studies?limit=x": http.StatusBadRequest,
	} {
		if status, body, _ := webGet(t, handler, target, ""); status != want {
			t.Errorf("%s: status %d, want %d: %s", target, status, want, body)
		}
	}
}

// TestWebReportNamesNoPatient checks the line printed for a request: the
// method, the path and the status, and not the query, which is where a
// search carries a patient's name.
func TestWebReportNamesNoPatient(t *testing.T) {
	var lines []string
	handler := webHandler(webStore(t), "", func(line string) { lines = append(lines, line) })
	webGet(t, handler, webPrefix+"/studies?PatientName=SECRET*", "")
	if len(lines) != 1 {
		t.Fatalf("%d lines reported", len(lines))
	}
	if strings.Contains(lines[0], "SECRET") || strings.Contains(lines[0], "?") {
		t.Errorf("the line carries the query: %s", lines[0])
	}
	if !strings.Contains(lines[0], "GET "+webPrefix+"/studies") || !strings.HasSuffix(lines[0], "200") {
		t.Errorf("the line is %q", lines[0])
	}
}

// TestWebServiceIsSearchedByTheClient checks the service as it runs: a real
// listener on a port the system chose, searched with the library's client
// sending the token, and stopped.
func TestWebServiceIsSearchedByTheClient(t *testing.T) {
	url, stop, err := serveWeb("127.0.0.1:0", "s3cret", webStore(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, webPrefix) {
		t.Errorf("the service URL is %s", url)
	}

	client, err := dicomweb.NewClient(url, dicomweb.WithBearerToken("s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	studies, err := client.SearchForStudies(context.Background(),
		dicomweb.Query{Match: map[string]string{"PatientName": "secret*"}})
	if err != nil || len(studies) != 1 {
		t.Fatalf("%d studies, %v", len(studies), err)
	}

	stranger, _ := dicomweb.NewClient(url)
	if _, err := stranger.SearchForStudies(context.Background(), dicomweb.Query{}); err == nil {
		t.Error("a client with no token was answered")
	}

	stop()
	if _, err := client.SearchForStudies(context.Background(), dicomweb.Query{}); err == nil {
		t.Error("the service still answers after it was stopped")
	}

	// A second service on the port just used by a running one is an error
	// at once, and not a line printed after the command says it is serving.
	url, stop, err = serveWeb("127.0.0.1:0", "", webStore(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	taken := strings.TrimSuffix(strings.TrimPrefix(url, "http://"), webPrefix)
	if _, _, err := serveWeb(taken, "", webStore(t), nil, nil); err == nil {
		t.Error("a port in use was taken twice")
	}
}
