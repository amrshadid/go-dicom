package dicomweb

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// withBulk is a three-frame image that also holds a long value of its own,
// an encapsulated document, and one inside a sequence, an icon's pixels:
// what metadata leaves out, at two depths.
func withBulk(t *testing.T) (ds *dataset.Dataset, document, icon []byte) {
	t.Helper()
	ds = multiFrame(t, "1.2.3.7", 3, 32, 32)
	document = bytes.Repeat([]byte("%PDF-1.4 "), 400)
	icon = make([]byte, 48*48)
	for i := range icon {
		icon[i] = byte(i * 3)
	}
	item := dataset.NewDataset()
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value any
	}{
		{tag.New(0x0028, 0x0002), dataelem.US, uint16(1)},
		{tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2"},
		{tag.New(0x0028, 0x0010), dataelem.US, uint16(48)},
		{tag.New(0x0028, 0x0011), dataelem.US, uint16(48)},
		{tag.New(0x0028, 0x0100), dataelem.US, uint16(8)},
		{tag.New(0x7FE0, 0x0010), dataelem.OB, icon},
	} {
		if err := item.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	icons := sequence.NewFromItems(item)
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0088, 0x0200), dataelem.SQ, icons)); err != nil {
		t.Fatal(err)
	}
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0042, 0x0011), dataelem.OB, document)); err != nil {
		t.Fatal(err)
	}
	return ds, document, icon
}

// TestMetadataNamesBulkDataURIs checks that metadata puts a BulkDataURI
// where each value it leaves out was (PS3.18 F.2.6), inside a sequence as
// well as at the top, and that each URI serves that value: the document's
// bytes, the icon's pixels, and the image's frames joined.
func TestMetadataNamesBulkDataURIs(t *testing.T) {
	ds, document, icon := withBulk(t)
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)

	response, body := get(t, backend, heldInstances+"1.2.3.7/metadata")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metadata: status %d: %s", response.StatusCode, body)
	}
	var described []map[string]map[string]any
	if err := json.Unmarshal([]byte(body), &described); err != nil {
		t.Fatal(err)
	}
	root := "http://example.com" + heldInstances + "1.2.3.7/bulkdata/"
	iconItem := described[0]["00880200"]["Value"].([]any)[0].(map[string]any)
	uris := map[string]any{
		"7FE00010":            described[0]["7FE00010"]["BulkDataURI"],
		"00420011":            described[0]["00420011"]["BulkDataURI"],
		"00880200/0/7FE00010": iconItem["7FE00010"].(map[string]any)["BulkDataURI"],
	}
	pixels, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	want := map[string][]byte{
		"7FE00010":            pixels.GetValue().([]byte),
		"00420011":            document,
		"00880200/0/7FE00010": icon,
	}
	for path, uri := range uris {
		if uri != root+path {
			t.Errorf("%s: BulkDataURI is %v, want %s", path, uri, root+path)
			continue
		}
		response, body := get(t, backend, strings.TrimPrefix(uri.(string), "http://example.com"))
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, response.StatusCode, body)
		}
		bodies, types, of := frameParts(t, response, body)
		if of != mediaOctetStream || len(bodies) != 1 {
			t.Fatalf("%s: %d parts of %q", path, len(bodies), of)
		}
		if !bytes.Equal(bodies[0], want[path]) {
			t.Errorf("%s: %d bytes, not the %d of the value", path, len(bodies[0]), len(want[path]))
		}
		if types[0] != mediaOctetStream+"; transfer-syntax="+syntaxExplicitLE {
			t.Errorf("%s: the part is labeled %q", path, types[0])
		}
	}
	// A short value is carried in the metadata, not named.
	if _, named := described[0]["00280010"]["BulkDataURI"]; named {
		t.Error("Rows was given a BulkDataURI")
	}
}

// TestBulkDataOfCompressedPixelData checks Pixel Data held compressed: as
// stored it is one part a frame in its own type, as the frames resource
// sends it; uncompressed, one part holding every frame decoded.
func TestBulkDataOfCompressedPixelData(t *testing.T) {
	ds := multiFrame(t, "1.2.3.8", 3, 16, 16)
	if err := ds.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.8", file)
	path := heldInstances + "1.2.3.8/bulkdata/7FE00010"

	response, body := get(t, backend, path, "Accept", acceptStored)
	bodies, _, of := frameParts(t, response, body)
	if of != "image/jpeg" || len(bodies) != 3 {
		t.Fatalf("as stored: %d parts of %q", len(bodies), of)
	}
	for i := range bodies {
		if want, _ := ds.GetEncapsulatedFrame(i); !bytes.Equal(bodies[i], want) {
			t.Errorf("as stored: part %d is not frame %d", i, i+1)
		}
	}

	response, body = get(t, backend, path)
	bodies, _, of = frameParts(t, response, body)
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	pixels, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	if of != mediaOctetStream || len(bodies) != 1 || !bytes.Equal(bodies[0], pixels.GetValue().([]byte)) {
		t.Errorf("uncompressed: %d parts of %q, not the decoded pixel data", len(bodies), of)
	}
}

// TestBulkDataRefusals checks what is not bulk data: a path that is not one
// to an element is a bad request; an element that is not there, or not a
// binary value, does not exist as bulk data; and another type than
// application/octet-stream for a value that is not Pixel Data is refused.
func TestBulkDataRefusals(t *testing.T) {
	ds, _, _ := withBulk(t)
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	cases := []struct {
		path, accept string
		status       int
	}{
		{"7FE0001", "", http.StatusBadRequest},
		{"7FE00010/0", "", http.StatusBadRequest},
		{"00880200/x/7FE00010", "", http.StatusBadRequest},
		{"zzzzzzzz", "", http.StatusBadRequest},
		{"00420012", "", http.StatusNotFound},
		{"00880200/1/7FE00010", "", http.StatusNotFound},
		{"00100010", "", http.StatusNotFound},
		{"00880200", "", http.StatusNotFound},
		{"00420011", `multipart/related; type="image/jpeg"`, http.StatusNotAcceptable},
		{"00420011", acceptStored, http.StatusOK},
	}
	for _, c := range cases {
		response, body := get(t, backend, heldInstances+"1.2.3.7/bulkdata/"+c.path, "Accept", c.accept)
		if response.StatusCode != c.status {
			t.Errorf("%s with %q: status %d, want %d: %s", c.path, c.accept, response.StatusCode, c.status, body)
		}
	}
}
