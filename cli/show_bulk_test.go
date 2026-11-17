package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// The table prints a hundred bytes of a value at most, and read the whole
// of every one to do it (#274).
func TestShowTableLeavesBulkValuesInTheFile(t *testing.T) {
	pixels := make([]byte, 3000)
	for i := range pixels {
		pixels[i] = byte(i)
	}
	path := writeInstance(t, t.TempDir(), "a.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.274.1", "1.2.840.10008.1.2.1", pixels)

	elements, err := readDICOMElements(path, true, false)
	if err != nil {
		t.Fatal(err)
	}
	last := elements[len(elements)-1]
	if last.Tag != "7FE0,0010" || last.Length != 3000 || len(last.Value) != shownBytes {
		t.Fatalf("Pixel Data is listed with %d of %d bytes held", len(last.Value), last.Length)
	}
	if last.Value[5] != 5 || last.display(shownBytes) != "<100 bytes>... (3000 bytes)" {
		t.Errorf("Pixel Data is shown as %q, starting % x", last.display(shownBytes), last.Value[:8])
	}

	// What converts the file still has the whole value.
	whole, err := readDICOMFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := whole[len(whole)-1]; len(got.Value) != 3000 || got.Length != 3000 {
		t.Errorf("convert would get %d bytes of Pixel Data", len(got.Value))
	}
}

// Every row of the table is what it was when every value was read.
func TestShowTableIsTheSameWithoutReadingBulkData(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	compared, held, total := 0, 0, 0
	for _, path := range paths {
		whole, err := readDICOMFile(path)
		if err != nil {
			continue
		}
		light, err := readDICOMElements(path, true, false)
		if err != nil {
			t.Errorf("%s lists in full and not for display: %v", filepath.Base(path), err)
			continue
		}
		// The group length leads the display list alone.
		if len(light) > 0 && light[0].Tag == "0002,0000" {
			light = light[1:]
		}
		if len(light) != len(whole) {
			t.Errorf("%s: %d rows, %d when every value is read", filepath.Base(path), len(light), len(whole))
			continue
		}
		for i, w := range whole {
			l := light[i]
			held, total = held+len(l.Value), total+len(w.Value)
			if l.Tag != w.Tag || l.VR != w.VR || l.Depth != w.Depth || l.Length != w.Length ||
				l.display(shownBytes) != w.display(shownBytes) || l.display(50) != w.display(50) {
				t.Errorf("%s %s: %s %d %q, and %s %d %q when every value is read", filepath.Base(path), l.Tag,
					l.VR, l.Length, l.display(shownBytes), w.VR, w.Length, w.display(shownBytes))
				break
			}
		}
		compared++
	}
	if compared < 60 || held*4 > total {
		t.Errorf("%d files compared; %d of %d bytes were held", compared, held, total)
	}
}
