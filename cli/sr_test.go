package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The sr package reads a content tree and no command printed one, so reading a
// report meant writing a program (#359).

// runSR runs the command in the test process and returns what it printed.
func runSR(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := (&SRCommand{out: &out}).Execute(args)
	return out.String(), err
}

// srCorpus is a report of pydicom's corpus, skipping the test without it.
func srCorpus(t *testing.T, name string) string {
	t.Helper()
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	return filepath.Join(corpus, name)
}

// decodeSR reads the JSON document of a file.
func decodeSR(t *testing.T, path string) srDocument {
	t.Helper()
	out, err := runSR(t, "--json", path)
	if err != nil {
		t.Fatal(err)
	}
	var doc srDocument
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	return doc
}

// flatten lists a tree depth first, the order a document reads in.
func (n *srNode) flatten() []*srNode {
	out := []*srNode{n}
	for i := range n.Children {
		out = append(out, n.Children[i].flatten()...)
	}
	return out
}

// pydicom's test-SR.dcm, the report dcmtk's own tests use: 29 content items of
// nearly every value type. Each line is what dsrdump prints for the node, in
// the words of this command: the same nodes, in the same order, with the same
// values.
func TestSRPrintsTheCorpusReportAsDsrdumpReadsIt(t *testing.T) {
	out, err := runSR(t, srCorpus(t, "test-SR.dcm"))
	if err != nil {
		t.Fatal(err)
	}
	body := out[strings.Index(out, "\n\n")+2:]
	want := []string{
		`CONTAINER  "Diagnosis" [1111, TEST] = SEPARATE  @ 2001-02-13 18:47:46`,
		`  has obs context UIDREF  "Some UID" [1234.0, 99_OFFIS_DCMTK] = 1.2.3.4.5`,
		`  contains CONTAINER = CONTINUOUS`,
		`    contains TEXT  "Text Code" [1234, 99_OFFIS_DCMTK] = "A mass of"`,
		`      has concept mod CODE  "Code" [1234, 99_OFFIS_DCMTK] = "Sample Code 1" [2222, 99_OFFIS_DCMTK]`,
		`      has concept mod CODE  "Code" [1234, 99_OFFIS_DCMTK] = "Sample Code 2" [2222, 99_OFFIS_DCMTK]`,
		`    contains NUM  "Diameter" [1234, 99_OFFIS_DCMTK] = 3 cm "Length Unit" [cm, 99_OFFIS_DCMTK]`,
		`      has concept mod CODE  "Code" [1234, 99_OFFIS_DCMTK] = "Sample Code" [2222, 99_OFFIS_DCMTK]`,
		`    contains TEXT  "Text Code" [1234, 99_OFFIS_DCMTK] = "was detected."`,
		`    contains CONTAINER = SEPARATE`,
		`      contains TEXT  "Text Code" [1234, 99_OFFIS_DCMTK] = "A mass of"`,
		`      contains NUM  "Diameter" [1234, 99_OFFIS_DCMTK] = 3 cm "Length Unit" [cm, 99_OFFIS_DCMTK]`,
		`      contains TEXT  "Text Code" [1234, 99_OFFIS_DCMTK] = "was detected."`,
		`  contains TEXT  "Code" [1234, 99_OFFIS_DCMTK] = "Sample Text\rA\nB\r\nC\n\r"`,
		`    inferred from TEXT  "Code" [1234, 99_OFFIS_DCMTK] = "Inferred Sample Text\nNew line.\n\r&%$§\"!()<>{}/;"`,
		`    has properties SCOORD  "SCoord Code" [1234, 99_OFFIS_DCMTK] = CIRCLE 0/0 255/255`,
		`    has properties TCOORD  "TCoord Code" [1234, 99_OFFIS_DCMTK] = SEGMENT offsets 1.000000,2.500000`,
		`      selected from -> 1.3.2`,
		`  contains COMPOSITE = Basic Text SR [1.2.840.10008.5.1.4.1.1.88.11] 9.8.7.6`,
		`    has acq context DATE  "Date" [1234.1, 99_OFFIS_DCMTK] = 2000-12-06`,
		`    has acq context TIME  "Time" [1234.2, 99_OFFIS_DCMTK] = 12:00:00`,
		`    has acq context DATETIME  "DateTime" [1234.3, 99_OFFIS_DCMTK] = 2000-12-06 12:00:00`,
		`  contains IMAGE = CT Image [1.2.840.10008.5.1.4.1.1.2] 1.2.3.4.5.0 frames 5,2 shown with ` +
			`Grayscale Softcopy Presentation State [1.2.840.10008.5.1.4.1.1.11.1] 1.2.3.5.6.7  @ 2001-02-13 18:47:46`,
		`    has concept mod CODE  "Code" [1234, 99_OFFIS_DCMTK] = "Sample Code 3" [2222, 99_OFFIS_DCMTK]`,
		`      has concept mod CODE  "Code" [1234, 99_OFFIS_DCMTK] = "Sample Code 2" [2222, 99_OFFIS_DCMTK]`,
		`        inferred from -> 1.2.2.1`,
		`    has concept mod TEXT  "Code" [1234, 99_OFFIS_DCMTK] = "Sample Text 2"  @ 2001-02-13 18:47:46`,
		`      has properties IMAGE  "Key Image" [1234, 99_OFFIS_DCMTK] = MR Image [1.2.840.10008.5.1.4.1.1.4] 1.2.3.4.0.1`,
		`      has properties WAVEFORM = Hemodynamic Waveform [1.2.840.10008.5.1.4.1.1.9.2.1] 1.2.3.4.5 channels 5/3,2/0`,
	}
	got := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d:\n%s", len(got), len(want), body)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %s\nwant %s", i+1, got[i], want[i])
		}
	}
	for _, header := range []string{"Comprehensive SR\n", "Patient: Test^S R\n", "Completion: COMPLETE  Verification: VERIFIED\n",
		"Content date/time: 2001-02-13 18:47:46\n", "29 content item(s)\n"} {
		if !strings.Contains(out, header) {
			t.Errorf("the header lacks %q:\n%s", header, out)
		}
	}
}

