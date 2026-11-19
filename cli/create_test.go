package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// Nothing made a DICOM file: convert went one way (#402).

func runCreate(args ...string) error { return (&CreateCommand{}).Execute(args) }

// createdFile reads what create wrote.
func createdFile(t *testing.T, path string) (string, *dataset.Dataset) {
	t.Helper()
	meta, ds := readAnonymized(t, path)
	return meta.TransferSyntaxUID, ds
}

func elementBytes(ds *dataset.Dataset, t tag.Tag) []byte {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	b, _ := elem.GetValue().([]byte)
	return b
}

// TestCreateFromDICOMJSONGivesBackTheDataSet: a file written as DICOM JSON
// and created back is the same data set, compared as DICOM JSON, which holds
// every element and every binary value.
func TestCreateFromDICOMJSONGivesBackTheDataSet(t *testing.T) {
	dir := t.TempDir()
	source := fixture(t)
	js := filepath.Join(dir, "in.json")
	if err := (&ConvertCommand{}).Execute([]string{source, js, "--format", "dicom-json"}); err != nil {
		t.Fatalf("convert: %v", err)
	}
	out := filepath.Join(dir, "out.dcm")
	if err := runCreate(js, out); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, original := readAnonymized(t, source)
	_, made := createdFile(t, out)
	want, err := original.ToDICOMJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := made.ToDICOMJSON()
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Errorf("the created data set differs from the source:\n%.400s\n---\n%.400s", wantJSON, gotJSON)
	}
	if !bytes.Equal(elementBytes(made, tag.New(0x7FE0, 0x0010)), elementBytes(original, tag.New(0x7FE0, 0x0010))) {
		t.Error("Pixel Data differs")
	}
}

// TestCreateFromDICOMJSONRefusesWhatItCannotMake: a value held by URI, more
// and an object with no SOP Class UID.
func TestCreateFromDICOMJSONRefusesWhatItCannotMake(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct{ body, want string }{
		"bulk": {`{"00080016":{"vr":"UI","Value":["1.2.840.10008.5.1.4.1.1.7"]},"7FE00010":{"vr":"OB","BulkDataURI":"http://x/1"}}`, "BulkDataURI"},
		"none": {`{"00100010":{"vr":"PN","Value":[{"Alphabetic":"Doe^Jane"}]}}`, "SOP Class UID"},
	} {
		in := filepath.Join(dir, name+".json")
		if err := os.WriteFile(in, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, name+".dcm")
		if err := runCreate(in, out); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a refusal naming %q", name, err, c.want)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: a file was written", name)
		}
	}
}

