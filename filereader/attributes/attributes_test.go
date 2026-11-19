package attributes_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filereader/attributes"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// A program that read attributes through filereader linked compress,
// compress/jpeg2000, encaps, pixels and waveforms whether it decoded a pixel
// or not: 4.7 MB of an archive's binary.

// The packages a reader of attributes must not link. dataset and dataelem
// are what bring the codecs in, so they are kept out as well: either back in
// the closure would bring the rest with it.
var notLinked = []string{
	"compress", "compress/jpeg2000", "encaps", "pixels", "waveforms",
	"dataset", "dataelem",
}

// The dependency closure of this package, as go list gives it, holds none of
// the codec packages, so a change that brings one back fails here rather
// than in someone's binary.
func TestTheCodecsAreNotLinked(t *testing.T) {
	const module = "github.com/amrshadid/go-dicom/"
	out, err := exec.Command("go", "list", "-deps", module+"filereader/attributes").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	deps := map[string]bool{}
	for _, line := range strings.Fields(string(out)) {
		deps[line] = true
	}
	if !deps[module+"internal/filereader"] {
		t.Fatalf("go list did not list the parser, so its output is not what this test reads:\n%s", out)
	}
	for _, p := range notLinked {
		if deps[module+p] {
			t.Errorf("%s is in the dependency closure of filereader/attributes", p)
		}
	}
	var ours []string
	for p := range deps {
		if strings.HasPrefix(p, module) {
			ours = append(ours, strings.TrimPrefix(p, module))
		}
	}
	t.Logf("module packages linked: %v", ours)
}

// corpus returns pydicom's corpus files, or skips.
func corpus(t *testing.T) []string {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	more, _ := filepath.Glob(filepath.Join(dir, "*", "*.dcm"))
	return append(paths, more...)
}

// For every corpus file, what Read returns is what filereader returns, and
// its elements agree one for one with the data set GetDataset builds: the
// same tags in the same order, the same VRs, sequences with the same items,
// and every value the same, text as DecodeText gives it.
func TestReadAgreesWithTheFullReadAcrossThePydicomCorpus(t *testing.T) {
	checked, values := 0, 0
	for _, path := range corpus(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		full, fullErr := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
		light, lightErr := attributes.Read(filebase.NewFileReader(bytes.NewReader(data)), attributes.ReadOptions{})
		name, _ := filepath.Rel(filepath.Dir(filepath.Dir(path)), path)
		t.Run(name, func(t *testing.T) {
			if (fullErr == nil) != (lightErr == nil) {
				t.Fatalf("filereader: %v; attributes: %v", fullErr, lightErr)
			}
			if fullErr != nil {
				return
			}
			if !reflect.DeepEqual(light.DataElements, full.DataElements) ||
				!reflect.DeepEqual(light.MetaElements, full.MetaElements) ||
				!reflect.DeepEqual(light.FileMetaInfo, full.FileMetaInfo) ||
				light.ExplicitVR != full.ExplicitVR || light.IsLittleEndian != full.IsLittleEndian ||
				light.HasPreamble != full.HasPreamble || light.DataSetOffset != full.DataSetOffset ||
				light.Deflated != full.Deflated {
				t.Fatal("Read and filereader.ReadDICOMFile return different files")
			}
			values += agree(t, light.DataElements, nil, full.GetDataset())
			if !reflect.DeepEqual(light.Warnings, full.Warnings[:len(light.Warnings)]) {
				t.Errorf("warnings differ:\n%q\n%q", light.Warnings, full.Warnings)
			}
		})
		checked++
	}
	t.Logf("%d files, %d values compared", checked, values)
	if checked < 70 || values == 0 {
		t.Errorf("%d files, %d values compared", checked, values)
	}
}

// agree holds the elements of one data set to the data set GetDataset built
// from them, and returns how many values it compared.
func agree(t *testing.T, elems []*attributes.DataElementValue, inherited []string, ds *dataset.Dataset) int {
	t.Helper()
	encodings := attributes.TextEncodings(elems, inherited)
	built := ds.GetAll()
	// GetDataset declares UTF-8 when it decoded text the file gave no
	// character set for; that element is its own, not the file's.
	var added *dataelem.DataElement
	for _, e := range built {
		if e.GetTag() == tag.New(0x0008, 0x0005) && !hasTag(elems, tag.New(0x0008, 0x0005)) {
			added = e
		}
	}
	if added != nil {
		var rest []*dataelem.DataElement
		for _, e := range built {
			if e != added {
				rest = append(rest, e)
			}
		}
		built = rest
	}
	if len(built) != len(elems) {
		t.Fatalf("%d elements read, %d in the data set", len(elems), len(built))
	}
	compared := 0
	for i, e := range elems {
		b := built[i]
		if b.GetTag() != e.Tag {
			t.Fatalf("element %d: %s read, %v in the data set", i, e.Tag, b.GetTag())
		}
		if seq, ok := b.GetValue().(*sequence.Sequence); ok {
			items := seq.Items()
			if len(items) != len(e.Items) {
				t.Fatalf("%s: %d items read, %d in the data set", e.Tag, len(e.Items), len(items))
			}
			for j, item := range e.Items {
				compared += agree(t, item.Elements, encodings, items[j].(*dataset.Dataset))
			}
			continue
		}
		if string(b.GetVR()) != e.VR {
			t.Errorf("%s: VR %s read, %s in the data set", e.Tag, e.VR, b.GetVR())
		}
		value, ok := b.GetValue().([]byte)
		if !ok {
			t.Errorf("%s: the data set holds %T", e.Tag, b.GetValue())
			continue
		}
		if e.Tag == tag.New(0x0008, 0x0005) {
			// GetDataset relabels the declaration ISO_IR 192 once its text
			// is UTF-8; what the file declared is in the element.
			continue
		}
		want := e.Value
		if dataelem.IsTextVR(dataelem.VR(e.VR)) {
			want = []byte(attributes.DecodeText(e.VR, e.Value, encodings))
		}
		if !bytes.Equal(value, want) {
			t.Errorf("%s %s: read as %q, %q in the data set", e.Tag, e.VR, short(want), short(value))
		}
		compared++
	}
	return compared
}

func hasTag(elems []*attributes.DataElementValue, t tag.Tag) bool {
	for _, e := range elems {
		if e.Tag == t {
			return true
		}
	}
	return false
}

func short(b []byte) []byte {
	if len(b) > 40 {
		return b[:40]
	}
	return b
}