// The JSON of the same report gives each item its position, the one a
// by-reference relationship names its target by, and every value in a field of
// its own kind.
func TestSRJSONPositionsAndValues(t *testing.T) {
	doc := decodeSR(t, srCorpus(t, "test-SR.dcm"))
	if doc.SchemaVersion != 1 || doc.Document.ContentItemCount != 29 || doc.Document.SOPClassName != "Comprehensive SR" {
		t.Errorf("document: %+v", doc.Document)
	}
	nodes := doc.Content.flatten()
	if len(nodes) != 29 {
		t.Fatalf("%d nodes", len(nodes))
	}
	byPosition := map[string]*srNode{}
	for _, n := range nodes {
		if byPosition[n.Position] != nil {
			t.Errorf("position %s is given twice", n.Position)
		}
		byPosition[n.Position] = n
	}
	for _, n := range nodes {
		if n.ReferencedPosition == "" {
			continue
		}
		target := byPosition[n.ReferencedPosition]
		if target == nil || target.ValueType == "" {
			t.Errorf("%s points at %s, which is not an item with a value", n.Position, n.ReferencedPosition)
		}
		if n.ValueType != "" || n.Text != nil {
			t.Errorf("%s only refers to another item and has a value of its own: %+v", n.Position, n)
		}
	}
	if n := byPosition["1.2.2"]; n.Numeric == nil || n.Numeric.Value != "3" || n.Numeric.Units == nil ||
		n.Numeric.Units.Value != "cm" || n.Numeric.Units.Scheme != "99_OFFIS_DCMTK" || n.Numeric.Units.Meaning != "Length Unit" {
		t.Errorf("1.2.2: %+v", n.Numeric)
	}
	if n := byPosition["1.3.2"]; n.Spatial == nil || n.Spatial.GraphicType != "CIRCLE" ||
		len(n.Spatial.Points) != 2 || n.Spatial.Points[1][0] != 255 {
		t.Errorf("1.3.2: %+v", n.Spatial)
	}
	if n := byPosition["1.5"]; n.Reference == nil || n.Reference.SOPInstanceUID != "1.2.3.4.5.0" ||
		len(n.Reference.Frames) != 2 || n.Reference.PresentationState == nil ||
		n.Reference.PresentationState.SOPInstanceUID != "1.2.3.5.6.7" {
		t.Errorf("1.5: %+v", n.Reference)
	}
	if n := byPosition["1"]; n.ObservationDateTime != "20010213184746" || n.Continuity != "SEPARATE" {
		t.Errorf("root: %+v", n)
	}
}

