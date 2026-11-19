package heldvalue_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/sequence"
)

// TestTheCopyStandsForTheSameBytes proves that every value of the copy, over
// pydicom's corpus, stands for the bytes the file held, and that the copy does
// hold values other than []byte. The tests that use the copy compare a package
// against itself on a file; a copy that changed a value would make them
// compare two different data sets.
func TestTheCopyStandsForTheSameBytes(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	held := map[string]int{}
	files := 0
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
		_ = f.Close()
		if err != nil {
			continue
		}
		files++
		compare(t, filepath.Base(path), df.GetDataset(), heldvalue.AsGoValues(df.GetDataset()), held)
	}
	if files == 0 {
		t.Fatal("no corpus file was read")
	}
	for _, kind := range []string{"string", "[]string", "uint16", "[]uint16", "uint32", "float64"} {
		if held[kind] == 0 {
			t.Errorf("no value was held as %s; the copy does not exercise it", kind)
		}
	}
	t.Logf("%d files; values held as %v", files, held)
}

func compare(t *testing.T, file string, want, got *dataset.Dataset, held map[string]int) {
	t.Helper()
	w, g := want.GetAll(), got.GetAll()
	if len(w) != len(g) {
		t.Fatalf("%s: %d elements, the copy has %d", file, len(w), len(g))
	}
	for i := range w {
		tw, _ := w[i].Tag()
		if seq, ok := w[i].GetValue().(*sequence.Sequence); ok {
			copied, isSeq := g[i].GetValue().(*sequence.Sequence)
			if !isSeq || copied.Length() != seq.Length() {
				t.Fatalf("%s %s: the sequence was not copied item for item", file, tw)
			}
			for j, item := range seq.Items() {
				inner, _ := item.(*dataset.Dataset)
				innerCopy, _ := copied.Items()[j].(*dataset.Dataset)
				if inner != nil && innerCopy != nil {
					compare(t, file, inner, innerCopy, held)
				}
			}
			continue
		}
		wb, wok := w[i].Bytes()
		gb, gok := g[i].Bytes()
		if wok != gok || !bytes.Equal(wb, gb) {
			t.Errorf("%s %s: the file holds % x, the copy (%T) stands for % x", file, tw, wb, g[i].GetValue(), gb)
		}
		held[typeName(g[i].GetValue())]++
	}
}

func typeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case []string:
		return "[]string"
	case uint16:
		return "uint16"
	case []uint16:
		return "[]uint16"
	case uint32:
		return "uint32"
	case float64:
		return "float64"
	case []byte:
		return "[]byte"
	}
	return "other"
}
