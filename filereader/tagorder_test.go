package filereader_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// PS3.5 7.1 puts the data elements of a data set in increasing tag order,
// each tag once. A file that does not is read as it is, and the reader says
// so in the file's warnings, the way it says an element has no VR.

// orderWarnings are the warnings of a read about the order of tags.
func orderWarnings(file *filereader.DICOMFile) []string {
	var out []string
	for _, w := range file.Warnings {
		if strings.Contains(w, "tag order") {
			out = append(out, w)
		}
	}
	return out
}

// explicitDataSet joins explicit VR elements.
func explicitDataSet(elements ...[]byte) []byte {
	return bytes.Join(elements, nil)
}

func tagElement(group, element uint16) []byte {
	return explicitElement(group, element, "LO", []byte("AB"))
}

// TestRepeatedTagIsWarnedOf proves a top-level tag that repeats the one
// before it is warned of, naming the tag and the offset of the second, and
// that both elements are kept.
func TestRepeatedTagIsWarnedOf(t *testing.T) {
	file := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(
		tagElement(0x0008, 0x0070),
		tagElement(0x0010, 0x0020),
		tagElement(0x0010, 0x0020),
		tagElement(0x0010, 0x0030),
	), false))

	got := orderWarnings(file)
	if len(got) != 1 {
		t.Fatalf("%d order warnings, want 1: %q", len(got), file.Warnings)
	}
	offset := file.DataElements[2].Position.Offset
	for _, want := range []string{"(0010,0020)", "repeats", fmt.Sprintf("offset %d", offset), "PS3.5 7.1"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("warning %q does not say %q", got[0], want)
		}
	}
	if len(file.DataElements) != 4 {
		t.Errorf("%d elements read, want all 4 kept", len(file.DataElements))
	}
}

// TestDecreasingTagIsWarnedOf proves a tag lower than the one before it is
// warned of, naming both tags and the offset, and nothing is reordered.
func TestDecreasingTagIsWarnedOf(t *testing.T) {
	file := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(
		tagElement(0x0010, 0x0020),
		tagElement(0x0008, 0x0070),
		tagElement(0x0010, 0x0030),
	), false))

	got := orderWarnings(file)
	if len(got) != 1 {
		t.Fatalf("%d order warnings, want 1: %q", len(got), file.Warnings)
	}
	offset := file.DataElements[1].Position.Offset
	for _, want := range []string{"(0008,0070)", "(0010,0020)", fmt.Sprintf("offset %d", offset), "follows"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("warning %q does not say %q", got[0], want)
		}
	}
	var order []string
	for _, e := range file.DataElements {
		order = append(order, e.Tag.String())
	}
	if strings.Join(order, " ") != "(0010,0020) (0008,0070) (0010,0030)" {
		t.Errorf("elements are %v, want them as read", order)
	}
}

// TestInOrderDataSetHasNoOrderWarning proves a conforming data set is not
// warned of.
func TestInOrderDataSetHasNoOrderWarning(t *testing.T) {
	file := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(
		tagElement(0x0008, 0x0070),
		tagElement(0x0010, 0x0020),
		tagElement(0x0010, 0x0030),
	), false))
	if got := orderWarnings(file); len(got) != 0 {
		t.Errorf("warnings for a data set in order: %q", got)
	}
}

// item encodes a sequence item of defined length holding the elements.
func item(elements ...[]byte) []byte {
	body := bytes.Join(elements, nil)
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, [2]uint16{0xFFFE, 0xE000})
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(body)))
	buf.Write(body)
	return buf.Bytes()
}

// sequenceOf encodes an explicit VR sequence of defined length.
func sequenceOf(group, element uint16, items ...[]byte) []byte {
	body := bytes.Join(items, nil)
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, [2]uint16{group, element})
	buf.WriteString("SQ")
	buf.Write([]byte{0, 0})
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(body)))
	buf.Write(body)
	return buf.Bytes()
}

// TestTagOrderIsCheckedInsideSequenceItems proves each item of a sequence is
// checked as a data set of its own: an item out of order is warned of as in a
// sequence item, a tag lower than the last of the previous item is not, and
// the order at the top level, around the sequence, is judged by the
// sequence's own tag.
func TestTagOrderIsCheckedInsideSequenceItems(t *testing.T) {
	file := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(
		tagElement(0x0008, 0x0070),
		sequenceOf(0x0008, 0x1111,
			item(tagElement(0x0010, 0x0020), tagElement(0x0010, 0x0030)),
			// Lower than where the first item ended, which is fine: it is
			// another data set. Its own two are not in order.
			item(tagElement(0x0010, 0x0030), tagElement(0x0010, 0x0020)),
		),
		tagElement(0x0010, 0x0010),
	), false))

	got := orderWarnings(file)
	if len(got) != 1 {
		t.Fatalf("%d order warnings, want 1: %q", len(got), file.Warnings)
	}
	for _, want := range []string{"(0010,0020)", "(0010,0030)", "sequence item"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("warning %q does not say %q", got[0], want)
		}
	}
}

