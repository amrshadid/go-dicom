package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// info prints a few header fields and some sizes, and read every value of the
// file to do it (#328). The sizes it prints are the bytes in the file, which
// are no longer the length of what is held.
func TestInfoLeavesBulkValuesInTheFileAndReportsTheirSize(t *testing.T) {
	pixels := make([]byte, 3000)
	path := writeInstance(t, t.TempDir(), "a.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.328.1", "1.2.840.10008.1.2.1", pixels)

	elements, err := readDICOMElements(path, listing{skipBulk: true})
	if err != nil {
		t.Fatal(err)
	}
	last := elements[len(elements)-1]
	if last.Tag != "7FE0,0010" || len(last.Value) >= len(pixels) || last.Length != len(pixels) {
		t.Fatalf("Pixel Data is listed with %d of %d bytes held", len(last.Value), last.Length)
	}
	if elements[0].Tag == "0002,0000" {
		t.Error("info lists the File Meta Information Group Length, which it never did")
	}

	// Total Data Size counts the 3000 bytes of Pixel Data, not the hundred held.
	out, err := captured(t, func() error { return NewInfoCommand().Execute([]string{path, "--stats", "--verbose"}) })
	if err != nil {
		t.Fatal(err)
	}
	whole, err := readDICOMFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := captured(t, func() error {
		ic := NewInfoCommand()
		ic.displayStatistics(whole)
		ic.displayVerboseInfo(whole)
		return nil
	})
	if !strings.Contains(out, want) || !strings.Contains(out, "Total Data Size:") {
		t.Errorf("info --stats --verbose prints\n%s\nand with every value read\n%s", out, want)
	}
}

// What info prints is what it printed when every value was read, on every
// file of the corpus: the key elements, the totals and the element list.
func TestInfoOutputIsTheSameWithoutReadingBulkData(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	compared := 0
	for _, path := range paths {
		whole, err := readDICOMFile(path)
		if err != nil {
			continue
		}
		out, err := captured(t, func() error { return NewInfoCommand().Execute([]string{path, "--stats", "--verbose"}) })
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
			continue
		}
		want, _ := captured(t, func() error {
			ic := NewInfoCommand()
			ic.displayStatistics(whole)
			ic.displayVerboseInfo(whole)
			ic.displayDictionaryInfo(whole)
			return nil
		})
		if !strings.HasSuffix(out, want) {
			t.Errorf("%s: info prints\n%s\nand with every value read\n%s", filepath.Base(path), out, want)
		}
		compared++
	}
	if compared < 60 {
		t.Errorf("%d files compared", compared)
	}
}