// writePNG writes img as a PNG and returns its path.
func writePNG(t *testing.T, dir, name string, img image.Image) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCreateSecondaryCaptureFromPNG: gray at 8 and 16 bits and color come out
// as their samples, described as the Image Pixel module says, with what the
// Secondary Capture IOD requires; a picture with transparency is refused.
func TestCreateSecondaryCaptureFromPNG(t *testing.T) {
	dir := t.TempDir()
	gray := image.NewGray(image.Rect(0, 0, 5, 3))
	gray16 := image.NewGray16(image.Rect(0, 0, 5, 3))
	rgb := image.NewNRGBA(image.Rect(0, 0, 5, 3))
	for i := range gray.Pix {
		gray.Pix[i] = byte(i * 17)
	}
	for i := 0; i < 15; i++ {
		gray16.SetGray16(i%5, i/5, color.Gray16{Y: uint16(i * 4000)})
		rgb.SetNRGBA(i%5, i/5, color.NRGBA{R: byte(i), G: byte(100 + i), B: byte(200 + i), A: 255})
	}
	for _, c := range []struct {
		name, photometric string
		img               image.Image
		samples, bits     int
		first             []byte
	}{
		{"gray.png", "MONOCHROME2", gray, 1, 8, []byte{0, 17}},
		{"gray16.png", "MONOCHROME2", gray16, 1, 16, []byte{0, 0, 0xA0, 0x0F}},
		{"rgb.png", "RGB", rgb, 3, 8, []byte{0, 100, 200, 1, 101, 201}},
	} {
		in := writePNG(t, dir, c.name, c.img)
		out := in + ".dcm"
		if err := runCreate("-patient-id", "P1", in, out); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		syntax, ds := createdFile(t, out)
		if syntax != "1.2.840.10008.1.2.1" {
			t.Errorf("%s: syntax %s", c.name, syntax)
		}
		for at, want := range map[tag.Tag]string{
			tagSOPClassUID:          secondaryCaptureClass,
			tag.New(0x0008, 0x0060): "OT",
			tag.New(0x0008, 0x0064): "WSD",
			tag.New(0x0028, 0x0004): c.photometric,
			tag.New(0x0010, 0x0020): "P1",
		} {
			if got := text(ds, at); got != want {
				t.Errorf("%s: %s = %q, want %q", c.name, at, got, want)
			}
		}
		for _, at := range []tag.Tag{tagStudyUID, tag.New(0x0020, 0x000E), tagSOPInstanceUID} {
			if !strings.HasPrefix(text(ds, at), "2.25.") {
				t.Errorf("%s: %s = %q, want a generated UID", c.name, at, text(ds, at))
			}
		}
		for _, at := range []tag.Tag{tagPatientName, tag.New(0x0010, 0x0030), tag.New(0x0008, 0x0090), tag.New(0x0020, 0x0010), tag.New(0x0008, 0x0050)} {
			if !ds.Contains(at) {
				t.Errorf("%s: the Type 2 attribute %s is absent", c.name, at)
			}
		}
		pixels := elementBytes(ds, tag.New(0x7FE0, 0x0010))
		if want := 5 * 3 * c.samples * c.bits / 8; len(pixels) < want || !bytes.HasPrefix(pixels, c.first) {
			t.Errorf("%s: Pixel Data is %d bytes beginning % x, want %d beginning % x", c.name, len(pixels), pixels[:min(6, len(pixels))], want, c.first)
		}
	}

	clear := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	in := writePNG(t, dir, "clear.png", clear)
	if err := runCreate(in, in+".dcm"); err == nil || !strings.Contains(err.Error(), "transparen") {
		t.Errorf("a transparent picture: %v", err)
	}
}

