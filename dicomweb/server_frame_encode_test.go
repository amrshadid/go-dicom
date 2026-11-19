package dicomweb

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// These tests decode what the server encodes with this library's own
// decoders, which says the frame is the one asked for and labeled with the
// syntax it is in. That the codestreams are what other implementations
// read is checked in scripts/interop-test.sh, where pylibjpeg's libjpeg and
// OpenJPEG and pydicom decode them.

// httpGet is a GET of target with an Accept header.
func httpGet(target, accept string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Accept", accept)
	return request
}

// serveWith answers request with server.
func serveWith(server *Server, request *http.Request) *http.Response {
	recorded := httptest.NewRecorder()
	server.ServeHTTP(recorded, request)
	return recorded.Result()
}

// readAll returns a response's body.
func readAll(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// encodedFrames asks for frames 3,1 of a five-frame 8-bit image held
// uncompressed, with accept, and returns the parts, their types, and the
// image's frames.
func encodedFrames(t *testing.T, accept string, options ...ServerOption) (bodies [][]byte, types []string, frames [][]byte, status int) {
	t.Helper()
	ds := multiFrame(t, "1.2.3.7", 5, 64, 64)
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	pixels, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	all := pixels.GetValue().([]byte)
	for n := range 5 {
		frames = append(frames, all[n*64*64:(n+1)*64*64])
	}
	request := httpGet(heldInstances+"1.2.3.7/frames/3,1", accept)
	recorded := serveWith(NewServer(backend, options...), request)
	if recorded.StatusCode != http.StatusOK {
		return nil, nil, frames, recorded.StatusCode
	}
	bodies, types, _ = frameParts(t, recorded, readAll(t, recorded))
	return bodies, types, frames, recorded.StatusCode
}

// decodeAs decodes a codestream in syntax as a frame of the 8-bit image
// encodedFrames holds.
func decodeAs(t *testing.T, syntax string, codestream []byte) []byte {
	t.Helper()
	ds := multiFrame(t, "1.2.3.7", 1, 64, 64)
	ds.Remove(tag.New(0x7FE0, 0x0010))
	ds.SetTransferSyntaxUID(syntax)
	samples, err := ds.DecodeFrame(codestream)
	if err != nil {
		t.Fatalf("decoding the %s frame: %v", syntax, err)
	}
	return samples
}

// TestFramesEncodedOnRequest checks that a frame held uncompressed is
// encoded into each syntax a request names, lossless ones to the frame's
// samples exactly and JPEG-LS near-lossless within the NEAR it is coded
// at, each part labeled with its type and syntax.
func TestFramesEncodedOnRequest(t *testing.T) {
	for syntax, media := range map[string]string{
		compress.RLELossless: "image/dicom-rle", compress.JPEGLSLossless: "image/jls",
		compress.JPEG2000Lossless: "image/jp2", compress.JPEGLosslessNonHierarch: "image/jpeg",
		compress.JPEGLossless: "image/jpeg", compress.JPEGLSNearLossless: "image/jls",
		compress.JPEG2000Lossy: "image/jp2", compress.JPEGBaseline: "image/jpeg", compress.JPEGExtended: "image/jpeg",
	} {
		accept := `multipart/related; type="` + media + `"; transfer-syntax=` + syntax
		bodies, types, frames, status := encodedFrames(t, accept)
		if status != http.StatusOK || len(bodies) != 2 {
			t.Fatalf("%s: status %d, %d parts", syntax, status, len(bodies))
		}
		for i, n := range []int{3, 1} {
			if types[i] != media+"; transfer-syntax="+syntax {
				t.Errorf("%s: part %d is labeled %q", syntax, i, types[i])
			}
			got := decodeAs(t, syntax, bodies[i])
			if len(got) != len(frames[n-1]) {
				t.Fatalf("%s frame %d: %d samples, want %d", syntax, n, len(got), len(frames[n-1]))
			}
			worst := 0
			for j := range got {
				worst = max(worst, int(got[j])-int(frames[n-1][j]), int(frames[n-1][j])-int(got[j]))
			}
			allowed := map[string]int{compress.JPEGLSNearLossless: DefaultFrameEncoding.JPEGLSNear,
				compress.JPEG2000Lossy: 255, compress.JPEGBaseline: 255, compress.JPEGExtended: 255}[syntax]
			if worst > allowed {
				t.Errorf("%s frame %d: a sample off by %d, %d allowed", syntax, n, worst, allowed)
			}
		}
	}
}

// TestFramesEncodedIntoTheDefault checks that a media type named without a
// syntax is the default PS3.18 Table 8.7.3-5 gives it, every one lossless,
// so a request never gets loss it did not name.
func TestFramesEncodedIntoTheDefault(t *testing.T) {
	for media, syntax := range map[string]string{
		"image/jpeg": compress.JPEGLossless, "image/jls": compress.JPEGLSLossless,
		"image/jp2": compress.JPEG2000Lossless, "image/dicom-rle": compress.RLELossless,
		"image/x-dicom-rle": compress.RLELossless,
	} {
		bodies, types, frames, status := encodedFrames(t, `multipart/related; type="`+media+`"`)
		if status != http.StatusOK {
			t.Fatalf("%s: status %d", media, status)
		}
		if !strings.HasSuffix(types[0], "transfer-syntax="+syntax) {
			t.Errorf("%s: labeled %q, want %s", media, types[0], syntax)
		}
		if !bytes.Equal(decodeAs(t, syntax, bodies[0]), frames[2]) {
			t.Errorf("%s: frame 3 does not decode to its samples", media)
		}
	}
}

// TestFramesEncodedInTheOrderAsked checks that the request's order decides:
// a viewer listing image/jls before anything else gets JPEG-LS, and one
// listing octet-stream first gets the frame as it is held.
func TestFramesEncodedInTheOrderAsked(t *testing.T) {
	_, types, _, _ := encodedFrames(t, `multipart/related; type="image/jls", */*`)
	if !strings.HasPrefix(types[0], "image/jls;") {
		t.Errorf("image/jls first: labeled %q", types[0])
	}
	_, types, _, _ = encodedFrames(t, `multipart/related; type="application/octet-stream", multipart/related; type="image/jls"`)
	if !strings.HasPrefix(types[0], mediaOctetStream) {
		t.Errorf("octet-stream first: labeled %q", types[0])
	}
}

// TestFrameEncodingIsTheServers checks that how much a lossy syntax loses
// is the server's: JPEG at quality 10 is smaller than at the default 90.
func TestFrameEncodingIsTheServers(t *testing.T) {
	accept := `multipart/related; type="image/jpeg"; transfer-syntax=` + compress.JPEGBaseline
	fine, _, _, _ := encodedFrames(t, accept)
	coarse, _, _, _ := encodedFrames(t, accept, WithFrameEncoding(FrameEncoding{JPEGQuality: 10}))
	if len(coarse[0]) >= len(fine[0]) {
		t.Errorf("quality 10 gives %d bytes, the default %d", len(coarse[0]), len(fine[0]))
	}
}

// TestFramesNotEncoded checks what is refused, 406 with the reason: a
// syntax that is not of the type named, one no encoder here writes (JPEG
// 2000 Part 2), an image a coder does not take (16 bits to JPEG Baseline),
// and a frame held lossy, which is not coded again.
func TestFramesNotEncoded(t *testing.T) {
	for _, c := range []struct{ accept, reason string }{
		{`multipart/related; type="image/jls"; transfer-syntax=` + compress.JPEG2000Lossless, "not a transfer syntax of image/jls"},
		{`multipart/related; type="image/jpx"`, "does not encode 1.2.840.10008.1.2.4.92"},
	} {
		request := httpGet(heldInstances+"1.2.3.7/frames/1", c.accept)
		ds := multiFrame(t, "1.2.3.7", 2, 4, 4)
		file, err := encodeFile(ds)
		if err != nil {
			t.Fatal(err)
		}
		backend := &recorder{}
		backend.hold("1.2.3.7", file)
		response := serveWith(NewServer(backend), request)
		if body := readAll(t, response); response.StatusCode != http.StatusNotAcceptable || !strings.Contains(body, c.reason) {
			t.Errorf("Accept %q: status %d: %s", c.accept, response.StatusCode, body)
		}
	}

	wide := multiFrame(t, "1.2.3.7", 2, 4, 4)
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value any
	}{
		{tag.New(0x0028, 0x0100), dataelem.US, uint16(16)},
		{tag.New(0x0028, 0x0101), dataelem.US, uint16(16)},
		{tag.New(0x0028, 0x0102), dataelem.US, uint16(15)},
		{tag.New(0x7FE0, 0x0010), dataelem.OW, make([]byte, 2*2*4*4)},
	} {
		wide.Remove(e.t)
		if err := wide.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	file, err := encodeFile(wide)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	response, body := get(t, backend, heldInstances+"1.2.3.7/frames/1", "Accept",
		`multipart/related; type="image/jpeg"; transfer-syntax=`+compress.JPEGBaseline)
	if response.StatusCode != http.StatusNotAcceptable || !strings.Contains(body, "cannot be encoded") {
		t.Errorf("16 bits as JPEG Baseline: status %d: %s", response.StatusCode, body)
	}

	held := multiFrame(t, "1.2.3.8", 2, 16, 16)
	if err := held.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	if file, err = encodeFile(held); err != nil {
		t.Fatal(err)
	}
	backend.hold("1.2.3.8", file)
	response, body = get(t, backend, heldInstances+"1.2.3.8/frames/1", "Accept", `multipart/related; type="image/jls"`)
	if response.StatusCode != http.StatusNotAcceptable || !strings.Contains(body, "held lossy") {
		t.Errorf("JPEG Baseline as image/jls: status %d: %s", response.StatusCode, body)
	}
}
