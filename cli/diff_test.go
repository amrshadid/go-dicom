package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// A command to compare two files was missing, so every check of a file
// against its anonymized or recompressed copy was a throwaway script (#329).

var (
	tagReferencedImages = tag.New(0x0008, 0x1140)
	tagReferencedSOP    = tag.New(0x0008, 0x1150)
	tagPixelData        = tag.New(0x7FE0, 0x0010)
)

// diffItem is an item of the Referenced Image Sequence the tests write.
func diffItem(t *testing.T, uid string) *dataset.Dataset {
	t.Helper()
	item := dataset.NewDataset()
	if err := item.Add(dataelem.NewDataElement(tagReferencedSOP, dataelem.UI, uid)); err != nil {
		t.Fatal(err)
	}
	return item
}

// writeDiffFixture writes a small file with a sequence of one item per uid,
// a name, and pixel data of the given bytes.
func writeDiffFixture(t *testing.T, path, name string, uids []string, pixels []byte, extra ...*dataelem.DataElement) {
	t.Helper()
	ds := dataset.NewDataset()
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value any
	}{
		{tagSOPClassUID, dataelem.UI, "1.2.840.10008.5.1.4.1.1.7"},
		{tagSOPInstanceUID, dataelem.UI, "1.2.3.4"},
		{tagPatientName, dataelem.PN, name},
		{tag.New(0x0010, 0x0020), dataelem.LO, "MRN1"},
	} {
		if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	seq := sequence.New()
	for _, uid := range uids {
		if err := seq.Append(diffItem(t, uid)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ds.AddSequence(tagReferencedImages, seq); err != nil {
		t.Fatal(err)
	}
	if pixels != nil {
		if err := ds.Add(dataelem.NewDataElement(tagPixelData, dataelem.OW, pixels)); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range extra {
		if err := ds.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.7", "1.2.3.4", ds); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// runDiff runs the command in the test process and returns what it printed
// and the exit status it asks for.
func runDiff(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	cmd := &DiffCommand{out: &out}
	err := cmd.Execute(args)
	if err == nil {
		return out.String(), 0
	}
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("diff returned %v, which carries no exit status", err)
	}
	if exit.Code == 2 && exit.Err == nil {
		t.Fatal("status 2 is an error and has to say what it was")
	}
	return out.String(), exit.Code
}

// decodeDiff reads the JSON document.
func decodeDiff(t *testing.T, text string) diffDocument {
	t.Helper()
	var doc diffDocument
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return doc
}

// find returns the entry at path.
func (d diffDocument) find(path string) *diffEntry {
	for i := range d.Differences {
		if d.Differences[i].Path == path {
			return &d.Differences[i]
		}
	}
	return nil
}

// Two equal files print nothing and are status 0, as diff(1) does; a copy
// that differs is status 1 and says where. Equal here includes the file
// meta group, which is compared too.
func TestDiffEqualFilesAreSilent(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.dcm"), filepath.Join(dir, "b.dcm")
	writeDiffFixture(t, a, "Doe^John", []string{"1.1", "1.2"}, []byte{1, 2, 3, 4})
	writeDiffFixture(t, b, "Doe^John", []string{"1.1", "1.2"}, []byte{1, 2, 3, 4})

	out, code := runDiff(t, a, b)
	if out != "" || code != 0 {
		t.Fatalf("equal files: status %d, output %q", code, out)
	}
	out, code = runDiff(t, "--json", a, b)
	doc := decodeDiff(t, out)
	if code != 0 || !doc.Equal || len(doc.Differences) != 0 || doc.SchemaVersion != 1 {
		t.Fatalf("equal files as JSON: status %d, %+v", code, doc)
	}
}

// Every kind of difference is found, at the depth it is at: an element in
// one file only, a changed value, a changed VR, an item of a sequence that
// one file has more of, an element inside an item, and a file meta element.
func TestDiffFindsEachKindOfDifference(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.dcm"), filepath.Join(dir, "b.dcm")
	writeDiffFixture(t, a, "Doe^John", []string{"1.1", "1.2"}, []byte{1, 2, 3, 4},
		dataelem.NewDataElement(tag.New(0x0010, 0x1010), dataelem.AS, "045Y"),
		dataelem.NewDataElement(tag.New(0x0020, 0x0013), dataelem.IS, "7"))
	writeDiffFixture(t, b, "Smith^Jane", []string{"1.1", "9.9", "1.3"}, []byte{1, 2, 3, 4},
		dataelem.NewDataElement(tag.New(0x0012, 0x0062), dataelem.CS, "YES"),
		dataelem.NewDataElement(tag.New(0x0020, 0x0013), dataelem.LO, "7"))

	out, code := runDiff(t, "--json", a, b)
	if code != 1 {
		t.Fatalf("status %d, want 1", code)
	}
	doc := decodeDiff(t, out)

	cases := []struct {
		path, kind string
		differs    []string
	}{
		{"(0010,0010)", "changed", []string{"value"}},
		{"(0010,1010)", "onlyInA", nil},
		{"(0012,0062)", "onlyInB", nil},
		{"(0020,0013)", "changed", []string{"vr"}},
		{"(0008,1140)[1]/(0008,1150)", "changed", []string{"value"}},
		{"(0008,1140)[2]", "onlyInB", nil},
	}
	for _, c := range cases {
		e := doc.find(c.path)
		if e == nil {
			t.Errorf("no difference at %s; got %d: %s", c.path, len(doc.Differences), out)
			continue
		}
		if e.Kind != c.kind || strings.Join(e.Differs, ",") != strings.Join(c.differs, ",") {
			t.Errorf("%s: kind %q differs %v, want %q %v", c.path, e.Kind, e.Differs, c.kind, c.differs)
		}
	}
	if len(doc.Differences) != len(cases) {
		t.Errorf("%d differences, want %d: %s", len(doc.Differences), len(cases), out)
	}
	if e := doc.find("(0010,0010)"); e != nil && (e.A == nil || e.A.Value == nil || *e.A.Value != "Doe^John" || *e.B.Value != "Smith^Jane") {
		t.Errorf("the changed name is not given as each file has it: %+v", e)
	}
	if e := doc.find("(0008,1140)[2]"); e != nil && (e.Item == nil || *e.Item != 2) {
		t.Errorf("an extra item does not say which: %+v", e)
	}

	text, _ := runDiff(t, a, b)
	for _, want := range []string{
		"(0010,0010) Patient's Name\n- PN Doe^John\n+ PN Smith^Jane\n",
		"(0010,1010) Patient's Age\n- AS 045Y\n",
		"(0012,0062) Patient Identity Removed\n+ CS YES\n",
		"(0008,1140)[1]/(0008,1150) Referenced SOP Class UID\n- UI 1.2\n+ UI 9.9\n",
		"(0008,1140)[2] Referenced Image Sequence\n+ SQ <1 items>\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the text form lacks %q:\n%s", want, text)
		}
	}
}

// A bulk value is compared from the two files in chunks, and says where it
// first differs: past the first chunk, in the last byte, and by one file
// being longer than the other.
func TestDiffComparesBulkValuesByStreaming(t *testing.T) {
	dir := t.TempDir()
	const size = 3*diffChunk + 1000
	pixels := make([]byte, size)
	for i := range pixels {
		pixels[i] = byte(i * 7)
	}
	a := filepath.Join(dir, "a.dcm")
	writeDiffFixture(t, a, "X", nil, pixels)

	for _, c := range []struct {
		name  string
		at    int64
		other []byte
	}{
		{"in the third chunk", 2*diffChunk + 17, nil},
		{"in the last byte", size - 1, nil},
		{"on a chunk boundary", diffChunk, nil},
		{"one longer", size, append(append([]byte{}, pixels...), 0, 0)},
	} {
		t.Run(c.name, func(t *testing.T) {
			other := c.other
			if other == nil {
				other = append([]byte{}, pixels...)
				other[c.at] ^= 0xFF
			}
			b := filepath.Join(dir, "b.dcm")
			writeDiffFixture(t, b, "X", nil, other)
			out, code := runDiff(t, "--json", a, b)
			doc := decodeDiff(t, out)
			e := doc.find("(7FE0,0010)")
			if code != 1 || e == nil || e.FirstDifference == nil || *e.FirstDifference != c.at {
				t.Fatalf("status %d, entry %+v, want the first difference at byte %d\n%s", code, e, c.at, out)
			}
			if len(doc.Differences) != 1 {
				t.Errorf("%d differences, want only the pixel data: %s", len(doc.Differences), out)
			}
			text, _ := runDiff(t, a, b)
			if !strings.Contains(text, "first difference at byte "+itoa(c.at)) {
				t.Errorf("the text form does not say where:\n%s", text)
			}
		})
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// Words of a bulk value are the other way round in a big endian file, and
// are the same pixels. They are compared as they mean, a byte swap not
// being a difference.
func TestSwapWordsMakesABigEndianStreamComparable(t *testing.T) {
	little := []byte{1, 0, 2, 0, 3, 0, 4, 0, 9}
	big := []byte{0, 1, 0, 2, 0, 3, 0, 4, 9}
	at, err := streamsDiffer(bytes.NewReader(little), bytes.NewReader(big), 1, 2)
	if err != nil || at != -1 {
		t.Fatalf("the same words in two byte orders differ at %d (%v)", at, err)
	}
	at, _ = streamsDiffer(bytes.NewReader(little), bytes.NewReader(big), 1, 1)
	if at != 0 {
		t.Fatalf("without the swap they differ at byte 0, got %d", at)
	}
	swapped := bytes.Clone(big)
	swapWords(swapped, 4)
	if !bytes.Equal(swapped[:4], []byte{2, 0, 1, 0}) || swapped[8] != 9 {
		t.Errorf("swapWords(4) = %v", swapped)
	}
}

// --ignore leaves a tag out wherever it is, given as a keyword, as numbers
// in either form, or in a list, and --ignore-meta and --ignore-private leave
// out the groups they name.
func TestDiffOptionsLeaveElementsOut(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.dcm"), filepath.Join(dir, "b.dcm")
	writeDiffFixture(t, a, "Doe^John", []string{"1.1"}, nil,
		dataelem.NewDataElement(tag.New(0x0009, 0x1001), dataelem.LO, "private"))
	writeDiffFixture(t, b, "Smith^Jane", []string{"2.2"}, nil)

	paths := func(args ...string) []string {
		out, _ := runDiff(t, append([]string{"--json", a, b}, args...)...)
		var got []string
		for _, e := range decodeDiff(t, out).Differences {
			got = append(got, e.Path)
		}
		return got
	}
	all := strings.Join(paths(), " ")
	for _, want := range []string{"(0010,0010)", "(0008,1140)[0]/(0008,1150)", "(0009,1001)"} {
		if !strings.Contains(all, want) {
			t.Fatalf("without options %s is not a difference: %s", want, all)
		}
	}
	for _, c := range []struct {
		args   []string
		gone   string
		stays  string
		reason string
	}{
		{[]string{"--ignore", "PatientName"}, "(0010,0010)", "(0009,1001)", "a keyword"},
		{[]string{"--ignore", "(0010,0010)"}, "(0010,0010)", "(0009,1001)", "a tag in parentheses"},
		{[]string{"--ignore", "00100010"}, "(0010,0010)", "(0009,1001)", "a tag as eight digits"},
		{[]string{"--ignore", "PatientName,ReferencedSOPClassUID"}, "(0008,1140)[0]/(0008,1150)", "(0009,1001)", "a list, inside an item"},
		{[]string{"--ignore", "(0010,0010),(0009,1001)"}, "(0009,1001)", "(0008,1140)[0]/(0008,1150)", "a list of tags"},
		{[]string{"--ignore", "PatientName", "--ignore", "0009,1001"}, "(0009,1001)", "(0008,1140)[0]/(0008,1150)", "the option repeated"},
		{[]string{"--ignore-private"}, "(0009,1001)", "(0010,0010)", "private elements"},
	} {
		got := strings.Join(paths(c.args...), " ")
		if strings.Contains(got, c.gone) || !strings.Contains(got, c.stays) {
			t.Errorf("%s (%v): %s", c.reason, c.args, got)
		}
	}

	// The file meta group differs by the instance UIDs written in it.
	writeDiffFixture(t, b, "Doe^John", []string{"1.1"}, nil,
		dataelem.NewDataElement(tag.New(0x0009, 0x1001), dataelem.LO, "private"))
	if err := os.WriteFile(b, rewriteMetaInstanceUID(t, b), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(), " "); !strings.Contains(got, "(0002,0003)") {
		t.Fatalf("the file meta group was not compared: %s", got)
	}
	if got := strings.Join(paths("--ignore-meta"), " "); got != "" {
		t.Errorf("--ignore-meta left %s", got)
	}
}

// rewriteMetaInstanceUID returns the file with the Media Storage SOP
// Instance UID of its file meta group changed, to the same length.
func rewriteMetaInstanceUID(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := data[:400]
	i := bytes.Index(head, []byte("1.2.3.4"))
	if i < 0 || i > 400 {
		t.Fatal("the instance UID is not in the file meta group")
	}
	copy(data[i:], "1.2.3.5")
	return data
}

// A bad tag, a missing file, a file that is not DICOM and a wrong number of
// arguments are status 2 with a message, which a script cannot mistake for
// "the files differ", and -h is status 0.
func TestDiffErrorsAreStatusTwo(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.dcm")
	writeDiffFixture(t, good, "X", nil, nil)
	notDICOM := filepath.Join(dir, "text.dcm")
	if err := os.WriteFile(notDICOM, []byte("this is not a DICOM file at all, only text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"missing file":  {good, filepath.Join(dir, "none.dcm")},
		"not DICOM":     {good, notDICOM},
		"one argument":  {good},
		"three":         {good, good, good},
		"unknown tag":   {"--ignore", "NotAKeyword", good, good},
		"unknown flags": {"--nope", good, good},
	} {
		out, code := runDiff(t, args...)
		if code != 2 || out != "" {
			t.Errorf("%s: status %d, output %q, want 2 and nothing on standard output", name, code, out)
		}
	}
	if _, code := runDiff(t, "-h"); code != 0 {
		t.Errorf("-h is status %d", code)
	}
}

// The built binary ends with the status the command asks for: 0, 1 and 2,
// where main ended every error with 1, and its message is on standard
// error alone.
func TestDiffExitStatusOfTheBinary(t *testing.T) {
	binary := buildCLI(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.dcm"), filepath.Join(dir, "b.dcm")
	writeDiffFixture(t, a, "Doe^John", nil, nil)
	writeDiffFixture(t, b, "Doe^Jane", nil, nil)

	run := func(args ...string) (stdout, stderr string, code int) {
		cmd := exec.Command(binary, args...)
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return so.String(), se.String(), code
	}
	if so, se, code := run("diff", a, a); code != 0 || so != "" || se != "" {
		t.Errorf("equal: status %d, %q, %q", code, so, se)
	}
	if so, se, code := run("diff", a, b); code != 1 || !strings.Contains(so, "+ PN Doe^Jane") || se != "" {
		t.Errorf("different: status %d, %q, %q", code, so, se)
	}
	if so, se, code := run("diff", a, filepath.Join(dir, "none")); code != 2 || so != "" || !strings.Contains(se, "none") {
		t.Errorf("error: status %d, %q, %q", code, so, se)
	}
	if _, _, code := run("show", filepath.Join(dir, "none")); code != 1 {
		t.Errorf("another command's error is status %d, want it left at 1", code)
	}
	if so, _, code := run("help", "diff"); code != 0 || !strings.Contains(so, "EXIT STATUS") {
		t.Errorf("help diff: status %d, %q", code, so)
	}
}

// The pydicom corpus: a file against its own copy in another byte order and
// VR encoding differs in the transfer syntax and in nothing else, pixel data
// included, where a compared byte for byte it would differ in every pixel.
func TestDiffCorpusCopiesInOtherTransferSyntaxes(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	big, implicit := filepath.Join(dir, "MR_small_bigendian.dcm"), filepath.Join(dir, "MR_small_implicit.dcm")
	out, code := runDiff(t, "--json", big, implicit)
	doc := decodeDiff(t, out)
	if code != 1 || len(doc.Differences) != 1 || doc.Differences[0].Path != "(0002,0010)" {
		t.Fatalf("status %d, differences %+v; want the transfer syntax alone", code, doc.Differences)
	}
	// Streamed from the files, and reported as what it is: encapsulated or
	// not, and fragment by fragment.
	rle, native := filepath.Join(dir, "MR_small_RLE.dcm"), filepath.Join(dir, "MR_small.dcm")
	out, _ = runDiff(t, "--json", rle, native)
	pixels := decodeDiff(t, out).find("(7FE0,0010)")
	if pixels == nil || pixels.A.Fragments == nil || *pixels.A.Fragments != 2 || pixels.B.Fragments != nil ||
		!strings.Contains(pixels.Detail, "encapsulated") {
		t.Errorf("RLE against native pixel data: %+v", pixels)
	}
}

// A file against its anonymized copy: what the profile changes is what is
// reported, the file meta group included, and what it keeps is not.
func TestDiffACorpusFileAgainstItsAnonymizedCopy(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	src := filepath.Join(corpus, "CT_small.dcm")
	anon := filepath.Join(t.TempDir(), "anon.dcm")
	if err := (&AnonymizeCommand{}).Execute([]string{src, anon}); err != nil {
		t.Fatal(err)
	}
	out, code := runDiff(t, "--json", src, anon)
	doc := decodeDiff(t, out)
	if code != 1 {
		t.Fatalf("status %d", code)
	}
	for path, kind := range map[string]string{
		"(0010,0010)": "changed", // Patient's Name, emptied
		"(0012,0062)": "onlyInB", // Patient Identity Removed
		"(0002,0003)": "changed", // the new instance UID, in the file meta group
		"(0008,0018)": "changed", // and in the data set
		"(0010,1002)": "onlyInA", // Other Patient IDs Sequence
	} {
		if e := doc.find(path); e == nil || e.Kind != kind {
			t.Errorf("%s: %+v, want %s", path, e, kind)
		}
	}
	for _, kept := range []string{"(0028,0010)", "(0028,0011)", "(7FE0,0010)", "(0008,0060)"} {
		if e := doc.find(kept); e != nil {
			t.Errorf("%s is the same in both and is reported: %+v", kept, e)
		}
	}
	// And the same two files with the changed elements named away are equal.
	if _, code := runDiff(t, "--ignore-meta", "--ignore-private", "-q", src, src); code != 0 {
		t.Errorf("a file against itself is status %d", code)
	}
}

// A file that could only be partly read is compared as far as it went and
// says so, rather than reporting "no difference" for what was never read.
func TestDiffSaysWhenAFileWasOnlyPartlyRead(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	path := filepath.Join(corpus, "MR_truncated.dcm")
	out, code := runDiff(t, "--json", path, path)
	doc := decodeDiff(t, out)
	if code != 0 || len(doc.Warnings.A) == 0 || !strings.Contains(doc.Warnings.A[0], "truncated") {
		t.Errorf("status %d, warnings %v", code, doc.Warnings)
	}
}

// A VR is compared where both files state one. An implicit VR file states
// none, and the VR it is shown under is what the reader made of an "OB or
// OW": a copy of a file in implicit VR must not differ in it.
func TestDiffDoesNotCompareAVRTheFileDoesNotState(t *testing.T) {
	scope := func() *displayScope { return &displayScope{deciding: dataset.NewDataset()} }
	element := func(vr string, explicit bool) *filereader.DataElementValue {
		return &filereader.DataElementValue{
			Tag: tag.New(0x0028, 0x0010), VR: vr, Value: []byte{1, 0},
			Position: filereader.ElementPosition{ExplicitVR: explicit},
		}
	}
	for _, c := range []struct {
		name string
		a, b *filereader.DataElementValue
		want int
	}{
		{"explicit against implicit", element("US", true), element("SS", false), 0},
		{"implicit against implicit", element("US", false), element("SS", false), 0},
		{"explicit against explicit", element("US", true), element("SS", true), 1},
	} {
		d := &differ{ignore: map[tag.Tag]bool{}}
		d.compareElement("(0028,0010)", c.a, c.b, scope(), scope())
		if len(d.entries) != c.want {
			t.Errorf("%s: %d differences, want %d: %+v", c.name, len(d.entries), c.want, d.entries)
		}
	}
}
