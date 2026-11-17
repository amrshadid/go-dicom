package dicomweb

import (
	"bytes"
	"context"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The retrieve service sends an instance as the archive holds it whenever
// the request allows, so these tests compare bytes: what a client receives
// is the file that was stored.

const (
	syntaxExplicitLE   = "1.2.840.10008.1.2.1"
	syntaxJPEGBaseline = "1.2.840.10008.1.2.4.50"
	syntaxJPEGLossless = "1.2.840.10008.1.2.4.70"
	acceptDICOM        = `multipart/related; type="application/dicom"`
)

// archive holds two instances of the study the test files are in.
func archive(t *testing.T) *recorder {
	t.Helper()
	backend := &recorder{}
	backend.hold("1.2.3.1", part10(t, "1.2.3.1"))
	backend.hold("1.2.3.2", part10(t, "1.2.3.2"))
	return backend
}

// parts reads a multipart response into its parts' bodies and types.
func parts(t *testing.T, response *http.Response, body string) (bodies [][]byte, types []string) {
	t.Helper()
	media, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "multipart/related" || parameters["type"] != "application/dicom" {
		t.Fatalf("Content-Type is %q", response.Header.Get("Content-Type"))
	}
	reader := multipart.NewReader(strings.NewReader(body), parameters["boundary"])
	for {
		part, err := reader.NextRawPart()
		if err != nil {
			break
		}
		var data bytes.Buffer
		if _, err := data.ReadFrom(part); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, data.Bytes())
		types = append(types, part.Header.Get("Content-Type"))
	}
	return bodies, types
}

// TestRetrieveResourceNamesWhatIsSent checks each retrieve resource of
// PS3.18 10.4.1: what the archive is asked for, and that each instance
// comes back as the stored file, labeled with its transfer syntax.
func TestRetrieveResourceNamesWhatIsSent(t *testing.T) {
	cases := []struct {
		path    string
		located [3]string
		want    []string
	}{
		{"/studies/1.2.3.100", [3]string{"1.2.3.100", "", ""}, []string{"1.2.3.1", "1.2.3.2"}},
		{"/studies/1.2.3.100/series/1.2.3.9", [3]string{"1.2.3.100", "1.2.3.9", ""}, []string{"1.2.3.1", "1.2.3.2"}},
		{"/studies/1.2.3.100/series/1.2.3.9/instances/1.2.3.2", [3]string{"1.2.3.100", "1.2.3.9", "1.2.3.2"}, []string{"1.2.3.2"}},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			backend := archive(t)
			response, body := get(t, backend, c.path, "Accept", acceptDICOM+"; transfer-syntax=*")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", response.StatusCode, body)
			}
			if backend.located != c.located {
				t.Errorf("the archive was asked for %v, want %v", backend.located, c.located)
			}
			bodies, types := parts(t, response, body)
			if len(bodies) != len(c.want) {
				t.Fatalf("%d parts, want %d", len(bodies), len(c.want))
			}
			for i, uid := range c.want {
				if !bytes.Equal(bodies[i], backend.files[uid]) {
					t.Errorf("part %d is not the stored file of %s", i, uid)
				}
				if types[i] != "application/dicom; transfer-syntax="+syntaxExplicitLE {
					t.Errorf("part %d is labeled %q", i, types[i])
				}
			}
		})
	}
}

// TestRetrieveAccept checks what each form of Accept is given (PS3.18
// 8.7.3.5): the stored file when the request allows it, the bare file for
// one instance asked for as application/dicom, and 406 otherwise.
func TestRetrieveAccept(t *testing.T) {
	const study = "/studies/1.2.3.100"
	const instance = study + "/series/1.2.3.9/instances/1.2.3.1"
	cases := []struct {
		name, path, accept string
		status             int
		bare               bool
	}{
		{"nothing said", study, "", http.StatusOK, false},
		{"anything", study, "*/*", http.StatusOK, false},
		{"the default syntax, which the files are in", study, acceptDICOM, http.StatusOK, false},
		{"the stored syntax by name", study, acceptDICOM + "; transfer-syntax=" + syntaxExplicitLE, http.StatusOK, false},
		{"as stored", study, acceptDICOM + "; transfer-syntax=*", http.StatusOK, false},
		{"a syntax the files are not in", study, acceptDICOM + "; transfer-syntax=" + syntaxJPEGLossless, http.StatusNotAcceptable, false},
		{"another media type", study, `multipart/related; type="application/octet-stream"`, http.StatusNotAcceptable, false},
		{"a page", study, "text/html", http.StatusNotAcceptable, false},
		{"several instances as one file", study, "application/dicom", http.StatusNotAcceptable, false},
		{"one instance as its file", instance, "application/dicom", http.StatusOK, true},
		{"one instance, multipart", instance, acceptDICOM, http.StatusOK, false},
		{"the accept parameter", study + "?accept=" + "multipart/related%3B%20type=%22application/dicom%22", "text/html", http.StatusOK, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			backend := archive(t)
			var header []string
			if c.accept != "" {
				header = []string{"Accept", c.accept}
			}
			response, body := get(t, backend, c.path, header...)
			if response.StatusCode != c.status {
				t.Fatalf("status %d, want %d: %s", response.StatusCode, c.status, body)
			}
			if c.bare {
				if !bytes.Equal([]byte(body), backend.files["1.2.3.1"]) {
					t.Error("the body is not the stored file")
				}
				if got := response.Header.Get("Content-Type"); got != "application/dicom; transfer-syntax="+syntaxExplicitLE {
					t.Errorf("Content-Type is %q", got)
				}
			} else if c.status == http.StatusOK {
				if bodies, _ := parts(t, response, body); len(bodies) == 0 {
					t.Error("no parts")
				}
			}
		})
	}
}