// TestOrderWarningsAreCapped proves a file out of order throughout gets ten
// warnings and one line saying how many more, not a warning per element.
func TestOrderWarningsAreCapped(t *testing.T) {
	var elements [][]byte
	for e := uint16(40); e > 0; e -= 2 {
		elements = append(elements, tagElement(0x0010, e))
	}
	file := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(elements...), false))

	got := orderWarnings(file)
	if len(got) != 11 {
		t.Fatalf("%d order warnings, want 10 and a summary: %q", len(got), got)
	}
	if !strings.Contains(got[10], "9 more") || !strings.Contains(got[10], "19 in all") {
		t.Errorf("summary is %q, want 9 more of 19 in all", got[10])
	}
}

// TestTagOrderWarningsStayOutOfTheCorpus proves no file of pydicom's corpus
// gets an order warning, so the warning means a file that is out of order and
// is not noise. It is skipped without the corpus.
func TestTagOrderWarningsStayOutOfTheCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	read := 0
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
		_ = f.Close()
		if err != nil {
			continue // a file this reader refuses has no warnings to speak of
		}
		read++
		if got := orderWarnings(file); len(got) > 0 || file.TagOrderViolationCount != 0 {
			t.Errorf("%s: %d violations: %q", filepath.Base(path), file.TagOrderViolationCount, got)
		}
	}
	if read == 0 {
		t.Fatal("no file was read")
	}
	t.Logf("%d files read, none with a tag order warning", read)
}

// TestTagOrderViolationsAreTyped proves each element out of order is given to
// a program as a value, its tag, the tag before it, its offset, whether it is
// in an item and whether it repeats, so that no caller has to match on the
// words of a warning; and that the count goes on past the warnings' cap.
func TestTagOrderViolationsAreTyped(t *testing.T) {
	file := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(
		tagElement(0x0008, 0x0070),
		tagElement(0x0010, 0x0020),
		tagElement(0x0010, 0x0020),
		tagElement(0x0008, 0x0090),
		sequenceOf(0x0008, 0x1111,
			item(tagElement(0x0010, 0x0030), tagElement(0x0010, 0x0020)),
		),
	), false))

	want := []filereader.TagOrderViolation{
		{Tag: tag.New(0x0010, 0x0020), Previous: tag.New(0x0010, 0x0020), Repeated: true},
		{Tag: tag.New(0x0008, 0x0090), Previous: tag.New(0x0010, 0x0020)},
		{Tag: tag.New(0x0010, 0x0020), Previous: tag.New(0x0010, 0x0030), InItem: true},
	}
	got := file.TagOrderViolations
	if len(got) != len(want) || file.TagOrderViolationCount != len(want) {
		t.Fatalf("%d violations, count %d; want %d: %+v", len(got), file.TagOrderViolationCount, len(want), got)
	}
	for i := range want {
		g := got[i]
		g.Offset = 0
		if g != want[i] {
			t.Errorf("violation %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[0].Offset != file.DataElements[2].Position.Offset || got[1].Offset != file.DataElements[3].Position.Offset {
		t.Errorf("offsets %d and %d, want %d and %d", got[0].Offset, got[1].Offset,
			file.DataElements[2].Position.Offset, file.DataElements[3].Position.Offset)
	}

	var elements [][]byte
	for e := uint16(40); e > 0; e -= 2 {
		elements = append(elements, tagElement(0x0010, e))
	}
	many := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(elements...), false))
	if many.TagOrderViolationCount != 19 || len(many.TagOrderViolations) != 19 {
		t.Errorf("%d violations, count %d; want all 19, past the cap of ten warnings",
			len(many.TagOrderViolations), many.TagOrderViolationCount)
	}

	inOrder := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataSet(
		tagElement(0x0008, 0x0070), tagElement(0x0010, 0x0020)), false))
	if inOrder.TagOrderViolationCount != 0 || inOrder.TagOrderViolations != nil {
		t.Errorf("a data set in order has %d violations", inOrder.TagOrderViolationCount)
	}
}