// TestCreateSecondaryCaptureFromJPEG: the JPEG goes in as it is, one
// fragment holding its every byte, in JPEG Baseline, described by its frame
// header and recorded as lossily compressed; a progressive JPEG is refused.
func TestCreateSecondaryCaptureFromJPEG(t *testing.T) {
	dir := t.TempDir()
	color := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for i := range color.Pix {
		color.Pix[i] = byte(i * 7)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, color, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(in, encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "photo.dcm")
	if err := runCreate(in, out); err != nil {
		t.Fatal(err)
	}
	syntax, ds := createdFile(t, out)
	if syntax != "1.2.840.10008.1.2.4.50" {
		t.Errorf("syntax %s, want JPEG Baseline", syntax)
	}
	// Go's encoder subsamples chroma 4:2:0.
	if got := text(ds, tag.New(0x0028, 0x0004)); got != "YBR_FULL_422" {
		t.Errorf("Photometric Interpretation %q", got)
	}
	if text(ds, tag.New(0x0028, 0x2110)) != "01" || text(ds, tag.New(0x0028, 0x2114)) != "ISO_10918_1" {
		t.Error("the loss is not recorded")
	}
	frame, err := ds.GetEncapsulatedFrame(0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimRight(frame, "\x00"), bytes.TrimRight(encoded.Bytes(), "\x00")) {
		t.Error("the fragment is not the JPEG's bytes")
	}
	if rows := elementBytes(ds, tag.New(0x0028, 0x0010)); binary.LittleEndian.Uint16(rows) != 8 {
		t.Errorf("Rows %d, want 8", binary.LittleEndian.Uint16(rows))
	}

	// SOI, then a progressive frame header (SOF2).
	progressive := filepath.Join(dir, "progressive.jpg")
	if err := os.WriteFile(progressive, []byte{0xFF, 0xD8, 0xFF, 0xC2, 0x00, 0x0B, 8, 0, 8, 0, 16, 1, 1, 0x11, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCreate(progressive, progressive+".dcm"); err == nil || !strings.Contains(err.Error(), "progressive") {
		t.Errorf("a progressive JPEG: %v", err)
	}
}

// TestCreateEncapsulatedPDF: the document's bytes are the value, padded to
// an even length with its own length beside it, and what the Encapsulated
// Document module requires is there.
func TestCreateEncapsulatedPDF(t *testing.T) {
	dir := t.TempDir()
	body := []byte("%PDF-1.4\n%%EOF\n") // 15 bytes, odd
	in := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(in, body, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report.dcm")
	if err := runCreate("-burned-in", "no", in, out); err != nil {
		t.Fatal(err)
	}
	_, ds := createdFile(t, out)
	value := elementBytes(ds, tag.New(0x0042, 0x0011))
	if len(value) != len(body)+1 || !bytes.HasPrefix(value, body) {
		t.Errorf("Encapsulated Document is %d bytes, want the %d of the PDF and one of padding", len(value), len(body))
	}
	if n := binary.LittleEndian.Uint32(elementBytes(ds, tag.New(0x0042, 0x0015))); n != uint32(len(body)) {
		t.Errorf("Encapsulated Document Length %d, want %d", n, len(body))
	}
	for at, want := range map[tag.Tag]string{
		tagSOPClassUID:          encapsulatedPDFClass,
		tag.New(0x0042, 0x0012): "application/pdf",
		tag.New(0x0028, 0x0301): "NO",
		tag.New(0x0008, 0x0060): "DOC",
		tag.New(0x0042, 0x0010): "report",
	} {
		if got := text(ds, at); got != want {
			t.Errorf("%s = %q, want %q", at, got, want)
		}
	}
	notPDF := filepath.Join(dir, "note.pdf")
	if err := os.WriteFile(notPDF, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCreate(notPDF, notPDF+".dcm"); err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Errorf("a file that is not a PDF: %v", err)
	}
}

// TestCreateJoinsTheTemplatesStudy: -template gives the patient and the
// study, a flag replaces one of them, and a value its VR does not allow is
// refused before anything is written.
func TestCreateJoinsTheTemplatesStudy(t *testing.T) {
	dir := t.TempDir()
	template := fixture(t)
	_, tmpl := readAnonymized(t, template)
	in := writePNG(t, dir, "shot.png", image.NewGray(image.Rect(0, 0, 2, 2)))
	out := filepath.Join(dir, "shot.dcm")
	if err := runCreate("-template", template, "-patient-name", "Doe^Jane", in, out); err != nil {
		t.Fatal(err)
	}
	_, ds := createdFile(t, out)
	if got, want := text(ds, tagStudyUID), text(tmpl, tagStudyUID); got != want {
		t.Errorf("Study Instance UID %q, want the template's %q", got, want)
	}
	if got := text(ds, tagPatientName); got != "Doe^Jane" {
		t.Errorf("Patient's Name %q, want the flag's", got)
	}
	if text(ds, tag.New(0x0020, 0x000E)) == text(tmpl, tag.New(0x0020, 0x000E)) {
		t.Error("the new instance took the template's series")
	}
	bad := filepath.Join(dir, "bad.dcm")
	if err := runCreate("-birth-date", "1990-01-01", in, bad); err == nil || !strings.Contains(err.Error(), "birth-date") {
		t.Errorf("a malformed birth date: %v", err)
	}
	if _, err := os.Stat(bad); err == nil {
		t.Error("a refused value left a file")
	}
}

// dicomJSONOf writes a corpus file as DICOM JSON, as `convert --format
// dicom-json` does, and returns the JSON object and the file's data set.
func dicomJSONOf(t *testing.T, source string) (map[string]any, *dataset.Dataset) {
	t.Helper()
	js := filepath.Join(t.TempDir(), "x.json")
	if err := (&ConvertCommand{}).Execute([]string{source, js, "--format", "dicom-json"}); err != nil {
		t.Fatalf("convert %s: %v", source, err)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	_, ds := readAnonymized(t, source)
	return object, ds
}

// sameAsJSON fails the test unless two data sets are the same as DICOM JSON,
// which holds every element and every binary value.
func sameAsJSON(t *testing.T, label string, got, want *dataset.Dataset) {
	t.Helper()
	g, err := got.ToDICOMJSON()
	if err != nil {
		t.Fatal(err)
	}
	w, err := want.ToDICOMJSON()
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(g)
	wantJSON, _ := json.Marshal(w)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Errorf("%s: the created data set differs from the source:\n%.300s\n---\n%.300s", label, gotJSON, wantJSON)
	}
}

// TestCreateMakesOneFilePerObjectOfAnArray: an array of three objects, as a
// DICOMweb metadata response is, makes three files named by SOP Instance
// UID, each the data set it came from; a name that two objects would share,
// an output that is a file, and an object with no SOP Class UID refuse the
// whole array with nothing written.
func TestCreateMakesOneFilePerObjectOfAnArray(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	dir := t.TempDir()
	var objects []map[string]any
	originals := map[string]*dataset.Dataset{}
	for _, name := range []string{"CT_small.dcm", "MR_small.dcm", "rtplan.dcm"} {
		object, ds := dicomJSONOf(t, filepath.Join(corpus, name))
		objects = append(objects, object)
		originals[text(ds, tagSOPInstanceUID)] = ds
	}
	writeJSON := func(name string, v any) string {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	in := writeJSON("array.json", objects)
	out := filepath.Join(dir, "out")
	if err := runCreate(in, out); err != nil {
		t.Fatalf("create: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 3 {
		t.Fatalf("the output holds %d entries (%v), want 3 files", len(entries), err)
	}
	for sopUID, original := range originals {
		_, made := createdFile(t, filepath.Join(out, sopUID+".dcm"))
		sameAsJSON(t, sopUID, made, original)
	}

	refuse := func(label, source, output, want string) {
		t.Helper()
		err := runCreate(source, output)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want a refusal naming %q", label, err, want)
		}
		if _, statErr := os.Stat(output); statErr == nil {
			t.Errorf("%s: %s was written", label, output)
		}
	}
	refuse("the same UID twice", writeJSON("twice.json", []any{objects[0], objects[0]}), filepath.Join(dir, "twice"), "same SOP Instance UID")
	noClass := map[string]any{"00080018": map[string]any{"vr": "UI", "Value": []any{"2.25.7"}}}
	refuse("an object with no SOP Class UID", writeJSON("noclass.json", []any{objects[0], noClass}), filepath.Join(dir, "noclass"), "object 2: ")
	refuse("a UID that is a path", writeJSON("path.json", []any{objects[0],
		map[string]any{"00080016": objects[1]["00080016"], "00080018": map[string]any{"vr": "UI", "Value": []any{"../x"}}}}), filepath.Join(dir, "path"), "cannot name a file")
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCreate(in, file); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("an array into a file: %v", err)
	}
	if err := runCreate(writeJSON("empty.json", []any{}), filepath.Join(dir, "empty")); err == nil || !strings.Contains(err.Error(), "no objects") {
		t.Errorf("an empty array: %v", err)
	}

	// An array of one object is as it was: the output is the file.
	single := filepath.Join(dir, "single.dcm")
	if err := runCreate(writeJSON("one.json", []any{objects[0]}), single); err != nil {
		t.Fatalf("an array of one: %v", err)
	}
	if _, made := createdFile(t, single); text(made, tagSOPInstanceUID) == "" {
		t.Error("the file made from an array of one has no SOP Instance UID")
	}
}

// TestCreateReadsABulkDataURIFromALocalFile: Pixel Data held at a BulkDataURI
// that is a relative path, a file: URI with a relative path and a file: URI
// with an absolute one comes back as the data set it was taken from; so does
// a value inside a sequence item. A network address is refused by name, as
// is a file outside the directory (by a path, by a file: URI, by a symbolic
// link), a value that is not binary, and a byte range.
func TestCreateReadsABulkDataURIFromALocalFile(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	object, original := dicomJSONOf(t, filepath.Join(corpus, "CT_small.dcm"))
	dir := t.TempDir()
	pixels := rawValue(original, tag.New(0x7FE0, 0x0010))
	if err := os.WriteFile(filepath.Join(dir, "pixels.raw"), pixels, 0o644); err != nil {
		t.Fatal(err)
	}
	element := object["7FE00010"].(map[string]any)
	if _, ok := element["InlineBinary"]; !ok {
		t.Fatal("the JSON has no inline Pixel Data to move out")
	}
	bulk := func(uri string) string {
		moved := map[string]any{}
		for k, v := range object {
			moved[k] = v
		}
		moved["7FE00010"] = map[string]any{"vr": element["vr"], "BulkDataURI": uri}
		data, _ := json.Marshal(moved)
		path := filepath.Join(dir, "bulk.json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	absolute := "file://" + filepath.ToSlash(filepath.Join(dir, "pixels.raw"))
	if filepath.VolumeName(dir) != "" {
		absolute = "file:///" + filepath.ToSlash(filepath.Join(dir, "pixels.raw"))
	}
	for _, uri := range []string{"pixels.raw", "file:pixels.raw", "./pixels.raw", absolute} {
		out := filepath.Join(t.TempDir(), "out.dcm")
		if err := runCreate(bulk(uri), out); err != nil {
			t.Errorf("BulkDataURI %q: %v", uri, err)
			continue
		}
		_, made := createdFile(t, out)
		sameAsJSON(t, uri, made, original)
		if !bytes.Equal(rawValue(made, tag.New(0x7FE0, 0x0010)), pixels) {
			t.Errorf("BulkDataURI %q: Pixel Data is not the file's bytes", uri)
		}
	}

	refused := []struct{ uri, want string }{
		{"http://pacs.example/studies/1/bulkdata/7FE00010", "does not fetch"},
		{"https://pacs.example/bulk", "reach the network"},
		{"ftp://host/x", "ftp"},
		{"file://other-host/pixels.raw", "other-host"},
		{"pixels.raw?range=0-9", "byte range"},
		{"../pixels.raw", "outside"},
		{"file:///etc/hosts", "outside"},
		{"absent.raw", "absent.raw"},
	}
	secret := t.TempDir()
	if err := os.WriteFile(filepath.Join(secret, "secret"), []byte("not for the file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link")); err == nil {
		refused = append(refused, struct{ uri, want string }{"link/secret", "outside"})
	}
	for _, c := range refused {
		out := filepath.Join(t.TempDir(), "out.dcm")
		err := runCreate(bulk(c.uri), out)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "BulkDataURI") {
			t.Errorf("BulkDataURI %q: %v, want a refusal naming BulkDataURI and %q", c.uri, err, c.want)
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Errorf("BulkDataURI %q: a file was written", c.uri)
		}
	}

	// A file outside the JSON's directory is read when -bulk-dir says so,
	// and a relative path then counts from there.
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "other.raw"), pixels, 0o644); err != nil {
		t.Fatal(err)
	}
	in := bulk("other.raw")
	if err := runCreate(in, filepath.Join(t.TempDir(), "o.dcm")); err == nil {
		t.Error("a file that is not in the JSON's directory was read without -bulk-dir")
	}
	out := filepath.Join(t.TempDir(), "out.dcm")
	if err := runCreate("-bulk-dir", elsewhere, in, out); err != nil {
		t.Fatalf("-bulk-dir: %v", err)
	}
	if _, made := createdFile(t, out); !bytes.Equal(rawValue(made, tag.New(0x7FE0, 0x0010)), pixels) {
		t.Error("-bulk-dir: Pixel Data is not the file's bytes")
	}

	// A value of a VR that is not binary is not read from a file.
	text := `{"00080016":{"vr":"UI","Value":["1.2.840.10008.5.1.4.1.1.7"]},"00100010":{"vr":"PN","BulkDataURI":"pixels.raw"}}`
	textIn := filepath.Join(dir, "text.json")
	if err := os.WriteFile(textIn, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCreate(textIn, filepath.Join(t.TempDir(), "o.dcm")); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Errorf("a BulkDataURI of a PN: %v", err)
	}
}
