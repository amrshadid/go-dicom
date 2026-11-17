package dicomweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// A store request is several instances, and each is kept or refused on its
// own. These tests check the three answers PS3.18 10.5.3 has for that, and
// that what the archive was given is what was sent.

// post sends files as one store request and returns the status and the
// answer, decoded when it is JSON.
func post(t *testing.T, server http.Handler, target string, files ...[]byte) (int, map[string]map[string]any, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", "application/dicom")
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(file)
	}
	_ = writer.Close()
	return postBody(t, server, target,
		`multipart/related; type="application/dicom"; boundary=`+writer.Boundary(), body.Bytes())
}

func postBody(t *testing.T, server http.Handler, target, contentType string, body []byte) (int, map[string]map[string]any, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorded := httptest.NewRecorder()
	server.ServeHTTP(recorded, request)
	raw, _ := io.ReadAll(recorded.Result().Body)
	var answer map[string]map[string]any
	if strings.HasPrefix(recorded.Result().Header.Get("Content-Type"), "application/dicom+json") {
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatalf("the answer is not a JSON object: %v\n%s", err, raw)
		}
	}
	return recorded.Code, answer, string(raw)
}

// listed returns the SOP Instance UIDs in one of the answer's sequences,
// each with its failure reason, or 0 when it has none.
func listed(answer map[string]map[string]any, sequence string) map[string]int {
	out := map[string]int{}
	element, ok := answer[sequence]
	if !ok {
		return out
	}
	for _, item := range element["Value"].([]any) {
		fields := item.(map[string]any)
		uid := fields["00081155"].(map[string]any)["Value"].([]any)[0].(string)
		reason := 0
		if failure, ok := fields["00081197"].(map[string]any); ok {
			reason = int(failure["Value"].([]any)[0].(float64))
		}
		out[uid] = reason
	}
	return out
}

// TestStoreKeepsEveryInstance checks the plain case: both instances reach
// the archive, the answer is 200 and lists both with where each is
// retrieved from.
func TestStoreKeepsEveryInstance(t *testing.T) {
	backend := &recorder{}
	status, answer, raw := post(t, NewServer(backend), "/studies", part10(t, "1.2.3.1"), part10(t, "1.2.3.2"))
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, raw)
	}
	if len(backend.put) != 2 || text(backend.put[0], tagSOPInstance) != "1.2.3.1" ||
		text(backend.put[1], tagSOPInstance) != "1.2.3.2" {
		t.Fatalf("the archive was given %d instances", len(backend.put))
	}
	if text(backend.put[0], tagPatientName) != "Doe^Jane" {
		t.Error("the instance the archive was given is not the one sent")
	}
	kept := listed(answer, "00081199")
	if len(kept) != 2 || len(listed(answer, "00081198")) != 0 {
		t.Errorf("kept %v, refused %v", kept, listed(answer, "00081198"))
	}
	if got := answer["00081190"]["Value"].([]any)[0]; got != "http://example.com/studies/1.2.3.100" {
		t.Errorf("the study's Retrieve URL is %v", got)
	}
}

// TestStoreIntoAStudy checks the study a request names: an instance of it
// is kept, and an instance of another is refused as not matching (A900H),
// which is 409 when it is all the request held.
func TestStoreIntoAStudy(t *testing.T) {
	backend := &recorder{}
	server := NewServer(backend)
	if status, _, raw := post(t, server, "/studies/1.2.3.100", part10(t, "1.2.3.1")); status != http.StatusOK {
		t.Fatalf("into its own study: status %d: %s", status, raw)
	}

	status, answer, raw := post(t, server, "/studies/1.2.9.9", part10(t, "1.2.3.2"))
	if status != http.StatusConflict {
		t.Fatalf("into another study: status %d: %s", status, raw)
	}
	if refused := listed(answer, "00081198"); refused["1.2.3.2"] != 0xA900 {
		t.Errorf("refused %v, want 1.2.3.2 for A900H", refused)
	}
	if len(backend.put) != 1 {
		t.Errorf("the archive was given %d instances, want the first alone", len(backend.put))
	}
	if _, ok := answer["00081190"]; ok {
		t.Error("an answer that kept nothing says where to retrieve it")
	}
}

// TestStoreKeepsWhatItCan checks a request the archive takes part of: 202,
// the one it kept and the one it did not each listed, the reason a
// processing failure (0110H), and nothing of the archive's own error.
func TestStoreKeepsWhatItCan(t *testing.T) {
	backend := &recorder{refuses: map[string]bool{"1.2.3.2": true}}
	status, answer, raw := post(t, NewServer(backend), "/studies", part10(t, "1.2.3.1"), part10(t, "1.2.3.2"))
	if status != http.StatusAccepted {
		t.Fatalf("status %d: %s", status, raw)
	}
	if kept := listed(answer, "00081199"); len(kept) != 1 || kept["1.2.3.1"] != 0 {
		t.Errorf("kept %v", kept)
	}
	if refused := listed(answer, "00081198"); refused["1.2.3.2"] != 0x0110 {
		t.Errorf("refused %v, want 1.2.3.2 for 0110H", refused)
	}
	if strings.Contains(raw, "/srv") || strings.Contains(raw, "disk") {
		t.Errorf("the answer repeats the archive's error: %s", raw)
	}
}

