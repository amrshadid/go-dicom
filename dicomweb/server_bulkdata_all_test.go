package dicomweb

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// locatedPart is one part of a multipart answer: its body, its type, and
// the resource its Content-Location says it is.
type locatedPart struct {
	body            []byte
	media, location string
}

// locatedParts reads a multipart answer of application/octet-stream into
// its parts.
func locatedParts(t *testing.T, response *http.Response, body string) []locatedPart {
	t.Helper()
	media, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "multipart/related" || parameters["type"] != mediaOctetStream {
		t.Fatalf("Content-Type is %q: %s", response.Header.Get("Content-Type"), body)
	}
	var parts []locatedPart
	reader := multipart.NewReader(strings.NewReader(body), parameters["boundary"])
	for {
		part, err := reader.NextRawPart()
		if err != nil {
			break
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, locatedPart{data, part.Header.Get("Content-Type"), part.Header.Get("Content-Location")})
	}
	return parts
}

// namedURIs adds every BulkDataURI in a DICOM JSON object to found.
func namedURIs(node map[string]any, found map[string]bool) {
	for _, value := range node {
		element, _ := value.(map[string]any)
		if uri, ok := element["BulkDataURI"].(string); ok {
			found[uri] = true
		}
		items, _ := element["Value"].([]any)
		for _, item := range items {
			if nested, ok := item.(map[string]any); ok {
				namedURIs(nested, found)
			}
		}
	}
}

// heldPair is an archive of two instances of one series: an image with a
// document and an icon in a sequence (1.2.3.7), and a two-frame JPEG image
// (1.2.3.8).
func heldPair(t *testing.T) *recorder {
	t.Helper()
	withValues, _, _ := withBulk(t)
	compressed := multiFrame(t, "1.2.3.8", 2, 16, 16)
	if err := compressed.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	for _, held := range []struct {
		uid  string
		file func() ([]byte, error)
	}{
		{"1.2.3.7", func() ([]byte, error) { return encodeFile(withValues) }},
		{"1.2.3.8", func() ([]byte, error) { return encodeFile(compressed) }},
	} {
		file, err := held.file()
		if err != nil {
			t.Fatal(err)
		}
		backend.hold(held.uid, file)
	}
	return backend
}

// TestBulkDataOfStudySeriesAndInstance checks the bulk data resources of a
// study, a series and an instance (PS3.18 Table 10.4.1.5-1): a part for
// each BulkDataURI the metadata of their instances gives, in the archive's
// order and none missing or more, each with that URI as its
// Content-Location and the bytes the URI serves on its own, the JPEG's
// pixels decoded.
func TestBulkDataOfStudySeriesAndInstance(t *testing.T) {
	backend := heldPair(t)

	// What metadata names for each instance, and what each URI serves on
	// its own.
	named := map[string][]string{}
	served := map[string][]byte{}
	for _, uid := range []string{"1.2.3.7", "1.2.3.8"} {
		response, body := get(t, backend, heldInstances+uid+"/metadata")
		var described []map[string]any
		if err := json.Unmarshal([]byte(body), &described); err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("%s metadata: status %d, %v", uid, response.StatusCode, err)
		}
		uris := map[string]bool{}
		namedURIs(described[0], uris)
		for uri := range uris {
			named[uid] = append(named[uid], uri)
			response, body := get(t, backend, strings.TrimPrefix(uri, "http://example.com"))
			bodies, _, _ := frameParts(t, response, body)
			if len(bodies) != 1 {
				t.Fatalf("%s: %d parts", uri, len(bodies))
			}
			served[uri] = bodies[0]
		}
	}
	if len(named["1.2.3.7"]) != 3 || len(named["1.2.3.8"]) != 1 {
		t.Fatalf("metadata names %v; want the image's pixels, document and icon, and the JPEG's pixels", named)
	}

	for _, c := range []struct {
		path string
		uids []string
	}{
		{"/studies/1.2.3.100/bulkdata", []string{"1.2.3.7", "1.2.3.8"}},
		{"/studies/1.2.3.100/series/1.2.3.9/bulkdata", []string{"1.2.3.7", "1.2.3.8"}},
		{heldInstances + "1.2.3.8/bulkdata", []string{"1.2.3.8"}},
		{heldInstances + "1.2.3.7/bulkdata", []string{"1.2.3.7"}},
	} {
		for _, accept := range []string{"", `multipart/related; type="application/octet-stream"`} {
			response, body := get(t, backend, c.path, "Accept", accept)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s: status %d: %s", c.path, response.StatusCode, body)
			}
			parts := locatedParts(t, response, body)
			var want []string
			for _, uid := range c.uids {
				want = append(want, named[uid]...)
			}
			if len(parts) != len(want) {
				t.Fatalf("%s: %d parts, metadata names %d values", c.path, len(parts), len(want))
			}
			at := 0
			for i, part := range parts {
				// Each instance's parts together, in the archive's order.
				for at < len(c.uids) && !strings.Contains(part.location, "/instances/"+c.uids[at]+"/") {
					at++
				}
				if at == len(c.uids) {
					t.Errorf("%s: part %d at %q is out of the archive's order", c.path, i, part.location)
					break
				}
				value, ok := served[part.location]
				if !ok {
					t.Errorf("%s: part %d at %q, which metadata does not name", c.path, i, part.location)
					continue
				}
				if !bytes.Equal(part.body, value) {
					t.Errorf("%s: %s is %d bytes, not the %d it serves alone", c.path, part.location, len(part.body), len(value))
				}
				if part.media != mediaOctetStream+"; transfer-syntax="+syntaxExplicitLE {
					t.Errorf("%s: %s is labeled %q", c.path, part.location, part.media)
				}
			}
		}
	}
}