// The templated report of pydicom's corpus has IMAGE items that refer to an
// invalid SOP class, which dcmtk will not read without being told to accept
// them: the tree is printed as the file has it.
func TestSRPrintsAReportWithInvalidReferences(t *testing.T) {
	for _, name := range []string{"reportsi.dcm", "reportsi_with_empty_number_tags.dcm"} {
		doc := decodeSR(t, srCorpus(t, name))
		nodes := doc.Content.flatten()
		if len(nodes) != 9 || doc.Document.SOPClassName != "Basic Text SR" {
			t.Errorf("%s: %d nodes, %+v", name, len(nodes), doc.Document)
		}
		var positions []string
		for _, n := range nodes {
			positions = append(positions, n.Position)
		}
		if got := strings.Join(positions, " "); got != "1 1.1 1.2 1.3 1.4 1.5 1.5.1 1.5.1.1 1.5.2" {
			t.Errorf("%s: positions %s", name, got)
		}
	}
}

// A file that is not a report is refused by what it lacks, not printed as an
// empty tree.
func TestSRRefusesAFileThatIsNotAReport(t *testing.T) {
	out, err := runSR(t, srCorpus(t, "CT_small.dcm"))
	if err == nil || !strings.Contains(err.Error(), "Value Type") || out != "" {
		t.Errorf("a CT image: %v, %q", err, out)
	}
	if _, err := runSR(t, filepath.Join(t.TempDir(), "none.dcm")); err == nil {
		t.Error("a missing file was accepted")
	}
	if _, err := runSR(t); err == nil {
		t.Error("no file was accepted")
	}
}

// srHelper builds the data sets of a report by hand.
type srHelper struct{ t *testing.T }

func (h srHelper) add(ds *dataset.Dataset, tg tag.Tag, vr dataelem.VR, value any) {
	h.t.Helper()
	if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
		h.t.Fatal(err)
	}
}

func (h srHelper) seq(ds *dataset.Dataset, tg tag.Tag, items ...*dataset.Dataset) {
	h.t.Helper()
	s := sequence.New()
	for _, item := range items {
		if err := s.Append(item); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := ds.AddSequence(tg, s); err != nil {
		h.t.Fatal(err)
	}
}

func (h srHelper) code(ds *dataset.Dataset, tg tag.Tag, value, scheme, meaning string) {
	c := dataset.NewDataset()
	h.add(c, tag.New(0x0008, 0x0100), dataelem.SH, value)
	h.add(c, tag.New(0x0008, 0x0102), dataelem.SH, scheme)
	h.add(c, tag.New(0x0008, 0x0104), dataelem.LO, meaning)
	h.seq(ds, tg, c)
}

func (h srHelper) item(rel, valueType, concept string) *dataset.Dataset {
	ds := dataset.NewDataset()
	if rel != "" {
		h.add(ds, tag.New(0x0040, 0xA010), dataelem.CS, rel)
	}
	h.add(ds, tag.New(0x0040, 0xA040), dataelem.CS, valueType)
	if concept != "" {
		h.code(ds, tag.New(0x0040, 0xA043), "C1", "99TEST", concept)
	}
	return ds
}

func floats(v ...float32) []byte {
	out := make([]byte, 0, 4*len(v))
	for _, f := range v {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(f))
	}
	return out
}

func uint16s(v ...uint16) []byte {
	out := make([]byte, 0, 2*len(v))
	for _, n := range v {
		out = binary.LittleEndian.AppendUint16(out, n)
	}
	return out
}