// TestStorePassesOverWhatIsNotAnInstance checks a part that is not a DICOM
// file: it cannot be listed, having no UID, so it shows in the status
// alone. Beside an instance the answer is 202, and on its own 409.
func TestStorePassesOverWhatIsNotAnInstance(t *testing.T) {
	backend := &recorder{}
	server := NewServer(backend)
	status, answer, raw := post(t, server, "/studies", []byte("notes about the patient"), part10(t, "1.2.3.1"))
	if status != http.StatusAccepted || len(listed(answer, "00081199")) != 1 {
		t.Errorf("beside an instance: status %d: %s", status, raw)
	}
	if status, _, _ := post(t, server, "/studies", []byte("notes about the patient")); status != http.StatusConflict {
		t.Errorf("on its own: status %d", status)
	}
	if len(backend.put) != 1 {
		t.Errorf("the archive was given %d instances", len(backend.put))
	}
}

// TestStoreRefusals checks the requests that are not store requests at
// all, each refused before the archive is given anything.
func TestStoreRefusals(t *testing.T) {
	file := part10(t, "1.2.3.1")
	var multi bytes.Buffer
	writer := multipart.NewWriter(&multi)
	part, _ := writer.CreatePart(textproto.MIMEHeader{})
	_, _ = part.Write(file)
	_ = writer.Close()
	boundary := writer.Boundary()

	cases := []struct {
		name, target, contentType string
		body                      []byte
		status                    int
	}{
		{"a bare file", "/studies", "application/dicom", file, http.StatusUnsupportedMediaType},
		{"no Content-Type", "/studies", "", multi.Bytes(), http.StatusUnsupportedMediaType},
		{"multipart with no boundary", "/studies", `multipart/related; type="application/dicom"`, multi.Bytes(), http.StatusUnsupportedMediaType},
		{"metadata and bulk data", "/studies", `multipart/related; type="application/dicom+json"; boundary=` + boundary, multi.Bytes(), http.StatusUnsupportedMediaType},
		{"no parts", "/studies", `multipart/related; type="application/dicom"; boundary=` + boundary, []byte("--" + boundary + "--\r\n"), http.StatusBadRequest},
		{"a body cut short", "/studies", `multipart/related; type="application/dicom"; boundary=` + boundary, multi.Bytes()[:len(multi.Bytes())/2], http.StatusBadRequest},
		{"a study that is not a UID", "/studies/abc", `multipart/related; boundary=` + boundary, multi.Bytes(), http.StatusBadRequest},
		{"to a series", "/studies/1.2.3.100/series/1.2.3.9", `multipart/related; boundary=` + boundary, multi.Bytes(), http.StatusMethodNotAllowed},
		{"to the instances resource", "/instances", `multipart/related; boundary=` + boundary, multi.Bytes(), http.StatusMethodNotAllowed},
		{"to a study's metadata", "/studies/1.2.3.100/metadata", `multipart/related; boundary=` + boundary, multi.Bytes(), http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			backend := &recorder{}
			status, _, raw := postBody(t, NewServer(backend), c.target, c.contentType, c.body)
			if status != c.status {
				t.Errorf("status %d, want %d: %s", status, c.status, raw)
			}
			if len(backend.put) != 0 {
				t.Error("the archive was given an instance")
			}
		})
	}

	// A type parameter left out is taken: the parts say what they are.
	backend := &recorder{}
	if status, _, raw := postBody(t, NewServer(backend), "/studies", `multipart/related; boundary=`+boundary, multi.Bytes()); status != http.StatusOK {
		t.Errorf("multipart/related with no type: status %d: %s", status, raw)
	}

	// The methods a resource has are named when another is used.
	request := httptest.NewRequest(http.MethodPut, "/studies", nil)
	recorded := httptest.NewRecorder()
	NewServer(backend).ServeHTTP(recorded, request)
	if recorded.Code != http.StatusMethodNotAllowed || recorded.Header().Get("Allow") != "GET, POST" {
		t.Errorf("PUT /studies: status %d, Allow %q", recorded.Code, recorded.Header().Get("Allow"))
	}
}

// TestStoreRequestIsBounded checks that a request larger than the server
// takes is refused with 413 and not read to its end.
func TestStoreRequestIsBounded(t *testing.T) {
	backend := &recorder{}
	server := NewServer(backend, WithMaxStoreBytes(256))
	status, _, raw := post(t, server, "/studies", part10(t, "1.2.3.1"), part10(t, "1.2.3.2"))
	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d: %s", status, raw)
	}
}

// TestClientStoresToServer checks the client against the server: what
// Store reports is what the server did, for a request kept whole, one kept
// in part, and one refused.
func TestClientStoresToServer(t *testing.T) {
	backend := &recorder{refuses: map[string]bool{"1.2.3.2": true}}
	service := httptest.NewServer(http.StripPrefix("/dicom-web", NewServer(backend)))
	defer service.Close()
	client, err := NewClient(service.URL + "/dicom-web")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	result, err := client.Store(ctx, "", part10(t, "1.2.3.1"))
	if err != nil || len(result.Stored) != 1 || len(result.Failed) != 0 {
		t.Fatalf("kept whole: %+v, %v", result, err)
	}
	if want := service.URL + "/dicom-web/studies/1.2.3.100"; result.RetrieveURL != want {
		t.Errorf("Retrieve URL %q, want %q", result.RetrieveURL, want)
	}

	result, err = client.Store(ctx, "", part10(t, "1.2.3.3"), part10(t, "1.2.3.2"))
	if err != nil || len(result.Stored) != 1 || len(result.Failed) != 1 || result.Failed[0].FailureReason != 0x0110 {
		t.Errorf("kept in part: %+v, %v", result, err)
	}

	result, err = client.Store(ctx, "1.2.9.9", part10(t, "1.2.3.4"))
	var refused *StatusError
	if !errors.As(err, &refused) || refused.StatusCode != http.StatusConflict {
		t.Fatalf("into another study: %v", err)
	}
	if result == nil || len(result.Failed) != 1 || result.Failed[0].FailureReason != 0xA900 {
		t.Errorf("the refusal's account: %+v", result)
	}
}