// TestBulkDataOfStudyAsStored checks a study's bulk data asked for with
// transfer-syntax=*: compressed pixels come as stored, a part a frame in
// their own type at the frame's resource, and the other values as before.
func TestBulkDataOfStudyAsStored(t *testing.T) {
	backend := heldPair(t)
	response, body := get(t, backend, "/studies/1.2.3.100/bulkdata", "Accept", acceptStored)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	parts := locatedParts(t, response, body)
	frames := 0
	for _, part := range parts {
		if !strings.Contains(part.location, "/instances/1.2.3.8/") {
			continue
		}
		frames++
		frame, frameBody := get(t, backend, strings.TrimPrefix(part.location, "http://example.com"), "Accept", acceptStored)
		if frame.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d: %s", part.location, frame.StatusCode, frameBody)
		}
		want, _, _ := frameParts(t, frame, frameBody)
		if part.media != "image/jpeg; transfer-syntax=1.2.840.10008.1.2.4.50" || !bytes.Equal(part.body, want[0]) {
			t.Errorf("%s: %q, %d bytes; the frame resource sends %d", part.location, part.media, len(part.body), len(want[0]))
		}
	}
	if frames != 2 || len(parts) != 5 {
		t.Errorf("%d parts, %d of them the JPEG's frames; want 5 and 2", len(parts), frames)
	}
}

// TestBulkDataOfStudyRefusals checks what the resources of several values
// do not serve: a single-part type, which PS3.18 allows only for a resource
// of one item, and a compressed type alone, 406; a study the archive does
// not hold, 404; an instance with no bulk value, 204 with no body; and a
// POST, which a study's bulk data is not stored to, 405.
func TestBulkDataOfStudyRefusals(t *testing.T) {
	ds := instance(t, "1.2.3.30")
	ds.Remove(tag.New(0x0020, 0x000E))
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000E), dataelem.UI, "1.2.3.9")); err != nil {
		t.Fatal(err)
	}
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.30", file)
	for _, c := range []struct {
		method, path, accept string
		status               int
	}{
		{http.MethodGet, "/studies/1.2.3.100/bulkdata", mediaOctetStream, http.StatusNotAcceptable},
		{http.MethodGet, "/studies/1.2.3.100/bulkdata", `multipart/related; type="image/jpeg"`, http.StatusNotAcceptable},
		{http.MethodGet, "/studies/1.2.3.100/bulkdata", "image/png", http.StatusNotAcceptable},
		{http.MethodGet, heldInstances + "1.2.3.30/bulkdata", "", http.StatusNoContent},
		{http.MethodGet, "/studies/1.2.3.100/series/1.2.3.9/bulkdata", "", http.StatusNoContent},
		{http.MethodPost, "/studies/1.2.3.100/bulkdata", "", http.StatusMethodNotAllowed},
	} {
		response, body := send(t, backend, c.method, c.path, "Accept", c.accept)
		if response.StatusCode != c.status {
			t.Errorf("%s %s with %q: status %d, want %d: %s", c.method, c.path, c.accept, response.StatusCode, c.status, body)
		}
		if c.status == http.StatusNoContent && body != "" {
			t.Errorf("%s: 204 with a body: %q", c.path, body)
		}
	}
	response, body := get(t, &recorder{}, "/studies/1.2.3.100/bulkdata")
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("an empty archive: status %d: %s", response.StatusCode, body)
	}
}