func uint32s(v ...uint32) []byte {
	out := make([]byte, 0, 4*len(v))
	for _, n := range v {
		out = binary.LittleEndian.AppendUint32(out, n)
	}
	return out
}

// writeGeneratedReport writes a report with what the corpus has none of: a
// number that is not given as one, a 3D spatial coordinate, a TCOORD by sample
// positions, an image with segments, and the date and time in each of their
// forms.
func writeGeneratedReport(t *testing.T, path string) {
	t.Helper()
	h := srHelper{t}
	root := h.item("", "CONTAINER", "Report")
	h.add(root, tag.New(0x0040, 0xA050), dataelem.CS, "SEPARATE")
	h.add(root, tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.88.33")
	h.add(root, tag.New(0x0008, 0x0018), dataelem.UI, "1.2.3.4")
	h.add(root, tag.New(0x0010, 0x0010), dataelem.PN, "Test^SR")
	h.add(root, tag.New(0x0040, 0xA491), dataelem.CS, "COMPLETE")

	var kids []*dataset.Dataset

	nan := h.item("CONTAINS", "NUM", "Volume")
	h.code(nan, tag.New(0x0040, 0xA301), "114000", "DCM", "Not a number")
	kids = append(kids, nan)

	num := h.item("CONTAINS", "NUM", "Diameter")
	measured := dataset.NewDataset()
	h.add(measured, tag.New(0x0040, 0xA30A), dataelem.DS, "12.5")
	h.code(measured, tag.New(0x0040, 0x08EA), "mm", "UCUM", "millimeter")
	h.seq(num, tag.New(0x0040, 0xA300), measured)
	kids = append(kids, num)

	s3 := h.item("CONTAINS", "SCOORD3D", "Region")
	h.add(s3, tag.New(0x0070, 0x0023), dataelem.CS, "POLYLINE")
	h.add(s3, tag.New(0x0070, 0x0022), dataelem.FL, floats(1, 2, 3.5, 4, 5, 6))
	h.add(s3, tag.New(0x3006, 0x0024), dataelem.UI, "1.2.3.4.5.6")
	kids = append(kids, s3)

	tc := h.item("CONTAINS", "TCOORD", "Range")
	h.add(tc, tag.New(0x0040, 0xA130), dataelem.CS, "MULTIPOINT")
	h.add(tc, tag.New(0x0040, 0xA132), dataelem.UL, uint32s(100, 200, 300))
	kids = append(kids, tc)

	img := h.item("CONTAINS", "IMAGE", "Illustration")
	ref := dataset.NewDataset()
	h.add(ref, tag.New(0x0008, 0x1150), dataelem.UI, "1.2.840.10008.5.1.4.1.1.2")
	h.add(ref, tag.New(0x0008, 0x1155), dataelem.UI, "1.2.3.9")
	h.add(ref, tag.New(0x0008, 0x1160), dataelem.IS, "3\\4\\5")
	h.add(ref, tag.New(0x0062, 0x000B), dataelem.US, uint16s(1, 2))
	h.seq(img, tag.New(0x0008, 0x1199), ref)
	kids = append(kids, img)

	wave := h.item("CONTAINS", "WAVEFORM", "Source")
	wref := dataset.NewDataset()
	h.add(wref, tag.New(0x0008, 0x1150), dataelem.UI, "1.2.840.10008.5.1.4.1.1.9.1.1")
	h.add(wref, tag.New(0x0008, 0x1155), dataelem.UI, "1.2.3.10")
	h.add(wref, tag.New(0x0040, 0xA0B0), dataelem.US, uint16s(1, 2, 3, 4))
	h.seq(wave, tag.New(0x0008, 0x1199), wref)
	kids = append(kids, wave)

	for _, c := range []struct {
		vt, tg, value string
		t             tag.Tag
		vr            dataelem.VR
	}{
		{"DATE", "", "20200229", tag.New(0x0040, 0xA121), dataelem.DA},
		{"TIME", "", "093015.25", tag.New(0x0040, 0xA122), dataelem.TM},
		{"DATETIME", "", "20200229093015+0100", tag.New(0x0040, 0xA120), dataelem.DT},
		{"TEXT", "", "line one\nline \"two\"\ttab", tag.New(0x0040, 0xA160), dataelem.UT},
	} {
		n := h.item("HAS OBS CONTEXT", c.vt, c.vt+" item")
		h.add(n, c.t, c.vr, c.value)
		kids = append(kids, n)
	}
	byRef := dataset.NewDataset()
	h.add(byRef, tag.New(0x0040, 0xA010), dataelem.CS, "INFERRED FROM")
	h.add(byRef, tag.New(0x0040, 0xDB73), dataelem.UL, uint32s(1, 3))
	h.seq(num, tag.New(0x0040, 0xA730), byRef)

	h.seq(root, tag.New(0x0040, 0xA730), kids...)
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.88.33", "1.2.3.4", root); err != nil {
		t.Fatal(err)
	}
}