// TestRetrieveRefusals checks what is not retrieved: nothing held is 404,
// a path that is not UIDs is refused before the archive is asked, and an
// archive that fails is a 500 that does not repeat its error.
func TestRetrieveRefusals(t *testing.T) {
	backend := archive(t)
	for target, want := range map[string]int{
		"/studies/1.2.3.100/series/1.2.3.9/instances/1.2.3.404":          http.StatusNotFound,
		"/studies/1.2.3.100/series/1.2.3.9/instances/1.2.3.404/metadata": http.StatusNotFound,
		"/studies/abc":                         http.StatusBadRequest,
		"/studies/1.2.3.100/series/x":          http.StatusBadRequest,
		"/studies/1.2.3.100/metadata/metadata": http.StatusNotFound,
	} {
		if response, body := get(t, backend, target); response.StatusCode != want {
			t.Errorf("%s: status %d, want %d: %s", target, response.StatusCode, want, body)
		}
	}

	empty := &recorder{}
	if response, _ := get(t, empty, "/studies/1.2.3.100"); response.StatusCode != http.StatusNotFound {
		t.Errorf("an archive holding nothing: status %d", response.StatusCode)
	}

	// Named by the archive, and gone when it is opened.
	broken := &recorder{order: []string{"1.2.3.7"}}
	response, body := get(t, broken, "/studies/1.2.3.100")
	if response.StatusCode != http.StatusInternalServerError || strings.Contains(body, "/srv") {
		t.Errorf("status %d: %s", response.StatusCode, body)
	}

	if response, _ := send(t, backend, http.MethodDelete, "/studies/1.2.3.100"); response.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("DELETE of a study: status %d", response.StatusCode)
	}
}

// image is an 8-bit image, so it can be held compressed.
func image(t *testing.T, uid string) *dataset.Dataset {
	t.Helper()
	ds := instance(t, uid)
	const rows, columns = 16, 16
	pixels := make([]byte, rows*columns)
	for i := range pixels {
		pixels[i] = byte(i)
	}
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value any
	}{
		{tag.New(0x0028, 0x0002), dataelem.US, uint16(1)},
		{tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2"},
		{tag.New(0x0028, 0x0010), dataelem.US, uint16(rows)},
		{tag.New(0x0028, 0x0011), dataelem.US, uint16(columns)},
		{tag.New(0x0028, 0x0100), dataelem.US, uint16(8)},
		{tag.New(0x0028, 0x0101), dataelem.US, uint16(8)},
		{tag.New(0x0028, 0x0102), dataelem.US, uint16(7)},
		{tag.New(0x0028, 0x0103), dataelem.US, uint16(0)},
		{tag.New(0x7FE0, 0x0010), dataelem.OB, pixels},
	} {
		if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	return ds
}

// TestRetrieveDecodesForTheDefaultSyntax checks an instance held
// compressed: as stored it is the stored bytes; for a request that names no
// syntax, which asks for Explicit VR Little Endian, it is decoded; and for
// a third syntax it is not acceptable.
func TestRetrieveDecodesForTheDefaultSyntax(t *testing.T) {
	ds := image(t, "1.2.3.5")
	if err := ds.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	stored, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.5", stored)
	const path = "/studies/1.2.3.100"

	response, body := get(t, backend, path, "Accept", acceptDICOM+"; transfer-syntax=*")
	bodies, types := parts(t, response, body)
	if len(bodies) != 1 || !bytes.Equal(bodies[0], stored) || !strings.HasSuffix(types[0], syntaxJPEGBaseline) {
		t.Fatalf("as stored: %d parts, labeled %v", len(bodies), types)
	}

	response, body = get(t, backend, path, "Accept", acceptDICOM)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the default syntax: status %d: %s", response.StatusCode, body)
	}
	bodies, types = parts(t, response, body)
	if len(bodies) != 1 || !strings.HasSuffix(types[0], syntaxExplicitLE) {
		t.Fatalf("the default syntax: %d parts, labeled %v", len(bodies), types)
	}
	decoded, err := Part{Data: bodies[0]}.Dataset()
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.TransferSyntaxUID(); got != syntaxExplicitLE {
		t.Errorf("the file says it is %s", got)
	}
	pixels, _ := decoded.Get(tag.New(0x7FE0, 0x0010))
	if raw, _ := pixels.GetValue().([]byte); len(raw) != 16*16 {
		t.Errorf("the decoded image holds %d bytes, want %d", len(raw), 16*16)
	}

	response, _ = get(t, backend, path, "Accept", acceptDICOM+"; transfer-syntax="+syntaxJPEGLossless)
	if response.StatusCode != http.StatusNotAcceptable {
		t.Errorf("a third syntax: status %d", response.StatusCode)
	}
}

