package attributes_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filereader/attributes"
	"github.com/amrshadid/go-dicom/tag"
)

// jsonCorpus returns the files the JSON writers are compared on: pydicom's
// corpus, wherever it sits under GODICOM_PYDICOM_DATA, and every .dcm this
// repository carries as test data. It skips when the corpus is not set, as
// the other corpus tests do.
func jsonCorpus(t *testing.T) []string {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	var paths []string
	for _, root := range []string{dir, filepath.Join("..", "..")} {
		extensionless := root == dir
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Files of the DICOMDIR tests have no extension, and the corpus
			// directory also holds a README and files of other formats, which
			// are not read.
			ext := strings.ToLower(filepath.Ext(path))
			if !d.IsDir() && (ext == ".dcm" || extensionless && ext == "" && !strings.Contains(filepath.Base(path), "README")) {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

// For every file of the corpus and of this repository's test data, the JSON
// attributes.DICOMJSON writes from the elements Read returns is, byte for byte,
// the JSON dataset writes from the data set GetDataset builds from the same
// file, and a file one refuses the other refuses. They share one
// implementation; this is what stops a later change to either side from
// splitting them: the decoding of text under the Specific Character Set, the
// VR an element is written with, private creators, nested sequences, and the
// number, person name, AT and base64 forms.
func TestDICOMJSONIsWhatDatasetWritesForEveryFile(t *testing.T) {
	equal, refused := 0, 0
	paths := jsonCorpus(t)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		full, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
		if err != nil {
			continue // unreadable to both: TestReadAgrees... holds them to that
		}
		light, err := attributes.Read(filebase.NewFileReader(bytes.NewReader(data)), attributes.ReadOptions{})
		if err != nil {
			t.Fatalf("%s: attributes.Read: %v", path, err)
		}

		want, wantErr := full.GetDataset().ToDICOMJSONString()
		doc, gotErr := attributes.DICOMJSON(light.DataElements, attributes.JSONOptions{})
		var got []byte
		if gotErr == nil {
			got, gotErr = json.Marshal(doc)
		}
		if (wantErr == nil) != (gotErr == nil) {
			t.Errorf("%s: dataset: %v; attributes: %v", path, wantErr, gotErr)
			continue
		}
		if wantErr != nil {
			refused++
			continue
		}
		if string(got) != want {
			t.Errorf("%s: the JSON differs\n dataset:    %.300s\n attributes: %.300s", path, want, got)
			continue
		}
		equal++
	}
	t.Logf("%d files compared, %d byte-equal, %d refused by both", len(paths), equal, refused)
	if equal < 80 {
		t.Errorf("only %d files were compared byte-equal", equal)
	}
}

// The bulk data hook does what dataset's threshold option does when it is
// given the same rule: the same document, byte for byte, on every file. The
// threshold option is a rule over the hook now, so this holds the two ways of
// asking to one answer.
func TestBulkDataHookAgreesWithDatasetsThreshold(t *testing.T) {
	const threshold = 256
	uri := func(tg tag.Tag) string { return fmt.Sprintf("bulk/%08X", uint32(tg)) }
	compared, held := 0, 0
	for _, path := range jsonCorpus(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		full, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
		if err != nil {
			continue
		}
		light, err := attributes.Read(filebase.NewFileReader(bytes.NewReader(data)), attributes.ReadOptions{})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		want, err := full.GetDataset().ToDICOMJSONWithOptions(datasetThreshold(threshold, uri))
		if err != nil {
			continue
		}
		doc, err := attributes.DICOMJSON(light.DataElements, attributes.JSONOptions{
			BulkData: func(v attributes.BulkDataValue) attributes.BulkDataDecision {
				switch v.VR {
				case "OB", "OD", "OF", "OL", "OV", "OW", "UN":
					if v.Length >= threshold {
						held++
						return attributes.BulkDataDecision{URI: uri(v.Tag)}
					}
				}
				return attributes.BulkDataDecision{}
			},
		})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(doc)
		if !bytes.Equal(a, b) {
			t.Errorf("%s: the JSON differs\n dataset:    %.300s\n attributes: %.300s", path, a, b)
		}
		compared++
	}
	t.Logf("%d files compared, %d values held out of line", compared, held)
	if compared < 80 || held == 0 {
		t.Errorf("%d files compared, %d held out", compared, held)
	}
}

// A file read with SkipBulkData has no bytes for its pixels. The hook is given
// where they are, the offset and length of the value in the file, and a URI
// built from them is written; the bytes at that place are the pixels a whole
// read holds. Without a decision for the value there is nothing to write, and
// DICOMJSON says so instead of writing an element that looks complete.
func TestBulkDataHookIsGivenWhereAValueLeftInTheFileIs(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	data, err := os.ReadFile(filepath.Join(dir, "CT_small.dcm"))
	if err != nil {
		t.Fatal(err)
	}
	whole, err := attributes.Read(filebase.NewFileReader(bytes.NewReader(data)), attributes.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var pixels []byte
	for _, e := range whole.DataElements {
		if e.Tag == tag.New(0x7FE0, 0x0010) {
			pixels = e.Value
		}
	}
	if len(pixels) == 0 {
		t.Fatal("CT_small.dcm has no pixel data")
	}

	skipped, err := attributes.Read(filebase.NewFileReader(bytes.NewReader(data)),
		attributes.ReadOptions{SkipBulkData: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := attributes.DICOMJSON(skipped.DataElements, attributes.JSONOptions{}); err == nil ||
		!strings.Contains(err.Error(), "left in the file") || !strings.Contains(err.Error(), "neither") {
		t.Fatalf("a value left in the file with no decision: err = %v", err)
	}

	var seen attributes.BulkDataValue
	doc, err := attributes.DICOMJSON(skipped.DataElements, attributes.JSONOptions{
		BulkData: func(v attributes.BulkDataValue) attributes.BulkDataDecision {
			if v.InFile {
				if v.Tag == tag.New(0x7FE0, 0x0010) {
					seen = v
				}
				return attributes.BulkDataDecision{URI: fmt.Sprintf("file:%d+%d", v.Offset, v.Length)}
			}
			return attributes.BulkDataDecision{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.Tag != tag.New(0x7FE0, 0x0010) || seen.Value != nil || len(seen.Path) != 0 {
		t.Errorf("the hook saw %+v", seen)
	}
	if seen.Length != int64(len(pixels)) || !bytes.Equal(data[seen.Offset:seen.Offset+seen.Length], pixels) {
		t.Errorf("offset %d length %d is not where the %d bytes of pixels are", seen.Offset, seen.Length, len(pixels))
	}
	if got, want := doc["7FE00010"].BulkDataURI, fmt.Sprintf("file:%d+%d", seen.Offset, seen.Length); got != want {
		t.Errorf("BulkDataURI = %q, want %q", got, want)
	}
	if got := doc["7FE00010"].VR; got != "OW" {
		t.Errorf("VR = %q, want OW", got)
	}
	if doc["00100010"].Value == nil {
		t.Error("the rest of the data set is not written")
	}

	// Omit leaves the element out and nothing else.
	omitted, err := attributes.DICOMJSON(skipped.DataElements, attributes.JSONOptions{
		BulkData: func(v attributes.BulkDataValue) attributes.BulkDataDecision {
			if v.Tag == tag.New(0x7FE0, 0x0010) {
				return attributes.BulkDataDecision{Omit: true}
			}
			return attributes.BulkDataDecision{URI: "elsewhere"} // the other values left in the file
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := omitted["7FE00010"]; present || len(omitted) != len(doc)-1 {
		t.Errorf("omitting the pixels left %d elements, pixels present = %v; want %d without them",
			len(omitted), present, len(doc)-1)
	}
}

// datasetThreshold is dataset's way of asking for the same rule.
func datasetThreshold(n int, uri func(tag.Tag) string) dataset.DICOMJSONOptions {
	return dataset.DICOMJSONOptions{BulkDataThreshold: n, BulkDataURIFunc: uri}
}

// implicit builds an implicit VR little endian element.
func implicit(group, element uint16, value []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, group)
	b = binary.LittleEndian.AppendUint16(b, element)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(value)))
	return append(b, value...)
}

// The hook is told which sequences an element is in, so that a URI for a value
// inside an item can say which one it is: two items of one sequence each have
// an element with the same tag.
func TestBulkDataHookIsGivenThePathOfANestedValue(t *testing.T) {
	item := func(pixels []byte) []byte {
		body := implicit(0x7FE0, 0x0010, pixels)
		return append(binary.LittleEndian.AppendUint32([]byte{0xFE, 0xFF, 0x00, 0xE0}, uint32(len(body))), body...)
	}
	sequence := append(item([]byte{1, 2, 3, 4}), item([]byte{5, 6, 7, 8, 9, 10})...)
	data := implicit(0x0088, 0x0200, sequence) // Icon Image Sequence

	file, err := attributes.Read(filebase.NewFileReader(bytes.NewReader(data)), attributes.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	doc, err := attributes.DICOMJSON(file.DataElements, attributes.JSONOptions{
		BulkData: func(v attributes.BulkDataValue) attributes.BulkDataDecision {
			p := v.Path[0]
			paths = append(paths, fmt.Sprintf("%08X/%d/%08X %s %d+%d", uint32(p.Sequence), p.Item,
				uint32(v.Tag), v.VR, v.Offset, v.Length))
			return attributes.BulkDataDecision{URI: "x"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"00880200/0/7FE00010 OW 24+4", "00880200/1/7FE00010 OW 44+6"}
	if strings.Join(paths, "|") != strings.Join(want, "|") {
		t.Errorf("hook calls = %q, want %q", paths, want)
	}
	items := doc["00880200"].Value
	if len(items) != 2 {
		t.Fatalf("%d items", len(items))
	}
	if got := items[1].(map[string]attributes.JSONElement)["7FE00010"].BulkDataURI; got != "x" {
		t.Errorf("BulkDataURI of the second item = %q", got)
	}
}

// What the package promises, that a program using it does not link the codecs,
// has to include the writer: the closure of the package lists the JSON
// implementation, and none of dataset, dataelem or the codecs. The test of
// the closure itself is TestTheCodecsAreNotLinked.
func TestTheJSONWriterIsInsideTheCodecFreeClosure(t *testing.T) {
	const module = "github.com/amrshadid/go-dicom/"
	out, err := exec.Command("go", "list", "-deps", module+"filereader/attributes").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	listed := "\n" + string(out)
	if !strings.Contains(listed, "\n"+module+"internal/dicomjson\n") {
		t.Errorf("the JSON writer, internal/dicomjson, is not in the closure of filereader/attributes:\n%s", out)
	}
	for _, p := range []string{"dataset", "dataelem", "compress", "image/jpeg"} {
		if strings.Contains(listed, "/"+p+"\n") || strings.Contains(listed, "\n"+p+"\n") {
			t.Errorf("%s is in the closure", p)
		}
	}
}