// What the corpus has none of, checked against dsrdump when this was written
// (the same file through dsrdump +Pn +Pc +Pl +Pu +Psu printed the same nodes
// and values): a number given by its qualifier, 3D points in their frame of
// reference, a TCOORD by sample positions, an image's frames and segments, a
// waveform's channels, the forms of DA, TM and DT, and a text with a quote, a
// line break and a tab in it, which stays on one line.
func TestSRPrintsWhatTheCorpusLacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.dcm")
	writeGeneratedReport(t, path)
	out, err := runSR(t, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`contains NUM  "Volume" [C1, 99TEST] = qualifier "Not a number" [114000, DCM]`,
		`contains NUM  "Diameter" [C1, 99TEST] = 12.5 mm "millimeter" [mm, UCUM]`,
		`    inferred from -> 1.3`,
		`SCOORD3D  "Region" [C1, 99TEST] = POLYLINE 1/2/3.5 4/5/6 in frame of reference 1.2.3.4.5.6`,
		`TCOORD  "Range" [C1, 99TEST] = MULTIPOINT samples 100,200,300`,
		`IMAGE  "Illustration" [C1, 99TEST] = CT Image [1.2.840.10008.5.1.4.1.1.2] 1.2.3.9 frames 3,4,5 segments 1,2`,
		`WAVEFORM  "Source" [C1, 99TEST] = 12-lead ECG Waveform [1.2.840.10008.5.1.4.1.1.9.1.1] 1.2.3.10 channels 1/2,3/4`,
		`DATE  "DATE item" [C1, 99TEST] = 2020-02-29`,
		`TIME  "TIME item" [C1, 99TEST] = 09:30:15.25`,
		`DATETIME  "DATETIME item" [C1, 99TEST] = 2020-02-29 09:30:15+0100`,
		`TEXT  "TEXT item" [C1, 99TEST] = "line one\nline \"two\"\ttab"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	doc := decodeSR(t, path)
	if n := doc.Content.Children[0]; n.Numeric == nil || n.Numeric.Value != "" || n.Numeric.Qualifier == nil || n.Numeric.Qualifier.Value != "114000" {
		t.Errorf("a number given by its qualifier: %+v", n.Numeric)
	}
}

// Dates and times are written as people read them where they are well formed,
// and as the file has them where they are not, and never made up.
func TestSRFormatsDatesAndTimesOnlyWhereTheyAreWellFormed(t *testing.T) {
	for _, c := range []struct {
		name string
		fn   func(string) string
		in   string
		want string
	}{
		{"date", formatSRDate, "20001206", "2000-12-06"},
		{"date, partial", formatSRDate, "2000", "2000"},
		{"date, with separators", formatSRDate, "2000-12-06", "2000-12-06"},
		{"date, not digits", formatSRDate, "2000120x", "2000120x"},
		{"time, hours and minutes", formatSRTime, "1200", "12:00"},
		{"time", formatSRTime, "120000", "12:00:00"},
		{"time, fraction", formatSRTime, "093015.250000", "09:30:15.250000"},
		{"time, hours", formatSRTime, "12", "12"},
		{"time, odd length", formatSRTime, "123", "123"},
		{"time, not digits", formatSRTime, "abcd", "abcd"},
		{"date and time", formatSRDateTime, "20001206120000", "2000-12-06 12:00:00"},
		{"date and minutes", formatSRDateTime, "200012061200", "2000-12-06 12:00"},
		{"date, time and offset", formatSRDateTime, "20001206120000-0500", "2000-12-06 12:00:00-0500"},
		{"date alone", formatSRDateTime, "20001206", "2000-12-06"},
		{"nothing written", formatSRDateTime, "", ""},
		{"not a date", formatSRDateTime, "yesterday", "yesterday"},
	} {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("%s: %q gave %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// writeDocumentedReport writes a report with what a reader of a verified
// document needs beyond its content: two verifying observers (one without a
// code), two predecessor studies, a template on the root and on two items
// below it, and an image that names two Real World Value Mapping instances
// beside one that names none.
func writeDocumentedReport(t *testing.T, path string) {
	t.Helper()
	h := srHelper{t}
	root := h.item("", "CONTAINER", "Report")
	h.add(root, tag.New(0x0040, 0xA050), dataelem.CS, "SEPARATE")
	h.add(root, tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.88.33")
	h.add(root, tag.New(0x0008, 0x0018), dataelem.UI, "1.2.3.4")
	h.add(root, tag.New(0x0040, 0xA493), dataelem.CS, "VERIFIED")
	template := func(ds *dataset.Dataset, id, resource string) {
		item := dataset.NewDataset()
		h.add(item, tag.New(0x0008, 0x0105), dataelem.CS, resource)
		h.add(item, tag.New(0x0040, 0xDB00), dataelem.CS, id)
		h.seq(ds, tag.New(0x0040, 0xA504), item)
	}
	template(root, "1500", "DCMR")

	first := dataset.NewDataset()
	h.add(first, tag.New(0x0040, 0xA075), dataelem.PN, "Verifier^First")
	h.add(first, tag.New(0x0040, 0xA027), dataelem.LO, "General Hospital")
	h.add(first, tag.New(0x0040, 0xA030), dataelem.DT, "20200301101500")
	h.code(first, tag.New(0x0040, 0xA088), "V1", "99LOCAL", "Dr. First")
	second := dataset.NewDataset()
	h.add(second, tag.New(0x0040, 0xA075), dataelem.PN, "Verifier^Second")
	h.add(second, tag.New(0x0040, 0xA030), dataelem.DT, "20200302111500")
	h.seq(root, tag.New(0x0040, 0xA073), first, second)

	predecessor := func(study, series string, instances ...string) *dataset.Dataset {
		p := dataset.NewDataset()
		h.add(p, tag.New(0x0020, 0x000D), dataelem.UI, study)
		s := dataset.NewDataset()
		h.add(s, tag.New(0x0020, 0x000E), dataelem.UI, series)
		var refs []*dataset.Dataset
		for _, instance := range instances {
			r := dataset.NewDataset()
			h.add(r, tag.New(0x0008, 0x1150), dataelem.UI, "1.2.840.10008.5.1.4.1.1.88.33")
			h.add(r, tag.New(0x0008, 0x1155), dataelem.UI, instance)
			refs = append(refs, r)
		}
		h.seq(s, tag.New(0x0008, 0x1199), refs...)
		h.seq(p, tag.New(0x0008, 0x1115), s)
		return p
	}
	h.seq(root, tag.New(0x0040, 0xA360),
		predecessor("1.2.3.1", "1.2.3.1.2", "1.2.3.1.2.1", "1.2.3.1.2.2"),
		predecessor("1.2.3.7", "1.2.3.1.3", "1.2.3.7.3.1"))

	group := h.item("CONTAINS", "CONTAINER", "Measurements")
	h.add(group, tag.New(0x0040, 0xA050), dataelem.CS, "SEPARATE")
	template(group, "1411", "DCMR")
	inner := h.item("CONTAINS", "CONTAINER", "Group")
	h.add(inner, tag.New(0x0040, 0xA050), dataelem.CS, "SEPARATE")
	template(inner, "1501", "99LOCAL")
	h.seq(group, tag.New(0x0040, 0xA730), inner)

	mapped := h.item("CONTAINS", "IMAGE", "Mapped")
	ref := dataset.NewDataset()
	h.add(ref, tag.New(0x0008, 0x1150), dataelem.UI, "1.2.840.10008.5.1.4.1.1.4")
	h.add(ref, tag.New(0x0008, 0x1155), dataelem.UI, "1.2.3.9")
	var mappings []*dataset.Dataset
	for _, uid := range []string{"1.2.3.70", "1.2.3.71"} {
		m := dataset.NewDataset()
		h.add(m, tag.New(0x0008, 0x1150), dataelem.UI, "1.2.840.10008.5.1.4.1.1.67")
		h.add(m, tag.New(0x0008, 0x1155), dataelem.UI, uid)
		mappings = append(mappings, m)
	}
	h.seq(ref, tag.New(0x0008, 0x114B), mappings...)
	h.seq(mapped, tag.New(0x0008, 0x1199), ref)
	plain := h.item("CONTAINS", "IMAGE", "Plain")
	plainRef := dataset.NewDataset()
	h.add(plainRef, tag.New(0x0008, 0x1150), dataelem.UI, "1.2.840.10008.5.1.4.1.1.2")
	h.add(plainRef, tag.New(0x0008, 0x1155), dataelem.UI, "1.2.3.10")
	h.seq(plain, tag.New(0x0008, 0x1199), plainRef)

	h.seq(root, tag.New(0x0040, 0xA730), group, mapped, plain)
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.88.33", "1.2.3.4", root); err != nil {
		t.Fatal(err)
	}
}

// TestSRPrintsObserversPredecessorsTemplatesAndMappings: what a verified
// report says about itself beyond its content, in text and in JSON: who
// verified it and when, which documents it replaces, the template of each
// item that names one (the root's stays in the header), and the Real World
// Value Mapping instances of an image. A report with none of them prints no
// field for them.
func TestSRPrintsObserversPredecessorsTemplatesAndMappings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.dcm")
	writeDocumentedReport(t, path)
	out, err := runSR(t, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Verifying observer: Verifier^First, General Hospital, 2020-03-01 10:15:00 \"Dr. First\" [V1, 99LOCAL]\n",
		"Verifying observer: Verifier^Second, 2020-03-02 11:15:00\n",
		"Predecessor document: Comprehensive SR [1.2.840.10008.5.1.4.1.1.88.33] 1.2.3.1.2.1 (series 1.2.3.1.2, study 1.2.3.1)\n",
		"Predecessor document: Comprehensive SR [1.2.840.10008.5.1.4.1.1.88.33] 1.2.3.1.2.2 (series 1.2.3.1.2, study 1.2.3.1)\n",
		"Predecessor document: Comprehensive SR [1.2.840.10008.5.1.4.1.1.88.33] 1.2.3.7.3.1 (series 1.2.3.1.3, study 1.2.3.7)\n",
		"Template: TID 1500 (DCMR)\n",
		`  contains CONTAINER  "Measurements" [C1, 99TEST] = SEPARATE  # TID 1411 (DCMR)`,
		`    contains CONTAINER  "Group" [C1, 99TEST] = SEPARATE  # TID 1501 (99LOCAL)`,
		`  contains IMAGE  "Mapped" [C1, 99TEST] = MR Image [1.2.840.10008.5.1.4.1.1.4] 1.2.3.9 mapped by ` +
			`Real World Value Mapping [1.2.840.10008.5.1.4.1.1.67] 1.2.3.70, Real World Value Mapping [1.2.840.10008.5.1.4.1.1.67] 1.2.3.71`,
		`  contains IMAGE  "Plain" [C1, 99TEST] = CT Image [1.2.840.10008.5.1.4.1.1.2] 1.2.3.10` + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "# TID") != 2 {
		t.Errorf("the root's template is also printed on its line, or one is missing:\n%s", out)
	}

	doc := decodeSR(t, path)
	h := doc.Document
	if doc.SchemaVersion != 1 {
		t.Errorf("schemaVersion %d: the fields were added, nothing was renamed or removed", doc.SchemaVersion)
	}
	if len(h.VerifyingObservers) != 2 || h.VerifyingObservers[0].Name != "Verifier^First" ||
		h.VerifyingObservers[0].Organization != "General Hospital" || h.VerifyingObservers[0].DateTime != "20200301101500" ||
		h.VerifyingObservers[0].Identification == nil || h.VerifyingObservers[0].Identification.Value != "V1" ||
		h.VerifyingObservers[1].Identification != nil {
		t.Errorf("verifying observers: %+v", h.VerifyingObservers)
	}
	if len(h.PredecessorDocuments) != 2 || h.PredecessorDocuments[0].StudyInstanceUID != "1.2.3.1" ||
		len(h.PredecessorDocuments[0].Series) != 1 || len(h.PredecessorDocuments[0].Series[0].Instances) != 2 ||
		h.PredecessorDocuments[1].Series[0].Instances[0].SOPInstanceUID != "1.2.3.7.3.1" {
		t.Errorf("predecessor documents: %+v", h.PredecessorDocuments)
	}
	byConcept := map[string]*srNode{}
	for _, n := range doc.Content.flatten() {
		if n.ConceptName != nil {
			byConcept[n.ConceptName.Meaning] = n
		}
	}
	if tmpl := byConcept["Group"].Template; tmpl == nil || tmpl.ID != "1501" || tmpl.Resource != "99LOCAL" {
		t.Errorf("the template of the inner container: %+v", tmpl)
	}
	if byConcept["Plain"].Template != nil || byConcept["Plain"].Reference.ValueMappings != nil {
		t.Error("an item with neither has a template or a mapping")
	}
	if m := byConcept["Mapped"].Reference.ValueMappings; len(m) != 2 || m[1].SOPInstanceUID != "1.2.3.71" || m[0].SOPClassName != "Real World Value Mapping" {
		t.Errorf("value mappings: %+v", m)
	}

	// A report that has none of them prints no field for any.
	bare := filepath.Join(t.TempDir(), "bare.dcm")
	writeGeneratedReport(t, bare)
	raw, err := runSR(t, "--json", bare)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"verifyingObservers", "predecessorDocuments", `"template"`, "realWorldValueMappings"} {
		if strings.Contains(raw, field) {
			t.Errorf("a report without them prints %s", field)
		}
	}
}