// TestRetrieveSendsWhatIsAcceptable checks a study held in two syntaxes,
// asked for in one of them: the instances in it are sent, and the status is
// 206 to say the others were not (PS3.18 10.4.3.1).
func TestRetrieveSendsWhatIsAcceptable(t *testing.T) {
	ds := image(t, "1.2.3.5")
	if err := ds.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	compressed, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.5", compressed)
	backend.hold("1.2.3.1", part10(t, "1.2.3.1"))

	response, body := get(t, backend, "/studies/1.2.3.100", "Accept", acceptDICOM+"; transfer-syntax="+syntaxExplicitLE)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Explicit VR Little Endian of both: status %d", response.StatusCode)
	}
	if bodies, _ := parts(t, response, body); len(bodies) != 2 {
		t.Errorf("%d parts, want both", len(bodies))
	}

	response, body = get(t, backend, "/studies/1.2.3.100", "Accept", acceptDICOM+"; transfer-syntax="+syntaxJPEGBaseline)
	bodies, types := parts(t, response, body)
	if len(bodies) != 1 || !bytes.Equal(bodies[0], compressed) || !strings.HasSuffix(types[0], syntaxJPEGBaseline) {
		t.Errorf("%d parts, labeled %v", len(bodies), types)
	}
}

// TestMetadataLeavesBulkDataOut checks metadata: each instance's attributes
// as DICOM JSON, the pixel data named and not carried, and nothing of the
// File Meta Information, which describes the file and not the instance.
func TestMetadataLeavesBulkDataOut(t *testing.T) {
	file, err := encodeFile(image(t, "1.2.3.5"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.5", file)

	response, body := get(t, backend, "/studies/1.2.3.100/metadata")
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/dicom+json" {
		t.Fatalf("status %d, Content-Type %q: %s", response.StatusCode, response.Header.Get("Content-Type"), body)
	}
	results := decode(t, body)
	if len(results) != 1 {
		t.Fatalf("%d results", len(results))
	}
	result := results[0]
	if got := result["00080018"]["Value"].([]any)[0]; got != "1.2.3.5" {
		t.Errorf("SOP Instance UID %v", got)
	}
	if got := result["00280010"]["Value"].([]any)[0]; got != float64(16) {
		t.Errorf("Rows %v", got)
	}
	pixels, ok := result["7FE00010"]
	if !ok {
		t.Fatal("the metadata does not name the pixel data")
	}
	if len(pixels) != 1 || pixels["vr"] != "OB" {
		t.Errorf("the pixel data is %v, want its VR alone", pixels)
	}
	for key := range result {
		if strings.HasPrefix(key, "0002") {
			t.Errorf("the metadata carries %s of the File Meta Information", key)
		}
	}
}

// TestClientRetrievesFromServer checks the client against the server under
// a prefix: instances byte for byte, metadata, and the Retrieve URL a
// search returns leading to the study it names.
func TestClientRetrievesFromServer(t *testing.T) {
	backend := archive(t)
	backend.answers = []*dataset.Dataset{study("1.2.3.100", "Doe^Jane")}
	service := httptest.NewServer(http.StripPrefix("/dicom-web", NewServer(backend)))
	defer service.Close()
	client, err := NewClient(service.URL + "/dicom-web")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var got [][]byte
	if err := client.RetrieveEach(ctx, "1.2.3.100", "", "", func(p Part) error {
		got = append(got, p.Data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !bytes.Equal(got[0], backend.files["1.2.3.1"]) || !bytes.Equal(got[1], backend.files["1.2.3.2"]) {
		t.Errorf("retrieved %d files that are not the stored ones", len(got))
	}

	one, err := client.RetrieveInstance(ctx, "1.2.3.100", "1.2.3.9", "1.2.3.2")
	if err != nil || text(one, tagSOPInstance) != "1.2.3.2" {
		t.Errorf("one instance: %v", err)
	}

	metadata, err := client.RetrieveMetadata(ctx, "1.2.3.100", "", "")
	if err != nil || len(metadata) != 2 || text(metadata[0], tagPatientName) != "Doe^Jane" {
		t.Errorf("metadata: %d, %v", len(metadata), err)
	}

	studies, err := client.SearchForStudies(ctx, Query{})
	if err != nil || len(studies) != 1 {
		t.Fatalf("search: %d, %v", len(studies), err)
	}
	if got, want := text(studies[0], tagRetrieveURL), service.URL+"/dicom-web/studies/1.2.3.100"; got != want {
		t.Errorf("Retrieve URL %q, want %q", got, want)
	}
}