// pydicom's test-SR.dcm has two verifying observers and one predecessor
// document, which dsrdump prints in its header as "Verifying Observers" and
// "Predecessor Docs : 1"; they are printed here too, with the same names,
// organizations, times and code.
func TestSRPrintsTheCorpusReportsVerifiersAndPredecessor(t *testing.T) {
	out, err := runSR(t, srCorpus(t, "test-SR.dcm"))
	if err != nil {
		t.Fatal(err)
	}
	header := out[:strings.Index(out, "\n\n")]
	for _, want := range []string{
		"Verifying observer: Riesmeier^Jörg, OFFIS e.V., 2001-02-13 18:47:46 \"JR\" [1705, 99_OFFIS_DCMTK]\n",
		// The organization's name is the file's own, in British spelling.
		"Verifying observer: Observer^Verifying, Organi" + "sation, 2001-02-13 18:47:46\n",
		"Predecessor document: Comprehensive SR [1.2.840.10008.5.1.4.1.1.88.33] 1.2.276.0.7230010.3.1.4.2139363186.7819.982086466.1 " +
			"(series 1.2.276.0.7230010.3.1.4.2139363186.7819.982086466.3, study 1.2.276.0.7230010.3.1.4.2139363186.7819.982086466.2)\n",
	} {
		if !strings.Contains(header+"\n", want) {
			t.Errorf("the header lacks %q:\n%s", want, header)
		}
	}
	if strings.Count(header, "Predecessor document") != 1 {
		t.Errorf("one predecessor, as dsrdump counts:\n%s", header)
	}
}
